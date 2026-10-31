package wikigit

import (
	"bytes"
	"context"
	"errors"
	"regexp"
	"strings"

	"github.com/heyNag/eva-md/internal/redact"
)

// maxLine bounds a diff line the scan judges. A longer added line counts as
// a secret: its file is left out rather than committed unread.
const maxLine = 1 << 20

var (
	// word finds what could be an identifier: a run of letters, digits and
	// . _ / # : = -.
	word = regexp.MustCompile(`[A-Za-z0-9._/#:=-]+`)
	// ref is a source ref, s:<session>#<line>.
	ref = regexp.MustCompile(`^s:[A-Za-z0-9._-]+#[0-9]+$`)

	errSecret = errors.New("wikigit: secret found")
)

// leaks reports whether a line the staged file adds holds a secret. A file
// that can't be read as text counts as one: a binary diff, a line too long
// to judge, or git failing.
func leaks(ctx context.Context, wiki, file string) bool {
	d := &diffScan{}
	err := run(ctx, gitTimeout, wiki, d, "diff", "--cached", "--no-color", "--no-ext-diff", "--no-textconv", "--no-renames", "-U0", "--", literal(file))
	if len(d.line) > 0 {
		d.judge()
	}
	return d.secret || err != nil
}

// diffScan reads one file's staged diff, as git writes it, and stops at the
// first added line that holds a secret.
type diffScan struct {
	line   []byte // the line being read, at most maxLine bytes of it
	long   bool   // the line being read is longer than maxLine
	body   bool   // past the diff's header, in its hunks
	secret bool
}

func (d *diffScan) Write(p []byte) (int, error) {
	n := len(p)
	for len(p) > 0 {
		i := bytes.IndexByte(p, '\n')
		part := p
		if i >= 0 {
			part = p[:i]
		}
		if !d.long {
			if room := maxLine - len(d.line); len(part) > room {
				part, d.long = part[:room], true
			}
			d.line = append(d.line, part...)
		}
		if i < 0 {
			break
		}
		if d.judge(); d.secret {
			return n - len(p) + i + 1, errSecret
		}
		p = p[i+1:]
	}
	return n, nil
}

// judge reads one complete line of the diff.
func (d *diffScan) judge() {
	line, long := string(d.line), d.long
	d.line, d.long = d.line[:0], false
	switch {
	case strings.HasPrefix(line, "@@"):
		d.body = true
	case !d.body && strings.HasPrefix(line, "Binary files "):
		d.secret = true
	case d.body && strings.HasPrefix(line, "+"):
		d.secret = long || secretLine(line[1:])
	}
}

// SecretLines returns the numbers, from 1, of the lines of text that the
// commit's scan would judge to hold a secret were a page to add them, so a
// writer can refuse them rather than have a commit leave its page out.
func SecretLines(text string) []int {
	var out []int
	for i, line := range strings.Split(text, "\n") {
		if len(line) > maxLine || secretLine(line) {
			out = append(out, i+1)
		}
	}
	return out
}

// secretLine reports whether a line holds a secret: whether redacting it
// replaces anything. Pages hold text already redacted, so a replacement
// means a secret got past redaction or was written by hand. The cleaning
// redacting starts with, such as dropping a byte order mark or an emoji's
// joiner, is no replacement.
//
// eva-md's identifiers, such as branch names, thread names, page ids and
// refs, are cleaned as identifiers are (redact.Identifier), not as text: as
// text, the entropy rule reads local-token-refresh-worker-c9a8c7 as the word
// token and a random value after it. So a word shaped like an identifier
// must pass redact.Identifier, and then stands as a run of x's, a value as
// plain as can be, while the line is redacted as text. A word followed by
// ':' or '=' is a key and stays, so password = hunter2hunter2 still holds a
// secret, and so does password: <a word shaped like an identifier>.
func secretLine(line string) bool {
	if _, n := redact.Redact(line); n == 0 {
		return false
	}
	var b strings.Builder
	last := 0
	for _, m := range word.FindAllStringIndex(line, -1) {
		w := line[m[0]:m[1]]
		if !identifier(w) || keyFollows(line[m[1]:]) {
			continue
		}
		if _, n := redact.Identifier(w, 0); n > 0 {
			return true
		}
		b.WriteString(line[last:m[0]])
		b.WriteString(strings.Repeat("x", len(w)))
		last = m[1]
	}
	if last == 0 {
		return true
	}
	b.WriteString(line[last:])
	_, n := redact.Redact(b.String())
	return n > 0
}

// identifier reports a word shaped like one of eva-md's identifiers: a ref,
// or two or more parts joined by '-', '_', '.' or '/', at least half of them
// words (see nameWord) and the rest lower case with digits, as in
// local-token-refresh-worker-c9a8c7, ENG-1234-rotate-api-key or
// projects/eva-md/threads/main.md. Generated secrets mix cases within a part,
// run without separators, or have few parts that are words.
func identifier(w string) bool {
	if ref.MatchString(w) {
		return true
	}
	parts := strings.FieldsFunc(w, func(r rune) bool { return r == '-' || r == '_' || r == '.' || r == '/' })
	if len(parts) < 2 {
		return false
	}
	words := 0
	for _, p := range parts {
		switch {
		case nameWord(p):
			words++
		case strings.Trim(p, lower+digits) != "":
			return false
		}
	}
	return 2*words >= len(parts)
}

const (
	lower  = "abcdefghijklmnopqrstuvwxyz"
	upper  = "ABCDEFGHIJKLMNOPQRSTUVWXYZ"
	digits = "0123456789"
)

// nameWord reports a part that reads as a word, as redact reads a name: lower
// case or a capital first, perhaps ending in digits, or capitals, as in
// cluster2, Rotate or ENG. Digits alone are no word.
func nameWord(p string) bool {
	w := strings.TrimRight(p, digits)
	if w == "" {
		return false
	}
	if w[0] >= 'A' && w[0] <= 'Z' {
		w = w[1:] // a capital first
	}
	return strings.Trim(w, lower) == "" || strings.Trim(p, upper) == ""
}

// keyFollows reports whether the text after a word starts with the
// separator that follows a key, ':' or '=', perhaps after a closing quote
// and spaces.
func keyFollows(rest string) bool {
	rest = strings.TrimLeft(strings.TrimLeft(rest, `"'`), " \t")
	return strings.HasPrefix(rest, ":") || strings.HasPrefix(rest, "=")
}
