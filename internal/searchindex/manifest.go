package searchindex

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"time"

	"github.com/heyNag/eva-md/internal/paths"
)

// manifestName is the manifest's name in an index folder.
const manifestName = "manifest.json"

// Manifest says what an index folder holds: for each session file of its
// inbox folder how far it is indexed, and the segments holding the
// documents. It holds names, counts and offsets only.
type Manifest struct {
	V        int                  `json:"v"`
	Built    time.Time            `json:"built"`
	By       string               `json:"by"` // maintain, index or search
	Files    map[string]FileState `json:"files"`
	Segments []SegmentInfo        `json:"segments"`
	// Retired names the segments a merge or a rebuild replaced, with when,
	// so they are deleted only after PruneAge, while a search may still be
	// opening them.
	Retired []Retired `json:"retired,omitempty"`
}

// FileState is how far a session file is indexed: To is the byte offset
// indexed up to, always just after a newline, Lines the physical lines
// before it and Off whether the session was off the record there. Ino is
// the file's inode, and Gen its generation: a file found shrunk, replaced
// or gone gets the next one, which makes its earlier documents dead. A file
// gone keeps its state, with Ino 0, while a segment names it.
type FileState struct {
	Gen   uint32 `json:"gen"`
	To    int64  `json:"to"`
	Lines int    `json:"lines"`
	Ino   uint64 `json:"ino"`
	Off   bool   `json:"off"`
}

// SegmentInfo is a segment as the manifest names it.
type SegmentInfo struct {
	File   string `json:"file"`
	Docs   int    `json:"docs"`
	Tokens int64  `json:"tokens"`
	Bytes  int64  `json:"bytes"`
}

// Retired is a segment a manifest no longer names, and when it stopped.
type Retired struct {
	File string    `json:"file"`
	At   time.Time `json:"at"`
}

// errVersion reports a manifest of another format version.
var errVersion = fmt.Errorf("%w: another format version", ErrDamaged)

// newManifest is the manifest of a folder nothing is indexed in yet.
func newManifest() Manifest {
	return Manifest{V: FormatVersion, Files: map[string]FileState{}}
}

// ReadManifest reads the manifest of the index folder root. A folder with
// none comes back as fs.ErrNotExist; a manifest over MaxManifest, of another
// version, that does not decode or names a file it can't hold is
// ErrDamaged.
func ReadManifest(root *os.Root) (Manifest, error) {
	f, err := root.OpenFile(manifestName, os.O_RDONLY|syscall.O_NOFOLLOW|syscall.O_NONBLOCK, 0)
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return Manifest{}, err
		}
		return Manifest{}, damaged("the manifest can't be opened")
	}
	defer f.Close()
	info, err := f.Stat()
	if err != nil || !info.Mode().IsRegular() || info.Size() > MaxManifest {
		return Manifest{}, damaged("the manifest's size")
	}
	data, err := io.ReadAll(io.LimitReader(f, MaxManifest+1))
	if err != nil || len(data) > MaxManifest {
		return Manifest{}, damaged("the manifest can't be read")
	}
	var m Manifest
	if err := json.Unmarshal(data, &m); err != nil {
		return Manifest{}, damaged("the manifest does not decode")
	}
	if m.V != FormatVersion {
		return Manifest{}, errVersion
	}
	if m.Files == nil {
		m.Files = map[string]FileState{}
	}
	for name, st := range m.Files {
		if !plainName(name) || st.To < 0 || st.Lines < 0 || st.To > 0 && st.Lines == 0 {
			return Manifest{}, damaged("the manifest's files")
		}
	}
	for _, s := range m.Segments {
		if !segmentPattern.MatchString(s.File) || s.Docs < 0 || s.Bytes < 0 {
			return Manifest{}, damaged("the manifest's segments")
		}
	}
	return m, nil
}

// publish replaces the folder's manifest atomically.
func publish(ctx context.Context, root *os.Root, m Manifest) error {
	data, err := json.Marshal(m)
	if err != nil {
		return fmt.Errorf("searchindex: encode the manifest: %w", err)
	}
	if err := paths.WriteFileAtomicAt(ctx, root, manifestName, data, 0o600); err != nil {
		return fmt.Errorf("searchindex: %w", err)
	}
	return nil
}

// lockFolder takes an index folder's lock without waiting; busy reports
// that another writer holds it. Searches read without it.
func lockFolder(dir string) (unlock func(), busy bool, err error) {
	unlock, busy, err = paths.TryLock(dir, "lock")
	if err != nil {
		err = fmt.Errorf("searchindex: %w", err)
	}
	return unlock, busy, err
}

// retire moves the segments named out of m's list into its retired ones,
// as of now.
func retire(m *Manifest, names []string, now time.Time) {
	gone := map[string]bool{}
	for _, n := range names {
		gone[n] = true
	}
	var kept []SegmentInfo
	for _, s := range m.Segments {
		if gone[s.File] {
			m.Retired = append(m.Retired, Retired{File: s.File, At: now})
			continue
		}
		kept = append(kept, s)
	}
	m.Segments = kept
}

// prune deletes from the index folder root, under its lock, what no reader
// can still need: a segment m does not name that was retired more than
// PruneAge ago, or never named and last changed that long ago, and a
// temporary file as old. It drops the retired entries it deleted, and
// reports whether m changed.
func prune(root *os.Root, m *Manifest, now time.Time) (changed bool, err error) {
	d, err := root.Open(".")
	if err != nil {
		return false, err
	}
	entries, err := d.ReadDir(-1)
	d.Close()
	if err != nil {
		return false, err
	}
	named := map[string]bool{manifestName: true, "lock": true}
	for _, s := range m.Segments {
		named[s.File] = true
	}
	retired := map[string]time.Time{}
	for _, r := range m.Retired {
		retired[r.File] = r.At
	}
	present := map[string]bool{}
	for _, e := range entries {
		name := e.Name()
		present[name] = true
		if named[name] || !e.Type().IsRegular() || !segmentPattern.MatchString(name) && !strings.HasPrefix(name, paths.TempPrefix) {
			continue
		}
		if at, ok := retired[name]; ok && now.Sub(at) < PruneAge {
			continue
		}
		if info, err := e.Info(); err != nil || now.Sub(info.ModTime()) < PruneAge {
			if _, ok := retired[name]; !ok {
				continue
			}
		}
		if err := root.Remove(name); err == nil || errors.Is(err, fs.ErrNotExist) {
			delete(present, name)
		}
	}
	var kept []Retired
	for _, r := range m.Retired {
		if present[r.File] {
			kept = append(kept, r)
		}
	}
	changed = len(kept) != len(m.Retired)
	m.Retired = kept
	return changed, nil
}

// folderPath is the index folder of the inbox project folder name, which
// must be a plain name.
func folderPath(index, name string) (string, error) {
	if !plainName(name) || strings.HasPrefix(name, ".") {
		return "", errors.New("searchindex: invalid project folder name")
	}
	return filepath.Join(index, name), nil
}
