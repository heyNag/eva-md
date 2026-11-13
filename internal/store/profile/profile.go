// Package profile checks that a folder is a store that follows the written
// format, docs/spec/store-format.md. It reads; it never writes. Each fault
// names the file, the field and the rule, where a rule is the name of a row
// of the specification's last table. Nothing calls it yet: the codec, the
// migrator and the storage properties build against the format it checks.
package profile

import (
	"errors"
	"fmt"
	"io/fs"
	"maps"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"sort"
	"strconv"
	"strings"
)

// Rules are the names of the specification's last table, in its order. The
// checker has one check for each, and no other.
var Rules = []string{
	"path", "symlink", "name", "encoding", "size", "unreadable", "frontmatter", "written-form", "month",
	"index", "readme", "gitattributes", "gitignore", "manifest", "schema-marker", "writer-record", "hand-edits", "coverage-v1",
	"project-record", "workstream-file", "item-file", "disagreement-file", "session-file", "notes", "legacy", "system-page",
	"type", "id", "title", "description", "status", "state", "state_at", "state_reason", "scope", "project",
	"workstream", "tag", "supports", "words", "generated", "learned", "asserted_at", "confirmed", "supersedes",
	"branch", "anchors", "cues", "sources", "migrated_from", "locked", "verified",
	"remotes", "root_commits", "legacy_keys", "merged_into", "related", "shares_with",
	"session", "writer", "started", "workstreams", "covers", "item", "op", "reason", "at", "stale_after",
	"schema", "min_reader", "store_id", "epoch", "schema_version", "label", "signing_key", "reads",
	"statement", "citations", "working-items", "decisions-in-force", "log",
}

// Fault is one way a store departs from the format.
type Fault struct {
	Path   string // the file, relative to the store, with slashes
	Field  string // the field, or "" for the file as a whole
	Rule   string // the name of the specification's row
	Detail string
}

func (f Fault) String() string {
	field := f.Field
	if field == "" {
		field = "(file)"
	}
	return fmt.Sprintf("%s: %s: %s (rule %s)", f.Path, field, f.Detail, f.Rule)
}

// Report is what Check found.
type Report struct {
	Faults []Fault
	// Protected lists each file that refuses the operations aimed at it.
	// A file at a path the layout rejects is not read, so it is not
	// listed: its path fault is its report.
	Protected map[string]Protection
	// Unacknowledged lists the hand-edited files whose edit carries no
	// authority yet.
	Unacknowledged []string
}

// Check reads the folder at root and reports how it departs from the
// format. An error means the folder itself could not be read.
func Check(root string) (Report, error) {
	info, err := os.Stat(root)
	if err != nil {
		return Report{}, err
	}
	if !info.IsDir() {
		return Report{}, errors.New("profile: not a folder")
	}
	c := &checker{root: root, rep: Report{Protected: map[string]Protection{}}, files: map[string]*doc{},
		hand: map[string]bool{}, writers: map[string]bool{}, ids: map[string]string{}}
	var paths []string
	err = filepath.WalkDir(root, func(p string, de fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel, err := filepath.Rel(root, p)
		if err != nil {
			return err
		}
		rel = filepath.ToSlash(rel)
		if rel == "." {
			return nil
		}
		if rel == ".git" {
			if de.IsDir() {
				return filepath.SkipDir
			}
			return nil
		}
		if de.Type()&fs.ModeSymlink != 0 {
			c.fault(rel, "", "symlink", "is a symlink")
			return nil
		}
		if !de.IsDir() {
			paths = append(paths, rel)
		}
		return nil
	})
	if err != nil {
		return Report{}, err
	}
	sort.Strings(paths)
	for _, p := range paths {
		l := classify(p)
		c.locs = append(c.locs, l)
		// Ids are indexed from file names, so a reference to a file that
		// cannot be read still resolves.
		id := l.id
		if l.class == "project-record" {
			id = l.pid
		}
		if id == "" || l.bad != "" {
			continue
		}
		if prev, ok := c.ids[id]; ok {
			c.fault(p, "id", "id", "is also the id of "+prev)
			continue
		}
		c.ids[id] = p
	}
	for _, p := range []string{"index.md", "README.md", ".gitattributes", ".gitignore", ".eva-md/store.toml", ".eva-md/schema.toml"} {
		if !slices.Contains(paths, p) {
			c.fault(p, "", classify(p).class, "is missing")
		}
	}
	// Every project folder holds its identity record. With ids unique, a
	// pid then names one folder: a second folder for it either has no
	// record or repeats the id.
	folders := map[string]bool{}
	for _, l := range c.locs {
		if strings.HasPrefix(l.area, "projects/") && !(l.class == "project-record" && l.bad != "") {
			folders[l.area] = folders[l.area] || l.class == "project-record"
		}
	}
	for _, area := range slices.Sorted(maps.Keys(folders)) {
		if !folders[area] {
			c.fault(area+"/project.md", "", "project-record", "is missing: every project folder holds its identity record")
		}
	}
	// Writer records and the hand-edit record come first: other checks ask
	// for them.
	for _, l := range c.locs {
		if l.class == "writer-record" && reWriter.MatchString(l.writer) {
			c.writers[l.writer] = true
		}
	}
	c.handEdits()
	for _, l := range c.locs {
		c.file(l)
	}
	c.crossFile()
	sort.SliceStable(c.rep.Faults, func(i, j int) bool { return c.rep.Faults[i].Path < c.rep.Faults[j].Path })
	return c.rep, nil
}

