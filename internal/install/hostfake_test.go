package install

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/heyNag/eva-md/internal/config"
	"github.com/heyNag/eva-md/internal/hostfake"
)

// The written fake CLIs run this test binary again; as one of them, it
// answers and exits here, before any test runs.
func init() { hostfake.Main() }

// The tests here run v1's install flows on the fake hosts, whose Codex
// binds hook approval to the plugin's copy, and check that install's
// NeedsTrust agrees with what the fake's hooks/list says.

// onFakeHosts makes a home as newHome does, with setup's config, and a fake
// whose root is outside the home.
func onFakeHosts(t *testing.T) (Options, *hostfake.Fake) {
	t.Helper()
	home, dirs := newHome(t)
	c := config.Default()
	c.WikiPath = filepath.Join(home, "wiki")
	c.ExpectedRemote = "github.com/example/wiki"
	c.Instance = "test-12345678"
	c.MachineLabel = "test"
	if err := config.Save(dirs.ConfigFile(), c); err != nil {
		t.Fatal(err)
	}
	root, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	return Options{Dirs: dirs, Binary: scriptBinary(t, "1"), Host: "all"}, hostfake.New(root)
}

// scriptBinary is a stand-in for the eva-md binary, outside the home, that
// a fired hook can run: it reads the payload and prints a reply naming its
// build.
func scriptBinary(t *testing.T, build string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "eva-md")
	script := "#!/bin/sh\ncat >/dev/null\necho '{\"build\":\"" + build + "\"}'\n"
	if err := os.WriteFile(path, []byte(script), 0o700); err != nil {
		t.Fatal(err)
	}
	return path
}

const fakeListRequest = `{"id":1,"method":"initialize","params":{"clientInfo":{"name":"test","version":"1"}}}
{"method":"initialized","params":{}}
{"id":2,"method":"hooks/list","params":{"cwds":["/made-up/project"]}}
`

// codexTrust is what the fake's hooks/list says of eva-md's Codex hooks, by
// event.
func codexTrust(t *testing.T, f *hostfake.Fake) map[string]string {
	t.Helper()
	out, err := f.RunStdin(context.Background(), []byte(fakeListRequest), "codex", "--no-daemon", "app-server", "--stdio")
	if err != nil {
		t.Fatal(err)
	}
	trust := map[string]string{}
	s := bufio.NewScanner(bytes.NewReader(out))
	for s.Scan() {
		var reply struct {
			ID     int
			Result struct {
				Data []struct {
					Hooks []struct {
						EventName   string
						PluginID    string `json:"pluginId"`
						TrustStatus string `json:"trustStatus"`
					}
				}
			}
		}
		if err := json.Unmarshal(s.Bytes(), &reply); err != nil {
			t.Fatal(err)
		}
		for _, d := range reply.Result.Data {
			for _, h := range d.Hooks {
				if reply.ID == 2 && h.PluginID == "eva-md@eva-md" {
					trust[h.EventName] = h.TrustStatus
				}
			}
		}
	}
	return trust
}

// needsReview says the fake has an eva-md Codex hook that won't run until a
// person reviews it.
func needsReview(trust map[string]string) bool {
	for _, word := range trust {
		if word != hostfake.Trusted && word != hostfake.Managed {
			return true
		}
	}
	return false
}

// wantCodexTrust checks eva-md's seven Codex hooks are listed, each with
// word, and that NeedsTrust agrees with the fake.
func wantCodexTrust(t *testing.T, f *hostfake.Fake, word string, needsTrust bool) {
	t.Helper()
	trust := codexTrust(t, f)
	if len(trust) != 7 {
		t.Errorf("the fake lists %d of eva-md's Codex hooks, want 7: %v", len(trust), trust)
	}
	for event, got := range trust {
		if got != word {
			t.Errorf("Codex's %s hook is %s, want %s", event, got, word)
		}
	}
	if needsReview(trust) != needsTrust {
		t.Errorf("NeedsTrust is %t, but the fake's hooks/list says %v", needsTrust, trust)
	}
}

// fired fires SessionStart on cli and gives its one eva-md hook's result.
func fired(t *testing.T, f *hostfake.Fake, cli string) hostfake.Result {
	t.Helper()
	results, err := f.Fire(context.Background(), cli, "SessionStart", []byte(`{"hook_event_name":"SessionStart","source":"startup"}`))
	if err != nil {
		t.Fatal(err)
	}
	if len(results) != 1 {
		t.Fatalf("%s fired %d SessionStart hooks, want 1: %+v", cli, len(results), results)
	}
	return results[0]
}

