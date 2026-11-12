package main

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// The events each host's scratch plugin declares: every event of F-075 for
// Claude Code and of F-120 for Codex.
var (
	claudeEvents = []string{
		"SessionStart", "UserPromptSubmit", "PreToolUse", "PostToolUse", "PostToolUseFailure", "PostToolBatch",
		"SubagentStart", "SubagentStop", "Stop", "StopFailure", "PreCompact", "PostCompact", "PostModelSwitch", "SessionEnd",
	}
	codexEvents = []string{
		"SessionStart", "UserPromptSubmit", "PreToolUse", "PostToolUse", "SubagentStart", "SubagentStop",
		"Stop", "PreCompact", "PostCompact", "Interrupt", "SessionEnd",
	}
)

// codexRow is a row of the hosts paper's generation 1 table (section 4.3),
// whose values the Codex scratch hooks declare.
type codexRow struct {
	async   bool
	timeout int
	limit   int // additionalContextLimit; 0 declares none
}

var codexTable = map[string]codexRow{
	"SessionStart":     {false, 10, 3000},
	"UserPromptSubmit": {false, 5, 3000},
	"PreToolUse":       {false, 2, 0},
	"PostToolUse":      {true, 5, 0},
	"SubagentStart":    {false, 5, 0},
	"SubagentStop":     {true, 5, 0},
	"Stop":             {false, 5, 0},
	"PreCompact":       {false, 10, 0},
	"PostCompact":      {false, 5, 0},
	"Interrupt":        {false, 1, 0},
	"SessionEnd":       {false, 2, 0},
}

// claudeTimeout is each Claude Code scratch hook's limit in seconds, but
// the one of F-092's timeout step.
const claudeTimeout = 30

// The plugins' names, and the marketplace both hosts know them by.
const (
	marketplace  = "hf-scratch"
	claudePlugin = "hf-recorder"
	codexPlugin  = "hf-hooks"
)

// shellQuote quotes a word for sh.
func shellQuote(s string) string { return "'" + strings.ReplaceAll(s, "'", `'\''`) + "'" }

// hookCommand is a scratch hook's command: an absolute path outside every
// plugin root.
func hookCommand(bin, host, event, via string) string {
	cmd := shellQuote(bin) + " hook --host " + host + " --event " + event
	if via != "" {
		cmd += " --via " + via
	}
	return cmd
}

type hookEntry = map[string]any
type hookGroup = map[string]any

func group(matcher string, hooks ...hookEntry) hookGroup {
	g := hookGroup{"hooks": hooks}
	if matcher != "" {
		g["matcher"] = matcher
	}
	return g
}

// claudeHooks is hf-recorder's hooks.json: a hook with no matcher for each
// event of F-075, and beside UserPromptSubmit's the exec-form hook under
// the plugin root (F-104), the one naming a binary outside it that the
// missing arm renames (F-050), and the one with a two-second limit (F-092).
func claudeHooks(tools string) map[string]any {
	bin := filepath.Join(tools, "hostrec")
	hooks := map[string]any{}
	for _, ev := range claudeEvents {
		groups := []hookGroup{group("", hookEntry{"type": "command", "command": hookCommand(bin, hostClaude, ev, ""), "timeout": claudeTimeout})}
		if ev == "UserPromptSubmit" {
			groups = append(groups,
				group("", hookEntry{"type": "command", "command": "${CLAUDE_PLUGIN_ROOT}/scripts/hf-exec-check", "args": []string{ev}, "timeout": claudeTimeout}),
				group("", hookEntry{"type": "command", "command": hookCommand(filepath.Join(tools, "hf-missing-check"), hostClaude, ev, "missing-check"), "timeout": claudeTimeout}),
				group("", hookEntry{"type": "command", "command": hookCommand(bin, hostClaude, ev, "timeout-check"), "timeout": 2}),
			)
		}
		hooks[ev] = groups
	}
	return map[string]any{"hooks": hooks}
}

// codexHooks is hf-hooks's hooks.json: a hook with no matcher for each
// event of F-120 with generation 1's values, a shell matcher and an
// Edit|Write matcher on PreToolUse (F-123), a second SessionStart handler
// that declares no context limit (F-125), and the hook naming the binary
// the missing arm renames (F-050).
func codexHooks(tools string) map[string]any {
	bin := filepath.Join(tools, "hostrec")
	entry := func(command string, row codexRow) hookEntry {
		h := hookEntry{"type": "command", "command": command, "timeout": row.timeout}
		if row.async {
			h["async"] = true
		}
		if row.limit > 0 {
			h["additionalContextLimit"] = row.limit
		}
		return h
	}
	hooks := map[string]any{}
	for _, ev := range codexEvents {
		row := codexTable[ev]
		groups := []hookGroup{group("", entry(hookCommand(bin, hostCodex, ev, ""), row))}
		switch ev {
		case "PreToolUse":
			groups = append(groups,
				group("^Bash$", entry(hookCommand(bin, hostCodex, ev, "shell-matcher"), row)),
				group("Edit|Write", entry(hookCommand(bin, hostCodex, ev, "edit-matcher"), row)))
		case "SessionStart":
			groups = append(groups, group("", entry(hookCommand(bin, hostCodex, ev, "plain-start"), codexRow{timeout: row.timeout})))
		case "UserPromptSubmit":
			groups = append(groups, group("", entry(hookCommand(filepath.Join(tools, "hf-missing-check"), hostCodex, ev, "missing-check"), row)))
		}
		hooks[ev] = groups
	}
	return map[string]any{"hooks": hooks}
}

