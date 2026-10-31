package update

import (
	"bytes"
	"context"
	"encoding/json"
)

// claudeEfforts are the values claude --effort takes, lowest first; "highest"
// is the last. eva-md doctor --live shows the effort the installed claude
// accepts from them. claude 2.1.312 also takes ultracode, left out
// on purpose: it is a session mode built on the Workflow tool, which update
// runs turn off with --tools "", so "highest" stays max.
var claudeEfforts = []string{"low", "medium", "high", "xhigh", "max"}

func runClaude(ctx context.Context, r Request, t try) (attempt, error) {
	out := &capped{limit: maxStdout}
	c, err := spawn(ctx, cliFor("claude", r.CLI), claudeArgs(r, t), r.RunDir, r.Input, out, t.timeout)
	if err != nil {
		return attempt{}, err
	}
	return claudeResult(c, out, t.effort), nil
}

// claudeArgs is the command from design.md "Model selection". The batch is
// stdin, never an argument. --tools "" turns off only the built-in tools, and
// an MCP server's tools stay callable, so --strict-mcp-config, with no
// --mcp-config, loads no MCP server at all.
func claudeArgs(r Request, t try) []string {
	args := []string{"-p", "--safe-mode", "--tools", ""}
	if !t.noStrictMCP {
		args = append(args, "--strict-mcp-config")
	}
	args = append(args, "--no-session-persistence", "--permission-prompts", "none")
	if t.model != "" {
		args = append(args, "--model", t.model)
	}
	if t.effort != "" {
		args = append(args, "--effort", t.effort)
	}
	return append(args, "--system-prompt", r.Rules, "--output-format", "json", "--json-schema", string(r.Schema))
}

// envelope is the JSON object claude -p --output-format json prints.
type envelope struct {
	Type             string          `json:"type"`
	Subtype          string          `json:"subtype"`
	IsError          bool            `json:"is_error"`
	Result           json.RawMessage `json:"result"`
	Errors           json.RawMessage `json:"errors"`
	StructuredOutput json.RawMessage `json:"structured_output"`
	Usage            json.RawMessage `json:"usage"`
	ModelUsage       json.RawMessage `json:"modelUsage"`
	// The tool calls --permission-prompts none refused, each a tool_name,
	// tool_use_id and tool_input; only whether there is one is read.
	PermissionDenials json.RawMessage `json:"permission_denials"`
	// The HTTP status and kind of the API error that ended the turn, when
	// claude reports them. Neither is documented, so without them the error
	// text names the failure.
	APIErrorStatus json.RawMessage `json:"api_error_status"`
	APIError       json.RawMessage `json:"api_error"`
}

// claudeResult reads the envelope. The answer is structured_output; without
// it the run is invalid_output, even when the subtype says success. A refused
// tool call makes the run tool_used whatever else it did: the run should
// have had no tool to call.
func claudeResult(c child, out *capped, effort string) attempt {
	switch {
	case c.missing:
		return attempt{reason: ReasonCLIMissing}
	case c.timedOut:
		return attempt{reason: ReasonTimeout}
	}
	d := diagnostics{effort: effort}
	d.note(string(c.stderr))
	var e envelope
	if out.overflow || json.Unmarshal(bytes.TrimSpace(out.Bytes()), &e) != nil || e.Type != "result" {
		// No envelope. A CLI that failed may have printed its error as plain
		// text; one that exited cleanly printed something else, perhaps the
		// model's own words, which must not name a failure.
		if !c.failed {
			return attempt{reason: ReasonInvalidOutput}
		}
		if !out.overflow {
			d.note(out.String())
		}
		return attempt{reason: d.reason(), effortRefused: d.refusesEffort(), strictMCPRefused: d.refusesStrictMCP()}
	}
	a := attempt{}
	a.tokensIn, a.tokensOut = claudeTokens(e)
	switch {
	case deniedTools(e.PermissionDenials):
		a.reason = ReasonToolUsed
	case e.Subtype == "error_max_structured_output_retries":
		a.reason = ReasonInvalidOutput
	case e.IsError || e.Subtype != "success" || c.failed:
		// result is error text only when is_error is set; otherwise it may be
		// the model's own words, which must not name the failure.
		if e.IsError {
			d.note(text(e.Result))
		}
		d.note(string(e.Errors))
		d.note(text(e.APIError))
		if a.reason = claudeStatus(e.APIErrorStatus); a.reason == "" {
			a.reason, a.effortRefused = d.reason(), d.refusesEffort()
		}
		a.strictMCPRefused = d.refusesStrictMCP()
	default:
		if output, ok := object(e.StructuredOutput); ok {
			a.output = output
		} else {
			a.reason = ReasonInvalidOutput
		}
	}
	return a
}

// claudeStatus names a failure from the API error's HTTP status, when the
// envelope has one. 529 is claude's overloaded status, a limit; any other 5xx
// is a server error.
func claudeStatus(raw json.RawMessage) string {
	var v any
	_ = json.Unmarshal(raw, &v)
	switch s := count(v); {
	case s == 401:
		return ReasonAuth
	case s == 404:
		return ReasonModelRejected
	case s == 429 || s == 529:
		return ReasonRateLimit
	case s >= 500 && s <= 599:
		return ReasonHostError
	}
	return ""
}

// deniedTools reports whether the envelope lists a refused tool call.
// Anything there but an empty list counts, so a changed shape can't hide a
// denial.
func deniedTools(raw json.RawMessage) bool {
	var denials []json.RawMessage
	if json.Unmarshal(raw, &denials) == nil {
		return len(denials) > 0
	}
	return len(bytes.TrimSpace(raw)) > 0
}

// claudeTokens reads the token counts. modelUsage covers every model call
// of the run, so it is preferred to usage, which covers the main loop only.
// The input count adds the cache writes and reads, which both report apart.
func claudeTokens(e envelope) (in, out int) {
	var models map[string]map[string]any
	if json.Unmarshal(e.ModelUsage, &models) == nil && len(models) > 0 {
		for _, u := range models {
			in += count(u["inputTokens"]) + count(u["cacheCreationInputTokens"]) + count(u["cacheReadInputTokens"])
			out += count(u["outputTokens"])
		}
		return in, out
	}
	var u map[string]any
	if json.Unmarshal(e.Usage, &u) != nil {
		return 0, 0
	}
	in = count(u["input_tokens"]) + count(u["cache_creation_input_tokens"]) + count(u["cache_read_input_tokens"])
	return in, count(u["output_tokens"])
}
