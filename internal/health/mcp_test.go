package health

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/heyNag/eva-md/internal/config"
	"github.com/heyNag/eva-md/internal/identity"
	"github.com/heyNag/eva-md/internal/inbox"
	"github.com/heyNag/eva-md/internal/install"
	"github.com/heyNag/eva-md/internal/paths"
	"github.com/heyNag/eva-md/internal/update"
	"github.com/heyNag/eva-md/internal/wikigit"
)

// marketplaceFor writes the marker install leaves for a plugin host.
func marketplaceFor(t *testing.T, d paths.Dirs, host string) {
	t.Helper()
	put(t, filepath.Join(d.Share, "marketplace", host, ".eva-md-managed"), []byte("eva-md marketplace v1\n"))
}

// Each plugin host's mcp manifest row compares the installed .mcp.json with
// the one install renders: PASS when they match, FAIL when it is missing or
// differs, UNKNOWN when eva-md is not installed for the host.
func TestMCPManifestRows(t *testing.T) {
	d := isolated(t)
	fakeOnPath(t, "claude", "codex")
	binary := filepath.Join(d.Share, "bin", "eva-md")
	marketplaceFor(t, d, "claude")
	marketplaceFor(t, d, "codex")
	put(t, filepath.Join(pluginFolder(d, "claude"), ".mcp.json"), install.MCPManifest("claude", binary))
	put(t, filepath.Join(pluginFolder(d, "codex"), ".mcp.json"), install.MCPManifest("claude", binary))
	var r Report
	inspectMCPManifests(&r, d)
	if c := findRow(t, r, "claude mcp manifest"); c.Status != "PASS" || c.Detail != "declares the eva-md server at the installed binary" {
		t.Fatalf("claude: %+v", c)
	}
	if c := findRow(t, r, "codex mcp manifest"); c.Status != "FAIL" || c.Detail != "the plugin folder predates the MCP server, or differs" || c.Fix != "run eva-md install" {
		t.Fatalf("codex, another host's file: %+v", c)
	}
	if err := os.Remove(filepath.Join(pluginFolder(d, "claude"), ".mcp.json")); err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(filepath.Join(d.Share, "marketplace", "codex", ".eva-md-managed")); err != nil {
		t.Fatal(err)
	}
	r = Report{}
	inspectMCPManifests(&r, d)
	if c := findRow(t, r, "claude mcp manifest"); c.Status != "FAIL" {
		t.Fatalf("claude, a folder from before the server: %+v", c)
	}
	if c := findRow(t, r, "codex mcp manifest"); c.Status != "UNKNOWN" || c.Detail != "eva-md is not installed for codex" || c.Fix != "run eva-md install" {
		t.Fatalf("codex, not installed: %+v", c)
	}
	fakeOnPath(t, "claude")
	r = Report{}
	inspectMCPManifests(&r, d)
	if c := findRow(t, r, "codex mcp manifest"); c.Status != "UNKNOWN" || c.Detail != "Codex isn't installed on this Mac" || c.Fix != "" {
		t.Fatalf("codex, no app: %+v", c)
	}
}

// standInServer installs a script as the stable binary. It records its
// environment and folder, then answers the doctor's handshake with the
// tools given, or as answer says.
func standInServer(t *testing.T, d paths.Dirs, answer string) {
	t.Helper()
	script := "#!/bin/sh\n" +
		"printf '%s|%s|%s|%s\\n' \"$HOME\" \"$PATH\" \"$EVA_MD_CANARY\" \"$(pwd)\" > \"$HOME/probe-env\"\n" +
		"echo ran >> \"$HOME/probe-runs\"\n" + answer
	put(t, filepath.Join(d.Share, "bin", "eva-md"), []byte(script))
	if err := os.Chmod(filepath.Join(d.Share, "bin", "eva-md"), 0o700); err != nil {
		t.Fatal(err)
	}
}

// handshake answers initialize and tools/list, with the tools named, and
// only for a probe that names itself as DoctorClient, so a probe sent
// under another name, which the server would keep waiting, fails.
func handshake(tools string) string {
	return "[ \"$1 $2 $3\" = 'mcp --host claude-code' ] || exit 2\n" +
		"read first\ncase \"$first\" in *'\"method\":\"initialize\"'*'\"name\":\"" + DoctorClient + "\"'*) ;; *) exit 5;; esac\n" +
		"printf '%s\\n' '{\"jsonrpc\":\"2.0\",\"id\":1,\"result\":{\"serverInfo\":{\"name\":\"eva-md\",\"version\":\"test\"}}}'\n" +
		"read second\nread third\ncase \"$third\" in *'\"method\":\"tools/list\"'*) ;; *) exit 7;; esac\n" +
		"printf '%s\\n' '{\"jsonrpc\":\"2.0\",\"id\":2,\"result\":{\"tools\":[" + tools + "]}}'\n" +
		"read rest\n"
}

