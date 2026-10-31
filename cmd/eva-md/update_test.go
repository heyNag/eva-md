package main

import (
	"bytes"
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/heyNag/eva-md/internal/config"
	"github.com/heyNag/eva-md/internal/inbox"
	"github.com/heyNag/eva-md/internal/okfadapter"
	"github.com/heyNag/eva-md/internal/paths"
	"github.com/heyNag/eva-md/internal/update"
)

// updateFixture readies a home for eva-md update: a wiki and a config that
// names it, a PATH holding only stand-ins for claude and codex that fail if
// anything starts them, and a fake runner answering with output.
func updateFixture(t *testing.T, output string) (paths.Dirs, *update.Fake) {
	t.Helper()
	d := isolated(t)
	bin := filepath.Join(t.TempDir(), "bin")
	if err := os.Mkdir(bin, 0o700); err != nil {
		t.Fatal(err)
	}
	for _, cli := range []string{"claude", "codex"} {
		script := "#!/bin/sh\necho 'a stand-in ran: no test may start a host CLI' >&2\nexit 97\n"
		if err := os.WriteFile(filepath.Join(bin, cli), []byte(script), 0o700); err != nil {
			t.Fatal(err)
		}
	}
	t.Setenv("PATH", bin+":/usr/bin:/bin")
	wiki := filepath.Join(os.Getenv("HOME"), "eva-wiki")
	if err := okfadapter.Init(wiki); err != nil {
		t.Fatal(err)
	}
	if err := config.SaveSchema(filepath.Join(wiki, ".eva-md", "schema.toml"), config.WikiSchema{SchemaVersion: 1, OKFSpec: "0.2", Instance: "test-0a1b2c3d"}); err != nil {
		t.Fatal(err)
	}
	c := config.Default()
	c.WikiPath, c.Instance = wiki, "test-0a1b2c3d"
	if err := config.Save(d.ConfigFile(), c); err != nil {
		t.Fatal(err)
	}
	// As install does, before any chat is captured.
	if err := update.Baseline(d, time.Now()); err != nil {
		t.Fatal(err)
	}
	fake := &update.Fake{Result: update.Result{Output: []byte(output), Attempts: 1}}
	saved := newRunner
	newRunner = func(string) update.Runner { return fake }
	t.Cleanup(func() { newRunner = saved })
	return d, fake
}

// hookTurn sends one exchange through the real hook, as Claude Code would.
func hookTurn(t *testing.T, cwd, sid, prompt, reply string) {
	t.Helper()
	for _, p := range []map[string]any{
		{"hook_event_name": "UserPromptSubmit", "session_id": sid, "cwd": cwd, "prompt": prompt},
		{"hook_event_name": "Stop", "session_id": sid, "cwd": cwd, "last_assistant_message": reply},
	} {
		data, _ := json.Marshal(p)
		var out bytes.Buffer
		if code := run(context.Background(), []string{"hook", "--host", "claude-code"}, bytes.NewReader(data), &out, &bytes.Buffer{}); code != 0 {
			t.Fatalf("hook %v: %d", p["hook_event_name"], code)
		}
	}
}

// The answer a model would give the first batch of a chat: entry 1 is the
// user's first prompt.
const tangerine = `{"summary":"The tangerine migration is planned.","last_chat":"Planned the tangerine migration.","done":[],` +
	`"items":[{"id":null,"kind":"next","action":"add","text":"Run the tangerine migration","tag":"stated","refs":[1],"reason":null,"replacement":null}],` +
	`"timeline":[{"kind":"decision","text":"Use the tangerine switch","tag":"stated","refs":[1]}],"unknowns":[]}`

