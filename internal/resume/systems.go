package resume

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"time"

	"github.com/heyNag/eva-md/internal/coderef"
	"github.com/heyNag/eva-md/internal/systempage"
)

// System pages, systems/<slug>.md, are the wiki's shared understanding of the
// systems its projects belong to (see internal/systempage). They are global
// to the wiki: a brief points at the project's from the system map alone,
// and every search scope reads them, one page at a time, as it reads thread
// pages.

const (
	// maxPointers is how many systems a brief points at.
	maxPointers = 3
	// pointerCap bounds one pointer, and systemsCap the pointers and the
	// map's line together, on both hosts.
	pointerCap = 200
	systemsCap = 600
	// mapCap bounds the system map a brief reads.
	mapCap = 256 << 10
	// minDescription is the shortest cut of a description a pointer keeps.
	minDescription = 20
)

// The bounds of measuring the code a system cites for the brief: only while
// at least codeLeft remains before the deadline, less gitReserve, at most
// CodeCall a git call and CodeBudget in all; the first call that runs out
// of time stops the rest. Tests raise CodeCall and CodeBudget before any
// test runs, so a slow machine can't fail a test of what the brief says; a
// test of the bounds passes its own (see codeClock).
var (
	codeLeft   = 200 * time.Millisecond
	CodeCall   = 60 * time.Millisecond
	CodeBudget = 150 * time.Millisecond
)

// systemPointers returns the brief's pointers to the system pages: a line for
// each of up to 3 systems whose repos include the project, in the map's
// order, then one for the map, as long as the map lists a system. A pointer
// to a system that cites the project's code says how far that code has
// moved (see codeNote); head is the commit HEAD names, when live git read
// it. A line that cleaning as wiki text would change is left out, so the
// length counted is the length shown, identifiers such as a slug included.
func systemPointers(ctx context.Context, o Options, head string) string {
	return pointersWithin(ctx, o, head, &codeClock{left: codeLeft, call: CodeCall, budget: CodeBudget})
}

// pointersWithin is systemPointers counting code within clock's bounds.
func pointersWithin(ctx context.Context, o Options, head string, clock *codeClock) string {
	entries := readMap(ctx, o)
	if len(entries) == 0 {
		return ""
	}
	bin := filepath.Join(o.Dirs.Share, "bin", "eva-md")
	last := asShown(fmt.Sprintf("System map: %d %s: %s show systems/map", len(entries), plural(len(entries), "system"), bin))
	if last == "" || runes(last) > systemsCap {
		return ""
	}
	left := systemsCap - runes(last)
	var lines []string
	saved := &savedStatus{state: o.Dirs.State}
	for _, e := range entries {
		if len(lines) == maxPointers {
			break
		}
		if !slices.Contains(e.Repos, o.Project.Key) {
			continue
		}
		if line := asShown(pointer(e, codeNote(ctx, o, e, head, clock, saved), bin, min(pointerCap, left-1))); line != "" {
			lines = append(lines, line)
			left -= runes(line) + 1
		}
	}
	return strings.Join(append(lines, last), "\n")
}

// codeNote says how far the project's code a system cites has moved since
// its page's as_of, as its pointer shows it (see coderef.Note): one count in
// the project's worktree for each commit of the project's repo the page
// cites, with the paths it cites there, and none when HEAD is that commit.
// A count of none is not measured, as the code status has it, when the
// saved status found the commit lacks a path the group cites, or found the
// commit off the branch at the HEAD live git read. It is "" when the page
// cites none of the project's code, a count fails or the brief has no time
// left for one, and the pointer then shows the day.
func codeNote(ctx context.Context, o Options, e systempage.MapEntry, head string, clock *codeClock, saved *savedStatus) string {
	var refs []coderef.Ref
	for _, r := range e.Code {
		if r.Key == o.Project.Key {
			refs = append(refs, r)
		}
	}
	if len(refs) == 0 || o.Project.Root == "" {
		return ""
	}
	var t coderef.Tally
	for _, group := range coderef.Groups(refs) {
		m, ok := coderef.Movement{}, true
		if head == "" || !strings.HasPrefix(head, group[0].Commit) {
			m, ok = clock.count(ctx, o.Project.Root, group)
		}
		if ok && m == (coderef.Movement{}) {
			r, found := saved.entry(e.Slug, group)
			ok = !found || !r.Lacks() && !(r.State == coderef.NotOnBranch && head != "" && r.Head == head)
		}
		t.Add(m, ok)
	}
	m, ok := t.Result()
	if !ok {
		return ""
	}
	return coderef.Note(e.AsOf, m)
}

