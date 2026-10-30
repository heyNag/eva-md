package install

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/heyNag/eva-md/internal/config"
	"github.com/heyNag/eva-md/internal/paths"
)

type fakeHost struct {
	dirs               paths.Dirs
	installed, markets map[string]bool
	calls              []string
	fail               string
	failAfter          bool
	foreign            string
	disabled           bool
}

func (f *fakeHost) run(_ context.Context, host string, args ...string) ([]byte, error) {
	command := host + " " + strings.Join(args, " ")
	f.calls = append(f.calls, command)
	fail := f.fail != "" && strings.HasPrefix(command, f.fail)
	if fail && !f.failAfter {
		f.fail = ""
		return nil, errors.New("synthetic host failure")
	}
	var value any = map[string]any{"success": true}
	switch strings.Join(args, " ") {
	case "plugin list --json":
		list := []any{}
		// An unrelated plugin must never be removed by install or rollback.
		if host == "codex" {
			list = append(list, map[string]any{"pluginId": "other@personal", "name": "other", "enabled": true})
		}
		if f.installed[host] {
			item := map[string]any{"pluginId": "eva-md@eva-md", "id": "eva-md@eva-md", "name": "eva-md", "enabled": true,
				"readFromFolder": pluginPath(f.dirs.Share, host), "folderVersion": "1", "errors": []any{},
				"source": map[string]any{"source": "local", "path": pluginPath(f.dirs.Share, host)}}
			list = append(list, item)
		}
		if host == "codex" {
			value = map[string]any{"installed": list, "available": []any{}}
		} else {
			value = list
		}
	case "plugin marketplace list --json":
		list := []any{map[string]any{"name": "unrelated", "root": "/unrelated", "installLocation": "/unrelated"}}
		if f.markets[host] {
			location := marketplacePath(f.dirs.Share, host)
			if f.foreign != "" {
				location = f.foreign
			}
			list = append(list, map[string]any{"name": "eva-md", "root": location, "installLocation": location})
		}
		if host == "codex" {
			value = map[string]any{"marketplaces": list}
		} else {
			value = list
		}
	case "features list":
		if f.disabled {
			return []byte("hooks experimental false\n"), nil
		}
		return []byte("hooks experimental true\n"), nil
	default:
		if len(args) >= 3 && args[0] == "plugin" && args[1] == "marketplace" {
			if args[2] == "add" {
				f.markets[host] = true
			}
			if args[2] == "remove" {
				delete(f.markets, host)
			}
		} else if len(args) >= 2 && args[0] == "plugin" {
			if args[1] == "add" || args[1] == "install" {
				f.installed[host] = true
			}
			if args[1] == "remove" || args[1] == "uninstall" {
				delete(f.installed, host)
			}
		}
	}
	if fail {
		f.fail = ""
		return nil, errors.New("synthetic failure after mutation")
	}
	return json.Marshal(value)
}
func setup(t *testing.T) (Options, *fakeHost) {
	t.Helper()
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("GIT_CONFIG_GLOBAL", filepath.Join(home, "gitconfig"))
	t.Setenv("GIT_CONFIG_NOSYSTEM", "1")
	dirs, err := paths.Default()
	if err != nil {
		t.Fatal(err)
	}
	c := config.Default()
	c.WikiPath = filepath.Join(home, "wiki")
	c.ExpectedRemote = "github.com/example/wiki"
	c.Instance = "test-12345678"
	c.MachineLabel = "test"
	if err = config.Save(dirs.ConfigFile(), c); err != nil {
		t.Fatal(err)
	}
	bin := filepath.Join(home, "fake-bin")
	if err = os.Mkdir(bin, 0o700); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"claude", "codex"} {
		if err = os.WriteFile(filepath.Join(bin, name), []byte("#!/bin/sh\nexit 99\n"), 0o700); err != nil {
			t.Fatal(err)
		}
	}
	t.Setenv("PATH", bin)
	source := filepath.Join(home, "eva-md-source")
	if err = os.WriteFile(source, []byte("binary version 1"), 0o700); err != nil {
		t.Fatal(err)
	}
	f := &fakeHost{dirs: dirs, installed: map[string]bool{}, markets: map[string]bool{}}
	return Options{Dirs: dirs, Binary: source, Host: "all"}, f
}
func mutateCalls(calls []string) []string {
	var result []string
	for _, call := range calls {
		if strings.Contains(call, " plugin add ") || strings.Contains(call, " plugin install ") || strings.Contains(call, " plugin remove ") || strings.Contains(call, " plugin uninstall ") || strings.Contains(call, " marketplace add ") || strings.Contains(call, " marketplace remove ") {
			result = append(result, call)
		}
	}
	return result
}