// The deterministic resume test with updates on: entries the hooks
// captured, then eva-md update --foreground with a canned valid answer,
// give a thread page eva-md brief shows as a fresh Now. The dry run before
// it prints the same bytes twice and calls nothing; a detached run logs to
// the day's log, not to the terminal.
func TestUpdateGivesTheBriefAFreshNow(t *testing.T) {
	d, fake := updateFixture(t, tangerine)
	cwd, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	sid := "update-e2e-session"
	for _, word := range []string{"first", "second", "third"} {
		hookTurn(t, cwd, sid, "Plan the tangerine migration, the "+word+" part, with the switch kept behind a flag until the dry run looks right. "+strings.Repeat("Context. ", 30),
			"Planned the "+word+" part of the tangerine migration; the flag stays off until the dry run passes. "+strings.Repeat("Detail. ", 30))
	}
	var dry [2]string
	for i := range dry {
		var out, errOut bytes.Buffer
		if code := run(context.Background(), []string{"update", "--session", "claude-code:" + sid, "--dry-run"}, strings.NewReader(""), &out, &errOut); code != 0 {
			t.Fatalf("dry run %d: %s", code, errOut.String())
		}
		dry[i] = out.String()
	}
	if dry[0] != dry[1] || !strings.Contains(dry[0], "no call was made") || len(fake.Requests()) != 0 {
		t.Fatalf("dry runs:\n%s\n---\n%s", dry[0], dry[1])
	}
	var out, errOut bytes.Buffer
	if code := run(context.Background(), []string{"update", "--session", "claude-code:" + sid, "--foreground"}, strings.NewReader(""), &out, &errOut); code != 0 {
		t.Fatalf("update %d: %s %s", code, out.String(), errOut.String())
	}
	if out.String() != "update: 1 pass: 1 ok\n" || !strings.Contains(errOut.String(), ": ok, ") || len(fake.Requests()) != 1 {
		t.Fatalf("update printed %q, stderr %q", out.String(), errOut.String())
	}
	out.Reset()
	errOut.Reset()
	if code := run(context.Background(), []string{"brief", "--cwd", cwd, "--host", "claude-code"}, strings.NewReader(""), &out, &errOut); code != 0 {
		t.Fatalf("brief %d %s", code, errOut.String())
	}
	brief := out.String()
	if !strings.Contains(brief, "Now (as of ") || strings.Contains(brief, "stale") || !strings.Contains(brief, "Run the tangerine migration") || strings.Contains(brief, "No summarized Now page yet") {
		t.Fatalf("the brief has no fresh Now:\n%s", brief)
	}
	hookTurn(t, cwd, sid, "Now plan the rollback of the tangerine migration. "+strings.Repeat("Context. ", 40), "The rollback reverts the flag and the schema. "+strings.Repeat("Detail. ", 40))
	// The page now holds n1, which every answer must give an action.
	fake.Result.Output = []byte(strings.Replace(tangerine, `"items":[`, `"items":[{"id":"n1","kind":"next","action":"keep","text":null,"tag":"stated","refs":[],"reason":null,"replacement":null},`, 1))
	out.Reset()
	errOut.Reset()
	if code := run(context.Background(), []string{"update", "--session", "claude-code:" + sid}, strings.NewReader(""), &out, &errOut); code != 0 {
		t.Fatalf("detached update %d: %s %s", code, out.String(), errOut.String())
	}
	logged, err := os.ReadFile(filepath.Join(d.Logs(), "update-"+time.Now().Format(time.DateOnly)+".log"))
	if errOut.Len() != 0 || !strings.HasPrefix(out.String(), "update: 1 pass: ") || err != nil || !strings.Contains(string(logged), "claude-code:update-e") {
		t.Fatalf("detached run: stdout %q stderr %q log %q %v", out.String(), errOut.String(), logged, err)
	}
}

