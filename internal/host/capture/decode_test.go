package capture

import (
	"encoding/json"
	"errors"
	"go/ast"
	"slices"
	"strconv"
	"strings"
	"testing"

	"github.com/heyNag/eva-md/internal/host/adapter"
	"github.com/heyNag/eva-md/internal/ledger/event"
)

const promptPayload = `{"session_id":"s-made-up","hook_event_name":"UserPromptSubmit","prompt_id":"p-made-up","prompt":"Summarize the release checklist."}`

func TestDecodeRefuses(t *testing.T) {
	big := `{"hook_event_name":"UserPromptSubmit","prompt":"` + strings.Repeat("a", MaxPayload) + `"}`
	for _, tc := range []struct {
		name, raw string
		want      error
	}{
		{"over 2 MiB", big, ErrTooLarge},
		{"not JSON", "made-up", ErrNotJSON},
		{"empty", "", ErrNotJSON},
		{"an array", `[` + promptPayload + `]`, ErrNotJSON},
		{"a string", `"UserPromptSubmit"`, ErrNotJSON},
		{"null", `null`, ErrNotJSON},
		{"two objects", promptPayload + promptPayload, ErrNotJSON},
		{"text after", promptPayload + " x", ErrNotJSON},
		{"cut short", promptPayload[:len(promptPayload)-1], ErrNotJSON},
		{"a key twice", `{"hook_event_name":"UserPromptSubmit","agent_id":"a-made-up","prompt":"made-up","agent_id":""}`, ErrNotJSON},
		{"a prompt that is no string", `{"hook_event_name":"UserPromptSubmit","prompt":["made-up"]}`, ErrNotJSON},
		{"an agent id that is no string", `{"hook_event_name":"UserPromptSubmit","prompt":"made-up","agent_id":7}`, ErrNotJSON},
		{"a source that is no string", `{"hook_event_name":"UserPromptSubmit","prompt":"made-up","source":{}}`, ErrNotJSON},
		{"an event name that is no string", `{"hook_event_name":true}`, ErrNotJSON},
		{"a reply that is no string", `{"hook_event_name":"Stop","last_assistant_message":1}`, ErrNotJSON},
	} {
		for _, host := range []adapter.ID{adapter.ClaudeCode, adapter.Codex} {
			d, err := Decode(host, []byte(tc.raw), Env{Entrypoint: "cli"})
			if !errors.Is(err, tc.want) || len(d.Events) != 0 {
				t.Errorf("%s on %s: %v, %+v; want %v", tc.name, host, err, d, tc.want)
			}
		}
	}
}

// A payload of exactly 2 MiB is read; one byte more is not.
func TestDecodeReadsUpTo2MiB(t *testing.T) {
	head := `{"hook_event_name":"UserPromptSubmit","prompt":"`
	raw := head + strings.Repeat("a", MaxPayload-len(head)-2) + `"}`
	if len(raw) != MaxPayload {
		t.Fatal(len(raw))
	}
	d, err := Decode(adapter.ClaudeCode, []byte(raw), Env{Entrypoint: "cli"})
	if err != nil || len(d.Events) != 1 || d.Events[0].Author != event.AuthorTyped || len(d.Events[0].Text) != MaxPayload-len(head)-2 {
		t.Fatalf("%v, %d events", err, len(d.Events))
	}
	if _, err := Decode(adapter.ClaudeCode, []byte(raw+" "), Env{Entrypoint: "cli"}); !errors.Is(err, ErrTooLarge) {
		t.Fatalf("one byte over: %v", err)
	}
}

