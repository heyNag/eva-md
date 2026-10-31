package update

import (
	"bytes"
	"context"
	"errors"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/heyNag/eva-md/internal/config"
	"github.com/heyNag/eva-md/internal/identity"
	"github.com/heyNag/eva-md/internal/inbox"
	"github.com/heyNag/eva-md/internal/okfadapter"
	"github.com/heyNag/eva-md/internal/paths"
	"github.com/heyNag/eva-md/internal/resume"
	"github.com/heyNag/eva-md/internal/threadpage"
)

// The job's tests run in a temporary home holding a wiki, a config and an
// inbox, with a fake runner and a clock they set. isolate leaves PATH with
// only the fake CLIs and the system folders, so no real claude or codex can
// run even if a runner were reached.

const (
	testInstance = "test-0a1b2c3d"
	testPage     = "projects/eva-md/threads/main"
	// The wiki's .gitignore, a copy of the one setup writes, which wikiinit
	// doesn't export.
	testGitignore = "*.jsonl\n/inbox/\n.eva-md/tmp/\n.tmp-okf-*\n.tmp-eva-*\n.DS_Store\n"
)

type env struct {
	t     *testing.T
	dirs  paths.Dirs
	cfg   config.Config
	fake  *Fake
	clock time.Time
	log   *bytes.Buffer
}

func newEnv(t *testing.T) *env {
	t.Helper()
	isolate(t)
	dirs, err := paths.Default()
	if err != nil {
		t.Fatal(err)
	}
	wiki := filepath.Join(os.Getenv("HOME"), "eva-wiki")
	if err := okfadapter.Init(wiki); err != nil {
		t.Fatal(err)
	}
	if err := config.SaveSchema(filepath.Join(wiki, ".eva-md", "schema.toml"), config.WikiSchema{SchemaVersion: 1, OKFSpec: "0.2", Instance: testInstance}); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(wiki, ".gitignore"), []byte(testGitignore), 0o600); err != nil {
		t.Fatal(err)
	}
	cfg := config.Default()
	cfg.WikiPath, cfg.Instance = wiki, testInstance
	if err := config.Save(dirs.ConfigFile(), cfg); err != nil {
		t.Fatal(err)
	}
	e := &env{t: t, dirs: dirs, cfg: cfg, fake: &Fake{}, clock: time.Date(2026, 1, 14, 12, 0, 0, 0, paris), log: &bytes.Buffer{}}
	e.fake.Respond = func(r Request) (Result, error) { return Result{Output: reply(t, r, "amber"), Attempts: 1}, nil }
	// As install does, before any chat is captured.
	if err := Baseline(dirs, e.clock); err != nil {
		t.Fatal(err)
	}
	return e
}

func (e *env) job() Job {
	return Job{Dirs: e.dirs, Config: e.cfg, Version: "0.1.0-test", Runner: func(string) Runner { return e.fake },
		Now: func() time.Time { return e.clock }, Log: e.log}
}

// write saves a chat's entries as its session file, as the hooks appended
// them, and returns the file's path.
func (e *env) write(c *chat) string {
	e.t.Helper()
	file := inbox.Store{Root: e.dirs.Inbox()}.Path(c.rows[0].Project, c.host, c.session)
	if err := os.MkdirAll(filepath.Dir(file), 0o700); err != nil {
		e.t.Fatal(err)
	}
	if err := os.WriteFile(file, c.jsonl(e.t), 0o600); err != nil {
		e.t.Fatal(err)
	}
	return file
}

// run runs the job for a chat's session.
func (e *env) run(c *chat) Summary {
	e.t.Helper()
	s, err := e.job().Session(context.Background(), c.host, c.session)
	if err != nil {
		e.t.Fatalf("Session: %v\n%s", err, e.log)
	}
	return s
}

func (e *env) mark(file string) inbox.Mark {
	e.t.Helper()
	m, err := inbox.Store{Root: e.dirs.Inbox()}.Mark(file)
	if err != nil {
		e.t.Fatal(err)
	}
	return m
}

func (e *env) wiki() *okfadapter.Wiki {
	e.t.Helper()
	w, err := okfadapter.Open(e.cfg.WikiPath, e.dirs.Locks())
	if err != nil {
		e.t.Fatal(err)
	}
	return w
}

