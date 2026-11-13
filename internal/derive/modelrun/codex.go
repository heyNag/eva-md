package modelrun

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"math"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"syscall"

	"github.com/heyNag/eva-md/internal/host/adapter"
)

// The Codex backend: v1's run (internal/update/codex.go), read against fixed
// lists. The --json stream is read as it arrives and the run stopped at the
// first line that decides: a tool item is tool_used, and an item or event
// this reader doesn't know is host, never the chat's fault. codex features
// list is read once per entry of the probe cache, not before every run, and
// the tokens a turn reported are kept whatever ends the run.

func init() {
	register(adapter.Codex, backend{
		efforts: codexEfforts,
		signals: []Signal{SignalCodexLineSkipped, SignalCodexMCPFailed},
		probe:   codexProbe,
		attempt: codexAttempt,
	})
}

// The Codex backend's signals.
const (
	// SignalCodexLineSkipped says the stream held a line that is not JSON
	// and holds no brace, such as a notice, which was skipped.
	SignalCodexLineSkipped Signal = "codex_line_skipped"
	// SignalCodexMCPFailed says an optional MCP server failed beside an
	// answer that was kept.
	SignalCodexMCPFailed Signal = "codex_mcp_optional_failed"
)

// codexEfforts are the model_reasoning_effort values, lowest first (v1's
// codexEfforts).
var codexEfforts = []string{"none", "minimal", "low", "medium", "high", "xhigh", "max", "ultra"}

// codexOff is disabled on every run, even one whose features list could not
// be read (v1's codexOff).
var codexOff = []string{"hooks", "plugins", "memories", "shell_tool", "apps"}

// codexTools are the other features that give the model a tool, from codex's
// feature registry and tool plan at 0.166.0 (v1's codexTools, where each is
// explained). unified_exec is left out: with shell_tool off codex offers no
// exec tool at all.
var codexTools = []string{
	"view_image", "sleep_tool", "request_permissions_tool", "standalone_web_search", "multi_env_copy_file",
	"multi_agent", "multi_agent_v2", "agent_message_board", "goals", "image_generation", "artifact",
	"tool_suggest", "enable_mcp_apps", "code_mode", "code_mode_only", "code_mode_host", "deferred_executor",
	"default_mode_request_user_input", "send_message_to_user_async", "token_budget", "current_time_reminder",
	"browser_use", "browser_use_external", "browser_use_full_cdp_access", "computer_use",
}

// The stream's fixed lists.
var (
	// codexEvents are the event types the reader knows, beside item.*.
	codexEvents = []string{"thread.started", "turn.started", "turn.completed", "turn.failed", "error"}
	// codexItemsAllowed are the item types that touch nothing: the answer's
	// text, the model's reasoning and a diagnostic.
	codexItemsAllowed = []string{"agent_message", "reasoning", "error"}
	// codexToolItems are the item types of a tool: a command, a file
	// change, an MCP tool, a web search and the plan tool, which every run
	// turns off, so its item means that switch didn't hold.
	codexToolItems = []string{"command_execution", "file_change", "mcp_tool_call", "web_search", "todo_list"}
)

// Size caps (v1's maxLine, maxOutput and maxDiag).
const (
	codexMaxLine   = 4 << 20  // one event
	codexMaxOutput = 4 << 20  // the -o file
	codexMaxDiag   = 64 << 10 // error text kept to name a failure
)

// codexProbe is the probe step: the features of codexOff and codexTools that
// codex features list names, on or off, read as v1's codexDisable reads it,
// since --ignore-user-config can turn one back on and codex refuses a name it
// doesn't know. A list that can't be read, or names no feature, gives
// codexOff, which is not kept.
func codexProbe(ctx context.Context, run probeRun) ([]string, bool) {
	out, err := run(ctx, "features", "list")
	if err != nil {
		return slices.Clone(codexOff), false
	}
	listed := map[string]bool{}
	for _, line := range strings.Split(string(out), "\n") {
		if f := strings.Fields(line); len(f) >= 3 && (f[len(f)-1] == "true" || f[len(f)-1] == "false") {
			listed[f[0]] = true
		}
	}
	if len(listed) == 0 {
		return slices.Clone(codexOff), false
	}
	var names []string
	for _, name := range append(slices.Clone(codexOff), codexTools...) {
		if listed[name] {
			names = append(names, name)
		}
	}
	return names, true
}

