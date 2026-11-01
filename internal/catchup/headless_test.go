package catchup

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"
)

// Headless reads the mark the hooks register: none for a person's chat or a
// session with no record, and the mark once a program's hook registers it.
func TestHeadlessReadsTheRecord(t *testing.T) {
	ctx := context.Background()
	dirs, path, e := fixture(t, "claude-code")
	if Headless(ctx, dirs, e.Host, e.Session) || Headless(ctx, dirs, e.Host, "no-record") {
		t.Fatal("marked without a headless registration")
	}
	home := os.Getenv("HOME")
	if err := Register(ctx, dirs, home, path, filepath.Join(home, e.Cwd), e, false, true, time.Date(2026, 10, 29, 10, 1, 0, 0, time.UTC)); err != nil {
		t.Fatal(err)
	}
	if !Headless(ctx, dirs, e.Host, e.Session) || Headless(ctx, dirs, "codex", e.Session) {
		t.Fatal("the headless registration is not read as its session's alone")
	}
}
