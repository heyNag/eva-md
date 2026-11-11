package audit

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
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

// A row longer than the limit is passed over and counted, the rows after it
// are read, and a last row with no newline is left unread.
func TestLongRowsAndAnUnfinishedRow(t *testing.T) {
	row := func(v any) string {
		b, err := json.Marshal(v)
		if err != nil {
			t.Fatal(err)
		}
		return string(b) + "\n"
	}
	long := cUser("L", at(0, 1), strings.Repeat("x", 200<<10)) // longer than the reader's buffer too
	data := row(cUser("A", at(0, 0), "first")) + row(long) + row(cUser("B", at(0, 2), "second")) + strings.TrimSuffix(row(cUser("C", at(0, 3), "unfinished")), "\n")
	rec, err := readHost(context.Background(), "claude-code", claudeID, strings.NewReader(data), 1024)
	if err != nil {
		t.Fatal(err)
	}
	var turns []string
	for _, it := range rec.items {
		turns = append(turns, it.turn)
	}
	if rec.tooLong != 1 || strings.Join(turns, ",") != "A,B" {
		t.Errorf("too long %d, turns %q", rec.tooLong, turns)
	}
}

// A Claude Code prompt with an attachment is a prompt, by its text; a
// prompt of an attachment alone, a tool's result, a meta or compaction row
// are not; a reply that also calls a tool is not a final reply.
func TestClaudeRows(t *testing.T) {
	rows := []any{
		cUser("A", at(0, 0), []map[string]any{text(marker + " look"), {"type": "image", "source": map[string]any{"data": marker}}}),
		cUser("B", at(0, 1), []map[string]any{{"type": "image", "source": map[string]any{"data": marker}}}),
		map[string]any{"type": "user", "sessionId": claudeID, "promptId": "C", "isMeta": true, "timestamp": at(0, 2), "message": map[string]any{"role": "user", "content": marker}},
		map[string]any{"type": "user", "sessionId": claudeID, "promptId": "D", "isCompactSummary": true, "timestamp": at(0, 3), "message": map[string]any{"role": "user", "content": marker}},
		cAssistant("E", at(0, 4), "end_turn", text(marker), toolUse("e1", "Bash")),
		cAssistant("E", at(0, 5), "max_tokens", text(marker)),
	}
	var data []byte
	for _, r := range rows {
		b, _ := json.Marshal(r)
		data = append(append(data, b...), '\n')
	}
	rec, err := readHost(context.Background(), "claude-code", claudeID, strings.NewReader(string(data)), maxRow)
	if err != nil {
		t.Fatal(err)
	}
	var got []string
	for _, it := range rec.items {
		got = append(got, names[it.kind]+":"+it.turn)
	}
	if strings.Join(got, ",") != "prompts:A,tool calls:E" {
		t.Errorf("items %q", got)
	}
}