func TestUpdateCommandErrors(t *testing.T) {
	isolated(t)
	for _, tc := range []struct {
		args []string
		code int
		want string
	}{
		{[]string{"update"}, 2, "needs --session host:id or --pending"},
		{[]string{"update", "--pending", "--session", "codex:x"}, 2, "not both"},
		{[]string{"update", "--pending", "--dry-run"}, 2, "--dry-run needs --session"},
		{[]string{"update", "--session", "codex"}, 2, "takes host:id"},
		{[]string{"update", "--session", "gemini:x"}, 2, "takes host:id"},
		{[]string{"update", "--session", "codex:x", "extra"}, 2, "invalid arguments"},
		{[]string{"update", "--session", "codex:x", "--session", "codex"}, 2, "takes host:id"},
		{[]string{"update", "--session", "codex:x", "--session", "codex:y", "--dry-run"}, 2, "--dry-run takes one --session"},
		{[]string{"update", "--session", "codex:x"}, 1, "no wiki is set up"},
	} {
		var out, errOut bytes.Buffer
		if code := run(context.Background(), tc.args, strings.NewReader(""), &out, &errOut); code != tc.code || !strings.Contains(errOut.String(), tc.want) {
			t.Errorf("%v: %d %q %q", tc.args, code, out.String(), errOut.String())
		}
	}
	updateFixture(t, tangerine)
	var out, errOut bytes.Buffer
	if code := run(context.Background(), []string{"update", "--session", "codex:no-such-session", "--foreground"}, strings.NewReader(""), &out, &errOut); code != 1 || !strings.Contains(errOut.String(), "no inbox file for codex:no-such-session") {
		t.Fatalf("missing session: %d %q %q", code, out.String(), errOut.String())
	}
	out.Reset()
	errOut.Reset()
	if code := run(context.Background(), []string{"update", "--session", "codex:no-such-session", "--session", "claude-code:nor-this", "--foreground"}, strings.NewReader(""), &out, &errOut); code != 1 || !strings.Contains(errOut.String(), "no inbox file for codex:no-such-session or claude-code:nor-this") {
		t.Fatalf("missing sessions: %d %q %q", code, out.String(), errOut.String())
	}
	out.Reset()
	if code := run(context.Background(), []string{"update", "--pending", "--foreground"}, strings.NewReader(""), &out, &errOut); code != 0 || out.String() != "update: nothing pending\n" {
		t.Fatalf("empty inbox: %d %q", code, out.String())
	}
}

