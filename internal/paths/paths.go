// Package paths locates eva-md's per-user folders and writes files atomically.
package paths

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// TempPrefix starts the name of every temporary file WriteFileAtomic and
// WriteFileAtomicAt create, and of the identity cache's temporary entries.
// A process killed mid-write can leave one behind.
const TempPrefix = ".tmp-eva-"

// maxNameLen caps the length of a name produced by SafeName.
const maxNameLen = 128

// ErrEmptyName is returned by SafeName for an empty name.
var ErrEmptyName = errors.New("paths: empty name")

// Dirs holds eva-md's per-user folders.
type Dirs struct {
	Config string // ~/.config/eva-md
	State  string // ~/.local/state/eva-md
	Share  string // ~/.local/share/eva-md
}

// Default returns the folders under the current user's home folder.
func Default() (Dirs, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return Dirs{}, fmt.Errorf("paths: home folder: %w", err)
	}
	if !filepath.IsAbs(home) {
		return Dirs{}, fmt.Errorf("paths: home folder %q is not absolute", home)
	}
	return Dirs{
		Config: filepath.Join(home, ".config", "eva-md"),
		State:  filepath.Join(home, ".local", "state", "eva-md"),
		Share:  filepath.Join(home, ".local", "share", "eva-md"),
	}, nil
}

// Inbox is the root of the local capture inbox.
func (d Dirs) Inbox() string { return filepath.Join(d.State, "inbox") }

// Sessions holds the per-session marker files.
func (d Dirs) Sessions() string { return filepath.Join(d.State, "sessions") }

// Locks holds eva-md's lock files.
func (d Dirs) Locks() string { return filepath.Join(d.State, "locks") }

// Logs holds the logs of detached jobs, such as the background update's
// update-<YYYY-MM-DD>.log.
func (d Dirs) Logs() string { return filepath.Join(d.State, "logs") }

// Cache holds caches that are rebuilt when missing, such as the identity
// cache. Deleting it loses nothing.
func (d Dirs) Cache() string { return filepath.Join(d.State, "cache") }

// ConfigFile is the per-Mac config file.
func (d Dirs) ConfigFile() string { return filepath.Join(d.Config, "config.toml") }

// EnsureDir creates path and any missing parents with mode 0700.
func EnsureDir(path string) error {
	if err := os.MkdirAll(path, 0o700); err != nil {
		return fmt.Errorf("paths: create %s: %w", path, err)
	}
	return nil
}

// OpenRoot opens a directory for traversal-confined operations. It optionally
// creates missing directories with mode 0700 and refuses symlink ancestors,
// except macOS's standard /var, /tmp and /etc aliases under /private.
func OpenRoot(path string, create bool) (*os.Root, error) {
	if !filepath.IsAbs(path) {
		return nil, errors.New("paths: root must be absolute")
	}
	path = filepath.Clean(path)
	for _, alias := range []string{"/var", "/tmp", "/etc"} {
		if path == alias || strings.HasPrefix(path, alias+"/") {
			if target, err := os.Readlink(alias); err == nil && filepath.Join("/", target) == "/private"+alias {
				path = "/private" + path
			}
			break
		}
	}
	root, err := os.OpenRoot(string(filepath.Separator))
	if err != nil {
		return nil, fmt.Errorf("paths: open root: %w", err)
	}
	for _, part := range strings.Split(strings.TrimPrefix(path, string(filepath.Separator)), string(filepath.Separator)) {
		if part == "" {
			continue
		}
		info, err := root.Lstat(part)
		if errors.Is(err, os.ErrNotExist) && create {
			if err = root.Mkdir(part, 0o700); err == nil || errors.Is(err, os.ErrExist) {
				info, err = root.Lstat(part)
			}
		}
		if err != nil {
			_ = root.Close()
			return nil, fmt.Errorf("paths: inspect root component: %w", err)
		}
		if !info.IsDir() {
			_ = root.Close()
			return nil, errors.New("paths: root component is not a real directory")
		}
		child, err := root.OpenRoot(part)
		_ = root.Close()
		if err != nil {
			return nil, fmt.Errorf("paths: open root component: %w", err)
		}
		opened, err := child.Stat(".")
		if err != nil || !os.SameFile(info, opened) {
			_ = child.Close()
			return nil, errors.New("paths: root changed while opening")
		}
		root = child
	}
	return root, nil
}

