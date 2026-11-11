// Package v1oracle_test holds the codec's reading of v1's pages, ReadV1,
// equal to v1's own: the page library v1 was built on, reached through
// internal/okfadapter, reads each page, and ReadV1 must read the same. It
// writes the goldens in ../testdata/v1 that the codec's own tests compare
// ReadV1 with, so the comparison still runs once the library is gone. It
// is a package of its own because the codec's package names no library.
package v1oracle_test

import (
	"bytes"
	"encoding/json"
	"flag"
	"fmt"
	"math/rand/v2"
	"os"
	"path/filepath"
	"reflect"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/heyNag/eva-md/internal/okfadapter"
	"github.com/heyNag/eva-md/internal/store/codec"
)

var (
	update = flag.Bool("update", false, "write the goldens in ../testdata/v1 from the library's reading")
	seed   = flag.Uint64("seed", 0, "the seed of the generated pages; 0 takes one from the clock")
)

func TestMain(m *testing.M) {
	okfadapter.LockTimeout = 10 * time.Minute
	os.Exit(m.Run())
}

const fixtures = "../testdata/v1"

// wiki is a new, empty wiki in a temporary home.
func wiki(t *testing.T) *okfadapter.Wiki {
	t.Helper()
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("GIT_CONFIG_GLOBAL", filepath.Join(home, "gitconfig"))
	t.Setenv("GIT_CONFIG_NOSYSTEM", "1")
	root := filepath.Join(home, "wiki")
	if err := okfadapter.Init(root); err != nil {
		t.Fatal(err)
	}
	w, err := okfadapter.Open(root, filepath.Join(home, "locks"))
	if err != nil {
		t.Fatal(err)
	}
	return w
}

// libraryRead saves raw as the page id and reads it as v1 read it.
func libraryRead(t *testing.T, w *okfadapter.Wiki, id string, raw []byte) *okfadapter.Page {
	t.Helper()
	if err := os.WriteFile(filepath.Join(w.Root(), id+".md"), raw, 0o600); err != nil {
		t.Fatal(err)
	}
	pg, err := w.Read(id)
	if err != nil {
		t.Fatal(err)
	}
	return pg
}

// fromLibrary is the library's page model as a V1Page.
func fromLibrary(c *okfadapter.Concept) codec.V1Page {
	p := codec.V1Page{Type: c.Type, Title: c.Title, Description: c.Description, Status: c.Status,
		StaleAfter: c.StaleAfter, Extra: c.Extra, Body: c.Body}
	if c.Generated != nil {
		p.Generated = &codec.V1Stamp{By: c.Generated.By, At: c.Generated.At}
	}
	for _, v := range c.Verified {
		p.Verified = append(p.Verified, codec.V1Stamp{By: v.By, At: v.At})
	}
	for _, s := range c.Sources {
		p.Sources = append(p.Sources, codec.V1Source{ID: s.ID, Resource: s.Resource, Title: s.Title,
			Author: s.Author, LastModified: s.LastModified, UsageCount: s.UsageCount, Extra: s.Extra})
	}
	return p
}

// golden is a page as its golden holds it: JSON, two spaces, no HTML
// escapes. The codec's v1_test.go writes it the same way.
func golden(p codec.V1Page) []byte {
	var b bytes.Buffer
	enc := json.NewEncoder(&b)
	enc.SetEscapeHTML(false)
	enc.SetIndent("", "  ")
	if err := enc.Encode(p); err != nil {
		panic(err)
	}
	return b.Bytes()
}

// agree reads raw both ways and reports how they differ, or "". A page v1
// reads as locked or verified must protect as that through the codec.
func agree(t *testing.T, w *okfadapter.Wiki, id string, raw []byte) (lib codec.V1Page, pg *okfadapter.Page, diff string) {
	t.Helper()
	pg = libraryRead(t, w, id, raw)
	lib = fromLibrary(pg.Concept)
	got, err := codec.ReadV1(raw)
	if err != nil {
		return lib, pg, fmt.Sprintf("ReadV1: %v", err)
	}
	prot := got.Doc.Protection()
	got.Doc = nil
	switch {
	case !reflect.DeepEqual(lib, got):
		var b strings.Builder
		lv, gv := reflect.ValueOf(lib), reflect.ValueOf(got)
		for i := range lv.NumField() {
			if l, g := lv.Field(i).Interface(), gv.Field(i).Interface(); !reflect.DeepEqual(l, g) {
				fmt.Fprintf(&b, "%s: v1 reads %#v, ReadV1 %#v\n", lv.Type().Field(i).Name, l, g)
			}
		}
		return lib, pg, b.String()
	case okfadapter.IsLocked(pg.Concept) && !prot.Locked:
		return lib, pg, "v1 reads it as locked; the codec does not"
	case okfadapter.IsVerified(pg) && !prot.Verified:
		return lib, pg, "v1 reads it as verified; the codec does not"
	}
	return lib, pg, ""
}

