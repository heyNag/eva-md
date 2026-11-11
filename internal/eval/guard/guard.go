// Package guard holds two checks of the v2 build that CI runs: that each
// commit stays inside the paths its card owns, and that the Go built into
// eva-md, net of v1 lines scheduled for deletion, is no larger than v1's.
// eva-eval's paths and size commands run them; eva-md never imports it.
//
// A card, docs/v2/cards/<ID>.md, carries two kinds of fenced block that
// this package reads. A block is opened by a line of three or more
// backticks followed by its info string, and closed by a line of at least
// as many backticks and nothing else; one entry a line, blank lines
// ignored.
//
// A block whose info string is "paths" lists what the package owns and may
// touch. An entry is an exact file, a folder ending in "/**", which
// matches everything under it ("a/**" matches a/b/c and not ab/c), or a
// pattern whose "*", "?" and "[...]" match within one name of a path, as
// path.Match reads them ("docs/v2/cards/*.md"). A card may carry more than
// one; their entries are read together.
//
// A block whose info string is "deletes" lists the v1 files the package
// schedules for deletion: exact files, or folders ending in "/**", with no
// other pattern.
//
// Entries are paths from the repository's root, with forward slashes: no
// leading "/" or "./", no "." or ".." name, and "**" only as the last name
// of a folder entry.
//
// A commit names its cards with "Card: <ID>" trailers, one card a trailer.
// The card index, docs/v2/cards/README.md, gives each card's state in the
// last column of its table; a card whose state begins with "merged" is
// merged.
package guard

import (
	"errors"
	"fmt"
	"path"
	"regexp"
	"strings"
)

// CardsDir is the folder of the cards, from the repository's root.
const CardsDir = "docs/v2/cards"

// cardID is the form of a card's id, such as EV-11, CT-3b or SEAM-0a.
var cardID = regexp.MustCompile(`^[A-Z]+-[0-9]+[a-z]*$`)

// CardFile is the card's file from the repository's root, or "" when id is
// not a card's id.
func CardFile(id string) string {
	if !cardID.MatchString(id) {
		return ""
	}
	return CardsDir + "/" + id + ".md"
}

// Block returns the entries of every fenced block in text whose info
// string is info, trimmed, without blank lines, and whether text has such
// a block. A fence left open is an error.
func Block(text, info string) (entries []string, found bool, err error) {
	open := 0 // the length of the open fence's backticks, 0 outside one
	in := false
	for _, line := range strings.Split(text, "\n") {
		trimmed := strings.TrimSpace(line)
		ticks := len(trimmed) - len(strings.TrimLeft(trimmed, "`"))
		switch {
		case open == 0 && ticks >= 3:
			open = ticks
			in = strings.TrimSpace(trimmed[ticks:]) == info
			found = found || in
		case open > 0 && ticks >= open && ticks == len(trimmed):
			open, in = 0, false
		case in && trimmed != "":
			entries = append(entries, trimmed)
		}
	}
	if open > 0 {
		return nil, false, errors.New("a fenced block is not closed")
	}
	return entries, found, nil
}

// A Pattern is one entry of a paths or deletes block.
type Pattern string

// ParsePattern checks an entry. glob allows "*", "?" and "[...]", which a
// paths block may use and a deletes block may not.
func ParsePattern(s string, glob bool) (Pattern, error) {
	name := strings.TrimSuffix(s, "/**")
	folder := name != s
	bad := func(why string) (Pattern, error) { return "", fmt.Errorf("the entry %q %s", s, why) }
	switch {
	case name == "" || strings.HasPrefix(name, "/") || strings.Contains(name, `\`):
		return bad("is not a path from the repository's root")
	case path.Clean(name) != name || name == "." || name == ".." || strings.HasPrefix(name, "../"):
		return bad(`is not a clean path from the repository's root, or has a "." or ".." name`)
	case strings.Contains(name, "**"):
		return bad(`uses "**" other than as the last name of a folder`)
	}
	meta := strings.ContainsAny(name, "*?[")
	switch {
	case meta && (!glob || folder):
		return bad("uses a pattern where only an exact file or a folder ending in /** is allowed")
	case meta:
		if _, err := path.Match(name, ""); err != nil {
			return bad("is not a valid pattern")
		}
	}
	return Pattern(s), nil
}

// folder returns the folder of a "/**" entry, and false for any other.
func (p Pattern) folder() (string, bool) {
	name, ok := strings.CutSuffix(string(p), "/**")
	return name, ok
}

// Match reports whether file, a path from the repository's root, is one
// the entry names.
func (p Pattern) Match(file string) bool {
	if dir, ok := p.folder(); ok {
		return strings.HasPrefix(file, dir+"/")
	}
	ok, _ := path.Match(string(p), file)
	return ok
}

// covers reports whether every file that q matches is one p matches too.
func (p Pattern) covers(q Pattern) bool {
	if p == q {
		return true
	}
	dir, ok := p.folder()
	if ok {
		// Only literal names come before a folder's "/**", and a glob's "*"
		// never crosses a "/", so q stays under its literal start.
		return strings.HasPrefix(string(q), dir+"/")
	}
	if _, isFolder := q.folder(); isFolder || strings.ContainsAny(string(q), "*?[") {
		return false
	}
	return p.Match(string(q))
}

// Patterns reads a card's block of the given kind, "paths" or "deletes",
// and checks its entries. found is false when the card has no such block.
func Patterns(card, kind string) (ps []Pattern, found bool, err error) {
	entries, found, err := Block(card, kind)
	if err != nil {
		return nil, false, err
	}
	for _, e := range entries {
		p, err := ParsePattern(e, kind == "paths")
		if err != nil {
			return nil, false, fmt.Errorf("its %s block: %w", kind, err)
		}
		ps = append(ps, p)
	}
	return ps, found, nil
}

// anyMatch reports whether one of ps matches file.
func anyMatch(ps []Pattern, file string) bool {
	for _, p := range ps {
		if p.Match(file) {
			return true
		}
	}
	return false
}

// indexRow is a row of the card index's table: its first cell links the
// card, as "[EV-11](EV-11.md)".
var indexRow = regexp.MustCompile(`^\|\s*\[([^\]]+)\]\([^)]*\)\s*\|`)

// Merged returns the ids of the cards the index lists as merged, in its
// order: those whose state, the last cell of their row, begins with
// "merged".
func Merged(index string) []string {
	var ids []string
	for _, line := range strings.Split(index, "\n") {
		line = strings.TrimSpace(line)
		m := indexRow.FindStringSubmatch(line)
		if m == nil || !strings.HasSuffix(line, "|") {
			continue
		}
		cells := strings.Split(strings.TrimSuffix(line, "|"), "|")
		if strings.HasPrefix(strings.TrimSpace(cells[len(cells)-1]), "merged") {
			ids = append(ids, strings.TrimSpace(m[1]))
		}
	}
	return ids
}
