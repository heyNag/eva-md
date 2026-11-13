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
// NeedsTrust agrees with what the fake's hooks/list says. install is given
// the fake's Run through asNative, so that it fails as install's own
// runner does.

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

// asNative is the fake's Run as install's own runner fails: a command that
// failed gives no output and a hostError, whose diagnostics are what the
// fake printed on stderr, as native gives. install reads Codex's
// missing-folder failure from a hostError alone, so the fake's ExitError,
// passed on as it is, would hide it.
func asNative(f *hostfake.Fake) runner {
	return func(ctx context.Context, cli string, args ...string) ([]byte, error) {
		out, err := f.Run(ctx, cli, args...)
		var e *hostfake.ExitError
		if errors.As(err, &e) {
			return nil, &hostError{msg: "install: " + commandDescription(cli, args) + " failed", diagnostics: e.Stderr}
		}
		if err != nil {
			return nil, err
		}
		return out, nil
	}
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

// codexWireEvents are the events of eva-md's Codex plugin as the app
// server's wire spells them, in camelCase: the names
// internal/health/hosts.go accepts.
var codexWireEvents = []string{"sessionStart", "userPromptSubmit", "postToolUse", "stop", "preCompact", "interrupt", "sessionEnd"}

// codexTrust is what the fake's hooks/list says of eva-md's Codex hooks, by
// event. It reads the reply as internal/health/hosts.go does, and fails
// the test on what the doctor would reject: an error or a warning, or an
// eva-md hook whose event is not one of codexWireEvents or is listed twice.
func codexTrust(t *testing.T, f *hostfake.Fake) map[string]string {
	t.Helper()
	out, err := f.RunStdin(context.Background(), []byte(fakeListRequest), "codex", "--no-daemon", "app-server", "--stdio")
	if err != nil {
		t.Fatal(err)
	}
	trust := map[string]string{}
	found := false
	s := bufio.NewScanner(bytes.NewReader(out))
	for s.Scan() {
		var reply struct {
			ID     int
			Result struct {
				Errors   []json.RawMessage
				Warnings []json.RawMessage
				Data     []struct {
					Errors   []json.RawMessage
					Warnings []json.RawMessage
					Hooks    []struct {
						EventName   string
						PluginID    string `json:"pluginId"`
						TrustStatus string `json:"trustStatus"`
					} `json:"hooks"`
				} `json:"data"`
			}
		}
		if err := json.Unmarshal(s.Bytes(), &reply); err != nil {
			t.Fatal(err)
		}
		if reply.ID != 2 {
			continue
		}
		found = true
		if len(reply.Result.Errors)+len(reply.Result.Warnings) > 0 {
			t.Errorf("hooks/list reports diagnostics: %s", s.Bytes())
		}
		for _, d := range reply.Result.Data {
			if len(d.Errors)+len(d.Warnings) > 0 {
				t.Errorf("hooks/list reports diagnostics: %s", s.Bytes())
			}
			for _, h := range d.Hooks {
				if h.PluginID != "eva-md@eva-md" {
					continue
				}
				if _, seen := trust[h.EventName]; seen || !slices.Contains(codexWireEvents, h.EventName) {
					t.Errorf("hooks/list lists eva-md's hook for event %q, which the doctor rejects as unexpected or duplicate", h.EventName)
				}
				trust[h.EventName] = h.TrustStatus
			}
		}
	}
	if !found {
		t.Fatalf("hooks/list gave no reply: %q", out)
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
	result, err := runInstall(context.Background(), opts, asNative(f))
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
	if _, err := runInstall(context.Background(), opts, asNative(f)); err != nil {
		t.Fatal(err)
	}
	if err := f.Approve("codex"); err != nil {
		t.Fatal(err)
	}
	before := len(fakeCalls(t, f))
	opts.Binary = scriptBinary(t, "2")
	result, err := runInstall(context.Background(), opts, asNative(f))
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
		if s, err := inspect(ctx, dirs, host, asNative(f)); err != nil || !s.Valid {
			t.Fatalf("2026.11.5 on the fake %s: %+v, %v", host, s, err)
		}
	}

	opts := Options{Dirs: dirs, Binary: scriptBinary(t, "current"), Host: "all"}
	result, err := upgrade(t, opts, asNative(f))
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
	if s, err := inspect(ctx, dirs, "claude", asNative(f)); err != nil || !s.Valid {
		t.Errorf("Claude Code after the upgrade: %+v, %v", s, err)
	}
}

// Uninstall removes both registrations and Codex's copy: the fake lists no
// eva-md hook, fires none, and NeedsTrust is false.
func TestUninstallOnTheFakeHosts(t *testing.T) {
	opts, f := onFakeHosts(t)
	if _, err := runInstall(context.Background(), opts, asNative(f)); err != nil {
		t.Fatal(err)
	}
	if err := f.Approve("codex"); err != nil {
		t.Fatal(err)
	}
	result, err := uninstall(context.Background(), opts.Dirs, true, asNative(f))
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
		if s, err := inspect(context.Background(), opts.Dirs, host, asNative(f)); err != nil || s.Installed || s.Marketplace {
			t.Errorf("%s after uninstall: %+v, %v", host, s, err)
		}
	}
	if _, err := os.Stat(f.CachePath("eva-md", "eva-md")); !errors.Is(err, os.ErrNotExist) {
		t.Errorf("Codex's copy is still there: %v", err)
	}
}

