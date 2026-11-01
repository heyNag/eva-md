package searchindex

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"maps"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"syscall"
	"time"

	"github.com/heyNag/eva-md/internal/inbox"
	"github.com/heyNag/eva-md/internal/paths"
)

// FileInfo is a session file as a folder listing found it.
type FileInfo struct {
	Name string
	Size int64
	Ino  uint64
	Mod  time.Time
}

// Range is what a refresh reads of a session file: from Pos, its offset,
// lines and off-the-record state there, to the file's end, as generation
// Gen.
type Range struct {
	FileInfo
	Gen uint32
	Pos inbox.Position
}

// Plan works out from m and an inbox folder's listing what a refresh
// indexes, and updates m's file states for what it found: a file grown
// since has its tail indexed; a new one is indexed whole, and so is one
// found shrunk or with another inode, under its next generation, which
// makes its earlier documents dead. A file gone from the listing gets its
// next generation and no range. Refresh also indexes a grown file whole,
// under its next generation, when where the index stopped no longer starts
// a line.
func Plan(m *Manifest, listing []FileInfo) []Range {
	var out []Range
	seen := map[string]bool{}
	for _, f := range listing {
		seen[f.Name] = true
		st, ok := m.Files[f.Name]
		switch {
		case !ok:
			st = FileState{Gen: 1, Ino: f.Ino}
		case st.Ino != f.Ino || f.Size < st.To:
			st = FileState{Gen: st.Gen + 1, Ino: f.Ino}
		case f.Size == st.To:
			continue
		}
		m.Files[f.Name] = st
		out = append(out, Range{FileInfo: f, Gen: st.Gen, Pos: inbox.Position{Offset: st.To, Line: st.Lines, Off: st.Off}})
	}
	for name, st := range m.Files {
		if !seen[name] && st.Ino != 0 {
			m.Files[name] = FileState{Gen: st.Gen + 1}
		}
	}
	return out
}

// Budget bounds a refresh: the inbox bytes it reads (0 for no limit) and
// when it stops (zero for no limit). It stops at a line boundary and keeps
// what it indexed.
type Budget struct {
	Bytes int64
	Until time.Time
}

// Options is how a refresh runs.
type Options struct {
	Budget
	NewestFirst bool   // the files changed last first, as a search's top-up reads them; otherwise the oldest first
	Reset       bool   // start the folder again, as for a damaged index
	By          string // who indexed: maintain, index or search
}

// Report says what a refresh, merge or catch-up did. Folder reports are in
// Projects, by name.
type Report struct {
	Files      int   // files indexed in whole or in part
	Bytes      int64 // inbox bytes read
	Segments   int   // segments written
	Merged     int   // segments merged away
	Size       int64 // the index's size afterwards: its segments' bytes
	Busy       bool  // another writer held the lock, so nothing was done
	Rebuilt    bool  // the folder was started again
	Unreadable int   // session files that couldn't be read
	Projects   []FolderReport
}

// FolderReport is a catch-up's report for one project folder.
type FolderReport struct {
	Name string
	Report
}

// Refresh indexes the session files of the inbox project folder name that
// the index lacks, under the folder's lock, which it takes without waiting:
// a folder another writer holds comes back Busy. It reads within o's
// budget, a block of BlockBytes at a time, and writes a segment and a new
// manifest for each block, so a refresh stopped early, or killed, keeps
// what it published.
func Refresh(ctx context.Context, dirs paths.Dirs, name string, o Options) (Report, error) {
	dir, err := folderPath(dirs.SearchIndex(), name)
	if err != nil {
		return Report{}, err
	}
	unlock, busy, err := lockFolder(dir)
	if busy || err != nil {
		return Report{Busy: busy}, err
	}
	defer unlock()
	root, err := paths.OpenRoot(dir, false)
	if err != nil {
		return Report{}, fmt.Errorf("searchindex: %w", err)
	}
	defer root.Close()
	m, r, err := refresh(ctx, dirs, name, root, dir, o)
	if err == nil {
		r.Size = size(m)
	}
	return r, err
}

