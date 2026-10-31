package update

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"testing"
	"time"
)

// codexRejected is a stream for a model the account can't use.
const codexRejected = `{"type":"thread.started","thread_id":"0199a000-0000-7000-8000-000000000001"}
{"type":"turn.started"}
{"type":"error","message":"unexpected status 400 Bad Request: {\"detail\":\"The 'gpt-test-model' model is not supported when using Codex with a ChatGPT account.\"}"}
{"type":"turn.failed","error":{"message":"unexpected status 400 Bad Request: The 'gpt-test-model' model is not supported when using Codex with a ChatGPT account."}}
`

// codexEffortRejected is a stream for an effort the model can't take. Codex
// quotes the refused value and doesn't say "effort".
func codexEffortRejected(effort string) string {
	refusal := `Unsupported value: '` + effort + `' is not supported with the 'gpt-test-model' model.`
	return `{"type":"thread.started","thread_id":"0199a000-0000-7000-8000-000000000003"}
{"type":"turn.started"}
{"type":"error","message":"unexpected status 400 Bad Request: {\"detail\":\"` + refusal + `\"}"}
{"type":"turn.failed","error":{"message":"unexpected status 400 Bad Request: ` + refusal + `"}}
`
}

func TestCodexArgumentsMatchTheDesign(t *testing.T) {
	isolate(t)
	dump := fakeHost(t, HostCodex)
	r := request(t, HostCodex)
	// A repeated name, or one of the fixed five, is passed once.
	r.CodexDisable = []string{"multi_agent", "view_image", "hooks", "multi_agent", "code_mode_host"}
	r.IgnoreUserConfig = true
	res, err := NewRunner(HostCodex).Run(context.Background(), r)
	if err != nil || res.Reason != "" {
		t.Fatalf("run failed: %+v %v", res, err)
	}
	got := argsOf(t, dump, 1)
	i := slices.Index(got, "--output-schema")
	if i < 0 || i+3 >= len(got) {
		t.Fatalf("no schema or output file in %q", got)
	}
	schema, output := got[i+1], got[i+3]
	want := []string{"--no-daemon", "exec", "--ephemeral", "--json", "-s", "read-only", "--skip-git-repo-check", "-C", r.RunDir,
		"--disable", "hooks", "--disable", "plugins", "--disable", "memories", "--disable", "shell_tool", "--disable", "apps",
		"--disable", "multi_agent", "--disable", "view_image", "--disable", "code_mode_host",
		"-c", "web_search=disabled", "-c", "tools.update_plan.enabled=false", "-m", "gpt-test-model", "-c", "model_reasoning_effort=ultra",
		"--ignore-user-config", "--output-schema", schema, "-o", output, "-"}
	if !slices.Equal(got, want) {
		t.Fatalf("arguments\n got %q\nwant %q", got, want)
	}
	if read(t, dump, "schema.1") != string(r.Schema) {
		t.Fatal("the schema file does not hold the request's schema")
	}
	// The schema and answer sit in a private folder outside the run folder,
	// removed after the run; the run folder stays empty.
	dir := filepath.Dir(schema)
	if filepath.Dir(output) != dir || strings.HasPrefix(dir, r.RunDir) {
		t.Fatalf("schema %s and output %s", schema, output)
	}
	if _, err := os.Stat(dir); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("private folder left behind: %v", err)
	}
	if entries, err := os.ReadDir(r.RunDir); err != nil || len(entries) != 0 {
		t.Fatalf("run folder holds %d entries: %v", len(entries), err)
	}
	real, err := filepath.EvalSymlinks(r.RunDir)
	if err != nil {
		t.Fatal(err)
	}
	if got := strings.TrimSpace(read(t, dump, "pwd.1")); got != real {
		t.Fatalf("working folder %s, want %s", got, real)
	}
	r.IgnoreUserConfig = false
	r.CodexDisable = nil
	if _, err := NewRunner(HostCodex).Run(context.Background(), r); err != nil {
		t.Fatal(err)
	}
	if got := argsOf(t, dump, 2); slices.Contains(got, "--ignore-user-config") || slices.Contains(got, "multi_agent") {
		t.Fatalf("second run arguments %q", got)
	}
}