// savedStatus is the code status saved in the state folder, read once, when
// a brief first needs it.
type savedStatus struct {
	state  string
	read   bool
	report coderef.Report
}

// entry returns the saved entry of a page's code in one group, at the same
// repo, commit and paths.
func (s *savedStatus) entry(slug string, group []coderef.Ref) (coderef.RepoStatus, bool) {
	if !s.read {
		s.read = true
		s.report, _, _ = coderef.Load(s.state, coderef.Limit)
	}
	var paths []string
	for _, r := range group {
		if r.Path != "" {
			paths = append(paths, r.Path)
		}
	}
	for _, p := range s.report.Pages {
		if p.Slug != slug {
			continue
		}
		for _, r := range p.Repos {
			if r.Key == group[0].Key && r.Commit == group[0].Commit && slices.Equal(r.Paths, paths) {
				return r, true
			}
		}
	}
	return coderef.RepoStatus{}, false
}

// codeClock keeps the brief's counts of code within their bounds: a count
// only while at least left remains before the deadline, less gitReserve, at
// most call a git call and budget in all.
type codeClock struct {
	left, call, budget time.Duration
	spent              time.Duration
	stopped            bool
}

// count counts the commits since a group's commit that touch its paths, in
// the worktree at root, and reports false when it can't within the bounds.
func (c *codeClock) count(ctx context.Context, root string, group []coderef.Ref) (coderef.Movement, bool) {
	if deadline, ok := ctx.Deadline(); ok && time.Until(deadline)-gitReserve < c.left {
		c.stopped = true
	}
	rest := c.budget - c.spent
	if c.stopped || rest <= 0 || ctx.Err() != nil {
		return coderef.Movement{}, false
	}
	var paths []string
	for _, r := range group {
		if r.Path != "" {
			paths = append(paths, r.Path)
		}
	}
	call, cancel := context.WithTimeout(ctx, min(c.call, rest))
	defer cancel()
	start := time.Now()
	m, err := coderef.Commits(call, root, group[0].Commit, paths)
	c.spent += time.Since(start)
	if errors.Is(err, coderef.ErrTimeout) {
		c.stopped = true
	}
	return m, err == nil
}

// asShown returns line when cleaning it as wiki text leaves it as it is, and
// "" otherwise.
func asShown(line string) string {
	if cleanPage(line) != line {
		return ""
	}
	return line
}

// pointer is a system's line, "System: <title>: <description> (<note>):
// <bin> show systems/<slug>", in at most limit runes, where the note says
// how far the code it cites has moved, or without one the day, "as of
// <day>". The description is cut to fit or left out, then the note gives way
// to the day, then the day goes; the title is cut last and the command
// never, so a line without room for the command is "".
func pointer(e systempage.MapEntry, note, bin string, limit int) string {
	title, desc := oneLine(e.Title, pointerCap), oneLine(e.Description, pointerCap)
	if title == "" {
		title = e.Slug
	}
	day := ""
	if _, err := time.Parse(time.DateOnly, e.AsOf); err == nil {
		day = " (as of " + e.AsOf + ")"
	}
	marks := []string{day, ""}
	if note != "" {
		marks = append([]string{" (" + note + ")"}, marks...)
	}
	head, tail := "System: "+title, ": "+bin+" show systems/"+e.Slug
	if desc != "" {
		if room := limit - runes(head+": "+marks[0]+tail); room >= min(runes(desc), minDescription) {
			return head + ": " + cut(desc, room) + marks[0] + tail
		}
	}
	for _, mark := range marks {
		if line := head + mark + tail; runes(line) <= limit {
			return line
		}
	}
	if room := limit - runes("System: "+tail); room > 0 {
		return "System: " + cut(title, room) + tail
	}
	return ""
}

// CodeMoves returns, by slug, how far the code each system page cites had
// moved when the code status was taken, for the pages whose pointers say it
// (see coderef.PageStatus.MovementSince). It reads the system map and the
// status file in eva-md's state folder, never git, so search hits and
// eva_status say what the prompt pointers say. A map or status that can't be
// read gives none.
func CodeMoves(ctx context.Context, o Options) map[string]coderef.Movement {
	entries := readMap(ctx, o)
	if len(entries) == 0 {
		return nil
	}
	r, ok, err := coderef.Load(o.Dirs.State, coderef.Limit)
	if err != nil || !ok {
		return nil
	}
	status := make(map[string]coderef.PageStatus, len(r.Pages))
	for _, p := range r.Pages {
		status[p.Slug] = p
	}
	moves := map[string]coderef.Movement{}
	for _, e := range entries {
		if m, ok := status[e.Slug].MovementSince(e.AsOf, e.Code); ok {
			moves[e.Slug] = m
		}
	}
	return moves
}

