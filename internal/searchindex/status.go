package searchindex

import (
	"errors"
	"io/fs"
	"path/filepath"
	"time"

	"github.com/heyNag/eva-md/internal/paths"
)

// Status is what the index covers, as the doctor, eva-md status and eva-md
// index --json report it. It is read from the manifests and the folder
// listings alone.
type Status struct {
	Folders  []FolderStatus `json:"folders"`
	Indexed  int            `json:"indexed"`       // project folders with an index
	Covered  int64          `json:"covered_bytes"` // inbox bytes the index covers
	Inbox    int64          `json:"inbox_bytes"`   // inbox bytes in session files
	Segments int            `json:"segments"`
	Size     int64          `json:"size_bytes"` // the segments' bytes
	Built    time.Time      `json:"built"`      // when the index was last written
	By       string         `json:"by"`         // and by what
	Damaged  []string       `json:"damaged"`    // project folders whose index can't be used
}

// FolderStatus is Status for one project folder.
type FolderStatus struct {
	Name     string    `json:"name"`
	Covered  int64     `json:"covered_bytes"`
	Inbox    int64     `json:"inbox_bytes"`
	Segments int       `json:"segments"`
	Size     int64     `json:"size_bytes"`
	Built    time.Time `json:"built"`
	By       string    `json:"by"`
	Damaged  bool      `json:"damaged"`
}

// Inspect reports what the index covers of each inbox project folder: the
// bytes of the session files that are indexed, against their sizes, and
// the segments. A folder whose manifest can't be read, or names a segment
// that is missing or of another size, is named as damaged. It writes
// nothing and takes no lock.
func Inspect(dirs paths.Dirs) Status {
	st := Status{Damaged: []string{}, Folders: []FolderStatus{}}
	folders, _ := subfolders(dirs.Inbox())
	for _, name := range folders {
		f := FolderStatus{Name: name}
		listing, _ := listFolder(filepath.Join(dirs.Inbox(), name))
		for _, file := range listing {
			f.Inbox += file.Size
		}
		dir, err := folderPath(dirs.SearchIndex(), name)
		if err != nil {
			continue
		}
		root, err := paths.OpenRoot(dir, false)
		if err == nil {
			var m Manifest
			m, err = ReadManifest(root)
			if err == nil {
				f.Segments, f.Size, f.Built, f.By = len(m.Segments), size(m), m.Built, m.By
				for _, s := range m.Segments {
					if info, e := root.Lstat(s.File); e != nil || !info.Mode().IsRegular() || info.Size() != s.Bytes {
						err = damaged("a segment is missing")
					}
				}
				for _, file := range listing {
					if state, ok := m.Files[file.Name]; ok && state.Ino == file.Ino {
						f.Covered += min(state.To, file.Size)
					}
				}
			}
			root.Close()
		}
		switch {
		case errors.Is(err, fs.ErrNotExist):
		case err != nil:
			f.Damaged = true
			st.Damaged = append(st.Damaged, name)
		default:
			st.Indexed++
		}
		st.Folders = append(st.Folders, f)
		st.Inbox += f.Inbox
		if !f.Damaged {
			st.Covered += f.Covered
		}
		st.Segments += f.Segments
		st.Size += f.Size
		if f.Built.After(st.Built) {
			st.Built, st.By = f.Built, f.By
		}
	}
	return st
}
