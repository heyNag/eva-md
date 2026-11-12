package migrate

import (
	"maps"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

// TestDryRunWritesNothing plans the step with a marker planted in a path
// and in contents: nothing in the store, its .git or the state folder
// changes, the counts equal the run's, and the marker is in no count line
// and in no commit.
func TestDryRunWritesNothing(t *testing.T) {
	isolate(t)
	const marker = "zqmarkerzq"
	files := maps.Clone(v1Files)
	files["projects/"+marker+"/now.md"] = "# Now\n\n" + marker + " is the codename.\n"
	files["general/now.md"] = "# Now\n\n" + marker + "\n"
	dir := v1Wiki(t, files)
	o := machine(t, writerA, "laptop", passA)
	if err := os.MkdirAll(o.StateDir, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(o.StateDir, "other.json"), []byte("{}"), 0o600); err != nil {
		t.Fatal(err)
	}
	store, state, locks := hashDir(t, dir), hashDir(t, o.StateDir), hashDir(t, o.LocksDir)

	counts, err := DryRun(bg(), dir, synthetic)
	if err != nil {
		t.Fatal(err)
	}
	if hashDir(t, dir) != store || hashDir(t, o.StateDir) != state || hashDir(t, o.LocksDir) != locks {
		t.Fatal("the dry run wrote to the store, its .git, the state folder or the locks")
	}
	want := Counts{Read: map[string]int{"legacy": 10}, Written: map[string]int{"legacy": 10, "index": 1, "readme": 1}, Removed: 8}
	if !reflect.DeepEqual(counts, want) {
		t.Fatalf("counts = %+v, want %+v", counts, want)
	}
	line := counts.String()
	if line != "read legacy=10; written index=1 legacy=10 readme=1; removed 8; unreadable 0; peoples_sections 0; protected 0; unaccounted 0; held_back 0" {
		t.Errorf("line = %q", line)
	}

	r, err := Run(bg(), dir, synthetic, o)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(r.Counts, counts) {
		t.Fatalf("the run's counts %+v differ from the dry run's %+v", r.Counts, counts)
	}
	for _, s := range []string{line, r.Counts.String(), gitT(t, dir, "cat-file", "commit", "HEAD"), gitT(t, dir, "for-each-ref")} {
		if strings.Contains(s, marker) {
			t.Errorf("the marker is in %q", s)
		}
	}
	// The marker did reach the tree, so its absence above means something.
	if !strings.Contains(gitT(t, dir, "ls-tree", "-r", "--name-only", "HEAD"), marker) {
		t.Fatal("the marker never reached the tree")
	}

	// At the step's To, a dry run counts nothing.
	if c, err := DryRun(bg(), dir, synthetic); err != nil || !reflect.DeepEqual(c, Counts{}) {
		t.Fatalf("DryRun at To = %+v, %v", c, err)
	}
}

func TestDryRunCountsWhatRunHoldsBack(t *testing.T) {
	isolate(t)
	files := maps.Clone(v1Files)
	files["general/now.md"] = "# Now\n\npassword = hunter2hunter2\n"
	dir := v1Wiki(t, files)
	counts, err := DryRun(bg(), dir, synthetic)
	if err != nil || counts.HeldBack != 1 {
		t.Fatalf("DryRun = %+v, %v; want one held back", counts, err)
	}
	r, _ := Run(bg(), dir, synthetic, machine(t, writerA, "laptop", passA))
	if !reflect.DeepEqual(r.Counts, counts) {
		t.Fatalf("the run's counts %+v differ from the dry run's %+v", r.Counts, counts)
	}
}
