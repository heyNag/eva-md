package main

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"syscall"
	"time"
)

// hostRun is one host command hostrec ran: its exit status, whether it
// asked anything, and its output, which hostrec reads for names and never
// writes to the recording.
type hostRun struct {
	exit   int
	asked  bool
	output string
}

// asking is how a command's output asks a question it could not wait for.
var asking = regexp.MustCompile(`(?im)(\?\s*$|\(y/n\)|\[y/n\]|\[Y/n\]|\[y/N\]|press (enter|return|any key)|are you sure|confirm)`)

// runHost runs a host command with nothing to read and no terminal: its
// standard input is empty and it runs in a session of its own, so a
// question fails rather than waits.
func runHost(env []string, argv []string) hostRun {
	ctx, cancel := context.WithTimeout(context.Background(), hostTimeout)
	defer cancel()
	cmd := exec.CommandContext(ctx, argv[0], argv[1:]...)
	cmd.Env = env
	cmd.Stdin = nil
	cmd.SysProcAttr = &syscall.SysProcAttr{Setsid: true}
	cmd.WaitDelay = time.Second
	var out bytes.Buffer
	cmd.Stdout, cmd.Stderr = &out, &out
	err := cmd.Run()
	r := hostRun{output: out.String()}
	var exitErr *exec.ExitError
	switch {
	case err == nil:
	case errors.As(err, &exitErr):
		r.exit = exitErr.ExitCode()
		if r.exit < 0 {
			r.exit = 255
		}
	default:
		r.exit = 127
	}
	r.asked = asking.MatchString(r.output)
	return r
}

// hostEnv is the environment of a host command: this one's, with the
// host's own folder when --config-dir or --codex-home names another.
func hostEnv(flags map[string]string) []string {
	env := os.Environ()
	if d := flags["config-dir"]; d != "" {
		env = append(env, "CLAUDE_CONFIG_DIR="+d)
	}
	if d := flags["codex-home"]; d != "" {
		env = append(env, "CODEX_HOME="+d)
	}
	return env
}

// pluginsOf are the scratch plugins of Part 1, by host.
var pluginsOf = map[string][]string{hostClaude: {claudePlugin}, hostCodex: {codexPlugin}}

// listCommand is the host's own list of its plugins.
func listCommand(host string) []string {
	if host == hostCodex {
		return []string{cliCodex, "plugin", "list", "--json"}
	}
	return []string{cliClaude, "plugin", "list", "--json"}
}

// listed gives the scratch plugins a host's list names.
func listed(env []string, host string) ([]string, hostRun) {
	r := runHost(env, listCommand(host))
	var have []string
	for _, p := range pluginsOf[host] {
		if strings.Contains(r.output, p+"@"+marketplace) || strings.Contains(r.output, `"`+p+`"`) {
			have = append(have, p)
		}
	}
	return have, r
}

func cmdRegister(c *cli, args []string) int {
	flags, words, err := parseArgs(args, []string{"config-dir", "codex-home"}, []string{"check"})
	if err != nil || len(words) > 2 {
		return c.fail("usage: hostrec register [claude-code|codex] [<plugin>] [--config-dir <dir>|--codex-home <dir>] | register --check")
	}
	e, code := c.needEnv()
	if e == nil {
		return code
	}
	hosts, plugin, ok := hostsAndPlugin(words, false)
	if !ok {
		return c.fail("usage: hostrec register [claude-code|codex] [<plugin>]; Part 1's plugins are %s for claude-code and %s for codex", claudePlugin, codexPlugin)
	}
	env := hostEnv(flags)
	if flags["check"] != "" {
		return registerCheck(c, e, env, hosts)
	}
	market := filepath.Join(e.home, marketName)
	failed := 0
	for _, host := range hosts {
		have, _ := listed(env, host)
		want := pluginsOf[host]
		if plugin != "" {
			want = []string{plugin}
		}
		if len(want) > 0 && !slices.ContainsFunc(want, func(p string) bool { return !slices.Contains(have, p) }) {
			c.say("%s lists %s already; nothing to add.", host, strings.Join(want, ", "))
			continue
		}
		for _, argv := range registerCommands(host, market) {
			if !tryForms(c, e, "register", host, env, argv) {
				failed++
			}
		}
		registerCheck(c, e, env, []string{host})
	}
	if failed > 0 {
		return 1
	}
	return 0
}

