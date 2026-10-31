package update

import (
	"context"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"testing"
	"time"
)

// claudeRejected is the envelope claude prints for a model the account can't
// use.
const claudeRejected = `{"type":"result","subtype":"success","is_error":true,"num_turns":1,"result":"There's an issue with the selected model (claude-test-model). It may not exist or you may not have access to it. Run /model to pick a different model.","usage":{"input_tokens":0,"output_tokens":0}}`

// claudeEffortRejected is the envelope claude prints for an effort the model
// can't take.
func claudeEffortRejected(effort string) string {
	return `{"type":"result","subtype":"success","is_error":true,"api_error_status":400,"result":"API Error: 400 {\"type\":\"error\",\"error\":{\"type\":\"invalid_request_error\",\"message\":\"effort ` +
		effort + ` is not supported by this model\"}}","usage":{"input_tokens":0,"output_tokens":0}}`
}

func TestClaudeArgumentsMatchTheDesign(t *testing.T) {
	isolate(t)
	dump := fakeHost(t, HostClaude)
	r := request(t, HostClaude)
	res, err := NewRunner(HostClaude).Run(context.Background(), r)
	if err != nil || res.Reason != "" {
		t.Fatalf("run failed: %+v %v", res, err)
	}
	want := []string{"-p", "--safe-mode", "--tools", "", "--strict-mcp-config", "--no-session-persistence", "--permission-prompts", "none",
		"--model", "claude-test-model", "--effort", "max", "--system-prompt", r.Rules,
		"--output-format", "json", "--json-schema", string(r.Schema)}
	if got := argsOf(t, dump, 1); !slices.Equal(got, want) {
		t.Fatalf("arguments\n got %q\nwant %q", got, want)
	}
	real, err := filepath.EvalSymlinks(r.RunDir)
	if err != nil {
		t.Fatal(err)
	}
	if got := strings.TrimSpace(read(t, dump, "pwd.1")); got != real {
		t.Fatalf("working folder %s, want %s", got, real)
	}
	if res.Model != "claude-test-model" || res.ModelNote != "" || res.Degraded != "" {
		t.Fatalf("result %+v", res)
	}
}

