package resume

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/heyNag/eva-md/internal/coderef"
	"github.com/heyNag/eva-md/internal/identity"
	"github.com/heyNag/eva-md/internal/okfadapter"
	"github.com/heyNag/eva-md/internal/systempage"
)

// codeLine is a line of the system map, as systempage writes it, for a
// system in repos that cites the refs given, as of 2026-10-28.
func codeLine(slug, title, repos string, refs ...string) string {
	return "- [" + title + "](" + slug + ".md): Takes orders · repos: " + repos + " · code: " + strings.Join(refs, ", ") + " · as of 2026-10-28"
}

// codeFixture is briefFixture in a worktree of the project whose history is
// an empty commit, then one holding internal/orders/a.go and README.md, then
// two that change internal/orders/a.go. It returns those last three commits,
// oldest first.
func codeFixture(t *testing.T) (Options, []string) {
	t.Helper()
	o := briefFixture(t)
	repo := newRepo(t, "main")
	var commits []string
	for i, files := range []map[string]string{
		{"internal/orders/a.go": "package orders\n", "README.md": "Orders\n"},
		{"internal/orders/a.go": "package orders // two\n"},
		{"internal/orders/a.go": "package orders // three\n"},
	} {
		for file, text := range files {
			if err := os.MkdirAll(filepath.Dir(filepath.Join(repo, file)), 0o700); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(filepath.Join(repo, file), []byte(text), 0o600); err != nil {
				t.Fatal(err)
			}
		}
		gitOut(t, repo, "add", "-A")
		gitOut(t, repo, "commit", "-m", "change "+string(rune('a'+i)))
		commits = append(commits, gitOut(t, repo, "rev-parse", "HEAD"))
	}
	o.Project.Root = repo
	return o, commits
}

// gitLog puts a git first on PATH that logs its arguments and then runs the
// real git, or when slow never ends a count by itself, and returns what it
// logged.
func gitLog(t *testing.T, slow bool) func() string {
	t.Helper()
	real, err := exec.LookPath("git")
	if err != nil {
		t.Fatal(err)
	}
	log := filepath.Join(t.TempDir(), "git.log")
	script := `echo "$*" >> '` + log + `'`
	if slow {
		script += "\ncase \"$*\" in *rev-list*) exec sleep 600;; esac"
	}
	fakeGit(t, script+"\nexec '"+real+"' \"$@\"")
	return func() string {
		data, _ := os.ReadFile(log)
		return string(data)
	}
}

// A brief's pointer to a system that cites the project's code says how far
// that code has moved since the page's as_of, counted in the worktree with
// the paths the page cites, on both hosts at startup, clear and compact; a
// system citing only another repo's code, or a commit the worktree lacks,
// shows its day.
func TestBriefSaysHowFarTheCodeMoved(t *testing.T) {
	for _, host := range []string{"claude-code", "codex"} {
		for _, variant := range []string{"startup", "clear", "compact"} {
			t.Run(host+"/"+variant, func(t *testing.T) {
				o, c := codeFixture(t)
				o.Host, o.Variant = host, variant
				if variant == "compact" {
					o.Session = "compact-chat"
					capture(t, o, host, o.Session, "main", o.Now.Add(-20*time.Minute), "Compact this chat.")
				}
				key, other := o.Project.Key, "github.com__team__other"
				writeMap(t, o,
					codeLine("billing", "Billing", key, key+"@"+c[0][:7]+":internal/orders", key+"@"+c[0][:7]+":README.md"),
					codeLine("ledger", "Ledger", key, key+"@"+c[0][:12]+":README.md", key+"@"+c[2]),
					codeLine("orders", "Orders", key+", "+other, other+"@4e2bf4c:cmd", key+"@0123456789ab:internal"))
				r, err := Build(context.Background(), o)
				if err != nil {
					t.Fatal(err)
				}
				want := []string{
					"System: Billing: Takes orders (code moved 2 commits since 2026-10-28): " + usualBin + " show systems/billing",
					"System: Ledger: Takes orders (code unchanged since 2026-10-28): " + usualBin + " show systems/ledger",
					"System: Orders: Takes orders (as of 2026-10-28): " + usualBin + " show systems/orders",
					"System map: 3 systems: " + usualBin + " show systems/map",
				}
				if got := pointers(r.Text); !reflect.DeepEqual(got, want) {
					t.Fatalf("pointers\n%s\nin:\n%s", strings.Join(got, "\n"), r.Text)
				}
				cap, _, _ := budgets(o.Host, o.Variant, false)
				if runes(r.Text) > cap {
					t.Fatalf("%d runes over the cap of %d", runes(r.Text), cap)
				}
			})
		}
	}
}

