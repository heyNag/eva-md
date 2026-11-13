package hostfake

import (
	"bytes"
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// Each policy flag gives its stated output, and no hook it blocks fires,
// whatever the fake is asked afterwards. A managed hook runs without review
// unless the policy forces every hook off.
func TestPolicyFlags(t *testing.T) {
	type check func(t *testing.T, h *host)
	failsWith := func(want string, args ...string) check {
		return func(t *testing.T, h *host) {
			t.Helper()
			_, err := h.f.Run(context.Background(), h.cli, args...)
			if e, ok := err.(*ExitError); !ok || e.Code != 1 || !strings.Contains(e.Stderr, want) {
				t.Errorf("%s %s gave %v, want a failure with %q", h.cli, strings.Join(args, " "), err, want)
			}
		}
	}
	prints := func(want string, args ...string) check {
		return func(t *testing.T, h *host) {
			t.Helper()
			if out := h.run(t, args...); !strings.Contains(string(out), want) {
				t.Errorf("%s %s printed %q, want %q", h.cli, strings.Join(args, " "), out, want)
			}
		}
	}
	lacks := func(unwanted string, args ...string) check {
		return func(t *testing.T, h *host) {
			t.Helper()
			if out := h.run(t, args...); strings.Contains(string(out), unwanted) {
				t.Errorf("%s %s printed %q, which holds %q", h.cli, strings.Join(args, " "), out, unwanted)
			}
		}
	}
	listed := func(want ...string) check {
		return func(t *testing.T, h *host) {
			t.Helper()
			got := hooksList(t, h.f)
			var keys []string
			for key := range got {
				keys = append(keys, key)
			}
			if len(got) != len(want) {
				t.Errorf("hooks/list lists %v, want %v", keys, want)
			}
			for _, key := range want {
				if got[key] == "" {
					t.Errorf("hooks/list lacks %s: %v", key, keys)
				}
			}
		}
	}
	// marketAddFailsWith checks that adding the host's own marketplace, which
	// the policy blocks, fails.
	marketAddFailsWith := func(want string) check {
		return func(t *testing.T, h *host) {
			t.Helper()
			failsWith(want, "plugin", "marketplace", "add", h.market, "--json")(t, h)
		}
	}
	managedKey := "managed:SessionStart:0"
	cases := []struct {
		name    string
		cli     string
		policy  Policy
		managed bool // the managed hook still runs
		output  []check
	}{
		{"codex hooks off", Codex, Policy{CodexHooksOff: true}, false, []check{
			prints("\nhooks stable false\n", "features list"),
			prints("\nplugins stable true\n", "features list"),
			listed(),
		}},
		{"codex plugins off", Codex, Policy{CodexPluginsOff: true}, true, []check{
			prints("\nplugins stable false\n", "features list"),
			prints("\nhooks stable true\n", "features list"),
			failsWith("plugins are turned off by your administrator", "plugin", "add", "eva-md@eva-md", "--json"),
			marketAddFailsWith("plugins are turned off by your administrator"),
			prints(`{"available":[],"installed":[]}`, "plugin", "list", "--json", "--marketplace=eva-md"),
			listed(managedKey),
		}},
		{"codex managed hooks only", Codex, Policy{CodexManagedHooksOnly: true}, true, []check{
			prints(`"pluginId":"eva-md@eva-md"`, "plugin", "list", "--json", "--marketplace=eva-md"),
			listed(managedKey),
		}},
		{"claude managed hooks only", Claude, Policy{ClaudeManagedHooksOnly: true}, true, []check{
			prints(`"hooksBlockedBy":"allowManagedHooksOnly"`, "plugin", "list", "--json"),
		}},
		{"codex blocked marketplace", Codex, Policy{BlockedMarketplace: "eva-md"}, true, []check{
			failsWith("marketplace eva-md is blocked by your administrator", "plugin", "add", "eva-md@eva-md", "--json"),
			marketAddFailsWith("marketplace eva-md is blocked by your administrator"),
			prints(`"enabled":false`, "plugin", "list", "--json", "--marketplace=eva-md"),
			listed(managedKey),
		}},
		{"claude blocked marketplace", Claude, Policy{BlockedMarketplace: "eva-md"}, true, []check{
			failsWith("marketplace eva-md is blocked by your administrator", "plugin", "install", "eva-md@eva-md", "--scope", "user", "--json"),
			marketAddFailsWith("marketplace eva-md is blocked by your administrator"),
			prints(`"enabled":false`, "plugin", "list", "--json"),
			lacks(`"enabled":true`, "plugin", "list", "--json"),
		}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			h := newHost(t, c.cli)
			h.approve(t)
			managed := filepath.Join(filepath.Dir(h.hookBin), "managed")
			write(t, managed, "#!/bin/sh\necho managed\n")
			if err := os.Chmod(managed, 0o700); err != nil {
				t.Fatal(err)
			}
			if err := h.f.DeclareManaged(c.cli, Hook{Event: "SessionStart", Command: quote(managed), Timeout: 5}); err != nil {
				t.Fatal(err)
			}
			if err := h.f.SetPolicy(c.cli, c.policy); err != nil {
				t.Fatal(err)
			}
			// Nothing the fake answers lifts the policy: every command that
			// changes something, run again, and a second approval.
			for _, args := range [][]string{
				{"plugin", "marketplace", "add", h.market, "--json"},
				{"plugin", "add", "eva-md@eva-md", "--json"},
				{"plugin", "install", "eva-md@eva-md", "--scope", "user", "--json"},
				{"--dangerously-bypass-hook-trust", "features", "list"},
			} {
				_, _ = h.f.Run(context.Background(), c.cli, args...)
			}
			_ = h.f.Approve(c.cli)
			for _, check := range c.output {
				check(t, h)
			}
			for _, event := range []string{"SessionStart", "Stop"} {
				for key, r := range h.fire(t, event) {
					if r.Managed && c.managed {
						wantRan(t, r, "managed")
						continue
					}
					if r.Status != Skipped || !strings.Contains(r.Reason, "policy") {
						t.Errorf("%s: %s (%s) under the policy", key, r.Status, r.Reason)
					}
				}
			}
			if got := h.fire(t, "SessionStart"); len(got) != 2 {
				t.Errorf("SessionStart has %d hooks, want the plugin's and the managed one", len(got))
			}
		})
	}
	// Without a policy the same commands answer otherwise, so each check
	// above sees the policy.
	for _, cli := range []string{Codex, Claude} {
		h := newHost(t, cli)
		out := h.run(t, "plugin", "list", "--json")
		if bytes.Contains(out, []byte("hooksBlockedBy")) || bytes.Contains(out, []byte(`"enabled":false`)) {
			t.Errorf("%s's plugin list without a policy: %s", cli, out)
		}
		if cli == Codex {
			if out := h.run(t, "features", "list"); string(out) != "apps experimental false\nhooks stable true\nplugins stable true\n" {
				t.Errorf("features list without a policy: %q", out)
			}
		}
	}
}

