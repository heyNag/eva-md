package main

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/heyNag/eva-md/internal/wikigit"
	"github.com/heyNag/eva-md/internal/wikiinit"
)

// eva-md commit prints one line, exits 1 only when the run is refused or
// fails, and never prints a secret it left out.
func TestCommitCommand(t *testing.T) {
	dirs, home := gitHomeFixture(t, "")
	commit := func(args ...string) (int, string, string) {
		var out, errOut bytes.Buffer
		code := run(context.Background(), append([]string{"commit"}, args...), strings.NewReader(""), &out, &errOut)
		return code, out.String(), errOut.String()
	}
	if code, _, errOut := commit(); code != 1 || !strings.Contains(errOut, "no wiki is set up") {
		t.Fatalf("without a wiki: %d %q", code, errOut)
	}
	if code, _, errOut := commit("--push"); code != 2 || !strings.Contains(errOut, "invalid arguments for commit") {
		t.Fatalf("bad flag: %d %q", code, errOut)
	}
	remote, wiki := filepath.Join(home, "remote.git"), filepath.Join(home, "wiki")
	if _, err := wikigit.Run(context.Background(), time.Minute, home, "init", "--bare", "--quiet", remote); err != nil {
		t.Fatal(err)
	}
	if _, err := wikiinit.Run(context.Background(), wikiinit.Options{Remote: remote, Dir: wiki, Dirs: dirs, Version: "test", OKFModule: "v0.7.2"}); err != nil {
		t.Fatal(err)
	}
	secret := "ghp_" + strings.Repeat("ab12", 10)
	for name, text := range map[string]string{"general/threads/leak.md": "The CI token is " + secret + "\n", "general/threads/notes.md": "Notes.\n"} {
		if err := os.WriteFile(filepath.Join(wiki, filepath.FromSlash(name)), []byte(text), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	for i, tc := range []struct {
		args []string
		code int
		line string
	}{
		{nil, 0, "committed (1 page, 1 left out), push ok\n"},
		{nil, 0, "nothing (1 left out), push skipped\n"},
		{[]string{"--push-only"}, 0, "push up_to_date\n"},
		{nil, 1, "refused (origin), push skipped\n"},
		{[]string{"--push-only"}, 1, "refused (origin), push skipped\n"},
	} {
		if i == 3 {
			if _, err := wikigit.Run(context.Background(), time.Minute, wiki, "remote", "set-url", "origin", "git@example.invalid:other/wiki.git"); err != nil {
				t.Fatal(err)
			}
		}
		code, out, errOut := commit(tc.args...)
		if code != tc.code || out != tc.line || errOut != "" {
			t.Fatalf("%v: %d %q %q", tc.args, code, out, errOut)
		}
		if strings.Contains(out+errOut, secret) {
			t.Fatal("printed the secret")
		}
	}
	// Another run holds the commit lock: a run, push-only or not, is busy and
	// exits 0.
	lock, err := os.OpenFile(filepath.Join(dirs.Locks(), "commit.lock"), os.O_CREATE|os.O_RDWR, 0o600)
	if err != nil {
		t.Fatal(err)
	}
	defer lock.Close()
	if err := syscall.Flock(int(lock.Fd()), syscall.LOCK_EX|syscall.LOCK_NB); err != nil {
		t.Fatal(err)
	}
	for _, args := range [][]string{nil, {"--push-only"}} {
		if code, out, errOut := commit(args...); code != 0 || out != "busy, push skipped\n" || errOut != "" {
			t.Fatalf("busy %v: %d %q %q", args, code, out, errOut)
		}
	}
}
