package hostfake

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"slices"
	"sort"
	"strconv"
	"strings"
)

// ExitError is a fake CLI's failure: the exit code it ended with and what it
// printed on stderr, as a written CLI ends.
type ExitError struct {
	Command string
	Code    int
	Stderr  string
}

func (e *ExitError) Error() string {
	return "hostfake: " + e.Command + ": exit status " + strconv.Itoa(e.Code)
}

// Run answers one command of cli, claude or codex, or a path whose last
// element is one, as install's runner does, with nothing on stdin. It gives
// what the command printed on stdout, and on a failure an *ExitError, or for
// a stalled command an error that wraps its context's. It fails as the
// written CLI does, not as a caller's runner does: a caller that reads a
// host's wording from its own error type, as install reads Codex's
// missing-folder failure from its hostError, maps an *ExitError to that type
// first, or it loses what it reads (see the package documentation).
func (f *Fake) Run(ctx context.Context, cli string, args ...string) ([]byte, error) {
	return f.RunStdin(ctx, nil, cli, args...)
}

// RunStdin is Run with stdin, as codex app-server reads its requests there.
func (f *Fake) RunStdin(ctx context.Context, stdin []byte, cli string, args ...string) ([]byte, error) {
	var stdout, stderr bytes.Buffer
	code, err := f.serve(ctx, bytes.NewReader(stdin), &stdout, &stderr, filepath.Base(cli), args)
	if err != nil {
		return stdout.Bytes(), err
	}
	if code != 0 {
		return stdout.Bytes(), &ExitError{Command: Call{CLI: filepath.Base(cli), Args: args}.String(), Code: code, Stderr: stderr.String()}
	}
	return stdout.Bytes(), nil
}

// stallNotice is what a stalled command prints on stderr before it waits.
const stallNotice = "hostfake: stalled until the context ends\n"

// serve answers one command, Run's and a written CLI's alike. It records
// the command, applies a fault, and answers. err is set only when a stalled
// command's context ends.
func (f *Fake) serve(ctx context.Context, stdin io.Reader, stdout, stderr io.Writer, cli string, args []string) (code int, err error) {
	if err := checkCLI(cli); err != nil {
		fmt.Fprintln(stderr, err)
		return 127, nil
	}
	command := Call{CLI: cli, Args: args}.String()
	var out, diag bytes.Buffer
	var when When
	appServer := cli == Codex && slices.Equal(stripNoDaemon(args), []string{"app-server", "--stdio"})
	err = f.withState(true, func(st *state) error {
		call := Call{CLI: cli, Args: slices.Clone(args)}
		if slices.Contains(args, "--dangerously-bypass-hook-trust") {
			call.Refused = true
			st.Calls = append(st.Calls, call)
			fmt.Fprintln(&diag, "hostfake: refused: --dangerously-bypass-hook-trust is never used, and grants no trust here")
			code = 2
			return nil
		}
		st.Calls = append(st.Calls, call)
		for i, ft := range st.Faults {
			if strings.HasPrefix(command, ft.Prefix) {
				when = ft.When
				st.Faults = slices.Delete(st.Faults, i, i+1)
				break
			}
		}
		if when == Before {
			fmt.Fprintf(&diag, "hostfake: %s failed before it took effect\n", command)
			code = 1
			return nil
		}
		if when == Stall || appServer {
			return nil
		}
		code = f.answer(st, cli, args, &out, &diag)
		if when == After && code == 0 {
			out.Reset()
			fmt.Fprintf(&diag, "hostfake: %s failed after it took effect\n", command)
			code = 1
		}
		return nil
	})
	if err != nil {
		fmt.Fprintln(stderr, "hostfake:", err)
		return 1, nil
	}
	if when == Stall {
		io.WriteString(stderr, stallNotice)
		if f.onStall != nil {
			f.onStall()
		}
		<-ctx.Done()
		return 1, fmt.Errorf("hostfake: %s stalled: %w", command, ctx.Err())
	}
	if appServer && code == 0 {
		code = f.appServer(stdin, stdout)
		if when == After && code == 0 {
			fmt.Fprintf(stderr, "hostfake: %s failed after it took effect\n", command)
			code = 1
		}
		return code, nil
	}
	stdout.Write(out.Bytes())
	stderr.Write(diag.Bytes())
	return code, nil
}

