package update

import (
	"context"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"
)

// The chats an inbox holds from before updates are set aside once, whether
// install or, for a binary replaced without one, the first job does it: no
// call is made for them, the chat goes on from where it was, and a second
// baseline changes nothing.
func TestBaselineSetsAsideChatsFromBeforeUpdates(t *testing.T) {
	for _, by := range []string{"install", "the first job"} {
		t.Run(by, func(t *testing.T) {
			e := newEnv(t)
			if err := os.Remove(filepath.Join(e.dirs.Inbox(), baselineName)); err != nil {
				t.Fatal(err)
			}
			old := newChat("3f2a91c0-base", start)
			talk(old, 3, "walnut")
			file := e.write(old)
			// A last line still being written is left for the update.
			partial := []byte(`{"v":1,"role":"user"`)
			f, err := os.OpenFile(file, os.O_WRONLY|os.O_APPEND, 0)
			if err != nil {
				t.Fatal(err)
			}
			whole := size(t, file)
			if _, err := f.Write(partial); err != nil {
				t.Fatal(err)
			}
			f.Close()
			if by == "install" {
				if err := Baseline(e.dirs, e.clock); err != nil {
					t.Fatal(err)
				}
				if m := e.mark(file); m.Offset != whole || m.More || m.Wait != 0 {
					t.Fatalf("mark %+v, want the end of the last whole line, %d", m, whole)
				}
			}
			if err := os.WriteFile(file, old.jsonl(t), 0o600); err != nil {
				t.Fatal(err)
			}
			if by == "the first job" {
				// Before it, a dry run says so, and sets nothing aside.
				var out strings.Builder
				if err := e.job().DryRun(context.Background(), old.host, old.session, &out); err != nil || !strings.Contains(out.String(), "would set aside") {
					t.Fatalf("dry run: %v\n%s", err, out.String())
				}
				if m := e.mark(file); m.Offset != 0 {
					t.Fatalf("the dry run moved the watermark: %+v", m)
				}
			}
			if s := e.run(old); len(e.fake.Requests()) != 0 {
				t.Fatalf("a chat from before updates was sent (%q)", outcomes(s))
			}
			if err := Baseline(e.dirs, e.clock); err != nil {
				t.Fatal(err)
			}
			talk(old, 2, "almond")
			e.write(old)
			s := e.run(old)
			reqs := e.fake.Requests()
			if len(reqs) != 1 || strings.Contains(string(reqs[0].Input), "walnut") || !strings.Contains(string(reqs[0].Input), "almond") {
				t.Fatalf("%d calls (%q); the call should hold the new exchanges only", len(reqs), outcomes(s))
			}
		})
	}
}

// Install's baseline leaves the work to a job that holds the update lock,
// which sets aside the same chats itself.
func TestBaselineWaitsForNoJob(t *testing.T) {
	e := newEnv(t)
	record := filepath.Join(e.dirs.Inbox(), baselineName)
	if err := os.Remove(record); err != nil {
		t.Fatal(err)
	}
	unlock, busy, err := lockUpdates(e.dirs.Locks())
	if err != nil || busy {
		t.Fatalf("lock: busy %v, %v", busy, err)
	}
	if err := Baseline(e.dirs, e.clock); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Lstat(record); !os.IsNotExist(err) {
		t.Fatalf("the baseline ran beside a job: %v", err)
	}
	unlock()
	if err := Baseline(e.dirs, e.clock); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Lstat(record); err != nil {
		t.Fatalf("no record after the job: %v", err)
	}
}

