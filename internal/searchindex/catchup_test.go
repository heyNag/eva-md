package searchindex

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/heyNag/eva-md/internal/inbox"
	"github.com/heyNag/eva-md/internal/paths"
)

// A catch-up indexes every project folder, then finds nothing to do; a
// rebuild starts each folder again; an index folder whose inbox folder is
// gone is deleted; and a catch-up already running makes another come back
// busy.
func TestCatchupAndRebuild(t *testing.T) {
	d := fixture(t)
	prompt(t, d, "one", 1, "general words")
	other := inbox.Entry{Host: "claude-code", Session: "two", Project: "github.com__team__other", Role: "user", Kind: "prompt", Text: "other words", TS: base.Format(time.RFC3339), Origin: "hook"}
	if err := (inbox.Store{Root: d.Inbox()}).Append(other); err != nil {
		t.Fatal(err)
	}
	r := catchupOK(t, d)
	if r.Files != 2 || len(r.Projects) != 2 || r.Projects[0].Name != "general" || r.Projects[1].Name != "github.com__team__other" || r.Size == 0 {
		t.Fatalf("first catch-up: %+v", r)
	}
	if r := catchupOK(t, d); r.Files != 0 || r.Segments != 0 || r.Merged != 0 {
		t.Fatalf("second catch-up: %+v", r)
	}
	r, err := Rebuild(context.Background(), d, "index")
	if err != nil || r.Files != 2 || !r.Projects[0].Rebuilt || !r.Projects[1].Rebuilt {
		t.Fatalf("rebuild: %+v %v", r, err)
	}
	if err := os.RemoveAll(filepath.Join(d.Inbox(), "github.com__team__other")); err != nil {
		t.Fatal(err)
	}
	catchupOK(t, d)
	if folders := Folders(d); len(folders) != 1 || folders[0] != "general" {
		t.Fatalf("index folders: %q", folders)
	}
	unlock, busy, err := paths.TryLock(d.Locks(), "search-index.lock")
	if err != nil || busy {
		t.Fatal(busy, err)
	}
	defer unlock()
	if r := catchupOK(t, d); !r.Busy {
		t.Fatalf("a held catch-up lock: %+v", r)
	}
}

// A catch-up checks every segment whole and builds a damaged folder again;
// until then a search's Open finds the folder unusable.
func TestCatchupRebuildsADamagedFolder(t *testing.T) {
	d := fixture(t)
	prompt(t, d, "damage", 1, "sound words")
	catchupOK(t, d)
	m := readManifest(t, d, "general")
	file := filepath.Join(d.SearchIndex(), "general", m.Segments[0].File)
	data, err := os.ReadFile(file)
	if err != nil {
		t.Fatal(err)
	}
	// A byte in the middle: a section only a full check reads.
	data[len(data)/2] ^= 0xff
	if err := os.WriteFile(file, data, 0o600); err != nil {
		t.Fatal(err)
	}
	r := catchupOK(t, d)
	if !r.Projects[0].Rebuilt || r.Files != 1 {
		t.Fatalf("catch-up over a damaged segment: %+v", r)
	}
	if all, _ := lookup(t, d, noWindow(), "sound"); len(all) != 1 {
		t.Fatalf("after the rebuild: %v", all)
	}
	// A manifest of another version is a folder to build again too.
	manifest := filepath.Join(d.SearchIndex(), "general", manifestName)
	if err := os.WriteFile(manifest, []byte(`{"v":2,"files":{},"segments":[]}`), 0o600); err != nil {
		t.Fatal(err)
	}
	if ix, unusable := Open(d, []string{"general"}); len(unusable) != 1 || ix.Folder("general") != nil {
		t.Fatalf("another version opened: %q", unusable)
	}
	if r := catchupOK(t, d); !r.Projects[0].Rebuilt {
		t.Fatalf("catch-up over another version: %+v", r)
	}
	if m := readManifest(t, d, "general"); m.V != FormatVersion || len(m.Segments) != 1 {
		t.Fatalf("manifest after: %+v", m)
	}
}

// A manifest over 16 MiB is refused, and a temporary file a killed publish
// left beside the manifest changes nothing until it is pruned.
func TestManifestBoundsAndLeftovers(t *testing.T) {
	d := fixture(t)
	prompt(t, d, "bounds", 1, "bounded words")
	catchupOK(t, d)
	dir := filepath.Join(d.SearchIndex(), "general")
	leftover := filepath.Join(dir, paths.TempPrefix+"0123456789abcdef")
	if err := os.WriteFile(leftover, []byte(`{"v":1,"files":{},"segments":[]}`), 0o600); err != nil {
		t.Fatal(err)
	}
	if all, _ := lookup(t, d, noWindow(), "bounded"); len(all) != 1 {
		t.Fatalf("with a leftover: %v", all)
	}
	catchupOK(t, d)
	if _, err := os.Stat(leftover); err != nil {
		t.Fatalf("a fresh leftover was pruned: %v", err)
	}
	old := time.Now().Add(-PruneAge - time.Minute)
	if err := os.Chtimes(leftover, old, old); err != nil {
		t.Fatal(err)
	}
	catchupOK(t, d)
	if _, err := os.Stat(leftover); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("an old leftover was kept: %v", err)
	}
	big := `{"v":1,"by":"` + strings.Repeat("x", MaxManifest) + `","files":{},"segments":[]}`
	if err := os.WriteFile(filepath.Join(dir, manifestName), []byte(big), 0o600); err != nil {
		t.Fatal(err)
	}
	root, err := os.OpenRoot(dir)
	if err != nil {
		t.Fatal(err)
	}
	defer root.Close()
	if _, err := ReadManifest(root); !errors.Is(err, ErrDamaged) {
		t.Fatalf("a manifest over 16 MiB: %v", err)
	}
}

