package update

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"syscall"
)

// codexEfforts are the model_reasoning_effort values, lowest first; "highest"
// is the last. eva-md doctor --live shows the effort the installed codex
// accepts from them.
var codexEfforts = []string{"none", "minimal", "low", "medium", "high", "xhigh", "max", "ultra"}

// codexOff is disabled on every run, ahead of the tool features recorded at
// install.
var codexOff = []string{"hooks", "plugins", "memories", "shell_tool", "apps"}

func runCodex(ctx context.Context, r Request, t try) (attempt, error) {
	// The schema and the answer live in a private folder of their own, so the
	// run folder stays empty and an earlier attempt can't leave an answer.
	dir, err := os.MkdirTemp("", "eva-md-codex-")
	if err != nil {
		return attempt{}, errors.New("update: could not create a private folder for the codex run")
	}
	defer os.RemoveAll(dir)
	schema, output := filepath.Join(dir, "schema.json"), filepath.Join(dir, "output.json")
	if err := os.WriteFile(schema, r.Schema, 0o600); err != nil {
		return attempt{}, errors.New("update: could not write the schema file for the codex run")
	}
	runCtx, stop := context.WithCancel(ctx)
	defer stop()
	s := &codexStream{stop: stop, diag: diagnostics{effort: t.effort}}
	c, err := spawn(runCtx, cliFor("codex", r.CLI), codexArgs(r, t, schema, output), r.RunDir, codexStdin(r), s, t.timeout)
	if err != nil {
		return attempt{}, err
	}
	s.flush()
	return s.result(c, output), nil
}

// codexArgs is the command from design.md "Model selection". Each tool
// feature is disabled once; the batch is stdin, never an argument.
// --no-daemon, a top-level flag that goes before exec, keeps the run off
// codex's shared background server, where it would escape the cleared
// environment and the process group. tools.update_plan.enabled=false turns
// off the plan tool, a key codex 0.166.0 itself sets for its tool-free
// structured turns.
func codexArgs(r Request, t try, schema, output string) []string {
	args := []string{"--no-daemon", "exec", "--ephemeral", "--json", "-s", "read-only", "--skip-git-repo-check", "-C", r.RunDir}
	seen := map[string]bool{}
	for _, name := range append(append([]string{}, codexOff...), r.CodexDisable...) {
		if !seen[name] {
			seen[name] = true
			args = append(args, "--disable", name)
		}
	}
	args = append(args, "-c", "web_search=disabled", "-c", "tools.update_plan.enabled=false")
	if t.model != "" {
		args = append(args, "-m", t.model)
	}
	if t.effort != "" {
		args = append(args, "-c", "model_reasoning_effort="+t.effort)
	}
	if r.IgnoreUserConfig {
		args = append(args, "--ignore-user-config")
	}
	return append(args, "--output-schema", schema, "-o", output, "-")
}

// codexStdin puts the rules ahead of the batch: codex exec has no flag for
// fixed instructions.
func codexStdin(r Request) []byte {
	return append([]byte(strings.TrimRight(r.Rules, "\n")+"\n\n"), r.Input...)
}

// codexStream reads codex's --json events as they arrive and stops the run at
// the first event that shows a tool. It keeps no message text, only the error
// text that names a failure.
type codexStream struct {
	stop                func()
	line                []byte // the unfinished line
	toolUsed            bool
	invalid             bool // a line too long to read or that may hide an event, or an unknown event
	turnFailed          bool
	skipped             int // lines that are not JSON, such as a notice
	diag                diagnostics
	tokensIn, tokensOut int
}