// page reads a thread page's Now and frontmatter, failing the test when it
// is missing or doesn't parse.
func (e *env) page(id string) (threadpage.Page, *okfadapter.Page) {
	e.t.Helper()
	pg, err := e.wiki().Read(id)
	if err != nil {
		e.t.Fatalf("read %s: %v", id, err)
	}
	p, err := threadpage.Parse(pg.Concept.Body)
	if err != nil {
		e.t.Fatalf("parse %s: %v", id, err)
	}
	return p, pg
}

func size(t *testing.T, file string) int64 {
	t.Helper()
	info, err := os.Stat(file)
	if err != nil {
		t.Fatal(err)
	}
	return info.Size()
}

// talk adds exchanges of a prompt and a reply, each long enough with the
// filler to update a page.
func talk(c *chat, n int, word string) {
	for i := 0; i < n; i++ {
		c.user("Plan the " + word + " step " + strconv.Itoa(i) + ". " + filler)
		c.assistant("Noted the " + word + " step " + strconv.Itoa(i) + ". " + filler)
	}
}

// idLine is a Now item of the page context, as Build writes it.
var idLine = regexp.MustCompile(`(?m)^- (n[0-9]+) (next|question|decision) `)

// reply is a valid answer to any batch, as a model would give it: a
// summary, a last chat, a keep for every id on the page and a next step its
// first entry states.
func reply(t testing.TB, r Request, word string) []byte {
	o := keeps(r)
	o.Summary, o.LastChat = "The "+word+" work goes on.", "Planned the "+word+" work."
	o.Items = append(o.Items, opAdd("next", "Finish the "+word+" step", "stated", 1))
	o.Timeline = []eventLine{tl("decision", "Use the "+word+" switch", "stated", 1)}
	return encode(t, o)
}

// keeps is an answer that keeps every id on the page and adds nothing: a
// pass that changes nothing.
func keeps(r Request) output {
	var o output
	for _, m := range idLine.FindAllStringSubmatch(string(r.Input), -1) {
		o.Items = append(o.Items, opKeep(m[1], m[2], "stated"))
	}
	return o
}

func calls(f *Fake) int { return len(f.Requests()) }

func outcomes(s Summary) string {
	var out []string
	for _, r := range s.Records {
		o := r.Outcome
		if r.Reason != "" {
			o += " (" + r.Reason + ")"
		}
		out = append(out, o)
	}
	return strings.Join(out, ", ")
}

