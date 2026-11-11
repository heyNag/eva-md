package audit

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/heyNag/eva-md/internal/catchup"
	"github.com/heyNag/eva-md/internal/config"
	"github.com/heyNag/eva-md/internal/paths"
)

// jsonl is rows as a host writes them, one JSON object a line.
func jsonl(t *testing.T, rows ...any) string {
	t.Helper()
	var b strings.Builder
	for _, r := range rows {
		if s, ok := r.(string); ok {
			b.WriteString(s + "\n")
			continue
		}
		data, err := json.Marshal(r)
		if err != nil {
			t.Fatal(err)
		}
		b.Write(data)
		b.WriteByte('\n')
	}
	return b.String()
}

// itemNames names a record's items by kind and turn.
func itemNames(rec hostRecord) []string {
	var out []string
	for _, it := range rec.items {
		name := map[int]string{interrupt: "interrupt", longRow: "long", awayItem: "away"}[it.kind]
		if it.kind >= 0 {
			name = names[it.kind]
		}
		out = append(out, name+":"+it.turn)
	}
	return out
}

var (
	claudeSource = catchup.Source{Host: "claude-code", Session: claudeID, Cwd: "/" + marker}
	codexSource  = catchup.Source{Host: "codex", Session: codexID, Cwd: "/" + marker}
)

// A row longer than the limit that holds no record control is counted as
// too long to read, at its time; the rows after it are read; and a last row
// with no newline is left unread.
func TestLongRowsAndAnUnfinishedRow(t *testing.T) {
	long := cUser("L", at(0, 1), strings.Repeat("x", 200<<10)) // longer than the reader's buffer too
	data := jsonl(t, cUser("A", at(0, 0), "first"), long, cUser("B", at(0, 2), "second")) + strings.TrimSuffix(jsonl(t, cUser("C", at(0, 3), "unfinished")), "\n")
	rec, err := readHost(context.Background(), claudeSource, nil, strings.NewReader(data), 1024)
	if err != nil {
		t.Fatal(err)
	}
	if got := strings.Join(itemNames(rec), ","); got != "prompts:A,long:,prompts:B" || rec.items[1].at.Format(time.RFC3339) != at(0, 1) {
		t.Errorf("items %q, the long row at %v", got, rec.items[1].at)
	}
}

// A record control in a row too long to decode is found, as recovery's
// scan finds it, on both hosts: nothing between it and the control that
// brings the chat back is counted, and the range is counted once.
func TestALongRowIsScannedForARecordControl(t *testing.T) {
	image := map[string]any{"type": "image", "source": map[string]any{"data": strings.Repeat("A", 4096)}}
	claude := jsonl(t,
		cUser("T1", at(0, 0), marker+" first"),
		cUser("T2", at(1, 0), []any{text("off the record"), image}),
		cUser("T3", at(2, 0), marker+" said off"),
		cAssistant("T3", at(2, 1), "tool_use", toolUse("p1", "Bash")),
		cAssistant("T3", at(2, 2), "end_turn", text(marker+" off reply")),
		cUser("T3", at(2, 3), []any{text(marker + " long and off"), image}),
		cUser("T4", at(4, 0), "back on the record"),
		cUser("T5", at(5, 0), marker+" after"),
	)
	codex := jsonl(t,
		cx(at(0, 0), "event_msg", map[string]any{"type": "user_message", "turn_id": "U1", "message": marker}),
		cx(at(1, 0), "event_msg", map[string]any{"type": "user_message", "turn_id": "U2", "message": "off the record\n" + strings.Repeat("B", 4096)}),
		cx(at(2, 0), "event_msg", map[string]any{"type": "user_message", "turn_id": "U3", "message": marker + " said off"}),
		cx(at(2, 1), "response_item", map[string]any{"type": "function_call", "name": "shell", "call_id": "c1"}),
		cx(at(4, 0), "event_msg", map[string]any{"type": "user_message", "turn_id": "U4", "message": "back on the record"}),
		cx(at(5, 0), "event_msg", map[string]any{"type": "user_message", "turn_id": "U5", "message": marker + " after"}),
	)
	end, _ := time.Parse(time.RFC3339, at(4, 1))
	for _, c := range []struct {
		s     catchup.Source
		data  string
		items string
	}{
		{claudeSource, claude, "prompts:T1,prompts:T5"},
		{codexSource, codex, "prompts:U1,prompts:U5"},
	} {
		rec, err := readHost(context.Background(), c.s, nil, strings.NewReader(c.data), 1024)
		if err != nil {
			t.Fatal(err)
		}
		if got := strings.Join(itemNames(rec), ","); got != c.items || rec.paused != 1 || len(rec.pauses) != 1 ||
			rec.pauses[0].start.Format(time.RFC3339) != at(1, 0) || !rec.pauses[0].end.Equal(end) {
			t.Errorf("%s: items %q, paused %d, %+v", c.s.Host, got, rec.paused, rec.pauses)
		}
	}
}