// readMap returns the systems the system map lists. It reads systems/map.md
// by its path through the wiki's pinned root, once, and nothing else of the
// systems folder: no listing and no page. A map that is missing, linked,
// not a regular file, unreadable or over 256 KB lists none.
func readMap(ctx context.Context, o Options) []systempage.MapEntry {
	if o.Config.WikiPath == "" || ctx.Err() != nil {
		return nil
	}
	root, err := openWiki(o.Config)
	if err != nil {
		return nil
	}
	defer root.Close()
	f, err := regular(root, filepath.Join("systems", "map.md"))
	if err != nil {
		return nil
	}
	defer f.Close()
	info, err := f.Stat()
	if err != nil || info.Size() > mapCap {
		return nil
	}
	data, err := io.ReadAll(io.LimitReader(f, mapCap+1))
	if err != nil || len(data) > mapCap {
		return nil
	}
	lines := strings.Split(string(data), "\n")
	// The map's own frontmatter is no line of the map.
	if lines[0] == "---" {
		if end := slices.Index(lines[1:], "---"); end >= 0 {
			lines = lines[end+2:]
		}
	}
	var out []systempage.MapEntry
	for _, l := range lines {
		if e, ok := systempage.ParseMapLine(strings.TrimRight(l, "\r")); ok {
			out = append(out, e)
		}
	}
	return out
}

// listSystems returns the system pages, as listPages returns thread pages
// but whatever the window: they hold the wiki's lasting understanding, which
// an old as_of does not make stale.
func (s *scan) listSystems(root *os.Root) []pageSource {
	pages := s.listFiles(root, "systems", false, func(name string) bool { return systempage.CheckSlug(name) == nil })
	for i := range pages {
		pages[i].folder, pages[i].system = "systems", true
	}
	return pages
}

// systemFile scans one system page a line at a time: the description and
// as_of in its frontmatter, then each line of its sections, which end where
// eva-md show ends them, except the map's link code ends every page with. A
// line's ref names its section by its anchor, one word to a shell. A page
// that fails to open or read is named as unreadable.
func (s *scan) systemFile(ctx context.Context, root *os.Root, src pageSource, loc *time.Location) {
	f, err := regular(root, filepath.FromSlash(src.rel))
	if err != nil {
		s.badPages = append(s.badPages, src.id)
		return
	}
	defer f.Close()
	r := bufio.NewReaderSize(f, 64<<10)
	var o outline
	var asOf time.Time
	description, at := "", 0
	front, section := false, ""
	for n := 1; ; n++ {
		if ctx.Err() != nil {
			s.cut = true
			break
		}
		line, ok, size, err := readLine(r, &s.line, maxSearchLine, true)
		s.pageBytes += int64(size)
		if err != nil {
			if !errors.Is(err, io.EOF) {
				s.badPages = append(s.badPages, src.id)
				return
			}
			break
		}
		if !ok {
			continue
		}
		text := string(line)
		switch bare := strings.TrimRight(text, "\r"); {
		case n == 1 && bare == "---":
			front = true
			continue
		case front && bare == "---":
			front = false
			if description != "" {
				s.systemUnit(description, src.id, "system", asOf, src, at)
			}
			continue
		case front:
			switch key, value, _ := frontValue(bare); key {
			case "description":
				description, at = value, n
			case "as_of":
				asOf, _ = time.ParseInLocation(time.DateOnly, value, loc)
			}
			continue
		}
		if level, name, heading := o.heading(text); heading && level <= 2 {
			section = ""
			if level == 2 {
				section = name
			}
			continue
		}
		if t := strings.TrimSpace(text); section == "" || t == "" || t == systempage.MapLink {
			continue
		}
		tag := "system"
		if strings.EqualFold(section, "Unverified") {
			tag = "unverified"
		}
		s.systemUnit(text, src.id+"#"+anchor(section), tag, asOf, src, n)
	}
	s.systemCount++
}

// systemUnit matches one line of a system page, found at ref and dated by
// the page's as_of, which the window does not test. Its tag, system or
// unverified, stands where an inbox hit's speaker does.
func (s *scan) systemUnit(text, ref, tag string, asOf time.Time, src pageSource, n int) {
	s.match(unit{text: text, shown: text, ts: asOf, day: true, role: tag, project: src.folder, ref: ref, file: src.rel, line: n, page: true, system: true})
}
