package searchindex

import (
	"context"
	"encoding/json"
	"math"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"
	"time"

	"github.com/heyNag/eva-md/internal/inbox"
	"github.com/heyNag/eva-md/internal/paths"
)

// fixture is an isolated home with eva-md's folders under it.
func fixture(t *testing.T) paths.Dirs {
	t.Helper()
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("GIT_CONFIG_GLOBAL", filepath.Join(home, "gitconfig"))
	t.Setenv("GIT_CONFIG_NOSYSTEM", "1")
	return paths.Dirs{State: filepath.Join(home, "state"), Share: filepath.Join(home, "share"), Config: filepath.Join(home, "config")}
}

// base is the time the fixtures' entries are dated from.
var base = time.Date(2026, 10, 1, 9, 0, 0, 0, time.UTC)

// say appends an entry of the general area's session sid, dated base plus
// minutes.
func say(t *testing.T, d paths.Dirs, sid string, minutes int, role, kind, text string) {
	t.Helper()
	e := inbox.Entry{Host: "codex", Session: sid, Project: "general", Branch: "main", TS: base.Add(time.Duration(minutes) * time.Minute).Format(time.RFC3339), Role: role, Kind: kind, Text: text, Origin: "hook"}
	if err := (inbox.Store{Root: d.Inbox()}).Append(e); err != nil {
		t.Fatal(err)
	}
}

// prompt appends a user prompt.
func prompt(t *testing.T, d paths.Dirs, sid string, minutes int, text string) {
	t.Helper()
	say(t, d, sid, minutes, "user", "prompt", text)
}

func sessionPath(d paths.Dirs, sid string) string {
	return (inbox.Store{Root: d.Inbox()}).Path("general", "codex", sid)
}

func catchupOK(t *testing.T, d paths.Dirs) Report {
	t.Helper()
	r, err := Catchup(context.Background(), d, "index")
	if err != nil {
		t.Fatal(err)
	}
	return r
}

// found is a candidate as a test reads it: the text of its line and its
// line number.
type found struct {
	text string
	line int
}

// lookup opens the general area's index, scores terms and returns the
// candidates holding every term, best first, with their lines read back
// from the inbox, and those holding some. Every document counts as live.
func lookup(t *testing.T, d paths.Dirs, since int64, terms ...string) (all, some []found) {
	t.Helper()
	return lookupLive(t, d, func(string, string, uint32) bool { return true }, since, terms...)
}

// listed is the Live a search gives from its listing: a file that is
// there, with the inode and the generation the manifest records.
func listed(t *testing.T, d paths.Dirs) Live {
	t.Helper()
	m := readManifest(t, d, "general")
	files, err := listFolder(filepath.Join(d.Inbox(), "general"))
	if err != nil {
		t.Fatal(err)
	}
	ino := map[string]uint64{}
	for _, f := range files {
		ino[f.Name] = f.Ino
	}
	return func(folder, file string, gen uint32) bool {
		st, ok := m.Files[file]
		return ok && st.Gen == gen && st.Ino != 0 && ino[file] == st.Ino
	}
}

// lookupLive is lookup with the documents live accepts.
func lookupLive(t *testing.T, d paths.Dirs, live Live, since int64, terms ...string) (all, some []found) {
	t.Helper()
	q, ok := NewQuery(terms)
	if !ok {
		t.Fatalf("%q is left to the scan", terms)
	}
	ix, unusable := Open(d, []string{"general"})
	defer ix.Close()
	if len(unusable) > 0 {
		t.Fatalf("unusable: %v", unusable)
	}
	st, bad := ix.Stats(q)
	if len(bad) > 0 {
		t.Fatalf("damaged: %v", bad)
	}
	a, s, _, _, bad := ix.Candidates(q, st, live, since, 1<<20)
	if len(bad) > 0 {
		t.Fatalf("damaged: %v", bad)
	}
	read := func(cs []Candidate) []found {
		var out []found
		for i := range cs {
			if err := ix.Locate(&cs[i]); err != nil {
				t.Fatal(err)
			}
			data, err := os.ReadFile(filepath.Join(d.Inbox(), cs[i].Folder, cs[i].File))
			if err != nil {
				t.Fatal(err)
			}
			line := data[cs[i].Loc.Offset : cs[i].Loc.Offset+int64(cs[i].Loc.Length)]
			e := entryText(t, line)
			out = append(out, found{e, cs[i].Loc.Line})
		}
		return out
	}
	return read(a), read(s)
}

func noWindow() int64 { return math.MinInt64 }

func texts(fs []found) string {
	var out []string
	for _, f := range fs {
		out = append(out, f.text)
	}
	return strings.Join(out, "|")
}

func sortedTexts(fs []found) string {
	var out []string
	for _, f := range fs {
		out = append(out, f.text)
	}
	sort.Strings(out)
	return strings.Join(out, "|")
}

// segmentBytes returns the bytes of every segment in the general area's
// index folder.
func segmentBytes(t *testing.T, d paths.Dirs) [][]byte {
	t.Helper()
	entries, err := os.ReadDir(filepath.Join(d.SearchIndex(), "general"))
	if err != nil {
		t.Fatal(err)
	}
	var out [][]byte
	for _, e := range entries {
		if strings.HasSuffix(e.Name(), ".idx") {
			data, err := os.ReadFile(filepath.Join(d.SearchIndex(), "general", e.Name()))
			if err != nil {
				t.Fatal(err)
			}
			out = append(out, data)
		}
	}
	return out
}

func readManifest(t *testing.T, d paths.Dirs, folder string) Manifest {
	t.Helper()
	root, err := os.OpenRoot(filepath.Join(d.SearchIndex(), folder))
	if err != nil {
		t.Fatal(err)
	}
	defer root.Close()
	m, err := ReadManifest(root)
	if err != nil {
		t.Fatal(err)
	}
	return m
}

func entryText(t *testing.T, line []byte) string {
	t.Helper()
	var e inbox.Entry
	if err := json.Unmarshal(line, &e); err != nil {
		t.Fatalf("the located line is not an entry: %v", err)
	}
	return e.Text
}

func openAppend(file string) (*os.File, error) {
	return os.OpenFile(file, os.O_APPEND|os.O_WRONLY, 0)
}

// readAt reads a located candidate's line from its file.
func readAt(t *testing.T, d paths.Dirs, c Candidate) []byte {
	t.Helper()
	data, err := os.ReadFile(filepath.Join(d.Inbox(), c.Folder, c.File))
	if err != nil {
		t.Fatal(err)
	}
	return data[c.Loc.Offset : c.Loc.Offset+int64(c.Loc.Length)]
}

// bulk appends prompts to a session file of the general area in one write,
// as appends would leave it but without an fsync each, dated base plus
// start minutes, then a minute apart.
func bulk(t *testing.T, d paths.Dirs, sid string, start int, texts ...string) {
	t.Helper()
	var b []byte
	for i, text := range texts {
		e := inbox.Entry{V: 1, Host: "codex", Session: sid, Project: "general", Branch: "main", TS: base.Add(time.Duration(start+i) * time.Minute).Format(time.RFC3339), Role: "user", Kind: "prompt", Text: text, Origin: "hook"}
		line, err := json.Marshal(e)
		if err != nil {
			t.Fatal(err)
		}
		b = append(append(b, line...), '\n')
	}
	file := sessionPath(d, sid)
	if err := os.MkdirAll(filepath.Dir(file), 0o700); err != nil {
		t.Fatal(err)
	}
	f, err := os.OpenFile(file, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o600)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	if _, err := f.Write(b); err != nil {
		t.Fatal(err)
	}
}
