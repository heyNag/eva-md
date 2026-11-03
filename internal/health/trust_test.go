package health

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/heyNag/eva-md/internal/paths"
)

func trustHooks() []any {
	var hooks []any
	for _, event := range hostEvents("codex") {
		wireEvent := strings.ToLower(event[:1]) + event[1:]
		hooks = append(hooks, map[string]any{"pluginId": "eva-md@eva-md", "enabled": true, "trustStatus": "trusted", "eventName": wireEvent})
	}
	return append(hooks, map[string]any{"pluginId": "other@other", "enabled": true, "trustStatus": "trusted", "eventName": "SessionStart"})
}
func fakeTrustHost(t *testing.T, result any) paths.Dirs {
	t.Helper()
	d := isolated(t)
	bin := filepath.Join(d.Share, "test-bin")
	if e := paths.EnsureDir(bin); e != nil {
		t.Fatal(e)
	}
	reply, e := json.Marshal(map[string]any{"id": 2, "result": result})
	if e != nil {
		t.Fatal(e)
	}
	// The fake accepts only the bounded read-only handshake. Any unexpected
	// method terminates before a response; no turn or trust write is allowed.
	script := "#!/bin/sh\n" +
		"read first\ncase \"$first\" in *'\"method\":\"initialize\"'*) ;; *) exit 5;; esac\n" +
		"printf '%s\\n' '{\"id\":1,\"result\":{}}'\n" +
		"read second\ncase \"$second\" in *'\"method\":\"initialized\"'*) ;; *) exit 6;; esac\n" +
		"read third\ncase \"$third\" in *'\"method\":\"hooks/list\"'*) ;; *) exit 7;; esac\n" +
		"printf '%s\\n' '" + string(reply) + "'\n"
	if e = os.WriteFile(filepath.Join(bin, "codex"), []byte(script), 0o700); e != nil {
		t.Fatal(e)
	}
	t.Setenv("PATH", bin)
	return d
}
func TestTrustProtocolIsReadOnly(t *testing.T) {
	d := fakeTrustHost(t, map[string]any{"data": []any{map[string]any{"hooks": trustHooks()}}})
	n, trusted, e := probeTrust(context.Background(), d.State)
	if e != nil || n != 7 || trusted != 7 {
		t.Fatalf("%d %d %v", n, trusted, e)
	}
}
func TestPartialTrustIsNotSevenEvents(t *testing.T) {
	d := fakeTrustHost(t, map[string]any{"data": []any{map[string]any{"hooks": trustHooks()[:1]}}})
	n, trusted, e := probeTrust(context.Background(), d.State)
	if e != nil || n != 1 || trusted != 1 {
		t.Fatalf("%d %d %v", n, trusted, e)
	}
	var r Report
	inspectHosts(context.Background(), &r, d, inventoryTimeout)
	for _, check := range r.Checks {
		if check.Name == "codex hook trust" && check.Status != "FAIL" {
			t.Fatalf("partial hooks reported healthy: %+v", check)
		}
	}
}

func TestStalledInventoryDoesNotHideTrust(t *testing.T) {
	d := fakeTrustHost(t, map[string]any{"data": []any{map[string]any{"hooks": trustHooks()}}})
	file := filepath.Join(d.Share, "test-bin", "codex")
	script, err := os.ReadFile(file)
	if err != nil {
		t.Fatal(err)
	}
	// A native inventory command can stall. The independent read-only hooks
	// RPC must still get its own opportunity. The inventory's first command
	// stalls, so wherever its budget ends, it ends inside a stalled command.
	prefix := "#!/bin/sh\ncase \"$*\" in\n" +
		"'plugin marketplace list --json') exec /bin/sleep 60;;\n" +
		"'features list') printf '%s\\n' 'hooks experimental true'; exit 0;;\n" +
		"esac\n"
	if err := os.WriteFile(file, []byte(prefix+strings.TrimPrefix(string(script), "#!/bin/sh\n")), 0700); err != nil {
		t.Fatal(err)
	}
	// The stalled listing outlasts the inventory's own budget, short here to
	// keep the test quick, so the inventory always runs out of it; the shared
	// deadline only guards against a hang.
	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()
	var report Report
	inspectHosts(ctx, &report, d, 100*time.Millisecond)
	if ctx.Err() != nil {
		t.Fatal("inventory exhausted the shared doctor deadline")
	}
	statuses := map[string]string{}
	for _, check := range report.Checks {
		statuses[check.Name] = check.Status
		if check.Name == "codex plugin" && check.Detail != "native plugin inventory timed out" {
			t.Errorf("stalled inventory detail=%q", check.Detail)
		}
	}
	for name, want := range map[string]string{"codex plugin": "UNKNOWN", "codex hooks feature": "PASS", "codex hook trust": "PASS"} {
		if got := statuses[name]; got != want {
			t.Errorf("%s=%s, want %s", name, got, want)
		}
	}
}
func TestDuplicateAndDiagnosticsNeverPassTrust(t *testing.T) {
	for _, scenario := range []string{"duplicate", "warnings", "errors", "unexpected"} {
		t.Run(scenario, func(t *testing.T) {
			hooks := trustHooks()
			group := map[string]any{"hooks": hooks}
			switch scenario {
			case "duplicate":
				group["hooks"] = append(hooks, hooks[0])
			case "unexpected":
				hooks[0].(map[string]any)["eventName"] = "Unknown"
			default:
				group[scenario] = []any{"host diagnostic must not be repeated"}
			}
			d := fakeTrustHost(t, map[string]any{"data": []any{group}})
			if _, _, e := probeTrust(context.Background(), d.State); e == nil {
				t.Fatal("invalid inventory accepted")
			}
		})
	}
}