// tryForms runs a host command, then once more without --json if it fails,
// keeps each form's exit status and whether it asked anything, and prints
// the forms tried, the last for the owner to type by hand.
func tryForms(c *cli, e *env, what, host string, env []string, argv []string) bool {
	forms := [][]string{argv}
	if i := slices.Index(argv, "--json"); i >= 0 {
		forms = append(forms, slices.Delete(slices.Clone(argv), i, i+1))
	}
	for i, form := range forms {
		r := runHost(env, form)
		_ = e.command(what, map[string]any{"host": host, "step": strings.Join(form[1:min(len(form), 4)], " "), "json": slices.Contains(form, "--json"), "exit": r.exit, "asked": r.asked})
		c.say("%s </dev/null: exit status %d%s", printable(form), r.exit, map[bool]string{true: ", and it asked a question", false: ""}[r.asked])
		if r.exit == 0 {
			return true
		}
		if i == len(forms)-1 {
			c.say("It failed in each form. Type it yourself, as the run-book says:")
			c.say("  %s", printable(form))
		}
	}
	return false
}

func registerCheck(c *cli, e *env, env []string, hosts []string) int {
	for _, host := range hosts {
		have, r := listed(env, host)
		_ = e.command("register --check", map[string]any{"host": host, "listed": have, "exit": r.exit})
		if r.exit != 0 {
			c.say("%s: its plugin list did not answer (exit status %d).", host, r.exit)
			continue
		}
		if len(have) == 0 {
			c.say("%s lists no scratch plugin.", host)
		} else {
			c.say("%s lists %s.", host, strings.Join(have, ", "))
		}
	}
	return 0
}

// hostsAndPlugin reads register's and unregister's words: a host or all,
// and a plugin of that host.
func hostsAndPlugin(words []string, allowAll bool) (hosts []string, plugin string, ok bool) {
	hosts = []string{hostClaude, hostCodex}
	if len(words) == 0 {
		return hosts, "", true
	}
	switch words[0] {
	case hostClaude, hostCodex:
		hosts = words[:1]
	case "all":
		if !allowAll || len(words) > 1 {
			return nil, "", false
		}
		return hosts, "", true
	default:
		return nil, "", false
	}
	if len(words) == 2 {
		if !slices.Contains(pluginsOf[hosts[0]], words[1]) {
			return nil, "", false
		}
		plugin = words[1]
	}
	return hosts, plugin, true
}

// unregisterCommands are the hosts' own commands that remove a scratch
// plugin, and the marketplace once no scratch plugin is left.
func unregisterCommands(host, plugin string, market bool) [][]string {
	var out [][]string
	switch host {
	case hostClaude:
		out = append(out, []string{cliClaude, "plugin", "uninstall", plugin + "@" + marketplace, "--scope", "user", "--json"})
		if market {
			out = append(out, []string{cliClaude, "plugin", "marketplace", "remove", marketplace, "--json"})
		}
	case hostCodex:
		out = append(out, []string{cliCodex, "plugin", "remove", plugin + "@" + marketplace, "--json"})
		if market {
			out = append(out, []string{cliCodex, "plugin", "marketplace", "remove", marketplace, "--json"})
		}
	}
	return out
}

func cmdUnregister(c *cli, args []string) int {
	flags, words, err := parseArgs(args, []string{"config-dir", "codex-home"}, nil)
	if err != nil || len(words) > 2 {
		return c.fail("usage: hostrec unregister [claude-code|codex|all] [<plugin>]")
	}
	e, code := c.needEnv()
	if e == nil {
		return code
	}
	hosts, plugin, ok := hostsAndPlugin(words, true)
	if !ok {
		return c.fail("usage: hostrec unregister [claude-code|codex|all] [<plugin>]")
	}
	env := hostEnv(flags)
	failed := 0
	for _, host := range hosts {
		have, _ := listed(env, host)
		remove := have
		if plugin != "" {
			remove = []string{plugin}
		}
		for i, p := range remove {
			last := plugin == "" && i == len(remove)-1
			for _, argv := range unregisterCommands(host, p, last) {
				if !tryForms(c, e, "unregister", host, env, argv) {
					failed++
				}
			}
		}
		registerCheck(c, e, env, []string{host})
	}
	if failed > 0 {
		return 1
	}
	return 0
}

// trustTimeout bounds Codex's read-only hooks/list; tests raise it.
var trustTimeout = 30 * time.Second

