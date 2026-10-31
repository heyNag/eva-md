// Package update is the background update. Build turns a session's new
// inbox entries and its thread page into the model's input; a runner starts
// claude or codex in an empty run folder with a cleared environment, passes
// that batch on stdin with Rules and Schema, and sorts the outcome into an
// answer or a failure reason; Validate checks the answer against the batch
// and returns the threadpage.Update to apply. Those are pure but for the
// runners. Job ties them together: one job at a time under its own lock, it
// drains each session's backlog past its inbox watermark, writes the thread
// page through the okf adapter, and records every pass in the run log
// (<state>/updates/<YYYY-MM-DD>.jsonl), from which the daily budget, holds,
// backoff and quarantine are worked out.
package update

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"time"

	"github.com/heyNag/eva-md/internal/config"
)

// Hosts, named as everywhere else in eva-md.
const (
	HostClaude = "claude-code"
	HostCodex  = "codex"
)

// Reasons a run failed, recorded in Result.Reason. host_error is a network,
// server or CLI-usage failure: an outage, or a command line a host upgrade
// no longer takes. The host could not make the call, through no fault of the
// batch, so the update job (increment 7) backs off on it as on rate_limit and
// never counts it toward a session's quarantine.
const (
	ReasonCLIMissing    = "cli_missing"
	ReasonAuth          = "auth"
	ReasonRateLimit     = "rate_limit"
	ReasonHostError     = "host_error"
	ReasonModelRejected = "model_rejected"
	ReasonTimeout       = "timeout"
	ReasonInvalidOutput = "invalid_output"
	ReasonToolUsed      = "tool_used"
	ReasonMCPFailed     = "mcp_failed"
)

// NoteDefaultModel is the ModelNote of a run without a model flag because the
// chat's model is unknown. A run that fell back to the host default after a
// rejected model gets "fallback from <model>" instead.
const NoteDefaultModel = "default (chat model unknown)"

// The timeout grows with the effort's place in the host's table.
const (
	minTimeout = 180 * time.Second
	maxTimeout = 600 * time.Second
)

// Request is one headless run.
type Request struct {
	Host             string        // claude-code or codex
	Model            string        // empty: the host default, with ModelNote "default (chat model unknown)"
	Effort           string        // a value from the host's effort table, or "highest" (also when empty)
	Rules            string        // fixed instructions (Claude: --system-prompt; Codex: leads stdin)
	Input            []byte        // the batch, on stdin, never in an argument
	Schema           []byte        // the strict JSON schema, a JSON object
	RunDir           string        // an empty folder, by absolute path, used as the child's working directory
	CodexDisable     []string      // tool features to disable, from the list recorded at install
	IgnoreUserConfig bool          // codex_ignore_user_config
	Timeout          time.Duration // zero: derived from the effort; one budget for all attempts
	CLI              string        // the CLI's absolute path, as install recorded it; empty, or no longer the CLI: looked up on PATH
}

// Result is a run's outcome. Output is set only when Reason is empty and
// holds a JSON object, which the caller still validates against the schema.
type Result struct {
	Output              []byte // the structured JSON, validated by the caller
	Model               string // the model actually requested; empty for the host default
	ModelNote           string // "fallback from <model>" or "default (chat model unknown)"
	TokensIn, TokensOut int    // from the CLI's own report, when present, over all attempts; input includes cached tokens
	Attempts            int    // CLI runs made, including the retries after a refusal; a missing CLI made none
	Effort              string // the effort flag the successful attempt used; empty when it ran without one
	Reason              string // "" on success, else one of the Reason constants
	Degraded            string // a note kept beside valid output, e.g. an optional MCP server failed
}

// Runner runs a host CLI headless. A run that fails is a Result with a Reason
// and a nil error. An error means the request could not be run, or ctx ended
// before the run did.
type Runner interface {
	Run(ctx context.Context, r Request) (Result, error)
}

// NewRunner returns the runner for host, claude-code or codex. For any other
// host its Run returns an error.
func NewRunner(host string) Runner { return cliRunner{host: host} }

