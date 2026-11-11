package scenario

import (
	"errors"
	"fmt"
	"time"

	"github.com/heyNag/eva-md/internal/inbox"
	"github.com/heyNag/eva-md/internal/ledger/event"
)

// The project and folder every rendered session is in, as v1's accuracy
// suite stores its chats. Every value is made up.
const (
	project = "github.com__you__eva-md"
	cwd     = "projects/eva-md"
)

// filler is ordinary chat text with no fact in it, which brings a v1 group
// past its 600 characters.
const filler = "We read through the scheduler logs and the retry loop together; nothing in them changes the plan, and the numbers match what we saw on Monday. " +
	"The rest of the hour went on reading old notes aloud, which settled nothing new and is only here so the chat is long enough to update a page."

// fillerExchange is the exchange both renderings end a session with, a
// minute and two minutes after its last turn. It plants nothing, and
// Locate finds no turn at its lines.
var fillerExchange = []Turn{
	{Author: event.AuthorTyped, Kind: event.KindPrompt, Text: "Thanks, that is all for now. " + filler},
	{Author: event.AuthorAgent, Kind: event.KindReply, Text: "Glad to help. " + filler},
}

// rendered is the session's turns with the filler exchange after them.
func rendered(s Session) ([]Turn, error) {
	if len(s.Turns) == 0 {
		return nil, fmt.Errorf("scenario: session %s has no turn", s.ID)
	}
	turns := append([]Turn(nil), s.Turns...)
	last := s.Turns[len(s.Turns)-1].At
	for i, t := range fillerExchange {
		t.At = last.Add(time.Duration(i+1) * time.Minute)
		turns = append(turns, t)
	}
	for i, t := range turns {
		if t.Author == event.AuthorSpoken || t.Author == event.AuthorFront {
			return nil, fmt.Errorf("scenario: %s#%d: a turn by %s has no rendering yet: it waits on HF-00's recorded wrapper", s.ID, i+1, t.Author)
		}
	}
	return turns, nil
}

// V1Entries renders a session as the inbox entries host's v1 hooks would
// store, filler exchange included, line n being the session's turn n. A
// prompt a program sent is v1's host_prompt event, which keeps no text, and
// a control its off_record or on_record event. A turn v1 keeps no entry
// for, such as a dialog or a plan decision, is an error, and so is a turn
// by a spoken or front-model author.
func V1Entries(s Session, host string) ([]inbox.Entry, error) {
	if host != "claude-code" && host != "codex" {
		return nil, fmt.Errorf("scenario: v1 captures no host %q", host)
	}
	turns, err := rendered(s)
	if err != nil {
		return nil, err
	}
	entries := make([]inbox.Entry, len(turns))
	for i, t := range turns {
		role, kind, text, err := v1Row(t)
		if err != nil {
			return nil, fmt.Errorf("scenario: %s#%d: %w", s.ID, i+1, err)
		}
		entries[i] = inbox.Entry{V: 1, TS: t.At.Format(time.RFC3339Nano), Host: host, Session: s.ID, Role: role, Kind: kind,
			Agent: t.Agent, Text: text, Project: project, Branch: s.Branch, Cwd: cwd, Origin: "hook"}
	}
	return entries, nil
}

// v1Row is the role, kind and text of the entry v1's hooks store for t.
func v1Row(t Turn) (role, kind, text string, err error) {
	switch {
	case t.Kind == event.KindPrompt && t.Author == event.AuthorProgram:
		if t.Text != "" {
			return "", "", "", errors.New("v1 keeps no text of a program's prompt")
		}
		return "event", "event", "host_prompt", nil
	case t.Kind == event.KindPrompt && (t.Author == event.AuthorTyped || t.Author == event.AuthorPasted || t.Author == event.AuthorV1User):
		return "user", "prompt", t.Text, nil
	case t.Kind == event.KindReply && (t.Author == event.AuthorAgent || t.Author == event.AuthorSubagent):
		return "assistant", "reply", t.Text, nil
	case t.Kind == event.KindTool && t.Author == event.AuthorTool:
		return "tool", "tool", t.Text, nil
	case t.Kind == event.KindControl && t.Text == "" && t.Op == event.OpOff:
		return "event", "event", "off_record", nil
	case t.Kind == event.KindControl && t.Text == "" && t.Op == event.OpOn:
		return "event", "event", "on_record", nil
	case t.Kind == event.KindDialog || t.Kind == event.KindPlanDecision:
		return "", "", "", fmt.Errorf("v1 keeps no %s entry", t.Kind)
	}
	return "", "", "", fmt.Errorf("v1 has no entry for a %s by %s", t.Kind, t.Author)
}

// Events renders a session as the record's events, filler exchange
// included: seq n is the session's turn n, each author as the turn says,
// each prompt opening a turn, and each event keyed by its turn, kind and
// place. A turn by a spoken or front-model author is an error.
func Events(s Session) ([]event.Event, error) {
	turns, err := rendered(s)
	if err != nil {
		return nil, err
	}
	events := make([]event.Event, len(turns))
	ordinal := 0
	for i, t := range turns {
		seq := int64(i + 1)
		if t.Kind == event.KindPrompt {
			ordinal++
		}
		events[i] = event.Event{Seq: seq, T: t.At.Format(time.RFC3339Nano), Turn: ordinal, Kind: t.Kind, Author: t.Author, Text: t.Text,
			Agent: t.Agent, Op: t.Op, Src: event.Source{Hook: true, Key: fmt.Sprintf("scenario:%d:%s:%d", ordinal, t.Kind, seq)},
			Ctx: &event.Context{Project: project, Branch: s.Branch, Cwd: cwd}}
		if err := events[i].Validate(); err != nil {
			return nil, fmt.Errorf("scenario: %s#%d: %w", s.ID, seq, err)
		}
	}
	return events, nil
}