func cmdTrust(c *cli, args []string) int {
	if len(args) != 0 {
		return c.fail("usage: hostrec trust")
	}
	e, code := c.needEnv()
	if e == nil {
		return code
	}
	cwd, _ := os.Getwd()
	counts, err := hooksList(cwd)
	if err != nil {
		_ = e.command("trust", map[string]any{"answered": false})
		return c.fail("Codex's read-only hooks/list did not answer: %v", err)
	}
	_ = e.command("trust", map[string]any{"answered": true, "hooks": counts})
	total, trusted := 0, 0
	for _, h := range counts {
		total += h.N
		if h.Enabled && (h.Trust == "trusted" || h.Trust == "managed") {
			trusted += h.N
		}
	}
	c.say("Codex loads %s from the scratch plugins; %d of them trusted and enabled.", plural(total, "hook", "hooks"), trusted)
	for _, h := range counts {
		c.say("  %s %s: %d, %s, enabled %v", h.Plugin, h.Event, h.N, h.Trust, h.Enabled)
	}
	if total == 0 || trusted != total {
		return 1
	}
	return 0
}

// hookCount is how many scratch hooks hooks/list gives for one plugin,
// event, trust state and enabled state.
type hookCount struct {
	Plugin  string `json:"plugin"`
	Event   string `json:"event"`
	Trust   string `json:"trust"`
	Enabled bool   `json:"enabled"`
	N       int    `json:"n"`
}

// hooksList asks Codex's app server for hooks/list, the read-only method
// internal/health asks, and counts the scratch plugins' hooks.
func hooksList(cwd string) ([]hookCount, error) {
	ctx, cancel := context.WithTimeout(context.Background(), trustTimeout)
	defer cancel()
	cmd := exec.CommandContext(ctx, cliCodex, "--no-daemon", "app-server", "--stdio")
	cmd.Stderr = io.Discard
	cmd.WaitDelay = time.Second
	cmd.SysProcAttr = &syscall.SysProcAttr{Setsid: true}
	in, err := cmd.StdinPipe()
	if err != nil {
		return nil, err
	}
	out, err := cmd.StdoutPipe()
	if err != nil {
		return nil, err
	}
	if err := cmd.Start(); err != nil {
		return nil, errors.New("codex did not start")
	}
	defer func() { in.Close(); cancel(); _ = cmd.Wait() }()
	type reply struct {
		ID     int             `json:"id"`
		Result json.RawMessage `json:"result"`
		Error  json.RawMessage `json:"error"`
	}
	lines := make(chan reply, 8)
	go func() {
		defer close(lines)
		s := bufio.NewScanner(io.LimitReader(out, 8<<20))
		s.Buffer(make([]byte, 4096), 4<<20)
		for s.Scan() {
			var v reply
			if json.Unmarshal(s.Bytes(), &v) == nil && v.ID > 0 {
				select {
				case lines <- v:
				case <-ctx.Done():
					return
				}
			}
		}
	}()
	enc := json.NewEncoder(in)
	recv := func(id int) (json.RawMessage, error) {
		for {
			select {
			case v, ok := <-lines:
				if !ok {
					return nil, errors.New("codex closed")
				}
				if v.ID == id {
					if len(v.Error) > 0 && string(v.Error) != "null" {
						return nil, errors.New("codex answered with an error")
					}
					return v.Result, nil
				}
			case <-ctx.Done():
				return nil, errors.New("no answer in time")
			}
		}
	}
	if err := enc.Encode(map[string]any{"id": 1, "method": "initialize", "params": map[string]any{"clientInfo": map[string]string{"name": "hf_recorder", "version": "1"}, "capabilities": map[string]bool{"experimentalApi": true}}}); err != nil {
		return nil, err
	}
	if _, err := recv(1); err != nil {
		return nil, err
	}
	if err := enc.Encode(map[string]any{"method": "initialized", "params": map[string]any{}}); err != nil {
		return nil, err
	}
	if err := enc.Encode(map[string]any{"id": 2, "method": "hooks/list", "params": map[string]any{"cwds": []string{cwd}}}); err != nil {
		return nil, err
	}
	raw, err := recv(2)
	if err != nil {
		return nil, err
	}
	var result struct {
		Data []struct {
			Hooks []struct {
				EventName string `json:"eventName"`
				PluginID  string `json:"pluginId"`
				Enabled   bool   `json:"enabled"`
				Trust     string `json:"trustStatus"`
			} `json:"hooks"`
		} `json:"data"`
	}
	if err := json.Unmarshal(raw, &result); err != nil {
		return nil, errors.New("hooks/list's answer does not read")
	}
	var counts []hookCount
	for _, g := range result.Data {
		for _, h := range g.Hooks {
			plugin, market, _ := strings.Cut(h.PluginID, "@")
			if market != marketplace {
				continue
			}
			k := hookCount{Plugin: plugin, Event: h.EventName, Trust: h.Trust, Enabled: h.Enabled}
			if i := slices.IndexFunc(counts, func(x hookCount) bool { x.N = 0; return x == k }); i >= 0 {
				counts[i].N++
			} else {
				k.N = 1
				counts = append(counts, k)
			}
		}
	}
	return counts, nil
}