func stripNoDaemon(args []string) []string {
	if len(args) > 0 && args[0] == "--no-daemon" {
		return args[1:]
	}
	return args
}

// cmdArgs are a command's words apart from its flags, and its flags.
type cmdArgs struct {
	words []string
	json  bool
	// strict is --strict; scope is --scope's value, and market
	// --marketplace='s.
	strict        bool
	scope, market string
	hasScope      bool
}

func parseArgs(args []string) (cmdArgs, error) {
	var a cmdArgs
	for i := 0; i < len(args); i++ {
		arg := args[i]
		switch {
		case arg == "--json":
			a.json = true
		case arg == "--strict":
			a.strict = true
		case arg == "--scope":
			if i+1 >= len(args) {
				return a, errors.New("--scope needs a value")
			}
			i++
			a.scope, a.hasScope = args[i], true
		case strings.HasPrefix(arg, "--marketplace="):
			a.market = strings.TrimPrefix(arg, "--marketplace=")
		case strings.HasPrefix(arg, "-") && arg != "--version":
			return a, fmt.Errorf("unknown option %s", arg)
		default:
			a.words = append(a.words, arg)
		}
	}
	return a, nil
}

// answer runs one command against the state. It returns the exit code.
func (f *Fake) answer(st *state, cli string, args []string, out, diag *bytes.Buffer) int {
	if cli == Codex {
		args = stripNoDaemon(args)
	}
	if cli == Claude && slices.Contains(args, "-p") || cli == Codex && len(args) > 0 && args[0] == "exec" {
		fmt.Fprintf(diag, "hostfake: %s runs no model\n", cli)
		return 2
	}
	a, err := parseArgs(args)
	if err != nil {
		fmt.Fprintf(diag, "error: %v\n", err)
		return 2
	}
	words := strings.Join(a.words, " ")
	fail := func(format string, v ...any) int {
		fmt.Fprintf(diag, "Error: "+format+"\n", v...)
		return 1
	}
	emit := func(v any) int {
		data, err := json.Marshal(v)
		if err != nil {
			return fail("%v", err)
		}
		out.Write(append(data, '\n'))
		return 0
	}
	policy := st.Policies[cli]
	switch {
	case words == "--version" && len(args) == 1:
		if cli == Codex {
			fmt.Fprintf(out, "codex-cli %s\n", st.version(cli))
		} else {
			fmt.Fprintf(out, "%s (Claude Code)\n", st.version(cli))
		}
		return 0
	case cli == Codex && words == "features list":
		stage := "experimental"
		if versionAtLeast(st.version(cli), "0.167.0") {
			stage = "stable"
		}
		fmt.Fprintf(out, "apps experimental false\nhooks %s %t\nplugins stable %t\n", stage, !policy.CodexHooksOff, !policy.CodexPluginsOff)
		return 0
	case words == "plugin marketplace list" && a.json:
		if cli == Codex {
			if msg := missingFolders(st, cli, ""); msg != "" {
				diag.WriteString(msg)
				return 1
			}
		}
		list := []any{}
		for _, m := range st.Markets[cli] {
			list = append(list, map[string]any{"name": m.Name, "root": m.Path, "installLocation": m.Path})
		}
		if cli == Codex {
			return emit(map[string]any{"marketplaces": list})
		}
		return emit(list)
	case len(a.words) == 4 && words == "plugin marketplace add "+a.words[3] && a.json:
		path := a.words[3]
		if !filepath.IsAbs(path) {
			return fail("marketplace source %s is not an absolute path", path)
		}
		path = filepath.Clean(path)
		if cli == Codex && policy.CodexPluginsOff {
			return fail("plugins are turned off by your administrator")
		}
		manifest, err := readMarketplace(cli, path)
		if err != nil {
			return fail("%s: marketplace root does not contain a supported manifest", path)
		}
		if manifest.Name == policy.BlockedMarketplace && policy.BlockedMarketplace != "" {
			return fail("marketplace %s is blocked by your administrator", manifest.Name)
		}
		if m, ok := st.market(cli, manifest.Name); ok {
			if m.Path != path {
				return fail("marketplace %s is already added from another folder", manifest.Name)
			}
			return emit(map[string]any{"name": manifest.Name, "added": false})
		}
		st.Markets[cli] = append(st.Markets[cli], market{Name: manifest.Name, Path: path, Real: resolved(path)})
		return emit(map[string]any{"name": manifest.Name, "added": true})
	case len(a.words) == 4 && words == "plugin marketplace remove "+a.words[3] && a.json:
		name := a.words[3]
		if _, ok := st.market(cli, name); !ok {
			return fail("marketplace %s is not added", name)
		}
		// Claude Code 2.1.316 keeps listing a marketplace removed with
		// --scope while another scope declares it.
		if cli == Claude && a.hasScope {
			return emit(map[string]any{"name": name, "removed": true})
		}
		st.Markets[cli] = slices.DeleteFunc(st.Markets[cli], func(m market) bool { return m.Name == name })
		return emit(map[string]any{"name": name, "removed": true})
	case cli == Claude && len(a.words) == 3 && words == "plugin validate "+a.words[2] && a.json && a.strict:
		path := a.words[2]
		for _, name := range []string{".claude-plugin/marketplace.json", ".claude-plugin/plugin.json"} {
			var v map[string]any
			if data, err := os.ReadFile(filepath.Join(path, filepath.FromSlash(name))); err == nil && json.Unmarshal(data, &v) == nil {
				return emit(map[string]any{"success": true})
			}
		}
		out.WriteString("{\"success\":false}\n")
		return 1
	case (cli == Codex && len(a.words) == 3 && words == "plugin add "+a.words[2] && a.json) ||
		(cli == Claude && len(a.words) == 3 && words == "plugin install "+a.words[2] && a.json && a.scope == "user"):
		return f.add(st, cli, a.words[2], policy, fail, emit)
	case (cli == Codex && len(a.words) == 3 && words == "plugin remove "+a.words[2] && a.json) ||
		(cli == Claude && len(a.words) == 3 && words == "plugin uninstall "+a.words[2] && a.json && a.scope == "user"):
		id := a.words[2]
		if p, ok := st.plugin(cli, id); ok && cli == Codex {
			if err := os.RemoveAll(f.CachePath(p.Market, p.Name)); err != nil {
				return fail("%v", err)
			}
		}
		st.Plugins[cli] = slices.DeleteFunc(st.Plugins[cli], func(p plugin) bool { return p.id() == id })
		return emit(map[string]any{"pluginId": id, "removed": true})
	case words == "plugin list" && a.json:
		return f.list(st, cli, a.market, policy, diag, emit)
	}
	fmt.Fprintf(diag, "hostfake: %s does not answer %q\n", cli, strings.Join(args, " "))
	return 2
}