// A control that brings the chat back on the record with no range open
// closes one opened at a row since the last control that could not be
// read, too long and not JSON the scan follows or not JSON at all, at the
// time of the last row before it; with no such row it closes none.
func TestAnUnreadRowMayHaveOpenedARange(t *testing.T) {
	end, _ := time.Parse(time.RFC3339, at(3, 1))
	start, _ := time.Parse(time.RFC3339, at(0, 0))
	for name, unread := range map[string]string{
		"too long":     `{"type":"user","sessionId":"` + claudeID + `","message":{"role":"user","content":"` + strings.Repeat("y", 4096),
		"not JSON":     `{"type":"user",` + marker,
		"none":         "",
		"another kind": `{"type":"user","sessionId":"` + claudeID + `","promptId":"X","timestamp":"` + at(1, 0) + `","cwd":"/` + marker + `","message":{"role":"user","content":"` + marker + `"}}`,
	} {
		rows := []any{cUser("T1", at(0, 0), marker)}
		if unread != "" {
			rows = append(rows, unread)
		}
		rows = append(rows, cUser("T3", at(2, 0), marker), cUser("T4", at(3, 0), "back on the record"), cUser("T5", at(4, 0), marker))
		rec, err := readHost(context.Background(), claudeSource, nil, strings.NewReader(jsonl(t, rows...)), 1024)
		if err != nil {
			t.Fatal(err)
		}
		want := []interval{{start: start, end: end}}
		if name == "none" || name == "another kind" {
			want = nil
		}
		if len(rec.pauses) != len(want) || rec.paused != len(want) || len(want) == 1 && (!rec.pauses[0].start.Equal(start) || !rec.pauses[0].end.Equal(end)) {
			t.Errorf("%s: paused %d, %+v", name, rec.paused, rec.pauses)
		}
	}
}

// countedItems names the items of a record outside every range it says
// was off the record, as the audit counts them.
func countedItems(rec hostRecord) []string {
	var out []string
	for _, it := range rec.items {
		if !within(it.at, rec.pauses) {
			out = append(out, itemNames(hostRecord{items: []hostItem{it}})...)
		}
	}
	return out
}

// Of two rows since the last control that could not be read, the first
// opens the range a control that brings the chat back closes, on both
// hosts: the prompt between them, whose row has a time, and the rows after
// it are counted in nothing, though a row with a later time and no item
// comes before the second. On Codex a row that is not JSON is such a row.
func TestTheFirstUnreadRowOpensTheRange(t *testing.T) {
	notJSON := `{"type":"` + marker
	claude := jsonl(t,
		cUser("T1", at(0, 0), marker+" before"),
		notJSON,
		cUser("T2", at(1, 0), marker+" between"),
		map[string]any{"type": "user", "sessionId": claudeID, "isMeta": true, "timestamp": at(2, 0), "message": map[string]any{"role": "user", "content": marker}},
		notJSON,
		cUser("T3", at(3, 0), "back on the record"),
		cUser("T4", at(4, 0), marker+" after"),
	)
	codex := jsonl(t,
		cx(at(0, 0), "event_msg", map[string]any{"type": "user_message", "turn_id": "U1", "message": marker + " before"}),
		notJSON,
		cx(at(1, 0), "event_msg", map[string]any{"type": "user_message", "turn_id": "U2", "message": marker + " between"}),
		cx(at(2, 0), "turn_context", map[string]any{"turn_id": "U2"}),
		notJSON,
		cx(at(3, 0), "event_msg", map[string]any{"type": "user_message", "turn_id": "U3", "message": "back on the record"}),
		cx(at(4, 0), "event_msg", map[string]any{"type": "user_message", "turn_id": "U4", "message": marker + " after"}),
	)
	start, _ := time.Parse(time.RFC3339, at(0, 0))
	end, _ := time.Parse(time.RFC3339, at(3, 1))
	for _, c := range []struct {
		s     catchup.Source
		data  string
		count string
	}{
		{claudeSource, claude, "prompts:T4"},
		{codexSource, codex, "prompts:U4"},
	} {
		rec, err := readHost(context.Background(), c.s, nil, strings.NewReader(c.data), maxRow)
		if err != nil {
			t.Fatal(err)
		}
		if got := strings.Join(countedItems(rec), ","); got != c.count || rec.paused != 1 || len(rec.pauses) != 1 ||
			!rec.pauses[0].start.Equal(start) || !rec.pauses[0].end.Equal(end) {
			t.Errorf("%s: counted %q, paused %d, %+v", c.s.Host, got, rec.paused, rec.pauses)
		}
	}
}