// With eva-md's Codex marketplace folder deleted, the fake fails as Codex
// 0.166.0 does, and install reads it as v1 does, through its runner and
// through the written CLIs alike: Inspect says the folder is missing,
// install stops there, and uninstall clears the registration and the copy.
func TestMissingCodexMarketplaceFolderOnTheFakeHosts(t *testing.T) {
	ctx := context.Background()
	opts, f := onFakeHosts(t)
	if _, err := runInstall(ctx, opts, asNative(f)); err != nil {
		t.Fatal(err)
	}
	if err := f.Approve("codex"); err != nil {
		t.Fatal(err)
	}
	if err := os.RemoveAll(marketplacePath(opts.Dirs.Share, "codex")); err != nil {
		t.Fatal(err)
	}
	wantMissing := func(how string, s Status, err error) {
		t.Helper()
		if err != nil || !s.Marketplace || !s.FolderMissing || s.Installed || s.Reason != "eva-md's marketplace folder is missing" {
			t.Errorf("Codex with its marketplace folder deleted, %s: %+v, %v", how, s, err)
		}
	}
	s, err := inspect(ctx, opts.Dirs, "codex", asNative(f))
	wantMissing("through install's runner", s, err)

	bin := filepath.Join(t.TempDir(), "bin")
	if err := f.WriteCLIs(bin); err != nil {
		t.Fatal(err)
	}
	path := os.Getenv("PATH")
	t.Setenv("PATH", bin)
	s, err = Inspect(ctx, opts.Dirs, "codex")
	wantMissing("through the written CLIs", s, err)
	t.Setenv("PATH", path)

	before := len(fakeCalls(t, f))
	if _, err := runInstall(ctx, opts, asNative(f)); err == nil || !strings.Contains(err.Error(), "its folder is missing") {
		t.Errorf("install with Codex's marketplace folder deleted: %v", err)
	}
	if found := registrations(fakeCalls(t, f)[before:]); len(found) != 0 {
		t.Errorf("the refused install registered %v", found)
	}

	preview, err := uninstall(ctx, opts.Dirs, false, asNative(f))
	if err != nil || !slices.Contains(preview.Actions, "codex: the eva-md plugin and its marketplace registration, whose folder is missing") {
		t.Fatalf("uninstall's preview: %+v, %v", preview.Actions, err)
	}
	result, err := uninstall(ctx, opts.Dirs, true, asNative(f))
	if err != nil {
		t.Fatal(err)
	}
	if s, err := inspect(ctx, opts.Dirs, "codex", asNative(f)); err != nil || s.Marketplace || s.Installed || s.FolderMissing {
		t.Errorf("Codex after uninstall: %+v, %v", s, err)
	}
	trust := codexTrust(t, f)
	if len(trust) != 0 || needsReview(trust) != result.NeedsTrust {
		t.Errorf("after uninstall the fake lists %v, NeedsTrust %t", trust, result.NeedsTrust)
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
	result, err := runInstall(context.Background(), opts, asNative(f))
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