// add installs a plugin from a registered marketplace. Codex copies it into
// its cache and loads the copy; Claude Code loads it in place, and refuses a
// plugin it has.
func (f *Fake) add(st *state, cli, id string, policy Policy, fail func(string, ...any) int, emit func(any) int) int {
	if cli == Codex && policy.CodexPluginsOff {
		return fail("plugins are turned off by your administrator")
	}
	name, marketName, ok := strings.Cut(id, "@")
	if !ok || name == "" || marketName == "" {
		return fail("plugin %s is not <plugin>@<marketplace>", id)
	}
	if marketName == policy.BlockedMarketplace {
		return fail("marketplace %s is blocked by your administrator", marketName)
	}
	m, ok := st.market(cli, marketName)
	if !ok {
		return fail("marketplace %s is not added", marketName)
	}
	if _, ok := st.plugin(cli, id); ok && cli == Claude {
		return fail("plugin %s is already installed", id)
	}
	manifest, err := readMarketplace(cli, m.Path)
	if err != nil {
		if cli == Codex {
			return fail("failed to load marketplace(s):\n%s", strings.TrimSuffix(missingFolderLine(m), "\n"))
		}
		return fail("marketplace %s has no manifest", marketName)
	}
	source := ""
	for _, p := range manifest.Plugins {
		if p.Name == name {
			source = filepath.Join(m.Path, filepath.FromSlash(p.Source))
		}
	}
	if source == "" {
		return fail("plugin %s is not in marketplace %s", name, marketName)
	}
	if info, err := os.Stat(source); err != nil || !info.IsDir() {
		return fail("plugin %s has no folder", id)
	}
	if cli == Codex {
		cache := f.CachePath(marketName, name)
		if err := os.RemoveAll(cache); err != nil {
			return fail("%v", err)
		}
		if err := copyTree(source, cache); err != nil {
			return fail("copy plugin %s: %v", id, err)
		}
	}
	st.Plugins[cli] = slices.DeleteFunc(st.Plugins[cli], func(p plugin) bool { return p.id() == id })
	st.Plugins[cli] = append(st.Plugins[cli], plugin{Name: name, Market: marketName, Source: source})
	return emit(map[string]any{"pluginId": id, "installed": true})
}

