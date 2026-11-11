package work

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"strings"
	"time"

	"github.com/heyNag/eva-md/internal/paths"
)

// The folders under <state>/work.
const (
	wakeDir    = "wake"    // marks waiting to run
	runDir     = "run"     // run/<run>/: the marks a run was given
	journalDir = "journal" // journal/<YYYY-MM>.jsonl
)

// openWork opens <state>/work, creating it and its folders, 0700.
func openWork(dirs paths.Dirs) (*os.Root, error) {
	if dirs.State == "" {
		return nil, errors.New("work: no state folder")
	}
	root, err := paths.OpenRoot(filepath.Join(dirs.State, "work"), true)
	if err != nil {
		return nil, fmt.Errorf("work: %w", err)
	}
	for _, dir := range []string{wakeDir, runDir, journalDir} {
		if err := root.Mkdir(dir, 0o700); err != nil && !errors.Is(err, fs.ErrExist) {
			root.Close()
			return nil, fmt.Errorf("work: %w", err)
		}
		if info, err := root.Lstat(dir); err != nil || !info.IsDir() {
			root.Close()
			return nil, fmt.Errorf("work: %s is not a folder", dir)
		}
	}
	return root, nil
}

// markText is what a mark's file holds: its reason, then its host if it has
// one.
func markText(m Mark) string {
	if m.Host == "" {
		return m.Reason + "\n"
	}
	return m.Reason + " " + m.Host + "\n"
}

// writeMark writes m into wake/, replacing the same mark if it is there.
func writeMark(ctx context.Context, root *os.Root, m Mark) error {
	if err := paths.WriteFileAtomicAt(ctx, root, path.Join(wakeDir, m.name()), []byte(markText(m)), 0o600); err != nil {
		return fmt.Errorf("work: write a mark: %w", err)
	}
	return nil
}

// A listed mark is a mark as found in a folder, with when it was written.
type listed struct {
	Mark
	written time.Time
}

// listMarks lists the marks in dir, by file name. A file whose name or
// content is not a mark's, or that went away while it was read, is passed
// over; so is a mark of a kind no job registered.
func listMarks(root *os.Root, dir string) ([]listed, error) {
	f, err := root.Open(dir)
	if err != nil {
		return nil, fmt.Errorf("work: %w", err)
	}
	entries, err := f.ReadDir(-1)
	f.Close()
	if err != nil {
		return nil, fmt.Errorf("work: %w", err)
	}
	var out []listed
	for _, e := range entries {
		if !e.Type().IsRegular() {
			continue
		}
		m, ok := parseName(e.Name())
		if !ok {
			continue
		}
		info, err := e.Info()
		if err != nil {
			continue
		}
		data, err := root.ReadFile(path.Join(dir, e.Name()))
		if err != nil {
			continue
		}
		words := strings.Fields(string(data))
		if len(words) > 0 && wordPattern.MatchString(words[0]) {
			m.Reason = words[0]
		}
		if len(words) > 1 && hostPattern.MatchString(words[1]) {
			m.Host = words[1]
		}
		out = append(out, listed{m, info.ModTime()})
	}
	return out, nil
}

// parseName reads a mark's kind and key from its file name, <kind>-<key>.
// A kind holds no '-', so the first one ends it.
func parseName(name string) (Mark, bool) {
	kind, key, ok := strings.Cut(name, "-")
	if !ok || !kindPattern.MatchString(kind) {
		return Mark{}, false
	}
	if safe, err := paths.SafeName(key); err != nil || safe != key {
		return Mark{}, false
	}
	if _, ok := registered(kind); !ok {
		return Mark{}, false
	}
	return Mark{Kind: kind, Key: key}, true
}

// take moves marks out of wake/ into run/<id>/ and lists what it moved,
// so a mark written again while the run goes on lands as a new file in
// wake/ and is not removed by the run's end.
func take(root *os.Root, id string, marks []listed) ([]listed, error) {
	dir := path.Join(runDir, id)
	if err := root.Mkdir(dir, 0o700); err != nil {
		return nil, fmt.Errorf("work: %w", err)
	}
	for _, m := range marks {
		err := root.Rename(path.Join(wakeDir, m.name()), path.Join(dir, m.name()))
		if err != nil && !errors.Is(err, fs.ErrNotExist) {
			return nil, errors.Join(fmt.Errorf("work: take a mark: %w", err), putBack(root, id))
		}
	}
	taken, err := listMarks(root, dir)
	if err != nil {
		return nil, errors.Join(err, putBack(root, id))
	}
	return taken, nil
}

// putBack returns the marks in run/<id>/ to wake/ and removes the folder.
// Where the same mark was written again meanwhile, the newer one stays.
func putBack(root *os.Root, id string) error {
	dir := path.Join(runDir, id)
	f, err := root.Open(dir)
	if err != nil {
		return fmt.Errorf("work: %w", err)
	}
	entries, err := f.ReadDir(-1)
	f.Close()
	if err != nil {
		return fmt.Errorf("work: %w", err)
	}
	for _, e := range entries {
		from := path.Join(dir, e.Name())
		if err := root.Link(from, path.Join(wakeDir, e.Name())); err != nil && !errors.Is(err, fs.ErrExist) {
			return fmt.Errorf("work: put a mark back: %w", err)
		}
		if err := root.Remove(from); err != nil {
			return fmt.Errorf("work: %w", err)
		}
	}
	if err := root.Remove(dir); err != nil {
		return fmt.Errorf("work: %w", err)
	}
	return nil
}

// drop removes the marks in run/<id>/ and the folder: the run used them.
func drop(root *os.Root, id string) error {
	if err := root.RemoveAll(path.Join(runDir, id)); err != nil {
		return fmt.Errorf("work: %w", err)
	}
	return nil
}
