// Package codec reads and writes the bytes of a store document, as
// docs/spec/store-format.md describes it. It keeps every line a person
// wrote: the frontmatter is read as ordered entries, each a key's line and
// the lines under it, and runs of lines that belong to no key; the body as
// a preamble and sections. A lock or a verified mark is read fail-closed.
// It writes only the written forms of the format's section 4, and an edit
// changes only the lines of what it names. It works on bytes: it opens no
// file, takes no lock and has no clock. SPEC.md beside it is its contract.
package codec

import (
	"bytes"
	"errors"
	"io"
	"math"
	"regexp"
	"strconv"
	"strings"
	"unicode/utf8"
)

// The errors an edit, a value or a bounded read returns. An edit that
// returns one changes nothing.
var (
	// ErrProtected refuses an edit of a locked or unreadable document, and
	// any Set or Delete of locked.
	ErrProtected = errors.New("codec: the document is locked or cannot be read, and locked is never written")
	// ErrNoKey says the key or the section is not there.
	ErrNoKey = errors.New("codec: no such key or section")
	// ErrRepeated says the key or the section is there more than once.
	ErrRepeated = errors.New("codec: the key or section is repeated")
	// ErrNotWritten says a value is in a form the codec does not decode,
	// or an edit cannot be written in a form it reads back.
	ErrNotWritten = errors.New("codec: not in a written form")
	// ErrTooLarge refuses a document over the bound ReadFrom was given.
	ErrTooLarge = errors.New("codec: the document is over the size bound")
)

// Protection says how a document refuses the operations aimed at it. Its
// fields are those of the profile checker's Protection.
type Protection struct {
	Locked, Verified, Unreadable bool
}

// Doc is a document as read. Its zero value reads as the empty document,
// which is unreadable.
type Doc struct {
	raw        []byte   // an unreadable document's bytes, as read
	unreadable string   // why it cannot be read; "" when it can
	lines      []string // a readable document's lines, each with its end
	close      int      // the index of the line that closes the frontmatter
	base       int      // the indentation of the frontmatter's top level
	entries    []*entry
	kinds      []kind // what each line of the frontmatter is
	sections   []section
}

// Read reads a document. It never fails: a document it cannot read is
// kept byte for byte, and Unreadable says why.
func Read(b []byte) *Doc {
	d := &Doc{}
	if d.unreadable = unreadableReason(b); d.unreadable != "" {
		d.raw = bytes.Clone(b)
		return d
	}
	d.lines = splitLines(string(b))
	if len(d.lines) == 0 || text(d.lines[0]) != "---" {
		return &Doc{raw: bytes.Clone(b), unreadable: "no frontmatter on line 1"}
	}
	d.close = -1
	for i := 1; i < len(d.lines); i++ {
		if text(d.lines[i]) == "---" {
			d.close = i
			break
		}
	}
	if d.close < 0 {
		return &Doc{raw: bytes.Clone(b), unreadable: "a frontmatter that does not close"}
	}
	d.readFront()
	d.readBody()
	return d
}

// ReadFrom reads at most max bytes and one from r. More than max bytes is
// ErrTooLarge; otherwise it equals Read. A max below zero is zero.
func ReadFrom(r io.Reader, max int64) (*Doc, error) {
	if max < 0 {
		max = 0
	}
	limit := int64(math.MaxInt64)
	if max < math.MaxInt64 {
		limit = max + 1
	}
	b, err := io.ReadAll(io.LimitReader(r, limit))
	if err != nil {
		return nil, err
	}
	if int64(len(b)) > max {
		return nil, ErrTooLarge
	}
	return Read(b), nil
}

// Write returns a document's bytes. A document read and not edited writes
// back its input byte for byte, unreadable input included.
func Write(d *Doc) ([]byte, error) {
	if d == nil {
		return nil, errors.New("codec: no document")
	}
	if d.why() != "" {
		return bytes.Clone(d.raw), nil
	}
	return []byte(strings.Join(d.lines, "")), nil
}

// Unreadable is the reason the document cannot be read, or "": a
// byte-order mark, invalid UTF-8, a NUL byte, a line over 64 KiB, a
// conflict marker line, or a frontmatter not opened on line 1 or never
// closed.
func (d *Doc) Unreadable() string { return d.why() }

func (d *Doc) why() string {
	if d.unreadable == "" && len(d.lines) == 0 {
		return "no frontmatter on line 1"
	}
	return d.unreadable
}

var reConflict = regexp.MustCompile(`^(<<<<<<<|=======|>>>>>>>|\|\|\|\|\|\|\|)( |$)`)

// unreadableReason says why b cannot be read as text, or "".
func unreadableReason(b []byte) string {
	switch {
	case bytes.HasPrefix(b, []byte("\xef\xbb\xbf")):
		return "a byte-order mark"
	case !utf8.Valid(b):
		return "invalid UTF-8"
	case bytes.IndexByte(b, 0) >= 0:
		return "a NUL byte"
	}
	for i, line := range strings.Split(string(b), "\n") {
		if len(line) > 64<<10 {
			return "line " + strconv.Itoa(i+1) + " is over 64 KiB"
		}
		if reConflict.MatchString(strings.TrimSuffix(line, "\r")) {
			return "a conflict marker on line " + strconv.Itoa(i+1)
		}
	}
	return ""
}

// splitLines splits s into lines, each keeping its end.
func splitLines(s string) []string {
	var out []string
	for s != "" {
		i := strings.IndexByte(s, '\n')
		if i < 0 {
			return append(out, s)
		}
		out = append(out, s[:i+1])
		s = s[i+1:]
	}
	return out
}

// text is a line without its end: LF, or CR LF.
func text(line string) string {
	return strings.TrimSuffix(strings.TrimSuffix(line, "\n"), "\r")
}
