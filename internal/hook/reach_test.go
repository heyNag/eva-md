package hook

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"go/ast"
	"go/build"
	"go/parser"
	"go/token"
	"io/fs"
	"maps"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/heyNag/eva-md/internal/inbox"
	"github.com/heyNag/eva-md/internal/searchindex"
	"github.com/heyNag/eva-md/internal/wikigit"
)

// What a hook reaches outside eva-md's own files (CAP-K05, PRD-K01,
// BRF-K01, RCL-K01, STO-K14, and the hooks' half of SRC-K12): the programs
// it starts, the packages it links, and the search index.

// outside are the programs the tests watch a hook start above all: git,
// which a hook may run only to read (see readOnly) and which commits,
// pushes and fetches for others, what reaches a network, and the hosts'
// CLIs, whose headless runs are model calls; a hook starts none of the
// last three, whose stand-ins exit 1 at once. Every other program a hook
// could start by its name is watched too.
var outside = []string{"git", "ssh", "curl", "claude", "codex"}

// plainName is a program's name a stand-in can be written for.
var plainName = regexp.MustCompile(`^[A-Za-z0-9._+-]+$`)

// standIns makes PATH a folder of recording stand-ins alone: one for each
// program of outside, and one for every other program a folder of PATH
// held, by its name. It returns the log they write: one line a start, the
// program's name and its arguments, and for git, after a tab, the two
// variables of its environment that keep a read from writing or fetching.
// The stand-ins of ssh, curl, claude and codex exit 1 at once; every other
// hands over to the program it stands for, so a hook that runs it gets
// what it would have. A program started by its full path is not seen.
func standIns(t *testing.T) string {
	t.Helper()
	real, err := exec.LookPath("git")
	if err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	log := filepath.Join(dir, "started.log")
	programs := map[string]string{} // name to the program it stands for, or "" for none
	for _, folder := range filepath.SplitList(os.Getenv("PATH")) {
		entries, err := os.ReadDir(folder)
		if !filepath.IsAbs(folder) || strings.Contains(folder, "'") || err != nil {
			continue
		}
		for _, e := range entries {
			path := filepath.Join(folder, e.Name())
			info, err := os.Stat(path)
			if _, ok := programs[e.Name()]; ok || !plainName.MatchString(e.Name()) || err != nil || info.IsDir() || info.Mode()&0o111 == 0 {
				continue
			}
			programs[e.Name()] = path
		}
	}
	for _, name := range outside {
		programs[name] = ""
	}
	for name, path := range programs {
		record := "printf '%s\\n' \"" + name + " $*\""
		then := "exec '" + path + "' \"$@\""
		switch {
		case name == "git":
			record = "printf '%s\\t%s\\n' \"git $*\" \"GIT_OPTIONAL_LOCKS=${GIT_OPTIONAL_LOCKS-} GIT_NO_LAZY_FETCH=${GIT_NO_LAZY_FETCH-}\""
			then = "exec '" + real + "' \"$@\""
		case path == "":
			then = "exit 1"
		}
		script := "#!/bin/sh\n" + record + " >> '" + log + "'\n" + then + "\n"
		if err := os.WriteFile(filepath.Join(dir, name), []byte(script), 0o700); err != nil {
			t.Fatal(err)
		}
	}
	t.Setenv("PATH", dir)
	return log
}

// startedOf reads a stand-ins' log, one start a line, or nil for none, and
// for each git start the variables its stand-in wrote of its environment.
func startedOf(t *testing.T, log string) (lines, envs []string) {
	t.Helper()
	data, err := os.ReadFile(log)
	if errors.Is(err, fs.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		t.Fatal(err)
	}
	for _, l := range strings.Split(strings.TrimSuffix(string(data), "\n"), "\n") {
		line, env, _ := strings.Cut(l, "\t")
		lines, envs = append(lines, line), append(envs, env)
	}
	return lines, envs
}

// The stand-ins record what they are started with, git with its
// environment, a program of PATH's beside those of outside, and nothing
// records a program that was not started, so the tests below can fail.
func TestStandInsRecordTheirStarts(t *testing.T) {
	setup(t)
	log := standIns(t)
	if got, _ := startedOf(t, log); got != nil {
		t.Fatalf("nothing started, yet the log reads %q", got)
	}
	for _, name := range append(slices.Clone(outside), "true") {
		cmd := exec.Command(name, "--a", "b")
		cmd.Dir = t.TempDir()
		cmd.Env = append(os.Environ(), "GIT_NO_LAZY_FETCH=1")
		_ = cmd.Run()
	}
	want := []string{"git --a b", "ssh --a b", "curl --a b", "claude --a b", "codex --a b", "true --a b"}
	got, envs := startedOf(t, log)
	if !slices.Equal(got, want) {
		t.Fatalf("the stand-ins logged %q, want %q", got, want)
	}
	if !strings.HasSuffix(envs[0], " GIT_NO_LAZY_FETCH=1") || envs[1] != "" {
		t.Errorf("the stand-ins logged the environments %q", envs)
	}
}

// lockedBuffer is the hook's output, which spawn reads while Run writes it.
type lockedBuffer struct {
	mu  sync.Mutex
	buf bytes.Buffer
}

func (b *lockedBuffer) Write(p []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.Write(p)
}

func (b *lockedBuffer) String() string {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.String()
}

// reachStep is one hook a chat runs, at a time after the fixture's day.
type reachStep struct {
	event, source, prompt string
	at                    time.Duration
}

// aChat is a chat that runs every event the package's fixtures drive: two
// starts, the second a minute after the first, when a pending push is due
// its retry, a prompt that names systems and knowledge, tools, a reply,
// failures, a compaction, a model switch, a clear, an end, and a start the
// next day, when that day's maintain is due.
var aChat = []reachStep{
	{"SessionStart", "startup", "", 0},
	{"SessionStart", "startup", "", time.Minute},
	{"UserPromptSubmit", "", "Why does LedgerSync miss the payments-api calls? Remind me of the capture latency target and the wiki lock timeout.", 2 * time.Minute},
	{"PostToolUse", "", "", 3 * time.Minute},
	{"PostToolUseFailure", "", "", 4 * time.Minute},
	{"Stop", "", "", 5 * time.Minute},
	{"StopFailure", "", "", 6 * time.Minute},
	{"Interrupt", "", "", 7 * time.Minute},
	{"PreCompact", "", "", 8 * time.Minute},
	{"PostCompact", "", "", 9 * time.Minute},
	{"SessionStart", "compact", "", 10 * time.Minute},
	{"PostModelSwitch", "", "", 11 * time.Minute},
	{"SessionStart", "clear", "", 12 * time.Minute},
	{"UserPromptSubmit", "", "Is the auth.v2 service behind, and are hooks offline?", 13 * time.Minute},
	{"SessionEnd", "", "", 14 * time.Minute},
	{"SessionStart", "resume", "", 24 * time.Hour},
}

// The steps of aChat that the tests below read by their place.
const (
	firstStart = 0
	retryStart = 1
	firstAsk   = 2
	secondAsk  = 13
	chatEnd    = 14
	nextDay    = 15
)

// reachRun is what one step of a chat did: its output, each job it started
// with the output as it stood when the job started, and what the
// stand-ins logged while it ran.
type reachRun struct {
	step    reachStep
	out     string
	jobs    [][]string
	before  []string
	started []string
	envs    []string // of each start, its environment as the stand-ins logged it
}

