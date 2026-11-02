package knowledgepage

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"time"

	"github.com/heyNag/eva-md/internal/coderef"
	"github.com/heyNag/eva-md/internal/identity"
	"github.com/heyNag/eva-md/internal/okfadapter"
)

// The actions of an operation.
const (
	Add       = "add"
	Supersede = "supersede"
	Drop      = "drop"
)

// Why Save passed over an operation.
const (
	ReasonLocked    = "locked"     // a person locked the page
	ReasonVerified  = "verified"   // a person verified the page
	ReasonDamaged   = "damaged"    // okf or eva-md can't read the page back safely
	ReasonOtherType = "other type" // the file is not a knowledge page
	ReasonMissing   = "missing"    // no page has the id
	ReasonRetired   = "retired"    // the page was retired already
	ReasonDuplicate = "duplicate"  // an active page holds the statement already
	ReasonSame      = "same"       // the page holds the new statement already
	ReasonInvalid   = "invalid"    // the operation lacks a kind, title, text, tag or action
)

// maxSaves is how many times Save tries a page that changes under it.
const maxSaves = 3

// update writes a page's change; tests make it fail.
var update = (*okfadapter.Wiki).Update

// Op is one operation the update validated: add a page, supersede a page's
// statement with a new one, or drop (retire) a page. Refs are the inbox
// refs it cites, already resolved to s:<session>#<n>, and At the time of
// the newest entry it cites, in that entry's own offset.
type Op struct {
	Action, Kind, Title, Text, Tag, Scope, Reason string
	// Project, when set, moves a superseded page to that project key: the
	// update sets it when the user narrows a page for all projects to the
	// project of the chat.
	Project string
	ID      int // the page a supersede or drop names
	Refs    []string
	At      time.Time
	Paths   []string // code paths the entries are about
}

// SaveMeta is what Save stamps on the pages it writes.
type SaveMeta struct {
	Project string           // the project key a new page is learned in; identity.GeneralKey when empty
	Commit  string           // the batch's HEAD, which code sources cite; without one, paths are left out
	Actor   string           // generated.by of the pages, as okfadapter.Actor makes it
	Version string           // eva-md's version, the map's author
	Now     func() time.Time // the clock for a date no entry gives; time.Now when nil
}

// Skip is an operation Save passed over: its place among the operations,
// the page it named (0 for an add) and why.
type Skip struct {
	Op, ID int
	Reason string
}

// Result is what Save did.
type Result struct {
	Created, Superseded, Dropped []int // the pages each kind of operation wrote
	Skipped                      []Skip
	Pages                        int      // the knowledge pages written
	PathsDropped                 int      // paths left out of code sources: no commit, or not a path a source can cite
	Files                        []string // every file written, by its path in the wiki
	Refresh                      Report   // the map, the indexes and the recall index after the operations
}

// Save applies the operations to the knowledge pages under one hold of the
// wiki lock, then builds the map, the folder's index, the recall index and,
// once, the root index's line again. It is idempotent: an add whose
// statement an active page holds, a supersede whose page holds its new
// statement and a drop of a retired page are passed over, so running the
// same operations again writes nothing. A new page takes one more than the
// largest number of any k<N>.md in the folder, so an id is never used twice.
// A locked, verified, damaged or other page is never written; its
// operations are passed over with the reason. When an operation fails, the
// map and the indexes are still built again for the pages already written,
// and the error is returned.
func Save(w *okfadapter.Wiki, ops []Op, meta SaveMeta) (Result, error) {
	var r Result
	err := w.Locked(func(lw *okfadapter.Wiki) error {
		s := &saver{w: lw, meta: meta, r: &r}
		if err := s.load(); err != nil {
			return err
		}
		var failed error
		for i, op := range ops {
			if failed = s.apply(i, op); failed != nil {
				break
			}
		}
		if failed != nil && r.Pages == 0 {
			return failed
		}
		rep, files, err := refresh(lw, meta.Version, true)
		r.Refresh = rep
		r.Files = append(r.Files, files...)
		return errors.Join(failed, err)
	})
	return r, err
}

// saver is one Save's view of the pages, kept current as it writes them.
type saver struct {
	w     *okfadapter.Wiki
	meta  SaveMeta
	r     *Result
	pages map[int]*page
	next  int
}

