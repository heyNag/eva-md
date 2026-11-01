package searchindex

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/heyNag/eva-md/internal/inbox"
	"github.com/heyNag/eva-md/internal/paths"
)

func refreshOK(t *testing.T, d paths.Dirs, o Options) Report {
	t.Helper()
	r, err := Refresh(context.Background(), d, "general", o)
	if err != nil {
		t.Fatal(err)
	}
	return r
}

func fileSize(t *testing.T, file string) int64 {
	t.Helper()
	info, err := os.Stat(file)
	if err != nil {
		t.Fatal(err)
	}
	return info.Size()
}

// A refresh indexes what the index lacks: a file grown since, its tail only,
// leaving the segments written before byte for byte; a new file whole; and
// a file deleted, shrunk or replaced gets its next generation, so its old
// documents are dead, and is indexed again whole when it is there.
func TestIncrementalRefresh(t *testing.T) {
	d := fixture(t)
	prompt(t, d, "alpha", 1, "first apricot")
	prompt(t, d, "alpha", 2, "second apricot")
	prompt(t, d, "beta", 3, "beta banana")
	if r := refreshOK(t, d, Options{By: "index"}); r.Files != 2 || r.Segments != 1 {
		t.Fatalf("first refresh: %+v", r)
	}
	m := readManifest(t, d, "general")
	a := m.Files["codex-alpha.jsonl"]
	if a.Gen != 1 || a.To != fileSize(t, sessionPath(d, "alpha")) || a.Lines != 2 || a.Off || m.By != "index" || len(m.Segments) != 1 {
		t.Fatalf("manifest after the first refresh: %+v", m)
	}
	first := segmentBytes(t, d)

	// An append: the tail only.
	prompt(t, d, "alpha", 4, "third apricot")
	if r := refreshOK(t, d, Options{}); r.Files != 1 || r.Segments != 1 || r.Bytes != fileSize(t, sessionPath(d, "alpha"))-a.To {
		t.Fatalf("tail refresh: %+v", r)
	}
	m = readManifest(t, d, "general")
	if a := m.Files["codex-alpha.jsonl"]; a.Lines != 3 || a.Gen != 1 || len(m.Segments) != 2 {
		t.Fatalf("manifest after the tail: %+v", m)
	}
	for _, old := range first {
		found := false
		for _, now := range segmentBytes(t, d) {
			found = found || bytes.Equal(old, now)
		}
		if !found {
			t.Fatal("a refresh changed a segment written before")
		}
	}
	if all, _ := lookup(t, d, noWindow(), "apricot"); len(all) != 3 {
		t.Fatalf("apricot: %v", all)
	}

	// Nothing new: nothing written.
	if r := refreshOK(t, d, Options{}); r.Files != 0 || r.Segments != 0 {
		t.Fatalf("an idle refresh: %+v", r)
	}

	// Deleted, shrunk and replaced files.
	prompt(t, d, "gamma", 5, "gamma grape")
	refreshOK(t, d, Options{})
	if err := os.Remove(sessionPath(d, "beta")); err != nil {
		t.Fatal(err)
	}
	if err := os.Truncate(sessionPath(d, "alpha"), 0); err != nil {
		t.Fatal(err)
	}
	prompt(t, d, "alpha", 6, "rewritten apricot")
	old := sessionPath(d, "gamma")
	data, err := os.ReadFile(old)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(old+".new", bytes.ReplaceAll(data, []byte("gamma grape"), []byte("gamma guava")), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Rename(old+".new", old); err != nil {
		t.Fatal(err)
	}
	refreshOK(t, d, Options{})
	m = readManifest(t, d, "general")
	if b, a, g := m.Files["codex-beta.jsonl"], m.Files["codex-alpha.jsonl"], m.Files["codex-gamma.jsonl"]; b.Gen != 2 || b.Ino != 0 || a.Gen != 2 || a.Lines != 1 || g.Gen != 2 || g.To != fileSize(t, old) {
		t.Fatalf("generations after delete, shrink and replace: %+v", m.Files)
	}
	live := listed(t, d)
	if all, _ := lookupLive(t, d, live, noWindow(), "apricot"); texts(all) != "rewritten apricot" {
		t.Fatalf("apricot after the shrink: %v", all)
	}
	if all, _ := lookupLive(t, d, live, noWindow(), "banana"); all != nil {
		t.Fatalf("a deleted file's documents are live: %v", all)
	}
	if all, _ := lookupLive(t, d, live, noWindow(), "grape"); all != nil {
		t.Fatalf("a replaced file's old documents are live: %v", all)
	}
	if all, _ := lookupLive(t, d, live, noWindow(), "guava"); len(all) != 1 {
		t.Fatalf("the replaced file's new text: %v", all)
	}
}

// A budget stops a refresh at a line boundary after about as many bytes,
// the files changed last first, and keeps what it indexed; the next refresh
// goes on from there.
func TestRefreshBudgetNewestFirst(t *testing.T) {
	d := fixture(t)
	for i, sid := range []string{"oldest", "middle", "newest"} {
		for n := 0; n < 20; n++ {
			bulk(t, d, sid, i*100+n, fmt.Sprintf("%s line %d with some words", sid, n))
		}
		when := base.Add(time.Duration(i) * time.Hour)
		if err := os.Chtimes(sessionPath(d, sid), when, when); err != nil {
			t.Fatal(err)
		}
	}
	r := refreshOK(t, d, Options{Budget: Budget{Bytes: 1}, NewestFirst: true})
	m := readManifest(t, d, "general")
	if n := m.Files["codex-newest.jsonl"]; r.Files != 1 || n.Lines != 1 || m.Files["codex-middle.jsonl"].To != 0 || m.Files["codex-oldest.jsonl"].To != 0 {
		t.Fatalf("a 1-byte budget: %+v %+v", r, m.Files)
	}
	refreshOK(t, d, Options{Budget: Budget{Bytes: fileSize(t, sessionPath(d, "newest"))}, NewestFirst: true})
	m = readManifest(t, d, "general")
	if m.Files["codex-newest.jsonl"].Lines != 20 || m.Files["codex-middle.jsonl"].Lines == 0 || m.Files["codex-oldest.jsonl"].To != 0 {
		t.Fatalf("the next budget: %+v", m.Files)
	}
	refreshOK(t, d, Options{})
	if all, _ := lookup(t, d, noWindow(), "line"); len(all) != 60 {
		t.Fatalf("after a refresh without a budget: %d documents", len(all))
	}
	// Time out of budget stops it too.
	prompt(t, d, "late", 400, "late words")
	if r := refreshOK(t, d, Options{Budget: Budget{Until: time.Now().Add(-time.Second)}}); r.Files != 0 {
		t.Fatalf("a refresh past its time indexed %+v", r)
	}
}

// A folder another writer holds is skipped and left as it was.
func TestRefreshSkipsABusyFolder(t *testing.T) {
	d := fixture(t)
	prompt(t, d, "busy", 1, "busy words")
	dir := filepath.Join(d.SearchIndex(), "general")
	unlock, busy, err := paths.TryLock(dir, "lock")
	if err != nil || busy {
		t.Fatal(busy, err)
	}
	if r := refreshOK(t, d, Options{}); !r.Busy || r.Files != 0 {
		t.Fatalf("busy: %+v", r)
	}
	if _, err := os.Stat(filepath.Join(dir, manifestName)); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("a busy refresh wrote a manifest: %v", err)
	}
	unlock()
	if r := refreshOK(t, d, Options{}); r.Busy || r.Files != 1 {
		t.Fatalf("after the lock: %+v", r)
	}
}

