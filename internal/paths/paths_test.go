package paths

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestDefault(t *testing.T) {
	isolate(t)
	home := t.TempDir()
	t.Setenv("HOME", home)
	d, err := Default()
	if err != nil {
		t.Fatal(err)
	}
	want := map[string]string{
		"Config":     filepath.Join(home, ".config", "eva-md"),
		"State":      filepath.Join(home, ".local", "state", "eva-md"),
		"Share":      filepath.Join(home, ".local", "share", "eva-md"),
		"Inbox":      filepath.Join(home, ".local", "state", "eva-md", "inbox"),
		"Sessions":   filepath.Join(home, ".local", "state", "eva-md", "sessions"),
		"Locks":      filepath.Join(home, ".local", "state", "eva-md", "locks"),
		"Logs":       filepath.Join(home, ".local", "state", "eva-md", "logs"),
		"Cache":      filepath.Join(home, ".local", "state", "eva-md", "cache"),
		"ConfigFile": filepath.Join(home, ".config", "eva-md", "config.toml"),
	}
	got := map[string]string{
		"Config":     d.Config,
		"State":      d.State,
		"Share":      d.Share,
		"Inbox":      d.Inbox(),
		"Sessions":   d.Sessions(),
		"Locks":      d.Locks(),
		"Logs":       d.Logs(),
		"Cache":      d.Cache(),
		"ConfigFile": d.ConfigFile(),
	}
	for k, w := range want {
		if got[k] != w {
			t.Errorf("%s = %q, want %q", k, got[k], w)
		}
	}
}

func TestDefaultRelativeHome(t *testing.T) {
	isolate(t)
	t.Setenv("HOME", "relative/home")
	if _, err := Default(); err == nil {
		t.Fatal("want an error for a relative home folder")
	}
}

func TestEnsureDir(t *testing.T) {
	isolate(t)
	dir := filepath.Join(t.TempDir(), "a", "b")
	if err := EnsureDir(dir); err != nil {
		t.Fatal(err)
	}
	fi, err := os.Stat(dir)
	if err != nil {
		t.Fatal(err)
	}
	if !fi.IsDir() || fi.Mode().Perm() != 0o700 {
		t.Fatalf("mode = %v, want a 0700 folder", fi.Mode())
	}
}

func TestWriteFileAtomic(t *testing.T) {
	isolate(t)
	dir := t.TempDir()
	p := filepath.Join(dir, "f.txt")
	for _, tc := range []struct {
		data string
		perm os.FileMode
	}{
		{"first", 0o600},
		{"second, longer than the first", 0o644},
		{"", 0o600},
	} {
		if err := WriteFileAtomic(p, []byte(tc.data), tc.perm); err != nil {
			t.Fatal(err)
		}
		got, err := os.ReadFile(p)
		if err != nil {
			t.Fatal(err)
		}
		if string(got) != tc.data {
			t.Errorf("content = %q, want %q", got, tc.data)
		}
		fi, err := os.Stat(p)
		if err != nil {
			t.Fatal(err)
		}
		if fi.Mode().Perm() != tc.perm {
			t.Errorf("mode = %v, want %v", fi.Mode().Perm(), tc.perm)
		}
	}
	assertNoTemp(t, dir)
}

func TestWriteFileAtomicMissingFolder(t *testing.T) {
	isolate(t)
	dir := t.TempDir()
	if err := WriteFileAtomic(filepath.Join(dir, "missing", "f"), []byte("x"), 0o600); err == nil {
		t.Fatal("want an error when the folder is missing")
	}
	assertNoTemp(t, dir)
}

