package codec

// What a reader that checks a document, rather than edits it, asks of the
// codec: every entry with its line, the lines that belong to no key, and
// every section. Nothing here changes a document or how it is read.

// Entry is one key of the frontmatter, as Entries gives it.
type Entry struct {
	// Key is the key as YAML reads it, or "*" for a key no scalar on its
	// line names (an alias, a flow collection, an explicit key that is no
	// scalar or a compact mapping), or "<<" for a merge.
	Key string
	// Line is the line the key is on, the opening --- being line 1.
	Line int
	// Written says the entry is in a written form of the format's section
	// 4; Value is then what Value would decode for it alone.
	Written bool
	Value   any
	// Scalar is the value on the key's line read as a scalar a person
	// wrote: spaces and tabs around it dropped, quotes undone, a comment
	// after a plain value dropped.
	Scalar string
	// Text is what follows the key's colon, or an explicit key's value
	// line, and then each line under the key after an LF, as written but
	// for the line ends.
	Text string
}

// Entries returns every entry of the frontmatter in file order, a repeated
// key as often as it is there. An unreadable document has none. Each call
// decodes afresh: the caller owns what it gets.
func (d *Doc) Entries() []Entry {
	if d.why() != "" {
		return nil
	}
	out := make([]Entry, 0, len(d.entries))
	for _, e := range d.entries {
		v, ok := writtenValue(e)
		text := e.rest
		for _, c := range e.cont {
			text += "\n" + c
		}
		out = append(out, Entry{Key: e.key, Line: e.lines[0] + 1, Written: ok, Value: v, Scalar: lenient(e.rest), Text: text})
	}
	return out
}

// Opaque returns the lines of the frontmatter that belong to no entry, in
// order and counted as Entry.Line is: a blank line, a comment at the top
// level, a line before the first key, a line in no form the codec reads.
func (d *Doc) Opaque() []int {
	if d.why() != "" {
		return nil
	}
	var out []int
	for i := 1; i < d.close; i++ {
		if d.kinds[i] == opaque {
			out = append(out, i+1)
		}
	}
	return out
}

// Section is one section of the body, as Sections gives it.
type Section struct {
	Heading string // the "## " line's text after "## ", a CR before its end dropped
	Text    string // the lines under it, up to the next section, with their ends
}

// Sections returns every section of the body in order, a repeated heading
// as often as it is there. A "## " line inside fenced code starts none. An
// unreadable document has none.
func (d *Doc) Sections() []Section {
	if d.why() != "" {
		return nil
	}
	ps := d.parts()[1:]
	out := make([]Section, len(ps))
	for i, p := range ps {
		out[i] = Section{Heading: p.heading, Text: p.text}
	}
	return out
}

// UnreadableText is the reason b cannot be read as text, or "": the
// reasons of Unreadable but the two of the frontmatter, for a file that
// has none, such as a static or record file of the store.
func UnreadableText(b []byte) string {
	return unreadableReason(b)
}
