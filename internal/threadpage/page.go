// Package threadpage models the body of a thread page: the Now section each
// update rewrites, the Timeline it only appends to, and any section a person
// adds. Its functions are pure, strings in and strings out; pages reach the
// disk only through internal/okfadapter.
//
// Render writes a body in this shape:
//
//	# eva-md · main
//
//	## Now
//	<!-- eva:now:start -->
//	The renderer is done; the validator is next.
//
//	### Last chat
//	2026-01-15 · claude-code/claude-opus-5-5 · Built the renderer.
//
//	### Done
//	- Wrote the renderer [observed] s:3f2a91c0#20
//
//	### Next
//	- Write the validator [stated] s:3f2a91c0#14 <!-- i:n4 t:2026-01-14T10:00:00+01:00 -->
//
//	### Open questions
//	- Proposed (assistant): Cache parsed pages? [suggested] s:3f2a91c0#15 <!-- i:n5 -->
//	- eva-md: decision over cap, kept: Keep one lock [stated] s:3f2a91c0#16 <!-- i:n6 -->
//	- Which lock, flock or a pid file? [stated] s:3f2a91c0#17 <!-- i:n7 -->
//	- Index threads by branch? [accepted] s:3f2a91c0#18 <!-- i:n8 -->
//
//	### Decisions in force
//	- Use JSONL for the inbox [stated] s:3f2a91c0#14 <!-- i:n3 t:2026-01-14T10:00:00+01:00 -->
//	<!-- eva:now:end -->
//
//	## Timeline
//	### 2026-01-14
//	- decision [stated]: Use JSONL for the inbox s:3f2a91c0#14
//	- done [stated]: Write the parser s:3f2a91c0#9 <!-- i:n2 -->
//
//	## Notes
//	A section a person added, kept byte for byte.
//
// Render's output starts with a blank line and ends with one newline, which
// is how okf reads a body back, so a saved page reads back byte for byte.
// okf drops white space at the end of a body, so Render drops it too.
// Empty Now sections are left out, and other sections follow the Timeline,
// even one a person wrote above the Now, whose text is kept.
//
// An item is one line: "- ", an optional label, the text, the tag in
// brackets, the refs, and a hidden comment with the id (i:) and the cited user prompt's
// time (t:, RFC 3339). Empty parts are left out, so a Done item usually ends
// at its refs. The label is "eva-md: <code>: " on a line eva-md wrote
// itself, which keeps its stored tag; "Proposed (assistant): " on an Open
// question tagged suggested; or "Unattributed: " on one tagged inferred,
// which no citation ties to the user or the assistant, such as a decision
// that cited no user prompt. Other items have none.
//
// Every Timeline line eva-md appends reads "- <word> [<tag>]: <text> <refs>".
// For an event the word is its kind. A retired item keeps its tag, refs and
// id comment, so an id is never used twice:
//
//	### 2026-01-14
//	- done [stated]: <text> <refs> <!-- i:n2 -->
//	- answered [suggested]: <question> → <answer> [stated] <refs> <!-- i:n5 -->
//	- decision [stated]: ~~<old>~~ → <new> (superseded 2026-01-14) <refs> <!-- i:n3 -->
//	- next [inferred]: ~~<text>~~ (dropped: <reason>) <refs> <!-- i:n9 -->
//	- question [suggested]: <text> (moved: cap) <refs> <!-- i:n10 -->
//
// A superseded, dropped or moved line starts with the item's kind (next,
// question or decision, which a decision kept under Open questions past the
// cap still is). An answer or replacement shows its own tag only when it
// differs from the item's.
//
// Free text (the title, summary, last chat, item and Timeline text, reasons,
// codes and Timeline dates) goes through one escaper before it is written:
// control and format characters are removed, HTML comment delimiters and
// fence runs are broken, bracketed speaker labels and tags become
// parenthesized, markdown links are broken, and a line that would read as a
// rule (---), a heading, a frontmatter key okf checks (docs/design.md
// Validation 8) or eva-md's own labels is escaped. Every text but the summary
// is collapsed to one line.
package threadpage

