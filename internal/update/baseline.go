package update

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/heyNag/eva-md/internal/inbox"
	"github.com/heyNag/eva-md/internal/paths"
)

// baselineName is the inbox's record, at its root, that the chats captured
// before updates were set aside, written at the first try. Its first line is
// that time. Each later line names what couldn't be set aside then: "left
// <size> <file>", a session file by its path in the inbox, with its size at
// the first try, or "folder <dir>", a project folder that couldn't be read.
const baselineName = ".baseline"

// Why a session file from before updates is left.
var (
	errLeftOpen = errors.New("update: a chat from before updates that couldn't be set aside yet: its file can't be opened")
	errLeftRead = errors.New("update: a chat from before updates that couldn't be set aside yet: its watermark can't be read")
	errLeftSave = errors.New("update: a chat from before updates that couldn't be set aside yet: its watermark can't be saved")
)

// LeftError is a baseline that left session files or project folders to set
// aside later.
type LeftError struct{ Files, Folders int }

func (e *LeftError) Error() string {
	return fmt.Sprintf("update: %d session files and %d project folders from before updates couldn't be set aside yet", e.Files, e.Folders)
}

// Baseline sets aside the chats the inbox holds from before updates: at the
// first try, every session file the update hasn't touched gets a watermark at
// the end of its last whole line, so the update never works through history
// captured before it ran, and the record is written, so no file made later is
// ever set aside. Install calls it, so a new install, whose inbox is empty,
// sets aside nothing; each job does too (baseline), for a binary replaced
// without an install. It takes the update lock, and leaves the work to a job
// that holds it. A *LeftError says what is left for later.
func Baseline(dirs paths.Dirs, now time.Time) error {
	unlock, busy, err := lockUpdates(dirs.Locks())
	if busy || err != nil {
		return err
	}
	defer unlock()
	left, folders, _, err := baseline(dirs, now)
	if err == nil && len(left)+len(folders) > 0 {
		err = &LeftError{Files: len(left), Folders: len(folders)}
	}
	return err
}

// baseline is Baseline for a job, which holds the update lock. A session file
// it can't set aside, as one whose watermark can't be read or written or that
// can't be opened, is listed in the record, and each later call tries it
// again, up to its size at the first try. So is a project folder it can't
// read: once it can, its session files last changed by the first try are set
// aside, and later ones are left to the update. It returns why each file is
// still left, by path: the job records those as failed and never drains them,
// so their history is never worked through. It also returns the folders
// still unread, and the files the record listed as left that are set aside
// or gone now, so the job can record that they no longer wait.
func baseline(dirs paths.Dirs, now time.Time) (map[string]error, []string, []sessionFile, error) {
	root, err := paths.OpenRoot(dirs.Inbox(), true)
	if err != nil {
		return nil, nil, nil, fmt.Errorf("update: baseline: %w", err)
	}
	defer root.Close()
	store := inbox.Store{Root: dirs.Inbox()}
	rec, err := readBaseline(root, dirs.Inbox())
	if err != nil {
		return nil, nil, nil, fmt.Errorf("update: baseline: %w", err)
	}
	todo, unread := rec.left, []string(nil)
	switch {
	case !rec.found:
		rec.stamp = now.UTC().Format(time.RFC3339Nano)
		files, _, folders, err := listInbox(store)
		if err != nil {
			return nil, nil, nil, err
		}
		todo, unread = files, folders
	case len(rec.left)+len(rec.folders) == 0:
		return nil, nil, nil, nil
	default:
		first := rec.first(now)
		for _, dir := range rec.folders {
			files, err := folderSessions(root, store, dir)
			switch {
			case errors.Is(err, fs.ErrNotExist):
				continue
			case err != nil:
				unread = append(unread, dir)
				continue
			}
			for _, f := range files {
				if !f.mod.After(first) {
					todo = append(todo, f)
				}
			}
		}
	}
	left := map[string]error{}
	record := rec.stamp + "\n"
	for _, f := range todo {
		if _, err := os.Lstat(f.path); !errors.Is(err, fs.ErrNotExist) {
			if why := setAside(store, f); why != nil {
				left[f.path] = why
				record += "left " + strconv.FormatInt(f.size, 10) + " " + f.dir + "/" + f.name + "\n"
			}
		}
	}
	var cleared []sessionFile
	for _, f := range rec.left {
		if _, still := left[f.path]; !still {
			cleared = append(cleared, f)
		}
	}
	for _, dir := range unread {
		record += "folder " + dir + "\n"
	}
	if record != rec.raw {
		if err := paths.WriteFileAtomicAt(context.Background(), root, baselineName, []byte(record), 0o600); err != nil {
			return nil, nil, nil, fmt.Errorf("update: baseline: %w", err)
		}
	}
	return left, unread, cleared, nil
}

