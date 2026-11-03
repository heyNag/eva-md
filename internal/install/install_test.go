package install

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"os"
	"os/signal"
	"path/filepath"
	"reflect"
	"slices"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/heyNag/eva-md/internal/config"
	"github.com/heyNag/eva-md/internal/paths"
)

// installContext is the key of a value a test may put in the context it
// gives runInstall: a *context.Context, which gets the context an interrupt
// of that install cancels.
type installContext struct{}

// Tests check what install does, not how fast the machine starts the host
// commands or syncs its disk: the time limits that protect a host are raised
// for every test, before any test runs. A test can also see the context an
// interrupt of its install cancels.
func TestMain(m *testing.M) {
	HostTimeout = 10 * time.Minute
	HostWaitDelay = 10 * time.Minute
	RollbackTimeout = 10 * time.Minute
	notify := notifyInterrupt
	notifyInterrupt = func(parent context.Context, signals ...os.Signal) (context.Context, context.CancelFunc) {
		ctx, stop := notify(parent, signals...)
		if seen, ok := parent.Value(installContext{}).(*context.Context); ok {
			*seen = ctx
		}
		return ctx, stop
	}
	os.Exit(m.Run())
}

type fakeHost struct {
	dirs               paths.Dirs
	installed, markets map[string]bool
	calls              []string
	fail               string
	failAfter          bool
	foreign            string
	disabled           bool
	refuseReadd        bool // adding a plugin the host has fails
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
	key := strings.Join(args, " ")
	market, scoped := strings.CutPrefix(key, "plugin list --json --marketplace=")
	if scoped {
		key = "plugin list --json"
	}
	// Codex fails its marketplace list and its plugin lists, as 0.166.0 does
	// for those that take eva-md's marketplace in, while the eva-md
	// marketplace it has registered at eva-md's folder has no manifest there.
	if host == "codex" && f.markets[host] && f.foreign == "" && (key == "plugin list --json" || key == "plugin marketplace list --json") {
		path := marketplacePath(f.dirs.Share, host)
		if _, err := os.Stat(filepath.Join(path, ".agents", "plugins", "marketplace.json")); err != nil {
			return nil, missingFolder(command, path)
		}
	}
	switch key {
	case "plugin list --json":
		// Codex's list of every plugin also asks chatgpt.com, and waits for
		// it on a network that holds the request, so Codex is asked one
		// marketplace at a time; Claude Code is asked for all.
		if scoped != (host == "codex") {
			return nil, errors.New("synthetic: the plugin list was asked for the wrong way")
		}
		list := []any{}
		// An unrelated plugin must never be removed by install or rollback.
		if host == "codex" && market == "unrelated" {
			list = append(list, map[string]any{"pluginId": "other@unrelated", "name": "other", "enabled": true})
		}
		if f.installed[host] && (host == "claude" || market == "eva-md") {
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
			if (args[1] == "add" || args[1] == "install") && f.refuseReadd && f.installed[host] {
				return nil, errors.New("synthetic refusal: already installed")
			}
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

// missingFolder is a Codex command's failure while the eva-md marketplace it
// has registered at path has no manifest, worded as Codex 0.166.0 words it.
func missingFolder(command, path string) error {
	return &hostError{msg: "install: " + command + " failed", diagnostics: "Error: failed to load marketplace(s):\n- `eva-md` at " + path + ": marketplace root does not contain a supported manifest\n"}
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

// Install records the absolute path of each host CLI it found, for the
// update job, whose PATH may lack them; a dry run records nothing, a
// reinstall of one host keeps the other's path, a folder on PATH that is a
// symlink, as a version manager's for one shell, is recorded as the folder
// it links to, with the CLI's own name, and uninstall removes the record.
func TestInstallRecordsTheCLIPaths(t *testing.T) {
	opts, f := setup(t)
	ctx := context.Background()
	file := filepath.Join(opts.Dirs.State, cliPathsFile)
	recorded := func() map[string]string {
		t.Helper()
		data, err := os.ReadFile(file)
		var got map[string]string
		if err == nil {
			err = json.Unmarshal(data, &got)
		}
		if info, e := os.Stat(file); err != nil || e != nil || info.Mode().Perm() != 0o600 {
			t.Fatalf("record: %v %v", err, e)
		}
		return got
	}
	// The temporary home may itself sit under a symlink, as on macOS.
	resolved := func(dir string) string {
		t.Helper()
		dir, err := filepath.EvalSymlinks(dir)
		if err != nil {
			t.Fatal(err)
		}
		return dir
	}
	dry := opts
	dry.DryRun = true
	if _, err := runInstall(ctx, dry, f.run); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(file); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("a dry run recorded the CLIs: %v", err)
	}
	if _, err := runInstall(ctx, opts, f.run); err != nil {
		t.Fatal(err)
	}
	bin := resolved(filepath.Join(os.Getenv("HOME"), "fake-bin"))
	want := map[string]string{"claude": filepath.Join(bin, "claude"), "codex": filepath.Join(bin, "codex")}
	if got := recorded(); !reflect.DeepEqual(got, want) {
		t.Fatalf("recorded %v, want %v", got, want)
	}
	moved := filepath.Join(os.Getenv("HOME"), "moved-bin")
	if err := os.Mkdir(moved, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(moved, "claude"), []byte("#!/bin/sh\nexit 99\n"), 0o700); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", moved+string(filepath.ListSeparator)+bin)
	one := opts
	one.Host = "claude"
	if _, err := runInstall(ctx, one, f.run); err != nil {
		t.Fatal(err)
	}
	want["claude"] = filepath.Join(resolved(moved), "claude")
	if got := recorded(); !reflect.DeepEqual(got, want) {
		t.Fatalf("after a reinstall of claude: %v, want %v", got, want)
	}
	// The shell's folder links to the version's, where claude links to the
	// script it runs, as an npm install leaves it.
	version := filepath.Join(os.Getenv("HOME"), "versions", "v1", "bin")
	shell := filepath.Join(os.Getenv("HOME"), "shell-4242-bin")
	if err := os.MkdirAll(version, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(version, "cli.js"), []byte("#!/bin/sh\nexit 99\n"), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink("cli.js", filepath.Join(version, "claude")); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(version, shell); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", shell+string(filepath.ListSeparator)+bin)
	if _, err := runInstall(ctx, one, f.run); err != nil {
		t.Fatal(err)
	}
	want["claude"] = filepath.Join(resolved(version), "claude")
	if got := recorded(); !reflect.DeepEqual(got, want) {
		t.Fatalf("from a symlinked folder: %v, want %v", got, want)
	}
	if _, err := uninstall(ctx, opts.Dirs, true, f.run); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(file); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("uninstall kept the record: %v", err)
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
	// Codex has eva-md's marketplace, folder and all, but not the plugin;
	// that registration must be preserved.
	opts.Host = "codex"
	if _, err := runInstall(context.Background(), opts, f.run); err != nil {
		t.Fatal(err)
	}
	delete(f.installed, "codex")
	f.calls = nil
	f.fail = "codex plugin add "
	f.failAfter = true
	opts.Host = "all"
	if err := os.WriteFile(opts.Binary, []byte("new binary"), 0o700); err != nil {
		t.Fatal(err)
	}
	_, err := runInstall(context.Background(), opts, f.run)
	if err == nil || !strings.Contains(err.Error(), "this run's changes were rolled back") || !slices.Contains(f.calls, "codex plugin add eva-md@eva-md --json") {
		t.Fatalf("expected a failed plugin add, rolled back: %v %v", err, f.calls)
	}
	if !f.installed["claude"] || f.installed["codex"] || !f.markets["codex"] {
		t.Fatalf("prior host state lost: %+v %+v", f.installed, f.markets)
	}
	if _, err := os.Stat(filepath.Join(marketplacePath(opts.Dirs.Share, "codex"), ".agents", "plugins", "marketplace.json")); err != nil {
		t.Fatalf("the Codex marketplace's folder went: %v", err)
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

// A host that refuses to add a plugin it has still loads the new files of
// its folder in place, so an upgrade, and the rollback of a failed one,
// verify the registration rather than fail on the refusal.
func TestUpgradeWhenTheHostRefusesToAddAnInstalledPlugin(t *testing.T) {
	opts, f := setup(t)
	opts.Host = "claude"
	if _, err := runInstall(context.Background(), opts, f.run); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(pluginPath(opts.Dirs.Share, "claude"), ".claude-plugin", "plugin.json")
	current, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	older := bytes.Replace(current, []byte("Resume your project"), []byte("Older plugin: resume your project"), 1)
	if err = os.WriteFile(path, older, 0o600); err != nil {
		t.Fatal(err)
	}
	f.refuseReadd = true
	if _, err := runInstall(context.Background(), opts, f.run); err != nil {
		t.Fatalf("upgrade: %v", err)
	}
	if got, _ := os.ReadFile(path); !bytes.Equal(got, current) || !f.installed["claude"] {
		t.Fatal("the upgrade didn't write the new plugin files")
	}
	// A failed upgrade of both hosts rolls the Claude plugin back without
	// stopping at the refusal.
	if err = os.WriteFile(path, older, 0o600); err != nil {
		t.Fatal(err)
	}
	opts.Host = "all"
	f.fail = "codex plugin add "
	_, err = runInstall(context.Background(), opts, f.run)
	if err == nil || strings.Contains(err.Error(), "rollback incomplete") {
		t.Fatalf("a failed upgrade = %v", err)
	}
	if got, _ := os.ReadFile(path); !bytes.Equal(got, older) || !f.installed["claude"] {
		t.Fatal("the rollback didn't restore the old plugin")
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

func TestInstallRestoresMissingBinary(t *testing.T) {
	opts, f := setup(t)
	result, err := runInstall(context.Background(), opts, f.run)
	if err != nil {
		t.Fatal(err)
	}
	if err = os.Remove(result.Binary); err != nil {
		t.Fatal(err)
	}
	f.calls = nil
	if _, err = runInstall(context.Background(), opts, f.run); err != nil {
		t.Fatalf("missing binary not restored: %v", err)
	}
	if data, _ := os.ReadFile(result.Binary); string(data) != "binary version 1" {
		t.Fatal("binary not restored")
	}
	if len(mutateCalls(f.calls)) != 0 {
		t.Fatalf("binary restore mutated registrations: %+v", f.calls)
	}
	// A plugin folder that differs, as after an upgrade, is rendered again.
	if err = os.Remove(result.Binary); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(pluginPath(opts.Dirs.Share, "claude"), ".claude-plugin", "plugin.json")
	if err = os.WriteFile(path, []byte("{}"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err = runInstall(context.Background(), opts, f.run); err != nil {
		t.Fatalf("missing binary not restored beside a changed plugin folder: %v", err)
	}
	if data, _ := os.ReadFile(result.Binary); string(data) != "binary version 1" {
		t.Fatal("binary not restored")
	}
	if data, _ := os.ReadFile(path); string(data) == "{}" {
		t.Fatal("changed plugin folder not rendered again")
	}
	// Without eva-md's install marker it still refuses.
	if err = os.Remove(result.Binary); err != nil {
		t.Fatal(err)
	}
	if err = os.Remove(filepath.Join(opts.Dirs.Share, installMarker)); err != nil {
		t.Fatal(err)
	}
	if _, err = runInstall(context.Background(), opts, f.run); err == nil || !strings.Contains(err.Error(), "uninstall --apply") {
		t.Fatalf("restored a binary without the install marker: %v", err)
	}
}

// A run that fails before its first change says that none of the planned
// actions ran.
func TestPreflightFailureSaysNothingRan(t *testing.T) {
	opts, f := setup(t)
	lists := 0
	run := func(ctx context.Context, host string, args ...string) ([]byte, error) {
		if host == "claude" && strings.Join(args, " ") == "plugin list --json" {
			// Another install registers the plugin between the two looks.
			if lists++; lists == 2 {
				f.installed["claude"] = true
			}
		}
		return f.run(ctx, host, args...)
	}
	_, err := runInstall(context.Background(), opts, run)
	if err == nil || !strings.Contains(err.Error(), "none of the planned actions ran") {
		t.Fatalf("preflight failure: %v", err)
	}
	if len(mutateCalls(f.calls)) != 0 {
		t.Fatalf("preflight failure changed registrations: %+v", f.calls)
	}
}

// Codex is asked for its plugins one marketplace at a time, never for all of
// them, which also asks chatgpt.com and waits for it on a network that holds
// the request. An eva-md copy from another of its marketplaces is still
// refused.
func TestCodexIsAskedOneMarketplaceAtATime(t *testing.T) {
	opts, f := setup(t)
	opts.Host = "codex"
	if _, err := runInstall(context.Background(), opts, f.run); err != nil {
		t.Fatal(err)
	}
	asked := map[string]int{}
	for _, call := range f.calls {
		if strings.HasPrefix(call, "codex plugin list") {
			asked[call]++
		}
	}
	for _, want := range []string{"codex plugin list --json --marketplace=unrelated", "codex plugin list --json --marketplace=eva-md"} {
		if asked[want] == 0 {
			t.Errorf("never asked %q: %v", want, f.calls)
		}
	}
	if asked["codex plugin list --json"] != 0 {
		t.Errorf("asked Codex for every plugin: %v", f.calls)
	}
	// A marketplace listed twice is asked once.
	twice := func(ctx context.Context, host string, args ...string) ([]byte, error) {
		if strings.Join(args, " ") == "plugin marketplace list --json" {
			return []byte(`{"marketplaces":[{"name":"unrelated","root":"/unrelated"},{"name":"unrelated","root":"/unrelated"}]}`), nil
		}
		return f.run(ctx, host, args...)
	}
	f.calls = nil
	if _, err := inspect(context.Background(), opts.Dirs, "codex", twice); err != nil {
		t.Fatal(err)
	}
	if n := strings.Count(strings.Join(f.calls, "\n"), "--marketplace=unrelated"); n != 1 {
		t.Errorf("a marketplace listed twice was asked %d times", n)
	}
	copied := func(ctx context.Context, host string, args ...string) ([]byte, error) {
		if strings.Join(args, " ") == "plugin list --json --marketplace=unrelated" {
			return []byte(`{"installed":[{"pluginId":"eva-md@unrelated","name":"eva-md","marketplaceName":"unrelated","enabled":true}]}`), nil
		}
		return f.run(ctx, host, args...)
	}
	if _, err := inspect(context.Background(), opts.Dirs, "codex", copied); !errors.Is(err, ErrConflict) || !strings.Contains(err.Error(), "another eva-md plugin copy") {
		t.Fatalf("a copy from another marketplace: %v", err)
	}
}

// A marketplace's list that keeps failing stops uninstall and install before
// they change anything, rather than taking the plugins it would name as
// missing.
func TestFailedMarketplaceListChangesNothing(t *testing.T) {
	opts, f := setup(t)
	opts.Host = "codex"
	if _, err := runInstall(context.Background(), opts, f.run); err != nil {
		t.Fatal(err)
	}
	failing := func(ctx context.Context, host string, args ...string) ([]byte, error) {
		if host == "codex" && strings.Join(args, " ") == "plugin list --json --marketplace=eva-md" {
			f.calls = append(f.calls, host+" "+strings.Join(args, " "))
			return nil, errors.New("synthetic: the marketplace's list failed")
		}
		return f.run(ctx, host, args...)
	}
	f.calls = nil
	if _, err := uninstall(context.Background(), opts.Dirs, true, failing); err == nil || errors.Is(err, ErrConflict) {
		t.Fatalf("uninstall without the marketplace's list: %v", err)
	}
	if _, err := runInstall(context.Background(), opts, failing); err == nil || errors.Is(err, ErrConflict) {
		t.Fatalf("install without the marketplace's list: %v", err)
	}
	if len(mutateCalls(f.calls)) != 0 || !f.installed["codex"] || !f.markets["codex"] {
		t.Fatalf("a failed list changed registrations: %v", f.calls)
	}
}

// Codex can't list while the eva-md marketplace it has registered at
// eva-md's folder has no manifest, as after that folder was deleted. Install
// stops there before it changes anything, its dry run too, and says to
// uninstall first; uninstall clears the registration, and install then
// starts afresh. Both hosts, with the marketplace folder alone gone or the
// whole install folder.
func TestInstallStopsAtAMissingCodexMarketplaceFolder(t *testing.T) {
	for _, gone := range []string{"marketplace folder", "install folder"} {
		t.Run(gone, func(t *testing.T) {
			ctx := context.Background()
			opts, f := setup(t)
			if _, err := runInstall(ctx, opts, f.run); err != nil {
				t.Fatal(err)
			}
			folder := filepath.Join(opts.Dirs.Share, "marketplace", "codex")
			if gone == "install folder" {
				folder = opts.Dirs.Share
			}
			if err := os.RemoveAll(folder); err != nil {
				t.Fatal(err)
			}
			s, err := inspect(ctx, opts.Dirs, "codex", f.run)
			if err != nil || !s.FolderMissing || !s.Marketplace || s.Installed || s.Valid || s.Reason == "" {
				t.Fatalf("status with the folder gone: %+v %v", s, err)
			}
			for _, dry := range []bool{true, false} {
				f.calls = nil
				opts.DryRun = dry
				_, err := runInstall(ctx, opts, f.run)
				if err == nil || !strings.Contains(err.Error(), "its folder is missing") || !strings.Contains(err.Error(), "uninstall --apply") || len(mutateCalls(f.calls)) != 0 {
					t.Fatalf("install, dry run %v, with the folder gone: %v %v", dry, err, f.calls)
				}
			}
			opts.DryRun = false
			if _, err := os.Stat(folder); !errors.Is(err, os.ErrNotExist) {
				t.Fatalf("install wrote %s: %v", folder, err)
			}
			if _, err := uninstall(ctx, opts.Dirs, true, f.run); err != nil {
				t.Fatal(err)
			}
			if _, err := runInstall(ctx, opts, f.run); err != nil {
				t.Fatal(err)
			}
			for _, host := range []string{"claude", "codex"} {
				if s, err := inspect(ctx, opts.Dirs, host, f.run); err != nil || !s.Valid {
					t.Fatalf("%s after uninstall and install: %+v %v", host, s, err)
				}
			}
		})
	}
}

// Uninstall removes a Codex registration whose marketplace folder is gone:
// its preview says so, and --apply removes the plugin and the marketplace.
func TestUninstallRemovesARegistrationWhoseFolderIsGone(t *testing.T) {
	opts, f := setup(t)
	opts.Host = "codex"
	if _, err := runInstall(context.Background(), opts, f.run); err != nil {
		t.Fatal(err)
	}
	if err := os.RemoveAll(opts.Dirs.Share); err != nil {
		t.Fatal(err)
	}
	f.calls = nil
	preview, err := uninstall(context.Background(), opts.Dirs, false, f.run)
	if err != nil || !slices.Contains(preview.Actions, "codex: the eva-md plugin and its marketplace registration, whose folder is missing") || len(mutateCalls(f.calls)) != 0 {
		t.Fatalf("preview: %+v %v %v", preview.Actions, err, f.calls)
	}
	if _, err = uninstall(context.Background(), opts.Dirs, true, f.run); err != nil {
		t.Fatal(err)
	}
	if want := []string{"codex plugin remove eva-md@eva-md --json", "codex plugin marketplace remove eva-md --json"}; !slices.Equal(mutateCalls(f.calls), want) {
		t.Errorf("removals %v, want %v", mutateCalls(f.calls), want)
	}
	if f.installed["codex"] || f.markets["codex"] {
		t.Fatalf("still registered: %+v %+v", f.installed, f.markets)
	}
	// With the folder there, a marketplace without the plugin loses only the
	// marketplace.
	opts, f = setup(t)
	opts.Host = "codex"
	if _, err := runInstall(context.Background(), opts, f.run); err != nil {
		t.Fatal(err)
	}
	delete(f.installed, "codex")
	f.calls = nil
	if _, err := uninstall(context.Background(), opts.Dirs, true, f.run); err != nil {
		t.Fatal(err)
	}
	if want := []string{"codex plugin marketplace remove eva-md --json"}; !slices.Equal(mutateCalls(f.calls), want) {
		t.Errorf("removals %v, want %v", mutateCalls(f.calls), want)
	}
}

// Only a manifest missing from eva-md's own folder is taken for it: Codex
// naming another folder, or another marketplace, is a failure as before. The
// folder is matched as given and with its symlinks resolved, as Codex keeps
// it.
func TestOnlyEvaMDsOwnFolderIsTakenForMissing(t *testing.T) {
	opts, f := setup(t)
	path := marketplacePath(opts.Dirs.Share, "codex")
	for _, other := range []string{"/elsewhere/marketplace", path + "-old"} {
		failing := func(ctx context.Context, host string, args ...string) ([]byte, error) {
			if host == "codex" && strings.Join(args, " ") == "plugin marketplace list --json" {
				return nil, missingFolder("codex plugin marketplace list --json", other)
			}
			return f.run(ctx, host, args...)
		}
		if s, err := inspect(context.Background(), opts.Dirs, "codex", failing); err == nil || s.FolderMissing {
			t.Errorf("%s taken for eva-md's folder: %+v %v", other, s, err)
		}
	}
	another := &hostError{msg: "failed", diagnostics: "- `other` at " + path + ": marketplace root does not contain a supported manifest"}
	if folderMissing(another, path) {
		t.Error("another marketplace taken for eva-md's")
	}
	// eva-md's folder alone: with another marketplace failing too, or none
	// named, it is a failure as before.
	both := &hostError{msg: "failed", diagnostics: "Error: failed to load marketplace(s):\n- `eva-md` at " + path + ": marketplace root does not contain a supported manifest\n- `other` at /other: marketplace root does not contain a supported manifest\n"}
	if folderMissing(both, path) {
		t.Error("taken for eva-md's folder alone while another marketplace fails too")
	}
	if folderMissing(&hostError{msg: "failed", diagnostics: "Error: something else failed"}, path) {
		t.Error("a failure naming no marketplace taken for eva-md's folder")
	}
	if !folderMissing(missingFolder("list", path), path) {
		t.Error("eva-md's own folder not matched")
	}
	real := filepath.Join(t.TempDir(), "real")
	if err := os.Mkdir(real, 0o700); err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(t.TempDir(), "link")
	if err := os.Symlink(real, link); err != nil {
		t.Fatal(err)
	}
	resolved, err := filepath.EvalSymlinks(real)
	if err != nil {
		t.Fatal(err)
	}
	if !folderMissing(missingFolder("list", filepath.Join(resolved, "marketplace", "codex")), filepath.Join(link, "marketplace", "codex")) {
		t.Error("a folder Codex names with its symlinks resolved is not matched")
	}
}

func TestRollbackReaddsPluginAFailedUpgradeRemoved(t *testing.T) {
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
	// The upgrade's install removes the old plugin, then fails.
	failed := false
	run := func(ctx context.Context, host string, args ...string) ([]byte, error) {
		if !failed && host+" "+strings.Join(args, " ") == "claude "+strings.Join(addArgs("claude"), " ") {
			failed = true
			delete(f.installed, "claude")
			return nil, errors.New("synthetic failure after removal")
		}
		return f.run(ctx, host, args...)
	}
	_, err = runInstall(context.Background(), opts, run)
	if err == nil || strings.Contains(err.Error(), "rollback incomplete") {
		t.Fatalf("expected fully rolled back failure: %v", err)
	}
	got, _ := os.ReadFile(path)
	if !f.installed["claude"] || !bytes.Equal(got, old) {
		t.Fatal("old plugin was not re-added")
	}
}

func TestInterruptRollsBackInstall(t *testing.T) {
	opts, f := setup(t)
	// Keep the test process alive if install does not catch the signal.
	caught := make(chan os.Signal, 1)
	signal.Notify(caught, syscall.SIGTERM)
	defer signal.Stop(caught)
	var install context.Context
	run := func(ctx context.Context, host string, args ...string) ([]byte, error) {
		if strings.HasPrefix(host+" "+strings.Join(args, " "), "codex plugin add ") {
			// The command that has started still finishes, once install has
			// noticed the signal; the signal then fails the next step.
			_ = syscall.Kill(os.Getpid(), syscall.SIGTERM)
			select {
			case <-install.Done():
			case <-time.After(time.Minute):
				t.Error("install still hasn't noticed the SIGTERM a minute after it")
			}
		}
		return f.run(ctx, host, args...)
	}
	_, err := runInstall(context.WithValue(context.Background(), installContext{}, &install), opts, run)
	if err == nil || strings.Contains(err.Error(), "rollback incomplete") {
		t.Fatalf("expected interrupted install to roll back: %v", err)
	}
	if len(f.installed) != 0 || len(f.markets) != 0 {
		t.Fatalf("registration remains: %+v %+v", f.installed, f.markets)
	}
	if _, err = os.Stat(filepath.Join(opts.Dirs.Share, "bin", "eva-md")); !errors.Is(err, os.ErrNotExist) {
		t.Fatal("new binary remains")
	}
}

// A host command that has started runs to its end when install is
// interrupted, and no command starts after that.
func TestFinishStartedLetsARunningCommandFinish(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	run := finishStarted(func(ctx context.Context, host string, args ...string) ([]byte, error) {
		// The interrupt comes while the command runs.
		cancel()
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		return []byte("done"), nil
	})
	if out, err := run(ctx, "codex", "plugin", "add", "eva-md@eva-md"); err != nil || string(out) != "done" {
		t.Fatalf("running command: %q %v", out, err)
	}
	if _, err := run(ctx, "codex", "plugin", "list", "--json"); err == nil {
		t.Fatal("a command started after the interrupt")
	}
}

func TestUninstallWithoutInstallFolderRemovesRegistrations(t *testing.T) {
	for _, scenario := range []string{"no-folder", "no-marker"} {
		t.Run(scenario, func(t *testing.T) {
			opts, f := setup(t)
			if scenario == "no-marker" {
				if err := os.MkdirAll(opts.Dirs.Share, 0o700); err != nil {
					t.Fatal(err)
				}
			}
			for _, host := range []string{"claude", "codex"} {
				f.installed[host], f.markets[host] = true, true
			}
			preview, err := uninstall(context.Background(), opts.Dirs, false, f.run)
			if err != nil || len(preview.Hosts) != 2 || len(mutateCalls(f.calls)) != 0 {
				t.Fatalf("preview %+v %v %+v", preview, err, f.calls)
			}
			if _, err = uninstall(context.Background(), opts.Dirs, true, f.run); err != nil {
				t.Fatal(err)
			}
			if len(f.installed) != 0 || len(f.markets) != 0 {
				t.Fatalf("host still registered: %+v %+v", f.installed, f.markets)
			}
			// Only eva-md's own plugin and marketplace names are removed.
			for _, call := range mutateCalls(f.calls) {
				if !strings.Contains(call, " eva-md@eva-md ") && !strings.Contains(call, " marketplace remove eva-md ") {
					t.Fatalf("unexpected host change: %s", call)
				}
			}
		})
	}
}

func TestOSMetadataFilesAreIgnoredAndCleaned(t *testing.T) {
	opts, f := setup(t)
	if _, err := runInstall(context.Background(), opts, f.run); err != nil {
		t.Fatal(err)
	}
	plugin := pluginPath(opts.Dirs.Share, "codex")
	stray := []string{filepath.Join(plugin, ".DS_Store"), filepath.Join(plugin, "hooks", "._hooks.json")}
	for _, path := range stray {
		if err := os.WriteFile(path, []byte("finder"), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	opts.DryRun = true
	if _, err := runInstall(context.Background(), opts, f.run); err != nil {
		t.Fatalf("metadata file blocked a preview: %v", err)
	}
	opts.DryRun = false
	f.calls = nil
	if _, err := runInstall(context.Background(), opts, f.run); err != nil {
		t.Fatalf("metadata file blocked install: %v", err)
	}
	if len(mutateCalls(f.calls)) != 0 {
		t.Fatalf("metadata file changed registrations: %+v", f.calls)
	}
	for _, path := range stray {
		if _, err := os.Stat(path); !errors.Is(err, os.ErrNotExist) {
			t.Fatalf("metadata file kept: %s", path)
		}
	}
	// Other unknown files still block, and the error names them.
	other := filepath.Join(plugin, "notes.txt")
	if err := os.WriteFile(other, []byte("user content"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := runInstall(context.Background(), opts, f.run); err == nil || !strings.Contains(err.Error(), "notes.txt") {
		t.Fatalf("unknown file error does not name it: %v", err)
	}
	if err := os.Remove(other); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(stray[0], []byte("finder"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := uninstall(context.Background(), opts.Dirs, true, f.run); err != nil {
		t.Fatalf("metadata file blocked uninstall: %v", err)
	}
}

func TestInstallThroughSymlinkedExecutable(t *testing.T) {
	opts, f := setup(t)
	link := filepath.Join(t.TempDir(), "eva-md")
	if err := os.Symlink(opts.Binary, link); err != nil {
		t.Fatal(err)
	}
	opts.Binary = link
	result, err := runInstall(context.Background(), opts, f.run)
	if err != nil {
		t.Fatalf("symlinked executable: %v", err)
	}
	if data, _ := os.ReadFile(result.Binary); string(data) != "binary version 1" {
		t.Fatal("binary not copied from the link target")
	}
}

func TestUninstallRemovesEmptyFolders(t *testing.T) {
	opts, f := setup(t)
	if _, err := runInstall(context.Background(), opts, f.run); err != nil {
		t.Fatal(err)
	}
	if _, err := uninstall(context.Background(), opts.Dirs, true, f.run); err != nil {
		t.Fatal(err)
	}
	// The install folder keeps only the record of what is left.
	if entries, err := os.ReadDir(opts.Dirs.Share); err != nil || len(entries) != 1 || entries[0].Name() != RecordFile {
		t.Fatalf("the install folder holds %v: %v", entries, err)
	}
	if _, err := os.Stat(filepath.Dir(opts.Dirs.Share)); err != nil {
		t.Fatalf("removed a folder eva-md does not own: %v", err)
	}
}