import (
	"errors"
	"fmt"
	"slices"
	"strings"
	"time"
	"unicode"
)

// The markers around the Now, and the Now text setup writes into a new page.
const (
	NowStart    = "<!-- eva:now:start -->"
	NowEnd      = "<!-- eva:now:end -->"
	Placeholder = "No chats captured yet."
)

// Caps, in characters for text and in items for sections.
const (
	SummaryCap  = 600
	TextCap     = 300 // an item, a Timeline text, a reason or a replacement
	LastChatCap = 400
	DoneCap     = 8
	NextCap     = 10
	OpenCap     = 10
	DecisionCap = 40
)

// OverCapCode marks a decision added past DecisionCap. It is kept under Open
// questions as a line eva-md wrote, with its own tag, and is never moved.
const OverCapCode = "decision over cap, kept"

// ErrDamaged reports a body that can't be read back safely. eva-md never
// writes such a page.
var ErrDamaged = errors.New("threadpage: page damaged")

// Item is one line of the Now.
type Item struct {
	ID   string    // "n3"; empty for Done items, which are replaced each pass
	Text string    // one escaped line
	Tag  string    // stated, accepted, suggested, observed or inferred
	Refs []string  // "s:3f2a91c0#14"
	At   time.Time // the cited user prompt's time, used for freshness; zero when none
	Code string    // set for a line eva-md wrote itself, e.g. OverCapCode
}

// Now is the section between the markers.
type Now struct {
	Summary, LastChat           string
	Done, Next, Open, Decisions []Item
}

// Group is one dated group of the Timeline.
type Group struct {
	Date  string   // YYYY-MM-DD
	Lines []string // rendered Timeline lines, never rewritten
}

// Page is a thread page's body.
type Page struct {
	Title    string // the "# " heading
	Now      Now
	Timeline []Group
	Other    []string // any other "## " section, verbatim, in order
}

const (
	lastChatHeading  = "### Last chat"
	doneHeading      = "### Done"
	nextHeading      = "### Next"
	openHeading      = "### Open questions"
	decisionsHeading = "### Decisions in force"

	codePrefix         = "eva-md: "
	proposedPrefix     = "Proposed (assistant): "
	unattributedPrefix = "Unattributed: "
)

// tags are the speaker tags an item can carry.
var tags = map[string]bool{"stated": true, "accepted": true, "suggested": true, "observed": true, "inferred": true}

// Render writes p's body. Free text is escaped and capped on the way out;
// Timeline lines and other sections are written as they are, except for
// white space at the end of the body. Parse(Render(p)) equals p for a page
// whose text is already escaped, such as one Parse read from a saved page
// and what Apply makes of it (Apply leaves the title to its caller), and
// Render(Parse(b)) equals b for any body Render wrote.
func Render(p Page) string {
	var parts []string
	if t := cleanLine(p.Title, 0); t != "" {
		parts = append(parts, "# "+t)
	}
	parts = append(parts, renderNow(p.Now), renderTimeline(p.Timeline))
	parts = append(parts, p.Other...)
	return "\n" + strings.TrimRightFunc(strings.Join(parts, "\n\n"), unicode.IsSpace) + "\n"
}

func renderNow(n Now) string {
	var blocks []string
	if s := cleanText(n.Summary, SummaryCap); s != "" {
		blocks = append(blocks, s)
	}
	if l := cleanLine(n.LastChat, LastChatCap); l != "" {
		blocks = append(blocks, lastChatHeading+"\n"+l)
	}
	for _, s := range []struct {
		heading string
		items   []Item
	}{{doneHeading, n.Done}, {nextHeading, n.Next}, {openHeading, n.Open}, {decisionsHeading, n.Decisions}} {
		if len(s.items) == 0 {
			continue
		}
		lines := []string{s.heading}
		for _, it := range s.items {
			lines = append(lines, itemLine(it, s.heading == openHeading))
		}
		blocks = append(blocks, strings.Join(lines, "\n"))
	}
	inner := ""
	if len(blocks) > 0 {
		inner = strings.Join(blocks, "\n\n") + "\n"
	}
	return "## Now\n" + NowStart + "\n" + inner + NowEnd
}