// setAside gives a session file the update hasn't touched a watermark at the
// end of its last whole line within f.size, and says why it couldn't.
func setAside(store inbox.Store, f sessionFile) error {
	m, err := store.Mark(f.path)
	switch {
	case err != nil:
		return errLeftRead
	case m != (inbox.Mark{}):
		return nil
	}
	end, ok := lineEnd(store, f)
	switch {
	case !ok:
		return errLeftOpen
	case end > 0 && store.SetMark(f.path, inbox.Mark{Offset: end}) != nil:
		return errLeftSave
	}
	return nil
}

// whyLeft says why a session file the record lists is still left, as a dry
// run tells it without trying again, which would write: its file can't be
// opened, or its watermark can't be read, or else the next update tries to
// set it aside again (nil).
func whyLeft(store inbox.Store, path string) error {
	if f, err := store.Open(path); err != nil {
		return errLeftOpen
	} else {
		f.Close()
	}
	if _, err := store.Mark(path); err != nil {
		return errLeftRead
	}
	return nil
}

// baselineRecord is what the record holds.
type baselineRecord struct {
	found   bool
	raw     string        // the record as read
	stamp   string        // its first line, the time of the first try
	left    []sessionFile // its left files, each with its size then
	folders []string      // its folders not read then
}

// first is the time of the first try. A stamp that can't be read counts as
// now, so a folder's chats are all taken for chats from before updates and
// set aside, never worked through.
func (rec baselineRecord) first(now time.Time) time.Time {
	if t, err := time.Parse(time.RFC3339Nano, rec.stamp); err == nil {
		return t
	}
	return now
}

// readBaseline reads the record. A line naming no plain session file or
// project folder of the inbox is ignored.
func readBaseline(root *os.Root, inboxDir string) (baselineRecord, error) {
	var rec baselineRecord
	f, err := root.OpenFile(baselineName, os.O_RDONLY|syscall.O_NOFOLLOW, 0)
	if errors.Is(err, fs.ErrNotExist) {
		return rec, nil
	}
	if err != nil {
		return rec, err
	}
	defer f.Close()
	data, err := io.ReadAll(io.LimitReader(f, 1<<20))
	if err != nil {
		return rec, err
	}
	rec.found, rec.raw = true, string(data)
	lines := strings.Split(strings.TrimRight(rec.raw, "\n"), "\n")
	rec.stamp = lines[0]
	for _, line := range lines[1:] {
		kind, rest, _ := strings.Cut(line, " ")
		switch kind {
		case "folder":
			if filepath.IsLocal(rest) && !strings.ContainsAny(rest, "/") && !strings.HasPrefix(rest, ".") {
				rec.folders = append(rec.folders, rest)
			}
		case "left":
			sizeText, file, _ := strings.Cut(rest, " ")
			size, err := strconv.ParseInt(sizeText, 10, 64)
			dir, name, ok := strings.Cut(file, "/")
			if err != nil || size < 0 || !ok || !filepath.IsLocal(file) || strings.Contains(name, "/") {
				continue
			}
			if host, _, ok := sessionID(name); ok {
				rec.left = append(rec.left, sessionFile{dir: dir, name: name, path: filepath.Join(inboxDir, dir, name), host: host, size: size})
			}
		}
	}
	return rec, nil
}

// baselined reads the record for a dry run: whether it exists, the session
// files it lists as left, by path, and whether a session file from a folder
// it lists as not read would be set aside by the next update.
func baselined(dirs paths.Dirs, now time.Time) (found bool, left map[string]bool, aside func(sessionFile) bool, err error) {
	left = map[string]bool{}
	aside = func(sessionFile) bool { return false }
	root, err := paths.OpenRoot(dirs.Inbox(), false)
	if errors.Is(err, fs.ErrNotExist) {
		return false, left, aside, nil
	}
	if err != nil {
		return false, left, aside, err
	}
	defer root.Close()
	rec, err := readBaseline(root, dirs.Inbox())
	for _, f := range rec.left {
		left[f.path] = true
	}
	first := rec.first(now)
	aside = func(f sessionFile) bool { return slices.Contains(rec.folders, f.dir) && !f.mod.After(first) }
	return rec.found, left, aside, err
}

// lineEnd returns where the last whole line of a session file ends, within
// its first f.size bytes, read from their last 1 MiB, which always holds one
// of the inbox's capped lines.
func lineEnd(store inbox.Store, f sessionFile) (int64, bool) {
	file, err := store.Open(f.path)
	if err != nil {
		return 0, false
	}
	defer file.Close()
	start := max(f.size-1<<20, 0)
	buf := make([]byte, f.size-start)
	n, err := file.ReadAt(buf, start)
	if err != nil && !errors.Is(err, io.EOF) {
		return 0, false
	}
	i := bytes.LastIndexByte(buf[:n], '\n')
	if i < 0 {
		return 0, start == 0
	}
	return start + int64(i) + 1, true
}