// list is a CLI's plugin list: Codex's, one marketplace's or all, as an
// object; Claude Code's as a list.
func (f *Fake) list(st *state, cli, only string, policy Policy, diag *bytes.Buffer, emit func(any) int) int {
	if cli == Codex {
		if msg := missingFolders(st, cli, only); msg != "" {
			diag.WriteString(msg)
			return 1
		}
	}
	installed := []any{}
	plugins := slices.Clone(st.Plugins[cli])
	sort.Slice(plugins, func(i, j int) bool { return plugins[i].id() < plugins[j].id() })
	for _, p := range plugins {
		if only != "" && p.Market != only {
			continue
		}
		if cli == Codex && policy.CodexPluginsOff {
			continue
		}
		enabled := p.Market != policy.BlockedMarketplace
		if cli == Codex {
			installed = append(installed, map[string]any{"pluginId": p.id(), "name": p.Name, "marketplaceName": p.Market,
				"enabled": enabled, "source": map[string]any{"source": "local", "path": p.Source}})
			continue
		}
		entry := map[string]any{"id": p.id(), "pluginId": p.id(), "name": p.Name, "marketplace": p.Market, "scope": "user",
			"enabled": enabled, "readFromFolder": p.Source, "folderVersion": manifestVersion(p.Source), "errors": pluginErrors(p.Source)}
		if policy.ClaudeManagedHooksOnly {
			entry["hooksBlockedBy"] = "allowManagedHooksOnly"
		}
		installed = append(installed, entry)
	}
	if cli == Claude {
		return emit(installed)
	}
	available := []any{}
	for _, m := range st.Markets[cli] {
		if only != "" && m.Name != only || policy.CodexPluginsOff {
			continue
		}
		manifest, _ := readMarketplace(cli, m.Path)
		for _, p := range manifest.Plugins {
			if _, ok := st.plugin(cli, p.Name+"@"+m.Name); !ok {
				available = append(available, map[string]any{"pluginId": p.Name + "@" + m.Name, "name": p.Name, "marketplaceName": m.Name})
			}
		}
	}
	return emit(map[string]any{"installed": installed, "available": available})
}

// marketplaceManifest is the part of a marketplace's manifest the fake
// reads: its name, and each plugin's name and folder.
type marketplaceManifest struct {
	Name    string
	Plugins []struct {
		Name   string
		Source string
	}
}

func readMarketplace(cli, path string) (marketplaceManifest, error) {
	var m marketplaceManifest
	name := ".claude-plugin/marketplace.json"
	if cli == Codex {
		name = ".agents/plugins/marketplace.json"
	}
	data, err := os.ReadFile(filepath.Join(path, filepath.FromSlash(name)))
	if err != nil {
		return m, err
	}
	var raw struct {
		Name    string `json:"name"`
		Plugins []struct {
			Name   string          `json:"name"`
			Source json.RawMessage `json:"source"`
		} `json:"plugins"`
	}
	if err := json.Unmarshal(data, &raw); err != nil {
		return m, err
	}
	if raw.Name == "" {
		return m, errors.New("marketplace has no name")
	}
	m.Name = raw.Name
	for _, p := range raw.Plugins {
		// Claude Code's source is a path; Codex's an object with one.
		var source string
		if json.Unmarshal(p.Source, &source) != nil {
			var obj struct {
				Path string `json:"path"`
			}
			_ = json.Unmarshal(p.Source, &obj)
			source = obj.Path
		}
		m.Plugins = append(m.Plugins, struct {
			Name   string
			Source string
		}{p.Name, source})
	}
	return m, nil
}

