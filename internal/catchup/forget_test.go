package catchup

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/heyNag/eva-md/internal/inbox"
	"github.com/heyNag/eva-md/internal/session"
)

// A recovery cursor that lags behind the hooks would bring a removed
// capture's rows back into a new inbox file, so Forget removes the record
// of a session whose capture in its project folder is removed, and recovery
// then passes the session by. A record that appends to another folder
// stays, a damaged one goes, and a missing one is no error.
func TestForgetKeepsRecoveryFromBringingBackARemovedCapture(t *testing.T) {
	ctx := context.Background()
	dirs, file, e := fixture(t, "claude-code")
	appendJSON(t, file, claudeRow(e, "user", "the plan we dropped", "turn1"))
	appendJSON(t, file, claudeRow(e, "assistant", "Dropped.", "turn1"))
	prompt := e
	prompt.Role, prompt.Kind, prompt.Text, prompt.Turn = "user", "prompt", "the plan we dropped", "turn1"
	store := inbox.Store{Root: dirs.Inbox()}
	if err := store.Append(prompt); err != nil {
		t.Fatal(err)
	}
	// The removal: the session's file goes, and with it its record.
	if err := os.Remove(store.Path(e.Project, e.Host, e.Session)); err != nil {
		t.Fatal(err)
	}
	record := filepath.Join(dirs.State, "catchup", e.Host, e.Session+".json")
	if err := Forget(ctx, dirs, e.Host, e.Session, []string{"elsewhere"}); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Lstat(record); err != nil {
		t.Fatalf("a record that appends to another folder went: %v", err)
	}
	if err := Forget(ctx, dirs, e.Host, e.Session, []string{"elsewhere", "project"}); err != nil {
		t.Fatal(err)
	}
	if n, err := Recover(ctx, dirs, e.Host, e.Session); err != nil || n != 0 {
		t.Fatalf("recovered %d rows, %v", n, err)
	}
	if rows := readInbox(t, dirs, e); len(rows) != 0 {
		t.Fatalf("the removed capture came back: %d rows", len(rows))
	}
	if _, err := os.Lstat(record); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("the record stayed: %v", err)
	}
	if err := Forget(ctx, dirs, e.Host, e.Session, []string{"project"}); err != nil {
		t.Fatalf("a missing record: %v", err)
	}
	if err := os.WriteFile(record, []byte("not a record"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := Forget(ctx, dirs, e.Host, e.Session, []string{"elsewhere"}); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Lstat(record); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("a damaged record stayed: %v", err)
	}
}

// A blocked record stays when its capture is removed, as one that alone
// keeps a chat off the record must: here its markers couldn't be written.
// Removed, the chat's next hook, finding no marker, would register the chat
// on the record, and recovery would store what it says next. Kept, the
// next registration keeps the chat off the record and recovery stores
// nothing.
func TestForgetKeepsARecordThatKeepsTheChatOffTheRecord(t *testing.T) {
	ctx := context.Background()
	dirs, file, e := fixture(t, "claude-code")
	home := os.Getenv("HOME")
	if err := os.MkdirAll(dirs.Sessions(), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(dirs.Sessions(), 0o500); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.Chmod(dirs.Sessions(), 0o700) })
	appendJSON(t, file, claudeRow(e, "user", "off the record", "turn1"))
	appendJSON(t, file, claudeRow(e, "assistant", "a private reply", "turn1"))
	if n, err := Recover(ctx, dirs, e.Host, e.Session); n != 0 || !errors.Is(err, ErrPrivacyBoundary) {
		t.Fatalf("the first pass recovered %d rows, %v", n, err)
	}
	markers := session.Markers{Root: dirs.Sessions()}
	if _, set, _ := markers.GetContext(ctx, e.Host, e.Session, session.OffRecord); set {
		t.Fatal("the off-the-record marker was written")
	}
	if s, found, err := load(ctx, dirs, e.Host, e.Session); err != nil || !found || !s.Blocked || s.Reason != "off_record" {
		t.Fatalf("the record doesn't keep the chat off the record: %+v %v %v", s, found, err)
	}
	if err := Forget(ctx, dirs, e.Host, e.Session, []string{"project"}); err != nil {
		t.Fatal(err)
	}
	// The chat's next hook finds no marker, so it registers it as on the
	// record.
	if err := Register(ctx, dirs, home, file, filepath.Join(home, e.Cwd), e, false, false, time.Date(2026, 10, 29, 11, 0, 0, 0, time.UTC)); err != nil {
		t.Fatal(err)
	}
	if s, found, err := load(ctx, dirs, e.Host, e.Session); err != nil || !found || !s.Blocked || s.Reason != "off_record" {
		t.Fatalf("the chat came back on the record: %+v %v %v", s, found, err)
	}
	appendJSON(t, file, claudeRow(e, "user", "still private", "turn2"))
	appendJSON(t, file, claudeRow(e, "assistant", "a reply still private", "turn2"))
	if n, err := Recover(ctx, dirs, e.Host, e.Session); n != 0 || !errors.Is(err, ErrPrivacyBoundary) {
		t.Fatalf("the next pass recovered %d rows, %v", n, err)
	}
	if rows := readInbox(t, dirs, e); len(rows) != 0 {
		t.Fatalf("%d rows off the record were stored", len(rows))
	}
}
