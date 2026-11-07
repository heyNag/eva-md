package update

import (
	"slices"
	"testing"
	"time"

	"github.com/heyNag/eva-md/internal/threadpage"
)

// Guarantees reports each guarantee a pass broke, as in an update that
// never went through the validator: an operation under the user's tag
// citing no user prompt, the user's item dropped without the user, a step
// done without a result, a decision done or answered, a date no entry
// holds, an id that left the page, and a session older than the page that
// changed the Now. An update that keeps them all reports nothing.
func TestGuarantees(t *testing.T) {
	isolate(t)
	c := newChat("3f2a91c0-1111-4000-8000-000000000000", start)
	c.user("Ship the parser on 14 January. " + filler)
	c.tool("Bash go test ./internal/parser/... (exit 0)")
	c.assistant("The parser tests pass. " + filler)
	b := build(t, c, threadpage.Page{}, Options{})
	older := build(t, c, threadpage.Page{}, Options{AsOf: start.Add(time.Hour)})
	user, tool := (&validator{b: b}).ref(b.Entries[0]), (&validator{b: b}).ref(b.Entries[1])
	before := threadpage.Page{Now: threadpage.Now{
		Next:      []threadpage.Item{{ID: "n1", Text: "Write the parser", Tag: "stated", Refs: []string{user}}},
		Decisions: []threadpage.Item{{ID: "n2", Text: "Use Go 1.26", Tag: "stated", Refs: []string{user}}, {ID: "n3", Text: "Keep SQLite", Tag: "stated", Refs: []string{user}}},
	}}
	kept := threadpage.Update{
		Date: "2026-01-14",
		Ops: []threadpage.Op{
			{ID: "n1", Kind: "next", Action: "done", Refs: []string{tool}},
			{Kind: "next", Action: "add", Text: "Ship the parser on 2026-01-14", Tag: "stated", Refs: []string{user}},
		},
		Events: []threadpage.Event{{Kind: "finding", Text: "The parser tests pass", Tag: "observed", Refs: []string{tool}}},
	}
	op := func(o threadpage.Op) threadpage.Update { return threadpage.Update{Ops: []threadpage.Op{o}} }
	for _, tc := range []struct {
		name   string
		b      Batch
		u      threadpage.Update
		after  *threadpage.Page // nil: the page Apply makes
		broken []string
	}{
		{name: "every guarantee kept", b: b, u: kept},
		{name: "the user's tag without a user prompt", b: b, u: op(threadpage.Op{Kind: "next", Action: "add", Text: "Ship it", Tag: "stated", Refs: []string{tool}}),
			broken: []string{`add "Ship it" is tagged stated without a user prompt`}},
		{name: "the user's item dropped without the user", b: b, u: op(threadpage.Op{ID: "n2", Kind: "decision", Action: "dropped", Reason: "not needed", Refs: []string{tool}}),
			broken: []string{"n2, the user's, is dropped without the user"}},
		{name: "a step done without a result", b: b, u: op(threadpage.Op{ID: "n1", Kind: "next", Action: "done", Refs: []string{user}}),
			broken: []string{"n1 is done without a result"}},
		{name: "a decision done", b: b, u: op(threadpage.Op{ID: "n3", Kind: "decision", Action: "done", Refs: []string{tool}}),
			broken: []string{"decision n3 is done"}},
		{name: "a decision answered", b: b, u: op(threadpage.Op{ID: "n3", Kind: "decision", Action: "answered", Replacement: "yes", Refs: []string{user}}),
			broken: []string{"decision n3 is answered"}},
		{name: "a date no entry holds", b: b, u: threadpage.Update{Events: []threadpage.Event{{Kind: "finding", Text: "The freeze is on 2026-03-03", Tag: "inferred", Refs: []string{tool}}}},
			broken: []string{`the date in "The freeze is on 2026-03-03" is in no entry`}},
		{name: "an id that left the page", b: b, after: &threadpage.Page{Now: threadpage.Now{Next: before.Now.Next, Decisions: before.Now.Decisions[:1]}},
			broken: []string{"id n3 left the page"}},
		{name: "an older session that changed the Now", b: older, u: kept,
			broken: []string{"a session older than the page changed the Now"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			after, _ := threadpage.Apply(before, tc.u)
			if tc.after != nil {
				after = *tc.after
			}
			if got := Guarantees(before, after, tc.b, tc.u); !slices.Equal(got, tc.broken) {
				t.Errorf("broken %q, want %q", got, tc.broken)
			}
		})
	}
}