// A system citing the commit HEAD names is unchanged with no count, when the
// thread page's head lets live git read HEAD.
func TestBriefCountsNothingAtHead(t *testing.T) {
	o, c := codeFixture(t)
	w, err := okfadapter.Open(o.Config.WikiPath, o.Dirs.Locks())
	if err != nil {
		t.Fatal(err)
	}
	pg, err := w.Read("projects/resume/threads/main")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := w.Update(pg.ID, pg.Hash, func(cc *okfadapter.Concept) error { cc.Extra["head"] = c[2]; return nil }, "eva-md/test"); err != nil {
		t.Fatal(err)
	}
	writeMap(t, o, codeLine("ledger", "Ledger", o.Project.Key, o.Project.Key+"@"+c[2][:7]+":internal/orders"))
	logged := gitLog(t, false)
	r, err := Build(context.Background(), o)
	if err != nil {
		t.Fatal(err)
	}
	if got := pointers(r.Text); len(got) != 2 || got[0] != "System: Ledger: Takes orders (code unchanged since 2026-10-28): "+usualBin+" show systems/ledger" {
		t.Fatalf("pointers %q", got)
	}
	if log := logged(); !strings.Contains(log, "rev-parse --verify HEAD") || strings.Contains(log, "rev-list") {
		t.Fatalf("git ran:\n%s", log)
	}
}

// threadHead sets the thread page's head, the commit live git counts from.
func threadHead(t *testing.T, o Options, head string) {
	t.Helper()
	w, err := okfadapter.Open(o.Config.WikiPath, o.Dirs.Locks())
	if err != nil {
		t.Fatal(err)
	}
	pg, err := w.Read("projects/resume/threads/main")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := w.Update(pg.ID, pg.Hash, func(cc *okfadapter.Concept) error { cc.Extra["head"] = head; return nil }, "eva-md/test"); err != nil {
		t.Fatal(err)
	}
}