// A key that is a field Decode reads in another letter case is refused,
// as a key twice is: v1's reader takes it for the field, so it could give
// or take away an agent id.
func TestDecodeRefusesAKeyInAnotherCase(t *testing.T) {
	for _, key := range readKeys {
		for _, other := range []string{strings.ToUpper(key), strings.ToUpper(key[:1]) + key[1:], strings.Replace(key, "s", "ſ", 1)} {
			if other == key {
				continue
			}
			raw := `{"hook_event_name":"UserPromptSubmit","prompt":"made-up","` + other + `":"a-made-up"}`
			if key == "hook_event_name" || key == "prompt" {
				raw = `{"` + other + `":"a-made-up"}`
			}
			for _, host := range []adapter.ID{adapter.ClaudeCode, adapter.Codex} {
				if d, err := Decode(host, []byte(raw), Env{Entrypoint: "cli"}); !errors.Is(err, ErrNotJSON) || len(d.Events) != 0 {
					t.Errorf("%s on %s: %v, %+v", raw, host, err, d.Events)
				}
			}
		}
	}
	// A key no field is read from is not looked at, in any case.
	d, err := Decode(adapter.ClaudeCode, []byte(`{"hook_event_name":"UserPromptSubmit","prompt":"made-up","agent_type":"x","AGENT_TYPE":"y","Cwd":"/made-up"}`), Env{Entrypoint: "cli"})
	if err != nil || len(d.Events) != 1 || d.Events[0].Author != event.AuthorTyped {
		t.Errorf("%v %+v", err, d.Events)
	}
}

// An error carries none of the payload's own text: a key that is no field
// Decode reads is never quoted, and a key in another case is named by the
// field it stands for.
func TestDecodeErrorsQuoteNoPayloadText(t *testing.T) {
	const secret = "sk-made-up-0000 my private note"
	for _, raw := range []string{
		`{"hook_event_name":"UserPromptSubmit","prompt":"x","` + secret + `":1,"` + secret + `":2}`,
		`{"hook_event_name":"UserPromptSubmit","prompt":"` + secret + `","prompt":"` + secret + `"}`,
		`{"hook_event_name":"UserPromptSubmit","prompt":"` + secret + `","PROMPT":"` + secret + `"}`,
		`{"hook_event_name":"UserPromptSubmit","prompt":"x","ſource":"` + secret + `"}`,
		`{"hook_event_name":"UserPromptSubmit","prompt":["` + secret + `"]}`,
		`{"` + secret + `":`,
	} {
		_, err := Decode(adapter.Codex, []byte(raw), Env{})
		if !errors.Is(err, ErrNotJSON) || strings.Contains(err.Error(), "sk-made-up") || strings.Contains(err.Error(), "ſ") || strings.Contains(err.Error(), "PROMPT") {
			t.Errorf("%s: %v", raw, err)
		}
	}
	_, err := Decode(adapter.Codex, []byte(`{"agent_id":"a","agent_id":"b"}`), Env{})
	if err == nil || !strings.Contains(err.Error(), "agent_id") {
		t.Errorf("a field Decode reads, twice: %v", err)
	}
}

// Every field Decode reads is one of readKeys, so a key in another case
// of any of them is refused.
func TestEveryFieldReadIsAReadKey(t *testing.T) {
	read := 0
	for name, f := range packageFiles(t, false) {
		ast.Inspect(f, func(n ast.Node) bool {
			var lit ast.Expr
			switch n := n.(type) {
			case *ast.CallExpr:
				if id, ok := n.Fun.(*ast.Ident); ok && id.Name == "text" && len(n.Args) == 2 {
					lit = n.Args[1]
				}
			case *ast.IndexExpr:
				if id, ok := n.X.(*ast.Ident); ok && id.Name == "fields" {
					lit = n.Index
				}
			}
			if lit == nil {
				return true
			}
			b, ok := lit.(*ast.BasicLit)
			if !ok {
				return true // the loop over the payload's ids, checked below
			}
			key, _ := strconv.Unquote(b.Value)
			read++
			if !slices.Contains(readKeys, key) {
				t.Errorf("%s reads the field %q, which is not in readKeys", name, key)
			}
			return true
		})
	}
	for _, key := range []string{"hook_event_name", "session_id", "turn_id", "prompt_id", "agent_id"} {
		if !slices.Contains(readKeys, key) {
			t.Errorf("%q is not in readKeys", key)
		}
	}
	if read < 6 {
		t.Fatalf("found %d fields read", read)
	}
}