// wantRanBuild checks the hook ran the binary of build.
func wantRanBuild(t *testing.T, r hostfake.Result, build string) {
	t.Helper()
	if r.Status != hostfake.Ran || r.Exit != 0 || string(r.Stdout) != `{"build":"`+build+`"}`+"\n" {
		t.Errorf("%s: %s (%s), exit %d, printed %q; want build %s to run", r.Key, r.Status, r.Reason, r.Exit, r.Stdout, build)
	}
}

// registrations are the calls that register a plugin or a marketplace.
func registrations(calls []hostfake.Call) []string {
	var found []string
	for _, c := range calls {
		s := c.String()
		if strings.Contains(s, " plugin add ") || strings.Contains(s, " plugin install ") || strings.Contains(s, " marketplace add ") {
			found = append(found, s)
		}
	}
	return found
}

func fakeCalls(t *testing.T, f *hostfake.Fake) []hostfake.Call {
	t.Helper()
	calls, err := f.Calls()
	if err != nil {
		t.Fatal(err)
	}
	return calls
}

// A first install leaves Codex's hooks untrusted, and unrun, until the
// person approves them; NeedsTrust says so. Claude Code's run at once.
// Through the written CLIs, Inspect then reports both hosts installed and
// valid.
func TestFirstInstallOnTheFakeHosts(t *testing.T) {
	opts, f := onFakeHosts(t)
	result, err := runInstall(context.Background(), opts, f.Run)
	if err != nil {
		t.Fatal(err)
	}
	if !result.NeedsTrust {
		t.Error("a first install doesn't say Codex's hooks need a review")
	}
	wantCodexTrust(t, f, hostfake.Untrusted, result.NeedsTrust)
	if r := fired(t, f, "codex"); r.Status != hostfake.Skipped {
		t.Errorf("an unapproved Codex hook was %s (%s)", r.Status, r.Reason)
	}
	wantRanBuild(t, fired(t, f, "claude"), "1")
	if err := f.Approve("codex"); err != nil {
		t.Fatal(err)
	}
	wantCodexTrust(t, f, hostfake.Trusted, false)
	wantRanBuild(t, fired(t, f, "codex"), "1")

	bin := filepath.Join(t.TempDir(), "bin")
	if err := f.WriteCLIs(bin); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", bin)
	before := len(fakeCalls(t, f))
	for _, host := range []string{"claude", "codex"} {
		s, err := Inspect(context.Background(), opts.Dirs, host)
		if err != nil || !s.Available || !s.Installed || !s.Enabled || !s.Valid || !s.Marketplace {
			t.Errorf("Inspect %s through the written CLIs: %+v, %v", host, s, err)
		}
	}
	if after := len(fakeCalls(t, f)); after <= before {
		t.Error("Inspect didn't reach the written CLIs")
	}
}

// A reinstall that changes only the binary registers nothing, needs no
// review and keeps Codex's hooks trusted; they run the new binary.
func TestBinaryOnlyReinstallKeepsCodexTrust(t *testing.T) {
	opts, f := onFakeHosts(t)
	if _, err := runInstall(context.Background(), opts, f.Run); err != nil {
		t.Fatal(err)
	}
	if err := f.Approve("codex"); err != nil {
		t.Fatal(err)
	}
	before := len(fakeCalls(t, f))
	opts.Binary = scriptBinary(t, "2")
	result, err := runInstall(context.Background(), opts, f.Run)
	if err != nil {
		t.Fatal(err)
	}
	if found := registrations(fakeCalls(t, f)[before:]); len(found) != 0 {
		t.Errorf("a binary-only reinstall registered %v", found)
	}
	if result.NeedsTrust {
		t.Error("a binary-only reinstall says Codex's hooks need a review")
	}
	if !slices.Contains(result.Actions, "replace stable binary") {
		t.Errorf("the reinstall didn't replace the binary: %v", result.Actions)
	}
	wantCodexTrust(t, f, hostfake.Trusted, result.NeedsTrust)
	wantRanBuild(t, fired(t, f, "codex"), "2")
	wantRanBuild(t, fired(t, f, "claude"), "2")
}

