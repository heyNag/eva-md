package adapter

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

// Each host's Plugin holds what v1's install did for it, value for value,
// and every host has one.
func TestPluginHoldsV1sValues(t *testing.T) {
	want := map[ID]Plugin{
		ClaudeCode: {
			MarketplaceManifest: ".claude-plugin/marketplace.json",
			PluginManifest:      ".claude-plugin/plugin.json",
			AddArgs:             []string{"plugin", "install", "eva-md@eva-md", "--scope", "user", "--json"},
			RemoveArgs:          []string{"plugin", "uninstall", "eva-md@eva-md", "--scope", "user", "--json"},
			ValidateArgs:        []string{"plugin", "validate", "--strict", "--json"},
			SourceKeys:          []string{"readFromFolder"},
			FolderVersion:       "1",
			SourceDiffers:       "Claude source folder or folder version differs",
		},
		Codex: {
			MarketplaceManifest: ".agents/plugins/marketplace.json",
			PluginManifest:      ".codex-plugin/plugin.json",
			AddArgs:             []string{"plugin", "add", "eva-md@eva-md", "--json"},
			RemoveArgs:          []string{"plugin", "remove", "eva-md@eva-md", "--json"},
			ListPerMarketplace:  true,
			SourceKeys:          []string{"source", "path"},
			SourceDiffers:       "Codex source folder differs",
			MissingFolderFails:  true,
			FeaturesArgs:        []string{"features", "list"},
			NeedsReview:         true,
		},
	}
	if len(plugins) != len(registry) {
		t.Errorf("the table has %d plugins for %d hosts", len(plugins), len(registry))
	}
	for _, h := range All() {
		if _, ok := plugins[h.ID]; !ok {
			t.Errorf("%s has no plugin", h.ID)
		}
		if got := h.Plugin(); !reflect.DeepEqual(got, want[h.ID]) {
			t.Errorf("%s's plugin:\n got %+v\nwant %+v", h.ID, got, want[h.ID])
		}
	}
	if got := (Host{ID: "generic"}).Plugin(); !reflect.DeepEqual(got, Plugin{}) {
		t.Errorf("a host eva-md doesn't know has a plugin: %+v", got)
	}
}

// A change to what Plugin gave leaves the table alone.
func TestPluginIsACopy(t *testing.T) {
	for _, h := range All() {
		p := h.Plugin()
		for _, args := range [][]string{p.AddArgs, p.RemoveArgs, p.ValidateArgs, p.SourceKeys, p.FeaturesArgs} {
			if len(args) > 0 {
				args[0] = "changed"
			}
		}
		again := h.Plugin()
		for _, args := range [][]string{again.AddArgs, again.RemoveArgs, again.ValidateArgs, again.SourceKeys, again.FeaturesArgs} {
			if len(args) > 0 && args[0] == "changed" {
				t.Fatalf("changing %s's plugin changed the table: %+v", h.ID, again)
			}
		}
	}
}