// Keys are matched exactly: a key that is no field's in any case is not
// read.
func TestDecodeMatchesKeysExactly(t *testing.T) {
	for _, tc := range []struct {
		raw   string
		agent string
		want  event.Author
	}{
		{`{"hook_event_name":"UserPromptSubmit","prompt":"made-up","agentid":"a-made-up"}`, "", event.AuthorTyped},
		{`{"hook_event_name":"UserPromptSubmit","prompt":"made-up","agent_id":"a-made-up","agent_id ":""}`, "a-made-up", event.AuthorSubagent},
		{`{"hook_event_name ":"UserPromptSubmit","prompt":"made-up"}`, "", ""},
		{`{"hook_event_name":"UserPromptSubmit","prompts":"made-up"}`, "", ""},
		{`{"hook_event_name":"UserPromptSubmit","prompt":"made-up","agent_id":null}`, "", event.AuthorTyped},
		{`{"hook_event_name":"UserPromptSubmit","prompt":"made-up","agent_id":"a-made-up"}`, "a-made-up", event.AuthorSubagent},
	} {
		d, err := Decode(adapter.ClaudeCode, []byte(tc.raw), Env{Entrypoint: "cli"})
		if err != nil || d.Agent != tc.agent {
			t.Errorf("%s: %v, agent %q", tc.raw, err, d.Agent)
			continue
		}
		if tc.want == "" && len(d.Events) != 0 || tc.want != "" && (len(d.Events) != 1 || d.Events[0].Author != tc.want) {
			t.Errorf("%s: %+v", tc.raw, d.Events)
		}
	}
}

// A host the adapter does not know declares no event.
func TestDecodeUnknownHost(t *testing.T) {
	d, err := Decode("made-up-host", []byte(promptPayload), Env{Entrypoint: "cli"})
	if err != nil || d.Rule != RuleUndeclared || len(d.Events) != 0 || d.Session != "s-made-up" || d.TID != "p-made-up" {
		t.Fatalf("%v %+v", err, d)
	}
}

// The entrypoint is read from Env and only on Claude Code, exactly as
// given.
func TestEntrypoints(t *testing.T) {
	for _, tc := range []struct {
		host       adapter.ID
		entrypoint string
		rule       string
		want       event.Author
	}{
		{adapter.ClaudeCode, "cli", RuleInteractive, event.AuthorTyped},
		{adapter.ClaudeCode, "claude-desktop", RuleInteractive, event.AuthorTyped},
		{adapter.ClaudeCode, "claude-vscode", RuleInteractive, event.AuthorTyped},
		{adapter.ClaudeCode, "sdk-cli", RuleProgram, event.AuthorProgram},
		{adapter.ClaudeCode, "sdk-ts", RuleProgram, event.AuthorProgram},
		{adapter.ClaudeCode, "sdk-py", RuleProgram, event.AuthorProgram},
		{adapter.ClaudeCode, "claude-desktop-3p", RuleEntrypointUnknown, event.AuthorUnknown},
		{adapter.ClaudeCode, "local-agent", RuleEntrypointUnknown, event.AuthorUnknown},
		{adapter.ClaudeCode, "", RuleEntrypointUnknown, event.AuthorUnknown},
		{adapter.ClaudeCode, "Cli", RuleEntrypointUnknown, event.AuthorUnknown},
		{adapter.ClaudeCode, " cli", RuleEntrypointUnknown, event.AuthorUnknown},
		{adapter.ClaudeCode, "cli\n", RuleEntrypointUnknown, event.AuthorUnknown},
		{adapter.Codex, "cli", RuleNoEvidence, event.AuthorUnknown},
		{adapter.Codex, "sdk-cli", RuleNoEvidence, event.AuthorUnknown},
		{adapter.Codex, "", RuleNoEvidence, event.AuthorUnknown},
	} {
		d, err := Decode(tc.host, []byte(promptPayload), Env{Entrypoint: tc.entrypoint})
		if err != nil || d.Rule != tc.rule || len(d.Events) != 1 || d.Events[0].Author != tc.want || d.Events[0].Text != "Summarize the release checklist." {
			t.Errorf("%s %q: %v %s %+v", tc.host, tc.entrypoint, err, d.Rule, d.Events)
		}
	}
}