// refresh is Refresh with the folder's lock held and its index folder open
// at dir, and returns the manifest it left. A manifest records how far a
// file is indexed only once the segment holding those documents is in
// place, so a block that can't be written leaves the folder as it was.
func refresh(ctx context.Context, dirs paths.Dirs, name string, root *os.Root, dir string, o Options) (Manifest, Report, error) {
	var r Report
	now := time.Now()
	m, err := ReadManifest(root)
	fresh := false
	switch {
	case err == nil && o.Reset:
		retire(&m, segmentNames(m), now)
		m.Files, r.Rebuilt = map[string]FileState{}, true
	case errors.Is(err, fs.ErrNotExist):
		m, fresh = newManifest(), true
	case errors.Is(err, ErrDamaged):
		// The segments it named are pruned as files no manifest names.
		m, r.Rebuilt = newManifest(), true
	case err != nil:
		return m, r, err
	default:
		for _, s := range m.Segments {
			if _, err := root.Lstat(s.File); err != nil {
				retire(&m, segmentNames(m), now)
				m.Files, r.Rebuilt = map[string]FileState{}, true
				break
			}
		}
	}
	folder := filepath.Join(dirs.Inbox(), name)
	listing, err := listFolder(folder)
	if err != nil && !errors.Is(err, fs.ErrNotExist) {
		return m, r, fmt.Errorf("searchindex: list the inbox folder: %w", err)
	}
	before := clone(m.Files)
	ranges := Plan(&m, listing)
	planned := fresh || r.Rebuilt || !maps.Equal(before, m.Files)
	sort.SliceStable(ranges, func(i, j int) bool {
		if !ranges[i].Mod.Equal(ranges[j].Mod) {
			return ranges[i].Mod.After(ranges[j].Mod) == o.NewestFirst
		}
		return ranges[i].Name < ranges[j].Name
	})
	walk := ctx
	if !o.Until.IsZero() {
		var cancel context.CancelFunc
		walk, cancel = context.WithDeadline(ctx, o.Until)
		defer cancel()
	}
	b, advanced := newBuilder(), map[string]FileState{}
	flush := func() error {
		info, wrote, err := b.flush(ctx, root, dir)
		if err != nil {
			return err
		}
		if wrote {
			m.Segments = append(m.Segments, info)
			r.Segments++
		}
		for file, st := range advanced {
			m.Files[file] = st
		}
		m.Built, m.By = time.Now().UTC(), o.By
		b, advanced, planned = newBuilder(), map[string]FileState{}, false
		return publish(ctx, root, m)
	}
	indexed := map[string]bool{}
	var inboxRoot *os.Root
	if len(ranges) > 0 {
		if inboxRoot, err = paths.OpenRoot(folder, false); err != nil {
			return m, r, fmt.Errorf("searchindex: %w", err)
		}
		defer inboxRoot.Close()
	}
	done := false
	for _, rg := range ranges {
		if done {
			break
		}
		f, info, err := inbox.OpenSession(inboxRoot, rg.Name)
		if err != nil || inode(info) != rg.Ino {
			if err == nil {
				f.Close()
			} else if !errors.Is(err, fs.ErrNotExist) {
				r.Unreadable++
			}
			continue
		}
		if rg.Pos.Offset > 0 && !startsLine(f, rg.Pos.Offset) {
			// Where the index stopped no longer starts a line, which only
			// a rewrite in place leaves, so the file is indexed whole
			// under its next generation, as Plan does for one shrunk.
			rg.Gen, rg.Pos = rg.Gen+1, inbox.Position{}
			m.Files[rg.Name], planned = FileState{Gen: rg.Gen, Ino: rg.Ino}, true
		}
		for pos := rg.Pos; !done; {
			limit := BlockBytes - b.bytes
			if o.Bytes > 0 {
				limit = min(limit, o.Bytes-r.Bytes)
			}
			if limit <= 0 {
				if done = o.Bytes > 0 && r.Bytes >= o.Bytes; !done {
					if err := flush(); err != nil {
						f.Close()
						return m, r, err
					}
				}
				continue
			}
			src := -1
			end, werr := inbox.Walk(walk, f, name, rg.Name, pos, limit, func(l inbox.Line) bool {
				if src < 0 {
					src = b.source(rg.Name, rg.Gen, pos.Offset)
				}
				b.add(l, src)
				return true
			})
			if src >= 0 {
				b.sources[src].To = end.Offset
			}
			read := end.Offset - pos.Offset
			b.bytes += read
			r.Bytes += read
			if read > 0 {
				advanced[rg.Name] = FileState{Gen: rg.Gen, To: end.Offset, Lines: end.Line, Ino: rg.Ino, Off: end.Off}
				indexed[rg.Name] = true
			}
			pos = end
			if werr != nil {
				// Out of time: stop, keeping what was read. A file that
				// fails to read is left for the next refresh.
				if done = walk.Err() != nil; !done {
					r.Unreadable++
				}
				break
			}
			if read == 0 || end.Offset >= rg.Size {
				break
			}
		}
		f.Close()
	}
	r.Files = len(indexed)
	if err := ctx.Err(); err != nil {
		return m, r, err
	}
	if len(b.ts) > 0 || len(advanced) > 0 || planned {
		if err := flush(); err != nil {
			return m, r, err
		}
	}
	return m, r, nil
}

// startsLine reports whether offset at of f starts a line: the byte before
// it is a newline.
func startsLine(f *os.File, at int64) bool {
	var prev [1]byte
	_, err := f.ReadAt(prev[:], at-1)
	return err == nil && prev[0] == '\n'
}

// listFolder lists the session files of an inbox project folder.
func listFolder(dir string) ([]FileInfo, error) {
	root, err := paths.OpenRoot(dir, false)
	if err != nil {
		return nil, err
	}
	defer root.Close()
	d, err := root.Open(".")
	if err != nil {
		return nil, err
	}
	entries, err := d.ReadDir(-1)
	d.Close()
	if err != nil {
		return nil, err
	}
	var out []FileInfo
	for _, e := range entries {
		if !e.Type().IsRegular() || !sessionFile(e.Name()) {
			continue
		}
		info, err := e.Info()
		if err != nil {
			continue
		}
		out = append(out, FileInfo{Name: e.Name(), Size: info.Size(), Ino: inode(info), Mod: info.ModTime()})
	}
	return out, nil
}

// sessionFile reports whether name is a capture file's: <host>-<id>.jsonl.
func sessionFile(name string) bool {
	base, ok := strings.CutSuffix(name, ".jsonl")
	if !ok {
		return false
	}
	for _, h := range []string{"claude-code-", "codex-"} {
		if id, found := strings.CutPrefix(base, h); found && id != "" {
			return true
		}
	}
	return false
}

func inode(info os.FileInfo) uint64 {
	if info == nil {
		return 0
	}
	if st, ok := info.Sys().(*syscall.Stat_t); ok {
		return st.Ino
	}
	return 0
}

func clone(files map[string]FileState) map[string]FileState {
	out := make(map[string]FileState, len(files))
	for k, v := range files {
		out[k] = v
	}
	return out
}

func segmentNames(m Manifest) []string {
	var out []string
	for _, s := range m.Segments {
		out = append(out, s.File)
	}
	return out
}

// size is the bytes of the segments m names.
func size(m Manifest) int64 {
	var n int64
	for _, s := range m.Segments {
		n += s.Bytes
	}
	return n
}