// A count of none is no movement, as the saved code status has it, for a
// path the cited commit lacks, and for a commit off the branch whose fork
// point is HEAD, when the status found it off the branch at the HEAD live
// git reads: those pointers show the day, as the prompt's do. A status taken
// at another HEAD, or none, leaves the count as it is.
func TestBriefTakesNoCountTheStatusCantMeasure(t *testing.T) {
	o, c := codeFixture(t)
	repo, key := o.Project.Root, o.Project.Key
	gitOut(t, repo, "checkout", "-q", "-b", "side")
	if err := os.WriteFile(filepath.Join(repo, "internal/orders/a.go"), []byte("package orders // side\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	gitOut(t, repo, "commit", "-q", "-am", "side")
	side := gitOut(t, repo, "rev-parse", "HEAD")
	gitOut(t, repo, "checkout", "-q", "main")
	threadHead(t, o, c[2])
	writeMap(t, o,
		codeLine("ledger", "Ledger", key, key+"@"+c[0][:7]+":internal/order"),
		codeLine("billing", "Billing", key, key+"@"+side[:7]+":internal/orders"),
		codeLine("orders", "Orders", key, key+"@"+c[1][:7]+":internal/orders"))
	unchanged := func(slug, title string) string {
		return "System: " + title + ": Takes orders (code unchanged since 2026-10-28): " + usualBin + " show systems/" + slug
	}
	asOf := func(slug, title string) string {
		return "System: " + title + ": Takes orders (as of 2026-10-28): " + usualBin + " show systems/" + slug
	}
	moved := "System: Orders: Takes orders (code moved 1 commit since 2026-10-28): " + usualBin + " show systems/orders"
	last := "System map: 3 systems: " + usualBin + " show systems/map"
	for _, tc := range []struct {
		name, head string
		want       []string
	}{
		{"none", "", []string{unchanged("ledger", "Ledger"), unchanged("billing", "Billing"), moved, last}},
		{"at HEAD", c[2], []string{asOf("ledger", "Ledger"), asOf("billing", "Billing"), moved, last}},
		{"at another HEAD", c[1], []string{asOf("ledger", "Ledger"), unchanged("billing", "Billing"), moved, last}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if tc.head != "" {
				report := coderef.Report{V: 1, At: "2026-10-29T11:00:00Z", Pages: []coderef.PageStatus{
					{Slug: "ledger", AsOf: "2026-10-28", Repos: []coderef.RepoStatus{{Key: key, Commit: c[0][:7], Paths: []string{"internal/order"}, State: coderef.Unchanged, Gone: []string{"internal/order"}, Clone: repo, Branch: "main", Head: tc.head}}},
					{Slug: "billing", AsOf: "2026-10-28", Repos: []coderef.RepoStatus{{Key: key, Commit: side[:7], Paths: []string{"internal/orders"}, State: coderef.NotOnBranch, Clone: repo, Branch: "main", Head: tc.head}}},
					{Slug: "orders", AsOf: "2026-10-28", Repos: []coderef.RepoStatus{{Key: key, Commit: c[1][:7], Paths: []string{"internal/orders"}, State: coderef.Unchanged, Clone: repo, Branch: "main", Head: tc.head}}},
				}}
				if err := coderef.Save(o.Dirs.State, report); err != nil {
					t.Fatal(err)
				}
			}
			r, err := Build(context.Background(), o)
			if err != nil {
				t.Fatal(err)
			}
			if got := pointers(r.Text); !reflect.DeepEqual(got, tc.want) {
				t.Fatalf("pointers\n%s\nin:\n%s", strings.Join(got, "\n"), r.Text)
			}
		})
	}
}

// At HEAD, the cited commit itself, no count runs, and a saved status that
// found the commit lacks the cited path still shows the day; a status of
// other paths, or none, leaves the pointer saying the code is unchanged.
func TestBriefAtTheCitedCommitTakesTheStatus(t *testing.T) {
	o, c := codeFixture(t)
	key := o.Project.Key
	threadHead(t, o, c[2])
	writeMap(t, o, codeLine("ledger", "Ledger", key, key+"@"+c[2][:7]+":internal/order"))
	save := func(paths ...string) {
		t.Helper()
		report := coderef.Report{V: 1, At: "2026-10-29T11:00:00Z", Pages: []coderef.PageStatus{
			{Slug: "ledger", AsOf: "2026-10-28", Repos: []coderef.RepoStatus{{Key: key, Commit: c[2][:7], Paths: paths, State: coderef.Unchanged, Gone: paths, Clone: o.Project.Root, Branch: "main", Head: c[2]}}},
		}}
		if err := coderef.Save(o.Dirs.State, report); err != nil {
			t.Fatal(err)
		}
	}
	first := func() string {
		t.Helper()
		r, err := Build(context.Background(), o)
		if err != nil {
			t.Fatal(err)
		}
		got := pointers(r.Text)
		if len(got) == 0 {
			t.Fatalf("no pointers in:\n%s", r.Text)
		}
		return got[0]
	}
	unchanged := "System: Ledger: Takes orders (code unchanged since 2026-10-28): " + usualBin + " show systems/ledger"
	if got := first(); got != unchanged {
		t.Fatalf("no status: %s", got)
	}
	save("internal/other")
	if got := first(); got != unchanged {
		t.Fatalf("a status of other paths: %s", got)
	}
	save("internal/order")
	if got := first(); got != "System: Ledger: Takes orders (as of 2026-10-28): "+usualBin+" show systems/ledger" {
		t.Fatalf("a status that found the path lacking: %s", got)
	}
}

// With log.showSignature on in the user's git config, live git lists the
// subjects of the commits since the thread page's head and nothing else: no
// signature is checked.
func TestLiveGitWithShowSignature(t *testing.T) {
	fixture(t)
	t.Setenv("SSH_AUTH_SOCK", "")
	home := os.Getenv("HOME")
	key := filepath.Join(home, "key")
	if out, err := exec.Command("ssh-keygen", "-q", "-t", "ed25519", "-N", "", "-f", key, "-C", "fixture").CombinedOutput(); err != nil {
		t.Fatalf("ssh-keygen: %v %s", err, out)
	}
	pub, err := os.ReadFile(key + ".pub")
	if err != nil {
		t.Fatal(err)
	}
	allowed := filepath.Join(home, "allowed")
	if err := os.WriteFile(allowed, []byte("fixture@example.invalid "+string(pub)), 0o600); err != nil {
		t.Fatal(err)
	}
	config := "[user]\nsigningkey = " + key + ".pub\n[gpg]\nformat = ssh\n[gpg \"ssh\"]\nallowedSignersFile = " + allowed + "\n[log]\nshowSignature = true\n"
	if err := os.WriteFile(filepath.Join(home, "gitconfig"), []byte(config), 0o600); err != nil {
		t.Fatal(err)
	}
	repo := newRepo(t, "main")
	base := gitOut(t, repo, "rev-parse", "HEAD")
	gitOut(t, repo, "config", "commit.gpgsign", "true")
	gitOut(t, repo, "commit", "-q", "--allow-empty", "-m", "First change")
	gitOut(t, repo, "commit", "-q", "--allow-empty", "-m", "Second change")
	if out := gitOut(t, repo, "log", "-1", "--format=%s"); !strings.Contains(out, "Good") {
		t.Fatalf("the fixture shows no signature: %q", out)
	}
	info := gitSummary(context.Background(), identity.Project{Root: repo}, base)
	want := "HEAD moved 2 commits since the page was written\n" + indent + "- Second change\n" + indent + "- First change\n"
	if !strings.Contains(info.text, want) || strings.Contains(strings.ToLower(info.text), "signature") {
		t.Fatalf("live git:\n%s", info.text)
	}
}

// Near its deadline the brief counts nothing and shows the day; a count that
// runs out of time is killed, stops the others, and never keeps the pointers
// past the brief's deadline. Each brief here counts within bounds of its own
// against a deadline over a minute away, so where the deadline stands
// against those bounds decides the outcome, never how fast the machine is.
func TestBriefCodeWithinItsDeadline(t *testing.T) {
	o, c := codeFixture(t)
	key := o.Project.Key
	writeMap(t, o,
		codeLine("billing", "Billing", key, key+"@"+c[0][:7]+":internal/orders"),
		codeLine("ledger", "Ledger", key, key+"@"+c[1][:7]+":internal/orders"))
	asOf := "System: Billing: Takes orders (as of 2026-10-28): " + usualBin + " show systems/billing\n" +
		"System: Ledger: Takes orders (as of 2026-10-28): " + usualBin + " show systems/ledger\n" +
		"System map: 2 systems: " + usualBin + " show systems/map"
	// Less time is left before the deadline, less gitReserve, than a count
	// needs.
	logged := gitLog(t, false)
	ctx, cancel := context.WithTimeout(context.Background(), gitReserve+time.Minute)
	defer cancel()
	if got := pointersWithin(ctx, o, "", &codeClock{left: 2 * time.Minute, call: time.Minute, budget: time.Minute}); got != asOf || strings.Contains(logged(), "rev-list") {
		t.Fatalf("near the deadline:\n%s\n%s", got, logged())
	}
	// A count whose git never ends by itself runs out of its own time, a
	// short one: it is killed, perhaps before its git has started, the
	// second count never runs, and the pointers come back long before the
	// deadline.
	logged = gitLog(t, true)
	ctx, cancel = context.WithTimeout(context.Background(), gitReserve+time.Minute)
	defer cancel()
	clock := &codeClock{left: codeLeft, call: 100 * time.Millisecond, budget: time.Minute}
	if got := pointersWithin(ctx, o, "", clock); got != asOf || ctx.Err() != nil || clock.spent == 0 || !clock.stopped || strings.Count(logged(), "rev-list") > 1 {
		t.Fatalf("a count out of time: %v, spent %v, stopped %v\n%s\n%s", ctx.Err(), clock.spent, clock.stopped, got, logged())
	}
}

// A brief in a partial clone never fetches what the clone lacks from its
// remote, with a git that honours GIT_NO_LAZY_FETCH and with one that, as
// git before 2.44 does, ignores it: the clone gains no object, the system
// citing a commit it lacks shows its day, live git says the thread page's
// commit is unavailable, and a count that needs trees a treeless clone lacks
// shows the day too. So it is, with a git that honours GIT_NO_LAZY_FETCH,
// in a clone whose promisor is set in the global config, which
// coderef.Partial can't see.
func TestBriefFetchesNothingInAPartialClone(t *testing.T) {
	real, err := exec.LookPath("git")
	if err != nil {
		t.Fatal(err)
	}
	for _, filter := range []string{"blob:none", "tree:0"} {
		for _, mode := range []string{"old=false", "old=true", "global"} {
			t.Run(filter+" "+mode, func(t *testing.T) {
				o, c := codeFixture(t)
				origin := o.Project.Root
				gitOut(t, origin, "config", "uploadpack.allowFilter", "true")
				gitOut(t, origin, "config", "uploadpack.allowAnySHA1InWant", "true")
				gitOut(t, origin, "checkout", "-q", "-b", "side")
				gitOut(t, origin, "commit", "-q", "--allow-empty", "-m", "side")
				side := gitOut(t, origin, "rev-parse", "HEAD")
				gitOut(t, origin, "checkout", "-q", "main")
				clone := filepath.Join(t.TempDir(), "clone")
				if out, err := exec.Command("git", "clone", "-q", "--filter="+filter, "--single-branch", "--branch", "main", "file://"+origin, clone).CombinedOutput(); err != nil {
					t.Fatalf("clone: %v %s", err, out)
				}
				o.Project.Root = clone
				if mode == "global" {
					gitOut(t, clone, "config", "--unset", "remote.origin.promisor")
					gitOut(t, clone, "config", "--unset", "remote.origin.partialclonefilter")
					gitOut(t, clone, "config", "--global", "remote.origin.promisor", "true")
					gitOut(t, clone, "config", "--global", "remote.origin.partialclonefilter", filter)
					if coderef.Partial(clone) {
						t.Fatal("a clone whose promisor is set in the global config is seen as partial")
					}
				}
				w, err := okfadapter.Open(o.Config.WikiPath, o.Dirs.Locks())
				if err != nil {
					t.Fatal(err)
				}
				pg, err := w.Read("projects/resume/threads/main")
				if err != nil {
					t.Fatal(err)
				}
				if _, err := w.Update(pg.ID, pg.Hash, func(cc *okfadapter.Concept) error { cc.Extra["head"] = side; return nil }, "eva-md/test"); err != nil {
					t.Fatal(err)
				}
				key := o.Project.Key
				writeMap(t, o,
					codeLine("billing", "Billing", key, key+"@"+side+":internal/orders"),
					codeLine("ledger", "Ledger", key, key+"@"+c[0]+":internal/orders"))
				if mode == "old=true" {
					fakeGit(t, "unset GIT_NO_LAZY_FETCH\nexec '"+real+"' \"$@\"")
				}
				t.Setenv("GIT_NO_LAZY_FETCH", "0")
				count := func() string {
					cmd := exec.Command(real, "-C", clone, "count-objects", "-v")
					cmd.Env = append(os.Environ(), "GIT_NO_LAZY_FETCH=1")
					out, err := cmd.CombinedOutput()
					if err != nil {
						t.Fatalf("count-objects: %v %s", err, out)
					}
					return string(out)
				}
				before := count()
				r, err := Build(context.Background(), o)
				if err != nil {
					t.Fatal(err)
				}
				if after := count(); after != before {
					t.Fatalf("the brief fetched into the partial clone:\nbefore %s\nafter %s", before, after)
				}
				ledger := "(code moved 2 commits since 2026-10-28)"
				if filter == "tree:0" {
					ledger = "(as of 2026-10-28)"
				}
				want := []string{
					"System: Billing: Takes orders (as of 2026-10-28): " + usualBin + " show systems/billing",
					"System: Ledger: Takes orders " + ledger + ": " + usualBin + " show systems/ledger",
					"System map: 2 systems: " + usualBin + " show systems/map",
				}
				if got := pointers(r.Text); !reflect.DeepEqual(got, want) || !strings.Contains(r.Text, "HEAD changed; previous commit unavailable") {
					t.Fatalf("pointers\n%s\nin:\n%s", strings.Join(got, "\n"), r.Text)
				}
			})
		}
	}
}

// Live git killed at its deadline takes the processes it started with it:
// here a child that holds git's output open. The test ends live git's time
// once git has started the child, so the kill always meets it.
func TestLiveGitKillsItsChildren(t *testing.T) {
	fixture(t)
	repo := newRepo(t, "main")
	real, err := exec.LookPath("git")
	if err != nil {
		t.Fatal(err)
	}
	fifo := filepath.Join(t.TempDir(), "pid")
	if err := syscall.Mkfifo(fifo, 0o600); err != nil {
		t.Fatal(err)
	}
	fakeGit(t, "case \"$*\" in *status*) sleep 600 &\necho $! > '"+fifo+"'\nwait;; esac\nexec '"+real+"' \"$@\"")
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	done := make(chan gitInfo, 1)
	go func() { done <- gitSummary(ctx, identity.Project{Root: repo}, "") }()
	pid := make(chan int, 1)
	go func() {
		data, _ := os.ReadFile(fifo)
		n, _ := strconv.Atoi(strings.TrimSpace(string(data)))
		pid <- n
	}()
	var child int
	select {
	case child = <-pid:
	case <-time.After(hangGuard):
		t.Fatalf("the fake git started no child within %v", hangGuard)
	}
	// The child runs until a kill ends it, so ps lists it now, if perhaps
	// as the shell it was forked from, not yet sleep: what tells it from a
	// later process given its pid is its start time, which exec keeps, and
	// not its command.
	_, born, _ := psRow(t, child)
	if born.IsZero() {
		t.Fatal("ps does not list the child the fake git started")
	}
	cancel()
	select {
	case info := <-done:
		if info.text != "Live git: unknown (unavailable or deadline)" {
			t.Fatalf("live git out of time: %q", info.text)
		}
	case <-time.After(hangGuard):
		// A child the kill missed holds git's output open, so live git
		// returns only when its WaitDelay ends. TestMain raises that to the
		// guard's minute, and os/exec starts it only once it has sent the
		// kill, as a rule after the guard started, so the guard ends first.
		// The test then kills the child, once ps shows it is still the
		// child and not a zombie, and fails.
		if state, start, comm := psRow(t, child); start.Equal(born) && !strings.HasPrefix(state, "Z") {
			syscall.Kill(child, syscall.SIGKILL)
			t.Fatalf("live git out of time hung: after %v, the child git started still ran, holding git's output open (%s, state %s)", hangGuard, comm, state)
		}
		t.Fatalf("live git out of time hung: it had not returned after %v", hangGuard)
	}
	// Live git returned. Either its output reached its end, or its
	// WaitDelay ended about when the guard did, and the select took the
	// return. In the first case the child, which holds the output open as
	// long as it runs, had closed it: the kill reached it, though ps can
	// still list it for a moment, as a system closes a killed process's
	// files before it makes it a zombie. In between, Linux can show it
	// running (R) or in an uninterruptible wait (D), and macOS can add an E
	// to its state while it exits, as in RE. In the second, a child the kill
	// missed still sleeps: ps shows it S, or I on macOS once it has slept
	// about 20 seconds. So the test waits, as long as the guard, for the
	// child to be a zombie (Z) or gone, its pid perhaps given to a later
	// process, which another start time tells; a child still alive then is
	// killed, and the test fails.
	for end := time.Now().Add(hangGuard); ; time.Sleep(10 * time.Millisecond) {
		state, start, comm := psRow(t, child)
		if state == "" || strings.HasPrefix(state, "Z") || !start.Equal(born) {
			return
		}
		if time.Now().After(end) {
			syscall.Kill(child, syscall.SIGKILL)
			t.Fatalf("the child git started outlived live git (%s, state %s %v after it returned)", comm, state, hangGuard)
		}
	}
}

// psRow gives ps's state of the process pid, its start time, which stays the
// same for as long as it runs, and its command, which exec changes, so it
// serves only in messages. ps lists the start time in five fields, as "Wed
// Nov  4 21:43:12 2026" in the C locale, on Linux and macOS alike. The state
// is "" and the time zero once no process has the pid, which ps tells by
// exiting 1 and printing nothing, to stdout or stderr; any other failure of
// ps, or a row it can't read, fails the test. Its twin is psRow in
// internal/coderef's git_test.go.
func psRow(t *testing.T, pid int) (state string, start time.Time, comm string) {
	t.Helper()
	cmd := exec.Command("ps", "-o", "stat=", "-o", "lstart=", "-o", "comm=", "-p", strconv.Itoa(pid))
	cmd.Env = append(os.Environ(), "LC_ALL=C")
	out, err := cmd.Output()
	var exit *exec.ExitError
	if errors.As(err, &exit) {
		if exit.ExitCode() == 1 && len(out) == 0 && len(exit.Stderr) == 0 {
			return "", time.Time{}, ""
		}
		t.Fatalf("ps -p %d: %v, stdout %q, stderr %q", pid, err, out, exit.Stderr)
	}
	if err != nil {
		t.Fatalf("ps -p %d: %v", pid, err)
	}
	f := strings.Fields(string(out))
	if len(f) < 6 {
		t.Fatalf("ps -p %d listed no state, start time and command: %q", pid, out)
	}
	start, err = time.Parse("Mon Jan 2 15:04:05 2006", strings.Join(f[1:6], " "))
	if err != nil {
		t.Fatalf("ps -p %d listed no start time: %q", pid, out)
	}
	return f[0], start, strings.Join(f[6:], " ")
}

// To fit, a pointer's description is cut, then left out; then the note gives
// way to the day, and the day goes; the command is never cut.
func TestPointerFitsItsNote(t *testing.T) {
	e := systempage.MapEntry{Slug: "orders", Title: "Orders", Description: strings.Repeat("Takes orders and much more; ", 6), AsOf: "2026-10-28"}
	note := "code moved 14 commits since 2026-10-28"
	head, tail := "System: Orders", ": "+usualBin+" show systems/orders"
	withNote, withDay := head+" ("+note+")"+tail, head+" (as of 2026-10-28)"+tail
	for _, tc := range []struct {
		limit int
		want  string
	}{
		{pointerCap, head + ": " + cut(e.Description, pointerCap-runes(head+":  ("+note+")"+tail)) + " (" + note + ")" + tail},
		{runes(withNote) + minDescription - 1, withNote},
		{runes(withNote), withNote},
		{runes(withNote) - 1, withDay},
		{runes(withDay) - 1, head + tail},
	} {
		if got := pointer(e, note, usualBin, tc.limit); got != tc.want || runes(got) > tc.limit {
			t.Errorf("limit %d:\n got %q\nwant %q", tc.limit, got, tc.want)
		}
	}
}