type cliRunner struct{ host string }

var _ Runner = cliRunner{}

// try holds one attempt's model and effort flags; an empty one is left out.
type try struct {
	model, effort string
	timeout       time.Duration
	noStrictMCP   bool // claude refused --strict-mcp-config, so it is left out
}

// attempt is one child run, classified.
type attempt struct {
	output              []byte
	reason              string
	effortRefused       bool // a model_rejected refusal names the effort, not only the model
	strictMCPRefused    bool // claude refused --strict-mcp-config
	degraded            string
	tokensIn, tokensOut int
}

// hostCLI is what differs between the hosts.
type hostCLI struct {
	efforts []string // accepted effort values, lowest first
	run     func(context.Context, Request, try) (attempt, error)
}

func lookupHost(host string) (hostCLI, bool) {
	switch host {
	case HostClaude:
		return hostCLI{efforts: claudeEfforts, run: runClaude}, true
	case HostCodex:
		return hostCLI{efforts: codexEfforts, run: runCodex}, true
	}
	return hostCLI{}, false
}

// Run runs r. A refusal that names the effort steps the effort down the
// host's table one entry at a time, so a model whose top level is below the
// table's top runs at its own top; below the lowest entry the run goes
// without the effort flag. Any other refusal drops the effort flag at once.
// A refusal with no effort flag left drops the model flag, which runs the
// host default. The attempts share the timeout, so the retries can't stretch
// the run.
func (c cliRunner) Run(ctx context.Context, r Request) (Result, error) {
	h, ok := lookupHost(c.host)
	if !ok {
		return Result{}, fmt.Errorf("update: unknown host %q", c.host)
	}
	if r.Host != c.host {
		return Result{}, fmt.Errorf("update: a request for host %q went to the %s runner", r.Host, c.host)
	}
	effort, err := resolveEffort(h.efforts, r.Effort)
	if err != nil {
		return Result{}, err
	}
	if err := check(r); err != nil {
		return Result{}, err
	}
	t := try{model: r.Model, effort: effort, timeout: r.Timeout}
	if t.timeout == 0 {
		t.timeout = effortTimeout(h.efforts, effort)
	}
	deadline := time.Now().Add(t.timeout)
	res := Result{Model: r.Model}
	if r.Model == "" {
		res.ModelNote = NoteDefaultModel
	}
	var mcpNote string
	for {
		a, err := h.run(ctx, r, t)
		if ctx.Err() != nil {
			return Result{}, fmt.Errorf("update: run canceled: %w", ctx.Err())
		}
		if err != nil {
			return Result{}, err
		}
		if a.reason != ReasonCLIMissing {
			res.Attempts++
		}
		res.TokensIn += a.tokensIn
		res.TokensOut += a.tokensOut
		switch {
		case a.reason == ReasonHostError && a.strictMCPRefused && !t.noStrictMCP:
			// An organization's managed MCP config makes claude refuse the
			// flag; --safe-mode still keeps the user's own MCP servers out.
			t.noStrictMCP = true
			mcpNote = "a managed MCP config refused --strict-mcp-config; the run went without it"
		case a.reason == ReasonModelRejected && a.effortRefused && t.effort != "":
			t.effort = lowerEffort(h.efforts, t.effort)
		case a.reason == ReasonModelRejected && t.effort != "":
			t.effort = ""
		case a.reason == ReasonModelRejected && t.model != "":
			t.model = ""
			res.Model, res.ModelNote = "", "fallback from "+r.Model
		default:
			res.Output, res.Reason = a.output, a.reason
			if a.reason == "" {
				res.Effort = t.effort
				res.Degraded = joinNotes(a.degraded, effortNote(r.Model, t, effort), mcpNote)
			}
			return res, nil
		}
		if t.timeout = time.Until(deadline); t.timeout <= 0 {
			res.Reason = ReasonTimeout
			return res, nil
		}
	}
}