// codexAttempt is one run of codex exec. The schema and the answer live in a
// private folder made for this run, so the run folder stays empty and no
// earlier run can leave an answer.
func codexAttempt(ctx context.Context, a *attempt) (result, error) {
	dir, err := os.MkdirTemp("", "eva-md-codex-")
	if err != nil {
		return result{}, errors.New("modelrun: no private folder for the run")
	}
	defer os.RemoveAll(dir)
	schema, output := filepath.Join(dir, "schema.json"), filepath.Join(dir, "output.json")
	if err := os.WriteFile(schema, a.call.Schema, 0o600); err != nil {
		return result{}, errors.New("modelrun: the schema could not be written")
	}
	runCtx, stop := context.WithCancel(ctx)
	defer stop()
	s := &codexStream{stop: stop}
	e, err := a.run(runCtx, codexArgs(a.call, a.caps.Features, schema, output), codexStdin(a.call), s)
	if err != nil {
		return result{}, err
	}
	s.flush()
	return s.result(e, output, a.call.Effort), nil
}

// codexArgs is v1's command, disabling each feature of disable; the input is
// stdin, never an argument. --no-daemon keeps the run off codex's shared
// background server, where it would escape the cleared environment and the
// process group; --ephemeral leaves no transcript.
func codexArgs(c Call, disable []string, schema, output string) []string {
	args := []string{"--no-daemon", "exec", "--ephemeral", "--json", "-s", "read-only", "--skip-git-repo-check", "-C", c.RunDir}
	for _, name := range disable {
		args = append(args, "--disable", name)
	}
	args = append(args, "-c", "web_search=disabled", "-c", "tools.update_plan.enabled=false")
	if c.Model != "" {
		args = append(args, "-m", c.Model)
	}
	if c.Effort != "" {
		args = append(args, "-c", "model_reasoning_effort="+c.Effort)
	}
	if c.IgnoreUserConfig {
		args = append(args, "--ignore-user-config")
	}
	return append(args, "--output-schema", schema, "-o", output, "-")
}

// codexStdin puts the instructions ahead of the input: codex exec has no
// flag for fixed instructions.
func codexStdin(c Call) []byte {
	return append([]byte(strings.TrimRight(c.System, "\n")+"\n\n"), c.Input...)
}

// codexStream reads codex's --json events as they arrive and stops the run
// at the first line that decides: a tool, or a line it can't vouch for. It
// keeps no message text, only error text, to name a failure with.
type codexStream struct {
	stop       func()
	line       []byte // the unfinished line
	toolUsed   bool
	invalid    bool // a line this reader can't vouch for: the run is host
	turnFailed bool
	errorEvent bool // an error event, apart from an error item
	skipped    bool // a line that is not JSON, such as a notice
	diag       codexDiag
	usage      Usage
	usageSeen  bool
}

// Write takes what the CLI printed, in any pieces, and reads each whole
// line. Once a line has decided, the rest is dropped.
func (s *codexStream) Write(p []byte) (int, error) {
	n := len(p)
	for len(p) > 0 && !s.decided() {
		i := bytes.IndexByte(p, '\n')
		part := p
		if i >= 0 {
			part = p[:i]
		}
		if len(s.line)+len(part) > codexMaxLine {
			s.decide(&s.invalid)
			break
		}
		s.line = append(s.line, part...)
		if i < 0 {
			break
		}
		s.event(s.line)
		s.line = s.line[:0]
		p = p[i+1:]
	}
	return n, nil
}

func (s *codexStream) decided() bool { return s.toolUsed || s.invalid }

// flush reads a last line that ended without a newline.
func (s *codexStream) flush() {
	if len(s.line) > 0 && !s.decided() {
		s.event(s.line)
	}
	s.line = nil
}

// decide marks the run and stops the child, since nothing it prints after
// this can change the outcome.
func (s *codexStream) decide(flag *bool) {
	*flag = true
	s.stop()
}

