package drive

import (
	"slices"
	"strconv"
	"testing"

	"github.com/heyNag/eva-md/internal/host/adapter"
)

// Every host the adapter knows has an entry with each part.
func TestEveryHostHasAnEntry(t *testing.T) {
	for _, h := range adapter.All() {
		e, ok := hosts[h.ID]
		if !ok || e.Argv == nil || len(e.Off) == 0 || e.Plugin == nil || e.Read == nil {
			t.Fatalf("%s: %+v", h.ID, e)
		}
	}
	if len(hosts) != len(adapter.All()) {
		t.Fatalf("the table has %d entries for %d hosts", len(hosts), len(adapter.All()))
	}
}

// Each host's arguments: the prompt, the turn bound where the host has
// one, the model, the session continued and the condition's flags.
func TestArgv(t *testing.T) {
	c := chat{Prompt: "a made-up prompt", Turns: 7, Model: "made-up-model", Resume: "s-1", Extra: []string{"--x", "y"}}
	cc := hosts[adapter.ClaudeCode].Argv(c)
	if want := []string{"-p", "a made-up prompt", "--output-format", "stream-json", "--verbose", "--max-turns", "7", "--permission-mode", "bypassPermissions",
		"--model", "made-up-model", "--resume", "s-1", "--x", "y"}; !slices.Equal(cc, want) {
		t.Fatalf("Claude Code: %q", cc)
	}
	cx := hosts[adapter.Codex].Argv(c)
	if want := []string{"exec", "--json", "--skip-git-repo-check", "--dangerously-bypass-approvals-and-sandbox", "--model", "made-up-model", "--x", "y",
		"resume", "s-1", "a made-up prompt"}; !slices.Equal(cx, want) {
		t.Fatalf("Codex: %q", cx)
	}
	c.Resume, c.Model = "", ""
	if got := hosts[adapter.Codex].Argv(c); slices.Contains(got, "resume") || slices.Contains(got, "--model") || got[len(got)-1] != c.Prompt {
		t.Fatalf("Codex, a start: %q", got)
	}
	if got := hosts[adapter.ClaudeCode].Argv(c); slices.Contains(got, "--resume") || slices.Contains(got, "--model") {
		t.Fatalf("Claude Code, a start: %q", got)
	}
}

// Claude Code's stream: tokens from modelUsage, cached included in the
// prompt's, the model that wrote the most, the tool calls, and lines that
// are not JSON skipped.
func TestReadStreamJSON(t *testing.T) {
	out := []byte(`{"type":"system","subtype":"init","session_id":"s-9","model":"m-a"}
a notice that is not JSON
{"type":"assistant","session_id":"s-9","message":{"model":"m-a","content":[{"type":"tool_use"},{"type":"text"},{"type":"tool_use"}]}}
{"type":"assistant","session_id":"s-9","message":{"model":"m-a","content":[{"type":"tool_use"}]}}
{"type":"result","is_error":false,"session_id":"s-9","usage":{"input_tokens":1,"output_tokens":1},"modelUsage":{"m-b":{"inputTokens":10,"cacheCreationInputTokens":5,"cacheReadInputTokens":100,"outputTokens":3},"m-a":{"inputTokens":20,"cacheReadInputTokens":200,"outputTokens":40}}}
`)
	got := readStreamJSON(out)
	want := report{Session: "s-9", AgentEvents: 3, PromptTokens: 335, CachedTokens: 300, OutputTokens: 43, Model: "m-a"}
	if got != want {
		t.Fatalf("%+v, want %+v", got, want)
	}
	usage := readStreamJSON([]byte(`{"type":"assistant","message":{"model":"m-c","content":[]}}` + "\n" +
		`{"type":"result","is_error":true,"api_error_status":429,"session_id":"s-2","usage":{"input_tokens":4,"cache_creation_input_tokens":2,"cache_read_input_tokens":6,"output_tokens":9}}`))
	if want := (report{Session: "s-2", PromptTokens: 12, CachedTokens: 6, OutputTokens: 9, Model: "m-c", Fail: CodeRateLimited}); usage != want {
		t.Fatalf("%+v, want %+v", usage, want)
	}
	for status, code := range map[int]string{401: CodeSignIn, 403: CodeSignIn, 429: CodeRateLimited, 500: ""} {
		r := readStreamJSON([]byte(`{"type":"result","is_error":true,"api_error_status":` + strconv.Itoa(status) + `}`))
		if r.Fail != code {
			t.Fatalf("status %d: %q", status, r.Fail)
		}
	}
	if r := readStreamJSON([]byte(`{"type":"result","is_error":false,"api_error_status":401}`)); r.Fail != "" {
		t.Fatalf("an answer that is no error failed: %q", r.Fail)
	}
}