// An entry of a host's plugin list loads the plugin from eva-md's folder
// only when the host's own keys name that folder exactly, and, on Claude
// Code, folderVersion is 1, as v1's inspect checked.
func TestLoadsFrom(t *testing.T) {
	const folder = "/Users/example/Library/Application Support/eva-md/marketplace/x/plugins/eva-md"
	claudeCode, codex := Host{ID: ClaudeCode}.Plugin(), Host{ID: Codex}.Plugin()
	for _, c := range []struct {
		name   string
		plugin Plugin
		entry  map[string]any
		want   bool
	}{
		{"claude-code: its folder and version 1", claudeCode, map[string]any{"readFromFolder": folder, "folderVersion": "1"}, true},
		{"claude-code: version 2", claudeCode, map[string]any{"readFromFolder": folder, "folderVersion": "2"}, false},
		{"claude-code: no version", claudeCode, map[string]any{"readFromFolder": folder}, false},
		{"claude-code: a numeric version", claudeCode, map[string]any{"readFromFolder": folder, "folderVersion": 1}, false},
		{"claude-code: another folder", claudeCode, map[string]any{"readFromFolder": folder + "-other", "folderVersion": "1"}, false},
		{"claude-code: the folder with a slash more", claudeCode, map[string]any{"readFromFolder": folder + "/", "folderVersion": "1"}, false},
		{"claude-code: Codex's keys", claudeCode, map[string]any{"source": map[string]any{"path": folder}, "folderVersion": "1"}, false},
		{"codex: its folder", codex, map[string]any{"source": map[string]any{"source": "local", "path": folder}}, true},
		{"codex: its folder, any version", codex, map[string]any{"source": map[string]any{"path": folder}, "folderVersion": "7"}, true},
		{"codex: another folder", codex, map[string]any{"source": map[string]any{"path": "/elsewhere"}}, false},
		{"codex: source is a string", codex, map[string]any{"source": folder}, false},
		{"codex: no source", codex, map[string]any{}, false},
		{"codex: Claude Code's keys", codex, map[string]any{"readFromFolder": folder, "folderVersion": "1"}, false},
		{"a host with no keys", Plugin{}, map[string]any{"readFromFolder": folder, "folderVersion": "1"}, false},
	} {
		if got := c.plugin.LoadsFrom(c.entry, folder); got != c.want {
			t.Errorf("%s: %v, want %v", c.name, got, c.want)
		}
	}
}

// fixture reads a quirk's fixture.
func fixture(t *testing.T, q Quirk) []byte {
	t.Helper()
	data, err := os.ReadFile(filepath.Join("testdata", "quirks", q.Fixture))
	if err != nil {
		t.Fatal(err)
	}
	return data
}

// fixtureFolder is the folder the fixtures name as eva-md's marketplace on
// each host.
func fixtureFolder(h Host) string {
	return "/Users/example/Library/Application Support/eva-md/marketplace/" + h.Names.InstallName
}

// Codex's words for eva-md's marketplace folder with no manifest are read
// from its fixture, and only when they name nothing but that folder.
func TestMarketplaceFolderMissingOnItsFixture(t *testing.T) {
	codex, _ := ByID(string(Codex))
	folder := fixtureFolder(codex)
	diagnostics := string(fixture(t, codexFolderMissing))
	if !MarketplaceFolderMissing(diagnostics, folder) {
		t.Fatalf("the fixture doesn't read as the folder missing:\n%s", diagnostics)
	}
	if !MarketplaceFolderMissing(diagnostics, "/elsewhere", folder) {
		t.Error("the folder given resolved, second, wasn't matched")
	}
	other := strings.Replace(diagnostics, "`eva-md`", "`unrelated`", 1)
	another := diagnostics + "- `unrelated` at /unrelated" + missingManifest + "\n"
	for name, c := range map[string]struct {
		diagnostics string
		folders     []string
	}{
		"another folder":                {diagnostics, []string{folder + "-other"}},
		"no folder":                     {diagnostics, nil},
		"another marketplace":           {other, []string{folder}},
		"another marketplace as well":   {another, []string{folder}},
		"no line naming a marketplace":  {"Error: something else failed\n", []string{folder}},
		"nothing":                       {"", []string{folder}},
		"another ending of the line":    {strings.Replace(diagnostics, "supported manifest", "manifest", 1), []string{folder}},
		"the folder with a slash more":  {diagnostics, []string{folder + "/"}},
		"the line's ending cut at path": {"- `eva-md` at " + folder + "\n", []string{folder}},
	} {
		if MarketplaceFolderMissing(c.diagnostics, c.folders...) {
			t.Errorf("%s: read as eva-md's folder missing", name)
		}
	}
	// The line is read wherever it starts in the line, with space around it.
	indented := "Error: failed to load marketplace(s):\n   - `eva-md` at " + folder + missingManifest + "   \n"
	if !MarketplaceFolderMissing(indented, folder) {
		t.Error("an indented line wasn't read")
	}
}