// A folder whose watermarks can't be written doesn't stop the updates: its
// chats are listed in the record and recorded as failed, never worked
// through; the other chats are set aside, a chat begun later is processed,
// not set aside; and once the folder can be written the next job sets the
// listed chats aside too, with no model call for them.
func TestBaselineLeavesAFolderItCantWrite(t *testing.T) {
	e := newEnv(t)
	record := filepath.Join(e.dirs.Inbox(), baselineName)
	if err := os.Remove(record); err != nil {
		t.Fatal(err)
	}
	stuck := newChat("3f2a91c0-stuck", start)
	talk(stuck, 2, "walnut")
	for i := range stuck.rows {
		stuck.rows[i].Project = "github.com__you__other"
	}
	stuckFile := e.write(stuck)
	state := filepath.Join(filepath.Dir(stuckFile), ".state")
	if err := os.MkdirAll(state, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(state, 0o500); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chmod(state, 0o700) })
	old := newChat("3f2a91c0-old", start)
	talk(old, 2, "almond")
	oldFile := e.write(old)
	s, err := e.job().Pending(context.Background())
	if err != nil || len(e.fake.Requests()) != 0 {
		t.Fatalf("first job: %v, %d calls (%q)", err, len(e.fake.Requests()), outcomes(s))
	}
	if m := e.mark(oldFile); m.Offset != size(t, oldFile) {
		t.Fatalf("the other chat wasn't set aside: %+v", m)
	}
	if data, err := os.ReadFile(record); err != nil || !strings.Contains(string(data), "\nleft ") || !strings.Contains(string(data), "github.com__you__other/") {
		t.Fatalf("the record doesn't list the chat left: %q %v", data, err)
	}
	if !slices.ContainsFunc(s.Records, func(r Record) bool { return r.Session == stuck.session && r.Reason == ReasonLeft }) {
		t.Fatalf("the chat left isn't recorded as failed: %+v", s.Records)
	}
	// Named on its own, it says why it waits, and so does a dry run.
	if s, err := e.job().Session(context.Background(), stuck.host, stuck.session); err != nil || s.Errors != 1 || len(e.fake.Requests()) != 0 {
		t.Fatalf("named: %v, %d errors, %d calls", err, s.Errors, len(e.fake.Requests()))
	}
	var out strings.Builder
	if err := e.job().DryRun(context.Background(), stuck.host, stuck.session, &out); err != nil || !strings.Contains(out.String(), "waits to be set aside: the next update tries to set it aside again") {
		t.Fatalf("dry run: %v\n%s", err, out.String())
	}
	// A chat begun now is processed, not set aside.
	fresh := newChat("3f2a91c0-fresh", start.Add(time.Hour))
	talk(fresh, 2, "pecan")
	e.write(fresh)
	if s := e.run(fresh); len(e.fake.Requests()) != 1 {
		t.Fatalf("a chat begun after the first job wasn't processed (%q)", outcomes(s))
	}
	if err := os.Chmod(state, 0o700); err != nil {
		t.Fatal(err)
	}
	if s := e.run(stuck); len(e.fake.Requests()) != 1 {
		t.Fatalf("the chat left was sent once its folder could be written (%q)", outcomes(s))
	}
	if m := e.mark(stuckFile); m.Offset != size(t, stuckFile) {
		t.Fatalf("the chat left wasn't set aside: %+v", m)
	}
	if data, err := os.ReadFile(record); err != nil || strings.Contains(string(data), "left ") {
		t.Fatalf("the record still lists a chat left: %q %v", data, err)
	}
	// Its newest record says it no longer waits, so the doctor stops naming it.
	l, err := readRunLog(e.dirs.State, e.clock)
	if err != nil {
		t.Fatal(err)
	}
	if h := Host(l, HostClaude, e.clock); len(h.Left) != 0 {
		t.Fatalf("still named as left: %v", h.Left)
	}
}

// A watermark that can't be read, or a project folder that can't be read, at
// the first try is left too, never drained: once it can be read, its chats
// from before updates are set aside, with no model call, while a chat begun
// in the folder since is processed.
func TestBaselineLeavesWhatItCantRead(t *testing.T) {
	for _, what := range []string{"watermarks", "folder"} {
		t.Run(what, func(t *testing.T) {
			e := newEnv(t)
			record := filepath.Join(e.dirs.Inbox(), baselineName)
			if err := os.Remove(record); err != nil {
				t.Fatal(err)
			}
			old := newChat("3f2a91c0-unread", start)
			talk(old, 2, "walnut")
			for i := range old.rows {
				old.rows[i].Project = "github.com__you__other"
			}
			oldFile := e.write(old)
			before := e.clock.Add(-time.Hour)
			if err := os.Chtimes(oldFile, before, before); err != nil {
				t.Fatal(err)
			}
			folder := filepath.Dir(oldFile)
			locked := folder
			if what == "watermarks" {
				locked = filepath.Join(folder, ".state")
				if err := os.MkdirAll(locked, 0o700); err != nil {
					t.Fatal(err)
				}
			}
			if err := os.Chmod(locked, 0o000); err != nil {
				t.Fatal(err)
			}
			t.Cleanup(func() { _ = os.Chmod(locked, 0o700) })
			if what == "watermarks" {
				var out strings.Builder
				if err := e.job().DryRun(context.Background(), old.host, old.session, &out); err != nil || !strings.Contains(out.String(), "waits to be set aside: its watermark can't be read") {
					t.Fatalf("dry run: %v\n%s", err, out.String())
				}
			}
			if _, err := e.job().Pending(context.Background()); err != nil || len(e.fake.Requests()) != 0 {
				t.Fatalf("first job: %v, %d calls", err, len(e.fake.Requests()))
			}
			want := "\nleft "
			if what == "folder" {
				want = "\nfolder github.com__you__other\n"
			}
			if data, err := os.ReadFile(record); err != nil || !strings.Contains(string(data), want) {
				t.Fatalf("the record doesn't keep what it couldn't read: %q %v", data, err)
			}
			if err := os.Chmod(locked, 0o700); err != nil {
				t.Fatal(err)
			}
			fresh := newChat("3f2a91c0-later", start.Add(time.Hour))
			talk(fresh, 2, "pecan")
			for i := range fresh.rows {
				fresh.rows[i].Project = "github.com__you__other"
			}
			e.write(fresh)
			if s, err := e.job().Pending(context.Background()); err != nil || len(e.fake.Requests()) != 1 || !strings.Contains(string(e.fake.Requests()[0].Input), "pecan") {
				t.Fatalf("after: %v, %d calls (%q)", err, len(e.fake.Requests()), outcomes(s))
			}
			if m := e.mark(oldFile); m.Offset != size(t, oldFile) {
				t.Fatalf("the chat from before updates wasn't set aside: %+v", m)
			}
			if data, err := os.ReadFile(record); err != nil || strings.Contains(string(data), "left ") || strings.Contains(string(data), "folder ") {
				t.Fatalf("the record still lists something: %q %v", data, err)
			}
		})
	}
}