// The upgrade over 2026.11.5, whose Codex hooks were approved, changes
// Codex's plugin: NeedsTrust says so, the fake says modified, and Codex's
// hooks fail until a review. Claude Code's run the new binary.
func TestUpgradeFrom2026_11_5OnTheFakeHosts(t *testing.T) {
	home, dirs := newHome(t)
	layDown(t, home, readFixture(t, home))
	root, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	f := hostfake.New(root)
	ctx := context.Background()
	for _, host := range []string{"claude", "codex"} {
		if _, err := f.Run(ctx, host, "plugin", "marketplace", "add", marketplacePath(dirs.Share, host), "--json"); err != nil {
			t.Fatal(err)
		}
		if _, err := f.Run(ctx, host, addArgs(host)...); err != nil {
			t.Fatal(err)
		}
	}
	if err := f.Approve("codex"); err != nil {
		t.Fatal(err)
	}
	wantCodexTrust(t, f, hostfake.Trusted, false)
	for _, host := range []string{"claude", "codex"} {
		if s, err := inspect(ctx, dirs, host, f.Run); err != nil || !s.Valid {
			t.Fatalf("2026.11.5 on the fake %s: %+v, %v", host, s, err)
		}
	}

	opts := Options{Dirs: dirs, Binary: scriptBinary(t, "current"), Host: "all"}
	result, err := upgrade(t, opts, f.Run)
	if err != nil {
		t.Fatalf("the install over 2026.11.5 failed: %v", err)
	}
	if !result.NeedsTrust {
		t.Error("the upgrade doesn't say Codex's hooks need a review")
	}
	wantCodexTrust(t, f, hostfake.Modified, result.NeedsTrust)
	if r := fired(t, f, "codex"); r.Status != hostfake.Failed || r.Reason != "plugin contents changed; hook approval required" {
		t.Errorf("Codex's hook after the upgrade: %s (%s)", r.Status, r.Reason)
	}
	wantRanBuild(t, fired(t, f, "claude"), "current")
	if s, err := inspect(ctx, dirs, "claude", f.Run); err != nil || !s.Valid {
		t.Errorf("Claude Code after the upgrade: %+v, %v", s, err)
	}
}

// Uninstall removes both registrations and Codex's copy: the fake lists no
// eva-md hook, fires none, and NeedsTrust is false.
func TestUninstallOnTheFakeHosts(t *testing.T) {
	opts, f := onFakeHosts(t)
	if _, err := runInstall(context.Background(), opts, f.Run); err != nil {
		t.Fatal(err)
	}
	if err := f.Approve("codex"); err != nil {
		t.Fatal(err)
	}
	result, err := uninstall(context.Background(), opts.Dirs, true, f.Run)
	if err != nil {
		t.Fatal(err)
	}
	trust := codexTrust(t, f)
	if len(trust) != 0 || needsReview(trust) != result.NeedsTrust {
		t.Errorf("after uninstall the fake lists %v, NeedsTrust %t", trust, result.NeedsTrust)
	}
	for _, host := range []string{"claude", "codex"} {
		results, err := f.Fire(context.Background(), host, "SessionStart", []byte(`{}`))
		if err != nil || len(results) != 0 {
			t.Errorf("%s fired %+v after uninstall: %v", host, results, err)
		}
		if s, err := inspect(context.Background(), opts.Dirs, host, f.Run); err != nil || s.Installed || s.Marketplace {
			t.Errorf("%s after uninstall: %+v, %v", host, s, err)
		}
	}
	if _, err := os.Stat(f.CachePath("eva-md", "eva-md")); !errors.Is(err, os.ErrNotExist) {
		t.Errorf("Codex's copy is still there: %v", err)
	}
}

// With Codex's hooks forced off by policy, install refuses with
// ErrHooksOff, registers nothing, and agrees with the fake, which lists no
// hook.
func TestHooksForcedOffOnTheFakeHosts(t *testing.T) {
	opts, f := onFakeHosts(t)
	if err := f.SetPolicy("codex", hostfake.Policy{CodexHooksOff: true}); err != nil {
		t.Fatal(err)
	}
	result, err := runInstall(context.Background(), opts, f.Run)
	if !errors.Is(err, ErrHooksOff) {
		t.Fatalf("install with Codex's hooks forced off: %v", err)
	}
	if found := registrations(fakeCalls(t, f)); len(found) != 0 {
		t.Errorf("the refused install registered %v", found)
	}
	trust := codexTrust(t, f)
	if len(trust) != 0 || needsReview(trust) != result.NeedsTrust {
		t.Errorf("the fake lists %v, NeedsTrust %t", trust, result.NeedsTrust)
	}
	if _, err := os.Stat(opts.Dirs.Share); !errors.Is(err, os.ErrNotExist) {
		t.Errorf("the refused install made %s: %v", opts.Dirs.Share, err)
	}
}