// A pass over hook-shaped entries writes the thread page, the project's
// overview and the index files, okf validates the wiki, the watermark moves
// to the end of the file and the run log records the pass.
func TestPassWritesThePageOverviewAndIndexes(t *testing.T) {
	e := newEnv(t)
	c := newChat("3f2a91c0-aaaa", start)
	talk(c, 2, "amber")
	file := e.write(c)
	s := e.run(c)
	if outcomes(s) != "ok" || calls(e.fake) != 1 {
		t.Fatalf("outcomes %q, %d calls\n%s", outcomes(s), calls(e.fake), e.log)
	}
	if m := e.mark(file); m.Offset != size(t, file) || m.More {
		t.Fatalf("watermark %+v, file %d bytes", m, size(t, file))
	}
	p, pg := e.page(testPage)
	if _, _, ok := item(p, "Finish the amber step"); !ok || p.Now.Summary != "The amber work goes on." {
		t.Fatalf("page Now: %+v", p.Now)
	}
	if !strings.HasPrefix(p.Now.LastChat, "2026-01-14 · claude-code/unknown · Planned the amber work.") {
		t.Fatalf("last chat %q", p.Now.LastChat)
	}
	x := pg.Concept.Extra
	if x["branch"] != "main" || x["as_of"] != c.rows[len(c.rows)-1].TS || x["writer"] != "eva-md/0.1.0-test" || x["locked"] != false || x["state"] != "active" {
		t.Fatalf("frontmatter %v", x)
	}
	if len(pg.Concept.Sources) != 1 || pg.Concept.Sources[0].Resource != "s:3f2a91c0" || pg.Concept.Title != "eva-md · main" {
		t.Fatalf("sources %+v title %q", pg.Concept.Sources, pg.Concept.Title)
	}
	now, _ := e.wiki().Read("projects/eva-md/now")
	if now == nil || now.Concept.Extra["remote"] != "github.com/you/eva-md" || !strings.Contains(now.Concept.Body, "](threads/main.md) — The amber work goes on.") {
		t.Fatalf("overview: %+v", now)
	}
	for rel, want := range map[string]string{
		"projects/eva-md/threads/index.md": "* [eva-md · main](main.md) - Branch main of github.com/you/eva-md",
		"projects/eva-md/index.md":         "* [eva-md](now.md) - Overview of the threads of github.com/you/eva-md",
		"projects/index.md":                "* [eva-md](eva-md/index.md) - Project memory",
		"index.md":                         "* [Projects](projects/index.md) - Project conversations",
	} {
		data, err := os.ReadFile(filepath.Join(e.cfg.WikiPath, rel))
		if err != nil || !strings.Contains(string(data), want) {
			t.Errorf("%s lacks %q:\n%s", rel, want, data)
		}
	}
	v, err := e.wiki().Validate()
	if err != nil || len(v.Errors)+len(v.Orphans)+len(v.BrokenLinks)+len(v.Warnings) != 0 {
		t.Fatalf("okf validate: %v %+v", err, v)
	}
	r := s.Records[0]
	if r.Project != "github.com__you__eva-md" || r.Thread != "main" || r.Pages != 1 || r.Calls != 1 || r.ChunkBytes == 0 || r.RulesVersion != RulesVersion || r.Effort != "max" || r.Validation == nil || r.BoundaryToSavedMS <= 0 {
		t.Fatalf("record %+v", r)
	}
	logged, err := readDay(e.dirs.State, "2026-01-14")
	if err != nil || len(logged) != 1 || logged[0].Outcome != OutcomeOK {
		t.Fatalf("run log %+v %v", logged, err)
	}
	// Nothing is pending once the watermark is at the end.
	if s := e.run(c); len(s.Records) != 0 || calls(e.fake) != 1 || s.Line() != "nothing pending" {
		t.Fatalf("second run %q, %d calls", s.Line(), calls(e.fake))
	}
}

// A pass whose output changes nothing advances the watermark once and is
// not run again: five runs, one model call.
func TestNoChangePassRunsOnce(t *testing.T) {
	e := newEnv(t)
	c := newChat("3f2a91c0-bbbb", start)
	talk(c, 2, "amber")
	e.write(c)
	e.run(c)
	before, _ := e.page(testPage)
	talk(c, 2, "cobalt")
	file := e.write(c)
	e.fake.Respond = func(r Request) (Result, error) { return Result{Output: encode(t, keeps(r)), Attempts: 1}, nil }
	var all []string
	for i := 0; i < 5; i++ {
		s := e.run(c)
		all = append(all, outcomes(s))
	}
	if strings.Join(all, "|") != "no_change||||" || calls(e.fake) != 2 {
		t.Fatalf("runs %q, %d calls", all, calls(e.fake))
	}
	if m := e.mark(file); m.Offset != size(t, file) {
		t.Fatalf("watermark %+v", m)
	}
	after, _ := e.page(testPage)
	if threadpage.Render(after) != threadpage.Render(before) {
		t.Fatal("a pass that changed nothing rewrote the page's body")
	}
}