// missingFolders is Codex 0.166.0's failure of its lists while a registered
// marketplace's folder has no manifest, all of them or only the one named,
// each named by its folder as it was added, symlinks resolved.
func missingFolders(st *state, cli, only string) string {
	var lines []string
	for _, m := range st.Markets[cli] {
		if only != "" && m.Name != only {
			continue
		}
		if _, err := readMarketplace(cli, m.Path); err != nil {
			lines = append(lines, missingFolderLine(m))
		}
	}
	if len(lines) == 0 {
		return ""
	}
	return "Error: failed to load marketplace(s):\n" + strings.Join(lines, "")
}

func missingFolderLine(m market) string {
	return "- `" + m.Name + "` at " + m.Real + ": marketplace root does not contain a supported manifest\n"
}

// resolved is path with the symlinks of its deepest folder that exists
// resolved, as Codex names a marketplace's folder.
func resolved(path string) string {
	dir, rest := filepath.Clean(path), ""
	for {
		if real, err := filepath.EvalSymlinks(dir); err == nil {
			return filepath.Join(real, rest)
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			return path
		}
		rest = filepath.Join(filepath.Base(dir), rest)
		dir = parent
	}
}

// manifestVersion is the version a Claude Code plugin's manifest gives, or
// "".
func manifestVersion(folder string) string {
	var v struct {
		Version string `json:"version"`
	}
	data, err := os.ReadFile(filepath.Join(folder, ".claude-plugin", "plugin.json"))
	if err != nil || json.Unmarshal(data, &v) != nil {
		return ""
	}
	return v.Version
}

// pluginErrors are the errors Claude Code lists for a plugin whose folder
// has no manifest it can read.
func pluginErrors(folder string) []any {
	var v map[string]any
	data, err := os.ReadFile(filepath.Join(folder, ".claude-plugin", "plugin.json"))
	if err != nil || json.Unmarshal(data, &v) != nil {
		return []any{"plugin manifest is missing or invalid"}
	}
	return []any{}
}

// appServer answers Codex's app server: JSON-RPC requests, one a line, until
// stdin ends. It answers initialize and hooks/list; other methods get an
// error, and notifications nothing.
func (f *Fake) appServer(stdin io.Reader, stdout io.Writer) int {
	s := bufio.NewScanner(stdin)
	s.Buffer(make([]byte, 4096), 4<<20)
	enc := json.NewEncoder(stdout)
	for s.Scan() {
		var req struct {
			ID     *int64          `json:"id"`
			Method string          `json:"method"`
			Params json.RawMessage `json:"params"`
		}
		if json.Unmarshal(s.Bytes(), &req) != nil || req.ID == nil {
			continue
		}
		var result any
		var rpcErr any
		switch req.Method {
		case "initialize":
			result = map[string]any{"userAgent": "hostfake"}
		case "hooks/list":
			var params struct {
				Cwds []string `json:"cwds"`
			}
			_ = json.Unmarshal(req.Params, &params)
			var err error
			result, err = f.hooksList(params.Cwds)
			if err != nil {
				rpcErr = map[string]any{"code": -32603, "message": "hooks could not be listed"}
			}
		default:
			rpcErr = map[string]any{"code": -32601, "message": "method not found"}
		}
		reply := map[string]any{"id": *req.ID}
		if rpcErr != nil {
			reply["error"] = rpcErr
		} else {
			reply["result"] = result
		}
		if enc.Encode(reply) != nil {
			return 1
		}
	}
	return 0
}

// copyTree copies a folder, its files' modes and its links as they are.
func copyTree(src, dst string) error {
	return filepath.WalkDir(src, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel, err := filepath.Rel(src, path)
		if err != nil {
			return err
		}
		target := filepath.Join(dst, rel)
		info, err := d.Info()
		if err != nil {
			return err
		}
		switch {
		case d.IsDir():
			return os.MkdirAll(target, 0o700)
		case info.Mode()&os.ModeSymlink != 0:
			link, err := os.Readlink(path)
			if err != nil {
				return err
			}
			return os.Symlink(link, target)
		case info.Mode().IsRegular():
			data, err := os.ReadFile(path)
			if err != nil {
				return err
			}
			return os.WriteFile(target, data, info.Mode().Perm())
		}
		return nil
	})
}
