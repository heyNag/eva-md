package searchindex

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/heyNag/eva-md/internal/paths"
)

// busyTries and busyWait bound how long a catch-up waits for a folder a
// search's top-up holds, which takes well under a second.
const (
	busyTries = 25
	busyWait  = 200 * time.Millisecond
)

// Catchup brings the index up to date with the inbox, as the daily maintain
// and eva-md index do. It runs under <locks>/search-index.lock, taken
// without waiting, so a catch-up already running makes it come back Busy.
// It takes each inbox project folder in turn, under that folder's lock:
// checks every segment whole and starts a damaged folder again, indexes what
// the inbox holds beyond the index, oldest file first, merges segments as
// the policy asks, and deletes what no reader can still need. An index
// folder whose inbox folder is gone is deleted. by names who ran it in the
// manifests, maintain or index.
func Catchup(ctx context.Context, dirs paths.Dirs, by string) (Report, error) {
	return catchup(ctx, dirs, by, false)
}

// Rebuild is Catchup that starts every folder again.
func Rebuild(ctx context.Context, dirs paths.Dirs, by string) (Report, error) {
	return catchup(ctx, dirs, by, true)
}

func catchup(ctx context.Context, dirs paths.Dirs, by string, rebuild bool) (Report, error) {
	var total Report
	unlock, busy, err := paths.TryLock(dirs.Locks(), "search-index.lock")
	if busy || err != nil {
		total.Busy = busy
		if err != nil {
			err = fmt.Errorf("searchindex: %w", err)
		}
		return total, err
	}
	defer unlock()
	folders, err := subfolders(dirs.Inbox())
	if err != nil {
		return total, fmt.Errorf("searchindex: list the inbox: %w", err)
	}
	for _, name := range folders {
		if err := ctx.Err(); err != nil {
			return total, err
		}
		r, err := catchupFolder(ctx, dirs, name, by, rebuild)
		total.Files += r.Files
		total.Bytes += r.Bytes
		total.Segments += r.Segments
		total.Merged += r.Merged
		total.Size += r.Size
		total.Unreadable += r.Unreadable
		total.Projects = append(total.Projects, FolderReport{Name: name, Report: r})
		if err != nil {
			return total, err
		}
	}
	// An index folder whose inbox folder is gone indexes nothing.
	indexed, _ := subfolders(dirs.SearchIndex())
	inInbox := map[string]bool{}
	for _, name := range folders {
		inInbox[name] = true
	}
	for _, name := range indexed {
		if !inInbox[name] {
			removeFolder(dirs, name)
		}
	}
	return total, nil
}

// catchupFolder is Catchup for one project folder.
func catchupFolder(ctx context.Context, dirs paths.Dirs, name, by string, rebuild bool) (Report, error) {
	dir, err := folderPath(dirs.SearchIndex(), name)
	if err != nil {
		return Report{}, nil
	}
	var unlock func()
	for try := 0; ; try++ {
		var busy bool
		unlock, busy, err = lockFolder(dir)
		if err != nil {
			return Report{}, err
		}
		if !busy {
			break
		}
		if try == busyTries || ctx.Err() != nil {
			return Report{Busy: true}, nil
		}
		time.Sleep(busyWait)
	}
	defer unlock()
	root, err := paths.OpenRoot(dir, false)
	if err != nil {
		return Report{}, fmt.Errorf("searchindex: %w", err)
	}
	defer root.Close()
	reset := rebuild || !sound(root)
	m, r, err := refresh(ctx, dirs, name, root, dir, Options{By: by, Reset: reset})
	if err != nil {
		return r, err
	}
	if m, r.Merged, err = compact(ctx, root, m); err != nil {
		return r, err
	}
	if changed, err := prune(root, &m, time.Now()); err == nil && changed {
		if err := publish(ctx, root, m); err != nil {
			return r, err
		}
	}
	r.Size = size(m)
	return r, nil
}

// sound reports whether an index folder's manifest reads and every segment
// it names passes a check of all its bytes. A folder with no manifest yet is
// sound.
func sound(root *os.Root) bool {
	m, err := ReadManifest(root)
	if errors.Is(err, fs.ErrNotExist) {
		return true
	}
	if err != nil {
		return false
	}
	for _, s := range m.Segments {
		seg, err := OpenSegment(root, s.File)
		if err != nil {
			return false
		}
		err = seg.Verify()
		seg.Close()
		if err != nil {
			return false
		}
	}
	return true
}

// compact merges m's segments as the policy asks, then drops the states of
// files that are gone and no segment names any more, publishing what
// changed, and returns the manifest and how many segments it merged.
func compact(ctx context.Context, root *os.Root, m Manifest) (Manifest, int, error) {
	segs, err := openAll(root, m)
	if err != nil {
		return m, 0, err
	}
	dead := map[string]int{}
	for _, seg := range segs {
		dead[seg.Name] = deadDocs(m, seg)
	}
	merged := 0
	if pick := mergeSet(m, dead); len(pick) > 0 {
		var chosen []*Segment
		for _, seg := range segs {
			for _, p := range pick {
				if seg.Name == p {
					chosen = append(chosen, seg)
				}
			}
		}
		out, err := merge(ctx, root, m, chosen)
		closeAll(segs)
		if err != nil {
			return m, 0, err
		}
		if err := publish(ctx, root, out); err != nil {
			return m, 0, err
		}
		m, merged = out, len(pick)
		if segs, err = openAll(root, m); err != nil {
			return m, merged, err
		}
	}
	defer closeAll(segs)
	named := map[string]bool{}
	for _, seg := range segs {
		for _, src := range seg.sources {
			named[src.Name] = true
		}
	}
	changed := false
	for file, st := range m.Files {
		if st.Ino == 0 && !named[file] {
			delete(m.Files, file)
			changed = true
		}
	}
	if changed {
		if err := publish(ctx, root, m); err != nil {
			return m, merged, err
		}
	}
	return m, merged, nil
}

func openAll(root *os.Root, m Manifest) ([]*Segment, error) {
	var segs []*Segment
	for _, s := range m.Segments {
		seg, err := OpenSegment(root, s.File)
		if err != nil {
			closeAll(segs)
			return nil, err
		}
		segs = append(segs, seg)
	}
	return segs, nil
}

func closeAll(segs []*Segment) {
	for _, s := range segs {
		s.Close()
	}
}

// subfolders lists the project folders under dir, by name: folders whose
// names are plain and don't start with a dot. A dir that doesn't exist has
// none.
func subfolders(dir string) ([]string, error) {
	root, err := paths.OpenRoot(dir, false)
	if errors.Is(err, fs.ErrNotExist) {
		return nil, nil
	}
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
	var out []string
	for _, e := range entries {
		if e.IsDir() && plainName(e.Name()) && !strings.HasPrefix(e.Name(), ".") {
			out = append(out, e.Name())
		}
	}
	sort.Strings(out)
	return out, nil
}

// removeFolder deletes an index folder, under its lock, which it takes
// without waiting.
func removeFolder(dirs paths.Dirs, name string) {
	dir, err := folderPath(dirs.SearchIndex(), name)
	if err != nil {
		return
	}
	unlock, busy, err := lockFolder(dir)
	if busy || err != nil {
		return
	}
	defer unlock()
	root, err := paths.OpenRoot(dirs.SearchIndex(), false)
	if err != nil {
		return
	}
	defer root.Close()
	_ = root.RemoveAll(filepath.Base(dir))
}
