// Package lint checks the wiki for what no single page write sees: thread
// pages grown past the size that calls for rotating their Timeline, and
// source refs the inbox no longer resolves. maintain runs it once a day and
// the doctor reads its last result.
package lint

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/heyNag/eva-md/internal/inbox"
	"github.com/heyNag/eva-md/internal/paths"
	"github.com/heyNag/eva-md/internal/threadpage"
)

// OversizeBytes is the size past which a thread page is flagged: the trigger
// for Timeline rotation, which is deferred.
const OversizeBytes = 512 << 10

// ref is a source ref, s:<session>#<line>.
var ref = regexp.MustCompile(`^s:([^#\s]+)#([0-9]+)$`)

// Result is the lint's report, saved as <state>/lint.json. It holds counts,
// page ids and the names of inbox project folders, never chat text.
type Result struct {
	At          string   `json:"at"`
	Oversize    []string `json:"oversize"` // thread pages over OversizeBytes
	BadRefs     int      `json:"bad_refs"` // refs whose line is past its file's end, or whose file this Mac had and lost
	BadRefPages []string `json:"bad_ref_pages"`
	// RemovedRefs are refs to chats eva-md inbox clear removed, and
	// ElsewhereRefs refs to chats this Mac never captured, as another Mac's
	// in a wiki two share, or chats from before this inbox. Neither is bad.
	RemovedRefs   int `json:"removed_refs"`
	ElsewhereRefs int `json:"elsewhere_refs"`
	// UnknownRefs are refs to chats the inbox's readable folders hold
	// nothing of while UnreadFolders, the inbox project folders the lint
	// couldn't read in full, is not empty: the chat may be in one of those,
	// so the lint can't tell it from another Mac's.
	UnknownRefs   int      `json:"unknown_refs"`
	UnreadFolders []string `json:"unreadable_inbox_folders"`
	Pages         int      `json:"pages"` // thread pages read
	DurationMS    int64    `json:"duration_ms"`
}

// Run lints the thread pages of the wiki at wiki against the inbox and saves
// the result as <state>/lint.json, mode 0600. It reads the inbox once,
// streaming each session file to count its lines, then each thread page on
// its own, line by line through threadpage.Scanner. It never loads the wiki
// whole or holds a session file in memory.
func Run(ctx context.Context, wiki string, dirs paths.Dirs) (Result, error) {
	start := time.Now()
	r := Result{At: start.Format(time.RFC3339), Oversize: []string{}, BadRefPages: []string{}, UnreadFolders: []string{}}
	s, err := readInbox(ctx, dirs.Inbox())
	if err != nil {
		return r, err
	}
	r.UnreadFolders = append(r.UnreadFolders, s.unread...)
	root, err := paths.OpenRoot(wiki, false)
	if err != nil {
		return r, fmt.Errorf("lint: open the wiki: %w", err)
	}
	defer root.Close()
	for _, id := range threadPages(root) {
		if err := ctx.Err(); err != nil {
			return r, err
		}
		var count [refKinds]int
		size, err := pageRefs(root, id, func(session string, line int) {
			count[s.judge(session, line)]++
		})
		if err != nil {
			continue
		}
		bad := count[refBad]
		r.RemovedRefs += count[refRemoved]
		r.ElsewhereRefs += count[refElsewhere]
		r.UnknownRefs += count[refUnknown]
		r.Pages++
		if size > OversizeBytes {
			r.Oversize = append(r.Oversize, id)
		}
		if bad > 0 {
			r.BadRefs += bad
			r.BadRefPages = append(r.BadRefPages, id)
		}
	}
	r.DurationMS = time.Since(start).Milliseconds()
	data, err := json.Marshal(r)
	if err != nil {
		return r, err
	}
	if err := paths.EnsureDir(dirs.State); err != nil {
		return r, err
	}
	return r, paths.WriteFileAtomic(filepath.Join(dirs.State, "lint.json"), append(data, '\n'), 0o600)
}

// sessions is what the lint knows of the sessions refs name, each by its
// id and by the first 8 characters a short ref names it by: the line count
// of each session file in the inbox (lines), the sessions whose file is
// gone while its watermark is here, so this Mac captured them and lost them
// (lost), and those eva-md inbox clear removed (removed). unread names the
// project folders it couldn't read in full, the folder itself, its
// watermarks or a session file in it, sorted.
type sessions struct {
	lines         ids[int]
	lost, removed ids[bool]
	unread        []string
}

