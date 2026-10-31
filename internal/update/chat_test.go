package update

import (
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/heyNag/eva-md/internal/inbox"
	"github.com/heyNag/eva-md/internal/threadpage"
)

// Times in a +01:00 offset, as a chat in Paris writes them. Every value in
// these fixtures is made up.
var (
	paris = time.FixedZone("", 3600)
	start = time.Date(2026, 1, 14, 10, 0, 0, 0, paris)
)

// chat writes one session's entries as the hooks store them and reads them
// back as the job will: JSONL, decoded with physical line numbers and end
// offsets.
type chat struct {
	host, session, branch string
	at                    time.Time
	rows                  []inbox.Entry
}

func newChat(session string, at time.Time) *chat {
	return &chat{host: HostClaude, session: session, branch: "main", at: at}
}

// add appends an entry a minute after the last one and returns its line.
func (c *chat) add(role, kind, text string) int {
	c.at = c.at.Add(time.Minute)
	c.rows = append(c.rows, inbox.Entry{V: 1, TS: c.at.Format(time.RFC3339Nano), Host: c.host, Session: c.session,
		Role: role, Kind: kind, Text: text, Project: "github.com__you__eva-md", Branch: c.branch, Cwd: "projects/eva-md", Origin: "hook"})
	return len(c.rows)
}

func (c *chat) user(text string) int      { return c.add("user", "prompt", text) }
func (c *chat) assistant(text string) int { return c.add("assistant", "reply", text) }
func (c *chat) tool(text string) int      { return c.add("tool", "tool", text) }
func (c *chat) plan(text string) int      { return c.add("plan", "plan", text) }
func (c *chat) event(text string) int     { return c.add("event", "event", text) }

// jsonl is the session file.
func (c *chat) jsonl(t testing.TB) []byte {
	t.Helper()
	var b strings.Builder
	for _, e := range c.rows {
		data, err := json.Marshal(e)
		if err != nil {
			t.Fatal(err)
		}
		b.Write(data)
		b.WriteByte('\n')
	}
	return []byte(b.String())
}

// entries reads the whole session file back.
func (c *chat) entries(t testing.TB) []Entry {
	t.Helper()
	return Decode(c.jsonl(t), 0, 1)
}

// filler is ordinary chat text with no fact in it, to bring a group past
// its 600 characters.
const filler = "We read through the scheduler logs and the retry loop together; nothing in them changes the plan, and the numbers match what we saw on Monday. " +
	"The rest of the hour went on reading old notes aloud, which settled nothing new and is only here so the chat is long enough to update a page."

// build builds a batch of the whole chat for page, failing the test on an
// error.
func build(t testing.TB, c *chat, page threadpage.Page, o Options) Batch {
	t.Helper()
	if o.Branch == "" {
		o.Branch = c.branch
	}
	b, err := Build(c.entries(t), page, o)
	if err != nil {
		t.Fatalf("Build: %v", err)
	}
	return b
}

// num is the number of the one batch entry whose text holds needle.
func num(t testing.TB, b Batch, needle string) int {
	t.Helper()
	found := 0
	for i, e := range b.Entries {
		if strings.Contains(e.Text, needle) {
			if found != 0 {
				t.Fatalf("%q is in entries %d and %d", needle, found, i+1)
			}
			found = i + 1
		}
	}
	if found == 0 {
		t.Fatalf("no entry holds %q", needle)
	}
	return found
}

// encode writes an output as a model would, with empty arrays, not null.
func encode(t testing.TB, o output) []byte {
	t.Helper()
	if o.Done == nil {
		o.Done = []doneLine{}
	}
	if o.Items == nil {
		o.Items = []itemOp{}
	}
	if o.Timeline == nil {
		o.Timeline = []eventLine{}
	}
	if o.Unknowns == nil {
		o.Unknowns = []string{}
	}
	if o.Rules == nil {
		o.Rules = []ruleOp{}
	}
	for i := range o.Items {
		if o.Items[i].Refs == nil {
			o.Items[i].Refs = []int{}
		}
	}
	data, err := json.Marshal(o)
	if err != nil {
		t.Fatal(err)
	}
	return data
}

func ptr(s string) *string { return &s }

// Operations as a model writes them.
func opAdd(kind, text, tag string, refs ...int) itemOp {
	return itemOp{Kind: kind, Action: "add", Text: ptr(text), Tag: tag, Refs: refs}
}

func opKeep(id, kind, tag string) itemOp {
	return itemOp{ID: ptr(id), Kind: kind, Action: "keep", Tag: tag}
}

func opEdit(id, kind, text, tag string, refs ...int) itemOp {
	o := itemOp{ID: ptr(id), Kind: kind, Action: "edit", Tag: tag, Refs: refs}
	if text != "" {
		o.Text = ptr(text)
	}
	return o
}

func opDone(id, kind, tag string, refs ...int) itemOp {
	return itemOp{ID: ptr(id), Kind: kind, Action: "done", Tag: tag, Refs: refs}
}

func opAnswer(id, answer, tag string, refs ...int) itemOp {
	return itemOp{ID: ptr(id), Kind: "question", Action: "answered", Tag: tag, Refs: refs, Replacement: ptr(answer)}
}

func opSupersede(id, kind, replacement, tag string, refs ...int) itemOp {
	return itemOp{ID: ptr(id), Kind: kind, Action: "superseded", Tag: tag, Refs: refs, Replacement: ptr(replacement)}
}

func opDrop(id, kind, tag, reason string, refs ...int) itemOp {
	o := itemOp{ID: ptr(id), Kind: kind, Action: "dropped", Tag: tag, Refs: refs}
	if reason != "" {
		o.Reason = ptr(reason)
	}
	return o
}

func tl(kind, text, tag string, refs ...int) eventLine {
	return eventLine{Kind: kind, Text: text, Tag: tag, Refs: refs}
}

// validate runs Validate on an output, failing the test on an error.
func validate(t testing.TB, o output, b Batch, page threadpage.Page) (threadpage.Update, Report) {
	t.Helper()
	u, r, err := Validate(encode(t, o), b, page)
	if err != nil {
		t.Fatalf("Validate: %v", err)
	}
	return u, r
}

// item finds the Now item whose text holds needle, and the section it is
// in: next, open or decisions.
func item(p threadpage.Page, needle string) (threadpage.Item, string, bool) {
	n := p.Now
	for _, s := range []struct {
		list []threadpage.Item
		name string
	}{{n.Next, "next"}, {n.Open, "open"}, {n.Decisions, "decisions"}} {
		for _, it := range s.list {
			if strings.Contains(strings.ToLower(it.Text), strings.ToLower(needle)) {
				return it, s.name, true
			}
		}
	}
	return threadpage.Item{}, "", false
}