// The root of eva-md's marketplace is read from both hosts' fixtures, and
// from the first of its keys that gives one.
func TestMarketplaceRootOnItsFixtures(t *testing.T) {
	for _, q := range []Quirk{claudeCodeMarketplaceRoot, codexMarketplaceRoot} {
		h, _ := ByID(string(q.Host))
		var raw any
		if err := json.Unmarshal(fixture(t, q), &raw); err != nil {
			t.Fatal(err)
		}
		if obj, ok := raw.(map[string]any); ok {
			raw = obj["marketplaces"]
		}
		list, _ := raw.([]any)
		found := false
		for _, item := range list {
			m, _ := item.(map[string]any)
			if m["name"] != "eva-md" {
				continue
			}
			found = true
			if got := MarketplaceRoot(m); got != fixtureFolder(h) {
				t.Errorf("%s: eva-md's root %q, want %q", q.ID, got, fixtureFolder(h))
			}
		}
		if !found {
			t.Errorf("%s: no eva-md marketplace in the fixture", q.ID)
		}
	}
	for _, c := range []struct {
		entry string
		want  string
	}{
		{`{"root": "/a", "path": "/b", "installLocation": "/c"}`, "/a"},
		{`{"root": "", "path": "/b", "installLocation": "/c"}`, "/b"},
		{`{"installLocation": "/c", "marketplaceSource": {"source": "/d"}}`, "/c"},
		{`{"root": 7, "marketplaceSource": {"source": "/d"}, "source": {"path": "/e"}}`, "/d"},
		{`{"marketplaceSource": {"source": ""}, "source": {"path": "/e", "source": "/f"}}`, "/e"},
		{`{"marketplaceSource": "/d", "source": {"source": "/f"}}`, "/f"},
		{`{"source": "/f"}`, ""},
		{`{"name": "eva-md"}`, ""},
	} {
		var m map[string]any
		if err := json.Unmarshal([]byte(c.entry), &m); err != nil {
			t.Fatal(err)
		}
		if got := MarketplaceRoot(m); got != c.want {
			t.Errorf("%s: %q, want %q", c.entry, got, c.want)
		}
	}
}

// installRule is the reading of codex features list v1's install did in
// place: it refused at the first hooks row whose last column is false.
func installRule(out []byte) bool {
	for _, line := range strings.Split(string(out), "\n") {
		fields := strings.Fields(line)
		if len(fields) >= 2 && fields[0] == "hooks" && fields[len(fields)-1] == "false" {
			return true
		}
	}
	return false
}

// doctorRule is the reading the doctor does in place (internal/health/
// hosts.go): the last hooks row decides, and passes only on true.
func doctorRule(out []byte) (known, enabled bool) {
	for _, line := range strings.Split(string(out), "\n") {
		fields := strings.Fields(line)
		if len(fields) >= 2 && fields[0] == "hooks" {
			known = true
			enabled = fields[len(fields)-1] == "true"
		}
	}
	return known, enabled
}

// The hooks rows read from the fixture say the feature is on, by both
// rules.
func TestReadHooksRowsOnItsFixture(t *testing.T) {
	rows := ReadHooksRows(fixture(t, codexHooksFeature))
	if !reflect.DeepEqual(rows, HooksRows{"true"}) {
		t.Fatalf("rows %q", rows)
	}
	if known, on := rows.On(); rows.Off() || !known || !on {
		t.Errorf("off %v, known %v, on %v", rows.Off(), known, on)
	}
	off := ReadHooksRows([]byte(strings.Replace(string(fixture(t, codexHooksFeature)), "hooks experimental true", "hooks experimental false", 1)))
	if known, on := off.On(); !off.Off() || !known || on {
		t.Errorf("turned off: off %v, known %v, on %v", off.Off(), known, on)
	}
}