// Eight writers append to three session files at once, in 10 steps, and a
// refresh runs after each step, while no writer writes, so each refresh but
// the first indexes tails the writers interleaved: no refresh fails, and a
// catch-up afterwards indexes every entry exactly once, at the line eva-md
// show finds it.
func TestConcurrentAppendersAndIndexer(t *testing.T) {
	d := fixture(t)
	const steps = 10
	appends := 250
	if raceOn {
		appends = 100
	}
	for step := 0; step < steps; step++ {
		var wg sync.WaitGroup
		errs := make(chan error, 8)
		for w := 0; w < 8; w++ {
			wg.Add(1)
			go func() {
				defer wg.Done()
				for i := step * appends / steps; i < (step+1)*appends/steps; i++ {
					e := inbox.Entry{Host: "codex", Session: fmt.Sprintf("writer-%d", w%3), Project: "general", Role: "tool", Kind: "tool", Text: fmt.Sprintf("Bash: concurrent writer%d entry%d", w, i), TS: base.Format(time.RFC3339), Origin: "hook"}
					if err := (inbox.Store{Root: d.Inbox()}).Append(e); err != nil {
						errs <- err
						return
					}
				}
			}()
		}
		wg.Wait()
		close(errs)
		for err := range errs {
			t.Fatal(err)
		}
		if _, err := Refresh(context.Background(), d, "general", Options{}); err != nil {
			t.Fatal(err)
		}
	}
	catchupOK(t, d)
	all, _ := lookup(t, d, noWindow(), "concurrent")
	seen := map[string]bool{}
	for _, f := range all {
		if seen[f.text] {
			t.Fatalf("%q indexed twice", f.text)
		}
		seen[f.text] = true
	}
	if len(seen) != 8*appends {
		t.Fatalf("%d of %d entries indexed", len(seen), 8*appends)
	}
	// Each at its physical line.
	for w := 0; w < 3; w++ {
		file := sessionPath(d, fmt.Sprintf("writer-%d", w))
		rows, _, err := (inbox.Store{Root: d.Inbox()}).ReadFrom(file, 0)
		if err != nil {
			t.Fatal(err)
		}
		line := map[string]int{}
		for i, r := range rows {
			line[r.Text] = i + 1
		}
		for _, f := range all {
			if n, ok := line[f.text]; ok && n != f.line {
				t.Fatalf("%q indexed at line %d, read at %d", f.text, f.line, n)
			}
		}
	}
}

// 1,000 session files, about 50 MB: the first build covers the inbox, a
// query finds the planted line, and the index is under 35% of the inbox's
// size. The build's and the query's times are logged, not bounded: a slow
// or loaded machine takes many times what this one does. Under the race
// detector, which slows decoding many times over, it builds 200.
func TestSmokeThousandSessions(t *testing.T) {
	d := fixture(t)
	filler := strings.Repeat("lorem ipsum dolor sit amet, consectetur adipiscing elit; ", 40)
	sessions := 1000
	if raceOn {
		sessions = 200
	}
	var size int64
	for i := 0; i < sessions; i++ {
		var texts []string
		for n := 1; n <= 20; n++ {
			text := filler
			if i == sessions*777/1000 && n == 13 {
				text = filler[:1000] + " the quokka migration " + filler[:1000]
			}
			texts = append(texts, fmt.Sprintf("session %d line %d %s", i, n, text))
		}
		sid := fmt.Sprintf("smoke-%04d-session", i)
		bulk(t, d, sid, 0, texts...)
		size += fileSize(t, sessionPath(d, sid))
	}
	start := time.Now()
	catchupOK(t, d)
	build := time.Since(start)
	start = time.Now()
	all, _ := lookup(t, d, noWindow(), "quokka")
	query := time.Since(start)
	st := Inspect(d)
	t.Logf("indexed %d session files (%.1f MB) in %s; a query took %s; the index is %.1f MB (%.0f%% of the inbox)", sessions, float64(size)/1e6, build, query, float64(st.Size)/1e6, 100*float64(st.Size)/float64(size))
	if len(all) != 1 || all[0].line != 13 || st.Covered != size || float64(st.Size) > 0.35*float64(size) {
		t.Fatalf("quokka: %v, covered %d of %d, size %d", all, st.Covered, size, st.Size)
	}
}