func TestCodexStreams(t *testing.T) {
	const (
		threadStarted = `{"type":"thread.started","thread_id":"0199a000-0000-7000-8000-000000000002"}` + "\n"
		turnStarted   = `{"type":"turn.started"}` + "\n"
		message       = `{"type":"item.completed","item":{"id":"item_1","type":"agent_message","text":"{\"summary\":\"done\"}"}}` + "\n"
		completed     = `{"type":"turn.completed","usage":{"input_tokens":500,"cached_input_tokens":100,"output_tokens":60}}` + "\n"
	)
	cases := []struct {
		name, stream, stderr, output string
		exit                         int
		reason, degraded             string
		tokensIn, tokensOut          int
	}{
		{name: "clean", stream: codexClean, output: answer, tokensIn: 500, tokensOut: 60},
		{name: "no usage", stream: threadStarted + turnStarted + message, output: answer},
		{name: "command execution", stream: threadStarted + turnStarted +
			`{"type":"item.started","item":{"id":"item_0","type":"command_execution","command":"cat notes","status":"in_progress"}}` + "\n" + message + completed,
			output: answer, reason: ReasonToolUsed},
		{name: "file change", stream: `{"type":"item.completed","item":{"id":"item_0","type":"file_change","changes":[]}}` + "\n", output: answer, reason: ReasonToolUsed},
		{name: "MCP tool call", stream: `{"type":"item.started","item":{"id":"item_0","type":"mcp_tool_call","server":"docs","tool":"search"}}` + "\n", output: answer, reason: ReasonToolUsed},
		{name: "web search", stream: `{"type":"item.started","item":{"id":"item_0","type":"web_search","query":"release"}}` + "\n", output: answer, reason: ReasonToolUsed},
		// Every run turns the plan tool off, so its item means the switch
		// didn't hold.
		{name: "plan item", stream: threadStarted + turnStarted +
			`{"type":"item.started","item":{"id":"item_0","type":"todo_list","items":[{"text":"Read the batch","completed":false}]}}` + "\n" + message + completed,
			output: answer, reason: ReasonToolUsed},
		{name: "unknown item type", stream: threadStarted + turnStarted +
			`{"type":"item.started","item":{"id":"item_0","type":"browser_session"}}` + "\n" + message + completed,
			output: answer, reason: ReasonToolUsed},
		{name: "item without a type", stream: `{"type":"item.completed","item":{"id":"item_0"}}` + "\n", output: answer, reason: ReasonToolUsed},
		// Tools arrive as items: an unknown event is output this reader can't
		// vouch for, not a tool, and stops the run before its usage is read.
		{name: "unknown event type", stream: threadStarted + `{"type":"turn.plan_updated","plan":[]}` + "\n" + message + completed,
			output: answer, reason: ReasonInvalidOutput},
		{name: "tool item before an unknown event", stream: threadStarted +
			`{"type":"item.started","item":{"id":"item_0","type":"command_execution","command":"cat notes"}}` + "\n" +
			`{"type":"thread.compacted"}` + "\n" + message + completed,
			output: answer, reason: ReasonToolUsed},
		{name: "error item beside valid output", stream: threadStarted + turnStarted +
			`{"type":"item.completed","item":{"id":"item_0","type":"error","message":"Reconnecting... 1/5"}}` + "\n" + message + completed,
			output: answer, tokensIn: 500, tokensOut: 60},
		{name: "optional MCP failure beside valid output", stream: threadStarted +
			`{"type":"item.completed","item":{"id":"item_0","type":"error","message":"MCP client for ` + "`docs`" + ` failed to start: handshaking with MCP server failed"}}` + "\n" +
			turnStarted + message + completed,
			output: answer, degraded: "an optional MCP server failed", tokensIn: 500, tokensOut: 60},
		{name: "optional MCP timeout on stderr", stream: codexClean, stderr: "MCP client for `docs` timed out after 10 seconds.\n",
			output: answer, degraded: "an optional MCP server failed", tokensIn: 500, tokensOut: 60},
		{name: "required MCP failure", stderr: "Error: required MCP servers failed to initialize: probe\n", exit: 1, reason: ReasonMCPFailed},
		{name: "authentication", stream: threadStarted + turnStarted +
			`{"type":"turn.failed","error":{"message":"unexpected status 401 Unauthorized: Your access token could not be refreshed. Please log out and sign in again."}}` + "\n",
			exit: 1, reason: ReasonAuth},
		{name: "rate limit", stream: threadStarted + turnStarted + `{"type":"error","message":"You've hit your usage limit. Try again later."}` + "\n" +
			`{"type":"turn.failed","error":{"message":"usage limit"}}` + "\n",
			exit: 1, reason: ReasonRateLimit},
		{name: "turn failed despite an answer", stream: threadStarted + turnStarted + `{"type":"turn.failed","error":{"message":"stream disconnected before completion"}}` + "\n",
			output: answer, reason: ReasonHostError},
		// An outage or a command line a host upgrade no longer takes is a
		// host error, not the batch's invalid output.
		{name: "server error", stream: threadStarted + turnStarted +
			`{"type":"turn.failed","error":{"message":"exceeded retry limit, last status: 503 Service Unavailable"}}` + "\n",
			exit: 1, reason: ReasonHostError},
		{name: "usage error after an upgrade", stderr: "error: invalid value 'browser_use' for '--disable <DISABLE>'\n\nFor more information, try '--help'.\n",
			exit: 1, reason: ReasonHostError},
		// The model's own words never name a failure.
		{name: "model text naming failures", stream: threadStarted + turnStarted +
			`{"type":"item.completed","item":{"id":"item_0","type":"reasoning","text":"Not logged in. Run claude auth login"}}` + "\n" +
			`{"type":"item.completed","item":{"id":"item_1","type":"agent_message","text":"we hit the rate limit yesterday"}}` + "\n",
			exit: 1, reason: ReasonInvalidOutput},
		{name: "turn failed beside model text naming a limit", stream: threadStarted + turnStarted +
			`{"type":"item.completed","item":{"id":"item_0","type":"reasoning","text":"usage limit reached"}}` + "\n" +
			`{"type":"turn.failed","error":{"message":"stream disconnected before completion"}}` + "\n",
			exit: 1, reason: ReasonHostError},
		// A notice that is not JSON reports no tool: it is skipped and counted.
		{name: "not a JSON line", stream: "Update available: run brew upgrade codex\n" + codexClean, output: answer,
			degraded: "codex printed lines that are not JSON events: 1", tokensIn: 500, tokensOut: 60},
		// A line with a brace that isn't JSON may hide an event.
		{name: "event run together with other text", stream: threadStarted + `notice {"type":"item.started","item":{"id":"item_0","type":"command_execution"}}` + "\n" + message + completed,
			output: answer, reason: ReasonInvalidOutput},
		{name: "answer missing", stream: codexClean, reason: ReasonInvalidOutput, tokensIn: 500, tokensOut: 60},
		{name: "answer not an object", stream: codexClean, output: `["done"]`, reason: ReasonInvalidOutput, tokensIn: 500, tokensOut: 60},
		{name: "failed exit", stream: codexClean, output: answer, exit: 1, reason: ReasonInvalidOutput, tokensIn: 500, tokensOut: 60},
		{name: "last line without a newline", stream: strings.TrimSuffix(threadStarted+turnStarted+message+completed, "\n"), output: answer, tokensIn: 500, tokensOut: 60},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			isolate(t)
			dump := fakeCLI(t, "")
			put(t, dump, "stdout", tc.stream)
			put(t, dump, "stderr", tc.stderr)
			put(t, dump, "exit", strconv.Itoa(tc.exit))
			if tc.output != "" {
				put(t, dump, "output", tc.output)
			}
			r := request(t, HostCodex)
			r.Timeout = 30 * time.Second
			res, err := NewRunner(HostCodex).Run(context.Background(), r)
			if err != nil {
				t.Fatal(err)
			}
			if res.Reason != tc.reason || res.Degraded != tc.degraded {
				t.Fatalf("result %+v, want reason %q and degraded %q", res, tc.reason, tc.degraded)
			}
			if res.TokensIn != tc.tokensIn || res.TokensOut != tc.tokensOut {
				t.Fatalf("tokens %d/%d, want %d/%d", res.TokensIn, res.TokensOut, tc.tokensIn, tc.tokensOut)
			}
			if tc.reason == "" && string(res.Output) != answer {
				t.Fatalf("output %s", res.Output)
			}
			if tc.reason != "" && res.Output != nil {
				t.Fatalf("a failed run kept output %s", res.Output)
			}
			if n := runs(t, dump); n != 1 || res.Attempts != 1 {
				t.Fatalf("ran %d times, %d attempts reported", n, res.Attempts)
			}
		})
	}
}

