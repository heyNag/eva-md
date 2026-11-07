package catchup

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/heyNag/eva-md/internal/paths"
	"github.com/heyNag/eva-md/internal/session"
)

// plantRecord writes a file under <state>/catchup, last changed at mod.
func plantRecord(t *testing.T, dirs paths.Dirs, rel string, mod time.Time) {
	t.Helper()
	file := filepath.Join(dirs.State, "catchup", rel)
	if err := os.MkdirAll(filepath.Dir(file), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(file, []byte("{}"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Chtimes(file, mod, mod); err != nil {
		t.Fatal(err)
	}
}

// checkRecords fails for each of gone that is still there and each of kept
// that isn't.
func checkRecords(t *testing.T, dirs paths.Dirs, gone, kept []string) {
	t.Helper()
	for _, rel := range gone {
		if _, err := os.Lstat(filepath.Join(dirs.State, "catchup", rel)); !os.IsNotExist(err) {
			t.Errorf("%s was kept: %v", rel, err)
		}
	}
	for _, rel := range kept {
		if _, err := os.Lstat(filepath.Join(dirs.State, "catchup", rel)); err != nil {
			t.Errorf("%s was removed: %v", rel, err)
		}
	}
}

// The daily prune removes the records not saved for 30 days, and nothing
// else: the drift records beside the hosts' folders, files that aren't
// records, a symlink, a record exactly 30 days old and one saved in the
// future all stay.
func TestPruneRemovesRecordsOlderThan30Days(t *testing.T) {
	dirs := paths.Dirs{State: t.TempDir()}
	now := time.Date(2026, 11, 7, 12, 0, 0, 0, time.UTC)
	old := now.Add(-60 * 24 * time.Hour)
	plantRecord(t, dirs, "claude-code/old.json", old)
	plantRecord(t, dirs, "claude-code/fresh.json", now.Add(-time.Minute))
	plantRecord(t, dirs, "codex/at-the-limit.json", now.Add(-30*24*time.Hour))
	plantRecord(t, dirs, "codex/past-the-limit.json", now.Add(-30*24*time.Hour-time.Second))
	plantRecord(t, dirs, "codex/from-the-future.json", now.Add(time.Hour))
	plantRecord(t, dirs, "codex/"+paths.TempPrefix+"0123abcd", old)
	plantRecord(t, dirs, "codex/notes.txt", old)
	plantRecord(t, dirs, "drift-codex.json", old)
	plantRecord(t, dirs, "drift-claude-code.json", old)
	if err := os.Symlink("past-the-limit.json", filepath.Join(dirs.State, "catchup", "codex", "link.json")); err != nil {
		t.Fatal(err)
	}
	if removed, err := Prune(context.Background(), dirs, now); err != nil || removed != 2 {
		t.Fatalf("Prune removed %d, %v; want 2", removed, err)
	}
	checkRecords(t, dirs, []string{"claude-code/old.json", "codex/past-the-limit.json"}, []string{
		"claude-code/fresh.json", "codex/at-the-limit.json", "codex/from-the-future.json", "codex/" + paths.TempPrefix + "0123abcd",
		"codex/notes.txt", "codex/link.json", "drift-codex.json", "drift-claude-code.json",
	})
	if removed, err := Prune(context.Background(), dirs, now); err != nil || removed != 0 {
		t.Fatalf("a second prune removed %d, %v", removed, err)
	}
}

// Past the newest records a host keeps, the older ones go, however recent,
// newest by when each was last saved and, for the same time, by name; each
// host counts its own. The test keeps 3, where Prune keeps RecordsKept.
func TestPruneKeepsTheNewestOfEachHost(t *testing.T) {
	dirs := paths.Dirs{State: t.TempDir()}
	now := time.Date(2026, 11, 7, 12, 0, 0, 0, time.UTC)
	for i := 0; i < 5; i++ {
		plantRecord(t, dirs, fmt.Sprintf("claude-code/chat-%d.json", i), now.Add(-time.Duration(i)*time.Minute))
	}
	for _, name := range []string{"a", "b", "c", "d"} {
		plantRecord(t, dirs, "codex/"+name+".json", now.Add(-time.Hour))
	}
	if removed, err := prune(context.Background(), dirs, now, RecordsMaxAge, 3); err != nil || removed != 3 {
		t.Fatalf("prune removed %d, %v; want 3", removed, err)
	}
	checkRecords(t, dirs, []string{"claude-code/chat-3.json", "claude-code/chat-4.json", "codex/d.json"},
		[]string{"claude-code/chat-0.json", "claude-code/chat-1.json", "claude-code/chat-2.json", "codex/a.json", "codex/b.json", "codex/c.json"})
}

// saveRecord saves a recovery record of a session as the hooks save it,
// last changed at mod, and returns its path under <state>/catchup.
func saveRecord(t *testing.T, dirs paths.Dirs, host, id string, mod time.Time) string {
	t.Helper()
	if err := save(context.Background(), dirs, Source{V: 1, Host: host, Session: id, Updated: mod.Format(time.RFC3339)}); err != nil {
		t.Fatal(err)
	}
	rel, err := stateFile(host, id)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Chtimes(filepath.Join(dirs.State, "catchup", rel), mod, mod); err != nil {
		t.Fatal(err)
	}
	return rel
}

// The hooks save a record only under its session's lock, so the prune
// removes a record only while it holds that lock, taken by the session the
// record names, also when its id is one a file's name can't hold: while a
// hook holds the session, its record stays, however old, and goes at a
// later prune once the hook lets go.
func TestPruneLeavesTheRecordOfASessionAHookHolds(t *testing.T) {
	dirs := paths.Dirs{State: t.TempDir()}
	now := time.Date(2026, 11, 7, 12, 0, 0, 0, time.UTC)
	old := now.Add(-60 * 24 * time.Hour)
	sessions := []struct{ host, id string }{{"claude-code", "3f2a91c0-1111-2222-3333-444444444444"}, {"codex", "odd:id"}}
	var records []string
	var holds []func()
	for _, s := range sessions {
		records = append(records, saveRecord(t, dirs, s.host, s.id, old))
		release, err := session.Lock(context.Background(), dirs.Locks(), s.host, s.id)
		if err != nil {
			t.Fatal(err)
		}
		holds = append(holds, release)
	}
	if records[1] == "codex/odd:id.json" {
		t.Fatal("the odd id's record is named by its id")
	}
	removed, err := Prune(context.Background(), dirs, now)
	if err != nil || removed != 0 {
		t.Fatalf("Prune removed %d records hooks hold, %v", removed, err)
	}
	checkRecords(t, dirs, nil, records)
	for _, release := range holds {
		release()
	}
	if removed, err := Prune(context.Background(), dirs, now); err != nil || removed != 2 {
		t.Fatalf("Prune removed %d, %v; want 2", removed, err)
	}
	checkRecords(t, dirs, records, nil)
}

// A record a hook saved again after the prune listed it is no longer the
// one listed, which the prune checks under the session's lock: it stays.
func TestPruneKeepsARecordSavedAgainAfterTheListing(t *testing.T) {
	dirs := paths.Dirs{State: t.TempDir()}
	now := time.Date(2026, 11, 7, 12, 0, 0, 0, time.UTC)
	listed := now.Add(-60 * 24 * time.Hour)
	rel := saveRecord(t, dirs, "codex", "019a2b3c-5555-6666-7777-888888888888", listed)
	saveRecord(t, dirs, "codex", "019a2b3c-5555-6666-7777-888888888888", now)
	root, err := paths.OpenRoot(filepath.Join(dirs.State, "catchup"), false)
	if err != nil {
		t.Fatal(err)
	}
	defer root.Close()
	if gone, err := pruneRecord(context.Background(), root, dirs, rel, listed); gone || err != nil {
		t.Fatalf("the record saved again went: %v %v", gone, err)
	}
	checkRecords(t, dirs, nil, []string{rel})
	if gone, err := pruneRecord(context.Background(), root, dirs, rel, now); !gone || err != nil {
		t.Fatalf("the record as listed stayed: %v %v", gone, err)
	}
	checkRecords(t, dirs, []string{rel}, nil)
}

// The prune checks the record is still the one listed only once it holds
// the session's lock: a hook that saves the record again after the prune
// read it, and before the prune takes the lock, replaces the record the
// listing named, and it stays. Here the hook takes the lock, free until the
// prune tries it, saves and lets go between the two.
func TestPruneKeepsARecordSavedBeforeItLocks(t *testing.T) {
	dirs := paths.Dirs{State: t.TempDir()}
	now := time.Date(2026, 11, 7, 12, 0, 0, 0, time.UTC)
	listed := now.Add(-60 * 24 * time.Hour)
	host, id := "codex", "019a2b3c-5555-6666-7777-888888888888"
	rel := saveRecord(t, dirs, host, id, listed)
	saves := 0
	beforePruneLock = func() {
		release, busy, err := session.TryLock(dirs.Locks(), host, id)
		if err != nil || busy {
			t.Errorf("the session's lock was held before the prune tried it: %v %v", busy, err)
			return
		}
		defer release()
		saveRecord(t, dirs, host, id, now)
		saves++
	}
	t.Cleanup(func() { beforePruneLock = func() {} })
	if removed, err := Prune(context.Background(), dirs, now); saves != 1 || err != nil || removed != 0 {
		t.Fatalf("Prune removed %d after %d saves, %v; want the record saved again kept", removed, saves, err)
	}
	checkRecords(t, dirs, nil, []string{rel})
}

// A blocked record of a chat off the record may be all that keeps the chat
// off the record, when its marker couldn't be written, so the prune leaves
// it, however old, while the session's off-the-record marker isn't set,
// and removes it once the marker, which recovery writes again at the chat's
// next pass, holds the boundary. A block older versions saved for another
// reason is pruned as any record is.
func TestPruneLeavesARecordThatAloneKeepsAChatOffTheRecord(t *testing.T) {
	ctx := context.Background()
	dirs := paths.Dirs{State: t.TempDir()}
	now := time.Date(2026, 11, 7, 12, 0, 0, 0, time.UTC)
	old := now.Add(-60 * 24 * time.Hour)
	var records []string
	for _, s := range []Source{
		{V: 1, Host: "claude-code", Session: "3f2a91c0-1111-2222-3333-444444444444", Blocked: true, Reason: "off_record"},
		{V: 1, Host: "codex", Session: "019a2b3c-5555-6666-7777-888888888888", Blocked: true, Reason: "elsewhere"},
	} {
		if err := save(ctx, dirs, s); err != nil {
			t.Fatal(err)
		}
		rel, err := stateFile(s.Host, s.Session)
		if err != nil {
			t.Fatal(err)
		}
		if err := os.Chtimes(filepath.Join(dirs.State, "catchup", rel), old, old); err != nil {
			t.Fatal(err)
		}
		records = append(records, rel)
	}
	if removed, err := Prune(ctx, dirs, now); err != nil || removed != 1 {
		t.Fatalf("Prune removed %d, %v; want 1", removed, err)
	}
	checkRecords(t, dirs, records[1:], records[:1])
	if err := (session.Markers{Root: dirs.Sessions()}).Set("claude-code", "3f2a91c0-1111-2222-3333-444444444444", session.OffRecord, offRecordFound); err != nil {
		t.Fatal(err)
	}
	if removed, err := Prune(ctx, dirs, now); err != nil || removed != 1 {
		t.Fatalf("Prune removed %d once the marker was set, %v; want 1", removed, err)
	}
	checkRecords(t, dirs, records, nil)
}

// A state folder with no records yet has nothing to prune.
func TestPruneWithoutRecords(t *testing.T) {
	if removed, err := Prune(context.Background(), paths.Dirs{State: t.TempDir()}, time.Now()); err != nil || removed != 0 {
		t.Fatalf("Prune removed %d, %v", removed, err)
	}
}
