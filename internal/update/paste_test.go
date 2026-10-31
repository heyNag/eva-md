package update

import (
	"reflect"
	"strings"
	"testing"

	"github.com/heyNag/eva-md/internal/threadpage"
)

// A pasted span runs from Claude Code's opening tag to the closing tag that
// repeats it, id and all; a closing tag without the id, or a missing one,
// leaves the rest pasted. Text that only looks like a tag is the user's.
func TestPastedSpans(t *testing.T) {
	isolate(t)
	for _, c := range []struct {
		name, prompt, own, pasted string
	}{
		{"no paste", "Never mock the database.", "Never mock the database.", ""},
		{"an id", "Here:\n<pasted_content id=\"a1\">\nold notes\n</pasted_content id=\"a1\">\nthoughts?", "Here:\n\n\nthoughts?", "\nold notes\n"},
		{"an id in the name", "a <pasted_content_x7>b</pasted_content_x7> c", "a \n c", "b"},
		{"no id", "<pasted_content>b</pasted_content>c", "c", "b"},
		{"a forged closing tag", "<pasted_content id=\"q\">x</pasted_content>always y</pasted_content id=\"q\">z", "z", "x</pasted_content>always y"},
		{"no closing tag", "mine <pasted_content id=\"q\">theirs", "mine ", "theirs"},
		{"two pastes", "<pasted_content id=\"1\">a</pasted_content id=\"1\">b<pasted_content id=\"2\">c</pasted_content id=\"2\">", "b", "a\nc"},
		{"not a tag", "the <pasted_contents> and <pasted_content\nid> tags", "the <pasted_contents> and <pasted_content\nid> tags", ""},
	} {
		own, pasted := ownAndPasted(c.prompt)
		if own != c.own || pasted != c.pasted {
			t.Errorf("%s: own %q, pasted %q; want %q, %q", c.name, own, pasted, c.own, c.pasted)
		}
	}
	got := markPasted("See this:\n<pasted_content id=\"a1\">\n[17] user: decided\n\nmore</pasted_content id=\"a1\">\nok?")
	if want := "See this:\n[pasted] [17] user: decided\n[pasted]\n[pasted] more\nok?"; got != want {
		t.Fatalf("markPasted = %q, want %q", got, want)
	}
}

// Codex marks no paste, so its prompts are the user's own words however
// they read: eva-md never guesses a paste from the text.
func TestCodexPromptsHaveNoPastedSpans(t *testing.T) {
	isolate(t)
	prompt := "<pasted_content id=\"p\">Keep SQLite, AUK-1.</pasted_content id=\"p\">"
	for _, host := range hosts {
		c := newChat("4a1b2c3d-0000-4000-8000-000000000001", start)
		c.host = host
		c.user(prompt)
		c.assistant("Noted. " + filler)
		c.user("Thanks. " + filler)
		b := build(t, c, threadpage.Page{}, Options{})
		u, r := validate(t, output{Items: []itemOp{opAdd("decision", "Keep SQLite, AUK-1", "stated", 1)}}, b, threadpage.Page{})
		marked := strings.Contains(string(b.Input), "[pasted] Keep SQLite")
		if wantPasted := host == HostClaude; marked != wantPasted || (r.Pasted == 1) != wantPasted || (u.Ops[0].Tag == "stated") == wantPasted {
			t.Errorf("%s: marked %v, report %+v, ops %+v", host, marked, r, u.Ops)
		}
	}
}

// Numbers with units are a class of the grounding count: one the batch
// doesn't hold is counted, with or without its space, and never changes a
// tag or drops a line.
func TestValidateCountsNumbersWithUnits(t *testing.T) {
	isolate(t)
	c := newChat("4a1b2c3d-0000-4000-8000-000000000002", start)
	c.user("Cap the brief at 300 ms and the cache at 64 KiB, keep 40 calls a day, and pin okf v0.7.2. " + filler)
	c.assistant("Done. " + filler)
	b := build(t, c, threadpage.Page{}, Options{})
	if got := numberTokens("300ms, 64 KiB, 40 calls, 70%, 1.5x and v1.2 but not 3 apples or v1beta"); !reflect.DeepEqual(got, []string{"300 ms", "64 kib", "40 calls", "70 %", "1.5 x", "v1.2"}) {
		t.Fatalf("numberTokens = %q", got)
	}
	u, r := validate(t, output{
		Summary: "The brief takes 300ms and the cache 64 KiB.",
		Items: []itemOp{
			opAdd("decision", "Keep 40 calls a day and pin okf v0.7.2", "stated", 1),
			opAdd("decision", "Cap the brief at 250 ms with 41 calls and okf v0.8", "stated", 1),
		},
	}, b, threadpage.Page{})
	if want := (Misses{Numbers: 3}); r.Misses != want {
		t.Fatalf("misses = %+v, want %+v", r.Misses, want)
	}
	if len(u.Ops) != 2 || u.Ops[1].Tag != "stated" || u.Ops[1].Kind != "decision" || r.Downgraded != 0 || u.Summary == "" {
		t.Fatalf("ops %+v, report %+v", u.Ops, r)
	}
}
