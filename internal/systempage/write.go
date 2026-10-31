package systempage

import (
	"context"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"maps"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"time"

	"github.com/heyNag/eva-md/internal/config"
	"github.com/heyNag/eva-md/internal/okfadapter"
	"github.com/heyNag/eva-md/internal/paths"
)

// schemaVersion is the wiki schema this eva-md writes.
const schemaVersion = 1

// The map's own frontmatter, and the line that lists the folder in the root
// index.
const (
	mapTitle       = "System map"
	mapDescription = "One line per system page, built from their frontmatter"
	rootTitle      = "Systems"
	rootDesc       = "The systems the projects share"
)

// slugPattern is a page's file name: 2 to 64 of a-z, 0-9 and '-', starting
// with a letter.
var slugPattern = regexp.MustCompile(`^[a-z][a-z0-9-]{1,63}$`)

// beforeLock runs between reading the page as it is and taking the wiki
// lock; a test changes the page there.
var beforeLock = func() {}

// Options are what Write needs.
type Options struct {
	Config  config.Config    // the wiki and its instance
	Dirs    paths.Dirs       // where the wiki lock is
	Version string           // eva-md's version, the map's author
	Now     func() time.Time // the clock; time.Now when nil
}

// Result is what a write did.
type Result struct {
	Page    string // systems/<slug>.md
	Bytes   int64  // the page's size as saved
	Systems int    // the system pages the map lists
}

// Refusal is a write refused before anything was written. Each problem
// names its lines, never their text.
type Refusal struct{ Problems []string }

func (r *Refusal) Error() string { return "systempage: " + strings.Join(r.Problems, "; ") }

func refuse(problem string) error { return &Refusal{Problems: []string{problem}} }

// CheckSlug reports whether s can name a system page: 2 to 64 of a-z, 0-9
// and '-', starting with a letter, and not map, index, log or agents, which
// the folder's own files and okf use.
func CheckSlug(s string) error {
	if !slugPattern.MatchString(s) {
		return errors.New("a system's slug is 2 to 64 characters of a-z, 0-9 and -, starting with a letter")
	}
	switch s {
	case "map", "index", "log", "agents":
		return fmt.Errorf("%s is a reserved name; choose another slug", s)
	}
	return nil
}

// Write reads one system page from in and saves it as systems/<slug>.md. A
// page that exists needs replace, and is then replaced only if it is still
// the page read when Write started, a person hasn't verified it and it isn't
// locked. The page goes through the adapter under the wiki lock, and the
// map, the folder's index and, the first time, the root index are written
// again under the same lock. It never commits: the daily commit does. A
// page Write won't save comes back as a *Refusal.
func Write(ctx context.Context, o Options, slug string, replace bool, in io.Reader) (Result, error) {
	if err := CheckSlug(slug); err != nil {
		return Result{}, err
	}
	raw, err := io.ReadAll(io.LimitReader(in, maxPage+1))
	if err != nil {
		return Result{}, fmt.Errorf("systempage: read the page: %w", err)
	}
	w, err := openWiki(o.Config, o.Dirs)
	if err != nil {
		return Result{}, err
	}
	// A refusal of the page on disk, such as one that exists without
	// replace, is listed with what the check finds, so one refusal names
	// everything to fix.
	old, err := readOld(w, slug, replace)
	var refused *Refusal
	if err != nil && !errors.As(err, &refused) {
		return Result{}, err
	}
	now := time.Now
	if o.Now != nil {
		now = o.Now
	}
	pg, problems := check(slug, string(raw), now(), func(other string) bool { return isPage(w, other) })
	if refused != nil {
		problems = append(refused.Problems, problems...)
	}
	if len(problems) > 0 {
		return Result{}, &Refusal{Problems: problems}
	}
	beforeLock()
	if err := ctx.Err(); err != nil {
		return Result{}, err
	}
	actor := okfadapter.Actor("eva-md", o.Version)
	r := Result{Page: "systems/" + slug + ".md"}
	err = w.Locked(func(lw *okfadapter.Wiki) error {
		m, err := readMap(lw)
		if err != nil {
			return err
		}
		if err := save(lw, pg, old); err != nil {
			return err
		}
		r.Systems, err = refresh(lw, m, actor)
		return err
	})
	if err != nil {
		return Result{}, err
	}
	info, err := os.Lstat(filepath.Join(w.Root(), "systems", slug+".md"))
	if err != nil {
		return Result{}, err
	}
	r.Bytes = info.Size()
	return r, nil
}

