package knowledgepage

import (
	"cmp"
	"errors"
	"io"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"syscall"
	"time"
	"unicode"
)

// IndexFile is the recall index, which the prompt hook, the brief and the
// update read in place of the pages. It lives in the wiki's hidden folder,
// which okf ignores and the daily commit stages. Every write builds it again
// from the pages, and so do eva-md doctor and the daily maintain when it is
// missing or out of date. It is a cache of the pages, never the truth.
const IndexFile = ".eva-md/knowledge.tsv"

// MaxIndex bounds the recall index. Past it, the active pages with the
// oldest as_of are left out: they are still pages, and search still finds
// them, but they are neither recalled nor listed.
const MaxIndex = 512 << 10

// indexHeader is the recall index's first line, naming its format.
const indexHeader = "# eva-md knowledge index 1"

// readOnly marks a row whose page a person locked or verified, or that
// eva-md can't read back safely, which the update may show but never change.
const readOnly = "read-only"

// Row is an active page as the recall index lists it. Its fields are
// cleaned: no tabs, line breaks or control characters, runs of space made
// one.
type Row struct {
	ID                              string // k<N>
	Kind, Tag, Scope, Project, AsOf string
	At                              time.Time // the newest entry the statement cites; zero when none
	ReadOnly                        bool      // a person locked or verified the page
	Title, Statement                string
	Paths                           []string // the paths the page's code sources cite
}

// Ref is the row's page, knowledge/k<N>.
func (r Row) Ref() string { return Folder + "/" + r.ID }

// line is the row's line in the recall index, fields separated by tabs:
// id, kind, tag, scope, project key, as_of, the time of the newest entry
// the statement cites, read-only or nothing, title, statement and the paths
// separated by commas. A path holding a comma is left out.
func (r Row) line() string {
	at := ""
	if !r.At.IsZero() {
		at = canonical(r.At).Format(time.RFC3339Nano)
	}
	access := ""
	if r.ReadOnly {
		access = readOnly
	}
	var paths []string
	for _, p := range r.Paths {
		if p = field(p); p != "" && !strings.Contains(p, ",") {
			paths = append(paths, p)
		}
	}
	fields := []string{r.ID, r.Kind, r.Tag, r.Scope, r.Project, r.AsOf, at, access, r.Title, r.Statement, strings.Join(paths, ",")}
	for i, f := range fields {
		fields[i] = field(f)
	}
	return strings.Join(fields, "\t")
}

// row is an entry's row in the recall index: read-only for a page a person
// locked or verified, or one eva-md reads only leniently.
func (e Entry) row() Row {
	return Row{ID: e.Name(), Kind: e.Kind, Tag: e.Tag, Scope: e.Scope, Project: e.Project, AsOf: e.AsOf, At: e.At,
		ReadOnly: e.Locked || e.Verified || e.damaged, Title: e.Title, Statement: e.Statement, Paths: e.Paths}
}

// field is a value as a field of the index holds it: tabs, line breaks and
// control or format characters become spaces, and runs of space one.
func field(s string) string {
	return strings.Join(strings.Fields(strings.Map(func(r rune) rune {
		if unicode.IsControl(r) || unicode.In(r, unicode.Cf) || unicode.IsSpace(r) {
			return ' '
		}
		return r
	}, s)), " ")
}

// indexText builds the recall index from the active pages, newest as_of
// first, within MaxIndex, and returns how many rows it lists and how many
// the bound left out.
func indexText(entries []Entry) (text string, listed, leftOut int) {
	rows := make([]Row, 0, len(entries))
	for _, e := range entries {
		if e.State == Active {
			rows = append(rows, e.row())
		}
	}
	newestFirst(rows)
	var b strings.Builder
	b.WriteString(indexHeader + "\n")
	for i, r := range rows {
		l := r.line() + "\n"
		if b.Len()+len(l) > MaxIndex {
			return b.String(), i, len(rows) - i
		}
		b.WriteString(l)
	}
	return b.String(), len(rows), 0
}

// newestFirst sorts rows by as_of, then by the time of their newest entry,
// then by id, the newest first.
func newestFirst(rows []Row) {
	slices.SortStableFunc(rows, func(a, b Row) int {
		ia, _ := ParseID(a.ID)
		ib, _ := ParseID(b.ID)
		return cmp.Or(strings.Compare(b.AsOf, a.AsOf), b.At.Compare(a.At), cmp.Compare(ib, ia))
	})
}

