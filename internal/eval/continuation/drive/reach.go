package drive

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"syscall"
	"time"

	"github.com/heyNag/eva-md/internal/eval/continuation/task"
)

// What a chat can reach: the trial's folder, which the driver checks for
// the task's hidden files after the workspace is prepared, after each call
// and after each grading a prompt follows, and removes once the trial is
// over. See the package comment for what it cannot guard.

// removeTimeout bounds the tries at removing a trial's folder, or a
// grading's copy, while a job of the trial's may still hold a file in it;
// removePause is the wait between two tries.
var (
	removeTimeout = 2 * time.Minute
	removePause   = 250 * time.Millisecond
)

// remove removes the folder dir and all it holds, folders made writable
// first, so that a folder the chat or the code it ran made read-only goes
// too. One that cannot be removed is tried again until removeTimeout has
// passed, from the run's context with its end ignored, so that a stopped
// run still removes it; then its error is returned.
func (p *plan) remove(ctx context.Context, dir string) error {
	limit := p.opts.removeLimit
	if limit == nil {
		limit = context.WithTimeout
	}
	lctx, cancel := limit(context.WithoutCancel(ctx), removeTimeout)
	defer cancel()
	removeAll := p.opts.removeAll
	if removeAll == nil {
		removeAll = os.RemoveAll
	}
	for {
		writable(dir)
		err := removeAll(dir)
		if err == nil {
			return nil
		}
		if lctx.Err() != nil {
			return pathless(err)
		}
		select {
		case <-lctx.Done():
			return pathless(err)
		case <-time.After(removePause):
		}
	}
}

// writable gives the owner every right on dir and each folder under it,
// links not followed.
func writable(dir string) {
	info, err := os.Lstat(dir)
	if err != nil || !info.IsDir() {
		return
	}
	if info.Mode().Perm()&0o700 != 0o700 {
		_ = os.Chmod(dir, info.Mode().Perm()|0o700)
	}
	entries, _ := os.ReadDir(dir)
	for _, e := range entries {
		if e.IsDir() {
			writable(filepath.Join(dir, e.Name()))
		}
	}
}

// removeRun removes the folders of the run's tasks and conditions under
// work, and the run's own, when they hold nothing: once its trials' folders
// are removed, the work folder is as empty as the run found it.
func (p *plan) removeRun(run string) {
	for _, t := range p.tasks {
		_ = os.Remove(filepath.Join(p.work, run, t.ID, string(p.opts.Condition)))
		_ = os.Remove(filepath.Join(p.work, run, t.ID))
	}
	_ = os.Remove(filepath.Join(p.work, run))
}

// hidden are a task's hidden files as a trial's workspace would hold them:
// the path each would have under its repository, and the bytes of each
// that is not empty.
type hidden struct {
	paths   []string
	content [][]byte
	longest int
	// start says which of paths the workspace held when it was prepared.
	start map[string]bool
}

// loadHidden reads the hidden files of task t for the workspace ws, and
// which of their paths ws holds as it was prepared.
func loadHidden(t task.Task, ws string) (*hidden, error) {
	h := &hidden{start: map[string]bool{}}
	for _, name := range t.Repos {
		src := filepath.Join(t.Dir, task.OverlayHidden, name)
		root := ws
		if t.Workdir != task.WorkdirRepo {
			root = filepath.Join(ws, name)
		}
		err := filepath.WalkDir(src, func(p string, d fs.DirEntry, err error) error {
			if errors.Is(err, fs.ErrNotExist) && p == src {
				return filepath.SkipDir
			}
			if err != nil || !d.Type().IsRegular() {
				return err
			}
			rel, err := filepath.Rel(src, p)
			if err != nil {
				return err
			}
			data, err := os.ReadFile(p)
			if err != nil {
				return err
			}
			path := filepath.Join(root, rel)
			h.paths = append(h.paths, path)
			if len(data) > 0 {
				h.content = append(h.content, data)
				h.longest = max(h.longest, len(data))
			}
			return nil
		})
		if err != nil {
			return nil, fmt.Errorf("the task's hidden files: %w", pathless(err))
		}
	}
	for _, p := range h.paths {
		if _, err := os.Lstat(p); err == nil {
			h.start[p] = true
		}
	}
	return h, nil
}

