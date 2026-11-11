package hook

import (
	"bytes"
	"encoding/json"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/heyNag/eva-md/internal/inbox"
)

// newBuildArgs is where the test binary, started as the installed binary's
// update job, writes the arguments it was given, under its HOME.
const newBuildArgs = "new-build-args"

// The test binary stands in for this build of eva-md as the job a hook
// spawns: started with update as its first argument, it writes its
// arguments and exits, before any test runs.
func init() {
	if len(os.Args) > 1 && os.Args[1] == "update" {
		file := filepath.Join(os.Getenv("HOME"), newBuildArgs)
		_ = os.WriteFile(file+".tmp", []byte(strings.Join(os.Args[1:], "\n")+"\n"), 0o600)
		_ = os.Rename(file+".tmp", file)
		os.Exit(0)
	}
}

// Each trigger names itself on the update it spawns, by the names the run
// log records: SessionEnd session_end, PreCompact compact, a Stop past 60
// KB stop_size, a prompt after a gap prompt_gap, and a chat start's
// catch-up chat_start. The flag follows the sessions, before a sweep.
func TestEachTriggerNamesItsUpdate(t *testing.T) {
	for _, tc := range []struct{ event, source, trigger string }{
		{"SessionEnd", "", "session_end"},
		{"PreCompact", "", "compact"},
		{"Stop", "", "stop_size"},
		{"UserPromptSubmit", "", "prompt_gap"},
		{"SessionStart", "resume", "chat_start"},
	} {
		t.Run(tc.event, func(t *testing.T) {
			env, p, spawned := triggerSetup(t)
			// The chat's own file: idle an hour, with 120 KB to process,
			// which each trigger takes.
			sessionFile(t, env, "claude-code", p.Session, 2*stopBacklog, inbox.Mark{}, env.Now().Add(-time.Hour))
			reply := "Done."
			p.Event, p.Source, p.Reply = tc.event, tc.source, &reply
			invoke(t, "claude-code", p, env)
			want := [][]string{{"update", "--session", "claude-code:test-session", "--trigger", tc.trigger}}
			if got := spawned(); !reflect.DeepEqual(got, want) {
				t.Fatalf("spawned %q, want %q", got, want)
			}
		})
	}
	// A chat start asking for the day's sweep alone names no trigger: the
	// job records what the sweep picks as the sweep's.
	env, p, spawned := triggerSetup(t)
	if err := os.Remove(filepath.Join(env.Dirs.State, "updates", "sweep")); err != nil {
		t.Fatal(err)
	}
	p.Event, p.Source = "SessionStart", "startup"
	invoke(t, "claude-code", p, env)
	if got, want := updateSpawns(spawned()), [][]string{{"update", "--sweep=" + env.Now().Format(time.DateOnly)}}; !reflect.DeepEqual(got, want) {
		t.Fatalf("spawned %q, want %q", got, want)
	}
}

// A sweep due or not, the trigger stays with the sessions.
func TestWithSweepKeepsTheTrigger(t *testing.T) {
	env, _, _ := triggerSetup(t)
	args := []string{"update", "--session", "codex:a", "--trigger", "chat_start", sweepFlag}
	if got, want := withSweep(env, args, env.Now()), []string{"update", "--session", "codex:a", "--trigger", "chat_start"}; !slices.Equal(got, want) {
		t.Fatalf("sweep taken: %q, want %q", got, want)
	}
	if err := os.Remove(filepath.Join(env.Dirs.State, "updates", "sweep")); err != nil {
		t.Fatal(err)
	}
	day := env.Now().Format(time.DateOnly)
	if got, want := withSweep(env, args, env.Now()), []string{"update", "--session", "codex:a", "--trigger", "chat_start", "--sweep=" + day}; !slices.Equal(got, want) {
		t.Fatalf("sweep due: %q, want %q", got, want)
	}
}

// fileInfo is a stand-in for what a stat found.
type fileInfo struct {
	os.FileInfo
	mod time.Time
}

func (f fileInfo) ModTime() time.Time { return f.mod }

// The hook passes the trigger only to the build it runs: the very file it
// was started from, written at least a minute before it started. A file
// written later may be any release, put there by an upgrade or a downgrade
// while the hook ran, and one the hook wasn't started from may be too.
func TestSameBuild(t *testing.T) {
	dir := t.TempDir()
	binary, other := filepath.Join(dir, "eva-md"), filepath.Join(dir, "other")
	for _, f := range []string{binary, other} {
		if err := os.WriteFile(f, []byte("#!/bin/sh\n"), 0o700); err != nil {
			t.Fatal(err)
		}
	}
	info, _ := os.Lstat(binary)
	self, _ := os.Stat(binary)
	otherInfo, _ := os.Stat(other)
	start := time.Date(2026, 10, 29, 15, 4, 5, 0, time.UTC)
	for _, tc := range []struct {
		name    string
		written time.Time
		self    os.FileInfo
		want    bool
	}{
		{"written long before", start.Add(-time.Hour), self, true},
		{"written just over a minute before", start.Add(-settled - time.Nanosecond), self, true},
		{"written a minute before", start.Add(-settled), self, false},
		{"written as the hook started", start, self, false},
		{"written after", start.Add(time.Second), self, false},
		{"another file", start.Add(-time.Hour), otherInfo, false},
		{"no file of its own", start.Add(-time.Hour), nil, false},
	} {
		got := sameBuild(fileInfo{info, tc.written}, tc.self, start)
		if got != tc.want {
			t.Errorf("%s: %v, want %v", tc.name, got, tc.want)
		}
	}
}

