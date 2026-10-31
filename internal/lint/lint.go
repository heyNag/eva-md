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

	"github.com/heyNag/eva-md/internal/paths"
	"github.com/heyNag/eva-md/internal/threadpage"
)

// OversizeBytes is the size past which a thread page is flagged: the trigger
// for Timeline rotation, which is deferred.
const OversizeBytes = 512 << 10

// ref is a source ref, s:<session>#<line>.
var ref = regexp.MustCompile(`^s:([^#\s]+)#([0-9]+)$`)

// Result is the lint's report, saved as <state>/lint.json. It holds counts
// and page ids only.
type Result struct {
	At          string   `json:"at"`
	Oversize    []string `json:"oversize"` // thread pages over OversizeBytes
	BadRefs     int      `json:"bad_refs"` // refs whose session is unknown or whose line is past its end
	BadRefPages []string `json:"bad_ref_pages"`
	Pages       int      `json:"pages"` // thread pages read
	DurationMS  int64    `json:"duration_ms"`
}

// Run lints the thread pages of the wiki at wiki against the inbox and saves
// the result as <state>/lint.json, mode 0600. It reads the inbox once,
// streaming each session file to count its lines, then each thread page on
// its own, line by line through threadpage.Scanner. It never loads the wiki
// whole or holds a session file in memory.
func Run(ctx context.Context, wiki string, dirs paths.Dirs) (Result, error) {
	start := time.Now()
	r := Result{At: start.Format(time.RFC3339), Oversize: []string{}, BadRefPages: []string{}}
	s, err := readInbox(ctx, dirs.Inbox())
	if err != nil {
		return r, err
	}
	root, err := paths.OpenRoot(wiki, false)
	if err != nil {
		return r, fmt.Errorf("lint: open the wiki: %w", err)
	}
	defer root.Close()
	for _, id := range threadPages(root) {
		if err := ctx.Err(); err != nil {
			return r, err
		}
		size, bad, err := s.page(root, id)
		if err != nil {
			continue
		}
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

// sessions holds the line count of each session file in the inbox, by
// session id and by the first 8 characters a short ref names it by.
type sessions struct{ full, prefix map[string]int }

// readInbox counts the lines of every session file. A session's id is its
// file name without the host, as eva-md show finds a ref's file. An inbox
// that doesn't exist holds no sessions.
func readInbox(ctx context.Context, inbox string) (sessions, error) {
	s := sessions{full: map[string]int{}, prefix: map[string]int{}}
	root, err := paths.OpenRoot(inbox, false)
	if errors.Is(err, fs.ErrNotExist) {
		return s, nil
	}
	if err != nil {
		return s, fmt.Errorf("lint: open the inbox: %w", err)
	}
	defer root.Close()
	for _, project := range list(root, ".") {
		if !project.IsDir() || strings.HasPrefix(project.Name(), ".") {
			continue
		}
		for _, f := range list(root, project.Name()) {
			name, ok := strings.CutSuffix(f.Name(), ".jsonl")
			if !ok || !f.Type().IsRegular() {
				continue
			}
			if err := ctx.Err(); err != nil {
				return s, err
			}
			n, err := countLines(root, filepath.Join(project.Name(), f.Name()))
			if err != nil {
				continue
			}
			for _, host := range []string{"claude-code-", "codex-"} {
				if id, ok := strings.CutPrefix(name, host); ok {
					name = id
					break
				}
			}
			short := name
			if r := []rune(name); len(r) > 8 {
				short = string(r[:8])
			}
			s.full[name] = max(s.full[name], n)
			s.prefix[short] = max(s.prefix[short], n)
		}
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

// resolves reports whether a ref names a session line the inbox holds.
func (s sessions) resolves(id string, line int) bool {
	n, ok := s.full[id]
	if !ok && len([]rune(id)) <= 8 {
		n, ok = s.prefix[id]
	}
	return ok && line >= 1 && line <= n
}

// page reads one thread page, skipping its frontmatter, and counts the refs
// in its Now items and Timeline lines that don't resolve. It returns the
// page's size too.
func (s sessions) page(root *os.Root, id string) (size int64, bad int, err error) {
	f, err := root.Open(filepath.FromSlash(id) + ".md")
	if err != nil {
		return 0, 0, err
	}
	defer f.Close()
	info, err := f.Stat()
	if err != nil || !info.Mode().IsRegular() {
		return 0, 0, errors.New("lint: not a regular file")
	}
	r := bufio.NewReaderSize(f, 64<<10)
	var sc threadpage.Scanner
	front := false
	for n := 1; ; n++ {
		line, ok, err := readLine(r)
		if err != nil {
			return 0, 0, err
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
			if num, err := strconv.Atoi(m[2]); err != nil || !s.resolves(m[1], num) {
				bad++
			}
		}
	}
	return info.Size(), bad, nil
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