// A pass that changes nothing still stamps the page's as_of and sources, so
// the brief shows the Now it confirmed as fresh, not stale against the
// exchanges it covered. A pass that moves neither writes nothing.
func TestNoChangePassStampsAsOf(t *testing.T) {
	e := newEnv(t)
	c := newChat("3f2a91c0-asof", start)
	talk(c, 2, "amber")
	e.write(c)
	e.run(c)
	before, _ := e.page(testPage)
	talk(c, 2, "cobalt")
	file := e.write(c)
	e.fake.Respond = func(r Request) (Result, error) { return Result{Output: encode(t, keeps(r)), Attempts: 1}, nil }
	e.clock = e.clock.Add(time.Hour)
	if s := e.run(c); outcomes(s) != "no_change" || s.Records[0].Pages != 1 || e.mark(file).Offset != size(t, file) {
		t.Fatalf("outcomes %q %+v\n%s", outcomes(s), s.Records, e.log)
	}
	after, pg := e.page(testPage)
	newest := c.rows[len(c.rows)-1].TS
	if threadpage.Render(after) != threadpage.Render(before) || pg.Concept.Extra["as_of"] != newest {
		t.Fatalf("as_of %v, want %s; body kept: %v", pg.Concept.Extra["as_of"], newest, threadpage.Render(after) == threadpage.Render(before))
	}
	p := identity.Project{Kind: identity.KindRepo, Key: "github.com__you__eva-md", Slug: "eva-md", Remote: "github.com/you/eva-md", Branch: "main"}
	r, err := resume.Build(context.Background(), resume.Options{Dirs: e.dirs, Config: e.cfg, Project: p, Host: HostClaude, Session: "another-chat", Now: e.clock})
	if err != nil || !strings.Contains(r.Text, "Now (as of ") || strings.Contains(r.Text, "stale") {
		t.Fatalf("brief after a pass that changed nothing: %v\n%s", err, r.Text)
	}
	// An older chat's pass that changes nothing adds its session to the
	// sources and leaves the as_of where it is.
	older := newChat("older000-asof", start.Add(-time.Hour))
	talk(older, 1, "amber")
	e.write(older)
	if s := e.run(older); outcomes(s) != "no_change" {
		t.Fatalf("older chat: %q\n%s", outcomes(s), e.log)
	}
	_, pg = e.page(testPage)
	if src := pg.Concept.Sources; pg.Concept.Extra["as_of"] != newest || len(src) != 2 || src[1].Resource != "s:older000" {
		t.Fatalf("as_of %v, sources %+v", pg.Concept.Extra["as_of"], src)
	}
	// The same chunk again, by another model, moves neither, and nothing
	// is written.
	raw := pg.Raw
	if err := (inbox.Store{Root: e.dirs.Inbox()}).SetMark(file, inbox.Mark{}); err != nil {
		t.Fatal(err)
	}
	e.fake.Respond = func(r Request) (Result, error) {
		return Result{Output: encode(t, keeps(r)), Attempts: 1, Model: "claude-other-model"}, nil
	}
	if s := e.run(c); outcomes(s) != "no_change" || s.Records[0].Pages != 0 {
		t.Fatalf("again: %q %+v", outcomes(s), s.Records)
	}
	if _, pg := e.page(testPage); !bytes.Equal(pg.Raw, raw) {
		t.Fatal("a pass that moved nothing wrote the page")
	}
}

// The job writes only a wiki whose schema version it writes and whose
// instance the config names: otherwise it stops with an error before any
// call, and the watermark stays.
func TestWikiSchemaAndInstanceAreChecked(t *testing.T) {
	e := newEnv(t)
	c := newChat("3f2a91c0-guard", start)
	talk(c, 2, "amber")
	file := e.write(c)
	schema := filepath.Join(e.cfg.WikiPath, ".eva-md", "schema.toml")
	for _, tc := range []struct {
		schema config.WikiSchema
		want   string
	}{
		{config.WikiSchema{SchemaVersion: 1, OKFSpec: "0.2", Instance: "other-0a1b2c3d"}, "the wiki's instance is not the one the config names"},
		{config.WikiSchema{SchemaVersion: 2, OKFSpec: "0.2", Instance: testInstance}, "the wiki's schema version is 2"},
	} {
		if err := config.SaveSchema(schema, tc.schema); err != nil {
			t.Fatal(err)
		}
		if _, err := e.job().Session(context.Background(), c.host, c.session); err == nil || !strings.Contains(err.Error(), tc.want) {
			t.Fatalf("schema %+v: %v", tc.schema, err)
		}
	}
	if calls(e.fake) != 0 || e.mark(file).Offset != 0 {
		t.Fatalf("%d calls, watermark %+v", calls(e.fake), e.mark(file))
	}
}

