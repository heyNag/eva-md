package audit

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

// The tools the audit counts are those the plugins' PostToolUse matchers
// name, as the installer writes them for each host.
func TestTheToolsAreThoseTheHooksTake(t *testing.T) {
	for host, want := range map[string]string{"claude": claudeMatcher, "codex": codexMatcher} {
		data, err := os.ReadFile(filepath.Join("..", "..", "install", "testdata", host+"-hooks.golden.json"))
		if err != nil {
			t.Fatal(err)
		}
		var hooks struct {
			Hooks map[string][]struct {
				Matcher string `json:"matcher"`
			} `json:"hooks"`
		}
		if err := json.Unmarshal(data, &hooks); err != nil {
			t.Fatal(err)
		}
		for _, event := range []string{"PostToolUse", "PostToolUseFailure"} {
			for _, h := range hooks.Hooks[event] {
				if h.Matcher != want {
					t.Errorf("%s's %s matcher is %q, the audit's %q", host, event, h.Matcher, want)
				}
			}
		}
		if len(hooks.Hooks["PostToolUse"]) == 0 {
			t.Errorf("%s declares no PostToolUse", host)
		}
	}
	for name, taken := range map[string]bool{"Bash": true, "mcp__x__y": true, "Read": true, "Task": false, "XBash": false, "Bashful": false} {
		if claudeTools.MatchString(name) != taken {
			t.Errorf("claude takes %s: %v", name, !taken)
		}
	}
	for name, taken := range map[string]bool{"Bash": true, "apply_patch": true, "update_plan": true, "mcp__x": true, "view_image": false, "shell": false} {
		if codexTools.MatchString(name) != taken {
			t.Errorf("codex takes %s: %v", name, !taken)
		}
	}
}

// Items pair turn by turn; those with no turn id pair with the other side's
// unpaired ones, their own first.
func TestMatch(t *testing.T) {
	for _, c := range []struct {
		host, stored map[string]int
		want         Coverage
	}{
		{map[string]int{"a": 2, "b": 1}, map[string]int{"a": 2, "b": 1}, Coverage{Found: 3}},
		{map[string]int{"a": 2}, map[string]int{"a": 1, "b": 1}, Coverage{Found: 1, Missing: 1, Extra: 1}},
		{map[string]int{"a": 1, "": 1}, map[string]int{"": 2}, Coverage{Found: 2}},
		{map[string]int{"": 2}, map[string]int{"a": 1, "b": 2}, Coverage{Found: 2, Extra: 1}},
		{map[string]int{"": 1, "a": 1}, map[string]int{"": 1, "b": 1}, Coverage{Found: 1, Missing: 1, Extra: 1}},
		{nil, map[string]int{"": 3}, Coverage{Extra: 3}},
		{map[string]int{"a": 3}, nil, Coverage{Missing: 3}},
	} {
		if got := match(c.host, c.stored); got != c.want {
			t.Errorf("match(%v, %v) = %+v, want %+v", c.host, c.stored, got, c.want)
		}
	}
}