// event reads one line. A tool item anywhere in it, at any depth, is a tool.
// A line that is not JSON and holds no brace, such as a notice, is skipped;
// one that holds a brace may be an event run together with other text, and
// an object with a key twice may be read two ways, so the run can't be shown
// to be tool-free: host.
func (s *codexStream) event(line []byte) {
	line = bytes.TrimSpace(line)
	if len(line) == 0 {
		return
	}
	tool, whole := codexScan(line)
	if tool {
		s.decide(&s.toolUsed)
		return
	}
	var e map[string]json.RawMessage
	if json.Unmarshal(line, &e) != nil {
		if bytes.IndexByte(line, '{') >= 0 {
			s.decide(&s.invalid)
		} else {
			s.skipped = true
		}
		return
	}
	if !whole {
		s.decide(&s.invalid)
		return
	}
	kind := codexText(e["type"])
	switch {
	case !strings.HasPrefix(kind, "item.") && !slices.Contains(codexEvents, kind):
		// An event this reader doesn't know, such as one a later codex
		// adds, is output it can't vouch for.
		s.decide(&s.invalid)
		return
	case strings.HasPrefix(kind, "item."):
		if !s.item(e["item"]) {
			return
		}
	case e["item"] != nil:
		// An item where no item belongs is read as one all the same.
		if !s.item(e["item"]) {
			return
		}
	}
	switch kind {
	case "turn.completed":
		s.addUsage(e["usage"])
	case "turn.failed":
		s.turnFailed = true
		s.diag.note(codexMessage(e["error"]))
	case "error":
		s.errorEvent = true
		s.diag.note(codexMessage(e["message"]))
	}
}

// item reads an item: agent messages, reasoning and error items pass, and an
// error item is a diagnostic; a tool item is a tool; any other type, or
// none, is host. It reports whether the stream goes on.
func (s *codexStream) item(raw json.RawMessage) bool {
	var item map[string]json.RawMessage
	_ = json.Unmarshal(raw, &item)
	kind := codexText(item["type"])
	switch {
	case slices.Contains(codexToolItems, kind):
		s.decide(&s.toolUsed)
		return false
	case !slices.Contains(codexItemsAllowed, kind):
		s.decide(&s.invalid)
		return false
	case kind == "error":
		s.diag.note(codexMessage(item["message"]))
	}
	return true
}

// addUsage adds a turn's tokens. Codex's input_tokens hold the cached ones,
// so the uncached input is input_tokens less cached_input_tokens, never
// below 0; its output_tokens hold the reasoning ones.
func (s *codexStream) addUsage(raw json.RawMessage) {
	var u map[string]any
	if json.Unmarshal(raw, &u) != nil || u == nil {
		return
	}
	in, cached := codexCount(u["input_tokens"]), codexCount(u["cached_input_tokens"])
	s.usage.In += max(in-cached, 0)
	s.usage.CachedIn += cached
	s.usage.Out += codexCount(u["output_tokens"])
	s.usageSeen = true
}

// result names the run's class from what the stream showed and how the CLI
// ended. The core puts a missing CLI, then a tool, then a timeout first.
func (s *codexStream) result(e exit, output, effort string) result {
	res := result{class: ClassHost, toolSign: s.toolUsed, usage: s.usage, usageSeen: s.usageSeen}
	if s.skipped {
		res.signals = append(res.signals, SignalCodexLineSkipped)
	}
	switch {
	case e.missing || e.timedOut || s.invalid:
	case s.toolUsed:
		res.class = ClassToolUsed
	default:
		s.diag.note(string(e.stderr))
		answer, ok := codexReadOutput(output)
		clean := !e.failed && !s.turnFailed
		switch {
		case clean && ok:
			res.class, res.answer = ClassOK, answer
			if s.diag.mcpFailed {
				res.signals = append(res.signals, SignalCodexMCPFailed)
			}
		case clean && !s.errorEvent:
			res.class = ClassAnswerInvalid
		default:
			res.class, res.refused = s.diag.class(effort)
		}
	}
	return res
}

// codexReadOutput reads the answer codex wrote with -o: a regular file, not
// a link, of at most codexMaxOutput bytes, holding one JSON object (v1's
// readOutput).
func codexReadOutput(file string) ([]byte, bool) {
	f, err := os.OpenFile(file, os.O_RDONLY|syscall.O_NOFOLLOW|syscall.O_NONBLOCK, 0)
	if err != nil {
		return nil, false
	}
	defer f.Close()
	if info, err := f.Stat(); err != nil || !info.Mode().IsRegular() {
		return nil, false
	}
	data, err := io.ReadAll(io.LimitReader(f, codexMaxOutput+1))
	if err != nil || len(data) > codexMaxOutput {
		return nil, false
	}
	return object(data)
}

