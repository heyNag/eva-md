package health

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/heyNag/eva-md/internal/inbox"
	"github.com/heyNag/eva-md/internal/searchindex"
	"github.com/heyNag/eva-md/internal/update"
)

// With the switch health on, an index that covers less than all of the
// inbox says search scans the rest; one that covers all, or the switch
// off, says nothing more.
func TestSearchIndexRowSaysSearchScansTheRest(t *testing.T) {
	d := isolated(t)
	appendEntry := func() {
		t.Helper()
		e := inbox.Entry{Host: update.HostCodex, Session: "row", Project: "general", Role: "user", Kind: "prompt", Text: "words to index", TS: time.Now().Format(time.RFC3339), Origin: "hook"}
		if err := (inbox.Store{Root: d.Inbox()}).Append(e); err != nil {
			t.Fatal(err)
		}
	}
	row := func(rest bool) Check {
		t.Helper()
		var r Report
		searchIndexRow(&r, d, rest)
		return r.Checks[0]
	}
	appendEntry()
	if _, err := searchindex.Catchup(context.Background(), d, "maintain"); err != nil {
		t.Fatal(err)
	}
	if c := row(true); c.Status != "PASS" || strings.Contains(c.Detail, "search scans the rest") {
		t.Fatalf("an index that covers all: %+v", c)
	}
	appendEntry()
	if c := row(true); c.Status != "PASS" || !strings.HasSuffix(c.Detail, "; search scans the rest") {
		t.Fatalf("an index that covers less than all: %+v", c)
	}
	if c := row(false); strings.Contains(c.Detail, "search scans the rest") {
		t.Fatalf("the switch off: %+v", c)
	}
}
