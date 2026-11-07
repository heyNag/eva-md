package inbox

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"syscall"
	"time"

	"github.com/heyNag/eva-md/internal/paths"
)

// What eva-md inbox clear needs of the inbox: what it holds of each session,
// the removal of a session's file with its watermark, and the record of the
// sessions removed, which the daily lint reads to tell a ref to a chat
// removed on purpose from one to a chat lost.

// removedName is that record, at the inbox's root: one JSON line for each
// session removed, written before its files go.
const removedName = ".removed"

// maxRemoved is the most of the record read; a record is about 100 bytes a
// session.
const maxRemoved = 16 << 20

// Capture is what the inbox holds of one session in one project folder: its
// file, or only the watermark of a file removed by hand.
type Capture struct {
	Project string    // the project folder
	Host    string    // claude-code or codex
	ID      string    // the session id as the file's name holds it
	Size    int64     // the file's size; 0 for a watermark alone
	Mod     time.Time // when the file, or the watermark alone, was last written
	File    bool      // the file is there; false when only its watermark is
}

// Name is the capture's file name, <host>-<id>.jsonl.
func (c Capture) Name() string { return c.Host + "-" + c.ID + ".jsonl" }

// splitName returns the host and session id of a session file's name,
// <host>-<id>.jsonl.
func splitName(name string) (host, id string, ok bool) {
	base, ok := strings.CutSuffix(name, ".jsonl")
	if !ok {
		return "", "", false
	}
	for _, h := range []string{"claude-code", "codex"} {
		if id, found := strings.CutPrefix(base, h+"-"); found && id != "" {
			return h, id, true
		}
	}
	return "", "", false
}

// Captures lists what the inbox holds of each session, by project folder and
// name: every session file, and every watermark whose file is gone. It
// names the project folders it couldn't read in full in unread. An inbox
// that doesn't exist holds none.
func (s Store) Captures() (captures []Capture, unread []string, err error) {
	root, err := paths.OpenRoot(s.Root, false)
	if errors.Is(err, fs.ErrNotExist) {
		return nil, nil, nil
	}
	if err != nil {
		return nil, nil, fmt.Errorf("inbox: %w", err)
	}
	defer root.Close()
	projects, err := readDir(root, ".")
	if err != nil {
		return nil, nil, fmt.Errorf("inbox: %w", err)
	}
	for _, p := range projects {
		if !p.IsDir() || strings.HasPrefix(p.Name(), ".") {
			continue
		}
		files, err := readDir(root, p.Name())
		if err != nil {
			unread = append(unread, p.Name())
			continue
		}
		have := map[string]bool{}
		for _, f := range files {
			host, id, ok := splitName(f.Name())
			if !ok || !f.Type().IsRegular() {
				continue
			}
			info, err := f.Info()
			if err != nil {
				continue
			}
			have[f.Name()] = true
			captures = append(captures, Capture{Project: p.Name(), Host: host, ID: id, Size: info.Size(), Mod: info.ModTime(), File: true})
		}
		marks, err := readDir(root, filepath.Join(p.Name(), ".state"))
		if err != nil && !errors.Is(err, fs.ErrNotExist) {
			unread = append(unread, p.Name())
			continue
		}
		for _, m := range marks {
			name, ok := strings.CutSuffix(m.Name(), ".wm")
			if !ok || have[name] || !m.Type().IsRegular() {
				continue
			}
			host, id, ok := splitName(name)
			if !ok {
				continue
			}
			info, err := m.Info()
			if err != nil {
				continue
			}
			captures = append(captures, Capture{Project: p.Name(), Host: host, ID: id, Mod: info.ModTime()})
		}
	}
	sort.Slice(captures, func(i, j int) bool {
		if captures[i].Project != captures[j].Project {
			return captures[i].Project < captures[j].Project
		}
		return captures[i].Name() < captures[j].Name()
	})
	return captures, unread, nil
}

// readDir lists a folder of root.
func readDir(root *os.Root, name string) ([]fs.DirEntry, error) {
	d, err := root.Open(name)
	if err != nil {
		return nil, err
	}
	defer d.Close()
	return d.ReadDir(-1)
}