// page is a knowledge page as Save last read it.
type page struct {
	pg    *okfadapter.Page
	entry Entry
	skip  string // why it is never written, or ""
}

// modeled reports a page whose entry holds what it says: one eva-md reads
// back safely, or a damaged one it reads leniently (see readDamaged).
func (p *page) modeled() bool {
	return p.skip != ReasonDamaged && p.skip != ReasonOtherType || p.entry.damaged
}

// load reads every page of the folder and finds the next id.
func (s *saver) load() error {
	s.pages = map[int]*page{}
	ids, top, err := listIDs(s.w)
	if err != nil {
		return err
	}
	s.next = top + 1
	for _, id := range ids {
		p, err := readPage(s.w, id)
		if err != nil {
			return err
		}
		if p != nil {
			s.pages[id] = p
		}
	}
	return nil
}

// listIDs lists the numbers of the folder's regular k<N>.md files, sorted,
// and the largest number of any k<N>.md entry in it, whatever its type, so
// no id is used twice.
func listIDs(w *okfadapter.Wiki) (ids []int, top int, err error) {
	files, err := os.ReadDir(filepath.Join(w.Root(), Folder))
	if errors.Is(err, fs.ErrNotExist) {
		return nil, 0, nil
	}
	if err != nil {
		return nil, 0, err
	}
	for _, f := range files {
		id, ok := ParseID(f.Name())
		if !ok || !strings.HasSuffix(f.Name(), ".md") {
			continue
		}
		top = max(top, id)
		if f.Type().IsRegular() {
			ids = append(ids, id)
		}
	}
	slices.Sort(ids)
	return ids, top, nil
}

// readPage reads one page, or nil when it is gone. A page that can't be
// read, or that eva-md must not write, comes back with the reason; a
// damaged page okf reads as a knowledge page comes back with its entry as
// readDamaged reads it.
func readPage(w *okfadapter.Wiki, id int) (*page, error) {
	pg, err := w.ReadMax(Ref(id), maxPage)
	var pathErr *fs.PathError
	switch {
	case errors.Is(err, fs.ErrNotExist):
		return nil, nil
	case errors.As(err, &pathErr):
		return nil, err
	case err != nil:
		return &page{skip: ReasonDamaged}, nil
	}
	p := &page{pg: pg}
	p.entry, err = Read(pg.Concept)
	switch {
	case errors.Is(err, ErrOtherType):
		p.skip = ReasonOtherType
	case err != nil:
		p.skip = ReasonDamaged
		if e, ok := readDamaged(pg.Concept); ok {
			p.entry = e
		}
	case p.entry.Locked:
		p.skip = ReasonLocked
	case p.entry.Verified:
		p.skip = ReasonVerified
	}
	return p, nil
}

func (s *saver) skip(i, id int, reason string) {
	s.r.Skipped = append(s.r.Skipped, Skip{Op: i, ID: id, Reason: reason})
}

func (s *saver) apply(i int, op Op) error {
	switch op.Action {
	case Add:
		return s.add(i, op)
	case Supersede, Drop:
		return s.change(i, op)
	}
	s.skip(i, op.ID, ReasonInvalid)
	return nil
}

// add creates a page, unless an active page holds the statement already.
func (s *saver) add(i int, op Op) error {
	title, text := cleanText(op.Title, MaxTitle), cleanText(op.Text, MaxStatement)
	if !kinds[op.Kind] || !tags[op.Tag] || title == "" || text == "" {
		s.skip(i, 0, ReasonInvalid)
		return nil
	}
	if s.holds(text) {
		s.skip(i, 0, ReasonDuplicate)
		return nil
	}
	e := s.added(op)
	for n := 1; ; n++ {
		e.ID = s.next
		s.next++
		c := &okfadapter.Concept{}
		e.set(c)
		err := s.w.Create(e.Ref(), c, s.actor())
		if errors.Is(err, okfadapter.ErrExists) && n < maxSaves {
			continue // a file took the id meanwhile
		}
		if err != nil {
			return err
		}
		break
	}
	s.r.Created = append(s.r.Created, e.ID)
	return s.wrote(e.ID)
}