// Off is install's rule and On the doctor's, each as v1 reads it in place,
// on every input here; the inputs include those on which the two rules give
// different answers, which stay different.
func TestInstallAndDoctorRulesOnHooksRows(t *testing.T) {
	for _, c := range []struct {
		out       string
		rows      HooksRows
		installOK bool // install goes on
		doctorOK  bool // the doctor passes
	}{
		// The rules agree.
		{"hooks experimental true\n", HooksRows{"true"}, true, true},
		{"hooks stable false\n", HooksRows{"false"}, false, false},
		{"hooks true\nhooks false\n", HooksRows{"true", "false"}, false, false},
		// Install refuses on any false row; the doctor reads the last row.
		{"hooks experimental false\nhooks stable true\n", HooksRows{"false", "true"}, false, true},
		// Install refuses only on false; the doctor passes only on true.
		{"hooks stable TRUE\n", HooksRows{"TRUE"}, true, false},
		{"hooks stable False\n", HooksRows{"False"}, true, false},
		{"hooks stable on\n", HooksRows{"on"}, true, false},
		{"hooks true\nhooks maybe\n", HooksRows{"true", "maybe"}, true, false},
		// No hooks row: install goes on; the doctor can't tell.
		{"", nil, true, false},
		{"apps experimental false\n", nil, true, false},
		{"hooks\n", nil, true, false},
		{"hooks_v2 stable false\n", nil, true, false},
		{"  hooks \t experimental   false  \r\n", HooksRows{"false"}, false, false},
	} {
		rows := ReadHooksRows([]byte(c.out))
		if !reflect.DeepEqual(rows, c.rows) {
			t.Errorf("%q: rows %q, want %q", c.out, rows, c.rows)
		}
		if got := !rows.Off(); got != c.installOK || got == installRule([]byte(c.out)) {
			t.Errorf("%q: install goes on %v, want %v and as v1's install", c.out, got, c.installOK)
		}
		known, on := rows.On()
		wantKnown, wantOn := doctorRule([]byte(c.out))
		if on != c.doctorOK || known != wantKnown || on != wantOn {
			t.Errorf("%q: the doctor's known %v, on %v; want on %v, and as the doctor reads it %v, %v", c.out, known, on, c.doctorOK, wantKnown, wantOn)
		}
	}
}

// FindCLI asks lookPath for the host's command and gives its absolute path
// with its folder's symlinks resolved, or "" when lookPath finds none; it
// runs nothing.
func TestFindCLI(t *testing.T) {
	real, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(t.TempDir(), "linked")
	if err := os.Symlink(real, link); err != nil {
		t.Fatal(err)
	}
	for _, h := range All() {
		var asked []string
		lookPath := func(file string) (string, error) {
			asked = append(asked, file)
			return filepath.Join(link, file), nil
		}
		if got, want := h.FindCLI(lookPath), filepath.Join(real, h.Names.CLI); got != want {
			t.Errorf("%s: %q, want %q", h.ID, got, want)
		}
		if !reflect.DeepEqual(asked, []string{h.Names.CLI}) {
			t.Errorf("%s: asked for %q", h.ID, asked)
		}
		missing := func(string) (string, error) { return "", errors.New("not found") }
		if got := h.FindCLI(missing); got != "" {
			t.Errorf("%s: a CLI lookPath doesn't find: %q", h.ID, got)
		}
	}
	// With no lookPath, PATH is searched; the CLI is never run.
	bin := filepath.Join(real, "bin")
	if err := os.Mkdir(bin, 0o700); err != nil {
		t.Fatal(err)
	}
	ran := filepath.Join(real, "ran")
	if err := os.WriteFile(filepath.Join(bin, "codex"), []byte("#!/bin/sh\ntouch '"+ran+"'\n"), 0o700); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", bin)
	codex, _ := ByID(string(Codex))
	claudeCode, _ := ByID(string(ClaudeCode))
	if got := codex.FindCLI(nil); got != filepath.Join(bin, "codex") {
		t.Errorf("codex on PATH: %q", got)
	}
	if got := claudeCode.FindCLI(nil); got != "" {
		t.Errorf("claude-code, not on PATH: %q", got)
	}
	if _, err := os.Stat(ran); !errors.Is(err, os.ErrNotExist) {
		t.Errorf("FindCLI ran the CLI: %v", err)
	}
}