// runChat runs steps as one chat of host, and returns what each did. log
// is the stand-ins' log, or "" for none. Each start the log holds is given
// to the step that was running when it was read, and the log is read once
// more after the chat: a start no step was given fails the test.
func runChat(t *testing.T, host string, p payload, env Env, steps []reachStep, log string) []reachRun {
	t.Helper()
	day := env.Now()
	// Every event carries what its kind reads, as in
	// TestEveryEventOnBothHosts, so each stores what it would.
	reply := "Done."
	p.Reply, p.Summary, p.ToModel, p.Tool = &reply, "host summary", "next-model", "Write"
	p.ToolInput = json.RawMessage(`{"file_path":"file.txt","content":"not captured"}`)
	p.ToolResponse = json.RawMessage(`"Exit code: 7\nTOOL OUTPUT"`)
	p.Error = json.RawMessage(`{"type":"rate_limit_error","message":"an error"}`)
	var runs []reachRun
	read := 0 // the starts of the log given to a step
	for i, s := range steps {
		var out lockedBuffer
		r := reachRun{step: s}
		env.spawn = func(args ...string) {
			r.jobs = append(r.jobs, append([]string(nil), args...))
			r.before = append(r.before, out.String())
		}
		env.Now = func() time.Time { return day.Add(s.at) }
		p.Event, p.Source, p.Turn = s.event, s.source, fmt.Sprint("turn-", i+1)
		if s.prompt != "" {
			p.Prompt = s.prompt
		}
		data, err := json.Marshal(p)
		if err != nil {
			t.Fatal(err)
		}
		if code := Run(context.Background(), host, bytes.NewReader(data), &out, env); code != 0 {
			t.Fatalf("%s exited %d", s.event, code)
		}
		if log != "" {
			lines, envs := startedOf(t, log)
			r.started, r.envs, read = lines[read:], envs[read:], len(lines)
		}
		r.out = out.String()
		runs = append(runs, r)
	}
	if log != "" {
		if lines, _ := startedOf(t, log); len(lines) > read {
			t.Errorf("after the chat's last hook, the stand-ins logged %q, which no hook's run was read with", lines[read:])
		}
	}
	return runs
}

// reachWiki is the fixture of every test here: a wiki with system pages,
// two of which cite code, the code status maintain saved, knowledge pages
// with their recall index, an earlier chat, the chat's own session with
// 120 KB unprocessed, another session idle an hour with a backlog, a push
// pending since two hours before, and a search index built from the inbox.
func reachWiki(t *testing.T, host string, env *Env, p payload) {
	t.Helper()
	reachWikiAt(t, host, env, p, "4e2bf4c")
}

// reachWikiAt is reachWiki with the system pages citing commit.
func reachWikiAt(t *testing.T, host string, env *Env, p payload, commit string) {
	t.Helper()
	systems := map[string]string{
		"ledger":   codeSystemPage("Ledger", "Keeps the double-entry books", "github.com/acme/ledger@"+commit+":internal/books/", "LedgerSync"),
		"payments": codeSystemPage("Payments", "Charges cards and pays out", "github.com/acme/ledger@"+commit, "payments-api"),
		"auth":     systemPage("Auth service", "Signs users in", nil, "auth.v2"),
	}
	knowledgeWiki(t, env, systems, facts)
	plantStatus(t, *env, map[string]int{"ledger": 14, "payments": 0, "auth": 3}, commit)
	now := env.Now()
	sweptOn(t, *env, now)
	plant(t, *env, "codex", "previous-session")
	sessionFile(t, *env, host, p.Session, 2*stopBacklog, inbox.Mark{}, now)
	sessionFile(t, *env, "codex", "idle-other", 1000, inbox.Mark{}, now.Add(-time.Hour))
	commitRecords(t, *env, wikigit.Result{At: now.Add(-2 * time.Hour).Format(time.RFC3339), Outcome: wikigit.Committed, Pages: 2, Push: wikigit.PushPending})
	if r, err := searchindex.Catchup(context.Background(), env.Dirs, "maintain"); err != nil || r.Busy {
		t.Fatalf("building the search index: %+v %v", r, err)
	}
	if len(searchindex.Folders(env.Dirs)) == 0 {
		t.Fatal("the fixture's search index has no folder")
	}
}

// hasJob says whether runs started a job whose arguments start with want.
func hasJob(runs []reachRun, want ...string) bool {
	for _, r := range runs {
		for _, j := range r.jobs {
			if len(j) >= len(want) && slices.Equal(j[:len(want)], want) {
				return true
			}
		}
	}
	return false
}

// checkStarts holds what each run of a chat started to what a hook may
// start: no program but git, git only to read (see readOnly), in a
// project that is a partial clone or may be, and its jobs only through the
// spawn seam, once its output is written. git runs in no step that gitAt
// does not allow. It returns a problem for each start that breaks this.
func checkStarts(runs []reachRun, gitAt func(i int) bool, partial bool) []string {
	var problems []string
	for i, r := range runs {
		what := r.step.event + "/" + r.step.source
		for j, line := range r.started {
			if why := readOnly(line, r.envs[j], partial); why != "" {
				problems = append(problems, fmt.Sprintf("%s: %s: %q", what, why, line))
			}
		}
		if len(r.started) > 0 && !gitAt(i) {
			problems = append(problems, fmt.Sprintf("%s ran %q", what, r.started))
		}
		for j, before := range r.before {
			if before != r.out {
				problems = append(problems, fmt.Sprintf("%s started %q before its output was written: %q of %q", what, r.jobs[j], before, r.out))
			}
		}
	}
	return problems
}

// onlyIdentity is a problem for each git start of the chat's first hook in
// a project of no repository but identity's rev-parse --show-toplevel.
func onlyIdentity(runs []reachRun) []string {
	var problems []string
	for _, line := range runs[firstStart].started {
		if command, args := gitCommand(strings.Fields(strings.TrimPrefix(line, "git "))); !strings.HasPrefix(line, "git ") || command != "rev-parse" || !slices.Equal(args, []string{"--show-toplevel"}) {
			problems = append(problems, fmt.Sprintf("the chat's first hook ran %q, not only identity's rev-parse", line))
		}
	}
	return problems
}

// checkDue holds the runs of aChat in a project of no repository to what
// the fixture made due: the jobs started, and the prompts got their
// pointers and recall, so the wiki was read and the triggers fired.
func checkDue(t *testing.T, host string, p payload, runs []reachRun) {
	t.Helper()
	for _, c := range []struct {
		at   int
		want []string
	}{
		{firstStart, []string{"maintain"}},
		{firstStart, []string{"update", "--session", "codex:idle-other"}},
		{retryStart, []string{"maintain", "--push-only"}},
		{chatEnd, []string{"update", "--session", host + ":" + p.Session}},
		{nextDay, []string{"maintain"}},
	} {
		if !hasJob(runs[c.at:c.at+1], c.want...) {
			t.Errorf("%s/%s started %q, not %q, which the fixture made due", runs[c.at].step.event, runs[c.at].step.source, runs[c.at].jobs, c.want)
		}
	}
	prompts := runs[firstAsk].out + runs[secondAsk].out
	for _, want := range []string{"show systems/ledger", "code moved 14 commits", "show systems/auth", "show knowledge/"} {
		if !strings.Contains(prompts, want) {
			t.Errorf("no prompt's output holds %q, so the wiki was not read: %q", want, prompts)
		}
	}
}