// Each rule is tried in its order: an earlier rule decides a prompt a
// later one would file otherwise.
func TestRulesInOrder(t *testing.T) {
	wrapper := handOverText("", "made-up request", "user: made-up")
	for _, tc := range []struct {
		name, prompt, source, agent, entrypoint string
		rule                                    string
		want                                    event.Author
	}{
		{"builtin before an agent id", "/compact", "", "a-made-up", "cli", RuleBuiltin, ""},
		{"an agent id before a source", "made-up", "cron", "a-made-up", "sdk-cli", RuleAgent, event.AuthorSubagent},
		{"an agent id before the entrypoint", "made-up", "", "a-made-up", "cli", RuleAgent, event.AuthorSubagent},
		{"a source before the wrapper", wrapper, "cron", "", "cli", RuleSource, event.AuthorHost},
		{"an unlisted source before the wrapper", wrapper, "made-up", "", "cli", RuleSourceUnlisted, event.AuthorUnknown},
		{"a source before a prefix", "<teammate-message>made-up", "cron", "", "cli", RuleSource, event.AuthorHost},
		{"a source before the entrypoint", "made-up", "other_session", "", "cli", RuleSource, event.AuthorProgram},
		{"a source in another case", "made-up", "CRON", "", "cli", RuleSource, event.AuthorHost},
		{"a source with white space", "made-up", " cron", "", "cli", RuleSourceUnlisted, event.AuthorUnknown},
		{"no hand-over on Claude Code: a person's entrypoint", wrapper, "", "", "cli", RuleTag, event.AuthorUnknown},
		{"no hand-over on Claude Code: a program's entrypoint", wrapper, "", "", "sdk-py", RuleTag, event.AuthorUnknown},
		{"a prefix before a person's entrypoint", "<task-notification>made-up", "", "", "cli", RulePrefix, event.AuthorHost},
		{"a prefix before a program's entrypoint", "<teammate-message>made-up", "", "", "sdk-ts", RulePrefix, event.AuthorProgram},
		{"a prefix in another case", "<TASK-NOTIFICATION>made-up", "", "", "cli", RulePrefix, event.AuthorHost},
		{"a prefix after white space", "\n\t <task-notification>made-up", "", "", "cli", RulePrefix, event.AuthorHost},
		{"a tag before a person's entrypoint", "<made-up>words", "", "", "cli", RuleTag, event.AuthorUnknown},
		{"a tag before a program's entrypoint", "<made-up>words", "", "", "sdk-py", RuleTag, event.AuthorUnknown},
		{"a tag in another script", "<é>words", "", "", "cli", RuleTag, event.AuthorUnknown},
		{"not a tag", "< made-up", "", "", "cli", RuleInteractive, event.AuthorTyped},
		{"a closing tag is no leading tag", "</made-up>", "", "", "cli", RuleInteractive, event.AuthorTyped},
		{"a builtin command's word inside a prompt", "please /compact this", "", "", "cli", RuleInteractive, event.AuthorTyped},
		{"a builtin command after white space", "\n /HELP me", "", "", "cli", RuleBuiltin, ""},
		{"a builtin command run into a word", "/helpful hint", "", "", "cli", RuleInteractive, event.AuthorTyped},
		{"a builtin command before a tab", "/model\tother", "", "", "cli", RuleBuiltin, ""},
	} {
		rule, evs := promptEvents(adapter.ClaudeCode, tc.prompt, tc.source, tc.agent != "", Env{Entrypoint: tc.entrypoint})
		if rule != tc.rule {
			t.Errorf("%s: rule %s, want %s", tc.name, rule, tc.rule)
		}
		switch {
		case tc.want == "":
			if len(evs) != 0 {
				t.Errorf("%s: %+v, want none", tc.name, evs)
			}
		default:
			if len(evs) != 1 || evs[0].Author != tc.want || evs[0].Text != tc.prompt {
				t.Errorf("%s: %+v, want one by %s", tc.name, evs, tc.want)
			}
		}
	}
	// On Codex the hand-over is tried after a source and before a prefix.
	for _, tc := range []struct {
		name, prompt, source string
		rule                 string
	}{
		{"a source before the hand-over", wrapper, "cron", RuleSource},
		{"an unlisted source before the hand-over", wrapper, "made-up", RuleSourceUnlisted},
		{"the hand-over", " \n" + wrapper, "", RuleVoice},
		{"out of form before a tag", VoiceWrapper + "<task-notification>", "", RuleWrapper},
	} {
		if rule, _ := promptEvents(adapter.Codex, tc.prompt, tc.source, false, Env{}); rule != tc.rule {
			t.Errorf("Codex, %s: rule %s, want %s", tc.name, rule, tc.rule)
		}
	}
}