// codexScan walks one line's JSON and reports whether any object in it, at
// any depth, has a type key, in any letter case, naming a tool item, and
// whether the line is one JSON value whose objects hold no key twice. A tool
// found before the line goes wrong still counts.
func codexScan(line []byte) (tool, whole bool) {
	dec := json.NewDecoder(bytes.NewReader(line))
	dec.UseNumber()
	var value func(tok json.Token) bool
	value = func(tok json.Token) bool {
		delim, ok := tok.(json.Delim)
		if !ok {
			return true
		}
		seen := map[string]bool{}
		for dec.More() {
			if delim == '{' {
				k, err := dec.Token()
				key, isKey := k.(string)
				if err != nil || !isKey || seen[key] {
					return false
				}
				seen[key] = true
				v, err := dec.Token()
				if err != nil {
					return false
				}
				if name, isText := v.(string); isText && strings.EqualFold(key, "type") &&
					slices.ContainsFunc(codexToolItems, func(t string) bool { return strings.EqualFold(t, name) }) {
					tool = true
				}
				if !value(v) {
					return false
				}
				continue
			}
			v, err := dec.Token()
			if err != nil || !value(v) {
				return false
			}
		}
		_, err := dec.Token()
		return err == nil
	}
	tok, err := dec.Token()
	if err != nil || !value(tok) {
		return tool, false
	}
	_, err = dec.Token()
	return tool, err == io.EOF
}

// The failure wording, lowercased, as v1 reads codex's (internal/update/
// classify.go). Neither the stream nor codex's exit names these classes in a
// documented field, so a failed run is named from its error text. A false
// auth holds the host for the day, so each term is a phrase seen in the
// binaries, not a loose word. Text that no term matches is host. The terms
// v1 keeps for Claude Code alone are left out.
var (
	// classify.go:18-19, authTerms, but "/login" and the other host's own
	// sign-in command.
	codexAuthTerms = []string{"authenticat", "unauthorized", "not logged in", "codex login",
		"log in", "sign in", "api key", "token expired", "token has expired", "token revoked", "token has been revoked"}
	// classify.go:20-22, rateTerms.
	codexRateTerms = []string{"rate limit", "rate_limit", "ratelimit", "rate-limit", "usage limit", "usage_limit",
		"hit your limit", "spend limit", "spend cap", "shared budget", "out of extra usage", "too many requests",
		"quota exceeded", "quota_exceeded", "overloaded", "at capacity", "out of credits", "credit balance"}
	// classify.go:27-43, hostTerms: codex's dropped stream and failure to
	// send (34), a server's status as codex words it (37), and clap's and
	// codex's own refusals of a command line (41); clap's invalid flag value
	// is codexInvalidFlagValue (classify.go:135-138).
	codexHostTerms = []string{
		"stream disconnected", "error sending request",
		"500 internal server error", "502 bad gateway", "503 service unavailable", "504 gateway timeout",
		"unexpected argument '", "unknown feature flag", "error parsing -c overrides",
	}
	// classify.go:47-48, refusalTerms: a refusal counts only on a line that
	// names the model or the effort.
	codexRefusalTerms = []string{"not exist", "not found", "not_found", "not supported", "unsupported", "invalid",
		"unknown", "not available", "isn't available", "access", "issue with"}
	// classify.go:51, mcpFailureTerms: codex words an MCP server that didn't
	// come up as failed to start, timed out, or not initialized.
	codexMCPFailureTerms = []string{"fail", "timed out", "not initialized", "interrupted", "incomplete", "error"}
)

// codexFiltered is codex's word for an answer it filtered or cut short
// ("Incomplete response returned, reason: content_filter"), which v1 keeps
// out of the host's failures (classify.go:109): the answer's fault, not the
// host's.
const codexFiltered = "incomplete response returned"