type checker struct {
	root    string
	rep     Report
	locs    []loc
	files   map[string]*doc // readable documents by path
	hand    map[string]bool // paths the hand-edit record lists
	writers map[string]bool // writers with a record
	ids     map[string]string
	later   []func()
}

func (c *checker) fault(path, field, rule, detail string) {
	if !slices.Contains(Rules, rule) {
		panic("profile: no rule " + rule)
	}
	c.rep.Faults = append(c.rep.Faults, Fault{Path: path, Field: field, Rule: rule, Detail: detail})
}

func (c *checker) read(rel string) ([]byte, error) {
	return os.ReadFile(filepath.Join(c.root, filepath.FromSlash(rel)))
}

// loc is a path and what its place in the tree says.
type loc struct {
	path, class string
	bad         string // the class's own rule is broken: why
	area        string // "projects/<pid>-<slug>" or "global"
	pid         string
	ym          string // "<yyyy>/<mm>" from the path
	id, slug    string // from the file name
	host, skey  string
	writer      string
}

var documents = map[string]bool{"project-record": true, "workstream-file": true, "item-file": true, "disagreement-file": true, "session-file": true, "system-page": true}

var caps = map[string]int{"project-record": 16 << 10, "workstream-file": 16 << 10, "item-file": 16 << 10, "disagreement-file": 16 << 10, "session-file": 256 << 10, "system-page": 24 << 10}

// classify finds a path's class from its place in the tree.
func classify(p string) loc {
	l := loc{path: p}
	seg := strings.Split(p, "/")
	switch {
	case len(seg) == 1:
		l.class = map[string]string{"index.md": "index", "README.md": "readme", ".gitattributes": "gitattributes", ".gitignore": "gitignore"}[p]
	case seg[0] == ".eva-md":
		switch {
		case p == ".eva-md/store.toml":
			l.class = "manifest"
		case p == ".eva-md/schema.toml":
			l.class = "schema-marker"
		case len(seg) == 3 && seg[1] == "writers":
			l.class, l.writer = "writer-record", strings.TrimSuffix(seg[2], ".toml")
			if !strings.HasSuffix(seg[2], ".toml") || !reWriter.MatchString(l.writer) {
				l.bad = "the name is not <writer>.toml"
			}
		case len(seg) == 3 && (seg[1] == "hand-edits" || seg[1] == "coverage-v1"):
			l.class, l.writer = seg[1], strings.TrimSuffix(seg[2], ".tsv")
			if !strings.HasSuffix(seg[2], ".tsv") || !reWriter.MatchString(l.writer) {
				l.bad = "the name is not <writer>.tsv"
			}
		}
	case seg[0] == "systems" && len(seg) == 2:
		l.class, l.slug = "system-page", strings.TrimSuffix(seg[1], ".md")
		if !strings.HasSuffix(seg[1], ".md") || !reSlug.MatchString(l.slug) || len(l.slug) > 48 {
			l.bad = "the name is not <slug>.md"
		}
	case seg[0] == "legacy" && len(seg) >= 3 && seg[1] == "v1":
		l.class = "legacy"
		for _, s := range seg[2:] {
			if !reV1Seg.MatchString(s) {
				l.bad = "a segment v1 would not have named: " + strconv.Quote(s)
			}
		}
	case seg[0] == "global" && len(seg) >= 3:
		l.area = "global"
		areaClass(&l, seg[1:])
	case seg[0] == "projects" && len(seg) >= 3:
		l.area = "projects/" + seg[1]
		pid, slug, ok := strings.Cut(seg[1], "-")
		l.pid = pid
		if !ok || !reProject.MatchString(pid) || !reSlug.MatchString(slug) || len(slug) > 48 {
			l.class, l.bad = "project-record", "the folder is not <pid>-<slug>"
			return l
		}
		if len(seg) == 3 && seg[2] == "project.md" {
			l.class = "project-record"
			return l
		}
		areaClass(&l, seg[2:])
	}
	return l
}

