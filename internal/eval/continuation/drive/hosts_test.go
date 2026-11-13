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
		if !ok || e.Argv == nil || len(e.Off) == 0 || e.Plugin == nil || e.Read == nil || len(e.ConfigEnv) == 0 {
			t.Fatalf("%s: %+v", h.ID, e)
		}
	}
	if len(hosts) != len(adapter.All()) {
		t.Fatalf("the table has %d entries for %d hosts", len(hosts), len(adapter.All()))
	}
}

// Each host's arguments: the turn bound where the host has one, the
// model, the session continued, the condition's flags, and the prompt
// last, after "--", so that one that starts with "-" is no flag.
func TestArgv(t *testing.T) {
	c := chat{Prompt: "- a made-up prompt that opens with a list item", Turns: 7, Model: "made-up-model", Resume: "s-1", Extra: []string{"--x", "y"}}
	cc := hosts[adapter.ClaudeCode].Argv(c)
	if want := []string{"-p", "--output-format", "stream-json", "--verbose", "--max-turns", "7", "--permission-mode", "bypassPermissions",
		"--model", "made-up-model", "--resume", "s-1", "--x", "y", "--", c.Prompt}; !slices.Equal(cc, want) {
		t.Fatalf("Claude Code: %q", cc)
	}
	cx := hosts[adapter.Codex].Argv(c)
	if want := []string{"exec", "--json", "--skip-git-repo-check", "--dangerously-bypass-approvals-and-sandbox", "--model", "made-up-model", "--x", "y",
		"resume", "s-1", "--", c.Prompt}; !slices.Equal(cx, want) {
		t.Fatalf("Codex: %q", cx)
	}
	c.Resume, c.Model = "", ""
	if got := hosts[adapter.Codex].Argv(c); slices.Contains(got, "resume") || slices.Contains(got, "--model") || !slices.Equal(got[len(got)-2:], []string{"--", c.Prompt}) {
		t.Fatalf("Codex, a start: %q", got)
	}
	if got := hosts[adapter.ClaudeCode].Argv(c); slices.Contains(got, "--resume") || slices.Contains(got, "--model") || !slices.Equal(got[len(got)-2:], []string{"--", c.Prompt}) {
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
{"type":"result","subtype":"success","is_error":false,"session_id":"s-9","usage":{"input_tokens":1,"output_tokens":1},"modelUsage":{"m-b":{"inputTokens":10,"cacheCreationInputTokens":5,"cacheReadInputTokens":100,"outputTokens":3},"m-a":{"inputTokens":20,"cacheReadInputTokens":200,"outputTokens":40}}}
`)
	got := readStreamJSON(out)
	want := report{Session: "s-9", AgentEvents: 3, PromptTokens: 335, CachedTokens: 300, OutputTokens: 43, Model: "m-a", Answered: true, Usage: true}
	if got != want {
		t.Fatalf("%+v, want %+v", got, want)
	}
	usage := readStreamJSON([]byte(`{"type":"assistant","message":{"model":"m-c","content":[]}}` + "\n" +
		`{"type":"result","is_error":true,"api_error_status":429,"session_id":"s-2","usage":{"input_tokens":4,"cache_creation_input_tokens":2,"cache_read_input_tokens":6,"output_tokens":9}}`))
	if want := (report{Session: "s-2", PromptTokens: 12, CachedTokens: 6, OutputTokens: 9, Model: "m-c", Fail: CodeRateLimited, Usage: true}); usage != want {
		t.Fatalf("%+v, want %+v", usage, want)
	}
	// Sign-in and rate limit have their codes; any other error, a failure
	// of the host's service among them, is no answer and has no code.
	for status, code := range map[int]string{401: CodeSignIn, 403: CodeSignIn, 429: CodeRateLimited, 500: "", 529: ""} {
		r := readStreamJSON([]byte(`{"type":"result","subtype":"error_during_execution","is_error":true,"api_error_status":` + strconv.Itoa(status) + `}`))
		if r.Fail != code || r.Answered {
			t.Fatalf("status %d: %+v", status, r)
		}
	}
	for name, c := range map[string]struct {
		out      string
		answered bool
	}{
		"no error, though a status":     {`{"type":"result","subtype":"success","is_error":false,"api_error_status":401}`, true},
		"the turn bound":                {`{"type":"result","subtype":"error_max_turns","is_error":true}`, true},
		"an error with no status":       {`{"type":"result","subtype":"success","is_error":true}`, false},
		"an error subtype":              {`{"type":"result","subtype":"error_during_execution","is_error":false}`, false},
		"no result":                     {`{"type":"system","subtype":"init","session_id":"s-1"}`, false},
		"nothing":                       {``, false},
		"an answer after a failed one":  {`{"type":"result","is_error":true,"api_error_status":500}` + "\n" + `{"type":"result","subtype":"success","is_error":false}`, false},
		"an answer before a failed one": {`{"type":"result","subtype":"success","is_error":false}` + "\n" + `{"type":"result","is_error":true,"api_error_status":500}`, false},
	} {
		if r := readStreamJSON([]byte(c.out)); r.Fail != "" || r.Answered != c.answered {
			t.Fatalf("%s: %+v", name, r)
		}
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
	if got, want := readExecJSON(out), (report{Session: "th-1", AgentEvents: 2, PromptTokens: 550, CachedTokens: 330, OutputTokens: 22, Model: "m-x", Answered: true, Usage: true}); got != want {
		t.Fatalf("%+v, want %+v", got, want)
	}
	// Sign-in and rate limit have their codes; any other failure, and an
	// output with no completed turn, is no answer.
	for line, code := range map[string]string{
		`{"type":"turn.failed","error":{"message":"unexpected status 401 Unauthorized"}}`:    CodeSignIn,
		`{"type":"error","message":"stream error: 429 Too Many Requests"}`:                   CodeRateLimited,
		`{"type":"turn.failed","error":{"message":"the model could not finish"}}`:            "",
		`{"type":"turn.failed","error":{"message":"unexpected status 529"}}`:                 "",
		`{"type":"turn.completed","usage":{}}` + "\n" + `{"type":"error","message":"a 5xx"}`: "",
		`{"type":"thread.started","thread_id":"th-2"}`:                                       "",
	} {
		if got := readExecJSON([]byte(line)); got.Fail != code || got.Answered {
			t.Fatalf("%s: %+v", line, got)
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

// Claude Code gives its usage in two places, usage and modelUsage: either
// is enough, and the one naming more tokens is counted, so neither place
// can hide what was spent. A result with neither, or a modelUsage naming
// no model, gave no usage, and so did a call with one result that lacks
// it among several.
func TestStreamJSONUsage(t *testing.T) {
	for name, c := range map[string]struct {
		out            string
		usage          bool
		prompt, output int64
	}{
		"usage alone":           {`{"type":"result","subtype":"success","usage":{"input_tokens":7,"output_tokens":2}}`, true, 7, 2},
		"modelUsage alone":      {`{"type":"result","subtype":"success","modelUsage":{"m-a":{"inputTokens":9,"outputTokens":4}}}`, true, 9, 4},
		"usage names more":      {`{"type":"result","subtype":"success","usage":{"input_tokens":70,"output_tokens":20},"modelUsage":{"m-a":{"inputTokens":9,"outputTokens":4}}}`, true, 70, 20},
		"modelUsage names more": {`{"type":"result","subtype":"success","usage":{"input_tokens":7,"output_tokens":2},"modelUsage":{"m-a":{"inputTokens":90,"outputTokens":40}}}`, true, 90, 40},
		"an empty usage":        {`{"type":"result","subtype":"success","usage":{}}`, true, 0, 0},
		"neither":               {`{"type":"result","subtype":"success"}`, false, 0, 0},
		"no model named":        {`{"type":"result","subtype":"success","modelUsage":{}}`, false, 0, 0},
		"usage not an object":   {`{"type":"result","subtype":"success","usage":12}`, false, 0, 0},
		"a null usage":          {`{"type":"result","subtype":"success","usage":null}`, false, 0, 0},
		"one of two lacks it": {`{"type":"result","subtype":"success","usage":{"input_tokens":7,"output_tokens":2}}` + "\n" +
			`{"type":"result","subtype":"success"}`, false, 7, 2},
		"no result": {`{"type":"assistant","message":{"model":"m-a","content":[]}}`, false, 0, 0},
	} {
		r := readStreamJSON([]byte(c.out))
		if r.Usage != c.usage || r.PromptTokens != c.prompt || r.OutputTokens != c.output {
			t.Errorf("%s: %+v", name, r)
		}
	}
}

// Codex gives its usage in each turn.completed: one turn without it, among
// several, means the call gave none.
func TestExecJSONUsage(t *testing.T) {
	for name, c := range map[string]struct {
		out   string
		usage bool
	}{
		"each turn":        {`{"type":"turn.completed","usage":{"input_tokens":5,"output_tokens":1}}`, true},
		"an empty usage":   {`{"type":"turn.completed","usage":{}}`, true},
		"none":             {`{"type":"turn.completed"}`, false},
		"a null usage":     {`{"type":"turn.completed","usage":null}`, false},
		"one of two lacks": {`{"type":"turn.completed","usage":{"input_tokens":5,"output_tokens":1}}` + "\n" + `{"type":"turn.completed"}`, false},
		"no turn":          {`{"type":"thread.started","thread_id":"th-1"}`, false},
	} {
		if r := readExecJSON([]byte(c.out)); r.Usage != c.usage {
			t.Errorf("%s: %+v", name, r)
		}
	}
}
