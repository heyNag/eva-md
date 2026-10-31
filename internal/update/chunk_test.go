package update

import (
	"bytes"
	"context"
	"encoding/json"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/heyNag/eva-md/internal/identity"
	"github.com/heyNag/eva-md/internal/inbox"
	"github.com/heyNag/eva-md/internal/okfadapter"
	"github.com/heyNag/eva-md/internal/threadpage"
)

// The dry run prints the exact command, by the CLI path install recorded
// when there is one, the environment's names, the rules, the input and the
// schema, the same bytes twice, with no value from the environment, and
// changes nothing.
func TestDryRunIsByteIdenticalAndShowsNoValues(t *testing.T) {
	e := newEnv(t)
	t.Setenv("SSL_CERT_FILE", "/secret/cert-value-7")
	t.Setenv("ANTHROPIC_API_KEY", "sk-ant-made-up-value")
	c := newChat("dryrun-a", start)
	talk(c, 2, "amber")
	file := e.write(c)
	var first, second bytes.Buffer
	for _, out := range []*bytes.Buffer{&first, &second} {
		if err := e.job().DryRun(context.Background(), c.host, c.session, out); err != nil {
			t.Fatal(err)
		}
	}
	got := first.String()
	if got != second.String() {
		t.Fatalf("two dry runs differ:\n%s\n---\n%s", got, second.String())
	}
	for _, want := range []string{
		"no call was made", "Thread page: projects/eva-md/threads/main (new)",
		"\n  claude -p --safe-mode --tools '' --strict-mcp-config --no-session-persistence --permission-prompts none --effort max --system-prompt '<the rules below>' --output-format json --json-schema '<the schema below>'",
		"SSL_CERT_FILE", "EVA_MD_INNER", "== Rules ==\n" + Rules, "== Input ==\nBranch: main\n", "[1] user 2026-01-14 10:01: Plan the amber step 0.",
		"== Schema ==\n{\"type\":\"object\"",
		// The rules of version 2: sharper Now items and standing rules.
		"== Rules ==\neva-md update rules, version 2.\n", "Make every Now item concrete and specific", "Standing rules on file, by id: none.\n", `"rules":{"type":"array","maxItems":10`,
	} {
		if !strings.Contains(got, want) {
			t.Fatalf("dry run lacks %q:\n%s", want, got)
		}
	}
	for _, value := range []string{"/secret/cert-value-7", "sk-ant-made-up-value", "ANTHROPIC_API_KEY", os.Getenv("HOME"), os.Getenv("PATH")} {
		if strings.Contains(got, value) {
			t.Fatalf("dry run shows %q", value)
		}
	}
	if calls(e.fake) != 0 || e.mark(file).Offset != 0 {
		t.Fatal("the dry run called the model or moved the watermark")
	}
	if _, err := os.Stat(filepath.Join(e.dirs.State, "updates")); err == nil {
		t.Fatal("the dry run wrote the run log")
	}
	codex := newChat("dryrun-b", start)
	codex.host = HostCodex
	talk(codex, 2, "amber")
	e.write(codex)
	recorded := filepath.Join(fakeBin, "codex")
	clis, _ := json.Marshal(map[string]string{"codex": recorded})
	if err := os.MkdirAll(e.dirs.State, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(e.dirs.State, "cli-paths.json"), clis, 0o600); err != nil {
		t.Fatal(err)
	}
	var out bytes.Buffer
	if err := e.job().DryRun(context.Background(), HostCodex, "dryrun-b", &out); err != nil {
		t.Fatal(err)
	}
	if want := "\n  " + shellQuote(recorded) + " --no-daemon exec --ephemeral --json -s read-only --skip-git-repo-check -C '<an empty run folder>'"; !strings.Contains(out.String(), want) ||
		!strings.Contains(out.String(), "--output-schema '<a file holding the schema below>' -o '<a file for the answer>' -") {
		t.Fatalf("codex command:\n%s", out.String())
	}
}

