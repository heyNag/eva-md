package health

import (
	"path/filepath"
	"testing"
)

// A plugin that is installed but disabled loads no hooks by design, so the
// trust row with no hooks loaded stays UNKNOWN, as with no plugin at all;
// only an installed and enabled plugin that loads none fails it.
func TestDisabledPluginWithNoLoadedHooksStaysUnknown(t *testing.T) {
	for _, c := range []struct {
		enabled bool
		want    Check
	}{
		{false, Check{"codex hook trust", "UNKNOWN", "no eva-md hooks loaded", "install eva-md before reviewing /hooks"}},
		{true, Check{"codex hook trust", "FAIL", "Codex loads no eva-md hooks for the enabled plugin", "check the Codex hooks feature, then run eva-md install --host codex"}},
	} {
		d := fakeTrustHost(t, map[string]any{"data": []any{map[string]any{"hooks": []any{}}}})
		plugin := filepath.Join(d.Share, "marketplace", "codex", "plugins", "eva-md")
		market := filepath.Join(d.Share, "marketplace", "codex")
		enabled := "false"
		if c.enabled {
			enabled = "true"
		}
		withInventory(t, d,
			`{"installed":[{"pluginId":"eva-md@eva-md","enabled":`+enabled+`,"source":{"path":"`+plugin+`"}}]}`,
			`{"marketplaces":[{"name":"eva-md","path":"`+market+`"}]}`)
		checks := hostStatuses(t, d)
		if p := checks["codex plugin"]; c.enabled != (p.Status == "PASS") {
			t.Fatalf("enabled %v: the plugin row %+v", c.enabled, p)
		}
		if got := checks["codex hook trust"]; got != c.want {
			t.Errorf("enabled %v: %+v, want %+v", c.enabled, got, c.want)
		}
	}
}