// A page edited by hand between the pass's read and its save is read again
// and the update applied to it again: the edit stays and the pass is ok.
func TestConflictIsRetriedOnTheEditedPage(t *testing.T) {
	e := newEnv(t)
	c := newChat("3f2a91c0-cas1", start)
	talk(c, 2, "amber")
	e.write(c)
	e.run(c)
	talk(c, 2, "cobalt")
	file := e.write(c)
	e.fake.Respond = func(r Request) (Result, error) {
		pg, err := e.wiki().Read(testPage)
		if err == nil {
			_, err = e.wiki().Update(testPage, pg.Hash, func(c *okfadapter.Concept) error {
				c.Body += "\n## Notes\n\nKept by hand.\n"
				return nil
			}, okfadapter.Actor("human", "alex"))
		}
		if err != nil {
			t.Error(err)
		}
		return Result{Output: reply(t, r, "cobalt"), Attempts: 1}, nil
	}
	if s := e.run(c); outcomes(s) != "ok" || e.mark(file).Offset != size(t, file) {
		t.Fatalf("outcomes %q\n%s", outcomes(s), e.log)
	}
	p, pg := e.page(testPage)
	if _, _, ok := item(p, "Finish the cobalt step"); !ok || !strings.Contains(pg.Concept.Body, "## Notes\n\nKept by hand.") {
		t.Fatalf("page after the retry:\n%s", pg.Concept.Body)
	}
}

// A save the wiki can't take, here into a folder it can't write, fails with
// wiki_error: the watermark stays where it was and the host backs off, 5
// minutes, then 10 and 20, but the failures count toward no session, so
// three in a row don't quarantine it. Once the folder can be written, the
// next run succeeds.
func TestSaveFailureKeepsTheWatermark(t *testing.T) {
	e := newEnv(t)
	c := newChat("3f2a91c0-cccc", start)
	talk(c, 2, "amber")
	file := e.write(c)
	threads := filepath.Join(e.cfg.WikiPath, "projects", "eva-md", "threads")
	if err := os.MkdirAll(threads, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(threads, 0o500); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chmod(threads, 0o700) })
	for i, wait := range []time.Duration{0, 5 * time.Minute, 10 * time.Minute} {
		e.clock = e.clock.Add(wait)
		if s := e.run(c); outcomes(s) != "failed (wiki_error)" || e.mark(file).Offset != 0 {
			t.Fatalf("run %d: outcomes %q, watermark %+v\n%s", i+1, outcomes(s), e.mark(file), e.log)
		}
	}
	if s := e.run(c); !s.Stopped(StopBackoff) || calls(e.fake) != 3 {
		t.Fatalf("no backoff after a wiki error: %+v, %d calls", s, calls(e.fake))
	}
	if _, err := e.wiki().Read(testPage); !errors.Is(err, fs.ErrNotExist) {
		t.Fatalf("a page after a failed save: %v", err)
	}
	if err := os.Chmod(threads, 0o700); err != nil {
		t.Fatal(err)
	}
	e.clock = e.clock.Add(20 * time.Minute)
	if s := e.run(c); outcomes(s) != "ok" || e.mark(file).Offset != size(t, file) {
		t.Fatalf("after three wiki errors %q %v, watermark %+v\n%s", outcomes(s), s.Stops, e.mark(file), e.log)
	}
}

// A watermark that can't be saved costs no model call: the job saves it as
// it stands before each call. When it can't be saved after a call, the
// pass is recorded as failed (watermark), so the call still counts toward
// the daily cap, and later runs spend no more calls on the same chunk.
func TestWatermarkFailureStaysWithinTheBudget(t *testing.T) {
	e := newEnv(t)
	e.cfg.DailyCap = 1
	c := newChat("3f2a91c0-wmwm", start)
	talk(c, 2, "amber")
	file := e.write(c)
	state := filepath.Join(filepath.Dir(file), ".state")
	if err := os.MkdirAll(state, 0o700); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chmod(state, 0o700) })
	if err := os.Chmod(state, 0o500); err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 3; i++ {
		if s := e.run(c); len(s.Records) != 0 || s.Errors != 1 || calls(e.fake) != 0 {
			t.Fatalf("run %d with no watermark to save: %+v, %d calls\n%s", i+1, s, calls(e.fake), e.log)
		}
	}
	if err := os.Chmod(state, 0o700); err != nil {
		t.Fatal(err)
	}
	// The folder turns read-only while the model runs, after the job saved
	// the watermark as it stood.
	e.fake.Respond = func(r Request) (Result, error) {
		if err := os.Chmod(state, 0o500); err != nil {
			t.Error(err)
		}
		return Result{Output: reply(t, r, "amber"), Attempts: 1}, nil
	}
	s := e.run(c)
	if outcomes(s) != "failed (watermark)" || s.Records[0].Calls != 1 || s.Errors != 1 || e.mark(file).Offset != 0 {
		t.Fatalf("a call whose watermark can't be saved: %q %+v, watermark %+v\n%s", outcomes(s), s, e.mark(file), e.log)
	}
	for i := 0; i < 2; i++ {
		if s := e.run(c); !s.Stopped(StopBudget) || calls(e.fake) != 1 {
			t.Fatalf("run %d after the unsaved watermark: %+v, %d calls", i+1, s, calls(e.fake))
		}
	}
	if records, _ := readDay(e.dirs.State, "2026-01-14"); len(records) != 1 {
		t.Fatalf("run log %+v", records)
	}
}