// eva-md update takes --session more than once, as the start-of-chat
// catch-up spawns it, and drains every session it names in one job: here
// two chats on two branches, each its own thread page.
func TestUpdateTakesSeveralSessions(t *testing.T) {
	d, fake := updateFixture(t, tangerine)
	store := inbox.Store{Root: d.Inbox()}
	for _, chat := range []struct{ sid, branch string }{{"several-one", "main"}, {"several-two", "feature/two"}} {
		var lines []byte
		for i, kind := range []string{"prompt", "reply", "prompt", "reply"} {
			role := map[string]string{"prompt": "user", "reply": "assistant"}[kind]
			e := inbox.Entry{V: 1, TS: time.Date(2026, 1, 14, 10, i, 0, 0, time.UTC).Format(time.RFC3339), Host: update.HostClaude, Session: chat.sid,
				Role: role, Kind: kind, Text: "Plan the tangerine migration, part " + strconv.Itoa(i) + ", with the switch kept behind a flag. " + strings.Repeat("Context. ", 40), Project: "github.com__you__demo", Branch: chat.branch, Origin: "hook"}
			data, err := json.Marshal(e)
			if err != nil {
				t.Fatal(err)
			}
			lines = append(append(lines, data...), '\n')
		}
		file := store.Path("github.com__you__demo", update.HostClaude, chat.sid)
		if err := os.MkdirAll(filepath.Dir(file), 0o700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(file, lines, 0o600); err != nil {
			t.Fatal(err)
		}
	}
	var out, errOut bytes.Buffer
	if code := run(context.Background(), []string{"update", "--session", "claude-code:several-one", "--session", "claude-code:several-two", "--foreground"}, strings.NewReader(""), &out, &errOut); code != 0 {
		t.Fatalf("update %d: %s %s", code, out.String(), errOut.String())
	}
	if out.String() != "update: 2 passes: 2 ok\n" || len(fake.Requests()) != 2 {
		t.Fatalf("update printed %q, stderr %q, %d calls", out.String(), errOut.String(), len(fake.Requests()))
	}
}

// A stop exits 0 with its summary line, a failed pass exits 1 with its, and
// a job that can't start, here for a missing wiki, exits 1 and prints no
// summary, since nothing was checked.
func TestUpdateCommandExitCodes(t *testing.T) {
	d, fake := updateFixture(t, tangerine)
	base, err := config.Load(d.ConfigFile())
	if err != nil {
		t.Fatal(err)
	}
	file := inbox.Store{Root: d.Inbox()}.Path("github.com__you__demo", update.HostClaude, "exit-codes")
	var lines []byte
	for i, kind := range []string{"prompt", "reply", "prompt", "reply"} {
		role := map[string]string{"prompt": "user", "reply": "assistant"}[kind]
		e := inbox.Entry{V: 1, TS: time.Date(2026, 1, 14, 10, i, 0, 0, time.UTC).Format(time.RFC3339), Host: update.HostClaude, Session: "exit-codes",
			Role: role, Kind: kind, Text: "Plan the demo migration, part " + strconv.Itoa(i) + ". " + strings.Repeat("Context. ", 40), Project: "github.com__you__demo", Branch: "main", Origin: "hook"}
		data, err := json.Marshal(e)
		if err != nil {
			t.Fatal(err)
		}
		lines = append(append(lines, data...), '\n')
	}
	if err := os.MkdirAll(filepath.Dir(file), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(file, lines, 0o600); err != nil {
		t.Fatal(err)
	}
	fake.Result = update.Result{Reason: update.ReasonRateLimit, Attempts: 1}
	for _, tc := range []struct {
		name   string
		edit   func(*config.Config)
		args   []string
		code   int
		out    string
		stderr string
	}{
		{"paused", func(c *config.Config) { c.Updates = false }, []string{"update", "--pending", "--foreground"}, 0, "update: stopped: paused\n", "stopped: paused"},
		{"failed", func(*config.Config) {}, []string{"update", "--session", "claude-code:exit-codes", "--foreground"}, 1, "update: 1 pass: 1 failed\n", "failed (rate_limit)"},
		{"no wiki", func(c *config.Config) { c.WikiPath = filepath.Join(os.Getenv("HOME"), "missing-wiki") }, []string{"update", "--pending", "--foreground"}, 1, "", "eva-md: okfadapter: wiki"},
	} {
		c := base
		tc.edit(&c)
		if err := config.Save(d.ConfigFile(), c); err != nil {
			t.Fatal(err)
		}
		var out, errOut bytes.Buffer
		code := run(context.Background(), tc.args, strings.NewReader(""), &out, &errOut)
		if code != tc.code || out.String() != tc.out || !strings.Contains(errOut.String(), tc.stderr) {
			t.Errorf("%s: %d, stdout %q, stderr %q", tc.name, code, out.String(), errOut.String())
		}
	}
	if n := len(fake.Requests()); n != 1 {
		t.Fatalf("%d calls", n)
	}
}

// eva-md with no arguments names update among its commands.
func TestUsageNamesUpdate(t *testing.T) {
	var errOut bytes.Buffer
	if code := run(context.Background(), nil, strings.NewReader(""), &bytes.Buffer{}, &errOut); code != 2 || !strings.Contains(errOut.String(), "| update |") {
		t.Fatalf("%d %q", code, errOut.String())
	}
}

// Install's line for chats from before updates it couldn't set aside says
// so, and not that the first update will.
func TestSetAsideSaysWhatIsLeft(t *testing.T) {
	d := isolated(t)
	file := inbox.Store{Root: d.Inbox()}.Path("github.com__you__demo", update.HostClaude, "left-behind")
	if err := os.MkdirAll(filepath.Join(filepath.Dir(file), ".state"), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(file, []byte(`{"v":1,"role":"user","kind":"prompt","text":"hello"}`+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	state := filepath.Join(filepath.Dir(file), ".state")
	if err := os.Chmod(state, 0o500); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chmod(state, 0o700) })
	var errOut bytes.Buffer
	setAside(d, &errOut)
	if got := errOut.String(); !strings.HasPrefix(got, "eva-md: 1 chat captured before updates couldn't be set aside yet, as its file or watermarks can't be read or written;") || strings.Contains(got, "will be set aside by the first update") || strings.Contains(got, "folder") {
		t.Fatalf("stderr %q", got)
	}
}