// added is the entry an add creates, before it has an id.
func (s *saver) added(op Op) Entry {
	refs := cleanRefs(op.Refs)
	e := Entry{
		Kind: op.Kind, Title: cleanText(op.Title, MaxTitle), Statement: cleanText(op.Text, MaxStatement), Tag: op.Tag,
		Scope: scope(op.Scope, ScopeProject), Project: s.project(), State: Active, AsOf: s.date(op.At), At: canonical(op.At),
		Refs: refs, Sources: refs,
	}
	e.cite(s.code(op.Paths))
	return e
}

// project is the project key a new page is learned in.
func (s *saver) project() string {
	if s.meta.Project == "" {
		return identity.GeneralKey
	}
	return s.meta.Project
}

// cite sets the entry's code sources and the paths they cite.
func (e *Entry) cite(code []string) {
	e.Code = code
	_, _, e.Paths, _ = splitSources(code)
}

// holds reports whether an active page that applies to the save's project,
// one for all projects or the project's own, holds the statement. A locked,
// verified or damaged page counts, as the recall index lists it.
func (s *saver) holds(text string) bool {
	key := normalize(text)
	for _, p := range s.pages {
		e := p.entry
		if p.modeled() && e.State == Active && (e.Scope == ScopeAll || e.Project == s.project()) && normalize(e.Statement) == key {
			return true
		}
	}
	return false
}

// change supersedes or drops a page, reading it again when it changed
// meanwhile.
func (s *saver) change(i int, op Op) error {
	for n := 1; ; n++ {
		p := s.pages[op.ID]
		switch {
		case p == nil:
			s.skip(i, op.ID, ReasonMissing)
			return nil
		case p.skip != "":
			s.skip(i, op.ID, p.skip)
			return nil
		case p.entry.State == Retired:
			s.skip(i, op.ID, ReasonRetired)
			return nil
		}
		e, reason := s.changed(p.entry, op)
		if reason != "" {
			s.skip(i, op.ID, reason)
			return nil
		}
		wrote, err := update(s.w, e.Ref(), p.pg.Hash, func(c *okfadapter.Concept) error { e.set(c); return nil }, s.actor())
		if errors.Is(err, okfadapter.ErrConflict) && n < maxSaves {
			if err := s.reread(op.ID); err != nil {
				return err
			}
			continue
		}
		if err != nil {
			return err
		}
		if !wrote {
			s.skip(i, op.ID, ReasonSame)
			return nil
		}
		if op.Action == Supersede {
			s.r.Superseded = append(s.r.Superseded, op.ID)
		} else {
			s.r.Dropped = append(s.r.Dropped, op.ID)
		}
		return s.wrote(op.ID)
	}
}

// changed is the entry after a supersede or a drop, or why it is passed
// over.
func (s *saver) changed(e Entry, op Op) (Entry, string) {
	refs := cleanRefs(op.Refs)
	if !tags[op.Tag] {
		return e, ReasonInvalid
	}
	when := s.date(op.At)
	switch op.Action {
	case Supersede:
		text := cleanText(op.Text, MaxStatement)
		if text == "" {
			return e, ReasonInvalid
		}
		if normalize(text) == normalize(e.Statement) {
			return e, ReasonSame
		}
		e.History = append(slices.Clone(e.History), historyLine("~~"+e.Statement+"~~ → "+text+" (superseded "+when+")", op.Tag, refs))
		e.Statement, e.Tag, e.Refs, e.At, e.AsOf = text, op.Tag, refs, canonical(op.At), when
		if kinds[op.Kind] {
			e.Kind = op.Kind
		}
		if title := cleanText(op.Title, MaxTitle); title != "" {
			e.Title = title
		}
		e.Scope = scope(op.Scope, e.Scope)
		if op.Project != "" {
			e.Project = op.Project
		}
		if code := s.code(op.Paths); len(code) > 0 {
			e.cite(code)
		}
	case Drop:
		why := "(retired " + when + ")"
		if reason := cleanText(op.Reason, MaxReason); reason != "" {
			why = "(retired " + when + ": " + reason + ")"
		}
		e.History = append(slices.Clone(e.History), historyLine("~~"+e.Statement+"~~ "+why, op.Tag, refs))
		e.State = Retired
	}
	e.Sources = mergeRefs(e.Sources, refs)
	return e, ""
}

// historyLine is a History line: "- <text> [<tag>] <refs>".
func historyLine(text, tag string, refs []string) string {
	return strings.Join(slices.Concat([]string{"- " + text, "[" + tag + "]"}, refs), " ")
}

