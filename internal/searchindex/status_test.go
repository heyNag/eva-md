package searchindex

import (
	"os"
	"path/filepath"
	"slices"
	"testing"
)

// Inspect reports the inbox bytes the index covers against the inbox's, its
// segments and when it was built, from the manifests and listings alone,
// and names a folder whose manifest names a segment that is gone.
func TestInspect(t *testing.T) {
	d := fixture(t)
	if st := Inspect(d); st.Indexed != 0 || st.Inbox != 0 || len(st.Damaged) != 0 {
		t.Fatalf("no inbox: %+v", st)
	}
	prompt(t, d, "inspect", 1, "inspected words")
	if st := Inspect(d); st.Indexed != 0 || st.Inbox == 0 || st.Covered != 0 {
		t.Fatalf("no index: %+v", st)
	}
	catchupOK(t, d)
	st := Inspect(d)
	if st.Indexed != 1 || st.Covered != st.Inbox || st.Segments != 1 || st.Size == 0 || st.By != "index" || st.Built.IsZero() {
		t.Fatalf("indexed: %+v", st)
	}
	prompt(t, d, "inspect", 2, "more words")
	if st := Inspect(d); st.Covered >= st.Inbox || st.Covered == 0 {
		t.Fatalf("a tail: %+v", st)
	}
	m := readManifest(t, d, "general")
	if err := os.Remove(filepath.Join(d.SearchIndex(), "general", m.Segments[0].File)); err != nil {
		t.Fatal(err)
	}
	if st := Inspect(d); !slices.Equal(st.Damaged, []string{"general"}) || st.Indexed != 0 || st.Covered != 0 {
		t.Fatalf("a segment gone: %+v", st)
	}
}
