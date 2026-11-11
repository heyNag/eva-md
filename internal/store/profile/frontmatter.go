package profile

import (
	"strings"

	"github.com/heyNag/eva-md/internal/store/codec"
)

// doc is a document read for checking. The codec reads it, as it reads it
// for every other caller: what is unreadable, which line is a key, which
// value is in a written form, protection and the body's sections. The
// checker reads no YAML of its own.
type doc struct {
	path       string
	loc        loc
	unreadable string // why it cannot be read; "" when it can
	prot       Protection
	entries    []codec.Entry
	opaque     []int // lines of the block that belong to no key
	sections   []codec.Section
	body       []string // the lines after the closing ---, CR dropped
	hand       bool     // the hand-edit record lists it
}

// unknownKey and mergeKey are the keys the codec gives an entry it reads
// as both locked and verified: one no scalar on its line names, and a
// merge.
const (
	unknownKey = "*"
	mergeKey   = "<<"
)

// Protection says how a file refuses the operations aimed at it.
type Protection struct {
	Locked, Verified, Unreadable bool
}

// load reads a document through the codec.
func load(path string, b []byte) *doc {
	cd := codec.Read(b)
	d := &doc{path: path, unreadable: cd.Unreadable(), prot: Protection(cd.Protection())}
	if d.unreadable != "" {
		return d
	}
	d.entries, d.opaque, d.sections = cd.Entries(), cd.Opaque(), cd.Sections()
	text := cd.Preamble()
	for _, s := range d.sections {
		text += "## " + s.Heading + "\n" + s.Text
	}
	for _, l := range strings.Split(strings.TrimSuffix(text, "\n"), "\n") {
		d.body = append(d.body, strings.TrimSuffix(l, "\r"))
	}
	if text == "" {
		d.body = nil
	}
	return d
}

// get returns the first entry for key, or nil.
func (d *doc) get(key string) *codec.Entry {
	for i := range d.entries {
		if d.entries[i].Key == key {
			return &d.entries[i]
		}
	}
	return nil
}

// under returns the lines that are not blank in each "## <name>" section,
// and whether there is one.
func (d *doc) under(name string) ([]string, bool) {
	var out []string
	found := false
	for _, s := range d.sections {
		if s.Heading != name {
			continue
		}
		found = true
		for _, l := range strings.Split(s.Text, "\n") {
			if l = strings.TrimSuffix(l, "\r"); strings.TrimSpace(l) != "" {
				out = append(out, l)
			}
		}
	}
	return out, found
}