// ids is a value for each session, by its id and by its short id.
type ids[V int | bool] struct{ full, prefix map[string]V }

func newIDs[V int | bool]() ids[V] {
	return ids[V]{full: map[string]V{}, prefix: map[string]V{}}
}

// short is the first 8 characters of a session id, which a short ref names.
func short(id string) string {
	if r := []rune(id); len(r) > 8 {
		return string(r[:8])
	}
	return id
}

// get finds the session a ref names: by its full id, or by a short one.
func (m ids[V]) get(id string) (V, bool) {
	v, ok := m.full[id]
	if !ok && len([]rune(id)) <= 8 {
		v, ok = m.prefix[id]
	}
	return v, ok
}

// readInbox counts the lines of every session file and notes the sessions
// lost and removed, and the project folders it couldn't read in full. A
// session's id is its file name without the host, as eva-md show finds a
// ref's file. It lists the inbox before it reads the record of removals,
// which a removal writes before its files go, so a session removed
// meanwhile is either still listed or already recorded. An inbox that
// doesn't exist holds no sessions.
func readInbox(ctx context.Context, dir string) (sessions, error) {
	s := sessions{lines: newIDs[int](), lost: newIDs[bool](), removed: newIDs[bool]()}
	store := inbox.Store{Root: dir}
	captures, unread, err := store.Captures()
	if err != nil {
		return s, fmt.Errorf("lint: read the inbox: %w", err)
	}
	s.unread = unread
	if len(captures) > 0 {
		root, err := paths.OpenRoot(dir, false)
		if err != nil {
			return s, fmt.Errorf("lint: open the inbox: %w", err)
		}
		defer root.Close()
		for _, c := range captures {
			if err := ctx.Err(); err != nil {
				return s, err
			}
			if !c.File {
				s.lost.full[c.ID], s.lost.prefix[short(c.ID)] = true, true
				continue
			}
			n, err := countLines(root, filepath.Join(c.Project, c.Name()))
			if err != nil {
				// Its lines can't be counted, so its folder wasn't read in
				// full.
				s.unread = append(s.unread, c.Project)
				continue
			}
			s.lines.full[c.ID] = max(s.lines.full[c.ID], n)
			s.lines.prefix[short(c.ID)] = max(s.lines.prefix[short(c.ID)], n)
		}
	}
	slices.Sort(s.unread)
	s.unread = slices.Compact(s.unread)
	removed, err := store.Removed()
	if err != nil {
		return s, fmt.Errorf("lint: %w", err)
	}
	for _, r := range removed {
		s.removed.full[r.Session], s.removed.prefix[short(r.Session)] = true, true
	}
	return s, nil
}

// countLines counts a file's lines as eva-md show numbers them: an
// unfinished last line counts.
func countLines(root *os.Root, name string) (int, error) {
	f, err := root.Open(name)
	if err != nil {
		return 0, err
	}
	defer f.Close()
	buf := make([]byte, 64<<10)
	n, last := 0, byte('\n')
	for {
		k, err := f.Read(buf)
		n += bytes.Count(buf[:k], []byte{'\n'})
		if k > 0 {
			last = buf[k-1]
		}
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			return 0, err
		}
	}
	if last != '\n' {
		n++
	}
	return n, nil
}

// What a ref names, as the lint sorts it.
const (
	refResolved  = iota // a line of a session file the inbox holds
	refBad              // a line past its file's end, or a session this Mac captured and lost
	refRemoved          // a session eva-md inbox clear removed
	refElsewhere        // a session this Mac never captured, as another Mac's
	refUnknown          // a session the inbox's readable folders hold nothing of, while some couldn't be read
	refKinds            // how many kinds there are
)

// judge sorts a ref to line of the session id, a line of -1 being one whose
// number can't be read. A session file here decides first: a ref to one of
// its lines resolves. A ref to a session removed with eva-md inbox clear
// counts as removed, whatever its line, since a chat still open when it was
// removed starts a new file. Otherwise a ref to a session whose file is here
// is past its end, and one to a session whose watermark alone is here names
// a chat this Mac captured, updated and lost, as a file deleted by hand
// leaves it: both are bad. A ref to a session this inbox holds nothing of
// names a chat this Mac never captured, as one another Mac captured for a
// wiki the two share, or one from before this inbox, as in a wiki set up
// again after the local data was removed; but while a project folder
// couldn't be read in full, its chat may be there, so it is unknown.
func (s sessions) judge(id string, line int) int {
	n, here := s.lines.get(id)
	_, removed := s.removed.get(id)
	_, lost := s.lost.get(id)
	switch {
	case here && line >= 1 && line <= n:
		return refResolved
	case removed:
		return refRemoved
	case here || lost:
		return refBad
	case len(s.unread) > 0:
		return refUnknown
	}
	return refElsewhere
}

