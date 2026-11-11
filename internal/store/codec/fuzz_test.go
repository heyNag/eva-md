package codec

import (
	"bytes"
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"testing"
)

// FuzzRoundTrip: any bytes write back as read, and nothing the codec does
// with them panics. An edit that succeeds reads back; one that fails
// changes nothing. Its seeds, the corpus and the fixture's documents, run
// in go test.
func FuzzRoundTrip(f *testing.F) {
	for _, b := range corpus(f) {
		f.Add(b)
	}
	filepath.WalkDir(fixture, func(p string, de fs.DirEntry, err error) error {
		if err == nil && !de.IsDir() && filepath.Ext(p) == ".md" {
			b, _ := os.ReadFile(p)
			f.Add(b)
		}
		return nil
	})
	for _, s := range []string{"", "---", "---\n---", "---\r\n---\r\n", "---\n? \n: x\n---\n", "---\n- x\n  - y\n---\n## a\n```\n## b"} {
		f.Add([]byte(s))
	}
	f.Fuzz(func(t *testing.T, b []byte) {
		d := Read(b)
		got, err := Write(d)
		if err != nil || !bytes.Equal(got, b) {
			t.Fatalf("does not write back its input: %v", err)
		}
		p := d.Protection()
		d.Value("title")
		d.Preamble()
		d.Section("Evidence")
		edits := []func(d *Doc) error{
			func(d *Doc) error { return d.Set("title", "Fuzzed: a title") },
			func(d *Doc) error { return d.SetBlock("sources", []map[string]string{{"id": "e1"}}) },
			func(d *Doc) error { return d.Delete("status") },
			func(d *Doc) error { return d.SetSection("Evidence", "[^e1]: x\n") },
			func(d *Doc) error { return d.RemoveSection("Detail") },
			func(d *Doc) error { return d.SetPreamble("A statement.\n\n") },
		}
		for i, edit := range edits {
			e := Read(b)
			err := edit(e)
			out, _ := Write(e)
			if err != nil {
				if !bytes.Equal(out, b) {
					t.Fatalf("edit %d refused with %v and changed the document", i, err)
				}
				if (p.Locked || p.Unreadable) != errors.Is(err, ErrProtected) {
					t.Fatalf("edit %d: %v on a document protected %+v", i, err, p)
				}
				continue
			}
			if p.Locked || p.Unreadable {
				t.Fatalf("edit %d of a protected document succeeded", i)
			}
			r := Read(out)
			if r.Unreadable() != "" || r.Protection() != p {
				t.Fatalf("edit %d: reads back unreadable %q or protected %+v, was %+v", i, r.Unreadable(), r.Protection(), p)
			}
			switch i {
			case 0:
				if v, err := r.Value("title"); err != nil || v != "Fuzzed: a title" {
					t.Fatalf("Set reads back %v %v", v, err)
				}
			case 3:
				if s, err := r.Section("Evidence"); err != nil || s != "[^e1]: x\n" {
					t.Fatalf("SetSection reads back %q %v", s, err)
				}
			case 5:
				if r.Preamble() != "A statement.\n\n" {
					t.Fatalf("SetPreamble reads back %q", r.Preamble())
				}
			}
		}
	})
}