func renderTimeline(groups []Group) string {
	var b strings.Builder
	b.WriteString("## Timeline")
	for i, g := range groups {
		b.WriteString("\n")
		if i > 0 {
			b.WriteString("\n")
		}
		b.WriteString(strings.TrimSpace("### " + cleanLine(g.Date, 0)))
		for _, l := range g.Lines {
			b.WriteString("\n" + l)
		}
	}
	return b.String()
}

// itemLine writes one Now item; open is set under Open questions.
func itemLine(it Item, open bool) string {
	parts := []string{label(it, open), tagToken(it.Tag)}
	parts = append(parts, cleanRefs(it.Refs)...)
	parts = append(parts, comment(it.ID, it.At))
	if line := joinNonEmpty(" ", parts...); line != "" {
		return "- " + line
	}
	return "-"
}

// label returns an item's text with its label: eva-md's own for a line it
// wrote, and under Open questions the assistant's for a suggestion or
// "Unattributed" for an inference.
func label(it Item, open bool) string {
	text := cleanLine(it.Text, TextCap)
	if code := cleanCode(it.Code); code != "" {
		return joinNonEmpty(": ", codePrefix+code, text)
	}
	switch {
	case open && it.Tag == "suggested":
		return joinNonEmpty(" ", strings.TrimSpace(proposedPrefix), text)
	case open && it.Tag == "inferred":
		return joinNonEmpty(" ", strings.TrimSpace(unattributedPrefix), text)
	}
	return text
}

// tagToken writes a tag in brackets. A tag other than the five keeps only
// letters, digits, '-' and '_', so it can't break the line.
func tagToken(tag string) string {
	if !tags[tag] {
		tag = word(tag)
	}
	if tag == "" {
		return ""
	}
	return "[" + tag + "]"
}

// comment writes an item's hidden id and time, or nothing when both are empty.
func comment(id string, at time.Time) string {
	var parts []string
	if id = token(id); id != "" {
		parts = append(parts, "i:"+id)
	}
	if !at.IsZero() {
		if t := canonical(at); t.Year() >= 0 && t.Year() <= 9999 {
			parts = append(parts, "t:"+t.Format(time.RFC3339Nano))
		}
	}
	if len(parts) == 0 {
		return ""
	}
	return "<!-- " + strings.Join(parts, " ") + " -->"
}

// canonical drops t's monotonic reading and keeps only its offset, so equal
// stamps compare equal after a round trip through the page.
func canonical(t time.Time) time.Time {
	if t.IsZero() {
		return time.Time{}
	}
	t = t.Round(0)
	if _, off := t.Zone(); off != 0 {
		return t.In(time.FixedZone("", off))
	}
	return t.UTC()
}

// Parse reads a body in the shape Render writes. Inside the markers, which
// each update rewrites, it is lenient: an item line in another form is still
// read as an item. Elsewhere it is strict, so no text is ever lost: missing
// or repeated markers, a marker outside the "## Now" section or sharing its
// line, text outside the markers in that section, an unknown heading inside
// them, text before the first section other than the title, Timeline lines
// before the first date, and a repeated Now or Timeline section return an
// error wrapping ErrDamaged. Timeline lines, but not blank ones, and other
// sections are kept byte for byte. A "## " line inside a fenced block
// doesn't start a section, while a "### " line in the Timeline always starts
// a group and ends any fence, so a fence a person leaves open can't swallow
// the groups after it.
func Parse(body string) (Page, error) {
	if strings.Count(body, NowStart) != 1 || strings.Count(body, NowEnd) != 1 {
		return Page{}, damaged("the Now markers are missing or repeated")
	}
	lines := strings.Split(body, "\n")
	start, end := -1, -1
	for i, l := range lines {
		switch strings.TrimSpace(l) {
		case NowStart:
			start = i
		case NowEnd:
			end = i
		}
	}
	switch {
	case start < 0 || end < 0:
		return Page{}, damaged("a Now marker shares its line with other text")
	case end < start:
		return Page{}, damaged("the Now markers are out of order")
	}
	var r reader
	for i := 0; i < len(lines); i++ {
		if i == start {
			if r.section != "now" {
				return Page{}, damaged("the Now markers are outside the ## Now section")
			}
			now, err := parseNow(lines[start+1 : end])
			if err != nil {
				return Page{}, err
			}
			r.page.Now, i = now, end
			continue
		}
		if err := r.line(lines[i]); err != nil {
			return Page{}, err
		}
	}
	r.flush()
	return r.page, nil
}