// checkProject refuses a project folder name that could reach outside the
// inbox or name its own files.
func checkProject(project string) error {
	if !filepath.IsLocal(project) || strings.ContainsAny(project, `/\`) || strings.HasPrefix(project, ".") {
		return errors.New("inbox: invalid project folder")
	}
	return nil
}

// check refuses a capture whose project folder or name could reach outside
// its folder.
func (c Capture) check() error {
	if err := checkProject(c.Project); err != nil {
		return err
	}
	if host, id, ok := splitName(c.Name()); !ok || host != c.Host || id != c.ID || strings.ContainsAny(c.ID, `/\`) || !filepath.IsLocal(c.Name()) {
		return errors.New("inbox: invalid session")
	}
	return nil
}

// SessionID returns the session id a capture's entries carry: that of its
// file's first line, when that line belongs to the file, as the hooks wrote
// it, and otherwise the id its name holds, which differs only for an id
// paths.SafeName had to change. The hooks lock a session by the id they
// were given.
func (s Store) SessionID(c Capture) string {
	if !c.File || c.check() != nil {
		return c.ID
	}
	root, err := paths.OpenRoot(filepath.Join(s.Root, c.Project), false)
	if err != nil {
		return c.ID
	}
	defer root.Close()
	f, _, err := OpenSession(root, c.Name())
	if err != nil {
		return c.ID
	}
	defer f.Close()
	var buf []byte
	line, ok, _, err := readLine(bufio.NewReaderSize(f, 64<<10), &buf)
	var e Entry
	if err != nil || !ok || json.Unmarshal(line, &e) != nil || !Trusted(e, c.Project, c.Name(), "") {
		return c.ID
	}
	return e.Session
}

// Remove removes a capture from its project folder: the session file, then
// its watermark, so a removal cut short leaves at most the watermark, which
// Captures lists for the next removal to take. A file or watermark already
// gone is no error. The caller holds the session's lock (session.Lock), so
// no hook appends meanwhile, and the update's lock, so no update writes the
// watermark again.
func (s Store) Remove(c Capture) error {
	if err := c.check(); err != nil {
		return err
	}
	root, err := paths.OpenRoot(s.Root, false)
	if errors.Is(err, fs.ErrNotExist) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("inbox: %w", err)
	}
	defer root.Close()
	for _, name := range []string{filepath.Join(c.Project, c.Name()), filepath.Join(c.Project, ".state", c.Name()+".wm")} {
		info, err := root.Lstat(name)
		if errors.Is(err, fs.ErrNotExist) {
			continue
		}
		if err == nil && info.IsDir() {
			err = errors.New("a folder")
		}
		if err == nil {
			err = root.Remove(name)
		}
		if err != nil && !errors.Is(err, fs.ErrNotExist) {
			return fmt.Errorf("inbox: remove %s: %w", name, err)
		}
		if err := paths.SyncDirAt(root, filepath.Dir(name)); err != nil {
			return fmt.Errorf("inbox: %w", err)
		}
	}
	return nil
}

// Tidy removes a project folder that removals left empty, with its empty
// .state folder. A folder that still holds anything, such as a file a hook
// has just made, stays. A hook of another chat in that project that made
// the folder an instant before can find it gone when it creates its file,
// and that event is lost, as for any write that fails, which transcript
// recovery repairs for a prompt or a reply.
func (s Store) Tidy(project string) {
	if checkProject(project) != nil {
		return
	}
	root, err := paths.OpenRoot(s.Root, false)
	if err != nil {
		return
	}
	defer root.Close()
	_ = root.Remove(filepath.Join(project, ".state"))
	_ = root.Remove(project)
}

// Removal is a line of the record of the sessions removed.
type Removal struct {
	V       int    `json:"v"`
	At      string `json:"at"` // when, RFC 3339
	Host    string `json:"host"`
	Session string `json:"session"` // the id as the file's name held it
}

// RecordRemoved appends a line for each capture to the record of the
// sessions removed, under an exclusive flock, and syncs it. A removal
// writes it before its files go, so the lint never takes a ref to a session
// removed on purpose for one lost.
func (s Store) RecordRemoved(ctx context.Context, at time.Time, captures []Capture) error {
	var data []byte
	for _, c := range captures {
		if err := c.check(); err != nil {
			return err
		}
		line, err := json.Marshal(Removal{V: 1, At: at.Format(time.RFC3339), Host: c.Host, Session: c.ID})
		if err != nil {
			return fmt.Errorf("inbox: encode: %w", err)
		}
		data = append(append(data, line...), '\n')
	}
	if len(data) == 0 {
		return nil
	}
	root, err := paths.OpenRoot(s.Root, true)
	if err != nil {
		return fmt.Errorf("inbox: root: %w", err)
	}
	defer root.Close()
	f, err := openRegular(root, removedName, os.O_APPEND|os.O_WRONLY, true)
	if err != nil {
		return fmt.Errorf("inbox: open the record of removals: %w", err)
	}
	defer f.Close()
	if err := lock(ctx, f); err != nil {
		return err
	}
	defer syscall.Flock(int(f.Fd()), syscall.LOCK_UN)
	if err := f.Chmod(0o600); err != nil {
		return fmt.Errorf("inbox: permissions: %w", err)
	}
	// A line a killed writer left unfinished is ended first, so the new
	// lines decode.
	partial, err := partialTail(root, removedName, f)
	if err != nil {
		return err
	}
	if partial {
		data = append([]byte{'\n'}, data...)
	}
	if _, err := f.Write(data); err != nil {
		return fmt.Errorf("inbox: append to the record of removals: %w", err)
	}
	if err := f.Sync(); err != nil {
		return fmt.Errorf("inbox: sync: %w", err)
	}
	return paths.SyncDirAt(root, ".")
}

// Removed reads the record of the sessions removed. A line that doesn't
// decode, names no session or is unfinished, as a killed writer leaves it,
// is passed over; past 16 MiB the record is not read. A missing record
// lists none.
func (s Store) Removed() ([]Removal, error) {
	root, err := paths.OpenRoot(s.Root, false)
	if errors.Is(err, fs.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("inbox: %w", err)
	}
	defer root.Close()
	f, err := openRegular(root, removedName, os.O_RDONLY, false)
	if errors.Is(err, fs.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("inbox: read the record of removals: %w", err)
	}
	defer f.Close()
	var out []Removal
	r := bufio.NewReaderSize(io.LimitReader(f, maxRemoved), 64<<10)
	var buf []byte
	for {
		line, ok, _, err := readLine(r, &buf)
		if errors.Is(err, io.EOF) {
			return out, nil
		}
		if err != nil {
			return out, fmt.Errorf("inbox: read the record of removals: %w", err)
		}
		var rm Removal
		if !ok || json.Unmarshal(line, &rm) != nil || rm.Session == "" {
			continue
		}
		out = append(out, rm)
	}
}