// Codex's events: the thread, tool items but not messages, reasoning or
// errors, every turn's tokens, the model a turn names, and failures.
func TestReadExecJSON(t *testing.T) {
	out := []byte(`{"type":"thread.started","thread_id":"th-1"}
{"type":"turn.started"}
{"type":"item.started","item":{"type":"command_execution"}}
{"type":"item.completed","item":{"type":"command_execution"}}
{"type":"item.completed","item":{"type":"file_change"}}
{"type":"item.completed","item":{"type":"agent_message"}}
{"type":"item.completed","item":{"type":"reasoning"}}
{"type":"item.completed","item":{"type":"error"}}
{"type":"turn.completed","model":"m-x","usage":{"input_tokens":500,"cached_input_tokens":300,"output_tokens":20}}
{"type":"turn.completed","usage":{"input_tokens":50,"cached_input_tokens":30,"output_tokens":2}}
`)
	if got, want := readExecJSON(out), (report{Session: "th-1", AgentEvents: 2, PromptTokens: 550, CachedTokens: 330, OutputTokens: 22, Model: "m-x"}); got != want {
		t.Fatalf("%+v, want %+v", got, want)
	}
	for line, code := range map[string]string{
		`{"type":"turn.failed","error":{"message":"unexpected status 401 Unauthorized"}}`: CodeSignIn,
		`{"type":"error","message":"stream error: 429 Too Many Requests"}`:                CodeRateLimited,
		`{"type":"turn.failed","error":{"message":"the model could not finish"}}`:         "",
	} {
		if got := readExecJSON([]byte(line)); got.Fail != code {
			t.Fatalf("%s: %q", line, got.Fail)
		}
	}
}

// A plugin's hooks.json becomes a managed requirements file's tables, its
// strings quoted as TOML reads them.
func TestManagedHooks(t *testing.T) {
	data := []byte(`{"hooks":{"Stop":[{"hooks":[{"type":"command","command":"'/a b/eva-md' hook --host x || echo '{}'","timeout":5}]}],
"SessionStart":[{"matcher":"startup|resume","hooks":[{"type":"command","command":"say \"hi\"\\there","additionalContextLimit":3000,"async":true}]}]}}`)
	got, err := managedHooks(data)
	want := "[[hooks.SessionStart]]\nmatcher = \"startup|resume\"\n[[hooks.SessionStart.hooks]]\nadditionalContextLimit = 3000\nasync = true\n" +
		"command = \"say \\\"hi\\\"\\\\there\"\ntype = \"command\"\n" +
		"[[hooks.Stop]]\n[[hooks.Stop.hooks]]\ncommand = \"'/a b/eva-md' hook --host x || echo '{}'\"\ntimeout = 5\ntype = \"command\"\n"
	if err != nil || string(got) != want {
		t.Fatalf("%v\n%s\nwant\n%s", err, got, want)
	}
	for _, bad := range []string{`{}`, `not json`, `{"hooks":{"Stop":[{"hooks":[{"args":["a"]}]}]}}`} {
		if _, err := managedHooks([]byte(bad)); err == nil {
			t.Fatalf("%s was rendered", bad)
		}
	}
	if s := tomlString("a\nb\x7f"); s != `"a\u000Ab\u007F"` {
		t.Fatalf("%s", s)
	}
}
