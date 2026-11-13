package capture

import (
	"errors"
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

// Keys are matched exactly: a key in another case is not the field, so it
// can neither give nor take away an agent id.
func TestDecodeMatchesKeysExactly(t *testing.T) {
	for _, tc := range []struct {
		raw   string
		agent string
		want  event.Author
	}{
		{`{"hook_event_name":"UserPromptSubmit","prompt":"made-up","AGENT_ID":"a-made-up"}`, "", event.AuthorTyped},
		{`{"hook_event_name":"UserPromptSubmit","prompt":"made-up","agent_id":"a-made-up","Agent_Id":""}`, "a-made-up", event.AuthorSubagent},
		{`{"Hook_Event_Name":"UserPromptSubmit","prompt":"made-up"}`, "", ""},
		{`{"hook_event_name":"UserPromptSubmit","Prompt":"made-up"}`, "", ""},
		{`{"hook_event_name":"UserPromptSubmit","prompt":"made-up","agent_id":null}`, "", event.AuthorTyped},
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
		{"the wrapper before the entrypoint", wrapper, "", "", "cli", RuleVoice, ""},
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
		case tc.want == "" && tc.rule != RuleVoice:
			if len(evs) != 0 {
				t.Errorf("%s: %+v, want none", tc.name, evs)
			}
		case tc.want != "":
			if len(evs) != 1 || evs[0].Author != tc.want || evs[0].Text != tc.prompt {
				t.Errorf("%s: %+v, want one by %s", tc.name, evs, tc.want)
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