// The doctor's mcp server row starts the installed binary from / with
// only HOME and PATH, and passes when it answers the handshake with the
// three tools; an empty list is the tools turned off, while a binary from
// before the server or one that lists other tools fails, as does the probe
// of one that hangs, at its time limit, and no binary is not installed.
func TestMCPServerRow(t *testing.T) {
	d := isolated(t)
	t.Setenv("EVA_MD_CANARY", "leaked")
	home := os.Getenv("HOME")
	row := func() Check {
		t.Helper()
		var r Report
		inspectMCPServer(context.Background(), &r, d)
		return findRow(t, r, "mcp server")
	}
	if c := row(); c.Status != "UNKNOWN" || c.Detail != "stable hook binary is not installed" {
		t.Fatalf("no binary: %+v", c)
	}
	standInServer(t, d, handshake(`{"name":"eva_search"},{"name":"eva_show"},{"name":"eva_status"}`))
	if c := row(); c.Status != "PASS" || !strings.HasPrefix(c.Detail, "answers initialize and lists eva_search, eva_show, eva_status in ") {
		t.Fatalf("a server: %+v", c)
	}
	env, err := os.ReadFile(filepath.Join(home, "probe-env"))
	if err != nil || string(env) != home+"|"+os.Getenv("PATH")+"||/\n" {
		t.Fatalf("the probe's environment and folder: %q %v", env, err)
	}
	standInServer(t, d, handshake(""))
	if c := row(); c.Status != "UNKNOWN" || c.Detail != "mcp = false in the config; the tools are off" {
		t.Fatalf("tools off: %+v", c)
	}
	standInServer(t, d, handshake(`{"name":"eva_search"}`))
	if c := row(); c.Status != "FAIL" || c.Detail != "the installed binary lists other tools: eva_search" {
		t.Fatalf("other tools: %+v", c)
	}
	standInServer(t, d, "echo 'eva-md: unknown command \"mcp\"; run eva-md with no arguments for the list' >&2\nexit 2\n")
	if c := row(); c.Status != "FAIL" || c.Detail != "the installed binary did not answer as an MCP server" || c.Fix != "run eva-md install" {
		t.Fatalf("an old binary: %+v", c)
	}
	// The stand-in reads the requests and never answers, so only the probe's
	// own limit, short here to keep the test quick, can end it; any error is
	// the FAIL row above.
	standInServer(t, d, "read a\nread b\nread c\nread d\n")
	const limit = 50 * time.Millisecond
	start := time.Now()
	probed := make(chan error, 1)
	go func() {
		_, _, err := probeMCP(context.Background(), filepath.Join(d.Share, "bin", "eva-md"), limit)
		probed <- err
	}()
	select {
	case err := <-probed:
		if err == nil || time.Since(start) < limit {
			t.Fatalf("a hang, after %v: %v", time.Since(start), err)
		}
	case <-time.After(time.Minute):
		t.Fatal("the probe of a server that hangs had not ended a minute later: it ignores its time limit")
	}
}

// eva-md status has the manifest rows, which compare files, and starts no
// server; the doctor's probe is its own.
func TestStatusStartsNoServer(t *testing.T) {
	d := isolated(t)
	standInServer(t, d, handshake(`{"name":"eva_search"},{"name":"eva_show"},{"name":"eva_status"}`))
	r := Status(context.Background(), d)
	findRow(t, r, "claude mcp manifest")
	findRow(t, r, "codex mcp manifest")
	for _, c := range r.Checks {
		if c.Name == "mcp server" {
			t.Fatalf("status probed the server: %+v", c)
		}
	}
	if _, err := os.Stat(filepath.Join(os.Getenv("HOME"), "probe-runs")); !os.IsNotExist(err) {
		t.Fatalf("status started the binary: %v", err)
	}
}

