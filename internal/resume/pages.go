package resume

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"io"
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/heyNag/eva-md/internal/identity"
	"github.com/heyNag/eva-md/internal/okfadapter"
	"github.com/heyNag/eva-md/internal/threadpage"
)

// pageSource is one thread page or system page in a search's scope.
type pageSource struct {
	rel, id string // its path in the wiki, and its page id: the path without ".md"
	folder  string // "general", the project's slug, or "systems"
	name    string // its file name without ".md", the branch when the page names none
	mod     time.Time
	system  bool // a system page
}

// searchPages scans the thread pages in scope: the project's and the
// general area's, or every project's with All. The project's folder is found
// as the brief finds it, through a now page; when none is found and the
// project's own folder holds threads, the trailer says they were not
// searched. The system pages, global to the wiki, are in every scope. Each
// page is read on its own, line by line, newest first; the wiki is never
// loaded whole.
func (s *scan) searchPages(ctx context.Context, o SearchOptions) {
	if o.Config.WikiPath == "" {
		s.noWiki = true
		return
	}
	root, err := openWiki(o.Config)
	if err != nil {
		s.badWiki = "the wiki is unreadable"
		if errors.Is(err, errInstance) {
			s.badWiki = "the wiki is not the instance the config names"
		}
		return
	}
	defer root.Close()
	s.wiki = true
	folders := []string{"general"}
	switch {
	case o.All:
		names, err := dirNames(root, "projects")
		if err != nil && !errors.Is(err, fs.ErrNotExist) {
			s.badFolders = append(s.badFolders, "projects")
		}
		for _, name := range names {
			folders = append(folders, "projects/"+name)
		}
	case o.Project.Key != identity.GeneralKey:
		slug, ok := "", false
		if w, err := okfadapter.Open(o.Config.WikiPath, o.Dirs.Locks()); err == nil {
			slug, ok = projectSlug(ctx, w, o.Project, slugCandidates(root, o.Project.Slug))
		}
		own := o.Project.Slug
		if ok {
			folders = append(folders, "projects/"+slug)
		} else if info, err := root.Stat(filepath.Join("projects", own, "threads")); err == nil && info.IsDir() && ctx.Err() == nil && own != "" && !strings.HasPrefix(own, ".") && plainName(own) {
			s.unproved = "projects/" + own
		}
	}
	var pages []pageSource
	for _, folder := range folders {
		pages = append(pages, s.listPages(root, folder)...)
	}
	pages = append(pages, s.listSystems(root)...)
	sort.Slice(pages, func(i, j int) bool {
		if !pages[i].mod.Equal(pages[j].mod) {
			return pages[i].mod.After(pages[j].mod)
		}
		return pages[i].rel < pages[j].rel
	})
	for _, src := range pages {
		switch s.cut = ctx.Err() != nil; {
		case s.cut:
		case src.system:
			s.systemFile(ctx, root, src, o.Now.Location())
		default:
			s.pageFile(ctx, root, src, o.Now.Location())
		}
		if s.cut {
			s.cutPages = true
			return
		}
	}
}

// dirNames returns the folders in dir whose names can be part of a page id:
// no leading dot, and no control, format or space character.
func dirNames(root *os.Root, dir string) ([]string, error) {
	d, err := root.Open(dir)
	if err != nil {
		return nil, err
	}
	entries, err := d.ReadDir(-1)
	d.Close()
	var names []string
	for _, e := range entries {
		if e.IsDir() && !strings.HasPrefix(e.Name(), ".") && plainName(e.Name()) {
			names = append(names, e.Name())
		}
	}
	return names, err
}

// listPages returns the pages in a folder's threads/ that changed within
// the window.
func (s *scan) listPages(root *os.Root, folder string) []pageSource {
	pages := s.listFiles(root, folder+"/threads", true, func(name string) bool { return name != "index" && !strings.HasPrefix(name, ".") })
	for i := range pages {
		pages[i].folder = path.Base(folder)
	}
	return pages
}

