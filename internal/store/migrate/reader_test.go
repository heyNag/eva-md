package migrate

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestCheck(t *testing.T) {
	const id = "s01mj780q8r2t6"
	manifest := func(schema, minReader string) string {
		return "schema = " + schema + "\nmin_reader = " + minReader + "\nstore_id = \"" + id + "\"\nepoch = 1\n"
	}
	full := Access{Read: true, Write: true, Schema: 2, MinReader: 2, StoreID: id}
	for _, tc := range []struct {
		name    string
		content string // "" leaves the file out
		setup   func(t *testing.T, root string)
		known   int
		want    Access
		err     error
	}{
		{name: "current", content: manifest("2", "2"), known: 2, want: full},
		{name: "older store", content: manifest("1", "1"), known: 2, want: Access{Read: true, Write: true, Schema: 1, MinReader: 1, StoreID: id}},
		{name: "newer schema, old reader", content: manifest("3", "2"), known: 2, want: Access{Read: true, Schema: 3, MinReader: 2, StoreID: id}},
		{name: "newer min_reader", content: manifest("3", "3"), known: 2, want: Access{Schema: 3, MinReader: 3, StoreID: id}, err: ErrTooOld},
		{name: "known 1", content: manifest("2", "2"), known: 1, want: Access{Schema: 2, MinReader: 2, StoreID: id}, err: ErrTooOld},
		{name: "other lines ignored", content: "# a note\nschema = 2\nflavour = \"x\"\nmin_reader = 2 # the lowest\nstore_id = \"" + id + "\"\n\n[later]\nschema = 9\n", known: 2, want: full},
		{name: "missing", known: 2, err: ErrNoManifest},
		{name: "empty", content: "\n", known: 2, err: ErrNoManifest},
		{name: "schema twice", content: manifest("2", "2") + "schema = 2\n", known: 2, err: ErrNoManifest},
		{name: "min_reader twice", content: manifest("2", "2") + "min_reader = 9\n", known: 2, err: ErrNoManifest},
		{name: "store_id twice", content: manifest("2", "2") + "store_id = \"" + id + "\"\n", known: 2, err: ErrNoManifest},
		{name: "min_reader quoted", content: manifest("2", "2") + "\"min_reader\" = 9\n", known: 2, err: ErrNoManifest},
		{name: "schema single-quoted", content: "'schema' = 3\nmin_reader = 2\nstore_id = \"" + id + "\"\n", known: 2, err: ErrNoManifest},
		{name: "min_reader dotted", content: manifest("2", "2") + "min_reader.major = 9\n", known: 2, err: ErrNoManifest},
		{name: "other keys quoted or dotted", content: manifest("2", "2") + "\"flavour\" = 1\nnote.a = 2\n'x.y' = 3\n", known: 2, want: full},
		{name: "schema not an integer", content: manifest("\"2\"", "2"), known: 2, err: ErrNoManifest},
		{name: "min_reader in hex", content: manifest("2", "0x2"), known: 2, err: ErrNoManifest},
		{name: "min_reader with a sign", content: manifest("2", "+2"), known: 2, err: ErrNoManifest},
		{name: "min_reader missing", content: "schema = 2\nstore_id = \"" + id + "\"\n", known: 2, err: ErrNoManifest},
		{name: "store_id missing", content: "schema = 2\nmin_reader = 2\n", known: 2, err: ErrNoManifest},
		{name: "min_reader zero", content: manifest("2", "0"), known: 2, err: ErrNoManifest},
		{name: "too long", content: manifest("2", "2") + "# " + strings.Repeat("x", 4<<10) + "\n", known: 2, err: ErrNoManifest},
		{name: "symlinked file", setup: func(t *testing.T, root string) {
			other := filepath.Join(root, "elsewhere.toml")
			writeT(t, other, manifest("2", "2"))
			mkdirT(t, filepath.Join(root, ".eva-md"))
			if err := os.Symlink(other, filepath.Join(root, ".eva-md", "store.toml")); err != nil {
				t.Fatal(err)
			}
		}, known: 2, err: ErrNoManifest},
		{name: "symlinked folder", setup: func(t *testing.T, root string) {
			other := filepath.Join(root, "elsewhere")
			writeT(t, filepath.Join(other, "store.toml"), manifest("2", "2"))
			if err := os.Symlink(other, filepath.Join(root, ".eva-md")); err != nil {
				t.Fatal(err)
			}
		}, known: 2, err: ErrNoManifest},
		{name: "a folder", setup: func(t *testing.T, root string) {
			mkdirT(t, filepath.Join(root, ".eva-md", "store.toml"))
		}, known: 2, err: ErrNoManifest},
	} {
		t.Run(tc.name, func(t *testing.T) {
			root := tempDir(t)
			if tc.content != "" {
				writeT(t, filepath.Join(root, ".eva-md", "store.toml"), tc.content)
			}
			if tc.setup != nil {
				tc.setup(t, root)
			}
			a, err := Check(root, tc.known)
			if a != tc.want || !errors.Is(err, tc.err) || (tc.err == nil) != (err == nil) {
				t.Fatalf("Check = %+v, %v; want %+v, %v", a, err, tc.want, tc.err)
			}
			var tooOld *TooOldError
			if errors.As(err, &tooOld) && (tooOld.StoreID != id || tooOld.Known != tc.known || tooOld.MinReader != tc.want.MinReader || tooOld.Schema != tc.want.Schema) {
				t.Fatalf("TooOldError = %+v", tooOld)
			}
		})
	}
}