// ParseIndex reads the recall index. It reads nothing from text that does
// not start with the index's header, and skips a line that is not a row.
func ParseIndex(data []byte) []Row {
	text := string(data)
	first, rest, _ := strings.Cut(text, "\n")
	if first != indexHeader {
		return nil
	}
	var out []Row
	for l := range strings.SplitSeq(rest, "\n") {
		if r, ok := parseRow(l); ok {
			out = append(out, r)
		}
	}
	return out
}

// parseRow reads one row of the recall index. A read-only row may have no
// tag: a person who edits a page can take its statement's tag away, and the
// page stays recalled, read-only, as it reads leniently.
func parseRow(l string) (Row, bool) {
	f := strings.Split(l, "\t")
	if len(f) != 11 {
		return Row{}, false
	}
	r := Row{ID: f[0], Kind: f[1], Tag: f[2], Scope: f[3], Project: f[4], AsOf: f[5], Title: f[8], Statement: f[9]}
	if _, ok := ParseID(r.ID); !ok || !kinds[r.Kind] || !tags[r.Tag] && r.Tag != "" || !scopes[r.Scope] || r.Project == "" || !day(r.AsOf) || r.Title == "" || r.Statement == "" {
		return Row{}, false
	}
	if f[6] != "" {
		at, err := time.Parse(time.RFC3339Nano, f[6])
		if err != nil {
			return Row{}, false
		}
		r.At = canonical(at)
	}
	switch f[7] {
	case "":
	case readOnly:
		r.ReadOnly = true
	default:
		return Row{}, false
	}
	if r.Tag == "" && !r.ReadOnly {
		return Row{}, false
	}
	for p := range strings.SplitSeq(f[10], ",") {
		if p != "" {
			r.Paths = append(r.Paths, p)
		}
	}
	return r, true
}

// ReadIndex reads the recall index of the wiki root holds by its path
// alone, as the prompt hook and the brief do: a regular file with no link
// on its path and one hard link, read up to MaxIndex. It never lists the
// folder or reads a page. A missing index is an error that wraps
// fs.ErrNotExist.
func ReadIndex(root *os.Root) ([]Row, error) {
	f, err := openIndex(root, filepath.FromSlash(IndexFile))
	if err != nil {
		return nil, err
	}
	defer f.Close()
	data, err := io.ReadAll(io.LimitReader(f, MaxIndex+1))
	if err != nil {
		return nil, err
	}
	if len(data) > MaxIndex {
		return nil, errors.New("knowledgepage: the recall index is over its bound")
	}
	return ParseIndex(data), nil
}

// ReadIndexAt is ReadIndex for the wiki at the given path.
func ReadIndexAt(wiki string) ([]Row, error) {
	root, err := os.OpenRoot(wiki)
	if err != nil {
		return nil, err
	}
	defer root.Close()
	return ReadIndex(root)
}

// openIndex opens name through a pinned root, refusing a link on its path,
// a file that isn't regular or is over MaxIndex, a file with more than one
// hard link and one that changed while it was opened, as the brief opens
// wiki files.
func openIndex(root *os.Root, name string) (*os.File, error) {
	if !filepath.IsLocal(name) {
		return nil, errors.New("knowledgepage: invalid file path")
	}
	parts := strings.Split(filepath.Clean(name), string(filepath.Separator))
	for i := 1; i < len(parts); i++ {
		info, err := root.Lstat(filepath.Join(parts[:i]...))
		if err != nil {
			return nil, err
		}
		if !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
			return nil, errors.New("knowledgepage: linked parent")
		}
	}
	before, err := root.Lstat(name)
	if err != nil {
		return nil, err
	}
	if !before.Mode().IsRegular() {
		return nil, errors.New("knowledgepage: not a regular file")
	}
	if before.Size() > MaxIndex {
		return nil, errors.New("knowledgepage: the recall index is over its bound")
	}
	f, err := root.OpenFile(name, os.O_RDONLY|syscall.O_NONBLOCK|syscall.O_NOFOLLOW, 0)
	if err != nil {
		return nil, err
	}
	after, err := f.Stat()
	if err != nil || !os.SameFile(before, after) {
		f.Close()
		return nil, errors.New("knowledgepage: file changed while opening")
	}
	if st, ok := after.Sys().(*syscall.Stat_t); ok && st.Nlink > 1 {
		f.Close()
		return nil, errors.New("knowledgepage: multiply linked file")
	}
	return f, nil
}
