package knowledgepage

import (
	"errors"
	"fmt"
	"io"
	"io/fs"
	"maps"
	"os"
	"path/filepath"
	"slices"
	"strings"

	"github.com/heyNag/eva-md/internal/okfadapter"
)

// The map's own frontmatter, the folder index's heading and the line that
// lists the folder in the root index.
const (
	MapRef         = Folder + "/map"
	mapTitle       = "Knowledge map"
	mapDescription = "Every knowledge page, one line each"
	indexHeading   = "Knowledge"
	rootTitle      = "Knowledge"
	rootDesc       = "Lasting facts, decisions, procedures and constraints from earlier chats"
	folderIndex    = Folder + "/index.md"
)

// sep is what the map writes between a line's fields.
const sep = " · "

// Report is what the knowledge folder holds and whether the files built
// from it are current.
type Report struct {
	Exists                                    bool // the folder, the recall index or the root index's line exists
	Pages, Active, Retired, Unreadable        int  // the pages, by state, and those that can't be read
	Facts, Decisions, Procedures, Constraints int  // the active pages of each kind
	Indexed, LeftOut                          int  // the active pages the recall index lists, and those its bound leaves out
	// Stale names, by path in the wiki, each file that was missing or out
	// of date: the map, the folder's index, the recall index or the root
	// index, which lacked its line. Rebuilt reports that they were written
	// again.
	Stale   []string
	Rebuilt bool
	// MapSkip is why the map is never written: a person locked or verified
	// it, or it can't be read as a map. The map is then left as it is.
	MapSkip string
}

// Refresh builds the map, the folder's index, the recall index and the root
// index's line from the pages, as eva-md doctor and the daily maintain do,
// and reports what was missing or out of date. It reads without the wiki
// lock; with repair, when something is out of date, it takes the lock, as
// any write of the wiki does, and builds them again from the pages read
// again under it. Without repair, as eva-md status, it writes nothing. A
// wiki without the folder whose recall index or root index still lists
// pages, as one a person removed the folder from, has both out of date: the
// recall index is then its header alone and the root index loses its line.
// A wiki with none of them has nothing to refresh.
func Refresh(w *okfadapter.Wiki, version string, repair bool) (Report, error) {
	r, _, err := refresh(w, version, false)
	if err != nil || !repair || len(r.Stale) == 0 {
		return r, err
	}
	err = w.Locked(func(lw *okfadapter.Wiki) error {
		r, _, err = refresh(lw, version, true)
		return err
	})
	return r, err
}

// listed is a page as the map and the indexes list it.
type listed struct {
	id          int
	entry       Entry
	description string // the description okf read, which the folder index repeats
	readable    bool
}

// refresh reads every page and compares the map, the folder's index, the
// recall index and the root index with what the pages make of them, and,
// with write, writes each that differs. It returns the files it wrote.
func refresh(w *okfadapter.Wiki, version string, write bool) (Report, []string, error) {
	var r Report
	var files []string
	stale := func(file string, ok bool, err error) error {
		if err == nil && !ok {
			r.Stale = append(r.Stale, file)
			if write {
				files = append(files, file)
				r.Rebuilt = true
			}
		}
		return err
	}
	info, err := os.Lstat(filepath.Join(w.Root(), Folder))
	switch {
	case errors.Is(err, fs.ErrNotExist):
		err := gone(w, write, &r, stale)
		return r, files, err
	case err != nil:
		return r, nil, err
	case !info.IsDir():
		return r, nil, fmt.Errorf("knowledgepage: %s is not a folder", Folder)
	}
	r.Exists = true
	ids, _, err := listIDs(w)
	if err != nil {
		return r, nil, err
	}
	var pages []listed
	var entries []Entry
	for _, id := range ids {
		p, err := readPage(w, id)
		if err != nil {
			return r, nil, err
		}
		if p == nil {
			continue
		}
		l := listed{id: id, readable: p.modeled()}
		if l.readable {
			l.entry, l.description = p.entry, strings.TrimSpace(p.pg.Concept.Description)
			entries = append(entries, p.entry)
		}
		pages = append(pages, l)
		r.count(l)
	}
	ok, skip, err := syncMap(w, pages, version, write)
	if r.MapSkip = skip; skip != "" && err == nil && !ok {
		// The map stays as a person left it, so it is out of date but
		// never rebuilt.
		r.Stale = append(r.Stale, MapRef+".md")
	} else if err := stale(MapRef+".md", ok, err); err != nil {
		return r, nil, err
	}
	ok, err = syncFile(w, folderIndex, folderText(pages, others(w)), write)
	if err := stale(folderIndex, ok, err); err != nil {
		return r, nil, err
	}
	text, indexed, leftOut := indexText(entries)
	r.Indexed, r.LeftOut = indexed, leftOut
	ok, err = syncIndex(w, text, write)
	if err := stale(IndexFile, ok, err); err != nil {
		return r, nil, err
	}
	ok, err = syncRoot(w, true, write)
	if err := stale("index.md", ok, err); err != nil {
		return r, nil, err
	}
	return r, files, nil
}