// A Codex chat whose first record names a folder the hooks leave out, and
// no turn's context names another, has its prompts, replies and tool calls
// counted apart, its interrupts not at all.
func TestACodexChatInAFolderLeftOut(t *testing.T) {
	rows := jsonl(t,
		cx(at(0, 0), "session_meta", map[string]any{"id": codexID, "cwd": "/" + marker + "-away"}),
		cx(at(0, 1), "event_msg", map[string]any{"type": "task_started", "turn_id": "U1"}),
		cx(at(0, 2), "event_msg", map[string]any{"type": "user_message", "turn_id": "U1", "message": marker}),
		cx(at(0, 3), "response_item", map[string]any{"type": "function_call", "name": "shell", "call_id": "c1"}),
		cx(at(0, 4), "event_msg", map[string]any{"type": "task_complete", "turn_id": "U1", "last_agent_message": marker}),
		cx(at(0, 5), "event_msg", map[string]any{"type": "turn_aborted", "turn_id": "U1"}),
	)
	outside := func(dir string) bool { return dir == "/"+marker+"-away" }
	rec, err := readHost(context.Background(), codexSource, outside, strings.NewReader(rows), maxRow)
	if err != nil {
		t.Fatal(err)
	}
	if got := strings.Join(itemNames(rec), ","); got != "away:,away:,away:" {
		t.Errorf("items %q", got)
	}
}

// A Claude Code prompt with an attachment is a prompt, by its text; a
// prompt of an attachment alone, a tool's result, a meta, compaction or
// compaction's copy row, a subagent's row named by its agent id alone, and
// a reply the host made or an error it shows are not; a reply that also
// calls a tool is not a final reply; and a prompt in a folder the hooks
// leave out is counted apart.
func TestClaudeRows(t *testing.T) {
	synthetic := cAssistant("G", at(0, 6), "end_turn", text(marker))
	synthetic["message"].(map[string]any)["model"] = "<synthetic>"
	agent := cUser("H", at(0, 7), marker)
	agent["agentId"] = marker + "-agent"
	repeats := cUser("I", at(0, 8), marker)
	repeats["repeatsCompactSummary"] = true
	away := cUser("J", at(0, 9), marker)
	away["cwd"] = "/" + marker + "-away"
	rows := jsonl(t,
		cUser("A", at(0, 0), []map[string]any{text(marker + " look"), {"type": "image", "source": map[string]any{"data": marker}}}),
		cUser("B", at(0, 1), []map[string]any{{"type": "image", "source": map[string]any{"data": marker}}}),
		map[string]any{"type": "user", "sessionId": claudeID, "promptId": "C", "isMeta": true, "timestamp": at(0, 2), "message": map[string]any{"role": "user", "content": marker}},
		map[string]any{"type": "user", "sessionId": claudeID, "promptId": "D", "isCompactSummary": true, "timestamp": at(0, 3), "message": map[string]any{"role": "user", "content": marker}},
		cAssistant("E", at(0, 4), "end_turn", text(marker), toolUse("e1", "Bash")),
		cAssistant("E", at(0, 5), "max_tokens", text(marker)),
		synthetic, agent, repeats, away,
	)
	outside := func(dir string) bool { return dir == "/"+marker+"-away" }
	rec, err := readHost(context.Background(), claudeSource, outside, strings.NewReader(rows), maxRow)
	if err != nil {
		t.Fatal(err)
	}
	if got := strings.Join(itemNames(rec), ","); got != "prompts:A,tool calls:E,away:" {
		t.Errorf("items %q", got)
	}
}