// listFiles returns the pages in dir whose names, without ".md", keep
// accepts, and, when windowed, that changed within the window, by
// modification time. A page that is not a regular file is named as
// unreadable, and so is a folder that can't be listed; a folder that doesn't
// exist holds no pages.
func (s *scan) listFiles(root *os.Root, dir string, windowed bool, keep func(name string) bool) []pageSource {
	d, err := root.Open(filepath.FromSlash(dir))
	var entries []fs.DirEntry
	if err == nil {
		entries, err = d.ReadDir(-1)
		d.Close()
	}
	if err != nil && !errors.Is(err, fs.ErrNotExist) {
		s.badFolders = append(s.badFolders, dir)
	}
	var out []pageSource
	for _, e := range entries {
		name, ok := strings.CutSuffix(e.Name(), ".md")
		if !ok || !keep(name) {
			continue
		}
		src := pageSource{rel: dir + "/" + e.Name(), id: dir + "/" + name, name: name}
		info, err := e.Info()
		if err != nil || !e.Type().IsRegular() {
			s.badPages = append(s.badPages, src.id)
			continue
		}
		if windowed && info.ModTime().Before(s.q.since) {
			continue
		}
		src.mod = info.ModTime()
		out = append(out, src)
	}
	return out
}

// pageFile scans one thread page: the as_of and branch in its frontmatter,
// then its body through threadpage.Scanner, one line at a time. Line numbers
// are physical and 1-based. A page that fails to open or read is named as
// unreadable.
func (s *scan) pageFile(ctx context.Context, root *os.Root, src pageSource, loc *time.Location) {
	f, err := regular(root, filepath.FromSlash(src.rel))
	if err != nil {
		s.badPages = append(s.badPages, src.id)
		return
	}
	defer f.Close()
	r := bufio.NewReaderSize(f, 64<<10)
	var sc threadpage.Scanner
	meta := pageMeta{branch: src.name}
	front := false
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
			continue
		case front:
			meta.read(bare)
			continue
		}
		if u, ok := sc.Line(text); ok {
			s.pageUnit(u, src, meta, n, loc)
		}
	}
	s.pageCount++
}

// pageMeta is what a search takes from a thread page's frontmatter.
type pageMeta struct {
	branch string
	asOf   time.Time
}

// read takes the as_of or branch on one frontmatter line.
func (m *pageMeta) read(line string) {
	key, value, ok := frontValue(line)
	if !ok || key != "as_of" && key != "branch" {
		return
	}
	if key == "as_of" {
		m.asOf, _ = time.Parse(time.RFC3339Nano, value)
	} else if value != "" {
		m.branch = value
	}
}

// frontValue reads one frontmatter line, which okf writes as a key and a
// value, plain or as a JSON string.
func frontValue(line string) (key, value string, ok bool) {
	key, value, ok = strings.Cut(line, ":")
	if !ok {
		return "", "", false
	}
	value = strings.TrimSpace(value)
	var s string
	if json.Unmarshal([]byte(value), &s) == nil {
		value = s
	}
	return key, value, true
}

// pageUnit matches one Now item or Timeline line. A Now item is found at
// <page>#Now and dated by the user prompt it cites, or by the page's as_of
// when it cites none; the window tests the page's as_of, so an item in force
// stays findable while its page is current. A Timeline line is dated by its
// group's day and found at <page>#<day>. Its kind and text are matched, with
// the label of a line eva-md wrote itself; its snippet ends with its refs;
// its tag stands where an inbox hit's speaker does.
func (s *scan) pageUnit(u threadpage.Unit, src pageSource, m pageMeta, n int, loc *time.Location) {
	kind, text := u.Kind, u.Item.Text
	if u.Item.Code != "" {
		text = "eva-md: " + u.Item.Code + ": " + text
		if u.Item.Code == threadpage.OverCapCode {
			kind = "decision" // kept under Open questions, but a decision
		}
	}
	if kind != "" {
		text = kind + ": " + text
	}
	ts, day, ref := m.asOf, false, src.id+"#Now"
	if !u.Now {
		ts, day, ref = time.Time{}, true, src.id+"#"+u.Date
		if d, err := time.ParseInLocation(time.DateOnly, u.Date, loc); err == nil {
			ts = d
		}
	}
	if !s.q.since.IsZero() {
		end := ts
		if day {
			end = ts.AddDate(0, 0, 1)
		}
		if ts.IsZero() || !end.After(s.q.since) {
			return
		}
	}
	if u.Now && !u.Item.At.IsZero() {
		ts = u.Item.At
	}
	tag := u.Item.Tag
	if tag == "" {
		tag = "untagged"
	}
	shown := strings.Join(append([]string{text}, u.Item.Refs...), " ")
	s.match(unit{text: text, shown: shown, ts: ts, day: day, role: tag, branch: m.branch, project: src.folder, ref: ref, file: src.rel, line: n, page: true})
}