// Every hook event of both hosts, in a chat whose project is no git
// repository, with a wiki, system pages citing code, knowledge pages and
// their recall index, a search index, and the update, the day's maintain
// and a push retry all due, starts none of ssh, curl, claude or codex,
// and runs git once: to ask, at the chat's first hook, whether the folder
// is in a repository (identity's rev-parse --show-toplevel), which the
// identity cache then answers. No other event runs git, the prompts with
// their pointers and recall, the push retry and the next day's maintain
// included. What a hook starts is its background job, through the spawn
// seam, once its output is written.
func TestHooksStartNothingOutside(t *testing.T) {
	for _, host := range hosts {
		t.Run(host, func(t *testing.T) {
			env, p := setup(t)
			reachWiki(t, host, &env, p)
			log := standIns(t)
			runs := runChat(t, host, p, env, aChat, log)
			for _, problem := range append(checkStarts(runs, notGitProject, false), onlyIdentity(runs)...) {
				t.Error(problem)
			}
			checkDue(t, host, p, runs)
		})
	}
}

// notGitProject is where a chat in a project of no repository may run git:
// at its first hook, to ask whether it is in one.
func notGitProject(i int) bool { return i == firstStart }

// The checks above fail a hook that starts what it may not: a handler of
// the hook's own list, registered here as a later package would register
// one, that runs git to fetch, git on a prompt, git that may write the
// index or start a program the repository names, curl at a chat's start,
// a host's CLI, any other program, or a job before the output is written,
// each makes the chat's checks report it, and only it.
func TestAHookThatStartsWhatItMayNotIsCaught(t *testing.T) {
	run := func(name string, args ...string) func(*turn) {
		return func(*turn) { _ = exec.Command(name, args...).Run() }
	}
	// read runs git in the chat's project as the hook's reads do, but for
	// what fault drops.
	project := ""
	read := func(fault string, args ...string) func(*turn) {
		return func(*turn) {
			var full []string
			for _, o := range []string{"-c core.fsmonitor=false", "-c core.hooksPath=/dev/null", "--no-optional-locks"} {
				if o != fault {
					full = append(full, strings.Fields(o)...)
				}
			}
			cmd := exec.Command("git", append(append(full, "-C", project), args...)...)
			cmd.Env = os.Environ()
			if fault != "GIT_NO_LAZY_FETCH=1" {
				cmd.Env = append(cmd.Env, "GIT_NO_LAZY_FETCH=1")
			}
			_ = cmd.Run()
		}
	}
	const first = `^the chat's first hook ran "git .*", not only identity's rev-parse$`
	for _, c := range []struct {
		name  string
		event string
		at    point
		act   func(*turn)
		want  []string
	}{
		{"git fetch at a chat's start", "SessionStart", atBriefed, run("git", "fetch", "origin"), []string{
			`^SessionStart/startup: git fetch, which is not one of the reads a hook may run: "git fetch origin"$`,
			`^the chat's first hook ran "git fetch origin", not only identity's rev-parse$`,
		}},
		{"git push on a reply", "Stop", atStored, run("git", "push"), []string{
			`^Stop/: git push, which is not one of the reads a hook may run: "git push"$`,
			`^Stop/ ran \["git push"\]$`,
		}},
		{"a git read on a prompt", "UserPromptSubmit", atStored, read("", "rev-parse", "HEAD"), []string{`^UserPromptSubmit/ ran \["git .* rev-parse HEAD"\]$`}},
		{"a git status that may write the index", "SessionStart", atBriefed, read("--no-optional-locks", "status", "--porcelain=v2"), []string{
			`^SessionStart/startup: git without --no-optional-locks, which lets it write the index: "git .*status --porcelain=v2"$`, first,
		}},
		{"a git read that may run the repository's monitor", "SessionStart", atBriefed, read("-c core.fsmonitor=false", "rev-parse", "HEAD"), []string{
			`^SessionStart/startup: git without -c core\.fsmonitor=false, which lets it start a program the repository names: `, first,
		}},
		{"a git read that may run the repository's hooks", "SessionStart", atBriefed, read("-c core.hooksPath=/dev/null", "rev-parse", "HEAD"), []string{
			`^SessionStart/startup: git without -c core\.hooksPath=/dev/null, which lets it start a program the repository names: `, first,
		}},
		{"a git read that may fetch what it lacks", "SessionStart", atBriefed, read("GIT_NO_LAZY_FETCH=1", "rev-parse", "HEAD"), []string{
			`^SessionStart/startup: git without GIT_NO_LAZY_FETCH=1, which lets a partial clone fetch what it lacks: `, first,
		}},
		{"a git read given a program to run", "SessionStart", atBriefed, read("", "-c", "core.sshCommand=/bin/false", "rev-parse", "HEAD"), []string{
			`^SessionStart/startup: git -c core\.sshCommand=/bin/false, which may start a program or write: `, first,
		}},
		{"a git log that checks signatures", "SessionStart", atBriefed, read("", "log", "--show-signature", "-1"), []string{
			`^SessionStart/startup: git log --show-signature, which starts gpg: `, first,
		}},
		{"curl at a chat's start", "SessionStart", atGuarded, run("curl", "https://example.invalid"), []string{
			`^SessionStart/startup: a start of curl: "curl https://example.invalid"$`,
			`^the chat's first hook ran "curl https://example.invalid", not only identity's rev-parse$`,
		}},
		{"a host's CLI at the end", "SessionEnd", atStored, run("claude", "-p", "summarize"), []string{
			`^SessionEnd/: a start of claude: "claude -p summarize"$`,
			`^SessionEnd/ ran \["claude -p summarize"\]$`,
		}},
		{"another program on a reply", "Stop", atStored, run("uname", "-s"), []string{
			`^Stop/: a start of uname: "uname -s"$`,
			`^Stop/ ran \["uname -s"\]$`,
		}},
		{"a job before the output", "Stop", atStored, func(t *turn) { t.env.spawn("update", "--early") }, []string{`^Stop/ started \["update" "--early"\] before its output was written: "" of "\{\}"$`}},
	} {
		t.Run(c.name, func(t *testing.T) {
			env, p := setup(t)
			project = p.Cwd
			host := "codex"
			pr := newProbe(t, &p, nil)
			fired := false
			pr.act = func(at point, tn *turn) {
				if at == c.at && tn.event == c.event && !fired {
					fired = true
					c.act(tn)
				}
			}
			reachWiki(t, host, &env, p)
			log := standIns(t)
			runs := runChat(t, host, p, env, aChat, log)
			problems := append(checkStarts(runs, notGitProject, false), onlyIdentity(runs)...)
			if !fired {
				t.Fatal("the seeded handler never ran")
			}
			if len(problems) == 0 {
				t.Fatal("the checks reported nothing")
			}
			for _, problem := range problems {
				if !slices.ContainsFunc(c.want, func(w string) bool { return regexp.MustCompile(w).MatchString(problem) }) {
					t.Errorf("a problem the seed does not explain: %q", problem)
				}
			}
			for _, w := range c.want {
				if !slices.ContainsFunc(problems, regexp.MustCompile(w).MatchString) {
					t.Errorf("no problem says %q: %q", w, problems)
				}
			}
		})
	}
}

