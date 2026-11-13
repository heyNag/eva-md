package spike

import (
	"bytes"
	"fmt"
	"regexp"
	"slices"
	"strings"
	"time"
	"unicode"

	"github.com/heyNag/eva-md/internal/eval/scenario"
	"github.com/heyNag/eva-md/internal/ledger/event"
)

// Shown is one item in force that a pass shows the model, by its handle:
// an item, a next step or an open question, with its kind and its tag.
type Shown struct {
	Handle, Kind, Tag, Text string
}

// Input is a rendered pass: the bytes the model reads after the prompt,
// and each entry they show.
type Input struct {
	Bytes   []byte
	Entries []Entry
}

// Entry is one shown event: its seq as N, its session, author and kind,
// its text as shown, marks in, and, in the segment form, its segments as
// shown, segment k being Segments[k-1].
type Entry struct {
	N        int64
	Session  string
	Author   event.Author
	Kind     event.Kind
	Text     string
	Segments []string
}

// Mark is put before a line of text that starts with a bracket, so that it
// cannot pass for an entry or an item.
const Mark = "[quoted] "

// labels are the authors shown by a label of their own; any other author is
// shown by its id.
var labels = map[event.Author]string{
	event.AuthorTyped:    "user typed",
	event.AuthorChosen:   "user chose",
	event.AuthorPasted:   "user pasted",
	event.AuthorAgent:    "assistant",
	event.AuthorSubagent: "subagent",
	event.AuthorTool:     "tool",
	event.AuthorProgram:  "program",
}

// Label is how an entry by author a is labelled.
func Label(a event.Author) string {
	if l, ok := labels[a]; ok {
		return l
	}
	return string(a)
}

// handlePattern is a handle the store mints, and words that may stand as a
// shown item's kind or tag.
var (
	handlePattern = regexp.MustCompile(`^[a-z0-9][a-z0-9._-]{0,31}$`)
	wordPattern   = regexp.MustCompile(`^[a-z][a-z_.]{0,31}$`)
)

// Render renders session s for one pass in form f, over scenario.Events(s):
// the items in force, ordered by handle, then one entry per event,
// "[n] <label> <time>: <text>", n the event's seq. Controls are not shown,
// nor any event from an off control to the next on. Lines of an entry after
// its first are indented, and a line that starts with a bracket is marked.
// In the segment form an entry's text is listed as its "[n.k]" segments
// instead, one a line.
func Render(s scenario.Session, shown []Shown, f Form) (Input, error) {
	events, err := scenario.Events(s)
	if err != nil {
		return Input{}, err
	}
	return render(s.ID, events, shown, f)
}

// render renders events of session as Render does.
func render(session string, events []event.Event, shown []Shown, f Form) (Input, error) {
	if !f.Valid() {
		return Input{}, fmt.Errorf("spike: unknown form %q", f)
	}
	items, err := order(shown)
	if err != nil {
		return Input{}, err
	}
	var b bytes.Buffer
	if len(items) == 0 {
		b.WriteString("Items in force: none.\n")
	} else {
		b.WriteString("Items in force:\n")
		for _, it := range items {
			writeText(&b, fmt.Sprintf("[%s] %s %s:", it.Handle, it.Kind, it.Tag), lines(it.Text))
		}
	}
	b.WriteString("\nEntries:\n")
	var in Input
	off := false
	day := ""
	for _, e := range events {
		if e.Kind == event.KindControl {
			switch e.Op {
			case event.OpOff:
				off = true
			case event.OpOn:
				off = false
			}
			continue
		}
		if off {
			continue
		}
		at, err := time.Parse(time.RFC3339Nano, e.T)
		if err != nil {
			return Input{}, fmt.Errorf("spike: event %d has no time: %w", e.Seq, err)
		}
		when := at.Format("15:04")
		if d := at.Format(time.DateOnly); d != day {
			when, day = d+" "+when, d
		}
		head := fmt.Sprintf("[%d] %s %s:", e.Seq, Label(e.Author), when)
		text := lines(e.Text)
		entry := Entry{N: e.Seq, Session: session, Author: e.Author, Kind: e.Kind, Text: strings.Join(text, "\n")}
		if f == FormSegments {
			entry.Segments = segments(text)
			b.WriteString(head + "\n")
			for k, seg := range entry.Segments {
				fmt.Fprintf(&b, "  [%d.%d] %s\n", e.Seq, k+1, seg)
			}
		} else {
			writeText(&b, head, text)
		}
		in.Entries = append(in.Entries, entry)
	}
	in.Bytes = b.Bytes()
	return in, nil
}