func TestWithoutTrigger(t *testing.T) {
	for _, tc := range []struct{ in, want []string }{
		{[]string{"update", "--session", "codex:a", "--trigger", "chat_start", "--sweep=2026-10-29"}, []string{"update", "--session", "codex:a", "--sweep=2026-10-29"}},
		{[]string{"update", "--session", "codex:a", "--trigger", "session_end"}, []string{"update", "--session", "codex:a"}},
		{[]string{"update", "--sweep=2026-10-29"}, []string{"update", "--sweep=2026-10-29"}},
		{[]string{"maintain"}, []string{"maintain"}},
		{[]string{"update", "--trigger"}, []string{"update", "--trigger"}},
	} {
		in := slices.Clone(tc.in)
		if got := withoutTrigger(tc.in); !slices.Equal(got, tc.want) || !slices.Equal(tc.in, in) {
			t.Errorf("withoutTrigger(%q) = %q, want %q, and the args left alone", in, got, tc.want)
		}
	}
}

// oldBuild stands in for the installed binary of 2026.11.5 to 2026.11.7,
// whose update refuses any flag it doesn't define, --trigger among them,
// with a usage line and status 2, and otherwise records its arguments.
const oldBuild = `#!/bin/sh
out=$HOME/old-build
mkdir -p "$out"
shift
for a in "$@"; do
	case "$a" in
	--session|--pending|--sweep|--sweep=*|--dry-run|--foreground|*:*) ;;
	-*) echo "eva-md: invalid arguments for update" >&2; echo refused > "$out/refused.tmp"; mv "$out/refused.tmp" "$out/refused"; exit 2 ;;
	esac
done
printf '%s\n' update "$@" > "$out/args.tmp" && mv "$out/args.tmp" "$out/args"
`

// A new hook that finds an older release installed, as when a downgrade
// replaced the binary while it ran, starts that release's update without
// the trigger, which it would refuse, so the update runs. The hook here is
// the test binary, not the installed file, which is the same case.
func TestAnOlderReleaseGetsItsUpdate(t *testing.T) {
	env, _ := setup(t)
	binary := filepath.Join(env.Dirs.Share, "bin", "eva-md")
	if err := os.MkdirAll(filepath.Dir(binary), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(binary, []byte(oldBuild), 0o700); err != nil {
		t.Fatal(err)
	}
	long := time.Now().Add(-time.Hour)
	if err := os.Chtimes(binary, long, long); err != nil {
		t.Fatal(err)
	}
	out := filepath.Join(os.Getenv("HOME"), "old-build")
	// The stand-in does refuse the flag, as the old release does.
	cmd := exec.Command(binary, "update", "--session", "codex:test-session", "--trigger", "session_end")
	if err := cmd.Run(); err == nil {
		t.Fatal("the stand-in took --trigger")
	}
	if err := os.Remove(filepath.Join(out, "refused")); err != nil {
		t.Fatal(err)
	}
	if err := detach(env.Dirs, time.Now(), "update", "--session", "codex:test-session", "--trigger", "session_end"); err != nil {
		t.Fatal(err)
	}
	args, ok := waitFor(filepath.Join(out, "args"), hangGuard)
	if !ok {
		refused, _ := os.ReadFile(filepath.Join(out, "refused"))
		t.Fatalf("the older release ran no update in %v (refused: %q)", hangGuard, refused)
	}
	if string(args) != "update\n--session\ncodex:test-session\n" {
		t.Fatalf("args %q", args)
	}
}

// The real spawn from a hook started as the installed binary itself: the
// job gets the trigger once the binary was written a minute or more before
// the hook started, and not while the binary is newer than that, as one
// an upgrade put in place while the hook ran would be. The test binary,
// copied in as the installed one, is both the hook and, through init, the
// job.
func TestTheInstalledBuildGetsTheTrigger(t *testing.T) {
	env, p := setup(t)
	home := os.Getenv("HOME")
	empty := filepath.Join(t.TempDir(), "bin")
	if err := os.Mkdir(empty, 0o700); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", empty+":/usr/bin:/bin")
	binary := filepath.Join(env.Dirs.Share, "bin", "eva-md")
	if err := os.MkdirAll(filepath.Dir(binary), 0o700); err != nil {
		t.Fatal(err)
	}
	exe, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	src, err := os.Open(exe)
	if err != nil {
		t.Fatal(err)
	}
	dst, err := os.OpenFile(binary, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o700)
	if err != nil {
		t.Fatal(err)
	}
	_, err = io.Copy(dst, src)
	src.Close()
	if cerr := dst.Close(); err != nil || cerr != nil {
		t.Fatal(err, cerr)
	}
	p.Event = "SessionEnd"
	payload, _ := json.Marshal(p)
	hook := func() string {
		t.Helper()
		_ = os.Remove(filepath.Join(home, newBuildArgs))
		cmd := exec.Command(binary)
		cmd.Env = append(os.Environ(), hookProcess+"=1")
		cmd.Stdin = bytes.NewReader(payload)
		if out, err := cmd.CombinedOutput(); err != nil || len(out) > 0 {
			t.Fatalf("hook: %v %q", err, out)
		}
		args, ok := waitFor(filepath.Join(home, newBuildArgs), hangGuard)
		if !ok {
			t.Fatalf("the hook started no job in %v: %s", hangGuard, hookRecord(env, nil))
		}
		return string(args)
	}
	if got, want := hook(), "update\n--session\nclaude-code:test-session\n"; got != want {
		t.Fatalf("a binary written just now: args %q, want %q", got, want)
	}
	long := time.Now().Add(-time.Hour)
	if err := os.Chtimes(binary, long, long); err != nil {
		t.Fatal(err)
	}
	if got, want := hook(), "update\n--session\nclaude-code:test-session\n--trigger\nsession_end\n"; got != want {
		t.Fatalf("a binary written an hour ago: args %q, want %q", got, want)
	}
}