func TestWriteFileAtomicOverFolderCleansUp(t *testing.T) {
	isolate(t)
	dir := t.TempDir()
	target := filepath.Join(dir, "taken")
	if err := os.Mkdir(target, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(target, "child"), []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := WriteFileAtomic(target, []byte("x"), 0o600); err == nil {
		t.Fatal("want an error when renaming over a non-empty folder")
	}
	assertNoTemp(t, dir)
}

func assertNoTemp(t *testing.T, dir string) {
	t.Helper()
	ents, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	for _, e := range ents {
		if strings.HasPrefix(e.Name(), TempPrefix) {
			t.Errorf("temp file left behind: %s", e.Name())
		}
	}
}

func TestSafeName(t *testing.T) {
	isolate(t)
	for _, tc := range []struct {
		in       string
		want     string // exact output, or "" to check only the prefix
		prefix   string
		wantHash bool
	}{
		{in: "claude-code", want: "claude-code"},
		{in: "3f2a91c0-1b2c-4d5e-8f90-123456789abc", want: "3f2a91c0-1b2c-4d5e-8f90-123456789abc"},
		{in: "off_record", want: "off_record"},
		{in: "v1.2", want: "v1.2"},
		{in: "..", prefix: "_.", wantHash: true},
		{in: ".hidden", prefix: "_hidden", wantHash: true},
		{in: "../../etc/passwd", prefix: "_.", wantHash: true},
		{in: "a/b", prefix: "a_b", wantHash: true},
		{in: "a\x00b", prefix: "a_b", wantHash: true},
		{in: "é", prefix: "__", wantHash: true},
		{in: strings.Repeat("x", 200), prefix: strings.Repeat("x", 119), wantHash: true},
		{in: "/" + strings.Repeat("x", 127), prefix: "_", wantHash: true},
	} {
		got, err := SafeName(tc.in)
		if err != nil {
			t.Fatalf("SafeName(%q): %v", tc.in, err)
		}
		if tc.want != "" && got != tc.want {
			t.Errorf("SafeName(%q) = %q, want %q", tc.in, got, tc.want)
		}
		if tc.prefix != "" && !strings.HasPrefix(got, tc.prefix) {
			t.Errorf("SafeName(%q) = %q, want prefix %q", tc.in, got, tc.prefix)
		}
		if tc.wantHash && !strings.Contains(got, "-") {
			t.Errorf("SafeName(%q) = %q, want a hash suffix", tc.in, got)
		}
		if strings.ContainsAny(got, "/\\\x00") || got == "." || got == ".." || strings.HasPrefix(got, ".") {
			t.Errorf("SafeName(%q) = %q is not a safe path component", tc.in, got)
		}
		if len(got) > maxNameLen {
			t.Errorf("SafeName(%q) is %d bytes, want at most %d", tc.in, len(got), maxNameLen)
		}
	}
	if _, err := SafeName(""); err != ErrEmptyName {
		t.Errorf("SafeName(\"\") error = %v, want ErrEmptyName", err)
	}
	a, _ := SafeName("a/b")
	b, _ := SafeName("a:b")
	if a == b {
		t.Errorf("SafeName collides: %q and %q both give %q", "a/b", "a:b", a)
	}
}

func TestRootedWritesRejectEscapeAndCancellation(t *testing.T) {
	isolate(t)
	base := t.TempDir()
	dir := filepath.Join(base, "root")
	root, err := OpenRoot(dir, true)
	if err != nil {
		t.Fatal(err)
	}
	defer root.Close()
	outside := filepath.Join(base, "outside")
	if err := os.Mkdir(outside, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(outside, filepath.Join(dir, "escape")); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"../outside/leak", "escape/leak"} {
		if err := WriteFileAtomicAt(context.Background(), root, name, []byte("private"), 0o600); err == nil {
			t.Errorf("write to %q succeeded", name)
		}
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if err := WriteFileAtomicAt(ctx, root, "cancelled", []byte("private"), 0o600); !errors.Is(err, context.Canceled) {
		t.Fatalf("cancelled write: %v", err)
	}
	entries, err := os.ReadDir(outside)
	if err != nil || len(entries) != 0 {
		t.Fatalf("outside changed: %v, %v", entries, err)
	}
	assertNoTemp(t, dir)
	if _, err := os.Stat(filepath.Join(dir, "cancelled")); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("cancelled write exists: %v", err)
	}
	if _, err := OpenRoot(filepath.Join(dir, "escape"), true); err == nil {
		t.Fatal("opened a symlink root")
	}
	if _, err := OpenRoot(filepath.Join(dir, "escape", "nested"), true); err == nil {
		t.Fatal("opened a root through a symlink ancestor")
	}
	if _, err := os.Stat(filepath.Join(outside, "nested")); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("created a root outside the intended tree: %v", err)
	}
}