// Only Codex writes a hand-over. On Claude Code a prompt in its form, in
// form or not, under any entrypoint, is a leading tag: no line of it is
// spoken, and no part of it a front model's, the person's or a program's.
func TestNoHandOverOnClaudeCode(t *testing.T) {
	for _, prompt := range []string{
		handOverText("", "made-up request", "user: made-up spoken\nassistant: made-up reply"),
		handOverText(TailFlush, "made-up", "user: bye"),
		" \n" + handOverText("", "made-up", ""),
		handOverText("", "this & that", "user: made-up spoken"),
		VoiceWrapper + "made-up",
	} {
		for _, ep := range []string{"cli", "claude-desktop", "claude-vscode", "sdk-cli", "sdk-ts", "sdk-py", "claude-desktop-3p", "local-agent", "", "made-up"} {
			raw, err := json.Marshal(map[string]string{"hook_event_name": "UserPromptSubmit", "session_id": "s-made-up", "prompt": prompt})
			if err != nil {
				t.Fatal(err)
			}
			d, err := Decode(adapter.ClaudeCode, raw, Env{Entrypoint: ep})
			if err != nil || d.Rule != RuleTag || len(d.Events) != 1 || d.Events[0].Author != event.AuthorUnknown || d.Events[0].Text != prompt || len(d.Events[0].Marks) != 0 {
				t.Errorf("entrypoint %q, %q: %v %s %+v", ep, prompt, err, d.Rule, d.Events)
			}
		}
	}
}

// The other events, each declared, carry the payload's turn and agent and
// no author of a person.
func TestDecodeOtherEvents(t *testing.T) {
	for _, tc := range []struct {
		host adapter.ID
		raw  string
		want []event.Event
	}{
		{adapter.ClaudeCode, `{"hook_event_name":"Stop","turn_id":"t","agent_id":"a","last_assistant_message":"done"}`,
			[]event.Event{{Kind: event.KindReply, Author: event.AuthorSubagent, Text: "done"}}},
		{adapter.Codex, `{"hook_event_name":"Stop","turn_id":"t","last_assistant_message":" "}`,
			[]event.Event{{Kind: event.KindReply, Author: event.AuthorAgent, Text: " "}}},
		{adapter.ClaudeCode, `{"hook_event_name":"SessionStart","source":"Startup"}`,
			[]event.Event{{Kind: event.KindLife, Author: event.AuthorHost, Text: "session_start", Marks: []event.Mark{event.MarkName}}}},
		{adapter.Codex, `{"hook_event_name":"SessionStart","source":"fork","agent_id":"a"}`,
			[]event.Event{{Kind: event.KindLife, Author: event.AuthorHost, Text: "session_start: fork", Marks: []event.Mark{event.MarkName}}}},
		{adapter.ClaudeCode, `{"hook_event_name":"StopFailure","error":"rate_limit"}`,
			[]event.Event{{Kind: event.KindLife, Author: event.AuthorHost, Text: "stop_failure", Marks: []event.Mark{event.MarkName}}}},
		{adapter.ClaudeCode, `{"hook_event_name":"StopFailure","error":{"type":"overloaded_error"}}`,
			[]event.Event{{Kind: event.KindLife, Author: event.AuthorHost, Text: "stop_failure: overloaded_error", Marks: []event.Mark{event.MarkName}}}},
		{adapter.ClaudeCode, `{"hook_event_name":"PostToolUseFailure","tool_name":"Bash","agent_id":"a"}`,
			[]event.Event{{Kind: event.KindTool, Author: event.AuthorSubagent}}},
		{adapter.ClaudeCode, `{"hook_event_name":"UserPromptSubmit ","prompt":"made-up"}`, nil},
		{adapter.ClaudeCode, `{"hook_event_name":"userpromptsubmit","prompt":"made-up"}`, nil},
		{adapter.ClaudeCode, `{"prompt":"made-up"}`, nil},
	} {
		d, err := Decode(tc.host, []byte(tc.raw), Env{Entrypoint: "cli"})
		if err != nil || len(d.Events) != len(tc.want) {
			t.Errorf("%s: %v %+v", tc.raw, err, d.Events)
			continue
		}
		for i, e := range d.Events {
			w := tc.want[i]
			if e.Kind != w.Kind || e.Author != w.Author || e.Text != w.Text || len(e.Marks) != len(w.Marks) || e.TID != d.TID || e.Agent != d.Agent || !e.Src.Hook {
				t.Errorf("%s: %+v, want %+v", tc.raw, e, w)
			}
		}
	}
}
