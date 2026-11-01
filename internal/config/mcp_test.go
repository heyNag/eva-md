package config

import (
	"os"
	"path/filepath"
	"reflect"
	"testing"
)

// mcp = false turns the MCP tools off: absent it is true, an explicit
// false stays false through a save, and a misspelled key is reported as
// unknown rather than read as the switch.
func TestMCPSwitch(t *testing.T) {
	isolate(t)
	if !Default().MCP {
		t.Fatal("the tools are off by default")
	}
	file := filepath.Join(t.TempDir(), "config.toml")
	for _, tc := range []struct {
		body    string
		mcp     bool
		unknown []string
	}{
		{"notice = \"start\"\n", true, nil},
		{"mcp = false\n", false, nil},
		{"mcp = true\nupdates = false\n", true, nil},
		{"mcp_tools = false\n", true, []string{"mcp_tools"}},
	} {
		if err := os.WriteFile(file, []byte(tc.body), 0o600); err != nil {
			t.Fatal(err)
		}
		c, err := Load(file)
		if err != nil || c.MCP != tc.mcp || !reflect.DeepEqual(c.Unknown, tc.unknown) {
			t.Fatalf("%q: mcp %v, unknown %q, %v", tc.body, c.MCP, c.Unknown, err)
		}
		if err := Save(file, c); err != nil {
			t.Fatal(err)
		}
		if again, err := Load(file); err != nil || again.MCP != tc.mcp {
			t.Fatalf("%q saved and read back: %v, %v", tc.body, again.MCP, err)
		}
	}
	if err := os.WriteFile(file, []byte("mcp = \"no\"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := Load(file); err == nil {
		t.Error("a string for mcp was accepted")
	}
}