// withInventory answers the native plugin commands before the hooks RPC:
// each marketplace's list with installed, and Codex's list of every plugin,
// which eva-md never asks for, with a failure.
func withInventory(t *testing.T, d paths.Dirs, installed, markets string) {
	t.Helper()
	file := filepath.Join(d.Share, "test-bin", "codex")
	script, err := os.ReadFile(file)
	if err != nil {
		t.Fatal(err)
	}
	prefix := "#!/bin/sh\ncase \"$*\" in\n" +
		"'plugin list --json') exit 9;;\n" +
		"'plugin list --json --marketplace='*) printf '%s\\n' '" + installed + "'; exit 0;;\n" +
		"'plugin marketplace list --json') printf '%s\\n' '" + markets + "'; exit 0;;\n" +
		"'features list') printf '%s\\n' 'hooks experimental true'; exit 0;;\n" +
		"esac\n"
	if err := os.WriteFile(file, []byte(prefix+strings.TrimPrefix(string(script), "#!/bin/sh\n")), 0700); err != nil {
		t.Fatal(err)
	}
}
func hostStatuses(t *testing.T, d paths.Dirs) map[string]Check {
	t.Helper()
	var r Report
	inspectHosts(context.Background(), &r, d, inventoryTimeout)
	checks := map[string]Check{}
	for _, c := range r.Checks {
		checks[c.Name] = c
	}
	return checks
}
func TestEnabledPluginWithNoLoadedHooksFails(t *testing.T) {
	d := fakeTrustHost(t, map[string]any{"data": []any{map[string]any{"hooks": []any{}}}})
	plugin := filepath.Join(d.Share, "marketplace", "codex", "plugins", "eva-md")
	market := filepath.Join(d.Share, "marketplace", "codex")
	withInventory(t, d,
		`{"installed":[{"pluginId":"eva-md@eva-md","enabled":true,"source":{"path":"`+plugin+`"}}]}`,
		`{"marketplaces":[{"name":"eva-md","path":"`+market+`"}]}`)
	checks := hostStatuses(t, d)
	if checks["codex plugin"].Status != "PASS" {
		t.Fatalf("plugin: %+v", checks["codex plugin"])
	}
	if c := checks["codex hook trust"]; c.Status != "FAIL" || c.Fix == "" {
		t.Fatalf("enabled plugin without hooks: %+v", c)
	}
}
func TestNoLoadedHooksWithoutPluginStaysUnknown(t *testing.T) {
	d := fakeTrustHost(t, map[string]any{"data": []any{map[string]any{"hooks": []any{}}}})
	withInventory(t, d, `{"installed":[]}`, `{"marketplaces":[]}`)
	if c := hostStatuses(t, d)["codex hook trust"]; c.Status != "UNKNOWN" {
		t.Fatalf("missing plugin judged as trust failure: %+v", c)
	}
}
func TestConflictingRegistrationFails(t *testing.T) {
	d := fakeTrustHost(t, map[string]any{"data": []any{map[string]any{"hooks": trustHooks()}}})
	withInventory(t, d,
		`{"installed":[{"pluginId":"eva-md@elsewhere","enabled":true}]}`,
		`{"marketplaces":[{"name":"elsewhere","path":"/elsewhere"}]}`)
	if c := hostStatuses(t, d)["codex plugin"]; c.Status != "FAIL" || !strings.HasPrefix(c.Detail, "another eva-md plugin copy") || !strings.HasPrefix(c.Fix, "remove the conflicting") {
		t.Fatalf("conflicting registration: %+v", c)
	}
}
func TestBrokenInventoryStaysUnknown(t *testing.T) {
	d := fakeTrustHost(t, map[string]any{"data": []any{map[string]any{"hooks": trustHooks()}}})
	withInventory(t, d, `not json`, `{"marketplaces":[{"name":"eva-md","path":"/eva-md"}]}`)
	if c := hostStatuses(t, d)["codex plugin"]; c.Status != "UNKNOWN" {
		t.Fatalf("unreadable inventory: %+v", c)
	}
}

