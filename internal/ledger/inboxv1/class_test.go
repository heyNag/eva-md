package inboxv1

import (
	"testing"

	"github.com/heyNag/eva-md/internal/inbox"
	"github.com/heyNag/eva-md/internal/ledger"
	"github.com/heyNag/eva-md/internal/ledger/event"
)

// A session's class comes only from what its file shows: a subagent's id
// on every entry, a program's start, an end with no start and no turn, no
// turn at all; otherwise it is unknown, and never a person's chat.
func TestSessionClass(t *testing.T) {
	ev := func(text string) inbox.Entry { return inbox.Entry{Role: "event", Kind: "event", Text: text} }
	prompt := inbox.Entry{Role: "user", Kind: "prompt", Turn: "t1", Text: "a prompt"}
	reply := inbox.Entry{Role: "assistant", Kind: "reply", Text: "a reply"}
	tool := inbox.Entry{Role: "tool", Kind: "tool", Text: "Bash ls"}
	agent := func(e inbox.Entry) inbox.Entry { e.Agent = "agent-3"; return e }
	forged := func(e inbox.Entry) inbox.Entry { e.Session = "someone-else"; return e }
	for _, c := range []struct {
		name    string
		entries []inbox.Entry
		raw     string
		want    event.Class
	}{
		{"empty", nil, "", event.ClassUnknown},
		{"only lines that do not decode", nil, "garbage\nmore garbage\n", event.ClassUnknown},
		{"only forged lines", []inbox.Entry{forged(ev("session_end"))}, "", event.ClassUnknown},
		{"an end with no start and no turn", []inbox.Entry{ev("session_end")}, "", event.ClassUnrun},
		{"an end, a compaction, no start, no turn", []inbox.Entry{ev("compact"), ev("session_end")}, "", event.ClassUnrun},
		{"a start and an end and no turn", []inbox.Entry{ev("session_start: startup"), ev("session_end")}, "", event.ClassHousekeeping},
		{"a start and no turn", []inbox.Entry{ev("session_start: startup")}, "", event.ClassHousekeeping},
		{"an end after a turn", []inbox.Entry{prompt, ev("session_end")}, "", event.ClassUnknown},
		{"an end after a pause", []inbox.Entry{ev("off_record"), ev("session_end")}, "", event.ClassUnknown},
		{"an end after a tool line", []inbox.Entry{tool, ev("session_end")}, "", event.ClassUnknown},
		{"an end after a failed stop", []inbox.Entry{ev("stop_failure: overloaded"), ev("session_end")}, "", event.ClassUnknown},
		{"an end after an interrupt", []inbox.Entry{ev("interrupt"), ev("session_end")}, "", event.ClassUnknown},
		{"an end and an event with a turn id", []inbox.Entry{{Role: "event", Kind: "event", Text: "compact", Turn: "t9"}, ev("session_end")}, "", event.ClassUnknown},
		{"every entry a subagent's", []inbox.Entry{agent(prompt), agent(tool), agent(reply)}, "", event.ClassSubagent},
		{"some entries a subagent's", []inbox.Entry{agent(prompt), reply}, "", event.ClassUnknown},
		{"a program's start", []inbox.Entry{ev("session_start: startup"), ev("host_prompt"), reply, ev("host_prompt")}, "", event.ClassProgram},
		{"a program's start, then a prompt filed as the user's", []inbox.Entry{ev("host_prompt"), reply, prompt}, "", event.ClassUnknown},
		{"a prompt filed as the user's, then a host prompt", []inbox.Entry{prompt, ev("host_prompt")}, "", event.ClassUnknown},
		{"a subagent's host prompt first", []inbox.Entry{agent(ev("host_prompt")), prompt}, "", event.ClassUnknown},
		{"a voice hand-over", []inbox.Entry{{Role: "user", Kind: "prompt", Turn: "t1", Text: wrapperPrompt}, reply}, "", event.ClassUnknown},
	} {
		t.Run(c.name, func(t *testing.T) {
			root := newInbox(t)
			data := ""
			for _, e := range c.entries {
				e.V, e.Host, e.Project, e.Origin = 1, "codex", "general", "hook"
				if e.Session == "" {
					e.Session = "class-1"
				}
				data += line(t, e)
			}
			appendRaw(t, root, "general", "codex-class-1.jsonl", data+c.raw)
			s, _ := checkV1(t, root, ledger.Key{Host: "codex", Session: "class-1", Folder: "general", File: "codex-class-1.jsonl"})
			if s.Class != c.want {
				t.Errorf("class %s, want %s", s.Class, c.want)
			}
		})
	}
}

// A prompt, reply, tool or task line that v1's walker did not yield is
// withheld, even where the reader's own reading would return it.
func TestUnconfirmedContentIsWithheld(t *testing.T) {
	r := reading{key: ledger.Key{Host: "codex", Session: "s-1", Folder: "general", File: "codex-s-1.jsonl"}, keys: map[string]int64{}}
	for i, pair := range v1Pairs[:4] {
		e := inbox.Entry{V: 1, Host: "codex", Session: "s-1", Project: "general", Role: pair[0], Kind: pair[1], Text: "tok-unconfirmed", Turn: "t1"}
		if ev := r.line(int64(2*i+1), e, false); ev.Why != event.WhyUnconfirmed || ev.Text != "" || ev.TID != "" {
			t.Errorf("%s/%s unconfirmed reads as %+v", pair[0], pair[1], ev)
		}
		if ev := r.line(int64(2*i+2), e, true); ev.Text != "tok-unconfirmed" {
			t.Errorf("%s/%s confirmed reads as %+v", pair[0], pair[1], ev)
		}
	}
}

// A line of a role and kind v1 never writes is returned as v1's walker
// returns it, by no author the reader can name, when the walker yields its
// kind; any other is withheld.
func TestUnknownRoleAndKind(t *testing.T) {
	root := newInbox(t)
	data := ""
	for _, e := range []inbox.Entry{
		{Role: "user", Kind: "reply", Text: "odd one"},
		{Role: "event", Kind: "prompt", Text: "<realtime_delegation> odd two"},
		{Role: "", Kind: "tool", Text: "odd three", Agent: "agent-1"},
		{Role: "system", Kind: "note", Text: "tok-odd four"},
		{Role: "user", Kind: "host_summary", Text: "tok-odd five"},
		{Role: "event", Kind: "", Text: "tok-odd six"},
	} {
		e.V, e.Host, e.Session, e.Project = 1, "codex", "odd-1", "general"
		data += line(t, e)
	}
	appendRaw(t, root, "general", "codex-odd-1.jsonl", data)
	_, events := checkV1(t, root, ledger.Key{Host: "codex", Session: "odd-1", Folder: "general", File: "codex-odd-1.jsonl"})
	kinds := []event.Kind{event.KindReply, event.KindPrompt, event.KindTool}
	for _, e := range events {
		if e.Seq <= 3 {
			if e.Kind != kinds[e.Seq-1] || e.Author != event.AuthorUnknown || e.Text == "" {
				t.Errorf("line %d reads as %+v", e.Seq, e)
			}
		} else if e.Why != event.WhyUnknownKind || e.Text != "" {
			t.Errorf("line %d reads as %+v", e.Seq, e)
		}
	}
	if !events[1].Has(event.MarkWrapper) {
		t.Error("a prompt of no known role that begins with a voice hand-over is not marked")
	}
}
