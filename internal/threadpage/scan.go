package threadpage

import (
	"slices"
	"strings"
)

// A Scanner reads a body one line at a time, as Parse reads it, so a page
// too large to hold whole can still be searched. It is given each line in
// order, without its newline, and reports the Now items and the Timeline
// lines. It never fails: where Parse would call a body damaged, it reports
// the items and lines it can still place and skips the rest, and a "## "
// line ends a Now whose end marker is missing.
type Scanner struct {
	section string // "", "now", "timeline" or "other"
	inside  bool   // between the Now markers
	heading string // the Now heading the lines inside the markers fall under
	date    string // the Timeline group's date
	grouped bool   // a Timeline group has started
	fence   string // the fence open in a Timeline group or another section
}

// Unit is a Now item or a Timeline line, as a Scanner reports it.
type Unit struct {
	Now  bool   // a Now item; otherwise a Timeline line
	Kind string // a Now item's heading as a kind (done, next, question or decision); a Timeline line's first word, or "" for a line in another form
	Date string // a Timeline line's group date
	Line string // the line as written
	// Item is the line read as an item. For a Timeline line it holds the
	// text after "<kind> [<tag>]: ", labels included, the tag and refs, and
	// the id and time of the hidden comment; a line in another form keeps
	// its text without the bullet, and its refs.
	Item Item
}

// nowKinds names the kind of the items under each Now heading.
var nowKinds = map[string]string{doneHeading: "done", nextHeading: "next", openHeading: "question", decisionsHeading: "decision"}

// timelineKinds are the words that start a Timeline line Apply writes: an
// event's kind or a retired item's.
var timelineKinds = map[string]bool{"decision": true, "done": true, "finding": true, "change": true, "problem": true, "question": true, "answered": true, "next": true}

// Line reads the next line of the body and reports whether it is a Now item
// or a Timeline line.
func (s *Scanner) Line(l string) (Unit, bool) {
	t := strings.TrimSpace(l)
	h, heading := sectionHeading(l)
	if s.inside && !heading {
		return s.nowLine(l, t)
	}
	if heading && (s.inside || s.fence == "") {
		s.inside, s.heading, s.date, s.grouped, s.fence = false, "", "", false, ""
		switch h {
		case "## Now":
			s.section = "now"
		case "## Timeline":
			s.section = "timeline"
		default:
			s.section = "other"
		}
		return Unit{}, false
	}
	switch s.section {
	case "now":
		if t == NowStart {
			s.inside, s.heading = true, ""
		}
	case "timeline":
		if t == "" {
			break
		}
		if date, ok := groupHeading(l); ok {
			s.date, s.grouped, s.fence = date, true, ""
			break
		}
		if s.grouped {
			s.fence = NextFence(s.fence, l)
			return timelineUnit(s.date, l), true
		}
	case "other":
		s.fence = NextFence(s.fence, l)
	}
	return Unit{}, false
}

// nowLine reads a line between the Now markers, as parseNow does.
func (s *Scanner) nowLine(l, t string) (Unit, bool) {
	switch t {
	case NowEnd:
		s.inside = false
		return Unit{}, false
	case lastChatHeading, doneHeading, nextHeading, openHeading, decisionsHeading:
		s.heading = t
		return Unit{}, false
	}
	if headingLine.MatchString(t) {
		s.heading = t // an unknown heading: what follows it is no item
		return Unit{}, false
	}
	kind := nowKinds[s.heading]
	if kind == "" || t == "" {
		return Unit{}, false
	}
	return Unit{Now: true, Kind: kind, Line: l, Item: parseItem(t, s.heading == openHeading)}, true
}

// timelineUnit reads one Timeline line under the group dated date.
func timelineUnit(date, l string) Unit {
	u := Unit{Date: date, Line: l}
	rest := strings.TrimSpace(l)
	// As in parseItem, the last comment is the hidden one.
	if a := strings.LastIndex(rest, "<!--"); a >= 0 {
		if b := strings.Index(rest[a+4:], "-->"); b >= 0 {
			u.Item.ID, u.Item.At = parseMeta(rest[a+4 : a+4+b])
			rest = rest[:a] + " " + rest[a+4+b+3:]
		}
	}
	rest = strings.TrimSpace(rest)
	body, bullet := strings.CutPrefix(rest, "- ")
	if !bullet {
		body, _ = strings.CutPrefix(rest, "* ")
	}
	rest = body
	// The head ends at the first colon, as lineTail reads it.
	if head, tail, ok := strings.Cut(body, ":"); ok && bullet {
		if kind, tag, ok := timelineHead(head); ok {
			u.Kind, u.Item.Tag, rest = kind, tag, tail
		}
	}
	fields := strings.Fields(rest)
	end := len(fields)
	for end > 0 && refPattern.MatchString(fields[end-1]) {
		end--
	}
	if end < len(fields) {
		u.Item.Refs = slices.Clone(fields[end:])
	}
	u.Item.Text = strings.Join(fields[:end], " ")
	return u
}

// timelineHead reads "<kind> [<tag>]", as timelineLine writes it with one of
// the five tags, or a known kind alone, as it writes it for an item without
// a tag.
func timelineHead(head string) (kind, tag string, ok bool) {
	kind, rest, _ := strings.Cut(head, " ")
	if kind == "" || word(kind) != kind {
		return "", "", false
	}
	if rest == "" {
		return kind, "", timelineKinds[kind]
	}
	tag, open := strings.CutPrefix(rest, "[")
	tag, closed := strings.CutSuffix(tag, "]")
	if !open || !closed || !tags[tag] {
		return "", "", false
	}
	return kind, tag, true
}