func damaged(why string) error { return fmt.Errorf("%w: %s", ErrDamaged, why) }

// reader holds Parse's state outside the markers.
type reader struct {
	page          Page
	section       string   // "" before the first section, then now, timeline or other
	title         bool     // a title line was read
	other         []string // the other section being read
	fence         string   // the fence open in that section or Timeline group, if any
	sawNow, sawTL bool
}

func (r *reader) line(l string) error {
	if h, ok := sectionHeading(l); ok && r.fence == "" {
		r.flush()
		switch h {
		case "## Now":
			if r.sawNow {
				return damaged("the ## Now section is repeated")
			}
			r.sawNow, r.section = true, "now"
		case "## Timeline":
			if r.sawTL {
				return damaged("the ## Timeline section is repeated")
			}
			r.sawTL, r.section = true, "timeline"
		default:
			r.section, r.other = "other", []string{l}
		}
		return nil
	}
	blank := strings.TrimSpace(l) == ""
	switch r.section {
	case "":
		if blank {
			return nil
		}
		if t, ok := titleLine(l); ok && !r.title {
			r.page.Title, r.title = t, true
			return nil
		}
		return damaged("text before the first section")
	case "now":
		if !blank {
			return damaged("text outside the Now markers")
		}
	case "timeline":
		if blank {
			return nil
		}
		if date, ok := groupHeading(l); ok {
			r.page.Timeline = append(r.page.Timeline, Group{Date: date})
			r.fence = ""
			return nil
		}
		n := len(r.page.Timeline)
		if n == 0 {
			return damaged("Timeline lines before the first date")
		}
		r.page.Timeline[n-1].Lines = append(r.page.Timeline[n-1].Lines, l)
		r.fence = NextFence(r.fence, l)
	case "other":
		r.other = append(r.other, l)
		r.fence = NextFence(r.fence, l)
	}
	return nil
}

// flush ends the other section being read, without its trailing blank lines.
func (r *reader) flush() {
	if r.other != nil {
		r.page.Other = append(r.page.Other, strings.Join(trimBlank(r.other), "\n"))
	}
	r.other, r.fence = nil, ""
}

func sectionHeading(l string) (string, bool) {
	t := strings.TrimRight(l, " \t\r")
	return t, t == "##" || strings.HasPrefix(t, "## ")
}

func titleLine(l string) (string, bool) {
	t := strings.TrimRight(l, " \t\r")
	if t != "#" && !strings.HasPrefix(t, "# ") {
		return "", false
	}
	return strings.TrimSpace(t[1:]), true
}

func groupHeading(l string) (string, bool) {
	t := strings.TrimRight(l, " \t\r")
	if t != "###" && !strings.HasPrefix(t, "### ") {
		return "", false
	}
	return strings.TrimSpace(t[3:]), true
}

// NextFence tracks fenced blocks: it returns the fence open after line l,
// given the fence open before it. eva-md show uses it to end a section
// where Parse does.
func NextFence(open, l string) string {
	t := strings.TrimLeft(l, " ")
	if len(l)-len(t) > 3 || len(t) < 3 || (t[0] != '`' && t[0] != '~') {
		return open
	}
	n := 0
	for n < len(t) && t[n] == t[0] {
		n++
	}
	switch {
	case n < 3:
		return open
	case open == "":
		return t[:n]
	case t[0] == open[0] && n >= len(open) && strings.TrimSpace(t[n:]) == "":
		return ""
	}
	return open
}