// Each fixture page, one of each type v1 writes, is in the form v1 writes
// it, reads through the library and through ReadV1 alike, and equals its
// golden.
func TestFixturesReadAsV1ReadThem(t *testing.T) {
	w := wiki(t)
	pages, err := filepath.Glob(filepath.Join(fixtures, "*.md"))
	if err != nil || len(pages) == 0 {
		t.Fatalf("no fixture pages: %v", err)
	}
	types := map[string]bool{}
	locked, verified := 0, 0
	for _, path := range pages {
		name := strings.TrimSuffix(filepath.Base(path), ".md")
		raw, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		lib, pg, diff := agree(t, w, name, raw)
		if diff != "" {
			t.Errorf("%s: %s", name, diff)
			continue
		}
		if got := okfadapter.Serialize(pg.Concept); got != string(raw) {
			t.Errorf("%s is not as v1 writes it; v1 writes\n%s", name, got)
		}
		types[lib.Type] = true
		if okfadapter.IsLocked(pg.Concept) {
			locked++
		}
		if okfadapter.IsVerified(pg) {
			verified++
		}
		want := filepath.Join(fixtures, name+".json")
		if *update {
			if err := os.WriteFile(want, golden(lib), 0o644); err != nil {
				t.Fatal(err)
			}
			continue
		}
		if b, err := os.ReadFile(want); err != nil || !bytes.Equal(b, golden(lib)) {
			t.Errorf("%s: the golden is not v1's reading (%v); run go test -run %s -update", name, err, t.Name())
		}
	}
	for _, typ := range []string{"thread", "rules", "now", "knowledge", "map", "system", "guide"} {
		if !types[typ] {
			t.Errorf("no fixture page of type %s", typ)
		}
	}
	if locked == 0 || verified == 0 {
		t.Errorf("the fixtures hold %d pages v1 reads as locked and %d as verified; each needs one", locked, verified)
	}
	thread, _ := os.ReadFile(filepath.Join(fixtures, "thread.md"))
	if want, _ := os.ReadFile("../../../okfadapter/testdata/thread.golden.md"); len(want) == 0 || !bytes.Equal(thread, want) {
		t.Error("thread.md is not a copy of internal/okfadapter/testdata/thread.golden.md")
	}
}

// Each input of the codec's table of differences is read otherwise by v1:
// ReadV1 refuses it, reads other fields, or protects where v1 did not.
func TestDifferencesAreDifferences(t *testing.T) {
	w := wiki(t)
	cases, err := filepath.Glob(filepath.Join(fixtures, "differences", "*.md"))
	if err != nil || len(cases) < 6 {
		t.Fatalf("%d differences: %v", len(cases), err)
	}
	for _, path := range cases {
		raw, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		name := strings.TrimSuffix(filepath.Base(path), ".md")
		_, pg, diff := agree(t, w, "d", raw)
		got, err := codec.ReadV1(raw)
		prot := got.Doc.Protection()
		switch {
		case diff != "":
		case err == nil && (prot.Locked && !okfadapter.IsLocked(pg.Concept) || prot.Verified && !okfadapter.IsVerified(pg)):
		default:
			t.Errorf("%s: v1 and the codec read it alike", name)
		}
	}
}

// 10,000 generated pages with the fields, keys and value types v1's writers
// set, saved as v1 saves them, read alike: every field equal, the body byte
// for byte, and no page v1 reads as locked or verified unprotected.
func TestGeneratedPagesReadAsV1ReadThem(t *testing.T) {
	s := *seed
	if s == 0 {
		s = uint64(time.Now().UnixNano())
	}
	t.Logf("seed %d (go test -run %s -seed %d repeats it)", s, t.Name(), s)
	r := rand.New(rand.NewPCG(s, s))
	w := wiki(t)
	locked, verified, failed := 0, 0, 0
	for i := range 10000 {
		raw := []byte(okfadapter.Serialize(page(r)))
		_, pg, diff := agree(t, w, "g", raw)
		if diff != "" {
			t.Errorf("page %d:\n%s\n%s", i, raw, diff)
			if failed++; failed == 5 {
				t.FailNow()
			}
		}
		if okfadapter.IsLocked(pg.Concept) {
			locked++
		}
		if okfadapter.IsVerified(pg) {
			verified++
		}
	}
	if locked < 100 || verified < 100 {
		t.Errorf("v1 reads %d generated pages as locked and %d as verified; the generator is to make more", locked, verified)
	}
}