// chunk is how much of a file is read at once.
const chunk = 1 << 20

// found reports whether the trial's folder root, but its grading folder,
// holds a hidden file: by path, a file at a path one would have in the
// workspace that the workspace lacked when it was prepared; by content, a
// regular file, no link followed, that holds a hidden file's bytes whole.
// Every file is read, however large, a piece at a time, so memory is
// bounded by the hidden files and not by what the chat wrote. A folder or
// file the owner cannot read is given the owner's rights first; one that
// still cannot be read is an error.
func (h *hidden) found(root, grade string) (bool, error) {
	window := make([]byte, chunk+h.longest)
	in, err := h.walk(root, grade, window)
	if in || err != nil {
		return in, err
	}
	for _, p := range h.paths {
		if h.start[p] {
			continue
		}
		_, err := os.Lstat(p)
		switch {
		case err == nil:
			return true, nil
		case !errors.Is(err, fs.ErrNotExist) && !errors.Is(err, syscall.ENOTDIR):
			return false, fmt.Errorf("checking a hidden file's path: %w", pathless(err))
		}
	}
	return false, nil
}

// walk reads every regular file under dir but the folder skip, links not
// followed, and reports whether one holds a hidden file's bytes.
func (h *hidden) walk(dir, skip string, window []byte) (bool, error) {
	entries, err := os.ReadDir(dir)
	if errors.Is(err, fs.ErrPermission) {
		if info, lerr := os.Lstat(dir); lerr == nil && info.IsDir() {
			_ = os.Chmod(dir, info.Mode().Perm()|0o700)
		}
		entries, err = os.ReadDir(dir)
	}
	if err != nil {
		return false, fmt.Errorf("reading the trial's folder: %w", pathless(err))
	}
	for _, e := range entries {
		p := filepath.Join(dir, e.Name())
		switch {
		case p == skip:
		case e.IsDir():
			if in, err := h.walk(p, skip, window); in || err != nil {
				return in, err
			}
		case e.Type().IsRegular():
			if in, err := h.holds(p, window); in || err != nil {
				return in, err
			}
		}
	}
	return false, nil
}

// holds reports whether the regular file p holds a hidden file's bytes,
// reading it into window a piece at a time, each piece after the last
// bytes of the one before, as many as the longest hidden file less one, so
// that bytes across two pieces are found too.
func (h *hidden) holds(p string, window []byte) (bool, error) {
	if len(h.content) == 0 {
		return false, nil
	}
	f, err := open(p)
	if err != nil {
		return false, fmt.Errorf("reading a file of the trial's folder: %w", pathless(err))
	}
	defer f.Close()
	if info, err := f.Stat(); err != nil || !info.Mode().IsRegular() {
		// A file swapped for something else since the folder was read.
		return false, pathless(err)
	}
	keep := h.longest - 1
	n := 0
	for {
		m, err := f.Read(window[n:])
		n += m
		for _, c := range h.content {
			if bytes.Contains(window[:n], c) {
				return true, nil
			}
		}
		if errors.Is(err, io.EOF) {
			return false, nil
		}
		if err != nil {
			return false, fmt.Errorf("reading a file of the trial's folder: %w", pathless(err))
		}
		if n > keep {
			copy(window, window[n-keep:n])
			n = keep
		}
	}
}

// open opens the file p to read, never through a link, and, when its
// owner may not read it, after giving the owner that right.
func open(p string) (*os.File, error) {
	f, err := os.OpenFile(p, os.O_RDONLY|syscall.O_NOFOLLOW|syscall.O_NONBLOCK, 0)
	if errors.Is(err, fs.ErrPermission) {
		if info, lerr := os.Lstat(p); lerr == nil && info.Mode().IsRegular() {
			_ = os.Chmod(p, info.Mode().Perm()|0o400)
		}
		f, err = os.OpenFile(p, os.O_RDONLY|syscall.O_NOFOLLOW|syscall.O_NONBLOCK, 0)
	}
	return f, err
}
