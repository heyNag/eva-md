package searchindex

import (
	"errors"
	"fmt"
	"io/fs"
	"time"

	"github.com/heyNag/eva-md/internal/paths"
)

// Hold is the index of some inbox project folders, held for a removal of
// session files from them (eva-md inbox clear). While it holds a folder's
// index, no catch-up or search top-up writes it, so none can index a file
// being removed, and Drop deletes that index whole, with every word of the
// files removed: a catch-up, by contrast, merges a deleted file's words out
// of the segments in use and deletes the segment that held them only
// PruneAge later.
type Hold struct {
	dirs    paths.Dirs
	unlocks map[string]func()
}

// HoldFolders takes the index lock of each inbox project folder named,
// trying again for up to wait while another writer holds one, as a search's
// top-up does for well under a second. busy reports that one stayed held;
// it then holds none.
func HoldFolders(dirs paths.Dirs, names []string, wait time.Duration) (h *Hold, busy bool, err error) {
	h = &Hold{dirs: dirs, unlocks: map[string]func(){}}
	until := time.Now().Add(wait)
	for _, name := range names {
		if _, held := h.unlocks[name]; held {
			continue
		}
		dir, err := folderPath(dirs.SearchIndex(), name)
		if err != nil {
			h.Release()
			return nil, false, err
		}
		for {
			unlock, busy, err := lockFolder(dir)
			if err != nil {
				h.Release()
				return nil, false, err
			}
			if !busy {
				h.unlocks[name] = unlock
				break
			}
			if !time.Now().Before(until) {
				h.Release()
				return nil, true, nil
			}
			time.Sleep(min(busyWait, time.Until(until)))
		}
	}
	return h, false, nil
}

// Drop deletes the index of a project folder h holds, the lock file it
// holds included. eva-md index and the daily maintain build it again from
// the inbox, and a search scans that folder's files meanwhile. A writer
// waiting for the lock takes a new one in the folder made again.
func (h *Hold) Drop(name string) error {
	if _, held := h.unlocks[name]; !held {
		return errors.New("searchindex: the folder's index is not held")
	}
	root, err := paths.OpenRoot(h.dirs.SearchIndex(), false)
	if errors.Is(err, fs.ErrNotExist) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("searchindex: %w", err)
	}
	defer root.Close()
	if err := root.RemoveAll(name); err != nil {
		return fmt.Errorf("searchindex: drop the index of %s: %w", name, err)
	}
	return nil
}

// Release releases every lock h holds.
func (h *Hold) Release() {
	if h == nil {
		return
	}
	for name, unlock := range h.unlocks {
		unlock()
		delete(h.unlocks, name)
	}
}