// A flag of one host's is refused on the other's.
func TestSetPolicyRefusesTheOtherHostsFlags(t *testing.T) {
	f := New(realTemp(t))
	for cli, p := range map[string]Policy{
		Claude: {CodexHooksOff: true},
		Codex:  {ClaudeManagedHooksOnly: true},
	} {
		if err := f.SetPolicy(cli, p); err == nil {
			t.Errorf("SetPolicy(%s, %+v) was taken", cli, p)
		}
	}
	if err := f.SetPolicy("made-up", Policy{}); err == nil {
		t.Error("SetPolicy took a CLI the fake doesn't answer for")
	}
}

// The plugin list without a marketplace option, Codex's, is the same
// object; checked here so the policy checks' strings stay in one shape.
func TestCodexPluginListShape(t *testing.T) {
	h := newHost(t, Codex)
	var v struct {
		Installed []map[string]any
		Available []any
	}
	if err := json.Unmarshal(h.run(t, "plugin", "list", "--json"), &v); err != nil || len(v.Installed) != 1 {
		t.Fatalf("plugin list: %+v %v", v, err)
	}
	source, _ := v.Installed[0]["source"].(map[string]any)
	if source["path"] != h.plugin || v.Installed[0]["enabled"] != true {
		t.Errorf("the entry %v names %v, want %s", v.Installed[0], source["path"], h.plugin)
	}
}