func TestClaudeEnvelopes(t *testing.T) {
	cases := []struct {
		name, stdout, stderr string
		exit                 int
		reason               string
		tokensIn, tokensOut  int
	}{
		{name: "structured output", stdout: claudeSuccess, tokensIn: 200, tokensOut: 40},
		{name: "no usage", stdout: `{"type":"result","subtype":"success","is_error":false,"structured_output":{"summary":"done"}}`},
		// modelUsage counts every model call, so it wins over usage.
		{name: "model usage", stdout: `{"type":"result","subtype":"success","is_error":false,"usage":{"input_tokens":1,"output_tokens":1},` +
			`"modelUsage":{"claude-a":{"inputTokens":100,"outputTokens":10,"cacheReadInputTokens":5,"cacheCreationInputTokens":5},` +
			`"claude-b":{"inputTokens":20,"outputTokens":2,"cacheReadInputTokens":0,"cacheCreationInputTokens":0}},"structured_output":{"summary":"done"}}`,
			tokensIn: 130, tokensOut: 12},
		{name: "structured output missing", stdout: `{"type":"result","subtype":"success","is_error":false,"result":"{\"summary\":\"done\"}","usage":{"input_tokens":10,"output_tokens":5}}`,
			reason: ReasonInvalidOutput, tokensIn: 10, tokensOut: 5},
		{name: "structured output null", stdout: `{"type":"result","subtype":"success","is_error":false,"structured_output":null}`, reason: ReasonInvalidOutput},
		{name: "structured output not an object", stdout: `{"type":"result","subtype":"success","is_error":false,"structured_output":"done"}`, reason: ReasonInvalidOutput},
		{name: "structured output retries used up", stdout: `{"type":"result","subtype":"error_max_structured_output_retries","is_error":true,"errors":["no valid output"],"usage":{"input_tokens":300,"output_tokens":90}}`,
			exit: 1, reason: ReasonInvalidOutput, tokensIn: 300, tokensOut: 90},
		{name: "authentication", stdout: `{"type":"result","subtype":"success","is_error":true,"result":"Invalid API key · Please run /login"}`, exit: 1, reason: ReasonAuth},
		{name: "authentication without an envelope", stderr: "Not logged in. Run claude auth login to authenticate.\n", exit: 1, reason: ReasonAuth},
		{name: "rate limit", stdout: `{"type":"result","subtype":"success","is_error":true,"result":"Claude AI usage limit reached|1767225600"}`, exit: 1, reason: ReasonRateLimit},
		{name: "rate limit by status", stdout: `{"type":"result","subtype":"success","is_error":true,"api_error_status":429,"result":"API Error: request rejected"}`, exit: 1, reason: ReasonRateLimit},
		{name: "overloaded by status", stdout: `{"type":"result","subtype":"success","is_error":true,"api_error_status":529,"result":"API Error: request rejected"}`, exit: 1, reason: ReasonRateLimit},
		{name: "authentication by status", stdout: `{"type":"result","subtype":"success","is_error":true,"api_error_status":401,"result":"API Error: request rejected"}`, exit: 1, reason: ReasonAuth},
		// An outage or a command line a host upgrade no longer takes is a
		// host error, not the batch's invalid output.
		{name: "server error by status", stdout: `{"type":"result","subtype":"success","is_error":true,"api_error_status":503,"result":"API Error: request failed"}`, exit: 1, reason: ReasonHostError},
		{name: "server error", stdout: `{"type":"result","subtype":"success","is_error":true,"result":"API Error: 500 Internal server error. This is a server-side issue, usually temporary — try again in a moment."}`,
			exit: 1, reason: ReasonHostError},
		{name: "no connection", stdout: `{"type":"result","subtype":"success","is_error":true,"result":"API Error: Can't reach the API server — check your internet or DNS (ENOTFOUND). A VPN, firewall, or security software can cause this. Check those or switch networks, then try again."}`,
			exit: 1, reason: ReasonHostError},
		{name: "unknown option after an upgrade", stderr: "error: unknown option '--strict-mcp-config'\n", exit: 1, reason: ReasonHostError},
		// A refusal of --strict-mcp-config under a managed MCP config is
		// retried once without the flag: TestClaudeManagedMCPConfigRetriesWithoutTheStrictFlag.
		// A refused tool call means the run had a tool to call: tool_used,
		// even beside an answer.
		{name: "tool denied", stdout: `{"type":"result","subtype":"success","is_error":false,"usage":{"input_tokens":10,"output_tokens":5},` +
			`"permission_denials":[{"tool_name":"mcp__docs__search","tool_use_id":"toolu_01","tool_input":{"query":"release"}}],"structured_output":{"summary":"done"}}`,
			reason: ReasonToolUsed, tokensIn: 10, tokensOut: 5},
		{name: "denials in another shape", stdout: `{"type":"result","subtype":"success","is_error":false,"permission_denials":{"count":1},"structured_output":{"summary":"done"}}`, reason: ReasonToolUsed},
		{name: "no denials", stdout: `{"type":"result","subtype":"success","is_error":false,"permission_denials":null,"structured_output":{"summary":"done"}}`},
		{name: "error during execution", stdout: `{"type":"result","subtype":"error_during_execution","is_error":true,"errors":["stream ended"]}`, exit: 1, reason: ReasonInvalidOutput},
		// The model's own words never name a failure, here a non-zero exit.
		{name: "failed exit with model text", stdout: `{"type":"result","subtype":"success","is_error":false,"result":"we hit the rate limit yesterday","structured_output":{"summary":"done"}}`,
			exit: 1, reason: ReasonInvalidOutput},
		{name: "plain-text error", stdout: "Error: Invalid API key · Please run /login\n", exit: 1, reason: ReasonAuth},
		{name: "plain text after a clean exit", stdout: "we hit the rate limit yesterday\n", reason: ReasonInvalidOutput},
		{name: "nothing printed", reason: ReasonInvalidOutput},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			isolate(t)
			dump := fakeCLI(t, "")
			put(t, dump, "stdout", tc.stdout)
			put(t, dump, "stderr", tc.stderr)
			put(t, dump, "exit", strconv.Itoa(tc.exit))
			res, err := NewRunner(HostClaude).Run(context.Background(), request(t, HostClaude))
			if err != nil {
				t.Fatal(err)
			}
			if res.Reason != tc.reason || res.TokensIn != tc.tokensIn || res.TokensOut != tc.tokensOut {
				t.Fatalf("result %+v, want reason %q and tokens %d/%d", res, tc.reason, tc.tokensIn, tc.tokensOut)
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

func TestClaudeModelRejectionFallsBack(t *testing.T) {
	isolate(t)
	dump := fakeCLI(t, "")
	put(t, dump, "stdout.1", claudeRejected)
	put(t, dump, "exit.1", "1")
	put(t, dump, "stdout.2", claudeRejected)
	put(t, dump, "exit.2", "1")
	put(t, dump, "stdout.3", claudeSuccess)
	res, err := NewRunner(HostClaude).Run(context.Background(), request(t, HostClaude))
	if err != nil {
		t.Fatal(err)
	}
	if n := runs(t, dump); n != 3 {
		t.Fatalf("ran %d times, want 3", n)
	}
	first, second, third := argsOf(t, dump, 1), argsOf(t, dump, 2), argsOf(t, dump, 3)
	if !hasPair(first, "--model", "claude-test-model") || !hasPair(first, "--effort", "max") {
		t.Fatalf("first attempt %q", first)
	}
	if !hasPair(second, "--model", "claude-test-model") || slices.Contains(second, "--effort") {
		t.Fatalf("second attempt must drop only the effort: %q", second)
	}
	if slices.Contains(third, "--model") || slices.Contains(third, "--effort") {
		t.Fatalf("third attempt must drop the model too: %q", third)
	}
	if res.Reason != "" || string(res.Output) != answer || res.Model != "" || res.ModelNote != "fallback from claude-test-model" || res.Attempts != 3 {
		t.Fatalf("result %+v", res)
	}
	// The host default ran at its default effort, which the note says.
	if res.Degraded != "effort max was dropped with the model; the run used the default effort" {
		t.Fatalf("degraded %q", res.Degraded)
	}
	if res.TokensIn != 200 || res.TokensOut != 40 {
		t.Fatalf("tokens %d/%d", res.TokensIn, res.TokensOut)
	}
}

// The step-down is bounded by the table: below its lowest entry the run goes
// without the effort flag and keeps the model, which a model that takes no
// effort setting at all needs.
func TestClaudeEffortRefusedAtEveryLevel(t *testing.T) {
	isolate(t)
	dump := fakeHost(t, HostClaude)
	for i := range claudeEfforts {
		n := strconv.Itoa(i + 1)
		put(t, dump, "stdout."+n, claudeEffortRejected(claudeEfforts[len(claudeEfforts)-1-i]))
		put(t, dump, "exit."+n, "1")
	}
	res, err := NewRunner(HostClaude).Run(context.Background(), request(t, HostClaude))
	if err != nil {
		t.Fatal(err)
	}
	want := len(claudeEfforts) + 1
	if n := runs(t, dump); n != want || res.Attempts != want {
		t.Fatalf("ran %d times, %d attempts reported, want %d", n, res.Attempts, want)
	}
	for i := range claudeEfforts {
		if args := argsOf(t, dump, i+1); !hasPair(args, "--effort", claudeEfforts[len(claudeEfforts)-1-i]) {
			t.Fatalf("attempt %d: %q", i+1, args)
		}
	}
	if last := argsOf(t, dump, want); slices.Contains(last, "--effort") || !hasPair(last, "--model", "claude-test-model") {
		t.Fatalf("last attempt must drop only the effort: %q", last)
	}
	if res.Reason != "" || res.Model != "claude-test-model" || res.ModelNote != "" || res.Effort != "" || res.Degraded != "effort max was rejected; the run used the default effort" {
		t.Fatalf("result %+v", res)
	}
}

// Where an organization manages MCP servers, claude refuses
// --strict-mcp-config. The run retries once without the flag and says so;
// it never retries that refusal twice.
func TestClaudeManagedMCPConfigRetriesWithoutTheStrictFlag(t *testing.T) {
	const refusal = "Error: You cannot use --strict-mcp-config when an enterprise MCP config is present"
	t.Run("retried once", func(t *testing.T) {
		isolate(t)
		dump := fakeCLI(t, "")
		put(t, dump, "stderr.1", refusal)
		put(t, dump, "exit.1", "1")
		put(t, dump, "stdout.2", claudeSuccess)
		res, err := NewRunner(HostClaude).Run(context.Background(), request(t, HostClaude))
		if err != nil {
			t.Fatal(err)
		}
		if n := runs(t, dump); n != 2 || res.Attempts != 2 {
			t.Fatalf("ran %d times, %d attempts reported, want 2", n, res.Attempts)
		}
		if first, second := argsOf(t, dump, 1), argsOf(t, dump, 2); !slices.Contains(first, "--strict-mcp-config") || slices.Contains(second, "--strict-mcp-config") {
			t.Fatalf("attempts %q then %q", first, second)
		}
		if res.Reason != "" || string(res.Output) != answer || res.Degraded != "a managed MCP config refused --strict-mcp-config; the run went without it" {
			t.Fatalf("result %+v", res)
		}
	})
	t.Run("not twice", func(t *testing.T) {
		isolate(t)
		dump := fakeCLI(t, "")
		put(t, dump, "stderr", refusal)
		put(t, dump, "exit", "1")
		res, err := NewRunner(HostClaude).Run(context.Background(), request(t, HostClaude))
		if err != nil {
			t.Fatal(err)
		}
		if n := runs(t, dump); n != 2 || res.Reason != ReasonHostError {
			t.Fatalf("ran %d times, reason %q; want 2 runs and host_error", n, res.Reason)
		}
	})
}

func TestClaudeModelRejectedOnEveryAttempt(t *testing.T) {
	isolate(t)
	dump := fakeCLI(t, "")
	put(t, dump, "stdout", claudeRejected)
	put(t, dump, "exit", "1")
	res, err := NewRunner(HostClaude).Run(context.Background(), request(t, HostClaude))
	if err != nil {
		t.Fatal(err)
	}
	if n := runs(t, dump); n != 3 {
		t.Fatalf("ran %d times, want 3", n)
	}
	if res.Reason != ReasonModelRejected || res.Model != "" || res.ModelNote != "fallback from claude-test-model" || res.Output != nil || res.Attempts != 3 {
		t.Fatalf("result %+v", res)
	}
}

func TestClaudeUnknownModelRunsTheDefault(t *testing.T) {
	isolate(t)
	dump := fakeHost(t, HostClaude)
	r := request(t, HostClaude)
	r.Model = ""
	r.Effort = "low"
	res, err := NewRunner(HostClaude).Run(context.Background(), r)
	if err != nil || res.Reason != "" {
		t.Fatalf("run failed: %+v %v", res, err)
	}
	args := argsOf(t, dump, 1)
	if slices.Contains(args, "--model") || !hasPair(args, "--effort", "low") {
		t.Fatalf("arguments %q", args)
	}
	if res.Model != "" || res.ModelNote != NoteDefaultModel {
		t.Fatalf("result %+v", res)
	}
	// Without a model there is only the effort left to drop.
	put(t, dump, "stdout", claudeRejected)
	put(t, dump, "exit", "1")
	if err := os.Remove(filepath.Join(dump, "count")); err != nil {
		t.Fatal(err)
	}
	res, err = NewRunner(HostClaude).Run(context.Background(), r)
	if err != nil || res.Reason != ReasonModelRejected || res.ModelNote != NoteDefaultModel {
		t.Fatalf("result %+v %v", res, err)
	}
	if n := runs(t, dump); n != 2 {
		t.Fatalf("ran %d times, want 2", n)
	}
}

// An envelope printed before the timeout doesn't count while the CLI is
// still running.
func TestClaudeAnswerBeforeTheTimeoutStillTimesOut(t *testing.T) {
	isolate(t)
	dump := fakeCLI(t, "sleep 30")
	put(t, dump, "stdout", claudeSuccess)
	r := request(t, HostClaude)
	r.Timeout = 300 * time.Millisecond
	start := time.Now()
	res, err := NewRunner(HostClaude).Run(context.Background(), r)
	if err != nil || res.Reason != ReasonTimeout {
		t.Fatalf("result %+v %v", res, err)
	}
	if elapsed := time.Since(start); elapsed > 10*time.Second {
		t.Fatalf("took %v", elapsed)
	}
}

// hasPair reports whether flag is followed by value in args.
func hasPair(args []string, flag, value string) bool {
	i := slices.Index(args, flag)
	return i >= 0 && i+1 < len(args) && args[i+1] == value
}
