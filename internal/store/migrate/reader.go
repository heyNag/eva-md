package migrate

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"slices"
	"syscall"

	"github.com/heyNag/eva-md/internal/paths"
)

// Known is the newest schema generation this binary reads and writes.
const Known = 2

// Access is what a binary may do with a store, by the reader rule.
type Access struct {
	Read, Write bool
	Schema      int
	MinReader   int
	StoreID     string
}

// ErrTooOld is the reader rule's refusal: the store needs a binary that
// knows a newer generation. A *TooOldError is ErrTooOld by errors.Is.
var ErrTooOld = errors.New("migrate: this memory needs a newer eva-md")

// ErrNoManifest means the store's manifest is missing or unreadable, so
// nothing may be read or written.
var ErrNoManifest = errors.New("migrate: the memory's manifest is missing or unreadable")

// TooOldError says which store refused this binary, and why.
type TooOldError struct {
	StoreID   string
	Schema    int
	MinReader int
	Known     int
}

func (e *TooOldError) Error() string {
	return fmt.Sprintf("migrate: this memory needs a reader of schema %d; this one reads %d", e.MinReader, e.Known)
}

func (e *TooOldError) Is(target error) bool { return target == ErrTooOld }

// maxManifest is the most of store.toml the reader rule reads.
const maxManifest = 4 << 10

// Check applies the reader rule to the store at root for a binary that
// knows generations up to known. It reads only .eva-md/store.toml, never
// through a symlink and at most 4 KiB. A min_reader above known allows
// nothing and returns a *TooOldError; a schema above known allows reading
// only; a missing or unreadable manifest allows nothing and returns
// ErrNoManifest.
func Check(root string, known int) (Access, error) {
	b, err := readManifest(root)
	if err != nil {
		return Access{}, fmt.Errorf("%w: %v", ErrNoManifest, err)
	}
	m, err := parseManifest(b)
	if err != nil {
		return Access{}, fmt.Errorf("%w: %v", ErrNoManifest, err)
	}
	a := Access{Schema: m.schema, MinReader: m.minReader, StoreID: m.storeID}
	switch {
	case m.minReader > known:
		return a, &TooOldError{StoreID: m.storeID, Schema: m.schema, MinReader: m.minReader, Known: known}
	case m.schema > known:
		a.Read = true
	default:
		a.Read, a.Write = true, true
	}
	return a, nil
}

// readManifest reads root's store.toml with no symlink on the way from
// root: neither .eva-md nor the file may be one.
func readManifest(root string) ([]byte, error) {
	dir := filepath.Join(root, ".eva-md")
	info, err := os.Lstat(dir)
	if err != nil {
		return nil, err
	}
	if !info.IsDir() {
		return nil, errors.New(".eva-md is not a folder")
	}
	return readSmall(filepath.Join(dir, "store.toml"), maxManifest)
}

// readSmall reads a regular file, never through a symlink, and refuses one
// longer than limit.
func readSmall(file string, limit int64) ([]byte, error) {
	f, err := os.OpenFile(file, os.O_RDONLY|syscall.O_NOFOLLOW|syscall.O_NONBLOCK, 0)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	info, err := f.Stat()
	if err != nil {
		return nil, err
	}
	if !info.Mode().IsRegular() {
		return nil, errors.New("not a regular file")
	}
	b, err := io.ReadAll(io.LimitReader(f, limit+1))
	if err != nil {
		return nil, err
	}
	if int64(len(b)) > limit {
		return nil, errors.New("too long")
	}
	return b, nil
}

// noticeFile records the refusals already said, by store id and minimum
// reader.
const noticeFile = "reader-notice.json"

// maxNotices bounds the record; the oldest are dropped past it.
const maxNotices = 64

type notice struct {
	StoreID   string `json:"store_id"`
	MinReader int    `json:"min_reader"`
}

// Notice gives the line that says a store refused this binary, and whether
// to say it: once per store id and minimum reader, as recorded in
// stateDir. When the record cannot be written, it says it every time. An
// error that is not a *TooOldError says nothing.
func Notice(stateDir string, err error) (line string, say bool) {
	var tooOld *TooOldError
	if !errors.As(err, &tooOld) {
		return "", false
	}
	line = fmt.Sprintf("eva.md: this memory needs a newer eva-md (schema %d; this one reads %d). Nothing was read or written. Run eva-md upgrade.", tooOld.MinReader, tooOld.Known)
	this := notice{StoreID: tooOld.StoreID, MinReader: tooOld.MinReader}
	file := filepath.Join(stateDir, noticeFile)
	var said []notice
	if b, err := readSmall(file, 64<<10); err == nil {
		if json.Unmarshal(b, &said) != nil {
			said = nil
		}
	}
	if slices.Contains(said, this) {
		return "", false
	}
	said = append(said, this)
	if len(said) > maxNotices {
		said = said[len(said)-maxNotices:]
	}
	if b, err := json.Marshal(said); err == nil && paths.EnsureDir(stateDir) == nil {
		_ = paths.WriteFileAtomic(file, b, 0o600)
	}
	return line, true
}
