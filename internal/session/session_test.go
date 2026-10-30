package session

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
)

func newMarkers(t *testing.T) Markers {
	t.Helper()
	return Markers{Root: filepath.Join(t.TempDir(), "sessions")}
}

func TestSymlinkConfinement(t *testing.T) {
	isolate(t)
	m := newMarkers(t)
	outside := t.TempDir()
	if err := os.MkdirAll(m.Root, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(outside, filepath.Join(m.Root, "codex")); err != nil {
		t.Fatal(err)
	}
	if err := m.Set("codex", "s", Model, "private"); err == nil {
		t.Fatal("Set followed escaping directory")
	}
	if _, _, err := m.Get("codex", "s", Model); err == nil {
		t.Fatal("Get followed escaping directory")
	}
	if err := m.Clear("codex", "s", Model); err == nil {
		t.Fatal("Clear followed escaping directory")
	}
	entries, err := os.ReadDir(outside)
	if err != nil || len(entries) != 0 {
		t.Fatalf("outside changed: %v, %v", entries, err)
	}
}

func TestReadRejectsNonRegularFile(t *testing.T) {
	isolate(t)
	m := newMarkers(t)
	if err := m.Set("codex", "s", Model, "v"); err != nil {
		t.Fatal(err)
	}
	file := filepath.Join(m.Root, "codex", "s", Model)
	if err := os.Remove(file); err != nil {
		t.Fatal(err)
	}
	if err := syscall.Mkfifo(file, 0o600); err != nil {
		t.Fatal(err)
	}
	if _, _, err := m.Get("codex", "s", Model); err == nil {
		t.Fatal("read FIFO")
	}
	if err := os.Remove(file); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink("../s/briefed", file); err != nil {
		t.Fatal(err)
	}
	if _, _, err := m.Get("codex", "s", Model); err == nil {
		t.Fatal("read symlink")
	}
}

func TestCancelledMarkersDoNotMutate(t *testing.T) {
	isolate(t)
	m := newMarkers(t)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if err := m.SetContext(ctx, "codex", "s", Model, "v"); !errors.Is(err, context.Canceled) {
		t.Fatalf("SetContext: %v", err)
	}
	if _, _, err := m.GetContext(ctx, "codex", "s", Model); !errors.Is(err, context.Canceled) {
		t.Fatalf("GetContext: %v", err)
	}
	if err := m.ClearContext(ctx, "codex", "s", Model); !errors.Is(err, context.Canceled) {
		t.Fatalf("ClearContext: %v", err)
	}
	if _, err := os.Stat(m.Root); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("root created: %v", err)
	}
}

func TestSetGetClear(t *testing.T) {
	isolate(t)
	m := newMarkers(t)
	if v, ok, err := m.Get("codex", "s1", OffRecord); err != nil || ok || v != "" {
		t.Fatalf("Get before Set = %q, %v, %v", v, ok, err)
	}
	for _, name := range []string{OffRecord, Briefed, Model, Thread} {
		if err := m.Set("codex", "s1", name, "value-"+name); err != nil {
			t.Fatalf("Set %s: %v", name, err)
		}
	}
	for _, name := range []string{OffRecord, Briefed, Model, Thread} {
		v, ok, err := m.Get("codex", "s1", name)
		if err != nil || !ok || v != "value-"+name {
			t.Fatalf("Get %s = %q, %v, %v", name, v, ok, err)
		}
	}
	if err := m.Set("codex", "s1", Model, "gpt-5.5-codex"); err != nil {
		t.Fatal(err)
	}
	if v, _, _ := m.Get("codex", "s1", Model); v != "gpt-5.5-codex" {
		t.Fatalf("overwrite: got %q", v)
	}
	if err := m.Clear("codex", "s1", OffRecord); err != nil {
		t.Fatal(err)
	}
	if _, ok, err := m.Get("codex", "s1", OffRecord); err != nil || ok {
		t.Fatalf("Get after Clear = %v, %v", ok, err)
	}
	if err := m.Clear("codex", "s1", OffRecord); err != nil {
		t.Fatalf("second Clear: %v", err)
	}
	if err := m.Clear("codex", "never-seen", Briefed); err != nil {
		t.Fatalf("Clear of an unknown session: %v", err)
	}
}

