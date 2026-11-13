package hostfake

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestMain(m *testing.M) {
	Main()
	os.Exit(m.Run())
}

// host is a fake with one marketplace registered on one CLI, from which its
// plugin, eva-md, is installed. The plugin's hooks run hookBin, outside the
// plugin, which prints its version, its argument and its stdin.
type host struct {
	f       *Fake
	cli     string
	market  string // the marketplace's folder
	plugin  string // the plugin's folder in it
	hookBin string
}

// realTemp is a new temporary folder by its real path, as macOS reaches
// its temporary folders through a link.
func realTemp(t *testing.T) string {
	t.Helper()
	dir, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	return dir
}

// hooksJSON is a hooks file with a SessionStart hook and a Stop hook that
// run bin, in Codex's shell form or Claude Code's exec form, with
// SessionStart's timeout given.
func hooksJSON(cli, bin string, timeout int) string {
	handler := func(arg string, timeout int) map[string]any {
		h := map[string]any{"type": "command", "command": quote(bin) + " " + arg, "timeout": timeout}
		if cli == Claude {
			h["command"], h["args"] = bin, []string{arg}
		}
		return h
	}
	file := map[string]any{"hooks": map[string]any{
		"SessionStart": []any{map[string]any{"matcher": "startup", "hooks": []any{handler("start", timeout)}}},
		"Stop":         []any{map[string]any{"hooks": []any{handler("stop", 5)}}},
	}}
	data, _ := json.MarshalIndent(file, "", "  ")
	return string(data) + "\n"
}

// newHost makes a fake whose root and marketplace are apart, and installs
// the marketplace's plugin on cli. The Codex hooks are not yet approved.
func newHost(t *testing.T, cli string) *host {
	t.Helper()
	dir := realTemp(t)
	h := &host{f: New(filepath.Join(dir, "fake")), cli: cli, market: filepath.Join(dir, "market"), hookBin: filepath.Join(dir, "bin", "hook")}
	h.plugin = filepath.Join(h.market, "plugins", "eva-md")
	h.writeHookBin(t, "v1")
	manifest, marketManifest := ".claude-plugin/plugin.json", ".claude-plugin/marketplace.json"
	market := `{"name": "eva-md", "plugins": [{"name": "eva-md", "source": "./plugins/eva-md"}]}`
	if cli == Codex {
		manifest, marketManifest = ".codex-plugin/plugin.json", ".agents/plugins/marketplace.json"
		market = `{"name": "eva-md", "plugins": [{"name": "eva-md", "source": {"source": "local", "path": "./plugins/eva-md"}}]}`
	}
	write(t, filepath.Join(h.market, marketManifest), market+"\n")
	write(t, filepath.Join(h.plugin, manifest), `{"name": "eva-md", "version": "1"}`+"\n")
	write(t, filepath.Join(h.plugin, "skills", "recall", "SKILL.md"), "made-up skill\n")
	write(t, filepath.Join(h.plugin, "hooks", "hooks.json"), hooksJSON(cli, h.hookBin, 10))
	h.run(t, "plugin", "marketplace", "add", h.market, "--json")
	h.install(t)
	return h
}

func (h *host) install(t *testing.T) {
	t.Helper()
	if h.cli == Codex {
		h.run(t, "plugin", "add", "eva-md@eva-md", "--json")
	} else {
		h.run(t, "plugin", "install", "eva-md@eva-md", "--scope", "user", "--json")
	}
}

func (h *host) writeHookBin(t *testing.T, version string) {
	t.Helper()
	write(t, h.hookBin, "#!/bin/sh\necho \""+version+" $1\"\ncat\n")
	if err := os.Chmod(h.hookBin, 0o700); err != nil {
		t.Fatal(err)
	}
}

func write(t *testing.T, path, data string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(data), 0o600); err != nil {
		t.Fatal(err)
	}
}

// run runs a command that must succeed.
func (h *host) run(t *testing.T, args ...string) []byte {
	t.Helper()
	out, err := h.f.Run(context.Background(), h.cli, args...)
	if err != nil {
		t.Fatalf("%s %s: %v %+v", h.cli, strings.Join(args, " "), err, err)
	}
	return out
}

func (h *host) approve(t *testing.T) {
	t.Helper()
	if err := h.f.Approve(h.cli); err != nil {
		t.Fatal(err)
	}
}

// cache is the folder Codex loads the plugin from.
func (h *host) cache() string { return h.f.CachePath("eva-md", "eva-md") }

// listRequest asks Codex's app server for its hooks, as the doctor does.
const listRequest = `{"id":1,"method":"initialize","params":{"clientInfo":{"name":"test","version":"1"}}}
{"method":"initialized","params":{}}
{"id":2,"method":"hooks/list","params":{"cwds":["/made-up/project"]}}
`

