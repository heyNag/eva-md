package wikigit_test

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"slices"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/heyNag/eva-md/internal/config"
	"github.com/heyNag/eva-md/internal/paths"
	"github.com/heyNag/eva-md/internal/redact"
	"github.com/heyNag/eva-md/internal/wikigit"
	"github.com/heyNag/eva-md/internal/wikiinit"
)

// day is the run's local date in every test, 2026-10-31.
var day = time.Date(2026, 10, 31, 9, 30, 0, 0, time.Local)

type fixture struct {
	home, wiki, remote string
	o                  wikigit.Options
}

// newFixture makes a wiki as setup does, with wikiinit, in a temporary home
// whose git config is a temporary file, pushed to a bare remote there.
func newFixture(t *testing.T) *fixture {
	t.Helper()
	home, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	t.Setenv("HOME", home)
	t.Setenv("GIT_CONFIG_GLOBAL", filepath.Join(home, "gitconfig"))
	t.Setenv("GIT_CONFIG_NOSYSTEM", "1")
	for _, name := range []string{"GIT_CONFIG_COUNT", "GIT_CONFIG_PARAMETERS", "GIT_DIR", "GIT_WORK_TREE", "GIT_INDEX_FILE", "GIT_COMMON_DIR", "GIT_TEMPLATE_DIR"} {
		t.Setenv(name, "")
	}
	if err := os.WriteFile(filepath.Join(home, "gitconfig"), []byte("[user]\nname = Eva Fixture\nemail = fixture@example.invalid\n[commit]\ngpgsign = false\n[init]\ndefaultBranch = main\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	dirs, err := paths.Default()
	if err != nil {
		t.Fatal(err)
	}
	f := &fixture{home: home, wiki: filepath.Join(home, "wiki"), remote: filepath.Join(home, "remote.git")}
	git(t, home, "init", "--bare", "--quiet", f.remote)
	if _, err := wikiinit.Run(context.Background(), wikiinit.Options{Remote: f.remote, Dir: f.wiki, Dirs: dirs, Version: "test", OKFModule: "v0.7.2"}); err != nil {
		t.Fatal(err)
	}
	c, err := config.Load(dirs.ConfigFile())
	if err != nil {
		t.Fatal(err)
	}
	f.o = wikigit.Options{Config: c, Dirs: dirs, Now: func() time.Time { return day }}
	return f
}

func git(t *testing.T, dir string, args ...string) string {
	t.Helper()
	out, err := wikigit.Run(context.Background(), time.Minute, dir, args...)
	if err != nil {
		t.Fatalf("git %s: %v", args[0], err)
	}
	return out
}

// write writes a file in the wiki, creating its folder.
func (f *fixture) write(t *testing.T, rel, text string) {
	t.Helper()
	file := filepath.Join(f.wiki, filepath.FromSlash(rel))
	if err := os.MkdirAll(filepath.Dir(file), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(file, []byte(text), 0o644); err != nil {
		t.Fatal(err)
	}
}

// edit appends a Timeline line to the default thread page.
func (f *fixture) edit(t *testing.T, line string) {
	t.Helper()
	file := filepath.Join(f.wiki, "general", "threads", "default.md")
	b, err := os.ReadFile(file)
	if err != nil {
		t.Fatal(err)
	}
	f.write(t, "general/threads/default.md", string(b)+"### 2026-10-31\n- "+line+"\n")
}

func (f *fixture) commit(t *testing.T) wikigit.Result {
	t.Helper()
	r, err := wikigit.Commit(context.Background(), f.o)
	if err != nil {
		t.Fatal(err)
	}
	return r
}

// records reads this month's commit records.
func (f *fixture) records(t *testing.T) []wikigit.Result {
	t.Helper()
	b, err := os.ReadFile(filepath.Join(f.o.Dirs.State, "commits", "2026-10.jsonl"))
	if err != nil {
		t.Fatal(err)
	}
	var out []wikigit.Result
	s := bufio.NewScanner(bytes.NewReader(b))
	for s.Scan() {
		var r wikigit.Result
		if err := json.Unmarshal(s.Bytes(), &r); err != nil {
			t.Fatal(err)
		}
		out = append(out, r)
	}
	return out
}

// committedFiles lists the files HEAD's commit changed.
func (f *fixture) committedFiles(t *testing.T) []string {
	t.Helper()
	return strings.Fields(git(t, f.wiki, "show", "--name-only", "--format=", "HEAD"))
}

// configFiles reads every git config file a run could change.
func (f *fixture) configFiles(t *testing.T) map[string]string {
	t.Helper()
	files := map[string]string{}
	for _, file := range []string{filepath.Join(f.home, "gitconfig"), filepath.Join(f.wiki, ".git", "config"), filepath.Join(f.remote, "config")} {
		b, err := os.ReadFile(file)
		if err != nil {
			t.Fatal(err)
		}
		files[file] = string(b)
	}
	return files
}

// A changed page is committed with the day's message and pushed, the record
// says so, and a second run has nothing to do. No git config file changes.
func TestCommitAndPush(t *testing.T) {
	f := newFixture(t)
	before := f.configFiles(t)
	f.edit(t, "done [stated]: Wrote the committer s:3f2a91c0#2")
	r := f.commit(t)
	if r.Outcome != wikigit.Committed || r.Reason != "" || r.Push != wikigit.PushOK || r.Pages != 1 || len(r.Unstaged) != 0 || r.ValidateErrors != 0 {
		t.Fatalf("result %+v", r)
	}
	if msg := git(t, f.wiki, "log", "-1", "--format=%s"); msg != "eva-md daily 2026-10-31: 1 page" {
		t.Fatalf("message %q", msg)
	}
	head := git(t, f.wiki, "rev-parse", "HEAD")
	if got := git(t, f.remote, "rev-parse", "refs/heads/main"); got != head {
		t.Fatal("the commit was not pushed")
	}
	if status := git(t, f.wiki, "status", "--porcelain"); status != "" {
		t.Fatalf("wiki left dirty: %s", status)
	}
	if r := f.commit(t); r.Outcome != wikigit.Nothing || r.Push != wikigit.PushSkipped {
		t.Fatalf("second run %+v", r)
	}
	if r, err := wikigit.Push(context.Background(), f.o); err != nil || r.Outcome != wikigit.Nothing || r.Reason != wikigit.ReasonPushOnly || r.Push != wikigit.PushUpToDate {
		t.Fatalf("push only %+v %v", r, err)
	}
	if git(t, f.wiki, "rev-parse", "HEAD") != head {
		t.Fatal("a run with nothing to do made a commit")
	}
	records := f.records(t)
	if len(records) != 3 || records[0].Outcome != wikigit.Committed || records[0].Push != wikigit.PushOK || records[0].At != day.Format(time.RFC3339) || records[1].Outcome != wikigit.Nothing || records[2].Push != wikigit.PushUpToDate {
		t.Fatalf("records %+v", records)
	}
	for name, mode := range map[string]os.FileMode{"commits": 0o700, "commits/2026-10.jsonl": 0o600} {
		if info, err := os.Stat(filepath.Join(f.o.Dirs.State, name)); err != nil || info.Mode().Perm() != mode {
			t.Errorf("%s: %v %v", name, info, err)
		}
	}
	if after := f.configFiles(t); !reflect.DeepEqual(before, after) {
		t.Fatal("a run changed a git config file")
	}
}

// Each guard refuses with its reason, makes no commit and keeps the change.
// A detached HEAD refuses a push too.
func TestGuardsRefuse(t *testing.T) {
	for _, tc := range []struct{ name, reason string }{
		{"wrong origin", "origin"}, {"wrong push URL", "origin"}, {"wrong instance", "instance"}, {"newer schema", "schema"},
		{"index.lock", "index_lock"}, {"rebase in progress", "rebase"}, {"merge in progress", "merge"},
		{"cherry-pick in progress", "cherry_pick"}, {"detached HEAD", "detached"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			f := newFixture(t)
			head := git(t, f.wiki, "rev-parse", "HEAD")
			f.edit(t, "done [stated]: A change that must wait")
			lock := filepath.Join(f.wiki, ".git", "index.lock")
			switch tc.name {
			case "wrong origin":
				git(t, f.wiki, "remote", "set-url", "origin", "git@example.invalid:other/wiki.git")
			case "wrong push URL":
				git(t, f.wiki, "remote", "set-url", "--push", "origin", "git@example.invalid:other/wiki.git")
			case "wrong instance":
				f.o.Config.Instance = "other-0000"
			case "newer schema":
				file := filepath.Join(f.wiki, ".eva-md", "schema.toml")
				schema, err := config.LoadSchema(file)
				if err != nil {
					t.Fatal(err)
				}
				schema.SchemaVersion = 2
				if err := config.SaveSchema(file, schema); err != nil {
					t.Fatal(err)
				}
			case "index.lock":
				if err := os.WriteFile(lock, nil, 0o600); err != nil {
					t.Fatal(err)
				}
				old := day.Add(-2 * time.Minute)
				if err := os.Chtimes(lock, old, old); err != nil {
					t.Fatal(err)
				}
			case "rebase in progress":
				if err := os.Mkdir(filepath.Join(f.wiki, ".git", "rebase-merge"), 0o700); err != nil {
					t.Fatal(err)
				}
			case "merge in progress":
				if err := os.WriteFile(filepath.Join(f.wiki, ".git", "MERGE_HEAD"), []byte(head+"\n"), 0o600); err != nil {
					t.Fatal(err)
				}
			case "cherry-pick in progress":
				if err := os.WriteFile(filepath.Join(f.wiki, ".git", "CHERRY_PICK_HEAD"), []byte(head+"\n"), 0o600); err != nil {
					t.Fatal(err)
				}
			case "detached HEAD":
				git(t, f.wiki, "checkout", "--quiet", "--detach")
			}
			r := f.commit(t)
			if r.Outcome != wikigit.Refused || r.Reason != tc.reason || r.Push != wikigit.PushSkipped {
				t.Fatalf("result %+v", r)
			}
			if tc.name == "index.lock" {
				if r.LockAge != 120 {
					t.Fatalf("lock age %d", r.LockAge)
				}
				if _, err := os.Stat(lock); err != nil {
					t.Fatal("the index.lock was deleted")
				}
			}
			if git(t, f.wiki, "rev-parse", "HEAD") != head {
				t.Fatal("a refused run made a commit")
			}
			if b, err := os.ReadFile(filepath.Join(f.wiki, "general", "threads", "default.md")); err != nil || !strings.Contains(string(b), "A change that must wait") {
				t.Fatal("the change was lost")
			}
			if records := f.records(t); len(records) != 1 || records[0].Reason != tc.reason {
				t.Fatalf("records %+v", records)
			}
			if tc.name == "detached HEAD" {
				if r, err := wikigit.Push(context.Background(), f.o); err != nil || r.Outcome != wikigit.Refused || r.Reason != "detached" || r.Push != wikigit.PushSkipped {
					t.Fatalf("push only %+v %v", r, err)
				}
				if git(t, f.remote, "rev-parse", "refs/heads/main") != head || git(t, f.wiki, "rev-parse", "refs/heads/main") != head {
					t.Fatal("a refused run moved a branch")
				}
			}
		})
	}
}

// JSONL files, the inbox folder, temporary files and paths outside the
// commit paths are left out, even when someone staged them, and so is a
// symlink, whose target git would commit as its content. All stay on disk.
func TestLeavesOutWhatIsNeverCommitted(t *testing.T) {
	f := newFixture(t)
	f.edit(t, "done [stated]: Kept the inbox out")
	junk := []string{"general/.tmp-eva-1a2b", "general/threads/.tmp-okf-3c4d.md", "general/threads/chat.jsonl", "inbox/chat.md", "notes.txt", "scratch/todo.md"}
	for _, rel := range junk {
		f.write(t, rel, "captured chat\n")
	}
	git(t, f.wiki, append([]string{"add", "--force", "--"}, junk...)...)
	outside := filepath.Join(f.home, "outside.md")
	if err := os.WriteFile(outside, []byte("outside the wiki\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(outside, filepath.Join(f.wiki, "general", "link.md")); err != nil {
		t.Fatal(err)
	}
	want := append([]string{"general/link.md"}, junk...)
	slices.Sort(want)
	r := f.commit(t)
	if r.Outcome != wikigit.Committed || !reflect.DeepEqual(r.Unstaged, want) {
		t.Fatalf("result %+v", r)
	}
	if files := f.committedFiles(t); !reflect.DeepEqual(files, []string{"general/threads/default.md"}) {
		t.Fatalf("committed %v", files)
	}
	if staged := git(t, f.wiki, "diff", "--cached", "--name-only"); staged != "" {
		t.Fatalf("still staged: %s", staged)
	}
	for _, rel := range want {
		if _, err := os.Lstat(filepath.Join(f.wiki, rel)); err != nil {
			t.Fatalf("%s removed", rel)
		}
	}
}

// A page holding a secret a current redact rule matches is left out and
// recorded by path, never by value, while the other pages are committed,
// including thread pages, and the area's overview, whose long, high-entropy
// branch names, one with an issue key, and refs redacting as text would
// change.
func TestSecretLeftOutIdentifiersCommitted(t *testing.T) {
	f := newFixture(t)
	secret := "ghp_" + strings.Repeat("ab12", 10)
	f.write(t, "general/threads/leak.md", "# Leak\n\nThe CI token is "+secret+".\n")
	now, err := os.ReadFile(filepath.Join(f.wiki, "general", "now.md"))
	if err != nil {
		t.Fatal(err)
	}
	overview := string(now)
	want := []string{"general/now.md", "general/threads/default.md"}
	for _, branch := range []string{"local-token-refresh-worker-c9a8c7f1e2d3b4a5", "ENG-1234-rotate-api-key-for-staging-cluster2"} {
		thread := "general/threads/" + branch + ".md"
		page := "---\ntype: thread\ntitle: \"General · " + branch + "\"\ndescription: General conversations outside a project\nstatus: stable\nbranch: \"" + branch + "\"\nproject: \"general\"\n---\n\n# General · " + branch + "\n\n## Now\n<!-- eva:now:start -->\nRotating tokens.\n\n### Next\n- Rotate the token on " + branch + " [stated] s:3f2a91c0#14 <!-- i:n1 -->\n<!-- eva:now:end -->\n\n## Timeline\n### 2026-10-31\n- decision [stated]: Rotate the token on " + branch + " s:3f2a91c0#14 s:3f2a91c0-1234-5678-9abc-def012345678#3\n"
		f.write(t, thread, page)
		line := "* [" + branch + "](threads/" + branch + ".md) — Rotating tokens. · last active 2026-10-31"
		if _, n := redact.Redact(line); n == 0 {
			t.Fatalf("redacting the overview line of %s as text replaces nothing, so the case proves nothing", branch)
		}
		overview += line + "\n"
		want = append(want, thread)
	}
	f.write(t, "general/now.md", overview)
	slices.Sort(want)
	f.edit(t, "done [stated]: Wrote the secret scan")
	r := f.commit(t)
	if r.Outcome != wikigit.Committed || !reflect.DeepEqual(r.Unstaged, []string{"general/threads/leak.md"}) || r.Pages != 4 {
		t.Fatalf("result %+v", r)
	}
	if files := f.committedFiles(t); !reflect.DeepEqual(files, want) {
		t.Fatalf("committed %v", files)
	}
	record, err := os.ReadFile(filepath.Join(f.o.Dirs.State, "commits", "2026-10.jsonl"))
	if err != nil {
		t.Fatal(err)
	}
	result, _ := json.Marshal(r)
	history := git(t, f.wiki, "log", "--all", "-p")
	for name, text := range map[string]string{"record": string(record), "result": string(result), "history": history} {
		if strings.Contains(text, secret) {
			t.Fatalf("the secret is in the %s", name)
		}
	}
	if git(t, f.wiki, "ls-files", "general/threads/leak.md") != "" {
		t.Fatal("the leaking page is tracked")
	}
}

// okf validation errors are counted, and the day's pages are committed
// anyway. A run with nothing to commit doesn't validate.
func TestValidationErrorsDoNotStopCommit(t *testing.T) {
	f := newFixture(t)
	f.write(t, "general/threads/notes.md", "Notes without frontmatter.\n")
	r := f.commit(t)
	if r.Outcome != wikigit.Committed || r.ValidateErrors == 0 || r.Push != wikigit.PushOK {
		t.Fatalf("result %+v", r)
	}
	if files := f.committedFiles(t); !slices.Contains(files, "general/threads/notes.md") {
		t.Fatalf("committed %v", files)
	}
	if r := f.commit(t); r.Outcome != wikigit.Nothing || r.ValidateErrors != 0 {
		t.Fatalf("second run %+v", r)
	}
}

// A signing program git can't start, as one a desktop app's short PATH
// doesn't reach, fails the run with its own reason.
func TestMissingSignerFails(t *testing.T) {
	f := newFixture(t)
	git(t, f.home, "config", "--global", "commit.gpgsign", "true")
	git(t, f.home, "config", "--global", "gpg.program", filepath.Join(f.home, "no-such-signer"))
	git(t, f.home, "config", "--global", "user.signingkey", "fixture")
	f.edit(t, "done [stated]: Asked for a signature")
	if r := f.commit(t); r.Outcome != wikigit.Failed || r.Reason != "commit_signer_missing" || r.Push != wikigit.PushSkipped {
		t.Fatalf("result %+v", r)
	}
}

// A declined signature fails the run with reason commit_signature after one
// attempt, pushes nothing and leaves the signing settings as they were.
func TestDeclinedSignatureFails(t *testing.T) {
	f := newFixture(t)
	signer := filepath.Join(f.home, "signer")
	if err := os.WriteFile(signer, []byte("#!/bin/sh\nprintf x >> \"$HOME/sign-attempts\"\nexit 1\n"), 0o700); err != nil {
		t.Fatal(err)
	}
	git(t, f.home, "config", "--global", "commit.gpgsign", "true")
	git(t, f.home, "config", "--global", "gpg.program", signer)
	git(t, f.home, "config", "--global", "user.signingkey", "fixture")
	before := f.configFiles(t)
	head := git(t, f.wiki, "rev-parse", "HEAD")
	f.edit(t, "done [stated]: Asked for a signature")
	r := f.commit(t)
	if r.Outcome != wikigit.Failed || r.Reason != "commit_signature" || r.Push != wikigit.PushSkipped || r.Pages != 1 {
		t.Fatalf("result %+v", r)
	}
	if b, err := os.ReadFile(filepath.Join(f.home, "sign-attempts")); err != nil || string(b) != "x" {
		t.Fatalf("attempts %q %v", b, err)
	}
	if git(t, f.wiki, "rev-parse", "HEAD") != head {
		t.Fatal("an unsigned commit was made")
	}
	if after := f.configFiles(t); !reflect.DeepEqual(before, after) {
		t.Fatal("the run changed a git config file")
	}
}

// A remote with a commit this clone lacks gives diverged, and its history
// is left as it was: no pull, no force.
func TestDivergedRemoteIsLeftAlone(t *testing.T) {
	f := newFixture(t)
	other := filepath.Join(f.home, "other")
	git(t, f.home, "clone", "--quiet", f.remote, other)
	if err := os.WriteFile(filepath.Join(other, "notes.md"), []byte("edited by hand elsewhere\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	git(t, other, "add", "notes.md")
	git(t, other, "commit", "--quiet", "-m", "A hand edit")
	git(t, other, "push", "--quiet", "origin", "main")
	remoteHead := git(t, f.remote, "rev-parse", "refs/heads/main")
	remoteLog := git(t, f.remote, "log", "--format=%H", "refs/heads/main")
	f.edit(t, "done [stated]: Wrote the diverged case")
	r := f.commit(t)
	if r.Outcome != wikigit.Committed || r.Push != wikigit.PushDiverged {
		t.Fatalf("result %+v", r)
	}
	if git(t, f.remote, "rev-parse", "refs/heads/main") != remoteHead || git(t, f.remote, "log", "--format=%H", "refs/heads/main") != remoteLog {
		t.Fatal("the remote's history changed")
	}
	if _, err := os.Stat(filepath.Join(f.wiki, "notes.md")); err == nil {
		t.Fatal("the run pulled the remote's commit")
	}
}

// A push to a remote that is away, as on a volume not mounted, is pending,
// not refused; a later run with nothing to commit tries it again, and
// --push-only pushes the commit once the remote is back.
func TestPendingPushThenPushOnly(t *testing.T) {
	f := newFixture(t)
	away := f.remote + ".away"
	if err := os.Rename(f.remote, away); err != nil {
		t.Fatal(err)
	}
	f.edit(t, "done [stated]: Committed while the remote was away")
	r := f.commit(t)
	if r.Outcome != wikigit.Committed || r.Push != wikigit.PushPending {
		t.Fatalf("result %+v", r)
	}
	head := git(t, f.wiki, "rev-parse", "HEAD")
	if r := f.commit(t); r.Outcome != wikigit.Nothing || r.Push != wikigit.PushPending {
		t.Fatalf("run with nothing to commit %+v", r)
	}
	if err := os.Rename(away, f.remote); err != nil {
		t.Fatal(err)
	}
	r, err := wikigit.Push(context.Background(), f.o)
	if err != nil || r.Outcome != wikigit.Nothing || r.Reason != wikigit.ReasonPushOnly || r.Push != wikigit.PushOK {
		t.Fatalf("push only %+v %v", r, err)
	}
	if got := git(t, f.remote, "rev-parse", "refs/heads/main"); got != head {
		t.Fatal("the earlier commit was not pushed")
	}
	if r := f.commit(t); r.Outcome != wikigit.Nothing || r.Push != wikigit.PushSkipped {
		t.Fatalf("run after the push %+v", r)
	}
}

// A run that finds the commit lock held by another process is busy at once,
// commits nothing and records it, a push-only run with its reason.
func TestBusyWhileAnotherProcessHoldsTheLock(t *testing.T) {
	f := newFixture(t)
	release := holdLock(t, filepath.Join(f.o.Dirs.Locks(), "commit.lock"))
	head := git(t, f.wiki, "rev-parse", "HEAD")
	f.edit(t, "done [stated]: Waited for the lock")
	if r := f.commit(t); r.Outcome != wikigit.Busy || r.Push != wikigit.PushSkipped {
		t.Fatalf("result %+v", r)
	}
	if r, err := wikigit.Push(context.Background(), f.o); err != nil || r.Outcome != wikigit.Busy {
		t.Fatalf("push only %+v %v", r, err)
	}
	if git(t, f.wiki, "rev-parse", "HEAD") != head {
		t.Fatal("a busy run made a commit")
	}
	release()
	if r := f.commit(t); r.Outcome != wikigit.Committed {
		t.Fatalf("after release %+v", r)
	}
	if records := f.records(t); len(records) != 3 || records[0].Outcome != wikigit.Busy || records[0].Reason != "" || records[1].Outcome != wikigit.Busy || records[1].Reason != wikigit.ReasonPushOnly {
		t.Fatalf("records %+v", records)
	}
}

// A run that finds a page write holding the wiki lock past the wait is busy,
// with reason wiki_lock, and stages and commits nothing.
func TestBusyWhileAPageWriteHoldsTheWikiLock(t *testing.T) {
	f := newFixture(t)
	defer wikigit.SetWikiLockWait(100 * time.Millisecond)()
	holdLock(t, filepath.Join(f.o.Dirs.Locks(), "wiki.lock"))
	head := git(t, f.wiki, "rev-parse", "HEAD")
	f.edit(t, "done [stated]: Waited for a page write")
	if r := f.commit(t); r.Outcome != wikigit.Busy || r.Reason != "wiki_lock" || r.Push != wikigit.PushSkipped {
		t.Fatalf("result %+v", r)
	}
	if git(t, f.wiki, "rev-parse", "HEAD") != head {
		t.Fatal("a busy run made a commit")
	}
	if staged := git(t, f.wiki, "diff", "--cached", "--name-only"); staged != "" {
		t.Fatalf("a busy run staged %s", staged)
	}
}

// holdLock starts this test binary as another process that holds the lock
// file until release is called.
func holdLock(t *testing.T, file string) (release func()) {
	t.Helper()
	cmd := exec.Command(os.Args[0], "-test.run=^TestHelperHoldsLock$")
	cmd.Env = append(os.Environ(), "EVA_MD_TEST_HOLD_LOCK="+file)
	stdin, err := cmd.StdinPipe()
	if err != nil {
		t.Fatal(err)
	}
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		t.Fatal(err)
	}
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	line, err := bufio.NewReader(stdout).ReadString('\n')
	if err != nil || line != "locked\n" {
		t.Fatalf("helper: %q %v", line, err)
	}
	done := false
	release = func() {
		if !done {
			done = true
			stdin.Close()
			_ = cmd.Wait()
		}
	}
	t.Cleanup(release)
	return release
}

// TestHelperHoldsLock is the other process of holdLock: it holds the lock
// until its stdin closes. Run directly, it does nothing.
func TestHelperHoldsLock(t *testing.T) {
	file := os.Getenv("EVA_MD_TEST_HOLD_LOCK")
	if file == "" {
		t.Skip("run by holdLock")
	}
	if err := os.MkdirAll(filepath.Dir(file), 0o700); err != nil {
		t.Fatal(err)
	}
	f, err := os.OpenFile(file, os.O_CREATE|os.O_RDWR, 0o600)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	if err := syscall.Flock(int(f.Fd()), syscall.LOCK_EX); err != nil {
		t.Fatal(err)
	}
	os.Stdout.WriteString("locked\n")
	_, _ = io.Copy(io.Discard, os.Stdin)
}
