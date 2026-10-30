package install

import (
	"embed"
	"encoding/json"
	"fmt"
	"path/filepath"
	"strings"
)

// Plugin bytes intentionally contain no binary version or timestamp. Codex
// trusts the plugin's contents, while binary-only updates keep that trust.
//
//go:embed templates/*.json templates/icon.png
var templates embed.FS

// iconPath is where both plugins keep their icon, the 🧠 emoji. Codex shows
// it (interface.composerIcon and logo); Claude Code reads plugin.json's icon
// only for Anthropic's public directory.
const iconPath = "plugins/eva-md/assets/icon.png"

const managed = ".eva-md-managed"
const managedContents = "eva-md marketplace v1\n"

func render(host, binary string) (map[string][]byte, error) {
	files := map[string][]byte{managed: []byte(managedContents)}
	marketManifest, pluginManifest := ".claude-plugin/marketplace.json", ".claude-plugin/plugin.json"
	if host == "codex" {
		marketManifest, pluginManifest = ".agents/plugins/marketplace.json", ".codex-plugin/plugin.json"
	}
	for key, name := range map[string]string{marketManifest: "marketplace", "plugins/eva-md/" + pluginManifest: "plugin"} {
		data, err := templates.ReadFile("templates/" + host + "-" + name + ".json")
		if err != nil {
			return nil, err
		}
		files[key] = data
	}
	icon, err := templates.ReadFile("templates/icon.png")
	if err != nil {
		return nil, err
	}
	files[iconPath] = icon
	type event struct {
		name, matcher string
		timeout       int
		async         bool
	}
	events := []event{
		{"SessionStart", "startup|resume|clear|compact|fork", 10, false},
		{"UserPromptSubmit", "", 5, false},
		{"PostToolUse", "Bash|Edit|Write|NotebookEdit|TaskCreate|TaskUpdate|TodoWrite", 0, true},
		{"PostToolUseFailure", "Bash|Edit|Write|NotebookEdit|TaskCreate|TaskUpdate|TodoWrite", 0, true},
		{"Stop", "", 5, false}, {"StopFailure", "", 3, false},
		{"PreCompact", "manual|auto", 10, false}, {"PostCompact", "", 5, false},
		{"PostModelSwitch", "", 3, false}, {"SessionEnd", "", 0, false},
	}
	if host == "codex" {
		events = []event{
			{"SessionStart", "startup|resume|clear|compact|fork", 10, false},
			{"UserPromptSubmit", "", 5, false},
			{"PostToolUse", "^(Bash|apply_patch|update_plan)$", 5, true},
			{"Stop", "", 5, false}, {"PreCompact", "manual|auto", 10, false},
			{"Interrupt", "", 1, false}, {"SessionEnd", "", 2, false},
		}
	}
	hooks := map[string]any{}
	for _, e := range events {
		command := map[string]any{"type": "command", "command": binary, "args": []string{"hook", "--host", "claude-code"}}
		if host == "codex" {
			command = map[string]any{"type": "command", "command": shellQuote(binary) + " hook --host codex || echo '{}'"}
			if e.name == "SessionStart" || e.name == "UserPromptSubmit" {
				command["additionalContextLimit"] = 3000
			}
		}
		if e.timeout != 0 {
			command["timeout"] = e.timeout
		}
		if e.async {
			command["async"] = true
		}
		entry := map[string]any{"hooks": []any{command}}
		if e.matcher != "" {
			entry["matcher"] = e.matcher
		}
		hooks[e.name] = []any{entry}
	}
	data, err := json.MarshalIndent(map[string]any{"hooks": hooks}, "", "  ")
	if err != nil {
		return nil, err
	}
	files["plugins/eva-md/hooks/hooks.json"] = append(data, '\n')
	return files, nil
}

func shellQuote(s string) string { return "'" + strings.ReplaceAll(s, "'", "'\"'\"'") + "'" }

func marketplacePath(share, host string) string { return filepath.Join(share, "marketplace", host) }
func pluginPath(share, host string) string {
	return filepath.Join(marketplacePath(share, host), "plugins", "eva-md")
}
func commandDescription(host string, args []string) string {
	return fmt.Sprintf("%s %s", host, strings.Join(args, " "))
}