func TestInstallBinaryOnlyAndUninstall(t *testing.T) {
	opts, f := setup(t)
	ctx := context.Background()
	result, err := runInstall(ctx, opts, f.run)
	if err != nil {
		t.Fatal(err)
	}
	if !result.NeedsTrust || len(result.Hosts) != 2 {
		t.Fatalf("unexpected result: %+v", result)
	}
	snap := map[string][]byte{}
	for _, host := range result.Hosts {
		files, _ := render(host, result.Binary)
		for name, want := range files {
			path := filepath.Join(marketplacePath(opts.Dirs.Share, host), name)
			got, err := os.ReadFile(path)
			if err != nil || !bytes.Equal(got, want) {
				t.Fatalf("generated %s: %v", name, err)
			}
			snap[path] = got
		}
	}
	// Replace only binary bytes. No native mutation, manifest rewrite, or
	// additional trust request is needed on an identical plugin installation.
	f.calls = nil
	if err = os.WriteFile(opts.Binary, []byte("binary version 2"), 0o700); err != nil {
		t.Fatal(err)
	}
	again, err := runInstall(ctx, opts, f.run)
	if err != nil {
		t.Fatal(err)
	}
	if again.NeedsTrust || len(mutateCalls(f.calls)) != 0 {
		t.Fatalf("binary update mutated registrations: %+v", f.calls)
	}
	for path, want := range snap {
		got, _ := os.ReadFile(path)
		if !bytes.Equal(got, want) {
			t.Fatalf("plugin changed: %s", path)
		}
	}
	binary, _ := os.ReadFile(result.Binary)
	if string(binary) != "binary version 2" {
		t.Fatal("binary was not replaced")
	}
	info, _ := os.Stat(result.Binary)
	if info.Mode().Perm() != 0o700 {
		t.Fatalf("binary mode %o", info.Mode().Perm())
	}
	f.calls = nil
	if _, err = uninstall(ctx, opts.Dirs, false, f.run); err != nil {
		t.Fatal(err)
	}
	if len(mutateCalls(f.calls)) != 0 {
		t.Fatal("preview mutated host")
	}
	// User data survives actual uninstall.
	if err = os.MkdirAll(opts.Dirs.Inbox(), 0o700); err != nil {
		t.Fatal(err)
	}
	keep := filepath.Join(opts.Dirs.Inbox(), "keep.jsonl")
	if err = os.WriteFile(keep, []byte("saved"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err = uninstall(ctx, opts.Dirs, true, f.run); err != nil {
		t.Fatal(err)
	}
	for _, host := range []string{"claude", "codex"} {
		if f.installed[host] || f.markets[host] {
			t.Fatal("host still registered")
		}
	}
	if _, err = os.Stat(result.Binary); !errors.Is(err, os.ErrNotExist) {
		t.Fatal("binary retained")
	}
	for _, path := range []string{keep, opts.Dirs.ConfigFile()} {
		if _, err = os.Stat(path); err != nil {
			t.Fatalf("removed user data: %s", path)
		}
	}
	if _, err = uninstall(ctx, opts.Dirs, true, f.run); err != nil {
		t.Fatalf("idempotent uninstall: %v", err)
	}
}

func TestDryRunHasNoFilesystemOrHostEffects(t *testing.T) {
	opts, f := setup(t)
	opts.DryRun = true
	if _, err := runInstall(context.Background(), opts, f.run); err != nil {
		t.Fatal(err)
	}
	if len(mutateCalls(f.calls)) != 0 {
		t.Fatal("dry run mutated host")
	}
	for _, dir := range []string{opts.Dirs.Share, opts.Dirs.State} {
		if _, err := os.Stat(dir); !errors.Is(err, os.ErrNotExist) {
			t.Fatalf("dry run created %s", dir)
		}
	}
}

func TestRollbackPartialAndUncertainWrites(t *testing.T) {
	for _, after := range []bool{false, true} {
		t.Run(map[bool]string{false: "before", true: "after"}[after], func(t *testing.T) {
			opts, f := setup(t)
			f.fail = "codex plugin add "
			f.failAfter = after
			_, err := runInstall(context.Background(), opts, f.run)
			if err == nil || strings.Contains(err.Error(), "rollback incomplete") {
				t.Fatalf("expected fully rolled back failure: %v", err)
			}
			if len(f.installed) != 0 || len(f.markets) != 0 {
				t.Fatalf("registration remains: %+v %+v", f.installed, f.markets)
			}
			if _, err = os.Stat(filepath.Join(opts.Dirs.Share, "bin", "eva-md")); !errors.Is(err, os.ErrNotExist) {
				t.Fatal("new binary remains")
			}
			for _, host := range []string{"claude", "codex"} {
				if _, err = os.Stat(marketplacePath(opts.Dirs.Share, host)); !errors.Is(err, os.ErrNotExist) {
					t.Fatal("new marketplace remains")
				}
			}
		})
	}
}

func TestRollbackKeepsPreexistingInstallAndMarketplace(t *testing.T) {
	opts, f := setup(t)
	opts.Host = "claude"
	if _, err := runInstall(context.Background(), opts, f.run); err != nil {
		t.Fatal(err)
	}
	f.markets["codex"] = true // Existing registration must be preserved.
	f.fail = "codex plugin add "
	f.failAfter = true
	opts.Host = "all"
	if err := os.WriteFile(opts.Binary, []byte("new binary"), 0o700); err != nil {
		t.Fatal(err)
	}
	_, err := runInstall(context.Background(), opts, f.run)
	if err == nil {
		t.Fatal("expected failure")
	}
	if !f.installed["claude"] || f.installed["codex"] || !f.markets["codex"] {
		t.Fatalf("prior host state lost: %+v %+v", f.installed, f.markets)
	}
	data, _ := os.ReadFile(filepath.Join(opts.Dirs.Share, "bin", "eva-md"))
	if string(data) != "binary version 1" {
		t.Fatal("old binary not restored")
	}
}

func TestFailedUpgradeRestoresOldPluginBytes(t *testing.T) {
	opts, f := setup(t)
	opts.Host = "claude"
	if _, err := runInstall(context.Background(), opts, f.run); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(pluginPath(opts.Dirs.Share, "claude"), ".claude-plugin", "plugin.json")
	old, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	old = bytes.Replace(old, []byte("Resume your project"), []byte("Older plugin: resume your project"), 1)
	if err = os.WriteFile(path, old, 0o600); err != nil {
		t.Fatal(err)
	}
	opts.Host = "all"
	f.fail = "codex plugin add "
	if _, err = runInstall(context.Background(), opts, f.run); err == nil {
		t.Fatal("expected failure")
	}
	got, _ := os.ReadFile(path)
	if !bytes.Equal(old, got) || !f.installed["claude"] {
		t.Fatal("old enabled plugin was not restored")
	}
}

func TestIncompleteRollbackKeepsLoadableFiles(t *testing.T) {
	opts, f := setup(t)
	run := func(ctx context.Context, host string, args ...string) ([]byte, error) {
		command := host + " " + strings.Join(args, " ")
		if strings.HasPrefix(command, "codex plugin add ") || strings.HasPrefix(command, "claude plugin uninstall ") {
			return nil, errors.New("synthetic failure")
		}
		return f.run(ctx, host, args...)
	}
	_, err := runInstall(context.Background(), opts, run)
	if err == nil || !strings.Contains(err.Error(), "rollback incomplete") {
		t.Fatalf("missing actionable rollback result: %v", err)
	}
	for _, path := range []string{filepath.Join(opts.Dirs.Share, "bin", "eva-md"), pluginPath(opts.Dirs.Share, "claude")} {
		if _, err = os.Stat(path); err != nil {
			t.Fatalf("deleted still-registered plugin files: %s", path)
		}
	}
}

func TestOwnershipAndSymlinkRefusals(t *testing.T) {
	for _, scenario := range []string{"foreign-marketplace", "unmanaged-binary", "symlink-share", "unknown-file", "symlink-file", "hooks-disabled"} {
		t.Run(scenario, func(t *testing.T) {
			opts, f := setup(t)
			switch scenario {
			case "foreign-marketplace":
				f.markets["codex"] = true
				f.foreign = "/other/marketplace"
			case "unmanaged-binary":
				_ = os.MkdirAll(filepath.Join(opts.Dirs.Share, "bin"), 0o700)
				_ = os.WriteFile(filepath.Join(opts.Dirs.Share, "bin", "eva-md"), []byte("not ours"), 0o700)
			case "symlink-share":
				_ = os.MkdirAll(filepath.Dir(opts.Dirs.Share), 0o700)
				_ = os.Symlink(t.TempDir(), opts.Dirs.Share)
			case "hooks-disabled":
				f.disabled = true
			default:
				if _, err := runInstall(context.Background(), opts, f.run); err != nil {
					t.Fatal(err)
				}
				target := filepath.Join(marketplacePath(opts.Dirs.Share, "codex"), "extra")
				if scenario == "symlink-file" {
					_ = os.Symlink(opts.Binary, target)
				} else {
					_ = os.WriteFile(target, []byte("user content"), 0o600)
				}
			}
			f.calls = nil
			if _, err := runInstall(context.Background(), opts, f.run); err == nil {
				t.Fatal("unsafe install accepted")
			}
			if len(mutateCalls(f.calls)) != 0 {
				t.Fatal("preflight failure mutated host")
			}
		})
	}
}

func TestGeneratedHookContracts(t *testing.T) {
	binary := "/Users/test/it's a space/bin/eva-md"
	for _, host := range []string{"claude", "codex"} {
		files, err := render(host, binary)
		if err != nil {
			t.Fatal(err)
		}
		var doc struct {
			Hooks map[string][]struct{ Hooks []map[string]any }
		}
		if err = json.Unmarshal(files["plugins/eva-md/hooks/hooks.json"], &doc); err != nil {
			t.Fatal(err)
		}
		want := 10
		if host == "codex" {
			want = 7
		}
		if len(doc.Hooks) != want {
			t.Fatalf("%s event count %d", host, len(doc.Hooks))
		}
		for event, groups := range doc.Hooks {
			c := groups[0].Hooks[0]
			if host == "claude" {
				if c["command"] != binary {
					t.Fatal("exec path changed")
				}
				if _, ok := c["args"].([]any); !ok {
					t.Fatal("missing exec arguments")
				}
			} else {
				command, _ := c["command"].(string)
				if !strings.HasSuffix(command, " || echo '{}'") || !strings.Contains(command, "'\"'\"'") {
					t.Fatal("missing safe shell quoting or fail-open fallback")
				}
				limit, ok := c["additionalContextLimit"]
				contextEvent := event == "SessionStart" || event == "UserPromptSubmit"
				if ok != contextEvent || (ok && limit != float64(3000)) {
					t.Fatalf("bad context limit on %s", event)
				}
			}
		}
		if host == "codex" {
			if _, exists := files["plugins/eva-md/plugin.json"]; exists {
				t.Fatal("portable manifest must not shadow compatibility hooks")
			}
		}
		if icon := files[iconPath]; !bytes.HasPrefix(icon, []byte("\x89PNG\r\n\x1a\n")) {
			t.Fatalf("%s plugin has no PNG icon", host)
		}
		manifest := "plugins/eva-md/.claude-plugin/plugin.json"
		if host == "codex" {
			manifest = "plugins/eva-md/.codex-plugin/plugin.json"
		}
		if !bytes.Contains(files[manifest], []byte(`"./assets/icon.png"`)) {
			t.Fatalf("%s manifest does not point at the icon", host)
		}
		next, _ := render(host, binary)
		if !reflect.DeepEqual(files, next) {
			t.Fatal("templates not deterministic")
		}
	}
}

func TestNativeRunnerIsBoundedAndHidesDiagnostics(t *testing.T) {
	opts, _ := setup(t)
	script := filepath.Join(os.Getenv("PATH"), "claude")
	_ = os.WriteFile(script, []byte("#!/bin/sh\nprintf '[]'\nprintf 'sensitive diagnostic' >&2\n"), 0o700)
	data, err := native(context.Background(), "claude", "plugin", "list", "--json")
	if err != nil || string(data) != "[]" {
		t.Fatalf("%q %v", data, err)
	}
	_ = os.WriteFile(script, []byte("#!/bin/sh\nprintf 'sensitive diagnostic' >&2\nexit 1\n"), 0o700)
	_, err = native(context.Background(), "claude", "plugin", "list", "--json")
	if err == nil || strings.Contains(err.Error(), "sensitive") {
		t.Fatalf("unsafe error %v", err)
	}
	_ = opts
}