// The run log and the job's log hold counts, names and reasons, never the
// text of an entry or of the model's output, whatever the outcome.
func TestRunLogHoldsNoEntryText(t *testing.T) {
	e := newEnv(t)
	const marker = "ZEBRA-QUOKKA-77"
	c := newChat("textless", start)
	c.user("Remember " + marker + " for the release. " + filler)
	c.assistant("Noted " + marker + ". " + filler)
	e.write(c)
	e.fake.Respond = func(Request) (Result, error) {
		return Result{Output: []byte(`{"summary":"` + marker + `"}`), Attempts: 1, Degraded: "an optional MCP server failed"}, nil
	}
	e.run(c)
	e.fake.Respond = func(r Request) (Result, error) {
		o := keeps(r)
		o.Summary, o.LastChat = marker+" is planned.", "Planned "+marker+"."
		return Result{Output: encode(t, o), Attempts: 1}, nil
	}
	e.run(c)
	locked := newChat("textless-2", start)
	locked.branch = "locked-branch"
	locked.user("Also " + marker + ". " + filler)
	locked.assistant("Yes, " + marker + ". " + filler)
	e.write(locked)
	if err := e.wiki().Create("projects/eva-md/threads/locked-branch", lockedPage(), "human:alex"); err != nil {
		t.Fatal(err)
	}
	e.run(locked)
	logFile, err := OpenLog(e.dirs, e.clock)
	if err != nil {
		t.Fatal(err)
	}
	logFile.Write(e.log.Bytes())
	logFile.Close()
	var seen []string
	for _, dir := range []string{"updates", "logs"} {
		filepath.WalkDir(filepath.Join(e.dirs.State, dir), func(path string, d fs.DirEntry, err error) error {
			if err == nil && !d.IsDir() {
				data, _ := os.ReadFile(path)
				seen = append(seen, path)
				if bytes.Contains(data, []byte(marker)) {
					t.Errorf("%s holds entry text:\n%s", path, data)
				}
				if info, _ := d.Info(); info.Mode().Perm() != 0o600 {
					t.Errorf("%s has mode %v", path, info.Mode().Perm())
				}
			}
			return nil
		})
	}
	records, _ := readDay(e.dirs.State, "2026-01-14")
	if len(seen) != 2 || len(records) != 3 || records[0].Degraded == "" {
		t.Fatalf("files %v, records %+v", seen, records)
	}
}

func lockedPage() *okfadapter.Concept {
	return &okfadapter.Concept{Type: "thread", Title: "eva-md · locked-branch", Status: "stable",
		Body: "\n## Now\n<!-- eva:now:start -->\nKept by hand.\n<!-- eva:now:end -->\n\n## Timeline", Extra: map[string]any{"locked": true}}
}

// A chat over 60 KB is cut into chunks. When the pass after the first
// fails, the watermark keeps "more", so a later run continues the group:
// its tail, a run of long replies with no prompt, is built rather than
// left waiting for a prompt that will never come.
func TestChunkedChatContinuesAcrossRuns(t *testing.T) {
	e := newEnv(t)
	c := newChat("chunked", start)
	c.user("Write up the cobalt rollout, in detail. " + filler)
	long := strings.Repeat("Every cobalt node reports its state in turn, and the report goes on. ", 100) // about 7 KB
	for i := 0; i < 12; i++ {
		c.assistant(long)
	}
	file := e.write(c)
	n := 0
	e.fake.Respond = func(r Request) (Result, error) {
		if n++; n == 2 {
			return Result{Reason: ReasonRateLimit, Attempts: 1}, nil
		}
		return Result{Output: reply(t, r, "cobalt"), Attempts: 1}, nil
	}
	s := e.run(c)
	m := e.mark(file)
	if outcomes(s) != "ok, failed (rate_limit)" || !m.More || m.Offset == 0 || m.Offset >= size(t, file) {
		t.Fatalf("first run %q, watermark %+v of %d\n%s", outcomes(s), m, size(t, file), e.log)
	}
	e.clock = e.clock.Add(time.Hour)
	s = e.run(c)
	if outcomes(s) != "ok" || e.mark(file) != (inbox.Mark{Offset: size(t, file)}) {
		t.Fatalf("second run %q, watermark %+v\n%s", outcomes(s), e.mark(file), e.log)
	}
	if third := e.fake.Requests()[2]; strings.Contains(string(third.Input), "] user ") {
		t.Fatal("the tail held a prompt, so it proves nothing")
	}
}