// order checks shown items and orders them by handle, h2 before h10.
func order(shown []Shown) ([]Shown, error) {
	items := slices.Clone(shown)
	for i, it := range items {
		switch {
		case !handlePattern.MatchString(it.Handle):
			return nil, fmt.Errorf("spike: shown item %d has no handle", i+1)
		case !wordPattern.MatchString(it.Kind) || !wordPattern.MatchString(it.Tag):
			return nil, fmt.Errorf("spike: shown item %s needs a kind and a tag, each one word", it.Handle)
		case lines(it.Text) == nil:
			return nil, fmt.Errorf("spike: shown item %s has no text", it.Handle)
		}
	}
	slices.SortFunc(items, func(a, b Shown) int {
		if c := len(a.Handle) - len(b.Handle); c != 0 {
			return c
		}
		return strings.Compare(a.Handle, b.Handle)
	})
	for i := 1; i < len(items); i++ {
		if items[i].Handle == items[i-1].Handle {
			return nil, fmt.Errorf("spike: handle %s is shown twice", items[i].Handle)
		}
	}
	return items, nil
}

// writeText writes head and text's first line on one line, and each later
// line indented.
func writeText(b *bytes.Buffer, head string, text []string) {
	b.WriteString(head)
	for i, l := range text {
		switch {
		case i == 0:
			b.WriteString(" " + l)
		case l == "":
			b.WriteString("\n")
		default:
			b.WriteString("\n  " + l)
		}
	}
	b.WriteString("\n")
}

// lines cleans text into its lines as shown: every kind of line break is
// one, control and format characters other than a tab are dropped, white
// space at a line's end is cut, and a line that starts with a bracket after
// any white space is marked. An empty text has no line.
func lines(text string) []string {
	text = strings.NewReplacer("\r\n", "\n", "\r", "\n", " ", "\n", " ", "\n", "\u0085", "\n").Replace(text)
	text = strings.Map(func(r rune) rune {
		if r != '\n' && r != '\t' && (unicode.IsControl(r) || unicode.In(r, unicode.Cf)) {
			return -1
		}
		return r
	}, text)
	if strings.TrimSpace(text) == "" {
		return nil
	}
	out := strings.Split(text, "\n")
	for i, l := range out {
		l = strings.TrimRightFunc(l, unicode.IsSpace)
		if strings.HasPrefix(strings.TrimLeftFunc(l, unicode.IsSpace), "[") {
			l = Mark + l
		}
		out[i] = l
	}
	return out
}

// sentenceEnds are the marks that end a sentence, in the scripts eva-md
// meets.
const sentenceEnds = ".!?…。！？"

// segments splits text's lines into segments: at each line break, and
// after a sentence-ending mark that ends its line or precedes white space.
// Each segment is trimmed; an empty one is dropped.
func segments(text []string) []string {
	var out []string
	add := func(s string) {
		if s = strings.TrimSpace(s); s != "" {
			out = append(out, s)
		}
	}
	for _, l := range text {
		runes := []rune(l)
		start := 0
		for i, r := range runes {
			if strings.ContainsRune(sentenceEnds, r) && (i+1 == len(runes) || unicode.IsSpace(runes[i+1])) {
				add(string(runes[start : i+1]))
				start = i + 1
			}
		}
		add(string(runes[start:]))
	}
	return out
}