// openWiki opens the configured wiki for writing, once its schema is the one
// this eva-md writes and it names the config's instance, as every wiki write
// checks.
func openWiki(c config.Config, dirs paths.Dirs) (*okfadapter.Wiki, error) {
	if c.WikiPath == "" {
		return nil, errors.New("systempage: no wiki is set up; run eva-md setup first")
	}
	w, err := okfadapter.Open(c.WikiPath, dirs.Locks())
	if err != nil {
		return nil, err
	}
	s, err := config.LoadSchema(filepath.Join(w.Root(), ".eva-md", "schema.toml"))
	switch {
	case err != nil:
		return nil, errors.New("systempage: the wiki's .eva-md/schema.toml can't be read")
	case s.SchemaVersion != schemaVersion:
		return nil, fmt.Errorf("systempage: the wiki's schema version is %d; this eva-md writes %d", s.SchemaVersion, schemaVersion)
	case c.Instance == "":
		return nil, errors.New("systempage: the config names no wiki instance")
	case s.Instance != c.Instance:
		return nil, errors.New("systempage: the wiki's instance is not the one the config names")
	}
	return w, nil
}

// readOld reads the page as it is before the write, or nil when there is
// none. A page that exists is replaced only with replace, and never when a
// person verified or locked it or okf can't read it.
func readOld(w *okfadapter.Wiki, slug string, replace bool) (*okfadapter.Page, error) {
	pg, err := w.Read("systems/" + slug)
	var pathErr *fs.PathError
	switch {
	case errors.Is(err, fs.ErrNotExist):
		return nil, nil
	case errors.As(err, &pathErr):
		return nil, err
	case !replace:
		return nil, refuse("the page exists; read it with eva-md show systems/" + slug + ", merge, and write it again with --replace")
	case err != nil:
		return nil, refuse("the page on disk can't be read as a wiki page; fix or remove it by hand")
	case len(pg.Concept.Verified) > 0:
		return nil, refuse("a person marked the page verified; eva-md leaves it as it is")
	case okfadapter.IsLocked(pg.Concept):
		return nil, refuse("a person locked the page; eva-md leaves it as it is")
	}
	return pg, nil
}

// isPage reports whether systems/<slug>.md exists as a file.
func isPage(w *okfadapter.Wiki, slug string) bool {
	info, err := os.Lstat(filepath.Join(w.Root(), "systems", slug+".md"))
	return err == nil && info.Mode().IsRegular()
}

// save writes the page: a new one through Create, an old one through
// Update's compare-and-swap against the page as Write read it. Either way a
// page that changed meanwhile is refused.
func save(w *okfadapter.Wiki, pg page, old *okfadapter.Page) error {
	id := "systems/" + pg.slug
	var err error
	if old == nil {
		c := &okfadapter.Concept{}
		pg.set(c)
		err = w.Create(id, c, pg.actor)
	} else {
		_, err = w.Update(id, old.Hash, func(c *okfadapter.Concept) error { pg.set(c); return nil }, pg.actor)
	}
	if errors.Is(err, okfadapter.ErrExists) || errors.Is(err, okfadapter.ErrConflict) || old != nil && errors.Is(err, fs.ErrNotExist) {
		return refuse("the page changed while this write ran; read it again, merge, and write it again")
	}
	return err
}

// set puts the page into c, keeping any other frontmatter a person added.
func (pg page) set(c *okfadapter.Concept) {
	c.Type, c.Title, c.Description, c.Status = fixed["type"], pg.title, pg.description, fixed["status"]
	c.Sources = nil
	for _, s := range pg.sources {
		c.Sources = append(c.Sources, okfadapter.Source{Resource: s})
	}
	if c.Extra == nil {
		c.Extra = map[string]any{}
	}
	c.Extra["repos"], c.Extra["aliases"] = pg.repos, pg.aliases
	c.Extra["as_of"], c.Extra["state"] = pg.asOf, fixed["state"]
	c.Body = "\n" + pg.body + "\n\n" + MapLink + "\n"
}

// saved is the page as okf saves it new, with the generated line okf adds
// at now.
func (pg page) saved(now time.Time) string {
	c := &okfadapter.Concept{}
	pg.set(c)
	c.Generated = &okfadapter.Generated{By: pg.actor, At: now.UTC().Format(time.RFC3339)}
	return okfadapter.Serialize(c)
}

// readMap reads the map as it is, or nil when there is none yet. The map is
// built, never edited, so one okf can't read stops the write.
func readMap(w *okfadapter.Wiki) (*okfadapter.Page, error) {
	pg, err := w.Read("systems/map")
	var pathErr *fs.PathError
	switch {
	case errors.Is(err, fs.ErrNotExist):
		return nil, nil
	case errors.As(err, &pathErr):
		return nil, err
	case err != nil:
		return nil, refuse("systems/map.md can't be read as a wiki page; remove it, and the next write builds it again")
	}
	return pg, nil
}

