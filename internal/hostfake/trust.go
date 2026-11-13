package hostfake

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"sort"
	"strconv"
	"strings"
)

// The trust words hooks/list gives.
const (
	Trusted   = "trusted"
	Managed   = "managed"
	Modified  = "modified"
	Untrusted = "untrusted"
)

// hooksFile is where a plugin declares its hooks, relative to its folder.
const hooksFile = "hooks/hooks.json"

// loaded is a hook as a CLI loaded it: from a plugin's folder, or an
// administrator's.
type loaded struct {
	Hook
	// Key is the positional key Codex stores its approval under:
	// <plugin>@<marketplace>:hooks/hooks.json:<event>:<group>:<handler>.
	Key     string
	Plugin  plugin
	Managed bool
	// Root is the folder the hook is loaded from, and Data its plugin's
	// data folder.
	Root, Data string
}

// CodexContentHash is the hash Codex binds a plugin hook's approval to: a
// SHA-256 over every file under dir, in order of its slash-separated path,
// each as its path, its size and its bytes. An entry named __pycache__ or
// .venv, or ending in .pyc or .egg-info, is passed over with all it holds. A
// symlink, or any entry but a folder or a regular file, fails the hash.
func CodexContentHash(dir string) (string, error) {
	type file struct {
		rel  string
		path string
	}
	var files []file
	err := filepath.WalkDir(dir, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if path == dir {
			return nil
		}
		if skipped(d.Name()) {
			if d.IsDir() {
				return filepath.SkipDir
			}
			return nil
		}
		rel, err := filepath.Rel(dir, path)
		if err != nil {
			return err
		}
		switch {
		case d.IsDir():
		case d.Type().IsRegular():
			files = append(files, file{rel: filepath.ToSlash(rel), path: path})
		default:
			return fmt.Errorf("unsupported plugin entry type: %s", filepath.ToSlash(rel))
		}
		return nil
	})
	if err != nil {
		return "", err
	}
	sort.Slice(files, func(i, j int) bool { return files[i].rel < files[j].rel })
	h := sha256.New()
	for _, f := range files {
		data, err := os.ReadFile(f.path)
		if err != nil {
			return "", err
		}
		h.Write([]byte(f.rel))
		h.Write([]byte{0})
		h.Write([]byte(strconv.Itoa(len(data))))
		h.Write([]byte{0})
		h.Write(data)
	}
	return "sha256:" + hex.EncodeToString(h.Sum(nil)), nil
}

func skipped(name string) bool {
	return name == "__pycache__" || name == ".venv" || strings.HasSuffix(name, ".pyc") || strings.HasSuffix(name, ".egg-info")
}

// declHash is the hash Codex binds a hook's approval to in every version:
// its event, matcher, command, timeout, async, statusMessage and a context
// limit that isn't the default.
func declHash(h Hook) string {
	data, _ := json.Marshal(struct {
		Event         string `json:"event"`
		Matcher       string `json:"matcher"`
		Command       string `json:"command"`
		Timeout       int    `json:"timeout"`
		Async         bool   `json:"async"`
		StatusMessage string `json:"statusMessage"`
		ContextLimit  int    `json:"contextLimit,omitempty"`
	}{h.Event, h.Matcher, h.Command, h.Timeout, h.Async, h.StatusMessage, h.ContextLimit})
	sum := sha256.Sum256(data)
	return "sha256:" + hex.EncodeToString(sum[:])
}