// Codex's rows: a final reply is counted once a turn, also where no row
// names its turn; a call is counted once by its id, a local shell call as
// a shell call; an aborted turn is an interrupt; and a turn whose context
// is a folder the hooks leave out is counted apart, its interrupt not at
// all.
func TestCodexRows(t *testing.T) {
	rows := jsonl(t,
		cx(at(0, 0), "session_meta", map[string]any{"id": codexID, "cwd": "/" + marker}),
		cx(at(0, 1), "event_msg", map[string]any{"type": "task_started"}),
		cx(at(0, 2), "event_msg", map[string]any{"type": "user_message", "message": marker + " one"}),
		cx(at(0, 3), "response_item", map[string]any{"type": "function_call", "name": "shell", "call_id": "c1"}),
		cx(at(0, 3), "response_item", map[string]any{"type": "function_call", "name": "shell", "call_id": "c1"}),
		cx(at(0, 4), "response_item", map[string]any{"type": "local_shell_call", "call_id": "c2"}),
		cx(at(0, 5), "event_msg", map[string]any{"type": "agent_message", "phase": "final", "message": marker}),
		cx(at(0, 6), "event_msg", map[string]any{"type": "task_complete", "last_agent_message": marker}),
		cx(at(1, 0), "event_msg", map[string]any{"type": "task_started"}),
		cx(at(1, 1), "event_msg", map[string]any{"type": "user_message", "message": marker + " two"}),
		cx(at(1, 2), "event_msg", map[string]any{"type": "task_complete", "last_agent_message": marker}),
		cx(at(1, 3), "event_msg", map[string]any{"type": "turn_aborted"}),
		cx(at(2, 0), "turn_context", map[string]any{"cwd": "/" + marker + "-away"}),
		cx(at(2, 1), "event_msg", map[string]any{"type": "user_message", "message": marker + " away"}),
		cx(at(2, 2), "response_item", map[string]any{"type": "function_call", "name": "shell", "call_id": "c3"}),
		cx(at(2, 3), "event_msg", map[string]any{"type": "turn_aborted"}),
	)
	outside := func(dir string) bool { return dir == "/"+marker+"-away" }
	rec, err := readHost(context.Background(), codexSource, outside, strings.NewReader(rows), maxRow)
	if err != nil {
		t.Fatal(err)
	}
	want := "prompts:,tool calls:,tool calls:,final replies:,prompts:,final replies:,interrupt:,away:,away:"
	if got := strings.Join(itemNames(rec), ","); got != want {
		t.Errorf("items %q, want %q", got, want)
	}
}

// The folders whose events the hooks leave out by design: those the config
// excludes, temporary folders, the spares, Codex's memories, eva-md's state
// and the wiki, and folders inside them, by their path and where their
// symlinks lead; a remote exclusion is not told.
func TestOutside(t *testing.T) {
	home, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	t.Setenv("HOME", home)
	wiki := filepath.Join(home, "wiki")
	mkdirs(t, wiki, filepath.Join(home, "project"), filepath.Join(home, "tmp", "here"))
	link := filepath.Join(home, "to-wiki")
	symlink(t, wiki, link)
	// The temporary folder is named through a link, as /tmp is on macOS.
	tmpLink := filepath.Join(home, "tmp-link")
	symlink(t, filepath.Join(home, "tmp"), tmpLink)
	o := Options{Home: home, TempDir: tmpLink, Dirs: paths.Dirs{State: filepath.Join(home, "state")},
		Config: config.Config{WikiPath: wiki, Exclude: []string{"~/secret-*", "remote:github.com/x/*"}}}
	away := outside(o)
	for dir, want := range map[string]bool{
		filepath.Join(home, "project"):                         false,
		filepath.Join(home, "secret-1"):                        true,
		filepath.Join(home, "secret-1", "deeper"):              true,
		filepath.Join(home, "tmp", "x"):                        true,
		filepath.Join(home, "tmp", "gone"):                     true,
		filepath.Join(tmpLink, "here"):                         true,
		filepath.Join(tmpLink, "x"):                            true,
		filepath.Join(tmpLink, "gone"):                         true,
		"/tmp/x":                                               true,
		filepath.Join(home, ".claude", "spares", "s"):          true,
		filepath.Join(home, ".codex", "memories"):              true,
		filepath.Join(home, "elsewhere", ".codex", "memories"): true,
		filepath.Join(home, "state"):                           true,
		filepath.Join(wiki, "pages"):                           true,
		link:                                                   true,
		filepath.Join(home, "wikis"):                           false,
		"relative":                                             false,
	} {
		if got := away(dir); got != want {
			t.Errorf("%s: %v", dir, got)
		}
	}
}

func mkdirs(t *testing.T, dirs ...string) {
	t.Helper()
	for _, d := range dirs {
		if err := os.MkdirAll(d, 0o700); err != nil {
			t.Fatal(err)
		}
	}
}

func symlink(t *testing.T, target, link string) {
	t.Helper()
	if err := os.Symlink(target, link); err != nil {
		t.Fatal(err)
	}
}