// TestHelperUpdateJob is the job a test kills; it runs only in the child
// process TestKillMidSave starts.
func TestHelperUpdateJob(t *testing.T) {
	if os.Getenv("EVA_MD_UPDATE_HELPER") != "1" {
		t.Skip("runs only as the child of TestKillMidSave")
	}
	dirs, _ := paths.Default()
	cfg, err := config.Load(dirs.ConfigFile())
	if err != nil {
		t.Fatal(err)
	}
	fake := &Fake{Respond: func(r Request) (Result, error) {
		_ = os.WriteFile(filepath.Join(os.Getenv("HOME"), "called"), nil, 0o600)
		return Result{Output: reply(t, r, "amber"), Attempts: 1}, nil
	}}
	j := Job{Dirs: dirs, Config: cfg, Version: "0.1.0-test", Runner: func(string) Runner { return fake }}
	_, _ = j.Session(context.Background(), HostClaude, os.Getenv("EVA_MD_UPDATE_SESSION"))
}

// A job killed in the middle of its save, while it waits for the wiki
// lock, leaves no page, the watermark where it was, no record and the
// update lock free. A save killed between okf's temporary file and its
// rename leaves only an orphaned temporary file, which the next run's page,
// overview and index leave out and the wiki's .gitignore keeps from ever
// being staged.
func TestKillMidSave(t *testing.T) {
	e := newEnv(t)
	c := newChat("3f2a91c0-dddd", start)
	talk(c, 2, "amber")
	file := e.write(c)
	lock, err := os.OpenFile(filepath.Join(e.dirs.Locks(), "wiki.lock"), os.O_CREATE|os.O_RDWR, 0o600)
	if err != nil {
		if err = os.MkdirAll(e.dirs.Locks(), 0o700); err == nil {
			lock, err = os.OpenFile(filepath.Join(e.dirs.Locks(), "wiki.lock"), os.O_CREATE|os.O_RDWR, 0o600)
		}
	}
	if err != nil {
		t.Fatal(err)
	}
	if err := syscall.Flock(int(lock.Fd()), syscall.LOCK_EX); err != nil {
		t.Fatal(err)
	}
	cmd := exec.Command(os.Args[0], "-test.run=^TestHelperUpdateJob$")
	cmd.Env = append(os.Environ(), "EVA_MD_UPDATE_HELPER=1", "EVA_MD_UPDATE_SESSION="+c.session)
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	if !waitFile(os.Getenv("HOME"), "called") {
		_ = cmd.Process.Kill()
		t.Fatal("the child never called the model")
	}
	time.Sleep(200 * time.Millisecond) // into the save, waiting for the wiki lock
	_ = cmd.Process.Signal(syscall.SIGKILL)
	// The child was still running when the kill came: it neither failed at
	// startup nor finished, which would have recorded the pass.
	var exit *exec.ExitError
	if err := cmd.Wait(); !errors.As(err, &exit) || !exit.Sys().(syscall.WaitStatus).Signaled() || exit.Sys().(syscall.WaitStatus).Signal() != syscall.SIGKILL {
		t.Fatalf("the child was not killed mid-save: %v", err)
	}
	if _, err := e.wiki().Read(testPage); !errors.Is(err, fs.ErrNotExist) {
		t.Fatalf("a killed save left a page: %v", err)
	}
	if m := e.mark(file); m.Offset != 0 {
		t.Fatalf("a killed save moved the watermark: %+v", m)
	}
	if _, err := os.Stat(filepath.Join(e.dirs.State, "updates")); !errors.Is(err, fs.ErrNotExist) {
		t.Fatalf("a killed save recorded its pass: %v", err)
	}
	unlock, busy, err := lockUpdates(e.dirs.Locks())
	if err != nil || busy {
		t.Fatalf("the update lock outlived the killed job: busy %v, %v", busy, err)
	}
	unlock()
	_ = syscall.Flock(int(lock.Fd()), syscall.LOCK_UN)
	lock.Close()

	threads := filepath.Join(e.cfg.WikiPath, "projects", "eva-md", "threads")
	if err := os.MkdirAll(threads, 0o700); err != nil {
		t.Fatal(err)
	}
	orphan := filepath.Join(threads, ".tmp-okf-4242")
	if err := os.WriteFile(orphan, []byte("---\ntype: thread\ntitle: half a pa"), 0o644); err != nil {
		t.Fatal(err)
	}
	if s := e.run(c); outcomes(s) != "ok" {
		t.Fatalf("after the kill: %q\n%s", outcomes(s), e.log)
	}
	e.page(testPage)
	if _, err := os.Stat(orphan); err != nil {
		t.Fatalf("the orphan was touched: %v", err)
	}
	for _, rel := range []string{"projects/eva-md/threads/index.md", "projects/eva-md/now.md"} {
		data, _ := os.ReadFile(filepath.Join(e.cfg.WikiPath, rel))
		if strings.Contains(string(data), "tmp-okf") {
			t.Fatalf("%s lists the orphan:\n%s", rel, data)
		}
	}
	git := exec.Command("git", "-C", e.cfg.WikiPath, "init", "-q")
	if out, err := git.CombinedOutput(); err != nil {
		t.Fatalf("git init: %v %s", err, out)
	}
	if out, err := exec.Command("git", "-C", e.cfg.WikiPath, "check-ignore", "-q", orphan).CombinedOutput(); err != nil {
		t.Fatalf("the orphan is not ignored: %v %s", err, out)
	}
}