// pluginHooks are the hooks a CLI loads from an installed plugin: Codex from
// its copy, and only when the copy has a .codex-plugin/plugin.json; Claude
// Code from the plugin's own folder, read again each time.
func (f *Fake) pluginHooks(cli string, p plugin) ([]loaded, error) {
	root := p.Source
	if cli == Codex {
		root = f.CachePath(p.Market, p.Name)
		if _, err := os.Stat(filepath.Join(root, ".codex-plugin", "plugin.json")); err != nil {
			return nil, nil
		}
	}
	data, err := os.ReadFile(filepath.Join(root, filepath.FromSlash(hooksFile)))
	if errors.Is(err, fs.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	var file struct {
		Hooks map[string][]struct {
			Matcher string `json:"matcher"`
			Hooks   []struct {
				Type          string   `json:"type"`
				Command       string   `json:"command"`
				Args          []string `json:"args"`
				Timeout       int      `json:"timeout"`
				Async         bool     `json:"async"`
				StatusMessage string   `json:"statusMessage"`
				ContextLimit  int      `json:"additionalContextLimit"`
			} `json:"hooks"`
		} `json:"hooks"`
	}
	if err := json.Unmarshal(data, &file); err != nil {
		return nil, fmt.Errorf("%s of %s: %w", hooksFile, p.id(), err)
	}
	var hooks []loaded
	events := make([]string, 0, len(file.Hooks))
	for event := range file.Hooks {
		events = append(events, event)
	}
	sort.Strings(events)
	for _, event := range events {
		for g, group := range file.Hooks[event] {
			for i, h := range group.Hooks {
				hooks = append(hooks, loaded{
					Hook: Hook{Event: event, Matcher: group.Matcher, Command: h.Command, Args: h.Args, Timeout: h.Timeout,
						Async: h.Async, StatusMessage: h.StatusMessage, ContextLimit: h.ContextLimit},
					Key:    fmt.Sprintf("%s:%s:%s:%d:%d", p.id(), hooksFile, event, g, i),
					Plugin: p,
					Root:   root,
					Data:   f.dataPath(cli, p),
				})
			}
		}
	}
	return hooks, nil
}

// all is every hook cli has: the managed ones, then each installed plugin's
// in order of its id. A plugin whose hooks file can't be read adds an error.
func (f *Fake) all(st *state, cli string) ([]loaded, []error) {
	var hooks []loaded
	for i, h := range st.Managed[cli] {
		hooks = append(hooks, loaded{Hook: h, Key: fmt.Sprintf("managed:%s:%d", h.Event, i), Managed: true, Root: "/"})
	}
	plugins := slices.Clone(st.Plugins[cli])
	sort.Slice(plugins, func(i, j int) bool { return plugins[i].id() < plugins[j].id() })
	var errs []error
	for _, p := range plugins {
		ph, err := f.pluginHooks(cli, p)
		if err != nil {
			errs = append(errs, err)
			continue
		}
		hooks = append(hooks, ph...)
	}
	return hooks, errs
}

// blocked says why policy keeps a hook from loading, or "".
func blocked(st *state, cli string, h loaded) string {
	p := st.Policies[cli]
	if cli == Codex && p.CodexHooksOff {
		return "hooks are turned off by policy"
	}
	if h.Managed {
		return ""
	}
	switch {
	case cli == Codex && p.CodexPluginsOff:
		return "plugins are turned off by policy"
	case cli == Codex && p.CodexManagedHooksOnly, cli == Claude && p.ClaudeManagedHooksOnly:
		return "only managed hooks run, by policy"
	case p.BlockedMarketplace != "" && h.Plugin.Market == p.BlockedMarketplace:
		return "marketplace " + h.Plugin.Market + " is blocked by policy"
	}
	return ""
}

// contentBound says the Codex version binds approval to a plugin's contents.
func contentBound(st *state) bool {
	return versionAtLeast(st.version(Codex), contentBoundSince)
}

// contents hashes each plugin folder once.
type contents map[string]struct {
	hash string
	err  error
}

func (c contents) of(root string) (string, error) {
	if v, ok := c[root]; ok {
		return v.hash, v.err
	}
	hash, err := CodexContentHash(root)
	c[root] = struct {
		hash string
		err  error
	}{hash, err}
	return hash, err
}

// trust is a Codex hook's trust word, and when it would not run, why: a
// skip ("untrusted" or "modified" by its declaration) or a failure (its
// plugin's contents changed, or can't be hashed).
func trust(st *state, h loaded, c contents) (word, skip, failure string) {
	if h.Managed {
		return Managed, "", ""
	}
	rec, ok := st.Trust[h.Key]
	if !ok {
		return Untrusted, "hook needs review: untrusted", ""
	}
	if rec.Decl != declHash(h.Hook) {
		return Modified, "hook needs review: its declaration changed", ""
	}
	if !contentBound(st) {
		return Trusted, "", ""
	}
	hash, err := c.of(h.Root)
	if err != nil {
		return Modified, "", "plugin contents could not be hashed: " + err.Error()
	}
	if hash != rec.Content {
		return Modified, "", "plugin contents changed; hook approval required"
	}
	return Trusted, "", ""
}

// hooksList is Codex's hooks/list result: for each folder asked, the hooks
// that load, with their trust word.
func (f *Fake) hooksList(cwds []string) (any, error) {
	if len(cwds) == 0 {
		cwds = []string{""}
	}
	var result map[string]any
	err := f.withState(false, func(st *state) error {
		hooks, errs := f.all(st, Codex)
		list := []any{}
		c := contents{}
		for _, h := range hooks {
			if blocked(st, Codex, h) != "" {
				continue
			}
			word, _, _ := trust(st, h, c)
			entry := map[string]any{"key": h.Key, "eventName": camel(h.Event), "matcher": h.Matcher, "enabled": true, "trustStatus": word, "source": "plugin", "pluginId": h.Plugin.id()}
			if h.Managed {
				entry["source"], entry["pluginId"] = "managed", ""
			}
			list = append(list, entry)
		}
		problems := []any{}
		for _, err := range errs {
			problems = append(problems, map[string]any{"message": err.Error()})
		}
		var data []any
		for _, cwd := range cwds {
			data = append(data, map[string]any{"cwd": cwd, "hooks": list, "errors": problems, "warnings": []any{}})
		}
		result = map[string]any{"data": data}
		return nil
	})
	return result, err
}

// camel is an event as the app server's wire spells it: SessionStart is
// sessionStart.
func camel(event string) string {
	if event == "" {
		return event
	}
	return strings.ToLower(event[:1]) + event[1:]
}

// Approve stands for the person's review of cli's hooks in the host: on
// Codex, each hook of each installed plugin is approved as its copy is now,
// its declaration and its plugin's contents. It fails, approving nothing,
// when a copy's contents can't be hashed. Claude Code asks for no approval,
// so there it does nothing. No command the fake answers approves.
func (f *Fake) Approve(cli string) error {
	if err := checkCLI(cli); err != nil {
		return err
	}
	if cli == Claude {
		return nil
	}
	return f.withState(true, func(st *state) error {
		hooks, errs := f.all(st, Codex)
		if len(errs) > 0 {
			return errs[0]
		}
		c := contents{}
		records := map[string]trustRecord{}
		for _, h := range hooks {
			if h.Managed {
				continue
			}
			hash, err := c.of(h.Root)
			if err != nil {
				return fmt.Errorf("hostfake: approve %s: %w", h.Plugin.id(), err)
			}
			records[h.Key] = trustRecord{Decl: declHash(h.Hook), Content: hash}
		}
		for key, rec := range records {
			st.Trust[key] = rec
		}
		return nil
	})
}

// parseVersion reads N.N.N.
func parseVersion(s string) ([3]int, bool) {
	var v [3]int
	parts := strings.Split(s, ".")
	if len(parts) != 3 {
		return v, false
	}
	for i, p := range parts {
		n, err := strconv.Atoi(p)
		if err != nil || p == "" || strings.Trim(p, "0123456789") != "" {
			return v, false
		}
		v[i] = n
	}
	return v, true
}

// versionAtLeast says v is min or newer. A version that can't be read is
// taken as the newest.
func versionAtLeast(v, min string) bool {
	a, ok := parseVersion(v)
	if !ok {
		return true
	}
	b, _ := parseVersion(min)
	for i := range a {
		if a[i] != b[i] {
			return a[i] > b[i]
		}
	}
	return true
}
