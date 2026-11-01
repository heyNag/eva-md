package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/heyNag/eva-md/internal/identity"
	"github.com/heyNag/eva-md/internal/inbox"
	"github.com/heyNag/eva-md/internal/paths"
	"github.com/heyNag/eva-md/internal/searchindex"
)

// captureHere appends prompts to a session of the project the tests run in.
func captureHere(t *testing.T, d paths.Dirs, sid string, texts ...string) identity.Project {
	t.Helper()
	cwd, _ := os.Getwd()
	p, err := identity.Resolve(context.Background(), cwd)
	if err != nil {
		t.Fatal(err)
	}
	for i, text := range texts {
		e := inbox.Entry{V: 1, TS: time.Now().Add(-time.Minute).Format(time.RFC3339), Host: "codex", Session: sid, Turn: fmt.Sprint(i), Role: "user", Kind: "prompt", Text: text, Project: p.Key, Branch: p.Branch, Origin: "hook"}
		if err := (inbox.Store{Root: d.Inbox()}).Append(e); err != nil {
			t.Fatal(err)
		}
	}
	return p
}

// files lists what is under dir, as paths relative to it.
func files(t *testing.T, dir string) []string {
	t.Helper()
	var out []string
	filepath.WalkDir(dir, func(path string, e fs.DirEntry, err error) error {
		if err == nil && !e.IsDir() {
			rel, _ := filepath.Rel(dir, path)
			out = append(out, rel)
		}
		return nil
	})
	return out
}

// eva-md index indexes the inbox now and prints a line for each project
// folder it indexed and a total; run again it finds nothing to do; with
// --rebuild it builds every folder again; with --json it prints the
// index's status. It writes only the index and its lock. A catch-up
// already running makes it print "index: busy" and exit 0.
func TestIndexCommand(t *testing.T) {
	d := isolated(t)
	p := captureHere(t, d, "index-session", "Remember the persimmon protocol.", "And the second line.")
	before := files(t, d.State)
	code, out, errOut := evaMD(t, "index")
	folder, _ := paths.SafeName(p.Key)
	line := regexp.MustCompile(`^index: ` + regexp.QuoteMeta(folder) + `: 1 file \(0\.0 MB\) indexed, 0\.0 MB in all\nindex: 1 file \(0\.0 MB\) indexed in 1 project, 0\.0 MB in all\n$`)
	if code != 0 || !line.MatchString(out) || errOut != "" {
		t.Fatalf("index: %d %q %q", code, out, errOut)
	}
	for _, f := range files(t, d.State) {
		new := true
		for _, b := range before {
			new = new && b != f
		}
		if new && !strings.HasPrefix(f, filepath.Join("cache", "search")+"/") && f != filepath.Join("locks", "search-index.lock") {
			t.Fatalf("eva-md index wrote %s", f)
		}
	}
	if code, out, _ := evaMD(t, "index"); code != 0 || !regexp.MustCompile(`^index: up to date \(0\.0 MB\)\n$`).MatchString(out) {
		t.Fatalf("index again: %d %q", code, out)
	}
	if code, out, _ := evaMD(t, "index", "--rebuild"); code != 0 || !strings.Contains(out, ": 1 file (0.0 MB) indexed, built again, ") || !strings.HasSuffix(out, "\nindex: 1 file (0.0 MB) indexed in 1 project, 1 project folder built again, 0.0 MB in all\n") {
		t.Fatalf("index --rebuild: %d %q", code, out)
	}
	code, out, _ = evaMD(t, "index", "--json")
	var st searchindex.Status
	if code != 0 || json.Unmarshal([]byte(out), &st) != nil || st.Indexed != 1 || st.Covered != st.Inbox || st.Covered == 0 {
		t.Fatalf("index --json: %d %q", code, out)
	}
	unlock, busy, err := paths.TryLock(d.Locks(), "search-index.lock")
	if err != nil || busy {
		t.Fatal(busy, err)
	}
	if code, out, _ := evaMD(t, "index"); code != 0 || out != "index: busy\n" {
		t.Fatalf("busy: %d %q", code, out)
	}
	unlock()
}

// A catch-up that fails, here on an index folder that is a file, exits 1
// and says why.
func TestIndexCommandFails(t *testing.T) {
	d := isolated(t)
	p := captureHere(t, d, "index-fails", "Some words.")
	folder, _ := paths.SafeName(p.Key)
	if err := os.MkdirAll(d.SearchIndex(), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(d.SearchIndex(), folder), nil, 0o600); err != nil {
		t.Fatal(err)
	}
	if code, _, errOut := evaMD(t, "index"); code != 1 || !strings.HasPrefix(errOut, "eva-md: index: ") {
		t.Fatalf("a failed catch-up: %d %q", code, errOut)
	}
}