// The folder case's edges: a chat changed in the first try's own second is
// from before it; a stamp that can't be read sets every chat of the folder
// aside; a folder deleted since leaves the record; and a dry run once the
// folder is readable says the next update sets the chat aside.
func TestBaselineFolderEdges(t *testing.T) {
	setup := func(t *testing.T) (*env, *chat, string, string) {
		e := newEnv(t)
		record := filepath.Join(e.dirs.Inbox(), baselineName)
		if err := os.Remove(record); err != nil {
			t.Fatal(err)
		}
		old := newChat("3f2a91c0-edge", start)
		talk(old, 2, "walnut")
		for i := range old.rows {
			old.rows[i].Project = "github.com__you__other"
		}
		return e, old, e.write(old), record
	}
	lock := func(t *testing.T, folder string) {
		if err := os.Chmod(folder, 0o000); err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { _ = os.Chmod(folder, 0o700) })
	}
	t.Run("changed in the first try's second", func(t *testing.T) {
		e, old, file, _ := setup(t)
		e.clock = e.clock.Add(800 * time.Millisecond)
		changed := e.clock.Add(-600 * time.Millisecond)
		if err := os.Chtimes(file, changed, changed); err != nil {
			t.Fatal(err)
		}
		lock(t, filepath.Dir(file))
		if _, err := e.job().Pending(context.Background()); err != nil {
			t.Fatal(err)
		}
		if err := os.Chmod(filepath.Dir(file), 0o700); err != nil {
			t.Fatal(err)
		}
		var out strings.Builder
		if err := e.job().DryRun(context.Background(), old.host, old.session, &out); err != nil || !strings.Contains(out.String(), "The next update would set aside") {
			t.Fatalf("dry run: %v\n%s", err, out.String())
		}
		if _, err := e.job().Pending(context.Background()); err != nil || len(e.fake.Requests()) != 0 {
			t.Fatalf("%v, %d calls: a chat from before updates was sent", err, len(e.fake.Requests()))
		}
	})
	t.Run("a stamp that can't be read", func(t *testing.T) {
		e, _, file, record := setup(t)
		before := e.clock.Add(-time.Hour)
		if err := os.Chtimes(file, before, before); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(record, []byte("damaged\nfolder github.com__you__other\n"), 0o600); err != nil {
			t.Fatal(err)
		}
		if _, err := e.job().Pending(context.Background()); err != nil || len(e.fake.Requests()) != 0 {
			t.Fatalf("%v, %d calls: a chat from before updates was sent", err, len(e.fake.Requests()))
		}
		if m := e.mark(file); m.Offset != size(t, file) {
			t.Fatalf("not set aside: %+v", m)
		}
	})
	t.Run("a file that can't be opened", func(t *testing.T) {
		e, old, file, record := setup(t)
		if err := os.Chmod(file, 0o000); err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { _ = os.Chmod(file, 0o600) })
		var out strings.Builder
		if err := e.job().DryRun(context.Background(), old.host, old.session, &out); err != nil || !strings.Contains(out.String(), "waits to be set aside: its file can't be opened") {
			t.Fatalf("dry run: %v\n%s", err, out.String())
		}
		if _, err := e.job().Pending(context.Background()); err != nil || len(e.fake.Requests()) != 0 {
			t.Fatalf("%v, %d calls", err, len(e.fake.Requests()))
		}
		if data, err := os.ReadFile(record); err != nil || !strings.Contains(string(data), "\nleft ") {
			t.Fatalf("not listed as left: %q %v", data, err)
		}
	})
	t.Run("a folder deleted since", func(t *testing.T) {
		e, _, _, record := setup(t)
		if err := os.WriteFile(record, []byte(start.UTC().Format(time.RFC3339Nano)+"\nfolder github.com__you__gone\n"), 0o600); err != nil {
			t.Fatal(err)
		}
		if _, err := e.job().Pending(context.Background()); err != nil {
			t.Fatal(err)
		}
		if data, err := os.ReadFile(record); err != nil || strings.Contains(string(data), "folder ") {
			t.Fatalf("the deleted folder stays: %q %v", data, err)
		}
	})
}