// The job reads a bounded window past the watermark and reads further only
// while the group runs on past it with room left in its chunk, never the
// whole of a large backlog.
func TestWindowGrowsOnlyWhenTheChunkNeedsIt(t *testing.T) {
	isolate(t)
	store := inbox.Store{Root: filepath.Join(os.Getenv("HOME"), "inbox")}
	write := func(name string, entries int, text string) string {
		var b bytes.Buffer
		for i := 0; i < entries; i++ {
			role, kind := "assistant", "reply"
			if i%2 == 0 {
				role, kind = "user", "prompt"
			}
			data, _ := json.Marshal(inbox.Entry{V: 1, TS: start.Add(time.Duration(i) * time.Minute).Format(time.RFC3339), Host: HostClaude, Session: name,
				Role: role, Kind: kind, Text: text, Project: "github.com__you__eva-md", Branch: "main", Origin: "hook"})
			b.Write(append(data, '\n'))
		}
		file := store.Path("github.com__you__eva-md", HostClaude, name)
		if err := os.MkdirAll(filepath.Dir(file), 0o700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(file, b.Bytes(), 0o600); err != nil {
			t.Fatal(err)
		}
		return file
	}
	for _, tc := range []struct {
		name    string
		entries int
		text    string
		window  int64 // the most the job may read
		minimum int   // the entries the chunk must cover
	}{
		{"small", 6, filler, windowBytes, 6},                         // a small backlog is read once, whole
		{"many", 9000, filler, windowBytes, 10},                      // many entries fill the chunk inside the first window
		{"huge", 30, strings.Repeat("x", 1<<20), 16 << 20, 8},        // 1 MiB entries: the window grows until the chunk is full
		{"to-the-end", 4, strings.Repeat("y", 1900<<10), 8 << 20, 4}, // it grows until the file ends
	} {
		t.Run(tc.name, func(t *testing.T) {
			file := write(tc.name, tc.entries, tc.text)
			b, err := openBacklog(store, file)
			if err != nil {
				t.Fatal(err)
			}
			defer b.close()
			g, err := b.next(Options{})
			if err != nil || g.err != nil {
				t.Fatalf("next: %v %v", err, g.err)
			}
			batch, err := Build(g.entries, threadpage.Page{}, Options{Branch: "main"})
			if err != nil {
				t.Fatal(err)
			}
			if b.window > tc.window || len(batch.Entries) < tc.minimum || b.window == size(t, file) && tc.name == "huge" {
				t.Fatalf("window %d of %d bytes, %d entries in the chunk", b.window, size(t, file), len(batch.Entries))
			}
		})
	}
}

// A session that switches branch is processed one branch at a time, each
// group on its own thread page; a group a switch closed under one exchange
// or 600 characters is skipped, since nothing can join it.
func TestBranchSwitchesSplitTheGroups(t *testing.T) {
	e := newEnv(t)
	c := newChat("switch", start)
	c.user("Quick check.")
	c.branch = "feature/x"
	talk(c, 1, "amber")
	c.branch = "main"
	talk(c, 1, "cobalt")
	file := e.write(c)
	s := e.run(c)
	if outcomes(s) != "skipped (small), ok, ok" || s.Records[1].Thread != "feature/x" || e.mark(file).Offset != size(t, file) {
		t.Fatalf("outcomes %q\n%s", outcomes(s), e.log)
	}
	// Each page cites its own group's prompt: line 2 on feature/x, line 4
	// on main.
	for id, ref := range map[string]string{"projects/eva-md/threads/" + threadpage.ThreadName("feature/x"): "s:switch#2", testPage: "s:switch#4"} {
		p, _ := e.page(id)
		if it, _, ok := item(p, "Finish the amber step"); !ok || len(it.Refs) != 1 || it.Refs[0] != ref {
			t.Fatalf("%s: %+v", id, p.Now)
		}
	}
	now, _ := os.ReadFile(filepath.Join(e.cfg.WikiPath, "projects/eva-md/now.md"))
	if !strings.Contains(string(now), "[feature/x](threads/"+threadpage.ThreadName("feature/x")+".md)") || !strings.Contains(string(now), "[main](threads/main.md)") {
		t.Fatalf("overview:\n%s", now)
	}
}

// The hooks record each entry's branch as HEAD was then, so a turn in which
// the assistant checks out a branch has its prompt on the old branch and
// its tool line and reply on the new one. Only a prompt of another branch
// ends a group, so the turn stays whole on its prompt's branch and the
// user's words reach the page with the reply.
func TestBranchSwitchMidTurnKeepsTheTurnWhole(t *testing.T) {
	e := newEnv(t)
	c := newChat("midturn", start)
	c.user("Start the cobalt work on a branch of its own. " + filler)
	c.branch = "feature/cobalt"
	c.tool("Bash git checkout -b feature/cobalt (exit 0)")
	c.assistant("Made feature/cobalt and switched to it. " + filler)
	talk(c, 1, "amber")
	file := e.write(c)
	s := e.run(c)
	if outcomes(s) != "ok, ok" || s.Records[0].Thread != "main" || s.Records[1].Thread != "feature/cobalt" || e.mark(file).Offset != size(t, file) {
		t.Fatalf("outcomes %q, records %+v\n%s", outcomes(s), s.Records, e.log)
	}
	first, second := string(e.fake.Requests()[0].Input), string(e.fake.Requests()[1].Input)
	if !strings.Contains(first, "Start the cobalt work") || !strings.Contains(first, "Made feature/cobalt") || strings.Contains(second, "Start the cobalt work") {
		t.Fatalf("first input:\n%s\nsecond input:\n%s", first, second)
	}
}

// When the pass that set the watermark stopped inside a group and its rest
// holds only events, such as the closed chat's session_end, the rest is
// skipped as small rather than left waiting for ever: events add nothing
// to a page.
func TestEventOnlyRestOfAGroupIsSkipped(t *testing.T) {
	e := newEnv(t)
	c := newChat("evtail", start)
	talk(c, 2, "amber")
	c.event("session_end")
	file := e.write(c)
	entries := c.entries(t)
	if err := (inbox.Store{Root: e.dirs.Inbox()}).SetMark(file, inbox.Mark{Offset: entries[len(entries)-2].End, More: true}); err != nil {
		t.Fatal(err)
	}
	if s := e.run(c); outcomes(s) != "skipped (small)" || calls(e.fake) != 0 || e.mark(file) != (inbox.Mark{Offset: size(t, file)}) {
		t.Fatalf("outcomes %q, watermark %+v of %d\n%s", outcomes(s), e.mark(file), size(t, file), e.log)
	}
	if s := e.run(c); s.Line() != "nothing pending" {
		t.Fatalf("again: %q", s.Line())
	}
}

// The chat's off-the-record state carries from one pass to the next: a
// group that ends off the record leaves the next group's entries out until
// the chat is back on the record, within one run and, through Prefix, in a
// later one.
func TestOffRecordCarriesAcrossPasses(t *testing.T) {
	for _, name := range []string{"one run", "two runs"} {
		t.Run(name, func(t *testing.T) {
			e := newEnv(t)
			c := newChat("offrec", start)
			talk(c, 1, "amber")
			c.event("off_record")
			var all []string
			if name == "two runs" {
				e.write(c)
				all = append(all, outcomes(e.run(c)))
			}
			c.branch = "feature/plum"
			c.user("SECRET-PLUM-11 is the private part. " + filler)
			c.assistant("Kept SECRET-PLUM-11 to this chat. " + filler)
			c.event("on_record")
			talk(c, 1, "cobalt")
			file := e.write(c)
			all = append(all, outcomes(e.run(c)))
			if strings.Join(all, ", ") != "ok, ok" || e.mark(file).Offset != size(t, file) {
				t.Fatalf("outcomes %q, watermark %+v\n%s", all, e.mark(file), e.log)
			}
			for i, r := range e.fake.Requests() {
				if strings.Contains(string(r.Input), "SECRET-PLUM-11") {
					t.Fatalf("request %d holds off-the-record text:\n%s", i+1, r.Input)
				}
			}
			if p, _ := e.page("projects/eva-md/threads/" + threadpage.ThreadName("feature/plum")); p.Now.Summary != "The amber work goes on." {
				t.Fatalf("feature/plum: %+v", p.Now)
			}
		})
	}
}

// The effort a run starts at is the one today's runs showed the model
// accepts, so a model below the table's top pays the refused steps once a
// day; a lower configured effort is asked as it is.
func TestRunsStartAtTheAcceptedEffort(t *testing.T) {
	e := newEnv(t)
	// The model's top level is xhigh: a run asked for max is accepted there.
	e.fake.Respond = func(r Request) (Result, error) {
		accepted := r.Effort
		if r.Effort == "max" {
			accepted = "xhigh"
		}
		return Result{Output: reply(t, r, "amber"), Attempts: 1, Model: r.Model, Effort: accepted}, nil
	}
	for i, want := range []string{"max", "xhigh", "xhigh"} {
		c := newChat("effort-"+string(rune('a'+i)), start.Add(time.Duration(i)*time.Hour))
		talk(c, 1, "amber")
		for j := range c.rows {
			c.rows[j].Model = "claude-test-model"
		}
		e.write(c)
		e.run(c)
		if got := e.fake.Requests()[i].Effort; got != want {
			t.Fatalf("run %d asked %q, want %q", i+1, got, want)
		}
	}
	e.cfg.Effort = map[string]string{"claude-code": "low"}
	c := newChat("effort-low", start.Add(5*time.Hour))
	talk(c, 1, "amber")
	e.write(c)
	e.run(c)
	if got := e.fake.Requests()[3]; got.Effort != "low" || got.Model != "" {
		t.Fatalf("configured low: %+v", got)
	}
	e.cfg.Effort = nil
	e.clock = e.clock.Add(24 * time.Hour)
	c = newChat("effort-next-day", start.Add(30*time.Hour))
	talk(c, 1, "amber")
	for j := range c.rows {
		c.rows[j].Model = "claude-test-model"
	}
	e.write(c)
	e.run(c)
	if got := e.fake.Requests()[4].Effort; got != "max" {
		t.Fatalf("next day asked %q", got)
	}
}

// The input names the branch's last commits, read from git in the chat's
// worktree, and the page records the branch's head; a new thread shows its
// parent's Now, picked by the brief's lineage rule, and names its parent.
func TestCommitsAndParentComeFromTheWorktree(t *testing.T) {
	e := newEnv(t)
	repo := filepath.Join(os.Getenv("HOME"), "projects", "demo")
	git := func(args ...string) string {
		out, err := exec.Command("git", append([]string{"-C", repo}, args...)...).CombinedOutput()
		if err != nil {
			t.Fatalf("git %v: %v %s", args, err, out)
		}
		return strings.TrimSpace(string(out))
	}
	if err := os.MkdirAll(repo, 0o700); err != nil {
		t.Fatal(err)
	}
	git("init", "-q", "-b", "main")
	git("config", "user.name", "Fixture")
	git("config", "user.email", "fixture@example.invalid")
	git("config", "commit.gpgsign", "false")
	git("remote", "add", "origin", "git@github.com:you/demo.git")
	git("commit", "-q", "--allow-empty", "-m", "first subject")
	git("commit", "-q", "--allow-empty", "-m", "second subject")
	git("branch", "feature")
	head := git("rev-parse", "HEAD")
	p, err := identity.Resolve(context.Background(), repo)
	if err != nil {
		t.Fatal(err)
	}
	main := newChat("worktree-a", start)
	talk(main, 3, "amber")
	for i := range main.rows {
		main.rows[i].Project, main.rows[i].Cwd = p.Key, "projects/demo"
	}
	e.write(main)
	if s := e.run(main); outcomes(s) != "ok" {
		t.Fatalf("main: %q\n%s", outcomes(s), e.log)
	}
	input := string(e.fake.Requests()[0].Input)
	if !strings.Contains(input, "Last commits on this branch, newest first:\n- second subject\n- first subject\n") {
		t.Fatalf("input:\n%s", input)
	}
	_, pg := e.page("projects/demo/threads/main")
	if pg.Concept.Extra["head"] != head {
		t.Fatalf("head %v, want %s", pg.Concept.Extra["head"], head)
	}
	feature := newChat("worktree-b", start.Add(time.Hour))
	feature.branch = "feature"
	talk(feature, 1, "cobalt")
	for i := range feature.rows {
		feature.rows[i].Project, feature.rows[i].Cwd = p.Key, "projects/demo"
	}
	e.write(feature)
	if s := e.run(feature); outcomes(s) != "ok" {
		t.Fatalf("feature: %q\n%s", outcomes(s), e.log)
	}
	input = string(e.fake.Requests()[1].Input)
	if !strings.Contains(input, "Parent thread main, its Now items, for context only") || !strings.Contains(input, "Finish the amber step") {
		t.Fatalf("no parent Now:\n%s", input)
	}
	if _, pg := e.page("projects/demo/threads/feature"); pg.Concept.Extra["parent"] != "main" {
		t.Fatalf("parent %v", pg.Concept.Extra["parent"])
	}
	// With the worktree gone, the project still comes from its key and the
	// commits are unknown.
	if err := os.RemoveAll(repo); err != nil {
		t.Fatal(err)
	}
	talk(main, 1, "cobalt")
	e.write(main)
	if s := e.run(main); outcomes(s) != "ok" || !strings.Contains(string(e.fake.Requests()[2].Input), "newest first:\n- unknown\n") {
		t.Fatalf("without the worktree: %q", outcomes(s))
	}
}