// gone refreshes a wiki without the folder: a recall index, or a root
// index line, is out of date when it still lists pages, and with write the
// index becomes its header alone and the line is removed. The folder is
// never created.
func gone(w *okfadapter.Wiki, write bool, r *Report, stale func(string, bool, error) error) error {
	_, err := os.Lstat(filepath.Join(w.Root(), filepath.FromSlash(IndexFile)))
	index := err == nil
	if err != nil && !errors.Is(err, fs.ErrNotExist) {
		return err
	}
	root, err := readWikiFile(w, "index.md", 1<<20)
	if err != nil {
		return err
	}
	if !index && !strings.Contains(root, "]("+folderIndex+")") {
		return nil
	}
	r.Exists = true
	if index {
		ok, err := syncIndex(w, indexHeader+"\n", write)
		if err := stale(IndexFile, ok, err); err != nil {
			return err
		}
	}
	ok, err := syncRoot(w, false, write)
	return stale("index.md", ok, err)
}

// count adds a page to the report's counts.
func (r *Report) count(l listed) {
	r.Pages++
	switch {
	case !l.readable:
		r.Unreadable++
	case l.entry.State == Retired:
		r.Retired++
	default:
		r.Active++
		switch l.entry.Kind {
		case Fact:
			r.Facts++
		case Decision:
			r.Decisions++
		case Procedure:
			r.Procedures++
		case Constraint:
			r.Constraints++
		}
	}
}

// mapLine is a page's line on the map: "- [<title>](k<N>.md): <kind> ·
// <scope> · <project key> · as of <day>", with " · retired" for a retired
// page, or "- [k<N> (unreadable)](k<N>.md)" for a page that can't be read.
func (l listed) mapLine() string {
	file := Name(l.id) + ".md"
	if !l.readable {
		return "- [" + Name(l.id) + " (unreadable)](" + file + ")"
	}
	e := l.entry
	where := "this project"
	if e.Scope == ScopeAll {
		where = "all projects"
	}
	line := "- [" + linkText(mapText(e.Title)) + "](" + file + "): " + strings.Join([]string{e.Kind, where, mapText(e.Project), "as of " + e.AsOf}, sep)
	if e.State == Retired {
		line += sep + "retired"
	}
	return line
}

// mapBody is the map's body: a heading and one line per page, in id order,
// retired and unreadable pages included, so okf counts none as orphaned.
func mapBody(pages []listed) string {
	var b strings.Builder
	b.WriteString("\n# " + mapTitle + "\n\n")
	for _, l := range pages {
		b.WriteString(l.mapLine() + "\n")
	}
	return b.String()
}

// syncMap compares the map with the pages' lines and, with write, creates
// or updates it. A map a person locked or verified, or one that can't be
// read as a map, is left as it is, with the reason. The map is eva-md's own
// and grows with the pages, so it is read without a page's bound: a large
// one is out of date at worst, never damaged.
func syncMap(w *okfadapter.Wiki, pages []listed, version string, write bool) (current bool, skip string, err error) {
	body := mapBody(pages)
	actor := okfadapter.Actor("eva-md", version)
	pg, err := w.Read(MapRef)
	var pathErr *fs.PathError
	switch {
	case errors.Is(err, fs.ErrNotExist):
		if !write {
			return false, "", nil
		}
		c := &okfadapter.Concept{Type: "map", Title: mapTitle, Description: mapDescription, Status: "stable", Body: body}
		return false, "", w.Create(MapRef, c, actor)
	case errors.As(err, &pathErr):
		return false, "", err
	case err != nil:
		return false, ReasonDamaged, nil
	}
	c := pg.Concept
	switch {
	case okfadapter.IsLocked(c):
		skip = ReasonLocked
	case okfadapter.IsVerified(pg):
		skip = ReasonVerified
	case strings.TrimSpace(c.Type) != "map":
		skip = ReasonOtherType
	}
	current = strings.TrimSpace(c.Type) == "map" && strings.TrimSpace(c.Title) == mapTitle && strings.TrimSpace(c.Description) == mapDescription &&
		strings.TrimSpace(c.Status) == "stable" && strings.TrimSpace(c.Body) == strings.TrimSpace(body)
	if current || skip != "" || !write {
		return current, skip, nil
	}
	_, err = w.Update(MapRef, pg.Hash, func(c *okfadapter.Concept) error {
		c.Type, c.Title, c.Description, c.Status, c.Body = "map", mapTitle, mapDescription, "stable", body
		return nil
	}, actor)
	return false, "", err
}

// folderText is the folder's index: the map, every page and every other
// page a person put in the folder, by file name, in okf's line format. A
// readable page's line repeats its description, as okf's drift check wants.
func folderText(pages []listed, other map[string]string) string {
	lines := map[string]string{"map.md": okfadapter.IndexLine(mapTitle, "map.md", mapDescription)}
	maps.Copy(lines, other)
	for _, l := range pages {
		file := Name(l.id) + ".md"
		if l.readable {
			lines[file] = okfadapter.IndexLine(mapText(l.entry.Title), file, l.description)
		} else {
			lines[file] = okfadapter.IndexLine(Name(l.id)+" (unreadable)", file, "")
		}
	}
	var out []string
	for _, file := range slices.Sorted(maps.Keys(lines)) {
		out = append(out, lines[file])
	}
	return "# " + indexHeading + "\n\n" + strings.Join(out, "\n") + "\n"
}