// hooksList is what hooks/list gives, by hook key: its trust word.
func hooksList(t *testing.T, f *Fake) map[string]string {
	t.Helper()
	result := hooksListResult(t, f)
	trust := map[string]string{}
	for _, d := range result.Data {
		for _, hook := range d.Hooks {
			wantWireEvent(t, hook.Key, hook.EventName)
			trust[hook.Key] = hook.TrustStatus
		}
	}
	return trust
}

// listResult is hooks/list's result, as internal/health/hosts.go reads it,
// with each hook's key.
type listResult struct {
	Errors   []json.RawMessage
	Warnings []json.RawMessage
	Data     []struct {
		Errors   []json.RawMessage
		Warnings []json.RawMessage
		Hooks    []struct {
			Key         string
			EventName   string
			PluginID    string `json:"pluginId"`
			Enabled     bool   `json:"enabled"`
			TrustStatus string `json:"trustStatus"`
		} `json:"hooks"`
	} `json:"data"`
}

// hooksListResult asks Codex's app server for its hooks, as the doctor does,
// and gives the reply to hooks/list.
func hooksListResult(t *testing.T, f *Fake) listResult {
	t.Helper()
	out, err := f.RunStdin(context.Background(), []byte(listRequest), Codex, "--no-daemon", "app-server", "--stdio")
	if err != nil {
		t.Fatal(err)
	}
	var result listResult
	found := false
	s := bufio.NewScanner(bytes.NewReader(out))
	for s.Scan() {
		var reply struct {
			ID     int
			Result json.RawMessage
		}
		if err := json.Unmarshal(s.Bytes(), &reply); err != nil {
			t.Fatal(err)
		}
		if reply.ID != 2 {
			continue
		}
		if err := json.Unmarshal(reply.Result, &result); err != nil {
			t.Fatal(err)
		}
		found = true
	}
	if !found {
		t.Fatalf("hooks/list gave no reply: %q", out)
	}
	return result
}

// wireEvents are the events these tests declare, as the app server's wire
// spells them: in camelCase, which internal/health/hosts.go reads.
var wireEvents = map[string]string{"SessionStart": "sessionStart", "Stop": "stop"}

// wantWireEvent checks a hook's eventName is its key's event as the wire
// spells it.
func wantWireEvent(t *testing.T, key, eventName string) {
	t.Helper()
	for _, segment := range strings.Split(key, ":") {
		if wire, ok := wireEvents[segment]; ok {
			if eventName != wire {
				t.Errorf("%s is listed with eventName %q, want %q", key, eventName, wire)
			}
			return
		}
	}
	t.Errorf("%s, eventName %q, names no event the tests declare", key, eventName)
}

// fire fires event on the host's CLI and gives each hook's result by key.
func (h *host) fire(t *testing.T, event string) map[string]Result {
	t.Helper()
	results, err := h.f.Fire(context.Background(), h.cli, event, []byte(`{"hook_event_name":"`+event+`"}`))
	if err != nil {
		t.Fatal(err)
	}
	byKey := map[string]Result{}
	for _, r := range results {
		byKey[r.Key] = r
	}
	return byKey
}

// The keys of eva-md's two hooks.
const (
	startKey = "eva-md@eva-md:hooks/hooks.json:SessionStart:0:0"
	stopKey  = "eva-md@eva-md:hooks/hooks.json:Stop:0:0"
)

// wantTrust checks every hook hooks/list gives has word, and that both of
// eva-md's are listed.
func wantTrust(t *testing.T, f *Fake, word string) {
	t.Helper()
	got := hooksList(t, f)
	if len(got) != 2 || got[startKey] == "" || got[stopKey] == "" {
		t.Fatalf("hooks/list = %v, want eva-md's two hooks", got)
	}
	for key, w := range got {
		if w != word {
			t.Errorf("%s is %s, want %s", key, w, word)
		}
	}
}

// wantRan checks the hook ran and printed want first.
func wantRan(t *testing.T, r Result, want string) {
	t.Helper()
	if r.Status != Ran || r.Exit != 0 || !strings.HasPrefix(string(r.Stdout), want+"\n") {
		t.Errorf("%s: %s (%s), exit %d, printed %q; want it to run and print %q", r.Key, r.Status, r.Reason, r.Exit, r.Stdout, want)
	}
}

// wantNotRun checks the hook didn't run, with status and a reason that
// holds reason.
func wantNotRun(t *testing.T, r Result, status, reason string) {
	t.Helper()
	if r.Status != status || !strings.Contains(r.Reason, reason) || len(r.Stdout) != 0 {
		t.Errorf("%s: %s (%s), printed %q; want %s with %q", r.Key, r.Status, r.Reason, r.Stdout, status, reason)
	}
}