// mergeRefs adds refs to the end of old, each once, keeping the newest
// MaxSources.
func mergeRefs(old, refs []string) []string {
	var out []string
	for _, r := range old {
		if !slices.Contains(refs, r) {
			out = append(out, r)
		}
	}
	out = append(out, refs...)
	return out[max(len(out)-MaxSources, 0):]
}

// cleanRefs keeps the refs that are inbox refs, each once, at most
// MaxSources.
func cleanRefs(refs []string) []string {
	var out []string
	for _, r := range refs {
		if r = strings.TrimSpace(r); refPattern.MatchString(r) && !slices.Contains(out, r) {
			out = append(out, r)
		}
	}
	return out[max(len(out)-MaxSources, 0):]
}

// code is the code sources for the paths, <project>@<commit>:<path>, each
// a source coderef.Parse reads back, at most MaxPaths. Without a commit, or
// for a path no source can cite, the path is left out and counted.
func (s *saver) code(paths []string) []string {
	var out []string
	for _, p := range paths {
		p = strings.TrimSuffix(strings.TrimPrefix(strings.TrimSpace(p), "./"), "/")
		if p == "" {
			continue
		}
		project := s.meta.Project
		source := project + "@" + s.meta.Commit + ":" + p
		r, ok := coderef.Parse(source)
		if s.meta.Commit == "" || !ok || r.Key != project || r.Path != p || len(out) == MaxPaths || slices.Contains(out, source) {
			s.r.PathsDropped++
			continue
		}
		out = append(out, source)
	}
	return out
}

// reread reads a page again after it changed under Save.
func (s *saver) reread(id int) error {
	p, err := readPage(s.w, id)
	if err != nil {
		return err
	}
	if p == nil {
		delete(s.pages, id)
	} else {
		s.pages[id] = p
	}
	return nil
}

// wrote counts a page written and reads it back, so a later operation in
// the same Save sees it as saved.
func (s *saver) wrote(id int) error {
	s.r.Pages++
	s.r.Files = append(s.r.Files, Ref(id)+".md")
	return s.reread(id)
}

// date is the day of an entry's time in its own offset, or today when no
// entry gave one.
func (s *saver) date(t time.Time) string {
	if t.IsZero() {
		now := time.Now
		if s.meta.Now != nil {
			now = s.meta.Now
		}
		return now().Format(time.DateOnly)
	}
	return canonical(t).Format(time.DateOnly)
}

// actor is generated.by of the pages Save writes.
func (s *saver) actor() string {
	if s.meta.Actor == "" {
		return okfadapter.Actor("eva-md", s.meta.Version)
	}
	return s.meta.Actor
}

// scope is s when it is a scope, and otherwise fallback.
func scope(s, fallback string) string {
	if scopes[s] {
		return s
	}
	return fallback
}

// String names a skip without any page text, for a log line.
func (k Skip) String() string {
	if k.ID == 0 {
		return fmt.Sprintf("op %d (a new page): %s", k.Op, k.Reason)
	}
	return fmt.Sprintf("op %d on %s: %s", k.Op, Name(k.ID), k.Reason)
}

// Written returns every line Save writes for op, so a caller can check them
// before the save, as the daily commit's secret scan does: the page as saved
// apart from its generated line, its History line among them, the page's
// line in the folder's index, on the map, in the recall index (for an active
// page) and, for a new page, in log.md. A supersede or a drop acts on on, an
// add on a new page with on's id, or k1. It reports nothing for an operation
// Save would pass over as invalid or the same.
func Written(op Op, on Entry, meta SaveMeta) []string {
	s := &saver{meta: meta, r: &Result{}}
	var e Entry
	switch op.Action {
	case Add:
		e = s.added(op)
		e.ID = max(on.ID, 1)
	case Supersede, Drop:
		var reason string
		if e, reason = s.changed(on, op); reason != "" {
			return nil
		}
	default:
		return nil
	}
	file := e.Name() + ".md"
	lines := strings.Split(e.Render(), "\n")
	lines = append(lines, okfadapter.IndexLine(mapText(e.Title), file, e.Statement), listed{id: e.ID, entry: e, readable: true}.mapLine())
	if e.State == Active {
		lines = append(lines, e.row().line())
	}
	if op.Action == Add {
		lines = append(lines, "* **Creation**: Documented concept `"+Folder+"/"+file+"` ("+e.Title+").")
	}
	return lines
}