// Without a wiki, maintain runs only the index step; a search then reads
// through the index and prints refs eva-md show opens; the doctor and
// eva-md status report the index as not built, built and damaged.
func TestMaintainWithoutAWikiIndexesForSearch(t *testing.T) {
	d := isolated(t)
	captureHere(t, d, "maintain-index", "Remember the persimmon protocol.")
	row := func(command string) (string, string) {
		t.Helper()
		var out, errOut bytes.Buffer
		run(context.Background(), []string{command, "--json"}, strings.NewReader(""), &out, &errOut)
		var r struct {
			Checks []struct{ Name, Status, Detail, Fix string }
		}
		if err := json.Unmarshal(out.Bytes(), &r); err != nil {
			t.Fatalf("%s: %v %q", command, err, out.String())
		}
		for _, c := range r.Checks {
			if c.Name == "search index" {
				return c.Status, c.Detail + "; " + c.Fix
			}
		}
		t.Fatalf("%s has no search index row", command)
		return "", ""
	}
	for _, command := range []string{"doctor", "status"} {
		if status, detail := row(command); status != "UNKNOWN" || detail != "no index yet, so search scans the inbox; run eva-md index, or wait for the day's maintenance" {
			t.Fatalf("%s before: %s %q", command, status, detail)
		}
	}
	if _, err := os.Stat(d.SearchIndex()); !os.IsNotExist(err) {
		t.Fatalf("status or the doctor wrote the index: %v", err)
	}
	code, out, errOut := evaMD(t, "maintain")
	if code != 0 || !regexp.MustCompile(`^maintain: index: 1 file \(0\.0 MB\) indexed in 1 project, 0\.0 MB in all\n$`).MatchString(out) || errOut != "" {
		t.Fatalf("maintain without a wiki: %d %q %q", code, out, errOut)
	}
	for _, command := range []string{"doctor", "status"} {
		if status, detail := row(command); status != "PASS" || !regexp.MustCompile(`^covers 0\.0 of 0\.0 MB of the inbox \(100\.0%\) in 1 project; 0\.0 MB in 1 segment; built \d{4}-\d\d-\d\d \d\d:\d\d by maintain; $`).MatchString(detail) {
			t.Fatalf("%s after: %s %q", command, status, detail)
		}
	}
	cwd, _ := os.Getwd()
	code, out, _ = evaMD(t, "search", "persimmon", "--cwd", cwd)
	ref := regexp.MustCompile(`s:[^ ]+#\d+`).FindString(out)
	if code != 0 || ref == "" || !strings.Contains(out, " through the index of ") {
		t.Fatalf("search: %d %q", code, out)
	}
	if code, out, _ := evaMD(t, "show", ref); code != 0 || !strings.Contains(out, "persimmon protocol") {
		t.Fatalf("show %s: %d %q", ref, code, out)
	}
	// A segment gone: the doctor fails the row.
	segs, _ := filepath.Glob(filepath.Join(d.SearchIndex(), "*", "seg-*.idx"))
	for _, s := range segs {
		os.Remove(s)
	}
	if status, detail := row("doctor"); status != "FAIL" || !strings.HasPrefix(detail, "the index for 1 project folder is damaged, so search scans its files: ") || !strings.HasSuffix(detail, "; run eva-md index") {
		t.Fatalf("damaged: %s %q", status, detail)
	}
}

// eva-md search, unlike the MCP server's search, indexes for the next
// search what the index lacks in scope, once that is 4 MiB or more.
func TestSearchCommandTopsUpTheIndex(t *testing.T) {
	d := isolated(t)
	p := captureHere(t, d, "topup-session", "Remember the persimmon protocol.")
	filler := strings.Repeat("filler words that pad the inbox out to its size; ", 20)
	var backlog []byte
	for len(backlog) < searchindex.TopUpMin+1<<20 {
		e := inbox.Entry{V: 1, TS: time.Now().Add(-time.Hour).Format(time.RFC3339), Host: "codex", Session: "backlog", Role: "user", Kind: "prompt", Text: filler, Project: p.Key, Branch: p.Branch, Origin: "hook"}
		line, err := json.Marshal(e)
		if err != nil {
			t.Fatal(err)
		}
		backlog = append(append(backlog, line...), '\n')
	}
	if err := os.WriteFile((inbox.Store{Root: d.Inbox()}).Path(p.Key, "codex", "backlog"), backlog, 0o600); err != nil {
		t.Fatal(err)
	}
	cwd, _ := os.Getwd()
	code, out, _ := evaMD(t, "search", "persimmon", "--cwd", cwd)
	folder, _ := paths.SafeName(p.Key)
	if _, err := os.Stat(filepath.Join(d.SearchIndex(), folder, "manifest.json")); code != 0 || err != nil || !regexp.MustCompile(`; indexed 2 files \([\d.]+ MB\) for the next search`).MatchString(out) {
		t.Fatalf("search: %d %v %q", code, err, out)
	}
}

// The index lines say what a catch-up did, counts only.
func TestIndexSummary(t *testing.T) {
	folder := func(name string, r searchindex.Report) searchindex.FolderReport {
		return searchindex.FolderReport{Name: name, Report: r}
	}
	for _, tc := range []struct {
		r     searchindex.Report
		total bool
		want  string
	}{
		{searchindex.Report{Size: 12e6}, true, "up to date (12.0 MB)"},
		{searchindex.Report{Files: 2, Bytes: 3.1e6, Merged: 4, Size: 12e6, Projects: []searchindex.FolderReport{folder("a", searchindex.Report{Files: 2}), folder("b", searchindex.Report{})}}, true, "2 files (3.1 MB) indexed in 1 project, 4 segments merged, 12.0 MB in all"},
		{searchindex.Report{Files: 1, Bytes: 1e5, Rebuilt: true, Size: 2e6}, false, "1 file (0.1 MB) indexed, built again, 2.0 MB in all"},
		{searchindex.Report{Unreadable: 1, Size: 2e6, Projects: []searchindex.FolderReport{folder("a", searchindex.Report{Busy: true}), folder("b", searchindex.Report{Rebuilt: true})}}, true, "1 project folder built again, 1 file unreadable, 1 project folder busy, 2.0 MB in all"},
	} {
		if got := indexSummary(tc.r, tc.total); got != tc.want {
			t.Errorf("indexSummary(%+v) = %q, want %q", tc.r, got, tc.want)
		}
	}
}
