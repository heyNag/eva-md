package searchindex

import (
	"bytes"
	"context"
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"testing"
)

// A folder's index held for a removal takes no writes: a top-up comes back
// busy. Dropping it deletes every file that held a word of the session
// removed at once, with no wait for a catch-up and the prune after it, and
// the next catch-up builds it again from what the inbox holds.
func TestHoldDropsAFoldersIndexWhole(t *testing.T) {
	d := fixture(t)
	prompt(t, d, "keep", 0, "Ordinary chat line about the export")
	prompt(t, d, "leak", 1, "the wifi passphrase is zanzibarquokka77")
	catchupOK(t, d)
	h, busy, err := HoldFolders(d, []string{"general"}, 0)
	if err != nil || busy {
		t.Fatal(busy, err)
	}
	defer h.Release()
	if r, err := Refresh(context.Background(), d, "general", Options{By: "search"}); err != nil || !r.Busy {
		t.Fatalf("a top-up wrote a held folder: %+v %v", r, err)
	}
	if err := os.Remove(sessionPath(d, "leak")); err != nil {
		t.Fatal(err)
	}
	if err := h.Drop("general"); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Lstat(filepath.Join(d.SearchIndex(), "general")); !errors.Is(err, fs.ErrNotExist) {
		t.Fatalf("the folder's index stayed: %v", err)
	}
	h.Release()
	if r := catchupOK(t, d); r.Files != 1 {
		t.Fatalf("the catch-up after: %+v", r)
	}
	err = filepath.WalkDir(d.SearchIndex(), func(path string, e fs.DirEntry, err error) error {
		if err != nil || e.IsDir() {
			return err
		}
		data, err := os.ReadFile(path)
		if err == nil && bytes.Contains(data, []byte("zanzibarquokka")) {
			t.Errorf("%s holds a word of the removed session", path)
		}
		return err
	})
	if err != nil {
		t.Fatal(err)
	}
	if all, _ := lookup(t, d, 0, "export"); len(all) != 1 {
		t.Fatalf("the index built again finds %v", all)
	}
}

// While another writer holds one of the folders past the wait, HoldFolders
// is busy and holds none of them; it drops only a folder it holds.
func TestHoldIsBusyWhileAWriterHoldsAFolder(t *testing.T) {
	d := fixture(t)
	unlock, busy, err := lockFolder(filepath.Join(d.SearchIndex(), "general"))
	if err != nil || busy {
		t.Fatal(busy, err)
	}
	h, busy, err := HoldFolders(d, []string{"acme", "general"}, 0)
	if err != nil || !busy || h != nil {
		t.Fatalf("%v %v %v", h, busy, err)
	}
	other, busy, err := lockFolder(filepath.Join(d.SearchIndex(), "acme"))
	if err != nil || busy {
		t.Fatalf("a busy hold kept a folder: %v %v", busy, err)
	}
	other()
	unlock()
	h, busy, err = HoldFolders(d, []string{"acme"}, 0)
	if err != nil || busy {
		t.Fatal(busy, err)
	}
	defer h.Release()
	if err := h.Drop("general"); err == nil {
		t.Fatal("a folder not held was dropped")
	}
	if _, _, err := HoldFolders(d, []string{"../x"}, 0); err == nil {
		t.Fatal("a folder outside the index was held")
	}
}