var (
	reV1Seg   = regexp.MustCompile(`^[a-z0-9._-]+$`)
	reNoteSeg = regexp.MustCompile(`^[A-Za-z0-9._-]+$`)
	reYear    = regexp.MustCompile(`^20[0-9]{2}$`)
	reMonth   = regexp.MustCompile(`^(0[1-9]|1[0-2])$`)
)

// areaClass classifies what is under an area: items, disagreements,
// workstreams, sessions and notes.
func areaClass(l *loc, seg []string) {
	inProject := l.area != "global"
	switch {
	case seg[0] == "notes":
		l.class = "notes"
		for _, s := range seg[1:] {
			if !reNoteSeg.MatchString(s) || s == "." || s == ".." {
				l.bad = "a segment outside A-Za-z0-9._-: " + strconv.Quote(s)
			}
		}
	case seg[0] == "workstreams" && inProject:
		l.class = "workstream-file"
		if len(seg) != 2 || !named(l, seg[1], 'w') {
			l.bad = "the path is not workstreams/<wid>-<slug>.md"
		}
	case seg[0] == "items" || seg[0] == "disagreements":
		l.class = strings.TrimSuffix(seg[0], "s") + "-file"
		letter := map[string]byte{"item-file": 'i', "disagreement-file": 'd'}[l.class]
		if len(seg) != 4 || !reYear.MatchString(seg[1]) || !reMonth.MatchString(seg[2]) || !named(l, seg[3], letter) {
			l.bad = "the path is not " + seg[0] + "/<yyyy>/<mm>/<id>-<slug>.md"
		} else {
			l.ym = seg[1] + "/" + seg[2]
		}
	case seg[0] == "sessions" && inProject:
		l.class = "session-file"
		name := strings.TrimSuffix(seg[len(seg)-1], ".md")
		parts := strings.Split(name, ".")
		if len(seg) != 4 || !reYear.MatchString(seg[1]) || !reMonth.MatchString(seg[2]) || !strings.HasSuffix(seg[3], ".md") || len(parts) != 3 {
			l.bad = "the path is not sessions/<yyyy>/<mm>/<host>.<skey>.<writer>.md"
			return
		}
		l.ym, l.host, l.skey, l.writer = seg[1]+"/"+seg[2], parts[0], parts[1], parts[2]
		if !reWriter.MatchString(l.writer) {
			l.bad = "the name's writer is not a writer id"
		}
	}
}

// named reads <id>-<slug>.md into l and reports whether it has that form.
func named(l *loc, name string, letter byte) bool {
	base, ok := strings.CutSuffix(name, ".md")
	if !ok || len(base) < 16 || base[14] != '-' {
		return false
	}
	l.id, l.slug = base[:14], base[15:]
	return validID(l.id, letter) && reSlug.MatchString(l.slug) && len(l.slug) <= 48
}

func itoa(n int) string { return strconv.Itoa(n) }