// refresh builds the map, the folder's index and the names index again from
// the pages' frontmatter, lists the folder in the root index once, and
// returns how many systems the map lists. An index is written only when it
// changes.
func refresh(w *okfadapter.Wiki, m *okfadapter.Page, actor string) (int, error) {
	entries := systems(w)
	var b strings.Builder
	b.WriteString("\n# " + mapTitle + "\n\n")
	for _, e := range entries {
		b.WriteString(e.line() + "\n")
	}
	if err := saveMap(w, m, b.String(), actor); err != nil {
		return 0, err
	}
	lines := map[string]string{"map.md": okfadapter.IndexLine(mapTitle, "map.md", mapDescription)}
	for _, e := range entries {
		lines[e.Slug+".md"] = e.indexLine()
	}
	var index []string
	for _, file := range slices.Sorted(maps.Keys(lines)) {
		index = append(index, lines[file])
	}
	if err := writeChanged(w, "systems/index.md", "# "+rootTitle+"\n\n"+strings.Join(index, "\n")+"\n"); err != nil {
		return 0, err
	}
	if text, _ := names(entries); !current(w, text) {
		if err := w.WriteFile(NamesFile, []byte(text)); err != nil {
			return 0, err
		}
	}
	return len(entries), linkRoot(w)
}

// saveMap writes the map's body, creating the map the first time.
func saveMap(w *okfadapter.Wiki, m *okfadapter.Page, body, actor string) error {
	if m == nil {
		c := &okfadapter.Concept{Type: "map", Title: mapTitle, Description: mapDescription, Status: "stable", Body: body}
		return w.Create("systems/map", c, actor)
	}
	_, err := w.Update("systems/map", m.Hash, func(c *okfadapter.Concept) error {
		c.Type, c.Title, c.Description, c.Status, c.Body = "map", mapTitle, mapDescription, "stable", body
		return nil
	}, actor)
	return err
}

// MapEntry is a system page as the map and the index show it: its slug,
// title, description, the project keys of its repos and its as_of day.
type MapEntry struct {
	Slug, Title, Description, AsOf string
	Repos                          []string
}

func (pg page) entry() MapEntry {
	return MapEntry{Slug: pg.slug, Title: clean(pg.title), Description: clean(pg.description), AsOf: pg.asOf, Repos: pg.repos}
}

// sep is what the map writes between a line's fields.
const sep = " · "

// line is the page's line on the map:
// "- [<title>](<slug>.md): <description> · repos: <key>, <key> · as of <day>".
func (e MapEntry) line() string {
	l := "- [" + linkText(e.Title) + "](" + e.Slug + ".md)"
	if e.Description != "" {
		l += ": " + e.Description
	}
	if len(e.Repos) > 0 {
		l += sep + "repos: " + strings.Join(e.Repos, ", ")
	}
	if e.AsOf != "" {
		l += sep + "as of " + e.AsOf
	}
	return l
}

// ParseMapLine reads a line of the map as line writes it. A line that is not
// one, or whose link names no system page, reports false. No field of a line
// the map was built with holds the separator, so the fields after the
// description are told apart by their names; one it doesn't know is skipped.
// A description edited by hand may hold the separator, so what comes before
// the first named field is the description.
func ParseMapLine(l string) (MapEntry, bool) {
	rest, ok := strings.CutPrefix(l, "- [")
	if !ok {
		return MapEntry{}, false
	}
	var title strings.Builder
	i := 0
	for ; i < len(rest) && rest[i] != ']'; i++ {
		if rest[i] == '\\' && i+1 < len(rest) {
			i++ // an escaped \, [ or ]
		}
		title.WriteByte(rest[i])
	}
	if i == len(rest) {
		return MapEntry{}, false
	}
	rest, ok = strings.CutPrefix(rest[i+1:], "(")
	end := strings.IndexByte(rest, ')')
	if !ok || end < 0 {
		return MapEntry{}, false
	}
	slug, ok := strings.CutSuffix(rest[:end], ".md")
	if !ok || CheckSlug(slug) != nil {
		return MapEntry{}, false
	}
	e := MapEntry{Slug: slug, Title: title.String()}
	var fields []string
	switch rest = rest[end+1:]; {
	case rest == "":
	case strings.HasPrefix(rest, ": "):
		fields = strings.Split(rest[2:], sep)
		n := 1
		for n < len(fields) && !named(fields[n]) {
			n++
		}
		e.Description, fields = strings.Join(fields[:n], sep), fields[n:]
	case strings.HasPrefix(rest, sep):
		fields = strings.Split(rest[len(sep):], sep)
	default:
		return MapEntry{}, false
	}
	for _, f := range fields {
		if repos, ok := strings.CutPrefix(f, "repos: "); ok {
			e.Repos = strings.Split(repos, ", ")
		} else if day, ok := strings.CutPrefix(f, "as of "); ok {
			e.AsOf = day
		}
	}
	return e, true
}