// readGit names the git commands a hook may run in a git project, each for
// what it reads: identity's root, branch and remote, the brief's state of
// the work, and how far the code a system page cites has moved. None writes
// or reaches a remote: config only with --get.
var readGit = map[string]string{
	"rev-parse":  "the project's root, its branch and HEAD, and a path in .git",
	"config":     "the remote's address, with --get only",
	"var":        "where the system's git config is",
	"status":     "the brief's state of the work",
	"rev-list":   "commits since a page or a brief, and stashes",
	"log":        "the subjects of the newest commits",
	"cat-file":   "whether a page's commit is there",
	"ls-tree":    "whether a page's paths are there",
	"merge-base": "whether a page's commit is on the branch",
}

// readObjects are the commands of readGit that read the repository's
// objects, which a partial clone may lack and fetch.
var readObjects = []string{"status", "rev-list", "log", "cat-file", "ls-tree", "merge-base"}

// readOptions are the options a read may give git before its command, but
// -c and -C, each with its reason.
var readOptions = map[string]string{
	"--no-optional-locks": "takes no lock it may do without, so a status never writes the index",
	"--literal-pathspecs": "reads a cited path as itself, never as a pattern",
	"--no-lazy-fetch":     "fetches nothing a partial clone lacks, in a git that ignores the variable",
}

// readConfig are the settings a read may give git with -c: those that turn
// off what would start a program the repository or the user names.
var readConfig = []string{"core.fsmonitor=false", "core.hooksPath=/dev/null", "log.showSignature=false"}

// gitCommand is the command of a git command line, after its options: -c
// and -C take a value.
func gitCommand(args []string) (string, []string) {
	for i := 0; i < len(args); i++ {
		switch a := args[i]; {
		case a == "-c" || a == "-C":
			i++
		case strings.HasPrefix(a, "-"):
		default:
			return a, args[i+1:]
		}
	}
	return "", nil
}

// readOnly says what of a git start the hook may not run, or "" when it
// only reads: one of readGit's commands, config only with --get and no
// --show-signature, which starts gpg; no option but -C and those of
// readOptions, and no -c but those of readConfig; with --no-optional-locks,
// fsmonitor and hooks off, and GIT_NO_LAZY_FETCH=1 in env, the environment
// the stand-in logged; and in a project that is a partial clone, or may be,
// --no-lazy-fetch on a command that reads objects too.
func readOnly(line, env string, partial bool) string {
	name, rest, _ := strings.Cut(line, " ")
	if name != "git" {
		return "a start of " + name
	}
	args := strings.Fields(rest)
	command, after := gitCommand(args)
	switch _, ok := readGit[command]; {
	case !ok:
		return "git " + command + ", which is not one of the reads a hook may run"
	case command == "config" && !slices.Contains(after, "--get"):
		return "git config without --get"
	case slices.Contains(after, "--show-signature"):
		return "git " + command + " --show-signature, which starts gpg"
	}
	options := args[:len(args)-len(after)-1]
	var config []string
	for i := 0; i < len(options); i++ {
		switch o := options[i]; {
		case (o == "-c" || o == "-C") && i+1 < len(options):
			if o == "-c" {
				config = append(config, options[i+1])
			}
			i++
		case readOptions[o] == "":
			return "git " + o + ", which is not an option of a read"
		}
	}
	for _, c := range config {
		if !slices.Contains(readConfig, c) {
			return "git -c " + c + ", which may start a program or write"
		}
	}
	if !slices.Contains(options, "--no-optional-locks") {
		return "git without --no-optional-locks, which lets it write the index"
	}
	for _, want := range readConfig[:2] {
		if !slices.Contains(config, want) {
			return "git without -c " + want + ", which lets it start a program the repository names"
		}
	}
	if !slices.Contains(strings.Fields(env), "GIT_NO_LAZY_FETCH=1") {
		return "git without GIT_NO_LAZY_FETCH=1, which lets a partial clone fetch what it lacks"
	}
	if partial && slices.Contains(readObjects, command) && !slices.Contains(options, "--no-lazy-fetch") {
		return "git " + command + " without --no-lazy-fetch in a partial clone, which a git before 2.44 needs to fetch nothing"
	}
	return ""
}

// The check of what git a hook may run passes a read as the program's
// reads run, and fails a write, a fetch, a push, a config write, a read
// that may write the index, start a program the repository or the user
// names, check a signature or fetch what a partial clone lacks.
func TestReadOnlyGitIsTold(t *testing.T) {
	const hardened = "git -c core.fsmonitor=false -c core.hooksPath=/dev/null --no-optional-locks"
	const env = "GIT_OPTIONAL_LOCKS=0 GIT_NO_LAZY_FETCH=1"
	for _, c := range []struct {
		line, env string
		partial   bool
		want      bool
	}{
		{hardened + " -C /p rev-parse --show-toplevel", env, false, true},
		{hardened + " -C /p config --get remote.x.url", env, false, true},
		{"git -c core.fsmonitor=false -c core.hooksPath=/dev/null -c log.showSignature=false --no-optional-locks --literal-pathspecs -C /p log -1", env, false, true},
		{hardened + " --no-lazy-fetch -C /p status --porcelain=v2", env, true, true},
		{hardened + " -C /p rev-parse HEAD", env, true, true},
		{hardened + " -C /p status --porcelain=v2", env, true, false},
		{hardened + " -C /p rev-list --count HEAD", env, true, false},
		{hardened + " -C /p status --porcelain=v2", "GIT_OPTIONAL_LOCKS=0 GIT_NO_LAZY_FETCH=", false, false},
		{"git -C /p status --porcelain=v2", env, false, false},
		{"git -c core.hooksPath=/dev/null --no-optional-locks -C /p status", env, false, false},
		{"git -c core.fsmonitor=false --no-optional-locks -C /p rev-parse HEAD", env, false, false},
		{"git -c core.fsmonitor=/tmp/evil -c core.hooksPath=/dev/null --no-optional-locks -C /p status", env, false, false},
		{hardened + " -c core.hooksPath=/tmp/hooks -C /p rev-parse HEAD", env, false, false},
		{hardened + " -c core.sshCommand=/tmp/x -C /p rev-list --count HEAD", env, false, false},
		{hardened + " -c gpg.program=/tmp/x -C /p log -1", env, false, false},
		{hardened + " -C /p log --show-signature -1", env, false, false},
		{hardened + " --exec-path=/tmp -C /p rev-parse HEAD", env, false, false},
		{hardened + " -C /p fetch origin", env, false, false},
		{hardened + " -C /p push", env, false, false},
		{hardened + " -C /p commit -m x", env, false, false},
		{hardened + " -C /p config user.name x", env, false, false},
		{hardened + " -C /p ls-remote origin", env, false, false},
		{"ssh host", "", false, false},
		{"git -C /p", env, false, false},
	} {
		if got := readOnly(c.line, c.env, c.partial) == ""; got != c.want {
			t.Errorf("%q with %q, partial %v, read as read-only %v, want %v: %q", c.line, c.env, c.partial, got, c.want, readOnly(c.line, c.env, c.partial))
		}
	}
}

