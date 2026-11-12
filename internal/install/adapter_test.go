package install

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/heyNag/eva-md/internal/host/adapter"
)

// renderedEvents gives the events of the hooks file render writes for a
// host, sorted.
func renderedEvents(t *testing.T, host string) []string {
	t.Helper()
	files, err := render(host, "/Users/example/.local/share/eva-md/bin/eva-md")
	if err != nil {
		t.Fatal(err)
	}
	var hooks struct {
		Hooks map[string]json.RawMessage `json:"hooks"`
	}
	if err = json.Unmarshal(files["plugins/eva-md/hooks/hooks.json"], &hooks); err != nil {
		t.Fatal(err)
	}
	var events []string
	for event := range hooks.Hooks {
		events = append(events, event)
	}
	slices.Sort(events)
	return events
}

// eventsDiffer says how the events render writes differ from a list of the
// adapter's, or "" when they are the same events.
func eventsDiffer(rendered, declared []string) string {
	declared = slices.Sorted(slices.Values(declared))
	if slices.Equal(rendered, declared) {
		return ""
	}
	return fmt.Sprintf("render writes %v, the adapter lists %v", rendered, declared)
}

// The events render writes for each host are the events the adapter's
// newest row of that host lists; with one event taken out of a host's list,
// they differ.
func TestRenderWritesTheAdaptersEvents(t *testing.T) {
	for _, h := range adapter.All() {
		rows := adapter.Rows(h.ID)
		if len(rows) == 0 {
			t.Fatalf("%s has no rows", h.ID)
		}
		declared := rows[len(rows)-1].Events
		rendered := renderedEvents(t, h.Names.InstallName)
		if diff := eventsDiffer(rendered, declared); diff != "" {
			t.Errorf("%s: %s", h.ID, diff)
		}
		for i := range declared {
			fewer := slices.Delete(slices.Clone(declared), i, i+1)
			if eventsDiffer(rendered, fewer) == "" {
				t.Errorf("%s: without %s the adapter's list still matched", h.ID, declared[i])
			}
		}
	}
}

// render and MCPManifest take the hooks' and the server's --host, and the
// skill's host, from the adapter: each host's id.
func TestRenderTakesTheHostIDFromTheAdapter(t *testing.T) {
	const binary = "/Users/example/.local/share/eva-md/bin/eva-md"
	for _, h := range adapter.All() {
		files, err := render(h.Names.InstallName, binary)
		if err != nil {
			t.Fatal(err)
		}
		var hooks struct {
			Hooks map[string][]struct {
				Hooks []struct {
					Command string   `json:"command"`
					Args    []string `json:"args"`
				} `json:"hooks"`
			} `json:"hooks"`
		}
		if err = json.Unmarshal(files["plugins/eva-md/hooks/hooks.json"], &hooks); err != nil {
			t.Fatal(err)
		}
		for event, entries := range hooks.Hooks {
			for _, entry := range entries {
				for _, hook := range entry.Hooks {
					line := hook.Command + " " + strings.Join(hook.Args, " ")
					if !strings.Contains(line+" ", " hook --host "+string(h.ID)+" ") {
						t.Errorf("%s %s: the hook doesn't run eva-md hook --host %s: %q", h.ID, event, h.ID, line)
					}
				}
			}
		}
		var manifest struct {
			MCPServers map[string]struct {
				Args []string `json:"args"`
			} `json:"mcpServers"`
		}
		if err = json.Unmarshal(files[mcpPath], &manifest); err != nil {
			t.Fatal(err)
		}
		if got := manifest.MCPServers["eva-md"].Args; !slices.Equal(got, []string{"mcp", "--host", string(h.ID)}) {
			t.Errorf("%s: the MCP server's args %q", h.ID, got)
		}
		if skill := string(files[skillPath]); !strings.Contains(skill, "generated: { by: "+string(h.ID)+"/") {
			t.Errorf("%s: the digest skill doesn't name the host", h.ID)
		}
		for _, manifest := range []string{h.Plugin().MarketplaceManifest, "plugins/eva-md/" + h.Plugin().PluginManifest} {
			if _, ok := files[manifest]; !ok {
				t.Errorf("%s: no %s", h.ID, manifest)
			}
		}
	}
	// A name install doesn't take, such as a host's id, renders nothing.
	for _, name := range []string{"claude-code", "all", "", "Codex"} {
		if files, err := render(name, binary); err == nil {
			t.Errorf("render(%q) gave %d files", name, len(files))
		}
	}
}

// Install keeps v1's rule on the hooks rows of Codex's features list: it
// refuses on any row whose last column is false, and goes on on any other,
// including the inputs on which the doctor's rule, which passes only on a
// last row of true, answers otherwise.
func TestInstallKeepsItsHooksRule(t *testing.T) {
	for list, off := range map[string]bool{
		"hooks experimental false\nhooks stable true": true,
		"hooks stable TRUE":                           false,
		"hooks stable on":                             false,
		"apps experimental false":                     false,
		"hooks true\nhooks false":                     true,
		"  hooks  stable  false  ":                    true,
		"hooks_v2 experimental false":                 false,
	} {
		opts, f := setup(t)
		run := func(ctx context.Context, host string, args ...string) ([]byte, error) {
			if host == "codex" && strings.Join(args, " ") == "features list" {
				return []byte(list + "\n"), nil
			}
			return f.run(ctx, host, args...)
		}
		opts.Preflight = true
		if _, err := runInstall(context.Background(), opts, run); errors.Is(err, ErrHooksOff) != off || !off && err != nil {
			t.Errorf("%q: %v", list, err)
		}
	}
}

// The flag of a plugin that needs a review in its host keeps v1's bytes,
// and an install for Claude Code alone leaves none.
func TestNeedsTrustFlagBytes(t *testing.T) {
	opts, f := setup(t)
	flag := filepath.Join(opts.Dirs.State, "needs-trust.json")
	only := opts
	only.Host = "claude"
	if _, err := runInstall(context.Background(), only, f.run); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(flag); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("an install for Claude Code alone left the flag: %v", err)
	}
	if _, err := runInstall(context.Background(), opts, f.run); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(flag)
	if err != nil {
		t.Fatal(err)
	}
	if string(data) != "{\"host\":\"codex\",\"needs_trust\":true}\n" {
		t.Errorf("the flag holds %q", data)
	}
}

// Install runs each host by its command, and a host's list, validate, add
// and features commands are the adapter's.
func TestInstallRunsTheAdaptersCommands(t *testing.T) {
	opts, f := setup(t)
	if _, err := runInstall(context.Background(), opts, f.run); err != nil {
		t.Fatal(err)
	}
	for _, h := range adapter.All() {
		p := h.Plugin()
		want := []string{h.Names.CLI + " " + strings.Join(p.AddArgs, " ")}
		if p.ValidateArgs != nil {
			want = append(want, h.Names.CLI+" "+strings.Join(p.ValidateArgs, " ")+" "+pluginPath(opts.Dirs.Share, h.Names.InstallName))
		}
		if p.FeaturesArgs != nil {
			want = append(want, h.Names.CLI+" "+strings.Join(p.FeaturesArgs, " "))
		}
		if p.ListPerMarketplace {
			want = append(want, h.Names.CLI+" plugin list --json --marketplace=eva-md")
		} else {
			want = append(want, h.Names.CLI+" plugin list --json")
		}
		for _, call := range want {
			if !slices.Contains(f.calls, call) {
				t.Errorf("%s: no call %q in %q", h.ID, call, f.calls)
			}
		}
	}
}