// Two jobs never run at once: the second exits as soon as it finds the lock
// held, and the first goes on.
func TestSecondJobExitsAtOnce(t *testing.T) {
	e := newEnv(t)
	c := newChat("3f2a91c0-eeee", start)
	talk(c, 2, "amber")
	file := e.write(c)
	entered, release := make(chan struct{}), make(chan struct{})
	e.fake.Respond = func(r Request) (Result, error) {
		close(entered)
		<-release
		return Result{Output: reply(t, r, "amber"), Attempts: 1}, nil
	}
	done := make(chan Summary)
	go func() {
		s, _ := e.job().Session(context.Background(), c.host, c.session)
		done <- s
	}()
	<-entered
	began := time.Now()
	second, err := e.job().Pending(context.Background())
	if err != nil || !second.Stopped(StopBusy) || len(second.Records) != 0 || time.Since(began) > 2*time.Second {
		t.Fatalf("second job: %+v %v after %s", second, err, time.Since(began))
	}
	close(release)
	if first := <-done; outcomes(first) != "ok" || e.mark(file).Offset != size(t, file) {
		t.Fatalf("first job: %q", outcomes(first))
	}
}

// updates = false stops every update at once, with no call and no record.
func TestUpdatesFalsePauses(t *testing.T) {
	e := newEnv(t)
	e.cfg.Updates = false
	c := newChat("3f2a91c0-ffff", start)
	talk(c, 2, "amber")
	file := e.write(c)
	s := e.run(c)
	if !s.Stopped(StopPaused) || len(s.Records) != 0 || calls(e.fake) != 0 || e.mark(file).Offset != 0 {
		t.Fatalf("paused job: %+v, %d calls", s, calls(e.fake))
	}
	if _, err := os.Stat(filepath.Join(e.dirs.State, "updates")); !errors.Is(err, fs.ErrNotExist) {
		t.Fatalf("a paused job wrote the run log: %v", err)
	}
}