// gitProject makes the project at dir a git repository whose remote is the
// repository the system pages cite, with one commit, and returns that
// commit, short; with partial, a partial clone of that remote, as its
// config says, whose objects are all there.
func gitProject(t *testing.T, dir string, partial bool) string {
	t.Helper()
	git(t, dir, "init", "--quiet", "--initial-branch=main")
	git(t, dir, "-c", "user.name=Fixture", "-c", "user.email=fixture@example.invalid", "-c", "commit.gpgsign=false", "commit", "--allow-empty", "--quiet", "-m", "fixture")
	git(t, dir, "remote", "add", "origin", "https://github.com/acme/ledger.git")
	if partial {
		git(t, dir, "config", "core.repositoryformatversion", "1")
		git(t, dir, "config", "extensions.partialClone", "origin")
		git(t, dir, "config", "remote.origin.promisor", "true")
		git(t, dir, "config", "remote.origin.partialclonefilter", "blob:none")
	}
	head, err := exec.Command("git", "-C", dir, "rev-parse", "--short", "HEAD").Output()
	if err != nil {
		t.Fatal(err)
	}
	return strings.TrimSpace(string(head))
}

// gitChat runs aChat of host in a git project, a partial clone or not,
// whose remote is the repository two system pages cite, and holds its
// starts to what a hook may start: git at a chat's start, only to read,
// and nothing else.
func gitChat(t *testing.T, host string, partial bool) []reachRun {
	t.Helper()
	env, p := setup(t)
	head := gitProject(t, p.Cwd, partial)
	// The pages cite the project's commit, and the code status maintain
	// saved is of another, so the brief counts how far the pages' code
	// moved itself.
	reachWikiAt(t, host, &env, p, head)
	plantStatus(t, env, map[string]int{"ledger": 14}, "9c1d2e3")
	log := standIns(t)
	runs := runChat(t, host, p, env, aChat, log)
	for _, problem := range checkStarts(runs, func(i int) bool { return aChat[i].event == "SessionStart" }, partial) {
		t.Error(problem)
	}
	var commands []string
	for _, r := range runs {
		for _, line := range r.started {
			command, _ := gitCommand(strings.Fields(strings.TrimPrefix(line, "git ")))
			if !slices.Contains(commands, command) {
				commands = append(commands, command)
			}
		}
	}
	for _, want := range []string{"rev-parse", "config", "status", "rev-list"} {
		if !slices.Contains(commands, want) {
			t.Errorf("the starts ran git %q, never %s, so the brief of a git project was not run", commands, want)
		}
	}
	if !strings.Contains(runs[firstStart].out, "Ledger: Keeps the double-entry books (code unchanged since 2026-10-28)") {
		t.Errorf("the brief did not count the pages' code: %q", runs[firstStart].out)
	}
	return runs
}

// In a git project whose remote is the repository two system pages cite,
// the chat's starts run git, to resolve the project and to brief it, with
// the code of those pages counted, but only commands that read, hardened
// so that none writes the index, starts a program the repository or the
// user names, or fetches, and no other program; no other event runs git,
// the prompts with their pointers and recall included.
func TestHooksInAGitProjectOnlyReadGit(t *testing.T) {
	for _, host := range hosts {
		t.Run(host, func(t *testing.T) {
			gitChat(t, host, false)
		})
	}
}

// In a project that is a partial clone, the git a hook runs to read the
// repository's objects asks git, by its option as well as its environment,
// to fetch nothing the clone lacks.
func TestHooksInAPartialCloneFetchNothing(t *testing.T) {
	for _, host := range hosts {
		t.Run(host, func(t *testing.T) {
			runs := gitChat(t, host, true)
			lazy := 0
			for _, r := range runs {
				for _, line := range r.started {
					if slices.Contains(strings.Fields(line), "--no-lazy-fetch") {
						lazy++
					}
				}
			}
			if lazy == 0 {
				t.Error("no git a hook ran was told --no-lazy-fetch: the project was not read as a partial clone")
			}
		})
	}
}

// The search index is never read or written by a hook: the same chat gives
// the same output, starts the same jobs and stores the same bytes with the
// index whole, damaged and gone, and leaves the index's files as they were,
// their times included.
func TestHooksIgnoreTheSearchIndex(t *testing.T) {
	for _, host := range hosts {
		t.Run(host, func(t *testing.T) {
			for _, problem := range indexProblems(t, host, "test-session") {
				t.Error(problem)
			}
		})
	}
}

// A hook that writes into the index, or stores what depends on it, fails
// the comparison: a handler of the hook's list, registered here as a later
// package would register one, does either at the chat's reply.
func TestAHookThatUsesTheIndexIsCaught(t *testing.T) {
	for _, c := range []struct {
		name string
		act  func(*turn)
		want []string
	}{
		{"one that writes the index", func(tn *turn) {
			_ = os.WriteFile(filepath.Join(tn.env.Dirs.SearchIndex(), "general", "manifest.json"), []byte("{}"), 0o600)
		}, []string{`^whole: the index's files changed`, `^damaged: the index's files changed`}},
		{"one that stores what it finds there", func(tn *turn) {
			_, err := os.Stat(filepath.Join(tn.env.Dirs.SearchIndex(), "general"))
			_ = os.WriteFile(filepath.Join(tn.env.Dirs.State, "seen"), []byte(fmt.Sprint(err == nil)), 0o600)
		}, []string{`^gone: /\.local/state/eva-md/seen differs from what the hooks stored with the index whole$`}},
	} {
		t.Run(c.name, func(t *testing.T) {
			_, p := setup(t)
			pr := newProbe(t, &p, nil)
			pr.act = func(at point, tn *turn) {
				if at == atStored && tn.event == "Stop" {
					c.act(tn)
				}
			}
			problems := indexProblems(t, "codex", p.Session)
			for _, problem := range problems {
				if !slices.ContainsFunc(c.want, func(w string) bool { return regexp.MustCompile(w).MatchString(problem) }) {
					t.Errorf("a problem the seed does not explain: %q", problem)
				}
			}
			for _, w := range c.want {
				if !slices.ContainsFunc(problems, regexp.MustCompile(w).MatchString) {
					t.Errorf("no problem says %q: %q", w, problems)
				}
			}
		})
	}
}

