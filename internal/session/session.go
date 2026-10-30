// Package session stores small per-session marker files, such as whether a
// chat is off the record or has already received its brief.
package session

import (
	"context"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"syscall"

	"github.com/heyNag/eva-md/internal/paths"
)

// Marker names.
const (
	OffRecord = "off_record" // set while the chat is off the record: "asked" or "1" (see catchup)
	Briefed   = "briefed"    // JSON: the brief's Now hash, its variant and whether the refresh was sent
	Model     = "model"      // the chat's model, when the host reported it
	Thread    = "thread"     // the thread the session last wrote to
)

// MaxValue caps the size of a marker's value.
const MaxValue = 64 << 10

// Markers stores one file per marker at Root/<host>/<session>/<name>.
// Host, session and name are passed through paths.SafeName, so values taken
// from hook payloads can't escape Root.
type Markers struct{ Root string } // paths.Dirs.Sessions()

// Set stores value for the marker, replacing any earlier value. The file is
// written atomically with mode 0600 in folders with mode 0700.
func (m Markers) Set(host, session, name, value string) error {
	return m.SetContext(context.Background(), host, session, name, value)
}

// SetContext is Set with cancellation checks before filesystem mutations.
func (m Markers) SetContext(ctx context.Context, host, session, name, value string) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if len(value) > MaxValue {
		return fmt.Errorf("session: marker %s is %d bytes, more than %d", name, len(value), MaxValue)
	}
	dir, file, err := m.locate(host, session, name)
	if err != nil {
		return err
	}
	root, err := paths.OpenRoot(m.Root, true)
	if err != nil {
		return fmt.Errorf("session: %w", err)
	}
	defer root.Close()
	if err := ctx.Err(); err != nil {
		return err
	}
	if err := root.MkdirAll(dir, 0o700); err != nil {
		return fmt.Errorf("session: create directory: %w", err)
	}
	if err := paths.WriteFileAtomicAt(ctx, root, file, []byte(value), 0o600); err != nil {
		return fmt.Errorf("session: %w", err)
	}
	return nil
}

// Get returns the marker's value and whether it is set.
func (m Markers) Get(host, session, name string) (string, bool, error) {
	return m.GetContext(context.Background(), host, session, name)
}

// GetContext reads only bounded regular files and never waits for a FIFO writer.
func (m Markers) GetContext(ctx context.Context, host, session, name string) (string, bool, error) {
	if err := ctx.Err(); err != nil {
		return "", false, err
	}
	_, file, err := m.locate(host, session, name)
	if err != nil {
		return "", false, err
	}
	root, err := paths.OpenRoot(m.Root, false)
	if errors.Is(err, fs.ErrNotExist) {
		return "", false, nil
	}
	if err != nil {
		return "", false, fmt.Errorf("session: %w", err)
	}
	defer root.Close()
	before, err := root.Lstat(file)
	if errors.Is(err, fs.ErrNotExist) {
		return "", false, nil
	}
	if err != nil || !before.Mode().IsRegular() {
		return "", false, errors.New("session: marker is not a regular file")
	}
	f, err := root.OpenFile(file, os.O_RDONLY|syscall.O_NONBLOCK|syscall.O_NOFOLLOW, 0)
	if errors.Is(err, fs.ErrNotExist) {
		return "", false, nil
	}
	if err != nil {
		return "", false, fmt.Errorf("session: read marker: %w", err)
	}
	defer f.Close()
	info, err := f.Stat()
	if err != nil || !info.Mode().IsRegular() || !os.SameFile(before, info) {
		return "", false, errors.New("session: marker is not a regular file")
	}
	if stat, ok := info.Sys().(*syscall.Stat_t); ok && stat.Nlink > 1 {
		return "", false, errors.New("session: marker has multiple links")
	}
	if err := ctx.Err(); err != nil {
		return "", false, err
	}
	data, err := io.ReadAll(io.LimitReader(f, MaxValue+1))
	if err != nil {
		return "", false, fmt.Errorf("session: read marker: %w", err)
	}
	if len(data) > MaxValue {
		return "", false, fmt.Errorf("session: marker %s is larger than %d bytes", name, MaxValue)
	}
	return string(data), true, nil
}

// Clear removes the marker. Clearing a marker that isn't set is not an error.
func (m Markers) Clear(host, session, name string) error {
	return m.ClearContext(context.Background(), host, session, name)
}

// ClearContext is Clear with cancellation checks before removing the marker.
func (m Markers) ClearContext(ctx context.Context, host, session, name string) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	dir, file, err := m.locate(host, session, name)
	if err != nil {
		return err
	}
	root, err := paths.OpenRoot(m.Root, false)
	if errors.Is(err, fs.ErrNotExist) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("session: %w", err)
	}
	defer root.Close()
	if err := ctx.Err(); err != nil {
		return err
	}
	if err := root.Remove(file); err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return nil
		}
		return fmt.Errorf("session: clear marker: %w", err)
	}
	if err := paths.SyncDirAt(root, dir); err != nil {
		return fmt.Errorf("session: %w", err)
	}
	return nil
}

func (m Markers) locate(host, session, name string) (dir, file string, err error) {
	if !filepath.IsAbs(m.Root) {
		return "", "", fmt.Errorf("session: root %q is not an absolute path", m.Root)
	}
	parts := make([]string, 3)
	for i, v := range []struct{ what, s string }{{"host", host}, {"session", session}, {"name", name}} {
		safe, err := paths.SafeName(v.s)
		if err != nil {
			return "", "", fmt.Errorf("session: %s: %w", v.what, err)
		}
		parts[i] = safe
	}
	dir = filepath.Join(parts[0], parts[1])
	return dir, filepath.Join(dir, parts[2]), nil
}