// A marketplace's list that fails is an inventory that can't be read, never
// a conflicting registration, though the command it names holds eva-md.
func TestFailedMarketplaceListStaysUnknown(t *testing.T) {
	d := fakeTrustHost(t, map[string]any{"data": []any{map[string]any{"hooks": trustHooks()}}})
	file := filepath.Join(d.Share, "test-bin", "codex")
	script, err := os.ReadFile(file)
	if err != nil {
		t.Fatal(err)
	}
	prefix := "#!/bin/sh\ncase \"$*\" in\n" +
		"'plugin marketplace list --json') printf '%s\\n' '{\"marketplaces\":[{\"name\":\"eva-md\",\"path\":\"/eva-md\"}]}'; exit 0;;\n" +
		"'plugin list --json --marketplace=eva-md') exit 2;;\n" +
		"'features list') printf '%s\\n' 'hooks experimental true'; exit 0;;\n" +
		"esac\n"
	if err := os.WriteFile(file, []byte(prefix+strings.TrimPrefix(string(script), "#!/bin/sh\n")), 0700); err != nil {
		t.Fatal(err)
	}
	if c := hostStatuses(t, d)["codex plugin"]; c.Status != "UNKNOWN" || c.Detail != "native plugin inventory is unavailable" {
		t.Fatalf("failed marketplace list: %+v", c)
	}
}

// Codex can't list while the eva-md marketplace it has registered at
// eva-md's folder has no manifest, as after that folder was deleted: the
// doctor names that, with uninstall and then install as the fix, from what
// Codex 0.166.0 prints.
func TestMissingMarketplaceFolderFails(t *testing.T) {
	d := fakeTrustHost(t, map[string]any{"data": []any{map[string]any{"hooks": trustHooks()}}})
	file := filepath.Join(d.Share, "test-bin", "codex")
	script, err := os.ReadFile(file)
	if err != nil {
		t.Fatal(err)
	}
	market := filepath.Join(d.Share, "marketplace", "codex")
	prefix := "#!/bin/sh\ncase \"$*\" in\n" +
		"'plugin marketplace list --json') printf '%s\\n' 'Error: failed to load marketplace(s):' '- `eva-md` at " + market + ": marketplace root does not contain a supported manifest' >&2; exit 1;;\n" +
		"'features list') printf '%s\\n' 'hooks experimental true'; exit 0;;\n" +
		"esac\n"
	if err := os.WriteFile(file, []byte(prefix+strings.TrimPrefix(string(script), "#!/bin/sh\n")), 0700); err != nil {
		t.Fatal(err)
	}
	if c := hostStatuses(t, d)["codex plugin"]; c.Status != "FAIL" || c.Detail != "eva-md's marketplace folder is missing" || c.Fix != "run eva-md uninstall --apply, then install eva-md again" {
		t.Fatalf("missing marketplace folder: %+v", c)
	}
}

// A Mac without Codex gets no Codex trust row and no fix to install eva-md
// for it, and keeps the Claude rows.
func TestHostRowsOnAMacWithoutCodex(t *testing.T) {
	d := isolated(t)
	bin := filepath.Join(d.Share, "test-bin")
	if e := paths.EnsureDir(bin); e != nil {
		t.Fatal(e)
	}
	if e := os.WriteFile(filepath.Join(bin, "claude"), []byte("#!/bin/sh\nexit 3\n"), 0o700); e != nil {
		t.Fatal(e)
	}
	t.Setenv("PATH", bin)
	checks := hostStatuses(t, d)
	if c := checks["codex plugin"]; c.Status != "UNKNOWN" || c.Detail != "Codex isn't installed on this Mac" || c.Fix != "" {
		t.Fatalf("codex plugin: %+v", c)
	}
	if c, ok := checks["codex hook trust"]; ok {
		t.Fatalf("a trust row for a Codex that isn't there: %+v", c)
	}
	if _, ok := checks["claude managed hook policy"]; !ok {
		t.Fatal("the Claude policy row is missing")
	}
	if c := checks["claude plugin"]; strings.Contains(c.Detail, "isn't installed") {
		t.Fatalf("claude plugin: %+v", c)
	}
}