func TestCheckFixtureStore(t *testing.T) {
	root, err := filepath.Abs(filepath.Join("..", "testdata", "fixture-store"))
	if err != nil {
		t.Fatal(err)
	}
	a, err := Check(root, 1)
	var tooOld *TooOldError
	if !errors.Is(err, ErrTooOld) || !errors.As(err, &tooOld) || a.Read || a.Write {
		t.Fatalf("Check(fixture, 1) = %+v, %v; want ErrTooOld", a, err)
	}
	if tooOld.StoreID != "s01mj780q8r2t6" || tooOld.MinReader != 2 || tooOld.Known != 1 {
		t.Fatalf("TooOldError = %+v", tooOld)
	}
	if a, err := Check(root, Known); err != nil || !a.Read || !a.Write {
		t.Fatalf("Check(fixture, Known) = %+v, %v", a, err)
	}
}

func TestNotice(t *testing.T) {
	state := filepath.Join(tempDir(t), "state")
	tooOld := func(id string, minReader int) error {
		return &TooOldError{StoreID: id, Schema: minReader, MinReader: minReader, Known: Known}
	}
	want := "eva.md: this memory needs a newer eva-md (schema 3; this one reads 2). Nothing was read or written. Run eva-md upgrade."
	line, say := Notice(state, tooOld(storeID, 3))
	if !say || line != want {
		t.Fatalf("first Notice = %q, %v; want %q", line, say, want)
	}
	if strings.Contains(line, "/") || strings.Contains(strings.ToLower(line), "okf") || strings.Contains(line, state) || strings.Contains(line, storeID) {
		t.Fatalf("the line holds a path, a library's name or the store id: %q", line)
	}
	if _, say := Notice(state, tooOld(storeID, 3)); say {
		t.Fatal("Notice said it twice")
	}
	if line, say := Notice(state, tooOld(storeID, 4)); !say || !strings.Contains(line, "schema 4;") {
		t.Fatalf("Notice for a higher min_reader = %q, %v", line, say)
	}
	if _, say := Notice(state, tooOld("s01mj780q8r2t7", 3)); !say {
		t.Fatal("Notice kept quiet for another store")
	}
	if _, say := Notice(state, tooOld(storeID, 4)); say {
		t.Fatal("Notice said the higher min_reader twice")
	}
	for _, err := range []error{nil, ErrNoManifest, ErrTooOld, errors.New("other")} {
		if line, say := Notice(state, err); say || line != "" {
			t.Fatalf("Notice(%v) = %q, %v; want nothing", err, line, say)
		}
	}

	// A record it cannot read is no record: it says it, and records anew.
	writeT(t, filepath.Join(state, noticeFile), "not json")
	if _, say := Notice(state, tooOld(storeID, 3)); !say {
		t.Fatal("Notice kept quiet over an unreadable record")
	}
	if _, say := Notice(state, tooOld(storeID, 3)); say {
		t.Fatal("Notice did not record over an unreadable record")
	}
}

func TestNoticeReadOnlySaysEveryTime(t *testing.T) {
	state := tempDir(t)
	if err := os.Chmod(state, 0o500); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chmod(state, 0o700) })
	err := &TooOldError{StoreID: storeID, Schema: 3, MinReader: 3, Known: Known}
	for i := range 3 {
		if _, say := Notice(state, err); !say {
			t.Fatalf("call %d kept quiet with nowhere to record", i+1)
		}
	}
	if _, err := os.Stat(filepath.Join(state, noticeFile)); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("a record was written into a read-only folder: %v", err)
	}
}

func writeT(t *testing.T, file, s string) {
	t.Helper()
	mkdirT(t, filepath.Dir(file))
	if err := os.WriteFile(file, []byte(s), 0o600); err != nil {
		t.Fatal(err)
	}
}

func mkdirT(t *testing.T, dir string) {
	t.Helper()
	if err := os.MkdirAll(dir, 0o700); err != nil {
		t.Fatal(err)
	}
}