// named reports a field of a map line that line names: the repos or the day.
func named(f string) bool {
	return strings.HasPrefix(f, "repos: ") || strings.HasPrefix(f, "as of ")
}

// separates reports whether a value would hold the map's separator on its
// line, where the value has a space or the end of a field on each side.
func separates(s string) bool {
	return strings.Contains(" "+strings.Join(strings.Fields(s), " ")+" ", sep)
}

// indexLine is the page's line in systems/index.md.
func (e MapEntry) indexLine() string {
	return okfadapter.IndexLine(e.Title, e.Slug+".md", e.Description)
}

// system is a system page as the map, the folder's index and the names
// index show it.
type system struct {
	MapEntry
	aliases []string
}

// systems reads the frontmatter of every system page, sorted by slug. A page
// okf can't read is still listed, by its slug, since okf counts a page
// nothing links to as orphaned.
func systems(w *okfadapter.Wiki) []system {
	files, err := os.ReadDir(filepath.Join(w.Root(), "systems"))
	if err != nil {
		return nil
	}
	var out []system
	for _, f := range files {
		slug, ok := strings.CutSuffix(f.Name(), ".md")
		if !ok || !f.Type().IsRegular() || CheckSlug(slug) != nil {
			continue
		}
		e := system{MapEntry: MapEntry{Slug: slug, Title: slug}}
		if pg, err := w.Read("systems/" + slug); err == nil {
			c := pg.Concept
			if t := clean(c.Title); t != "" {
				e.Title = t
			}
			e.Description = clean(c.Description)
			as, _ := c.Extra["as_of"].(string)
			e.AsOf = clean(as)
			e.Repos = texts(c.Extra["repos"])
			e.aliases = texts(c.Extra["aliases"])
		}
		out = append(out, e)
	}
	slices.SortFunc(out, func(a, b system) int { return strings.Compare(a.Slug, b.Slug) })
	return out
}

// texts reads a frontmatter list, each item cleaned, leaving out an item
// that is not text or that cleans to nothing.
func texts(v any) []string {
	list, _ := v.([]any)
	var out []string
	for _, item := range list {
		if s, ok := item.(string); ok && clean(s) != "" {
			out = append(out, clean(s))
		}
	}
	return out
}

// linkRoot lists systems/index.md in the root index once, after the lines
// already there.
func linkRoot(w *okfadapter.Wiki) error {
	data, err := readWikiFile(w, "index.md")
	if err != nil || data == "" || strings.Contains(data, "](systems/index.md)") {
		return err
	}
	if !strings.HasSuffix(data, "\n") {
		data += "\n"
	}
	return w.WriteFile("index.md", []byte(data+okfadapter.IndexLine(rootTitle, "systems/index.md", rootDesc)+"\n"))
}

// writeChanged writes a wiki file that isn't a page when its text differs
// from what is there.
func writeChanged(w *okfadapter.Wiki, rel, text string) error {
	if old, err := readWikiFile(w, rel); err == nil && old == text {
		return nil
	}
	return w.WriteFile(rel, []byte(text))
}

// readWikiFile reads a regular file of the wiki that isn't a page, or
// returns "" when it is missing.
func readWikiFile(w *okfadapter.Wiki, rel string) (string, error) {
	full := filepath.Join(w.Root(), filepath.FromSlash(rel))
	info, err := os.Lstat(full)
	if errors.Is(err, fs.ErrNotExist) {
		return "", nil
	}
	if err != nil {
		return "", err
	}
	if !info.Mode().IsRegular() {
		return "", fmt.Errorf("systempage: %s is not a regular file", rel)
	}
	data, err := os.ReadFile(full)
	return string(data), err
}

// clean makes a frontmatter value one line of plain text for the map and
// the index, as internal/threadpage escapes model text: control and format
// characters become spaces, runs of space collapse, and comment openers,
// fence runs, links and the map's separator are broken. A value Write
// checked comes out as it went in; one a person wrote by hand may not.
func clean(s string) string {
	s = strings.Join(strings.Fields(strings.Map(func(r rune) rune {
		if hidden(r) {
			return ' '
		}
		return r
	}, s)), " ")
	s = strings.ReplaceAll(s, "<!--", "‹!--")
	s = fenceRun.ReplaceAllStringFunc(s, func(run string) string {
		return strings.NewReplacer("`", "ˋ", "~", "˜").Replace(run)
	})
	s = " " + strings.ReplaceAll(s, "](", "] (") + " "
	for strings.Contains(s, sep) {
		s = strings.ReplaceAll(s, sep, " - ") // twice for "a · · b"
	}
	return strings.TrimSpace(s)
}

// linkText escapes a title for a link's text, so it can't end the link
// early.
func linkText(s string) string {
	return strings.NewReplacer(`\`, `\\`, "[", `\[`, "]", `\]`).Replace(s)
}