func (s *codexStream) Write(p []byte) (int, error) {
	n := len(p)
	for len(p) > 0 && !s.toolUsed && !s.invalid {
		i := bytes.IndexByte(p, '\n')
		part := p
		if i >= 0 {
			part = p[:i]
		}
		if len(s.line)+len(part) > maxLine {
			s.reject(&s.invalid)
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

// flush reads a last line that ended without a newline.
func (s *codexStream) flush() {
	if len(s.line) > 0 && !s.toolUsed && !s.invalid {
		s.event(s.line)
	}
	s.line = nil
}

// reject marks the run and stops the child, since nothing it prints after
// this can change the outcome.
func (s *codexStream) reject(flag *bool) {
	*flag = true
	s.stop()
}

func (s *codexStream) event(line []byte) {
	line = bytes.TrimSpace(line)
	if len(line) == 0 {
		return
	}
	var e map[string]json.RawMessage
	if json.Unmarshal(line, &e) != nil {
		// A line that is not JSON, such as a notice, reports no tool and is
		// skipped. One holding a brace may be an event run together with
		// other text, so the run can't be shown to be tool-free.
		if bytes.IndexByte(line, '{') >= 0 {
			s.reject(&s.invalid)
		} else {
			s.skipped++
		}
		return
	}
	switch kind := text(e["type"]); {
	case strings.HasPrefix(kind, "item."):
		s.item(e["item"])
	case kind == "thread.started" || kind == "turn.started":
	case kind == "turn.completed":
		s.usage(e["usage"])
	case kind == "turn.failed":
		s.turnFailed = true
		s.diag.note(message(e["error"]))
	case kind == "error":
		s.diag.note(message(e["message"]))
	default:
		// Tools arrive as items, so an event this reader doesn't know, such as
		// one a later codex adds, is no sign of a tool. It still stops the run,
		// as output this reader can't vouch for.
		s.reject(&s.invalid)
	}
}

// item allows agent messages, reasoning and error items. An error item is a
// diagnostic, not a tool use. Any other item type, known or not, is a tool.
// That includes todo_list, the plan tool's item: it touches nothing, but
// every run turns the plan tool off, so one means that switch didn't hold.
func (s *codexStream) item(raw json.RawMessage) {
	var item map[string]json.RawMessage
	_ = json.Unmarshal(raw, &item)
	switch text(item["type"]) {
	case "agent_message", "reasoning":
	case "error":
		s.diag.note(message(item["message"]))
	default:
		s.reject(&s.toolUsed)
	}
}

// usage adds a turn's tokens. Codex's input_tokens already include the
// cached ones, and its output_tokens the reasoning ones.
func (s *codexStream) usage(raw json.RawMessage) {
	var u map[string]any
	if json.Unmarshal(raw, &u) == nil {
		s.tokensIn += count(u["input_tokens"])
		s.tokensOut += count(u["output_tokens"])
	}
}

// result names the run's outcome. The answer is the -o file. An MCP server
// that failed, or skipped lines, beside an otherwise valid answer are
// degraded notes, not a failure.
func (s *codexStream) result(c child, output string) attempt {
	a := attempt{tokensIn: s.tokensIn, tokensOut: s.tokensOut}
	switch {
	case c.missing:
		a.reason = ReasonCLIMissing
	case s.toolUsed:
		a.reason = ReasonToolUsed
	case c.timedOut:
		a.reason = ReasonTimeout
	case s.invalid:
		a.reason = ReasonInvalidOutput
	default:
		s.diag.note(string(c.stderr))
		answer, ok := readOutput(output)
		if c.failed || s.turnFailed || !ok {
			a.reason, a.effortRefused = s.diag.reason(), s.diag.refusesEffort()
			break
		}
		a.output = answer
		mcp, skipped := "", ""
		if s.diag.mcpFailed {
			mcp = "an optional MCP server failed"
		}
		if s.skipped > 0 {
			skipped = fmt.Sprintf("codex printed lines that are not JSON events: %d", s.skipped)
		}
		a.degraded = joinNotes(mcp, skipped)
	}
	return a
}

// readOutput reads the answer codex wrote with -o: a regular file, not a
// link, holding one JSON object.
func readOutput(file string) ([]byte, bool) {
	f, err := os.OpenFile(file, os.O_RDONLY|syscall.O_NOFOLLOW|syscall.O_NONBLOCK, 0)
	if err != nil {
		return nil, false
	}
	defer f.Close()
	if info, err := f.Stat(); err != nil || !info.Mode().IsRegular() {
		return nil, false
	}
	data, err := io.ReadAll(io.LimitReader(f, maxOutput+1))
	if err != nil || len(data) > maxOutput {
		return nil, false
	}
	return object(data)
}