func TestCodexToolEventStopsTheRun(t *testing.T) {
	isolate(t)
	// The fake starts a child, reports a tool, then would run for 30 s.
	dump := fakeCLI(t, `sleep 30 & echo $! > "$dump/child"; cat "$dump/event"; sleep 30`)
	put(t, dump, "event", `{"type":"item.started","item":{"id":"item_0","type":"command_execution","command":"cat notes","status":"in_progress"}}`+"\n")
	put(t, dump, "output", answer)
	r := request(t, HostCodex)
	r.Timeout = time.Minute
	start := time.Now()
	res, err := NewRunner(HostCodex).Run(context.Background(), r)
	if err != nil || res.Reason != ReasonToolUsed || res.Output != nil {
		t.Fatalf("result %+v %v", res, err)
	}
	if elapsed := time.Since(start); elapsed > 15*time.Second {
		t.Fatalf("the run went on for %v after the tool event", elapsed)
	}
	waitGone(t, dump, "child")
}

func TestCodexModelRejectionFallsBack(t *testing.T) {
	isolate(t)
	dump := fakeCLI(t, "")
	// The first attempt leaves an answer behind; a later attempt must not
	// pick it up.
	put(t, dump, "stdout.1", codexRejected)
	put(t, dump, "output.1", answer)
	put(t, dump, "exit.1", "1")
	put(t, dump, "stdout.2", codexRejected)
	put(t, dump, "exit.2", "1")
	put(t, dump, "stdout.3", codexClean)
	put(t, dump, "output.3", answer)
	res, err := NewRunner(HostCodex).Run(context.Background(), request(t, HostCodex))
	if err != nil {
		t.Fatal(err)
	}
	if n := runs(t, dump); n != 3 {
		t.Fatalf("ran %d times, want 3", n)
	}
	first, second, third := argsOf(t, dump, 1), argsOf(t, dump, 2), argsOf(t, dump, 3)
	for i, args := range [][]string{first, second, third} {
		if len(args) < 2 || args[0] != "--no-daemon" || args[1] != "exec" {
			t.Fatalf("attempt %d does not start with --no-daemon exec: %q", i+1, args)
		}
	}
	effort := "model_reasoning_effort=ultra"
	if !hasPair(first, "-m", "gpt-test-model") || !slices.Contains(first, effort) {
		t.Fatalf("first attempt %q", first)
	}
	if !hasPair(second, "-m", "gpt-test-model") || slices.Contains(second, effort) {
		t.Fatalf("second attempt must drop only the effort: %q", second)
	}
	if slices.Contains(third, "-m") || slices.Contains(third, effort) {
		t.Fatalf("third attempt must drop the model too: %q", third)
	}
	if res.Reason != "" || string(res.Output) != answer || res.Model != "" || res.ModelNote != "fallback from gpt-test-model" || res.Attempts != 3 {
		t.Fatalf("result %+v", res)
	}
	if res.Degraded != "effort ultra was dropped with the model; the run used the default effort" {
		t.Fatalf("degraded %q", res.Degraded)
	}
}

func TestCodexStaleAnswerIsNotReused(t *testing.T) {
	isolate(t)
	dump := fakeCLI(t, "")
	put(t, dump, "stdout.1", codexRejected)
	put(t, dump, "output.1", answer)
	put(t, dump, "exit.1", "1")
	put(t, dump, "stdout.2", codexClean)
	res, err := NewRunner(HostCodex).Run(context.Background(), request(t, HostCodex))
	if err != nil {
		t.Fatal(err)
	}
	if res.Reason != ReasonInvalidOutput || res.Output != nil {
		t.Fatalf("result %+v, want invalid_output", res)
	}
	if n := runs(t, dump); n != 2 {
		t.Fatalf("ran %d times, want 2", n)
	}
}
