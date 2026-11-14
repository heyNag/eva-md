package drive

import (
	"bytes"
	"errors"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"testing"

	"github.com/heyNag/eva-md/internal/eval/continuation/drive/fakehost"
	"github.com/heyNag/eva-md/internal/eval/results"
)

// tracedWord is a made-up word the fake prints, in its answer and on
// stderr, that only a trace may hold.
const tracedWord = "madeupTRACEDword7Q"

// traceRig is a rig whose fake prints a line on stderr and tracedWord in
// each answer, keeps what each call printed, and whose run keeps a trace
// in a folder of its own, not yet made.
func traceRig(t *testing.T) *rig {
	t.Helper()
	r := newRig(t, 1)
	r.script.Printed = true
	r.script.Stderr = "made-up warning " + tracedWord + "\n"
	r.script.Say = tracedWord
	r.opts.Trace = filepath.Join(r.base, "trace")
	return r
}

// rigGitDir is the git folder of the rig's repository, relative to the
// rig's base.
var rigGitDir = filepath.Join("repo", ".git")

// tree lists the files under dir, as paths relative to it, with their
// modes, folders included. It does not go into repo/.git, the git folder
// of the rig's repository when dir is the rig's base: what is in there is
// git's, and git may make and remove files in it at any moment, which
// would fail the walk.
func tree(t *testing.T, dir string) map[string]fs.FileMode {
	t.Helper()
	got := map[string]fs.FileMode{}
	err := filepath.WalkDir(dir, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel, err := filepath.Rel(dir, p)
		if err != nil {
			return err
		}
		if d.IsDir() && rel == rigGitDir {
			return filepath.SkipDir
		}
		info, err := d.Info()
		if err != nil {
			return err
		}
		got[rel] = info.Mode()
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	return got
}

// On each host, a trace keeps each call's stdout and stderr exactly as the
// fake printed them, the version's and each chat's, of each trial, and how
// each call ended, an exit of 0 here, in files named by run, task,
// condition, trial and call, and nothing else;
// its folders are its user's alone, and so are its files. The plan says a
// trace is kept.
func TestTraceKeepsEachCall(t *testing.T) {
	for _, host := range []string{"claude-code", "codex"} {
		t.Run(host, func(t *testing.T) {
			r := traceRig(t)
			r.opts.Host = host
			r.opts.Trials = 2
			s, out, err := r.run()
			if err != nil || s.Judged != 2 {
				t.Fatalf("run: %v, %+v\n%s", err, s, out)
			}
			if !strings.Contains(out, "; trial folders removed; each call's output kept in the trace\n") {
				t.Fatalf("the plan does not say a trace is kept:\n%s", out)
			}
			calls := r.calls()
			if len(calls) != 6 {
				t.Fatalf("%d calls, want a version, a start and a follow-up for each trial", len(calls))
			}
			want := map[string]fs.FileMode{".": fs.ModeDir | 0o700}
			for _, dir := range []string{s.Run, filepath.Join(s.Run, fakehost.FixtureID), filepath.Join(s.Run, fakehost.FixtureID, "off")} {
				want[dir] = fs.ModeDir | 0o700
			}
			trial, k := 0, 0
			for i, c := range calls {
				kind := "chat"
				if c.Mode == "version" {
					kind = "version"
					trial, k = trial+1, 0
					want[filepath.Join(s.Run, fakehost.FixtureID, "off", "t"+strconv.Itoa(trial))] = fs.ModeDir | 0o700
				}
				k++
				name := filepath.Join(s.Run, fakehost.FixtureID, "off", "t"+strconv.Itoa(trial), strconv.Itoa(k)+"-"+kind)
				printed, printedErr, err := fakehost.Printed(r.script.Dir, i+1)
				if err != nil {
					t.Fatal(err)
				}
				if len(printed) == 0 || string(printedErr) != r.script.Stderr {
					t.Fatalf("call %d printed %q and %q", i+1, printed, printedErr)
				}
				for ext, body := range map[string][]byte{".stdout": printed, ".stderr": printedErr} {
					got, err := os.ReadFile(filepath.Join(r.opts.Trace, name+ext))
					if err != nil || !bytes.Equal(got, body) {
						t.Fatalf("%s holds %q, want %q: %v", name+ext, got, body, err)
					}
					want[name+ext] = 0o600
				}
				if got, err := os.ReadFile(filepath.Join(r.opts.Trace, name+".exit")); err != nil || string(got) != "exit 0\n" {
					t.Fatalf("%s.exit holds %q: %v", name, got, err)
				}
				want[name+".exit"] = 0o600
			}
			if got := tree(t, r.opts.Trace); !mapsEqual(got, want) {
				t.Fatalf("the trace holds %v, want %v", got, want)
			}
		})
	}
}

func mapsEqual(a, b map[string]fs.FileMode) bool {
	if len(a) != len(b) {
		return false
	}
	for k, v := range a {
		if w, ok := b[k]; !ok || w != v {
			return false
		}
	}
	return true
}

// An empty trace folder that was there is closed to all but its owner.
func TestAnEmptyTraceFolderIsClosed(t *testing.T) {
	r := traceRig(t)
	if err := os.Mkdir(r.opts.Trace, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(r.opts.Trace, 0o755); err != nil {
		t.Fatal(err)
	}
	if s, out, err := r.run(); err != nil || s.Judged != 1 {
		t.Fatalf("run: %v, %+v\n%s", err, s, out)
	}
	if info, err := os.Stat(r.opts.Trace); err != nil || info.Mode().Perm() != 0o700 {
		t.Fatalf("the trace folder: %v, %v", info.Mode(), err)
	}
}

// A trace that is cut at the output's limit holds what the driver read:
// the limit's bytes of stdout, and as many of stderr.
func TestTraceKeepsWhatTheLimitKeeps(t *testing.T) {
	r := traceRig(t)
	r.script.Pad = 4096
	r.script.Stderr = strings.Repeat("e", 3000)
	r.opts.outputLimit = 1024
	s, _, err := r.run()
	var stopped *Stopped
	if !errors.As(err, &stopped) || stopped.Code != CodeNoUsage {
		t.Fatalf("run: %v", err)
	}
	printed, _, err := fakehost.Printed(r.script.Dir, 2)
	if err != nil {
		t.Fatal(err)
	}
	name := filepath.Join(r.opts.Trace, s.Run, fakehost.FixtureID, "off", "t1", "2-chat")
	got, err := os.ReadFile(name + ".stdout")
	if err != nil || !bytes.Equal(got, printed[:1024]) {
		t.Fatalf("the trace holds %d bytes of stdout: %v", len(got), err)
	}
	if got, err := os.ReadFile(name + ".stderr"); err != nil || string(got) != r.script.Stderr[:1024] {
		t.Fatalf("the trace holds %d bytes of stderr: %v", len(got), err)
	}
}

// What the fake prints is in the trace, and in no row, record, output or
// error, nor anywhere else in the rig.
func TestATracedWordGoesNowhereElse(t *testing.T) {
	r := traceRig(t)
	s, out, err := r.run()
	if err != nil || s.Judged != 1 {
		t.Fatalf("run: %v, %+v\n%s", err, s, out)
	}
	if strings.Contains(out, tracedWord) {
		t.Fatalf("the output holds the word:\n%s", out)
	}
	stdout, err := os.ReadFile(filepath.Join(r.opts.Trace, s.Run, fakehost.FixtureID, "off", "t1", "2-chat.stdout"))
	if err != nil || !strings.Contains(string(stdout), tracedWord) {
		t.Fatalf("the trace's stdout does not hold the word: %v", err)
	}
	stderr, err := os.ReadFile(filepath.Join(r.opts.Trace, s.Run, fakehost.FixtureID, "off", "t1", "1-version.stderr"))
	if err != nil || !strings.Contains(string(stderr), tracedWord) {
		t.Fatalf("the trace's stderr does not hold the word: %v", err)
	}
	if len(r.rows()) == 0 {
		t.Fatal("no row")
	}
	if found := holding(t, r.base, tracedWord, r.opts.Trace, r.script.Dir, r.file, filepath.Join(r.base, rigGitDir)); len(found) != 0 {
		t.Fatalf("the word is in %v", found)
	}
}

// holding lists the files under dir that hold word, but those under the
// paths skipped. A caller that walks the rig's base skips the rig's
// repository's .git, which git changes at any moment, as tree does.
func holding(t *testing.T, dir, word string, skip ...string) []string {
	t.Helper()
	var found []string
	err := filepath.WalkDir(dir, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if slices.Contains(skip, p) {
			if d.IsDir() {
				return filepath.SkipDir
			}
			return nil
		}
		if d.Type().IsRegular() {
			data, err := os.ReadFile(p)
			if err != nil {
				return err
			}
			if bytes.Contains(data, []byte(word)) {
				found = append(found, p)
			}
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	return found
}

// Without --trace, what a call printed is kept nowhere: the word the fake
// prints is in no file of the rig but the fake's own, and nothing in it is
// named as a trace's file.
func TestNoTraceWithoutTheFlag(t *testing.T) {
	r := traceRig(t)
	r.opts.Trace = ""
	s, out, err := r.run()
	if err != nil || s.Judged != 1 {
		t.Fatalf("run: %v, %+v\n%s", err, s, out)
	}
	if strings.Contains(out, "trace") {
		t.Fatalf("the plan names a trace:\n%s", out)
	}
	if found := holding(t, r.base, tracedWord, r.script.Dir, r.file, filepath.Join(r.base, rigGitDir)); len(found) != 0 {
		t.Fatalf("the word is in %v", found)
	}
	for p := range tree(t, r.base) {
		if strings.HasSuffix(p, ".stdout") || strings.HasSuffix(p, ".stderr") || strings.HasSuffix(p, ".exit") {
			if !strings.HasPrefix(p, filepath.Base(r.script.Dir)+string(filepath.Separator)) {
				t.Fatalf("%s was made", p)
			}
		}
	}
	if _, err := os.Lstat(filepath.Join(r.base, "trace")); !errors.Is(err, fs.ErrNotExist) {
		t.Fatalf("a trace was made: %v", err)
	}
}

// A trace folder that is not new or empty, or that lies inside or holds
// --work, --out, --tasks or --repos, by name or through a link, or lies in
// the git checkout of --repo or of the folder the driver is run from, is
// refused before anything starts, and nothing is made.
func TestTraceFoldersRefused(t *testing.T) {
	for name, c := range map[string]struct {
		set  func(t *testing.T, r *rig) // sets r.opts.Trace, and what the case needs
		want string
	}{
		"not empty": {func(t *testing.T, r *rig) {
			write(t, filepath.Join(r.opts.Trace, "left"), "made up\n")
		}, "--trace is not empty"},
		"a file": {func(t *testing.T, r *rig) {
			write(t, r.opts.Trace, "made up\n")
		}, "--trace is not a folder that can be read"},
		"a link to nothing": {func(t *testing.T, r *rig) {
			if err := os.Symlink(filepath.Join(r.base, "none"), r.opts.Trace); err != nil {
				t.Fatal(err)
			}
		}, "--trace is a link to nothing"},
		"inside --work": {func(_ *testing.T, r *rig) {
			r.opts.Trace = filepath.Join(r.opts.Work, "trace")
		}, "--trace overlaps --work"},
		"holding --work": {func(_ *testing.T, r *rig) {
			r.opts.Trace = filepath.Join(r.base, "both")
			r.opts.Work = filepath.Join(r.opts.Trace, "work")
		}, "--trace overlaps --work"},
		"inside --out": {func(_ *testing.T, r *rig) {
			r.opts.Trace = filepath.Join(r.opts.Out, "trace")
		}, "--trace overlaps --out"},
		"holding --out": {func(_ *testing.T, r *rig) {
			r.opts.Trace = filepath.Join(r.base, "both")
			r.opts.Out = filepath.Join(r.opts.Trace, "out")
		}, "--trace overlaps --out"},
		"inside --out through a link": {func(t *testing.T, r *rig) {
			mkdir(t, r.opts.Out)
			if err := os.Symlink(r.opts.Out, filepath.Join(r.base, "alias")); err != nil {
				t.Fatal(err)
			}
			r.opts.Trace = filepath.Join(r.base, "alias", "trace")
		}, "--trace overlaps --out"},
		"inside --tasks": {func(_ *testing.T, r *rig) {
			r.opts.Trace = filepath.Join(r.opts.Tasks, "made-up-trace")
		}, "--trace overlaps --tasks"},
		"inside --repos": {func(_ *testing.T, r *rig) {
			r.opts.Trace = filepath.Join(r.opts.Repos, "made-up-trace")
		}, "--trace overlaps --repos"},
		"in the checkout of --repo": {func(_ *testing.T, r *rig) {
			r.opts.Trace = filepath.Join(r.opts.Repo, "made-up", "trace")
		}, "--trace is in a git checkout"},
		"in the checkout it is run from": {func(t *testing.T, r *rig) {
			other := filepath.Join(r.base, "other")
			mkdir(t, filepath.Join(other, ".git"))
			t.Chdir(filepath.Join(other))
			r.opts.Trace = filepath.Join(other, "made-up", "trace")
		}, "--trace is in a git checkout"},
	} {
		t.Run(name, func(t *testing.T) {
			r := traceRig(t)
			c.set(t, r)
			before := tree(t, r.base)
			_, out, err := r.run()
			var usage *UsageError
			if !errors.As(err, &usage) || !strings.Contains(err.Error(), c.want) {
				t.Fatalf("run: %v, want an error saying %q\n%s", err, c.want, out)
			}
			if calls := r.calls(); len(calls) != 0 {
				t.Fatalf("the fake ran: %+v", calls)
			}
			// The run writes the fake's script; tree leaves out the rig's
			// repository's .git, which is git's.
			after := tree(t, r.base)
			for _, m := range []map[string]fs.FileMode{before, after} {
				delete(m, "script.json")
			}
			if !mapsEqual(before, after) {
				t.Fatalf("the run made or changed %v, from %v", after, before)
			}
		})
	}
}

// A path is inside a folder by name, through a link, or by the folder a
// name reaches, and not inside a folder beside it or one that is not there.
func TestInsideFolder(t *testing.T) {
	base, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	dir := filepath.Join(base, "dir")
	mkdir(t, filepath.Join(dir, "sub"))
	mkdir(t, filepath.Join(base, "dirx"))
	alias := filepath.Join(base, "alias")
	if err := os.Symlink(dir, alias); err != nil {
		t.Fatal(err)
	}
	for _, p := range []string{dir, filepath.Join(dir, "sub"), filepath.Join(dir, "new", "trace"), alias, filepath.Join(alias, "new", "trace")} {
		if !insideFolder(p, dir) || !insideFolder(p, alias) {
			t.Errorf("%s is not inside the folder", p)
		}
	}
	for _, p := range []string{base, filepath.Join(base, "dirx", "trace"), filepath.Join(base, "none", "trace")} {
		if insideFolder(p, dir) {
			t.Errorf("%s is inside the folder", p)
		}
	}
	if insideFolder(filepath.Join(base, "trace"), filepath.Join(base, "none")) {
		t.Error("a path is inside a folder that is not there")
	}
	// On a file system that ignores letter case, the folder spelt in other
	// letters is the folder itself, which a comparison of names alone
	// misses.
	other := filepath.Join(base, "DIR")
	a, errA := os.Stat(other)
	b, errB := os.Stat(dir)
	oneFolder := errA == nil && errB == nil && os.SameFile(a, b)
	if got := insideFolder(filepath.Join(other, "trace"), dir); got != oneFolder {
		t.Errorf("a path through the folder spelt in other letters is inside it %v, and they are one folder %v", got, oneFolder)
	}
}

// traceFailure is a trace file that fails as a test says.
type traceFailure struct {
	f                   *os.File
	failWrite, failShut bool
}

func (w *traceFailure) Write(p []byte) (int, error) {
	if w.failWrite {
		return 0, &os.PathError{Op: "write", Path: w.f.Name(), Err: errors.New("made-up write failure")}
	}
	return w.f.Write(p)
}

func (w *traceFailure) Close() error {
	err := w.f.Close()
	if w.failShut {
		return &os.PathError{Op: "close", Path: w.f.Name(), Err: errors.New("made-up close failure")}
	}
	return err
}

// A trace that cannot be written, opened or closed stops the run before
// its next call: the run errs, its record ends in error, the trial writes
// no row and its folder goes, the call's tokens are counted, and the error
// names no path and nothing the fake printed.
func TestATraceThatCannotBeWrittenStopsTheRun(t *testing.T) {
	for name, c := range map[string]struct {
		file  string // the file of the trial t1 that fails
		how   string // "open", "write" or "close"
		calls int    // the calls made
	}{
		"opening the version's":     {"1-version.stdout", "open", 0},
		"writing the version's":     {"1-version.stdout", "write", 1},
		"opening a chat's":          {"2-chat.stderr", "open", 1},
		"writing a chat's":          {"2-chat.stdout", "write", 2},
		"closing a follow-up's":     {"3-chat.stderr", "close", 3},
		"opening a version's end":   {"1-version.exit", "open", 0},
		"writing a chat's end":      {"2-chat.exit", "write", 2},
		"closing a follow-up's end": {"3-chat.exit", "close", 3},
	} {
		t.Run(name, func(t *testing.T) {
			r := traceRig(t)
			r.opts.Trials = 2
			r.opts.openTrace = func(path string) (io.WriteCloser, error) {
				hit := filepath.Base(path) == c.file && filepath.Base(filepath.Dir(path)) == "t1"
				if hit && c.how == "open" {
					return nil, &os.PathError{Op: "open", Path: path, Err: errors.New("made-up open failure")}
				}
				f, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
				if err != nil {
					return nil, err
				}
				return &traceFailure{f: f, failWrite: hit && c.how == "write", failShut: hit && c.how == "close"}, nil
			}
			s, out, err := r.run()
			var stopped *Stopped
			var usage *UsageError
			if err == nil || errors.As(err, &stopped) || errors.As(err, &usage) || !strings.Contains(err.Error(), "the trace: ") {
				t.Fatalf("run: %v", err)
			}
			if strings.Contains(err.Error(), r.base) || strings.Contains(err.Error(), tracedWord) || strings.Contains(out, tracedWord) {
				t.Fatalf("the error or output holds a path or the word: %v\n%s", err, out)
			}
			if got := len(r.calls()); got != c.calls {
				t.Fatalf("%d calls, want %d", got, c.calls)
			}
			if rows := r.rows(); len(rows) != 0 {
				t.Fatalf("%d rows", len(rows))
			}
			if entries, err := os.ReadDir(r.opts.Work); err != nil || len(entries) != 0 {
				t.Fatalf("the work folder holds %v: %v", entries, err)
			}
			records, err := LoadRuns(r.opts.Out)
			if err != nil || len(records) != 1 || records[0].Ended != EndedError || records[0].NotRun != 1 {
				t.Fatalf("records %+v: %v", records, err)
			}
			// A start's tokens are counted, even the one whose trace failed.
			if want := int64(1050 * (c.calls - 1)); c.calls > 0 && (s.Tokens != max(want, 0) || records[0].Tokens != s.Tokens) {
				t.Fatalf("tokens %d, record %d, want %d", s.Tokens, records[0].Tokens, want)
			}
			if data, err := os.ReadFile(filepath.Join(r.opts.Out, RunsFile)); err != nil || bytes.Contains(data, []byte(tracedWord)) {
				t.Fatalf("the record: %v", err)
			}
			if _, err := os.Stat(filepath.Join(r.opts.Out, results.FileName)); !errors.Is(err, fs.ErrNotExist) {
				t.Fatalf("a rows file: %v", err)
			}
		})
	}
}

// The rig's repository starts no background work of git's, and the walks
// that read the rig's base do not go into its .git: git may make and
// remove files there at any moment (CI's macOS job once failed a walk on
// a lock file of git's maintenance that had gone), so what is in there
// may decide nothing. A folder in .git no walk may read stands in for a
// file that goes while the walk lists it: a walk that goes in fails.
func TestTheRigsGitIsLeftAlone(t *testing.T) {
	r := traceRig(t)
	for key, want := range map[string]string{"maintenance.auto": "false", "gc.auto": "0"} {
		if got := git(t, r.opts.Repo, "config", "--get", key); got != want {
			t.Errorf("the rig's repository reads %s = %q, want %q", key, got, want)
		}
	}
	shut := filepath.Join(r.base, rigGitDir, "made-up-shut")
	mkdir(t, shut)
	if err := os.Chmod(shut, 0); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.Chmod(shut, 0o755) })
	if _, err := os.ReadDir(shut); err == nil {
		t.Fatal("the shut folder can be read, so it shows nothing (is the test run as root?)")
	}
	got := tree(t, r.base)
	if _, ok := got["repo"]; !ok {
		t.Fatalf("the walk missed the repository: %v", got)
	}
	for p := range got {
		if p == rigGitDir || strings.HasPrefix(p, rigGitDir+string(filepath.Separator)) {
			t.Fatalf("the walk went into the repository's .git: %s", p)
		}
	}
	if found := holding(t, r.base, tracedWord, r.script.Dir, r.file, filepath.Join(r.base, rigGitDir)); len(found) != 0 {
		t.Fatalf("the word is in %v", found)
	}
}