// A locked, human-verified or damaged page is never written: the pass is
// skipped with its reason and no call, and the watermark still moves. A
// page is locked by true, yes or on in any case, as system write reads it.
func TestProtectedPagesAreSkipped(t *testing.T) {
	body := threadpage.Render(threadpage.Page{Title: "eva-md · main", Now: threadpage.Now{Summary: "Hand written."}})
	for _, tc := range []struct {
		reason string
		edit   func(*okfadapter.Concept)
	}{
		{ReasonLocked, func(c *okfadapter.Concept) { c.Extra["locked"] = true }},
		{ReasonLocked, func(c *okfadapter.Concept) { c.Extra["locked"] = "yes" }},
		{ReasonLocked, func(c *okfadapter.Concept) { c.Extra["locked"] = "On" }},
		{ReasonVerified, func(c *okfadapter.Concept) {
			c.Verified = []okfadapter.Verified{{By: "human:alex", At: "2026-01-13T09:00:00Z"}}
		}},
		{ReasonDamaged, func(c *okfadapter.Concept) { c.Body = "\n## Now\n" + threadpage.NowStart + "\nNo end marker.\n" }},
	} {
		t.Run(tc.reason, func(t *testing.T) {
			e := newEnv(t)
			pg := &okfadapter.Concept{Type: "thread", Title: "eva-md · main", Status: "stable", Body: body, Extra: map[string]any{"locked": false}}
			tc.edit(pg)
			if err := e.wiki().Create(testPage, pg, okfadapter.Actor("human", "alex")); err != nil {
				t.Fatal(err)
			}
			raw, _ := os.ReadFile(filepath.Join(e.cfg.WikiPath, testPage+".md"))
			c := newChat("3f2a91c0-0"+tc.reason, start)
			talk(c, 2, "amber")
			file := e.write(c)
			s := e.run(c)
			if outcomes(s) != "skipped ("+tc.reason+")" || calls(e.fake) != 0 || s.Records[0].Calls != 0 {
				t.Fatalf("outcomes %q, %d calls\n%s", outcomes(s), calls(e.fake), e.log)
			}
			if m := e.mark(file); m.Offset != size(t, file) {
				t.Fatalf("watermark %+v", m)
			}
			if after, _ := os.ReadFile(filepath.Join(e.cfg.WikiPath, testPage+".md")); !bytes.Equal(after, raw) {
				t.Fatal("a protected page was written")
			}
		})
	}
}

// A chat outside any project updates the general area's one thread and its
// overview, as setup laid them out.
func TestGeneralAreaPass(t *testing.T) {
	e := newEnv(t)
	c := newChat("general-a", start)
	c.branch = "default"
	talk(c, 2, "amber")
	for i := range c.rows {
		c.rows[i].Project, c.rows[i].Cwd = "general", "Downloads"
	}
	file := e.write(c)
	if s := e.run(c); outcomes(s) != "ok" || e.mark(file).Offset != size(t, file) {
		t.Fatalf("general: %q\n%s", outcomes(s), e.log)
	}
	p, pg := e.page("general/threads/default")
	if p.Now.Summary != "The amber work goes on." || pg.Concept.Title != "General · default" || pg.Concept.Extra["project"] != "general" {
		t.Fatalf("general page: %+v %+v", p.Now, pg.Concept)
	}
	now, err := e.wiki().Read("general/now")
	if err != nil || now.Concept.Title != "General memory" || !strings.Contains(now.Concept.Body, "](threads/default.md) — The amber work goes on.") {
		t.Fatalf("general overview: %v %+v", err, now)
	}
	index, _ := os.ReadFile(filepath.Join(e.cfg.WikiPath, "general", "index.md"))
	if string(index) != "# General\n\n* [General memory](now.md) - Overview of general conversations\n* [Threads](threads/index.md) - Conversation threads\n" {
		t.Fatalf("general index:\n%s", index)
	}
	if _, err := os.Stat(filepath.Join(e.cfg.WikiPath, "projects")); !errors.Is(err, fs.ErrNotExist) {
		t.Fatal("a general chat made a projects folder")
	}
}