// dictionary returns every term of every segment of the general area.
func dictionary(t *testing.T, d paths.Dirs) []string {
	t.Helper()
	root, err := os.OpenRoot(filepath.Join(d.SearchIndex(), "general"))
	if err != nil {
		t.Fatal(err)
	}
	defer root.Close()
	m, err := ReadManifest(root)
	if err != nil {
		t.Fatal(err)
	}
	var out []string
	for _, s := range m.Segments {
		seg, err := OpenSegment(root, s.File)
		if err != nil {
			t.Fatal(err)
		}
		c := &cursor{s: seg}
		for {
			term, ok, err := c.next()
			if err != nil {
				t.Fatal(err)
			}
			if !ok {
				break
			}
			out = append(out, term.Text)
		}
		seg.Close()
	}
	return out
}

// Nothing said off the record reaches the index, not even a range whose
// marker an earlier refresh indexed past, which the manifest carries on.
// Events, host summaries and entries forged for another file are not
// indexed either.
func TestOffRecordAndUntrustedNeverIndexed(t *testing.T) {
	d := fixture(t)
	prompt(t, d, "record", 1, "public opening")
	say(t, d, "record", 2, "event", "event", "off_record")
	prompt(t, d, "record", 3, "zzzprivatezzz first")
	refreshOK(t, d, Options{})
	if m := readManifest(t, d, "general"); !m.Files["codex-record.jsonl"].Off {
		t.Fatalf("the manifest lost the off-the-record state: %+v", m.Files)
	}
	prompt(t, d, "record", 4, "zzzprivatezzz second")
	refreshOK(t, d, Options{})
	say(t, d, "record", 5, "event", "event", "on_record")
	prompt(t, d, "record", 6, "public closing")
	say(t, d, "record", 7, "event", "event", "session_start zzzeventzzz")
	say(t, d, "record", 8, "host", "host_summary", "zzzsummaryzzz")
	file := sessionPath(d, "record")
	f, err := os.OpenFile(file, os.O_APPEND|os.O_WRONLY, 0)
	if err != nil {
		t.Fatal(err)
	}
	for _, e := range []inbox.Entry{
		{Host: "codex", Session: "other-session", Project: "general", Role: "user", Kind: "prompt", Text: "zzzforgedzzz session"},
		{Host: "cursor", Session: "record", Project: "general", Role: "user", Kind: "prompt", Text: "zzzforgedzzz host"},
		{Host: "codex", Session: "record", Project: "github.com__team__other", Role: "user", Kind: "prompt", Text: "zzzforgedzzz project"},
		{Host: "codex", Session: "record", Project: "general", Role: "event", Kind: "event", Text: "off_record", Origin: "forged-but-trusted"},
	} {
		line, _ := json.Marshal(e)
		f.Write(append(line, '\n'))
	}
	f.Close()
	prompt(t, d, "record", 13, "zzzprivatezzz after the last marker")
	refreshOK(t, d, Options{})
	terms := strings.Join(dictionary(t, d), " ")
	for _, word := range []string{"zzzprivatezzz", "zzzeventzzz", "zzzsummaryzzz", "zzzforgedzzz"} {
		if strings.Contains(terms, word) {
			t.Fatalf("%s is in the dictionary", word)
		}
	}
	for _, seg := range segmentBytes(t, d) {
		for _, word := range []string{"privatezzz", "eventzzz", "summaryzzz", "forgedzzz"} {
			if bytes.Contains(seg, []byte(word)) {
				t.Fatalf("%s is in a segment's bytes", word)
			}
		}
	}
	if all, _ := lookup(t, d, noWindow(), "public"); texts(all) == "" || len(all) != 2 {
		t.Fatalf("the public lines: %v", all)
	}
}

