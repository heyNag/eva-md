package codec

import (
	"fmt"
	"strings"
)

// section is a "## " line outside fenced code, and the lines under it up
// to the next.
type section struct {
	heading string // the line's text after "## "
	at      int    // the index of its line
}

// readBody finds the body's sections.
func (d *Doc) readBody() {
	fence := ""
	for i := d.close + 1; i < len(d.lines); i++ {
		l := text(d.lines[i])
		switch {
		case fence != "":
			if closesFence(l, fence) {
				fence = ""
			}
		case opensFence(l) != "":
			fence = opensFence(l)
		case strings.HasPrefix(l, "## "):
			d.sections = append(d.sections, section{heading: l[3:], at: i})
		}
	}
}

// opensFence returns the run of backticks or tildes that opens a fenced
// code block on l, or "": up to three spaces, then three or more of one
// of them, and for backticks no backtick after.
func opensFence(l string) string {
	s := strings.TrimPrefix(strings.TrimPrefix(strings.TrimPrefix(l, " "), " "), " ")
	if s == "" || (s[0] != '`' && s[0] != '~') {
		return ""
	}
	n := len(s) - len(strings.TrimLeft(s, s[:1]))
	if n < 3 || (s[0] == '`' && strings.Contains(s[n:], "`")) {
		return ""
	}
	return s[:n]
}

// closesFence reports whether l closes the fence f: up to three spaces, at
// least as many of its character, then only spaces or tabs.
func closesFence(l, f string) bool {
	s := strings.TrimPrefix(strings.TrimPrefix(strings.TrimPrefix(l, " "), " "), " ")
	rest := strings.TrimLeft(s, f[:1])
	return len(s)-len(rest) >= len(f) && strings.Trim(rest, " \t") == ""
}

// part is the preamble or a section, as bytes.
type part struct {
	line    string // a section's heading line, with its end; "" for the preamble
	heading string
	text    string // the lines under it
	given   bool   // the text is the caller's, not read
}

// parts returns the body as the preamble and then each section.
func (d *Doc) parts() []part {
	end := len(d.lines)
	ps := make([]part, len(d.sections)+1)
	for i := len(d.sections) - 1; i >= 0; i-- {
		s := d.sections[i]
		ps[i+1] = part{line: d.lines[s.at], heading: s.heading, text: strings.Join(d.lines[s.at+1:end], "")}
		end = s.at
	}
	ps[0] = part{text: strings.Join(d.lines[d.close+1:end], "")}
	return ps
}

// Preamble is the body before its first section. An unreadable document
// has none.
func (d *Doc) Preamble() string {
	if d.why() != "" {
		return ""
	}
	return d.parts()[0].text
}

// Section returns the lines under the "## heading" line, up to the next
// section. It returns ErrNoKey, ErrRepeated, or ErrNotWritten for an
// unreadable document.
func (d *Doc) Section(heading string) (string, error) {
	if d.why() != "" {
		return "", ErrNotWritten
	}
	i, err := d.sectionIndex(heading)
	if err != nil {
		return "", err
	}
	return d.parts()[i].text, nil
}

// sectionIndex is the index in parts of the one section headed heading.
func (d *Doc) sectionIndex(heading string) (int, error) {
	at := -1
	for i, s := range d.sections {
		if s.heading == heading {
			if at >= 0 {
				return 0, ErrRepeated
			}
			at = i + 1
		}
	}
	if at < 0 {
		return 0, ErrNoKey
	}
	return at, nil
}

// SetPreamble replaces the body before its first section with text.
func (d *Doc) SetPreamble(text string) error {
	if err := d.bodyEditable(); err != nil {
		return err
	}
	ps := d.parts()
	ps[0].text, ps[0].given = text, true
	return d.putBody(ps)
}

// SetSection replaces the lines under the "## heading" line with text, or
// adds the section at the end when there is none.
func (d *Doc) SetSection(heading, text string) error {
	if err := d.bodyEditable(); err != nil {
		return err
	}
	// A heading a reader shows as this one, "## Evidence " or
	// "## Evidence ##", is not this section: setting it would add a second
	// that looks the same. Nor is a new heading written in such a form.
	for _, s := range d.sections {
		if s.heading != heading && shown(s.heading) == shown(heading) {
			return fmt.Errorf("%w: a heading reads as this one in another form", ErrNotWritten)
		}
	}
	ps := d.parts()
	i, err := d.sectionIndex(heading)
	if err == ErrNoKey && shown(heading) != heading {
		return fmt.Errorf("%w: the heading is not in the form the codec writes", ErrNotWritten)
	}
	switch err {
	case nil:
		ps[i].text, ps[i].given = text, true
	case ErrNoKey:
		ps = append(ps, part{line: "## " + heading + "\n", heading: heading, text: text, given: true})
	default:
		return err
	}
	return d.putBody(ps)
}

// shown is a heading's text as Markdown shows it: spaces and tabs around
// it dropped, and a closing run of "#" after a space or a tab.
func shown(h string) string {
	s := strings.Trim(h, " \t")
	if t := strings.TrimRight(s, "#"); t != s && (t == "" || strings.HasSuffix(t, " ") || strings.HasSuffix(t, "\t")) {
		s = strings.Trim(t, " \t")
	}
	return s
}

// RemoveSection removes the "## heading" line and the lines under it.
func (d *Doc) RemoveSection(heading string) error {
	if err := d.bodyEditable(); err != nil {
		return err
	}
	i, err := d.sectionIndex(heading)
	if err != nil {
		return err
	}
	ps := d.parts()
	return d.putBody(append(ps[:i], ps[i+1:]...))
}

func (d *Doc) bodyEditable() error {
	if d.why() != "" || d.Protection().Locked {
		return ErrProtected
	}
	return nil
}

// putBody writes the body as ps after the frontmatter. A part followed by
// more bytes gets a final newline when the document had none there; a
// caller's text never does. The result must read back as ps, or nothing
// changes.
func (d *Doc) putBody(ps []part) error {
	closing := d.lines[d.close]
	type piece struct {
		s     *string
		given bool
	}
	pieces := []piece{{&closing, false}}
	for i := range ps {
		if i > 0 {
			pieces = append(pieces, piece{&ps[i].line, false})
		}
		pieces = append(pieces, piece{&ps[i].text, ps[i].given})
	}
	var prev piece
	for _, p := range pieces {
		if *p.s == "" {
			continue
		}
		if prev.s != nil && !strings.HasSuffix(*prev.s, "\n") {
			if prev.given {
				return fmt.Errorf("%w: a text followed by more must end in a newline", ErrNotWritten)
			}
			*prev.s += "\n"
		}
		prev = p
	}
	var b strings.Builder
	b.WriteString(strings.Join(d.lines[:d.close], ""))
	for _, p := range pieces {
		b.WriteString(*p.s)
	}
	nd := Read([]byte(b.String()))
	if why := nd.why(); why != "" {
		return fmt.Errorf("%w: the edit would leave %s", ErrNotWritten, why)
	}
	got := nd.parts()
	same := nd.close == d.close && len(got) == len(ps)
	for i := 0; same && i < d.close; i++ {
		same = nd.lines[i] == d.lines[i]
	}
	for i := 0; same && i < len(ps); i++ {
		same = got[i].line == ps[i].line && got[i].heading == ps[i].heading && got[i].text == ps[i].text
	}
	if !same {
		return fmt.Errorf("%w: the body would not read back as written", ErrNotWritten)
	}
	*d = *nd
	return nil
}