// pluginFile is one file setup writes, with its mode.
type pluginFile struct {
	path string
	data []byte
	mode os.FileMode
}

func jsonFile(path string, v any) pluginFile {
	data, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		panic(err)
	}
	return pluginFile{path, append(data, '\n'), 0o644}
}

func textFile(path, text string) pluginFile { return pluginFile{path, []byte(text), 0o644} }

// pluginFiles are the scratch marketplaces of Part 1, under market: Claude
// Code's with hf-recorder, Codex's with hf-hooks. Part 2 adds hf-recorder's
// MCP entry, hf-tools and hf-hooks-two.
func pluginFiles(market, tools string) []pluginFile {
	cc := filepath.Join(market, hostClaude)
	rec := filepath.Join(cc, "plugins", claudePlugin)
	cx := filepath.Join(market, hostCodex)
	hk := filepath.Join(cx, "plugins", codexPlugin)
	about := "Made-up scratch plugin for the host sittings. Nothing in it is real."
	return []pluginFile{
		jsonFile(filepath.Join(cc, ".claude-plugin", "marketplace.json"), map[string]any{
			"name": marketplace, "description": about, "owner": map[string]string{"name": marketplace},
			"plugins": []map[string]string{{"name": claudePlugin, "source": "./plugins/" + claudePlugin}},
		}),
		jsonFile(filepath.Join(rec, ".claude-plugin", "plugin.json"), map[string]any{
			"name": claudePlugin, "version": "1", "description": about, "author": map[string]string{"name": marketplace},
		}),
		jsonFile(filepath.Join(rec, "hooks", "hooks.json"), claudeHooks(tools)),
		{filepath.Join(rec, "scripts", "hf-exec-check"), []byte("#!/bin/sh\n# The exec-form scratch hook: it runs the recorder.\nexec " +
			shellQuote(filepath.Join(tools, "hostrec")) + ` hook --host claude-code --event "${1:-UserPromptSubmit}" --via exec-check` + "\n"), 0o755},
		textFile(filepath.Join(rec, "commands", "hf-pause-check.md"), "---\ndescription: Made-up scratch command for the host sittings.\n---\n"+
			"Made-up test command. Reply with the single word PAUSE-CHECK-PLUM and nothing else.\n"),
		textFile(filepath.Join(rec, "skills", "hf-orchard-skill", "SKILL.md"), "---\nname: hf-orchard-skill\ndescription: Made-up scratch skill for the host sittings. Use it only when asked for the orchard skill by name.\n---\n\n"+
			"Made-up test skill. When used, say the word ORCHARD-SKILL-PLUM.\n"),
		textFile(filepath.Join(rec, "agents", "hf-gardener.md"), "---\nname: hf-gardener\ndescription: Made-up scratch agent for the host sittings. Use it only when asked for the hf-gardener agent by name.\n---\n\n"+
			"You are a made-up test agent. Do what you are asked, in one sentence, and nothing else.\n"),
		jsonFile(filepath.Join(cx, ".agents", "plugins", "marketplace.json"), map[string]any{
			"name": marketplace, "interface": map[string]string{"displayName": marketplace},
			"plugins": []map[string]any{{
				"name": codexPlugin, "source": map[string]string{"source": "local", "path": "./plugins/" + codexPlugin},
				"policy": map[string]string{"installation": "AVAILABLE", "authentication": "ON_INSTALL"}, "category": "Productivity",
			}},
		}),
		jsonFile(filepath.Join(hk, ".codex-plugin", "plugin.json"), map[string]any{
			"name": codexPlugin, "version": "1.0.0", "description": about, "author": map[string]string{"name": marketplace},
			"skills": "./skills/", "interface": map[string]any{"displayName": codexPlugin, "shortDescription": about, "developerName": marketplace, "category": "Productivity"},
		}),
		jsonFile(filepath.Join(hk, "hooks", "hooks.json"), codexHooks(tools)),
		textFile(filepath.Join(hk, "skills", "hf-inside-skill", "SKILL.md"), "---\nname: hf-inside-skill\ndescription: Made-up scratch skill inside the hooks plugin. Use it only when asked for it by name.\n---\n\n"+
			"Made-up test skill. When used, say the word INSIDE-SKILL-PEAR.\n"),
	}
}

// pluginRoots are the roots of the scratch plugins of Part 1.
func pluginRoots(market string) []string {
	return []string{
		filepath.Join(market, hostClaude, "plugins", claudePlugin),
		filepath.Join(market, hostCodex, "plugins", codexPlugin),
	}
}

// registerCommands are the hosts' own commands that add the scratch
// marketplace and plugin, as setup prints them and register runs them.
func registerCommands(host, market string) [][]string {
	switch host {
	case hostClaude:
		return [][]string{
			{cliClaude, "plugin", "marketplace", "add", filepath.Join(market, hostClaude), "--json"},
			{cliClaude, "plugin", "install", claudePlugin + "@" + marketplace, "--scope", "user", "--json"},
		}
	case hostCodex:
		return [][]string{
			{cliCodex, "plugin", "marketplace", "add", filepath.Join(market, hostCodex), "--json"},
			{cliCodex, "plugin", "add", codexPlugin + "@" + marketplace, "--json"},
		}
	}
	return nil
}

func printable(argv []string) string {
	out := make([]string, len(argv))
	for i, a := range argv {
		if strings.ContainsAny(a, " '\"$\\") {
			a = shellQuote(a)
		}
		out[i] = a
	}
	return fmt.Sprint(strings.Join(out, " "))
}