// indexProblems runs aChat of host, as session, three times in one home
// built again each time: with the search index whole, damaged and gone. It
// returns a problem for each run whose index changed, and for each output,
// job or stored file of the second and third that differs from the first.
func indexProblems(t *testing.T, host, session string) []string {
	t.Helper()
	type result struct {
		runs   []reachRun
		stored map[string]string
	}
	var problems []string
	// Each variant builds the same fixture in the same home, so paths
	// written into what is stored are the same too.
	home := t.TempDir()
	results := map[string]result{}
	for _, variant := range []string{"whole", "damaged", "gone"} {
		clearFolder(t, home)
		env, p := setup(t)
		t.Setenv("HOME", home)
		t.Setenv("GIT_CONFIG_GLOBAL", filepath.Join(home, "gitconfig"))
		t.Setenv("TMPDIR", filepath.Join(home, "tmp"))
		if err := os.MkdirAll(filepath.Join(home, "project"), 0o700); err != nil {
			t.Fatal(err)
		}
		env.Dirs.Config = filepath.Join(home, ".config", "eva-md")
		env.Dirs.State = filepath.Join(home, ".local", "state", "eva-md")
		p.Cwd, p.Session = filepath.Join(home, "project"), session
		env.elapsed = func(time.Time) time.Duration { return 20 * time.Millisecond }
		reachWiki(t, host, &env, p)
		index := env.Dirs.SearchIndex()
		switch variant {
		case "damaged":
			damage(t, index)
		case "gone":
			if err := os.RemoveAll(index); err != nil {
				t.Fatal(err)
			}
		}
		before, stored := indexOf(t, index), contents(t, home, index)
		runs := runChat(t, host, p, env, aChat, "")
		if after := indexOf(t, index); !mapsEqual(before, after) {
			problems = append(problems, fmt.Sprintf("%s: the index's files changed: before %v, after %v", variant, before, after))
		}
		results[variant] = result{runs, withoutSigs(t, changed(stored, contents(t, home, index)))}
	}
	whole := results["whole"]
	for _, variant := range []string{"damaged", "gone"} {
		got := results[variant]
		for i, r := range got.runs {
			w := whole.runs[i]
			if r.out != w.out || fmt.Sprint(r.jobs) != fmt.Sprint(w.jobs) {
				problems = append(problems, fmt.Sprintf("%s: %s/%s gave %q and started %q; with the index whole %q and %q", variant, r.step.event, r.step.source, r.out, r.jobs, w.out, w.jobs))
			}
		}
		for _, name := range slices.Sorted(maps.Keys(whole.stored)) {
			if got.stored[name] != whole.stored[name] {
				problems = append(problems, fmt.Sprintf("%s: %s differs from what the hooks stored with the index whole", variant, name))
			}
		}
		for _, name := range slices.Sorted(maps.Keys(got.stored)) {
			if _, ok := whole.stored[name]; !ok {
				problems = append(problems, fmt.Sprintf("%s: the hooks stored %s, which they did not with the index whole", variant, name))
			}
		}
	}
	if len(whole.stored) < 5 || !hasJob(whole.runs, "update") {
		t.Fatalf("the chat changed %d files and started no update: too little for the comparison to mean anything", len(whole.stored))
	}
	return problems
}

// indexOf is every file and folder of the search index at index, by its
// path, with its mode, size and time, and its bytes, or nothing when the
// index is gone.
func indexOf(t *testing.T, index string) map[string]string {
	t.Helper()
	if _, err := os.Lstat(index); errors.Is(err, fs.ErrNotExist) {
		return nil
	}
	out := map[string]string{}
	for path, e := range tree(t, index) {
		out[path] = fmt.Sprint(e)
	}
	for path, data := range contents(t, index, "") {
		out[index+path] += " " + data
	}
	return out
}

// changed is what after holds that before did not, or held otherwise, and
// each name before held that after does not, as gone.
func changed(before, after map[string]string) map[string]string {
	out := map[string]string{}
	for name, data := range after {
		if old, ok := before[name]; !ok || old != data {
			out[name] = data
		}
	}
	for name := range before {
		if _, ok := after[name]; !ok {
			out[name] = "(gone)"
		}
	}
	return out
}

// withoutSigs is stored with the signature and checksum of each identity
// cache entry left out: the signature names the inodes of the folders it
// looked at, which a home built again does not keep.
func withoutSigs(t *testing.T, stored map[string]string) map[string]string {
	t.Helper()
	for name, data := range stored {
		if !strings.Contains(name, "/cache/identity/") {
			continue
		}
		var entry map[string]any
		if err := json.Unmarshal([]byte(data), &entry); err != nil {
			t.Fatalf("%s: %v", name, err)
		}
		if entry["sig"] == nil || entry["sum"] == nil {
			t.Fatalf("%s has no signature or checksum: %q", name, data)
		}
		delete(entry, "sig")
		delete(entry, "sum")
		kept, err := json.Marshal(entry)
		if err != nil {
			t.Fatal(err)
		}
		stored[name] = string(kept)
	}
	return stored
}

// clearFolder empties dir, keeping dir.
func clearFolder(t *testing.T, dir string) {
	t.Helper()
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	for _, e := range entries {
		if err := os.RemoveAll(filepath.Join(dir, e.Name())); err != nil {
			t.Fatal(err)
		}
	}
}

// damage flips a byte in the middle of every file under dir, which fails
// the checks of the manifest and of every segment.
func damage(t *testing.T, dir string) {
	t.Helper()
	n := 0
	err := filepath.WalkDir(dir, func(path string, d fs.DirEntry, err error) error {
		if err != nil || !d.Type().IsRegular() {
			return err
		}
		data, err := os.ReadFile(path)
		if err != nil || len(data) == 0 {
			return err
		}
		data[len(data)/2] ^= 0x5a
		n++
		return os.WriteFile(path, data, 0o600)
	})
	if err != nil || n < 2 {
		t.Fatalf("damaged %d files of the index: %v", n, err)
	}
}

// contents reads every regular file under root but those under skip, by
// its path from root, and every link, by its target.
func contents(t *testing.T, root, skip string) map[string]string {
	t.Helper()
	out := map[string]string{}
	err := filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		switch {
		case err != nil:
			return err
		case skip != "" && path == skip:
			return filepath.SkipDir
		case d.Type()&fs.ModeSymlink != 0:
			target, err := os.Readlink(path)
			out[path[len(root):]] = "-> " + target
			return err
		case !d.Type().IsRegular():
			return nil
		}
		data, err := os.ReadFile(path)
		out[path[len(root):]] = string(data)
		return err
	})
	if err != nil && !errors.Is(err, fs.ErrNotExist) {
		t.Fatal(err)
	}
	return out
}