// others returns the index lines of the folder's other regular .md pages, a
// person's own, by file name: each by its title and description as okf
// reads them, or by its name when okf can't read it, so okf never finds one
// missing from the folder's index.
func others(w *okfadapter.Wiki) map[string]string {
	files, err := os.ReadDir(filepath.Join(w.Root(), Folder))
	if err != nil {
		return nil
	}
	out := map[string]string{}
	for _, f := range files {
		name, ok := strings.CutSuffix(f.Name(), ".md")
		if _, isPage := ParseID(f.Name()); !ok || isPage || name == "index" || name == "map" || !f.Type().IsRegular() {
			continue
		}
		title, description := name, ""
		if pg, err := w.ReadMax(Folder+"/"+name, maxPage); err == nil {
			if t := strings.TrimSpace(pg.Concept.Title); t != "" {
				title = t
			}
			description = strings.TrimSpace(pg.Concept.Description)
		}
		out[f.Name()] = okfadapter.IndexLine(title, f.Name(), description)
	}
	return out
}

// syncIndex compares the recall index with text as its readers open it (see
// openIndex): one they refuse, such as a hard-linked file, is out of date,
// and with write it is written again, which replaces the file.
func syncIndex(w *okfadapter.Wiki, text string, write bool) (bool, error) {
	if readable(w, text) {
		return true, nil
	}
	if !write {
		return false, nil
	}
	return false, w.WriteFile(IndexFile, []byte(text))
}

// readable reports whether the readers would open the recall index and read
// exactly text.
func readable(w *okfadapter.Wiki, text string) bool {
	root, err := os.OpenRoot(w.Root())
	if err != nil {
		return false
	}
	defer root.Close()
	f, err := openIndex(root, filepath.FromSlash(IndexFile))
	if err != nil {
		return false
	}
	defer f.Close()
	data, err := io.ReadAll(io.LimitReader(f, int64(len(text))+1))
	return err == nil && string(data) == text
}

// syncFile compares a wiki file that isn't a page with text and, with
// write, writes it when it differs.
func syncFile(w *okfadapter.Wiki, rel, text string, write bool) (bool, error) {
	if same(w, rel, text) {
		return true, nil
	}
	if !write {
		return false, nil
	}
	return false, w.WriteFile(rel, []byte(text))
}

// syncRoot checks that the root index lists the folder's index when the
// folder is there, and doesn't when it is gone, and, with write, adds its
// line once, after the lines already there, or removes every line that
// links to it. An empty or missing root index is left alone:
// okfadapter.Open refuses a wiki without one.
func syncRoot(w *okfadapter.Wiki, there, write bool) (bool, error) {
	data, err := readWikiFile(w, "index.md", 1<<20)
	link := "](" + folderIndex + ")"
	if err != nil || data == "" || strings.Contains(data, link) == there {
		return true, err
	}
	if !write {
		return false, nil
	}
	if !there {
		lines := strings.SplitAfter(data, "\n")
		return false, w.WriteFile("index.md", []byte(strings.Join(slices.DeleteFunc(lines, func(l string) bool { return strings.Contains(l, link) }), "")))
	}
	if !strings.HasSuffix(data, "\n") {
		data += "\n"
	}
	return false, w.WriteFile("index.md", []byte(data+okfadapter.IndexLine(rootTitle, folderIndex, rootDesc)+"\n"))
}

// same reports whether the wiki file holds exactly text: a regular file of
// text's size, so a file far too large is never read.
func same(w *okfadapter.Wiki, rel, text string) bool {
	old, err := readWikiFile(w, rel, int64(len(text)))
	return err == nil && old == text
}

// readWikiFile reads a regular file of the wiki that isn't a page, of at
// most limit bytes, or returns "" when it is missing or larger.
func readWikiFile(w *okfadapter.Wiki, rel string, limit int64) (string, error) {
	full := filepath.Join(w.Root(), filepath.FromSlash(rel))
	info, err := os.Lstat(full)
	if errors.Is(err, fs.ErrNotExist) {
		return "", nil
	}
	if err != nil {
		return "", err
	}
	if !info.Mode().IsRegular() {
		return "", fmt.Errorf("knowledgepage: %s is not a regular file", rel)
	}
	if info.Size() > limit {
		return "", nil
	}
	data, err := os.ReadFile(full)
	return string(data), err
}

// mapText is a frontmatter value as the map and the index show it: the
// page's free text cleaned as a page writes it, which leaves text Save wrote
// as it is, with the map's separator broken.
func mapText(s string) string {
	s = " " + cleanText(s, 0) + " "
	for strings.Contains(s, sep) {
		s = strings.ReplaceAll(s, sep, " - ")
	}
	return strings.TrimSpace(s)
}

// linkText escapes a title for a link's text, so it can't end the link
// early.
func linkText(s string) string {
	return strings.NewReplacer(`\`, `\\`, "[", `\[`, "]", `\]`).Replace(s)
}