// pageRefs reads one thread page, skipping its frontmatter, and calls visit
// with each ref in its Now items and Timeline lines: the session as the ref
// names it, and the line, or -1 when its number can't be read. It returns
// the page's size.
func pageRefs(root *os.Root, id string, visit func(session string, line int)) (size int64, err error) {
	f, err := root.Open(filepath.FromSlash(id) + ".md")
	if err != nil {
		return 0, err
	}
	defer f.Close()
	info, err := f.Stat()
	if err != nil || !info.Mode().IsRegular() {
		return 0, errors.New("lint: not a regular file")
	}
	r := bufio.NewReaderSize(f, 64<<10)
	var sc threadpage.Scanner
	front := false
	for n := 1; ; n++ {
		line, ok, err := readLine(r)
		if err != nil {
			return 0, err
		}
		if !ok {
			break
		}
		switch bare := strings.TrimRight(line, "\r"); {
		case n == 1 && bare == "---":
			front = true
			continue
		case front:
			front = bare != "---"
			continue
		}
		u, ok := sc.Line(line)
		if !ok {
			continue
		}
		for _, field := range u.Item.Refs {
			m := ref.FindStringSubmatch(field)
			if m == nil {
				continue
			}
			num, err := strconv.Atoi(m[2])
			if err != nil {
				num = -1
			}
			visit(m[1], num)
		}
	}
	return info.Size(), nil
}

// Citing counts the refs in the thread pages of the wiki at wiki that name
// one of the sessions ids, by its full id or by the first 8 characters a
// short ref names it by, reading the pages as the lint does, and lists the
// pages holding them. eva-md inbox clear says so before it removes them.
func Citing(ctx context.Context, wiki string, sessionIDs []string) (refs int, pages []string, err error) {
	named := newIDs[bool]()
	for _, id := range sessionIDs {
		named.full[id], named.prefix[short(id)] = true, true
	}
	root, err := paths.OpenRoot(wiki, false)
	if err != nil {
		return 0, nil, fmt.Errorf("lint: open the wiki: %w", err)
	}
	defer root.Close()
	for _, id := range threadPages(root) {
		if err := ctx.Err(); err != nil {
			return refs, pages, err
		}
		n := 0
		if _, err := pageRefs(root, id, func(session string, _ int) {
			if _, ok := named.get(session); ok {
				n++
			}
		}); err != nil || n == 0 {
			continue
		}
		refs += n
		pages = append(pages, id)
	}
	return refs, pages, nil
}

// readLine returns the next line without its newline, keeping only the
// first 64 KiB of a longer one; ok is false at the end.
func readLine(r *bufio.Reader) (line string, ok bool, err error) {
	b, err := r.ReadSlice('\n')
	line = string(b)
	for errors.Is(err, bufio.ErrBufferFull) {
		_, err = r.ReadSlice('\n')
	}
	if errors.Is(err, io.EOF) {
		return strings.TrimSuffix(line, "\n"), line != "", nil
	}
	if err != nil {
		return "", false, err
	}
	return strings.TrimSuffix(line, "\n"), true, nil
}

// threadPages lists the ids of the thread pages: general/threads/*.md and
// projects/*/threads/*.md, without the index files, sorted.
func threadPages(root *os.Root) []string {
	folders := []string{"general"}
	for _, p := range list(root, "projects") {
		if p.IsDir() && !strings.HasPrefix(p.Name(), ".") {
			folders = append(folders, "projects/"+p.Name())
		}
	}
	var ids []string
	for _, folder := range folders {
		dir := folder + "/threads"
		for _, e := range list(root, filepath.FromSlash(dir)) {
			name, ok := strings.CutSuffix(e.Name(), ".md")
			if ok && name != "index" && !strings.HasPrefix(name, ".") && e.Type().IsRegular() {
				ids = append(ids, dir+"/"+name)
			}
		}
	}
	slices.Sort(ids)
	return ids
}

// list returns a folder's entries, or none when it can't be read.
func list(root *os.Root, dir string) []fs.DirEntry {
	d, err := root.Open(dir)
	if err != nil {
		return nil
	}
	defer d.Close()
	entries, _ := d.ReadDir(-1)
	return entries
}
