package health

import (
	"context"
	"os"
	"path/filepath"
	"regexp"
	"testing"
	"time"

	"github.com/heyNag/eva-md/internal/inbox"
	"github.com/heyNag/eva-md/internal/searchindex"
)

// The search index row: nothing to index, no index yet, what an index
// covers, and a damaged folder, which fails the row. Reading it writes
// nothing.
func TestSearchIndexRow(t *testing.T) {
	d := isolated(t)
	row := func() Check {
		t.Helper()
		var r Report
		inspectSearchIndex(&r, d)
		return r.Checks[0]
	}
	if c := row(); c.Status != "UNKNOWN" || c.Detail != "nothing to index yet" || c.Fix != "" {
		t.Fatalf("an empty inbox: %+v", c)
	}
	for i := 0; i < 2; i++ {
		e := inbox.Entry{Host: "codex", Session: "row", Project: "general", Role: "user", Kind: "prompt", Text: "words to index", TS: time.Now().Format(time.RFC3339), Origin: "hook"}
		if err := (inbox.Store{Root: d.Inbox()}).Append(e); err != nil {
			t.Fatal(err)
		}
	}
	if c := row(); c.Status != "UNKNOWN" || c.Detail != "no index yet, so search scans the inbox" || c.Fix != "run eva-md index, or wait for the day's maintenance" {
		t.Fatalf("no index: %+v", c)
	}
	if _, err := os.Stat(d.SearchIndex()); !os.IsNotExist(err) {
		t.Fatalf("the row wrote the index: %v", err)
	}
	if _, err := searchindex.Catchup(context.Background(), d, "maintain"); err != nil {
		t.Fatal(err)
	}
	if c := row(); c.Status != "PASS" || !regexp.MustCompile(`^covers 0\.0 of 0\.0 MB of the inbox \(100\.0%\) in 1 project; 0\.0 MB in 1 segment; built \d{4}-\d\d-\d\d \d\d:\d\d by maintain$`).MatchString(c.Detail) {
		t.Fatalf("indexed: %+v", c)
	}
	segs, _ := filepath.Glob(filepath.Join(d.SearchIndex(), "general", "seg-*.idx"))
	if len(segs) != 1 || os.Remove(segs[0]) != nil {
		t.Fatalf("segments: %q", segs)
	}
	if c := row(); c.Status != "FAIL" || c.Detail != "the index for 1 project folder is damaged, so search scans its files: general" || c.Fix != "run eva-md index" {
		t.Fatalf("damaged: %+v", c)
	}
}

func TestMegabytes(t *testing.T) {
	for n, want := range map[int64]string{0: "0.0", 49_000: "0.0", 51_000: "0.1", 1_073_200_000: "1,073.2", 12_345_678_901: "12,345.7", 999_999_999: "1,000.0"} {
		if got := megabytes(n); got != want {
			t.Errorf("megabytes(%d) = %q, want %q", n, got, want)
		}
	}
}