// pieces are the parts generated text is made of: words, and the
// characters and words a YAML reader or v1's writer treats apart.
var pieces = []string{
	"tide", "harbour", "Station", "feed", "eva", "main", "2026-11-04", "2026-11-04T06:12:09Z", "12", "3.5", "-7", "0x1f",
	"true", "false", "null", "yes", "on", "~", ":", ": ", " #", "#", "# x", "\"", "'", "''", "[", "]", "{", "}", ",", "&a", "*a",
	"!", "|", ">", "%", "@", "`", "?", "-", "- ", "--", "\\", "\\n", "\t", " ", "  ", "é", "Gezeiten", "日本語", "·", "—", "\u00a0",
	"\u0085", "\u2028", "\x01", "\n", "\r", "<b>", "&amp;", "s:9c41aa07#14", "git.example.test:example-org/tide-tables", "a: b", "x # y",
}

// text is one to four pieces, sometimes with spaces before or after.
func text(r *rand.Rand) string {
	var b strings.Builder
	if r.IntN(8) == 0 {
		b.WriteString("  ")
	}
	for range 1 + r.IntN(4) {
		b.WriteString(pieces[r.IntN(len(pieces))])
	}
	if r.IntN(8) == 0 {
		b.WriteString(" ")
	}
	return b.String()
}

// value is a value of a type v1's writers set in Extra: mostly a string,
// else a bool, a list of strings, a number or null.
func value(r *rand.Rand) any {
	switch r.IntN(10) {
	case 0:
		return r.IntN(2) == 0
	case 1:
		var list []any
		for range r.IntN(4) {
			list = append(list, text(r))
		}
		return list
	case 2:
		return []any{float64(r.IntN(100)), 2.5, true, nil, text(r)}
	case 3:
		return nil
	}
	return text(r)
}

// stamp is a generated or verified record's who and when.
func stamp(r *rand.Rand) (by, at string) {
	by = []string{"claude-code/model-a", "codex/model-b", "eva-md/2026.11.5", "human:river"}[r.IntN(4)]
	at = time.Date(2026, 1+time.Month(r.IntN(12)), 1+r.IntN(28), r.IntN(24), r.IntN(60), r.IntN(60), 0, time.UTC).Format(time.RFC3339)
	if r.IntN(10) == 0 {
		by = text(r)
	}
	if r.IntN(10) == 0 {
		at = text(r)
	}
	return by, at
}

// page is a page with the fields the thread, rules, now, knowledge, map,
// system and guide writers set, and a generated line.
func page(r *rand.Rand) *okfadapter.Concept {
	c := &okfadapter.Concept{
		Type:        []string{"thread", "rules", "now", "knowledge", "map", "system", "guide"}[r.IntN(7)],
		Title:       text(r),
		Description: text(r),
		Status:      "stable",
		Extra:       map[string]any{},
	}
	if r.IntN(10) == 0 {
		c.Type, c.Status = text(r), text(r)
	}
	c.Generated = &okfadapter.Generated{}
	c.Generated.By, c.Generated.At = stamp(r)
	for range []int{0, 0, 0, 1, 1, 2, 3}[r.IntN(7)] {
		var v okfadapter.Verified
		v.By, v.At = stamp(r)
		c.Verified = append(c.Verified, v)
	}
	if r.IntN(3) == 0 {
		c.StaleAfter = []string{"2027-02-01", text(r)}[r.IntN(2)]
	}
	for range r.IntN(5) {
		s := okfadapter.Source{Resource: text(r)}
		if r.IntN(3) == 0 {
			s.ID, s.Title, s.Author, s.LastModified, s.UsageCount = text(r), text(r), text(r), "2026-11-07", r.IntN(5)
		}
		for range r.IntN(3) {
			if s.Extra == nil {
				s.Extra = map[string]string{}
			}
			s.Extra[[]string{"capture", "note #1", "a.b", "x-y", "True", "两"}[r.IntN(6)]] = text(r)
		}
		c.Sources = append(c.Sources, s)
	}
	keys := []string{"aliases", "as_of", "branch", "head", "kind", "locked", "model", "model_note", "parent", "project", "remote", "repos", "scope", "state", "writer"}
	for _, k := range keys {
		if r.IntN(2) == 0 {
			c.Extra[k] = value(r)
		}
	}
	if r.IntN(2) == 0 {
		c.Extra["locked"] = []any{true, false, "true", "TRUE", "yes", "On", "false", "no", text(r)}[r.IntN(9)]
	}
	var body []string
	for range r.IntN(12) {
		body = append(body, []string{"## Now", "## Notes", "```", "---", "  indented", "# Title", "", "- item " + text(r),
			"<!-- eva:now:start -->", "text: with a colon", "locked: true", text(r)}[r.IntN(12)])
	}
	c.Body = strings.Join(body, "\n")
	if slices.Contains(body, "") && r.IntN(2) == 0 {
		c.Body = strings.ReplaceAll(c.Body, "\n", "\r\n")
	}
	return c
}