func TestEmptyValueIsSet(t *testing.T) {
	isolate(t)
	m := newMarkers(t)
	if err := m.Set("claude-code", "s", OffRecord, ""); err != nil {
		t.Fatal(err)
	}
	if v, ok, err := m.Get("claude-code", "s", OffRecord); err != nil || !ok || v != "" {
		t.Fatalf("Get = %q, %v, %v; want an empty value that is set", v, ok, err)
	}
}

func TestHostsAndSessionsAreSeparate(t *testing.T) {
	isolate(t)
	m := newMarkers(t)
	if err := m.Set("codex", "same", Model, "a"); err != nil {
		t.Fatal(err)
	}
	if err := m.Set("claude-code", "same", Model, "b"); err != nil {
		t.Fatal(err)
	}
	if err := m.Set("codex", "other", Model, "c"); err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct{ host, session, want string }{
		{"codex", "same", "a"},
		{"claude-code", "same", "b"},
		{"codex", "other", "c"},
	} {
		if v, _, _ := m.Get(tc.host, tc.session, Model); v != tc.want {
			t.Errorf("Get(%s, %s) = %q, want %q", tc.host, tc.session, v, tc.want)
		}
	}
}

func TestPermissions(t *testing.T) {
	isolate(t)
	m := newMarkers(t)
	if err := m.Set("codex", "s1", Briefed, "h"); err != nil {
		t.Fatal(err)
	}
	for _, p := range []string{m.Root, filepath.Join(m.Root, "codex"), filepath.Join(m.Root, "codex", "s1")} {
		fi, err := os.Stat(p)
		if err != nil {
			t.Fatal(err)
		}
		if fi.Mode().Perm() != 0o700 {
			t.Errorf("%s mode = %v, want 0700", p, fi.Mode().Perm())
		}
	}
	fi, err := os.Stat(filepath.Join(m.Root, "codex", "s1", Briefed))
	if err != nil {
		t.Fatal(err)
	}
	if fi.Mode().Perm() != 0o600 {
		t.Errorf("marker mode = %v, want 0600", fi.Mode().Perm())
	}
}

func TestHostileNamesStayInsideRoot(t *testing.T) {
	isolate(t)
	base := t.TempDir()
	m := Markers{Root: filepath.Join(base, "sessions")}
	for _, tc := range []struct{ host, session, name string }{
		{"..", "..", "escape"},
		{"codex", "../../outside", Model},
		{"codex", "/etc/passwd", Model},
		{"codex", "s1", "../../../x"},
		{"codex", ".", Model},
		{"codex", "a\x00b", Model},
	} {
		if err := m.Set(tc.host, tc.session, tc.name, "v"); err != nil {
			t.Fatalf("Set(%q, %q, %q): %v", tc.host, tc.session, tc.name, err)
		}
		if v, ok, err := m.Get(tc.host, tc.session, tc.name); err != nil || !ok || v != "v" {
			t.Fatalf("Get(%q, %q, %q) = %q, %v, %v", tc.host, tc.session, tc.name, v, ok, err)
		}
	}
	err := filepath.Walk(base, func(p string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		if !strings.HasPrefix(p, m.Root) && p != base {
			t.Errorf("file written outside Root: %s", p)
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
}

func TestRejectsBadInput(t *testing.T) {
	isolate(t)
	m := newMarkers(t)
	if err := m.Set("", "s", Model, "v"); err == nil {
		t.Error("want an error for an empty host")
	}
	if err := m.Set("codex", "", Model, "v"); err == nil {
		t.Error("want an error for an empty session")
	}
	if err := m.Set("codex", "s", "", "v"); err == nil {
		t.Error("want an error for an empty name")
	}
	if err := m.Set("codex", "s", Model, strings.Repeat("x", MaxValue+1)); err == nil {
		t.Error("want an error for an oversized value")
	}
	if err := (Markers{Root: "relative"}).Set("codex", "s", Model, "v"); err == nil {
		t.Error("want an error for a relative root")
	}
}

func TestGetRefusesOversizedFile(t *testing.T) {
	isolate(t)
	m := newMarkers(t)
	if err := m.Set("codex", "s", Model, "v"); err != nil {
		t.Fatal(err)
	}
	file := filepath.Join(m.Root, "codex", "s", Model)
	if err := os.WriteFile(file, []byte(strings.Repeat("x", MaxValue+1)), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, _, err := m.Get("codex", "s", Model); err == nil {
		t.Fatal("want an error for an oversized marker file")
	}
}