// WriteFileAtomicAt replaces a root-relative file without traversing outside
// root. Cancellation is checked before writing and immediately before rename.
// The caller must create the containing directory first.
func WriteFileAtomicAt(ctx context.Context, root *os.Root, name string, data []byte, perm os.FileMode) (err error) {
	if err := ctx.Err(); err != nil {
		return err
	}
	dir := filepath.Dir(name)
	var random [16]byte
	if _, err := rand.Read(random[:]); err != nil {
		return fmt.Errorf("paths: temporary filename: %w", err)
	}
	tmp := filepath.Join(dir, TempPrefix+hex.EncodeToString(random[:]))
	f, err := root.OpenFile(tmp, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o600)
	if err != nil {
		return fmt.Errorf("paths: temporary file: %w", err)
	}
	defer func() {
		_ = f.Close()
		_ = root.Remove(tmp)
	}()
	if _, err := f.Write(data); err != nil {
		return fmt.Errorf("paths: write: %w", err)
	}
	if err := f.Chmod(perm); err != nil {
		return fmt.Errorf("paths: chmod: %w", err)
	}
	if err := f.Sync(); err != nil {
		return fmt.Errorf("paths: fsync: %w", err)
	}
	if err := f.Close(); err != nil {
		return fmt.Errorf("paths: close: %w", err)
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	if err := root.Rename(tmp, name); err != nil {
		return fmt.Errorf("paths: rename: %w", err)
	}
	return SyncDirAt(root, dir)
}

// SyncDirAt fsyncs a root-relative directory.
func SyncDirAt(root *os.Root, name string) error {
	f, err := root.Open(name)
	if err != nil {
		return fmt.Errorf("paths: open directory: %w", err)
	}
	defer f.Close()
	return f.Sync()
}

// WriteFileAtomic replaces path with data. It writes a temporary file in the
// same folder, sets perm, fsyncs it, renames it over path and fsyncs the
// folder, so a crash leaves either the old file or the new one.
func WriteFileAtomic(path string, data []byte, perm os.FileMode) (err error) {
	dir := filepath.Dir(path)
	f, err := os.CreateTemp(dir, TempPrefix+"*")
	if err != nil {
		return fmt.Errorf("paths: temp file for %s: %w", path, err)
	}
	tmp := f.Name()
	defer func() {
		if err != nil {
			_ = f.Close()
			_ = os.Remove(tmp)
		}
	}()
	if _, err = f.Write(data); err != nil {
		return fmt.Errorf("paths: write %s: %w", tmp, err)
	}
	if err = f.Chmod(perm); err != nil {
		return fmt.Errorf("paths: chmod %s: %w", tmp, err)
	}
	if err = f.Sync(); err != nil {
		return fmt.Errorf("paths: fsync %s: %w", tmp, err)
	}
	if err = f.Close(); err != nil {
		return fmt.Errorf("paths: close %s: %w", tmp, err)
	}
	if err = os.Rename(tmp, path); err != nil {
		return fmt.Errorf("paths: rename %s: %w", path, err)
	}
	return SyncDir(dir)
}

// SyncDir fsyncs a folder so that a rename or create inside it survives a
// power loss.
func SyncDir(dir string) error {
	d, err := os.Open(dir)
	if err != nil {
		return fmt.Errorf("paths: open folder %s: %w", dir, err)
	}
	serr := d.Sync()
	cerr := d.Close()
	if serr != nil {
		return fmt.Errorf("paths: fsync folder %s: %w", dir, serr)
	}
	if cerr != nil {
		return fmt.Errorf("paths: close folder %s: %w", dir, cerr)
	}
	return nil
}

// SafeName turns an outside value (a session id, a host, a project key) into a
// single path component that can't escape its parent folder. Letters, digits,
// '.', '_' and '-' are kept; anything else becomes '_'. A leading '.' becomes
// '_'. Whenever the value had to change, or was longer than 128 bytes, a short
// hash of the original is appended to distinguish ordinary sanitized inputs.
func SafeName(s string) (string, error) {
	if s == "" {
		return "", ErrEmptyName
	}
	var b strings.Builder
	changed := false
	for i := 0; i < len(s); i++ {
		c := s[i]
		ok := c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' || c >= '0' && c <= '9' ||
			c == '_' || c == '-' || (c == '.' && i > 0)
		if ok {
			b.WriteByte(c)
		} else {
			b.WriteByte('_')
			changed = true
		}
	}
	out := b.String()
	if len(out) > maxNameLen || changed && len(out) > maxNameLen-9 {
		out = out[:maxNameLen-9]
		changed = true
	}
	if changed {
		sum := sha256.Sum256([]byte(s))
		out += "-" + hex.EncodeToString(sum[:4])
	}
	return out, nil
}
