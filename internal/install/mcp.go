package install

import (
	"encoding/json"

	"github.com/heyNag/eva-md/internal/host/adapter"
)

// mcpPath is where both plugins declare eva-md's MCP server. Claude Code
// reads a plugin's .mcp.json by default; the Codex manifest names it in its
// mcpServers field.
const mcpPath = "plugins/eva-md/.mcp.json"

// MCPManifest is a plugin's .mcp.json for host, a plugin host (claude or
// codex): one server, eva-md, that runs the installed binary by its absolute
// path as eva-md mcp --host and the host's id, claude-code or codex; a name
// the adapter doesn't know gets Claude Code's. It names no type, since both
// hosts start a command as a stdio server, and no env, cwd, version or date,
// so the folder stays byte-identical across binary-only releases.
func MCPManifest(host, binary string) []byte {
	id := adapter.ClaudeCode
	if h, ok := adapter.ByInstallName(host); ok {
		id = h.ID
	}
	server := map[string]any{"command": binary, "args": []string{"mcp", "--host", string(id)}}
	data, err := json.MarshalIndent(map[string]any{"mcpServers": map[string]any{"eva-md": server}}, "", "  ")
	if err != nil {
		panic(err)
	}
	return append(data, '\n')
}