// parseNow reads the lines between the markers.
func parseNow(lines []string) (Now, error) {
	var n Now
	var summary, last []string
	section := ""
	for _, l := range lines {
		t := strings.TrimSpace(l)
		switch t {
		case lastChatHeading, doneHeading, nextHeading, openHeading, decisionsHeading:
			section = t
			continue
		}
		if headingLine.MatchString(t) {
			return Now{}, damaged("an unknown heading inside the Now markers")
		}
		switch {
		case section == "":
			summary = append(summary, l)
		case t == "":
		case section == lastChatHeading:
			last = append(last, t)
		case section == doneHeading:
			n.Done = append(n.Done, parseItem(t, false))
		case section == nextHeading:
			n.Next = append(n.Next, parseItem(t, false))
		case section == openHeading:
			n.Open = append(n.Open, parseItem(t, true))
		default:
			n.Decisions = append(n.Decisions, parseItem(t, false))
		}
	}
	n.Summary = strings.Join(trimBlank(summary), "\n")
	n.LastChat = strings.Join(last, " ")
	return n, nil
}

// parseItem reads one item line, as itemLine writes it.
func parseItem(line string, open bool) Item {
	var it Item
	rest := line
	if rest == "-" {
		return it
	}
	if strings.HasPrefix(rest, "- ") || strings.HasPrefix(rest, "* ") {
		rest = rest[2:]
	}
	// Free text never holds a comment delimiter, so the last comment is the
	// hidden one.
	if a := strings.LastIndex(rest, "<!--"); a >= 0 {
		if b := strings.Index(rest[a+4:], "-->"); b >= 0 {
			it.ID, it.At = parseMeta(rest[a+4 : a+4+b])
			rest = rest[:a] + " " + rest[a+4+b+3:]
		}
	}
	fields := strings.Fields(rest)
	end := len(fields)
	for i := len(fields) - 1; i >= 0; i-- {
		if t := strings.TrimSuffix(strings.TrimPrefix(fields[i], "["), "]"); len(t) == len(fields[i])-2 && tags[t] {
			it.Tag, end = t, i
			break
		}
	}
	if it.Tag != "" {
		it.Refs = slices.Clone(fields[end+1:])
	} else {
		for end > 0 && refPattern.MatchString(fields[end-1]) {
			end--
		}
		it.Refs = slices.Clone(fields[end:])
	}
	if len(it.Refs) == 0 {
		it.Refs = nil
	}
	text := strings.Join(fields[:end], " ")
	switch {
	case strings.HasPrefix(text, codePrefix):
		it.Code, it.Text, _ = strings.Cut(text[len(codePrefix):], ": ")
	case open && (text == strings.TrimSpace(proposedPrefix) || text == strings.TrimSpace(unattributedPrefix)):
	case open && strings.HasPrefix(text, proposedPrefix):
		it.Text = text[len(proposedPrefix):]
	case open && strings.HasPrefix(text, unattributedPrefix):
		it.Text = text[len(unattributedPrefix):]
	default:
		it.Text = text
	}
	return it
}

// parseMeta reads the id and time from a hidden comment's inside.
func parseMeta(s string) (id string, at time.Time) {
	for _, f := range strings.Fields(s) {
		if v, ok := strings.CutPrefix(f, "i:"); ok {
			id = v
		} else if v, ok := strings.CutPrefix(f, "t:"); ok {
			if t, err := time.Parse(time.RFC3339Nano, v); err == nil {
				at = canonical(t)
			}
		}
	}
	return id, at
}

// trimBlank drops blank lines at both ends.
func trimBlank(lines []string) []string {
	for len(lines) > 0 && strings.TrimSpace(lines[0]) == "" {
		lines = lines[1:]
	}
	for len(lines) > 0 && strings.TrimSpace(lines[len(lines)-1]) == "" {
		lines = lines[:len(lines)-1]
	}
	return lines
}

func joinNonEmpty(sep string, parts ...string) string {
	var kept []string
	for _, p := range parts {
		if p != "" {
			kept = append(kept, p)
		}
	}
	return strings.Join(kept, sep)
}