// effortNote says when a run went below its effort or without the flag, or a
// refused effort setting would go unnoticed on every run. Once the model is
// dropped too, the host default runs at its default effort.
func effortNote(model string, t try, effort string) string {
	switch {
	case t.effort == effort:
		return ""
	case t.effort != "":
		return "effort " + effort + " was rejected; the run used " + t.effort
	case t.model != model:
		return "effort " + effort + " was dropped with the model; the run used the default effort"
	}
	return "effort " + effort + " was rejected; the run used the default effort"
}

// lowerEffort is the entry below effort in the host's table, or "" below the
// lowest, where the run goes without the effort flag.
func lowerEffort(levels []string, effort string) string {
	if i := slices.Index(levels, effort); i > 0 {
		return levels[i-1]
	}
	return ""
}

func joinNotes(notes ...string) string {
	var kept []string
	for _, n := range notes {
		if n != "" {
			kept = append(kept, n)
		}
	}
	return strings.Join(kept, "; ")
}

var (
	// A model name never starts with a dash, so it can't be read as a flag.
	// The rest allows aliases, dated ids, provider prefixes and "[1m]".
	modelName = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._:/@\[\]-]{0,199}$`)
	// Codex feature names are snake_case.
	featureName = regexp.MustCompile(`^[a-z][a-z0-9_]{0,63}$`)
)

// check refuses a request with nothing to run, with a value a CLI could read
// as another flag, or without an empty run folder, so nothing unexpected
// reaches the child. Blank rules would replace Claude's system prompt with
// nothing.
func check(r Request) error {
	switch {
	case len(bytes.TrimSpace(r.Input)) == 0:
		return errors.New("update: the batch is empty")
	case strings.TrimSpace(r.Rules) == "":
		return errors.New("update: the rules are empty")
	case r.Model != "" && !modelName.MatchString(r.Model):
		return errors.New("update: the model name has characters a CLI flag can't take")
	case strings.HasPrefix(r.Rules, "-") || strings.ContainsRune(r.Rules, 0):
		return errors.New("update: the rules must not start with a dash or hold a NUL byte")
	case !isObject(r.Schema):
		return errors.New("update: the schema is not a JSON object")
	case r.Timeout < 0:
		return errors.New("update: the timeout is negative")
	}
	for _, name := range r.CodexDisable {
		if !featureName.MatchString(name) {
			return fmt.Errorf("update: %q is not a Codex feature name", name)
		}
	}
	return checkRunDir(r.RunDir)
}

// checkRunDir requires an existing, empty folder: the child must find nothing
// there to read, and no answer left by an earlier run.
func checkRunDir(dir string) error {
	if !filepath.IsAbs(dir) {
		return errors.New("update: the run folder must be an absolute path")
	}
	f, err := os.Open(dir)
	if err != nil {
		return fmt.Errorf("update: run folder: %w", err)
	}
	defer f.Close()
	if info, err := f.Stat(); err != nil || !info.IsDir() {
		return fmt.Errorf("update: run folder %s is not a folder", dir)
	}
	names, err := f.Readdirnames(1)
	if len(names) > 0 {
		return fmt.Errorf("update: run folder %s is not empty", dir)
	}
	if err != nil && !errors.Is(err, io.EOF) {
		return fmt.Errorf("update: run folder: %w", err)
	}
	return nil
}

// resolveEffort maps an effort to a value from the host's table, where
// "highest", and no effort at all, is the last entry.
func resolveEffort(levels []string, effort string) (string, error) {
	if effort == "" || effort == config.EffortHighest {
		return levels[len(levels)-1], nil
	}
	if slices.Contains(levels, effort) {
		return effort, nil
	}
	return "", fmt.Errorf("update: effort %q is not one of %s or %s", effort, strings.Join(levels, ", "), config.EffortHighest)
}

// effortTimeout scales from 180 s at the table's lowest effort to 600 s at
// its highest.
func effortTimeout(levels []string, effort string) time.Duration {
	i := slices.Index(levels, effort)
	if i < 0 || len(levels) < 2 {
		return maxTimeout
	}
	return minTimeout + (maxTimeout-minTimeout)*time.Duration(i)/time.Duration(len(levels)-1)
}