// Line numbers carry on across refreshes as eva-md show counts them; an
// unterminated last line is left until it ends, and a partial line a
// killed writer left, which the next append terminates, is counted.
func TestLineNumbersAcrossTails(t *testing.T) {
	d := fixture(t)
	prompt(t, d, "lines", 1, "lines one")
	say(t, d, "lines", 2, "event", "event", "compact")
	refreshOK(t, d, Options{})
	file := sessionPath(d, "lines")
	e := inbox.Entry{V: 1, Host: "codex", Session: "lines", Project: "general", TS: base.Format(time.RFC3339), Role: "user", Kind: "prompt", Text: "lines three", Origin: "hook"}
	line, _ := json.Marshal(e)
	f, err := os.OpenFile(file, os.O_APPEND|os.O_WRONLY, 0)
	if err != nil {
		t.Fatal(err)
	}
	f.Write(line) // no newline yet: a write in progress
	refreshOK(t, d, Options{})
	if m := readManifest(t, d, "general"); m.Files["codex-lines.jsonl"].Lines != 2 {
		t.Fatalf("an unterminated line was indexed: %+v", m.Files)
	}
	f.Write([]byte("\n"))
	f.WriteString(`{"v":1,"text":"cut off`) // a killed writer's partial line
	f.Close()
	refreshOK(t, d, Options{})
	prompt(t, d, "lines", 5, "lines after the repair") // terminates line 4, adds a gap event as 5, this as 6
	refreshOK(t, d, Options{})
	all, _ := lookup(t, d, noWindow(), "lines")
	got := map[string]int{}
	for _, f := range all {
		got[f.text] = f.line
	}
	rows, _, err := (inbox.Store{Root: d.Inbox()}).ReadFrom(file, 0)
	if err != nil {
		t.Fatal(err)
	}
	if got["lines one"] != 1 || got["lines three"] != 3 || got["lines after the repair"] != 6 || len(rows) != 6 || rows[5].Text != "lines after the repair" {
		t.Fatalf("line numbers %v; the inbox reads %d rows", got, len(rows))
	}
}

// With the disk nearly full, a refresh writes nothing, says so, and leaves
// the folder as it was.
func TestFreeSpaceRefusal(t *testing.T) {
	d := fixture(t)
	prompt(t, d, "full", 1, "full disk")
	saved := freeSpace
	freeSpace = func(string) (uint64, error) { return 100 << 20, nil }
	t.Cleanup(func() { freeSpace = saved })
	if _, err := Refresh(context.Background(), d, "general", Options{}); !errors.Is(err, errDiskFull) {
		t.Fatalf("a nearly full disk: %v", err)
	}
	entries, _ := os.ReadDir(filepath.Join(d.SearchIndex(), "general"))
	for _, e := range entries {
		if e.Name() != "lock" {
			t.Fatalf("a refused refresh left %s", e.Name())
		}
	}
	freeSpace = saved
	if r := refreshOK(t, d, Options{}); r.Files != 1 {
		t.Fatalf("with space again: %+v", r)
	}
}