// codexDiag sorts a run's error text into MCP lines and the rest (v1's
// diagnostics). The rest names a failed run's class. MCP lines only tell
// whether an MCP server failed, so a server's own words, such as
// "authentication required", can't pass for codex's.
type codexDiag struct {
	text      strings.Builder // lowercased lines without "mcp", at most codexMaxDiag bytes
	mcpFailed bool
}

func (d *codexDiag) note(s string) {
	for _, line := range strings.Split(strings.ToLower(s), "\n") {
		if strings.Contains(line, "mcp") {
			d.mcpFailed = d.mcpFailed || codexContainsAny(line, codexMCPFailureTerms)
			continue
		}
		if line = strings.TrimSpace(line); line != "" && d.text.Len()+len(line) < codexMaxDiag {
			d.text.WriteString(line + "\n")
		}
	}
}

// class names a failed run, as v1's reason does (classify.go:85-101): a
// sign-in problem first, then a limit, then a network, server or command
// line failure, then a refused model or effort, which is host with Refused
// set. An outage that names the model is still an outage. A filtered answer
// is answer_invalid; anything else, a failed required MCP server included,
// is host.
func (d *codexDiag) class(effort string) (Class, string) {
	text := d.text.String()
	lines := strings.Split(text, "\n")
	switch {
	case codexContainsAny(text, codexAuthTerms):
		return ClassAuth, ""
	case codexContainsAny(text, codexRateTerms):
		return ClassRateLimit, ""
	case slices.ContainsFunc(lines, codexHostFailure):
		return ClassHost, ""
	case slices.ContainsFunc(lines, codexRefusal):
		if d.refusesEffort(effort) {
			return ClassHost, RefusedEffort
		}
		return ClassHost, RefusedModel
	case strings.Contains(text, codexFiltered):
		return ClassAnswerInvalid, ""
	}
	return ClassHost, ""
}

// codexHostFailure reports a network, server or command line failure on one
// line; a filtered answer is not one (classify.go:108-110).
func codexHostFailure(line string) bool {
	return (codexContainsAny(line, codexHostTerms) || codexInvalidFlagValue(line)) && !strings.Contains(line, codexFiltered)
}

// refusesEffort reports whether a refusal names the effort: the word, or the
// run's value in quotes, as codex quotes it ("Unsupported value: 'ultra' is
// not supported with the 'gpt-x' model.") (classify.go:122-127).
func (d *codexDiag) refusesEffort(effort string) bool {
	quoted := "'" + strings.ToLower(effort) + "'"
	return slices.ContainsFunc(strings.Split(d.text.String(), "\n"), func(line string) bool {
		return codexRefusal(line) && (strings.Contains(line, "effort") || effort != "" && strings.Contains(line, quoted))
	})
}

// codexRefusal is a refusal on a line that names the model or the effort
// (classify.go:129-131).
func codexRefusal(line string) bool {
	return (strings.Contains(line, "model") || strings.Contains(line, "effort")) && codexContainsAny(line, codexRefusalTerms)
}

// codexInvalidFlagValue is clap's "invalid value 'x' for '--flag <VALUE>'",
// whose two parts must share a line (classify.go:135-138).
func codexInvalidFlagValue(line string) bool {
	i := strings.Index(line, "invalid value '")
	return i >= 0 && strings.Contains(line[i:], "' for '--")
}

func codexContainsAny(s string, terms []string) bool {
	return slices.ContainsFunc(terms, func(t string) bool { return strings.Contains(s, t) })
}

// codexText decodes a JSON string, and gives "" for anything else.
func codexText(raw json.RawMessage) string {
	var s string
	if json.Unmarshal(raw, &s) != nil {
		return ""
	}
	return s
}

// codexMessage reads an error message given as a string or as an object
// with a message field, and otherwise keeps the raw JSON, to classify only.
func codexMessage(raw json.RawMessage) string {
	if s := codexText(raw); s != "" {
		return s
	}
	var obj map[string]json.RawMessage
	if json.Unmarshal(raw, &obj) == nil {
		if s := codexText(obj["message"]); s != "" {
			return s
		}
	}
	return string(raw)
}

// codexCount reads a token count, and gives 0 for anything that isn't a
// plausible one.
func codexCount(v any) int64 {
	f, ok := v.(float64)
	if !ok || f < 0 || f > 1e12 || f != math.Trunc(f) {
		return 0
	}
	return int64(f)
}
