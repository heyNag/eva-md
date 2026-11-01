package install

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"regexp"
	"slices"
	"testing"
)

// Both plugins declare eva-md's MCP server in .mcp.json: the installed
// binary by its absolute path, with the host's name and nothing that
// changes across binary-only releases. The Codex manifest names the file,
// the Claude manifest doesn't, and hooks.json stays as it was.
func TestMCPManifest(t *testing.T) {
	binary := "/Users/test/it's a space/.local/share/eva-md/bin/eva-md"
	for _, host := range []string{"claude", "codex"} {
		files, err := render(host, binary)
		if err != nil {
			t.Fatal(err)
		}
		golden, err := os.ReadFile(filepath.Join("testdata", host+"-mcp.golden.json"))
		if err != nil {
			t.Fatal(err)
		}
		manifest := files["plugins/eva-md/.mcp.json"]
		if !bytes.Equal(manifest, golden) || !bytes.Equal(MCPManifest(host, binary), golden) {
			t.Fatalf("%s .mcp.json:\n%s", host, manifest)
		}
		var m struct {
			MCPServers map[string]struct {
				Command string
				Args    []string
			} `json:"mcpServers"`
		}
		if err := json.Unmarshal(manifest, &m); err != nil || len(m.MCPServers) != 1 {
			t.Fatalf("%s: %v %+v", host, err, m)
		}
		want := map[string]string{"claude": "claude-code", "codex": "codex"}[host]
		if s := m.MCPServers["eva-md"]; s.Command != binary || !reflect.DeepEqual(s.Args, []string{"mcp", "--host", want}) {
			t.Fatalf("%s server %+v", host, s)
		}
		// No version, date or build id: Codex trusts the folder's bytes.
		if regexp.MustCompile(`\d{2,}`).Match(manifest) {
			t.Fatalf("%s .mcp.json holds a number that reads as a version or date", host)
		}
		hooks, err := os.ReadFile(filepath.Join("testdata", host+"-hooks.golden.json"))
		if err != nil || !bytes.Equal(files["plugins/eva-md/hooks/hooks.json"], hooks) {
			t.Fatalf("%s hooks.json changed: %v", host, err)
		}
		again, _ := render(host, binary)
		if !reflect.DeepEqual(files, again) {
			t.Fatalf("%s render is not deterministic", host)
		}
	}
	var codex struct {
		MCPServers string `json:"mcpServers"`
	}
	files, _ := render("codex", binary)
	if err := json.Unmarshal(files["plugins/eva-md/.codex-plugin/plugin.json"], &codex); err != nil || codex.MCPServers != "./.mcp.json" {
		t.Fatalf("Codex manifest mcpServers %q %v", codex.MCPServers, err)
	}
	// Claude Code reads the plugin's .mcp.json on its own; naming servers
	// in its manifest as well would declare the server twice.
	files, _ = render("claude", binary)
	if bytes.Contains(files["plugins/eva-md/.claude-plugin/plugin.json"], []byte("mcpServers")) {
		t.Fatal("the Claude manifest names MCP servers")
	}
}

// A machine installed before the MCP server: the dry run plans to render
// both marketplaces again and writes nothing, the install adds .mcp.json,
// validates the Claude folder again and asks for the Codex hooks to be
// approved again, a second install changes nothing, and uninstall removes
// the folders.
func TestMCPInstallAndUninstall(t *testing.T) {
	opts, f := setup(t)
	ctx := context.Background()
	result, err := runInstall(ctx, opts, f.run)
	if err != nil {
		t.Fatal(err)
	}
	manifests := map[string][]byte{}
	for _, host := range []string{"claude", "codex"} {
		file := filepath.Join(pluginPath(opts.Dirs.Share, host), ".mcp.json")
		if got, err := os.ReadFile(file); err != nil || !bytes.Equal(got, MCPManifest(host, result.Binary)) {
			t.Fatalf("%s .mcp.json not installed: %v", host, err)
		}
		manifests[host] = MCPManifest(host, result.Binary)
		if err := os.Remove(file); err != nil {
			t.Fatal(err)
		}
	}
	f.calls = nil
	dry := opts
	dry.DryRun = true
	planned, err := runInstall(ctx, dry, f.run)
	if err != nil {
		t.Fatal(err)
	}
	for _, host := range []string{"claude", "codex"} {
		if !slices.Contains(planned.Actions, host+": render generated marketplace") {
			t.Fatalf("dry run actions %v", planned.Actions)
		}
		if _, err := os.Stat(filepath.Join(pluginPath(opts.Dirs.Share, host), ".mcp.json")); !errors.Is(err, os.ErrNotExist) {
			t.Fatalf("the dry run wrote the %s .mcp.json", host)
		}
	}
	if len(mutateCalls(f.calls)) != 0 {
		t.Fatalf("dry run mutated hosts: %v", f.calls)
	}
	f.calls = nil
	again, err := runInstall(ctx, opts, f.run)
	if err != nil {
		t.Fatal(err)
	}
	if !again.NeedsTrust {
		t.Fatal("a changed Codex plugin folder needs the hooks approved again")
	}
	validated := 0
	for _, call := range f.calls {
		if call == "claude plugin validate --strict --json "+pluginPath(opts.Dirs.Share, "claude") {
			validated++
		}
	}
	if validated != 1 {
		t.Fatalf("the Claude plugin folder was validated %d times: %v", validated, f.calls)
	}
	for host, want := range manifests {
		if got, err := os.ReadFile(filepath.Join(pluginPath(opts.Dirs.Share, host), ".mcp.json")); err != nil || !bytes.Equal(got, want) {
			t.Fatalf("%s .mcp.json not restored: %v", host, err)
		}
	}
	f.calls = nil
	third, err := runInstall(ctx, opts, f.run)
	if err != nil {
		t.Fatal(err)
	}
	if third.NeedsTrust || len(mutateCalls(f.calls)) != 0 || !slices.Contains(third.Actions, "codex: marketplace bytes unchanged") {
		t.Fatalf("a second install changed something: %+v %v", third, f.calls)
	}
	if _, err := uninstall(ctx, opts.Dirs, true, f.run); err != nil {
		t.Fatal(err)
	}
	for host := range manifests {
		if _, err := os.Stat(marketplacePath(opts.Dirs.Share, host)); !errors.Is(err, os.ErrNotExist) {
			t.Fatalf("uninstall kept the %s marketplace", host)
		}
	}
}
