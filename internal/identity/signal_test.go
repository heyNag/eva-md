package identity

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
)

// fakeGitOnConfig puts a git first on PATH that runs the real git, but runs
// onConfig instead when git is asked for its config, as Resolve does last.
func fakeGitOnConfig(t *testing.T, onConfig string) {
	t.Helper()
	real, err := exec.LookPath("git")
	if err != nil {
		t.Skip("git not found")
	}
	dir := t.TempDir()
	script := "#!/bin/sh\nulimit -c 0 2>/dev/null\nfor a in \"$@\"; do\n\tif [ \"$a\" = config ]; then\n\t\t" + onConfig + "\n\tfi\ndone\nexec '" + real + "' \"$@\"\n"
	if err := os.WriteFile(filepath.Join(dir, "git"), []byte(script), 0o700); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", dir+string(os.PathListSeparator)+os.Getenv("PATH"))
}

// A git that a signal ends fails Resolve closed, as before, and the error
// now names the signal in place of "exited with -1". The signal's name is
// all it adds: no path of the repository or of git.
func TestResolveNamesTheSignalThatEndedGit(t *testing.T) {
	for _, c := range []struct {
		name string
		sig  syscall.Signal
	}{
		{"KILL", syscall.SIGKILL},
		{"SEGV", syscall.SIGSEGV},
		{"TERM", syscall.SIGTERM},
	} {
		t.Run(c.name, func(t *testing.T) {
			home := gitEnvForTest(t)
			repo := newRepo(t, filepath.Join(home, "code", "repo"), true)
			// The shell ends itself; exit 99 shows a signal that did not.
			fakeGitOnConfig(t, "kill -s "+c.name+" $$; exit 99")
			got, err := Resolve(context.Background(), repo)
			if got != General() {
				t.Fatalf("got %+v; want the general area", got)
			}
			want := "identity: git config ended by signal: " + c.sig.String()
			if err == nil || err.Error() != want {
				t.Fatalf("got error %v; want %q", err, want)
			}
			if strings.Contains(err.Error(), home) || strings.Contains(err.Error(), "exited with") {
				t.Fatalf("error %q names a path or an exit code", err)
			}
		})
	}
}

// A git that exits with a status reads as it always has.
func TestResolveNamesTheStatusGitExitedWith(t *testing.T) {
	home := gitEnvForTest(t)
	repo := newRepo(t, filepath.Join(home, "code", "repo"), true)
	fakeGitOnConfig(t, "exit 3")
	got, err := Resolve(context.Background(), repo)
	if got != General() || err == nil || err.Error() != "identity: git config exited with 3" {
		t.Fatalf("got %+v, %v; want the general area and git config exited with 3", got, err)
	}
}

// The wrapper gitCounter puts first on PATH runs git as its child, so a git
// that a signal ends leaves its status, 128 plus the signal's number, in the
// file beside the count, and the helper's text shows it; a git that exits 0
// leaves nothing there. The count is one a run, as before.
func TestGitCounterKeepsTheStatusOfAGitASignalEnded(t *testing.T) {
	home := gitEnvForTest(t)
	repo := newRepo(t, filepath.Join(home, "code", "repo"), true)
	fakeGitOnConfig(t, "kill -s KILL $$; exit 99")
	f := cacheFixture{home: home, cache: Cache{Dir: filepath.Join(home, "cache")}}
	f.runs, f.statuses = wrapGit(t)

	if out, err := exec.Command("git", "--version").CombinedOutput(); err != nil {
		t.Fatalf("git --version: %v\n%s", err, out)
	}
	if n := f.runs(); n != 1 {
		t.Fatalf("git ran %d times; want 1", n)
	}
	if lines := readStatuses(t, f.statuses); len(lines) != 0 {
		t.Fatalf("a git that exited 0 left %q", lines)
	}
	if text := f.gitExits(); text != "no git the test started exited with a status other than 0" {
		t.Fatalf("helper says %q", text)
	}

	got, err := Resolve(context.Background(), repo)
	if err == nil || got != General() {
		t.Fatalf("got %+v, %v; want an error", got, err)
	}
	// The wrapper outlives git, so Resolve sees its exit, not a signal.
	status := fmt.Sprint(128 + int(syscall.SIGKILL))
	if want := "identity: git config exited with " + status; err.Error() != want {
		t.Fatalf("got error %v; want %q", err, want)
	}
	if n := f.runs(); n != 4 {
		t.Fatalf("git ran %d times; want 4", n)
	}
	lines := readStatuses(t, f.statuses)
	if len(lines) != 1 || lines[0] != status {
		t.Fatalf("status file holds %q; want one line, %s", lines, status)
	}
	if text, want := f.gitExits(), "statuses of the gits the test started that did not exit 0: "+status; text != want {
		t.Fatalf("helper says %q; want %q", text, want)
	}
}

func readStatuses(t *testing.T, path string) []string {
	t.Helper()
	data, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		return nil
	}
	if err != nil {
		t.Fatal(err)
	}
	return strings.Fields(string(data))
}