// The newest heartbeat of an event, or of any of a host's events.
func TestNewestBeat(t *testing.T) {
	d := isolated(t)
	now := time.Now()
	if _, ok := NewestBeat(d, "codex", "", now); ok {
		t.Fatal("a heartbeat with none written")
	}
	saveBeat(t, d, "codex", "SessionStart", now.Add(-2*time.Hour))
	saveBeat(t, d, "codex", "Stop", now.Add(-time.Hour))
	saveBeat(t, d, "codex", "PreCompact", now.Add(time.Hour)) // from the future: not valid
	if at, ok := NewestBeat(d, "codex", "", now); !ok || !at.Equal(now.Add(-time.Hour)) {
		t.Fatalf("any event: %v %v", at, ok)
	}
	if at, ok := NewestBeat(d, "codex", "SessionStart", now); !ok || !at.Equal(now.Add(-2*time.Hour)) {
		t.Fatalf("SessionStart: %v %v", at, ok)
	}
	if _, ok := NewestBeat(d, "claude-code", "", now); ok {
		t.Fatal("another host's heartbeat")
	}
}

// A project's backlog counts its session files longer than their
// watermark, and the bytes past it, leaving out a damaged watermark and
// another project's files.
func TestBacklog(t *testing.T) {
	d := isolated(t)
	p := identity.Project{Key: "github.com__example__a"}
	store := inbox.Store{Root: d.Inbox()}
	for i, id := range []string{"done", "waiting", "pending", "damaged"} {
		file := store.Path(p.Key, "codex", id)
		put(t, file, []byte(strings.Repeat("{}\n", 10)))
		var m inbox.Mark
		switch i {
		case 0:
			m = inbox.Mark{Offset: 30}
		case 1:
			m = inbox.Mark{Wait: 30}
		case 2:
			m = inbox.Mark{Offset: 6}
		case 3:
			m = inbox.Mark{Offset: 300}
		}
		if err := store.SetMark(file, m); err != nil {
			t.Fatal(err)
		}
	}
	put(t, store.Path("github.com__example__b", "codex", "other"), []byte(strings.Repeat("{}\n", 100)))
	if n, size, err := Backlog(context.Background(), d, p); err != nil || n != 1 || size != 24 {
		t.Fatalf("%d sessions, %d bytes, %v", n, size, err)
	}
	if n, size, err := Backlog(context.Background(), d, identity.Project{Key: "github.com__example__none"}); err != nil || n != 0 || size != 0 {
		t.Fatalf("no folder: %d %d %v", n, size, err)
	}
}

// Summary's two lines: each host's hooks, when they last ran and how many
// of their runs failed, Codex's awaiting approval; then each host's last
// update, the daily commit and the push.
func TestSummary(t *testing.T) {
	d := isolated(t)
	c := wikiConfig(t)
	marketplaceFor(t, d, "claude")
	marketplaceFor(t, d, "codex")
	putRuns(t, d, "claude-code", "Stop", append(series(clock.Add(-10*time.Minute), 3, false), hookRun{At: clock.Add(-5 * time.Minute), Failed: true}))
	put(t, filepath.Join(d.State, "needs-trust.json"), []byte(`{"host":"codex","needs_trust":true}`))
	plantRunLog(t, d, okRecord(ago(3*time.Hour), update.HostClaude, "0199a000-0001"),
		failedRecord(ago(2*time.Hour), update.HostCodex, "0199a000-0002", update.ReasonAuth))
	plantCommits(t, d, wikigit.Result{At: ago(5 * time.Hour), Outcome: wikigit.Committed, Pages: 3, Push: wikigit.PushPending})
	got := Summary(context.Background(), d, c, clock)
	want := []string{
		"hooks: claude-code ran 5 minutes ago (1 of the last 4 runs failed), codex never ran; Codex hooks await approval in /hooks",
		"updates: claude-code last " + when(ago(3*time.Hour)) + " ok, codex paused today (not logged in); daily commit " + when(ago(5*time.Hour)) + " committed, push pending since " + clock.Add(-5*time.Hour).Format("2006-01-02 15:04"),
	}
	if strings.Join(got, "\n") != strings.Join(want, "\n") {
		t.Fatalf("summary:\n%s\nwant:\n%s", strings.Join(got, "\n"), strings.Join(want, "\n"))
	}
	c.Updates, c.Commits = false, false
	if got := Summary(context.Background(), d, c, clock); got[1] != "updates: off (updates = false); daily commit off (commits = false)" {
		t.Fatalf("off: %q", got[1])
	}
	if got := Summary(context.Background(), d, config.Default(), clock); got[1] != "updates: no wiki is set up" {
		t.Fatalf("no wiki: %q", got[1])
	}
}