func mapsEqual[V comparable](a, b map[string]V) bool {
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

// hookPackage is the import path of the package the hooks run.
const hookPackage = "github.com/heyNag/eva-md/internal/hook"

// connects says whether a standard package can open a connection: net and
// every package under it but net/url and net/netip, which only parse, and
// the packages beside it that dial. The standard library is followed too,
// so a package that reaches net through another is found by its chain.
func connects(path string) bool {
	switch path {
	case "net/url", "net/netip":
		return false
	case "net", "crypto/tls", "log/syslog", "expvar":
		return true
	}
	return strings.HasPrefix(path, "net/")
}

// linker resolves import paths to folders, for a module, the modules it
// requires and Go's standard library, and reads each folder's imports for
// one build context.
type linker struct {
	ctx     build.Context
	modules map[string]string // module path to folder
	goroot  string
}

// dir is the folder of an import path, or "" when none holds it.
func (l linker) dir(path string) string {
	best := ""
	for m := range l.modules {
		if (path == m || strings.HasPrefix(path, m+"/")) && len(m) > len(best) {
			best = m
		}
	}
	if best != "" {
		return filepath.Join(l.modules[best], filepath.FromSlash(strings.TrimPrefix(path, best)))
	}
	for _, d := range []string{filepath.Join(l.goroot, "src", filepath.FromSlash(path)), filepath.Join(l.goroot, "src", "vendor", filepath.FromSlash(path))} {
		if info, err := os.Stat(d); err == nil && info.IsDir() {
			return d
		}
	}
	return ""
}

// inModule says whether path is a package of the module or of a module it
// requires, not of Go's own library.
func (l linker) inModule(path string) bool {
	for m := range l.modules {
		if path == m || strings.HasPrefix(path, m+"/") {
			return true
		}
	}
	return false
}

// socketPackages are the packages whose calls of socketNames open or
// connect a socket, by import path.
var socketPackages = []string{"syscall", "golang.org/x/sys/unix"}

// socketNames are the calls, and the numbers of the system's calls, that
// open or connect a socket.
var socketNames = []string{"Socket", "Socketpair", "Connect", "SYS_SOCKET", "SYS_SOCKETPAIR", "SYS_CONNECT"}

// socketCalls lists each use, in the Go files of pkg in dir that build,
// of a name of socketNames from a package of socketPackages, as
// <package>.<name>.
func socketCalls(dir string, pkg *build.Package) []string {
	var out []string
	fset := token.NewFileSet()
	for _, name := range append(slices.Clone(pkg.GoFiles), pkg.CgoFiles...) {
		f, err := parser.ParseFile(fset, filepath.Join(dir, name), nil, parser.SkipObjectResolution)
		if err != nil {
			out = append(out, name+" does not parse")
			continue
		}
		names := map[string]string{} // the name a socket package is known by in f
		for _, spec := range f.Imports {
			path := strings.Trim(spec.Path.Value, `"`)
			if !slices.Contains(socketPackages, path) {
				continue
			}
			names[path[strings.LastIndex(path, "/")+1:]] = path
			if spec.Name != nil {
				delete(names, path[strings.LastIndex(path, "/")+1:])
				names[spec.Name.Name] = path
			}
		}
		ast.Inspect(f, func(n ast.Node) bool {
			sel, ok := n.(*ast.SelectorExpr)
			if !ok {
				return true
			}
			if id, ok := sel.X.(*ast.Ident); ok && names[id.Name] != "" && slices.Contains(socketNames, sel.Sel.Name) {
				if call := names[id.Name] + "." + sel.Sel.Name; !slices.Contains(out, call) {
					out = append(out, call)
				}
			}
			return true
		})
	}
	return out
}

// reach follows start's imports, not its tests', through every package it
// reaches, and gives the chain of imports to each package that can open a
// connection: one of Go's library that connects names, and one of the
// module or its dependencies that uses cgo or opens a socket through the
// system's calls. It gives a problem for an import no folder holds.
func (l linker) reach(start string) []string {
	var found []string
	seen := map[string]bool{start: true}
	type node struct {
		path  string
		chain []string
	}
	queue := []node{{start, []string{start}}}
	for len(queue) > 0 {
		n := queue[0]
		queue = queue[1:]
		dir := l.dir(n.path)
		if dir == "" {
			found = append(found, strings.Join(n.chain, " -> ")+": no folder holds it")
			continue
		}
		pkg, err := l.ctx.ImportDir(dir, 0)
		var none *build.NoGoError
		if err != nil && !errors.As(err, &none) {
			found = append(found, strings.Join(n.chain, " -> ")+": "+err.Error())
			continue
		}
		// Go's own library opens sockets only in the packages connects
		// names; any other package does it through the system's calls or
		// through C.
		if l.inModule(n.path) {
			if slices.Contains(pkg.Imports, "C") {
				found = append(found, strings.Join(n.chain, " -> ")+": cgo, through which C can open a socket")
			}
			for _, call := range socketCalls(dir, pkg) {
				found = append(found, strings.Join(n.chain, " -> ")+": "+call+", which opens a socket")
			}
		}
		for _, imp := range pkg.Imports {
			if imp == "C" || imp == "unsafe" || seen[imp] {
				continue
			}
			seen[imp] = true
			chain := append(slices.Clone(n.chain), imp)
			if connects(imp) {
				found = append(found, strings.Join(chain, " -> "))
				continue
			}
			queue = append(queue, node{imp, chain})
		}
	}
	slices.Sort(found)
	return found
}

// requires reads the modules a go.mod requires, and its replacements, as
// folders: a local replacement's, or the module cache's.
func requires(t *testing.T, root, modcache string) (string, map[string]string) {
	t.Helper()
	f, err := os.Open(filepath.Join(root, "go.mod"))
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	self := ""
	versions, replaced := map[string]string{}, map[string]string{}
	inRequire := false
	s := bufio.NewScanner(f)
	for s.Scan() {
		line := strings.TrimSpace(s.Text())
		if i := strings.Index(line, "//"); i >= 0 {
			line = strings.TrimSpace(line[:i])
		}
		fields := strings.Fields(line)
		switch {
		case len(fields) == 0:
		case fields[0] == "module" && len(fields) == 2:
			self = fields[1]
		case line == "require (":
			inRequire = true
		case inRequire && line == ")":
			inRequire = false
		case inRequire && len(fields) == 2:
			versions[fields[0]] = fields[1]
		case fields[0] == "require" && len(fields) == 3:
			versions[fields[1]] = fields[2]
		case fields[0] == "replace":
			_, to, ok := strings.Cut(line, "=>")
			from := fields[1]
			if to = strings.TrimSpace(to); !ok || to == "" {
				t.Fatalf("go.mod: a replace this test can't read: %q", line)
			}
			target := strings.Fields(to)
			switch {
			case strings.HasPrefix(target[0], "./") || strings.HasPrefix(target[0], "../") || filepath.IsAbs(target[0]):
				replaced[from] = filepath.Join(root, target[0])
			case len(target) == 2:
				replaced[from] = filepath.Join(modcache, escapeModule(target[0])+"@"+target[1])
			default:
				t.Fatalf("go.mod: a replace this test can't read: %q", line)
			}
		}
	}
	if err := s.Err(); err != nil {
		t.Fatal(err)
	}
	if self == "" {
		t.Fatal("go.mod names no module")
	}
	modules := map[string]string{self: root}
	for m, v := range versions {
		modules[m] = filepath.Join(modcache, escapeModule(m)+"@"+v)
		if dir, ok := replaced[m]; ok {
			modules[m] = dir
		}
	}
	return self, modules
}

// escapeModule writes a module path as the module cache names its folder:
// an upper-case letter as '!' and the letter in lower case.
func escapeModule(path string) string {
	var b strings.Builder
	for _, r := range path {
		if 'A' <= r && r <= 'Z' {
			b.WriteByte('!')
			r += 'a' - 'A'
		}
		b.WriteRune(r)
	}
	return b.String()
}

// moduleCache is the module cache go test filled: GOMODCACHE, or the first
// GOPATH's pkg/mod.
func moduleCache() string {
	if dir := os.Getenv("GOMODCACHE"); dir != "" {
		return dir
	}
	gopath := os.Getenv("GOPATH")
	if gopath == "" {
		gopath = build.Default.GOPATH
	}
	return filepath.Join(filepath.SplitList(gopath)[0], "pkg", "mod")
}

// linkers are a linker for each system and architecture eva-md is built
// for, with cgo off and on, for the module at root.
func linkers(t *testing.T, root, modcache string) (string, []linker) {
	t.Helper()
	self, modules := requires(t, root, modcache)
	for m, dir := range modules {
		if info, err := os.Stat(dir); err != nil || !info.IsDir() {
			t.Fatalf("module %s is not in %s (set GOMODCACHE to the module cache go test uses): %v", m, dir, err)
		}
	}
	goroot := build.Default.GOROOT
	if info, err := os.Stat(filepath.Join(goroot, "src", "net")); err != nil || !info.IsDir() {
		t.Fatalf("Go's library is not at GOROOT %q: %v", goroot, err)
	}
	var out []linker
	for _, target := range []struct{ goos, goarch string }{{"darwin", "arm64"}, {"darwin", "amd64"}, {"linux", "amd64"}} {
		for _, cgo := range []bool{false, true} {
			ctx := build.Default
			ctx.GOOS, ctx.GOARCH, ctx.CgoEnabled, ctx.GOROOT = target.goos, target.goarch, cgo, goroot
			ctx.BuildTags = nil
			out = append(out, linker{ctx: ctx, modules: modules, goroot: goroot})
		}
	}
	return self, out
}

// No package the hook's package reaches, by its imports followed through
// this module, its two dependencies and Go's own library, on any system it
// is built for, imports a package that can open a connection. The imports
// are read from the source, not from a command's listing.
func TestHookLinksNothingThatConnects(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	root, err := filepath.Abs(filepath.Join("..", ".."))
	if err != nil {
		t.Fatal(err)
	}
	self, ls := linkers(t, root, moduleCache())
	if self+"/internal/hook" != hookPackage {
		t.Fatalf("the module is %s", self)
	}
	for _, l := range ls {
		if found := l.reach(hookPackage); len(found) > 0 {
			t.Errorf("%s/%s, cgo %v: the hook reaches %q", l.ctx.GOOS, l.ctx.GOARCH, l.ctx.CgoEnabled, found)
		}
	}
	// The walk read what it should: the library's codec and the config's
	// parser, through their modules, and some of Go's library.
	l := ls[0]
	reached := l.reached(hookPackage)
	for _, want := range []string{"github.com/okf-memory/okf-agent-memory", "github.com/BurntSushi/toml", "os/exec", "net/url"} {
		if !slices.ContainsFunc(reached, func(p string) bool { return p == want || strings.HasPrefix(p, want+"/") }) {
			t.Errorf("the walk never reached %s: %d packages", want, len(reached))
		}
	}
}

// reached lists every package start reaches, for the test's own check that
// the walk read the modules.
func (l linker) reached(start string) []string {
	seen := map[string]bool{start: true}
	queue := []string{start}
	for len(queue) > 0 {
		path := queue[0]
		queue = queue[1:]
		dir := l.dir(path)
		if dir == "" {
			continue
		}
		pkg, _ := l.ctx.ImportDir(dir, 0)
		if pkg == nil {
			continue
		}
		for _, imp := range pkg.Imports {
			if !seen[imp] && imp != "C" && imp != "unsafe" {
				seen[imp] = true
				queue = append(queue, imp)
			}
		}
	}
	return slices.Sorted(func(yield func(string) bool) {
		for p := range seen {
			if !yield(p) {
				return
			}
		}
	})
}

// The link check finds a package that connects wherever the hook reaches
// it: imported by a package of the module, by a required module's, or by
// Go's own library through another package, and a package of the module or
// a required module that opens a socket through the system's calls or uses
// cgo; and passes what only parses.
func TestALinkThatConnectsIsFound(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	for _, c := range []struct {
		name  string
		files map[string]string
		want  []string
	}{
		{"net/http in a package the hook reaches", map[string]string{
			"m/a/a.go": "package a\n\nimport (\n\t_ \"example.com/lib\"\n\t_ \"net/http\"\n)\n",
		}, []string{"example.com/m/hook -> example.com/m/a -> net/http"}},
		{"net in the required module", map[string]string{
			"lib/lib.go": "package lib\n\nimport _ \"net\"\n",
		}, []string{"example.com/m/hook -> example.com/m/a -> example.com/lib -> net"}},
		{"net reached through Go's library", map[string]string{
			"m/a/a.go": "package a\n\nimport (\n\t_ \"example.com/lib\"\n\t_ \"mime/multipart\"\n)\n",
		}, []string{"example.com/m/hook -> example.com/m/a -> mime/multipart -> net/textproto"}},
		{"crypto/tls on one system only", map[string]string{
			"m/a/a_darwin.go": "package a\n\nimport _ \"crypto/tls\"\n",
		}, []string{"example.com/m/hook -> example.com/m/a -> crypto/tls"}},
		{"a socket through the system's calls", map[string]string{
			"m/a/a.go": "package a\n\nimport (\n\t_ \"example.com/lib\"\n\tsys \"syscall\"\n)\n\nfunc open() { fd, _ := sys.Socket(sys.AF_INET, sys.SOCK_STREAM, 0); _ = sys.Connect(fd, nil) }\n",
		}, []string{
			"example.com/m/hook -> example.com/m/a: syscall.Connect, which opens a socket",
			"example.com/m/hook -> example.com/m/a: syscall.Socket, which opens a socket",
		}},
		{"a socket through the system's call by its number", map[string]string{
			"lib/lib.go": "package lib\n\nimport \"syscall\"\n\nvar _, _, _ = syscall.Syscall(syscall.SYS_CONNECT, 0, 0, 0)\n",
		}, []string{"example.com/m/hook -> example.com/m/a -> example.com/lib: syscall.SYS_CONNECT, which opens a socket"}},
		{"cgo in the required module", map[string]string{
			"lib/c.go": "package lib\n\n// #include <sys/socket.h>\nimport \"C\"\n\nvar _ = C.socket\n",
		}, []string{"example.com/m/hook -> example.com/m/a -> example.com/lib: cgo, through which C can open a socket"}},
		{"an import no folder holds", map[string]string{
			"m/a/a.go": "package a\n\nimport (\n\t_ \"example.com/lib\"\n\t_ \"example.com/nowhere\"\n)\n",
		}, []string{"example.com/m/hook -> example.com/m/a -> example.com/nowhere: no folder holds it"}},
		{"only what parses", map[string]string{}, nil},
	} {
		t.Run(c.name, func(t *testing.T) {
			dir := t.TempDir()
			files := map[string]string{
				"m/go.mod":       "module example.com/m\n\ngo 1.26.0\n\nrequire example.com/lib v1.0.0\n\nreplace example.com/lib => ../lib\n",
				"m/hook/hook.go": "package hook\n\nimport (\n\t_ \"example.com/m/a\"\n\t_ \"net/url\"\n)\n",
				"m/a/a.go":       "package a\n\nimport (\n\t_ \"example.com/lib\"\n\t_ \"net/netip\"\n\t_ \"os/exec\"\n)\n",
				"m/a/a_test.go":  "package a\n\nimport _ \"net/http/httptest\"\n",
				"lib/go.mod":     "module example.com/lib\n",
				"lib/lib.go":     "package lib\n\nimport _ \"strings\"\n",
			}
			maps.Copy(files, c.files)
			for name, text := range files {
				if err := os.MkdirAll(filepath.Dir(filepath.Join(dir, name)), 0o755); err != nil {
					t.Fatal(err)
				}
				if err := os.WriteFile(filepath.Join(dir, name), []byte(text), 0o644); err != nil {
					t.Fatal(err)
				}
			}
			_, ls := linkers(t, filepath.Join(dir, "m"), filepath.Join(dir, "cache"))
			var found []string
			for _, l := range ls {
				for _, f := range l.reach("example.com/m/hook") {
					if !slices.Contains(found, f) {
						found = append(found, f)
					}
				}
			}
			if !slices.Equal(found, c.want) {
				t.Errorf("found %q, want %q", found, c.want)
			}
		})
	}
}
