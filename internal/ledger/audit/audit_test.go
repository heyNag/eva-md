package audit

import (
	"bytes"
	"context"
	"encoding/json"
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/heyNag/eva-md/internal/catchup"
	"github.com/heyNag/eva-md/internal/inbox"
	"github.com/heyNag/eva-md/internal/ledger/event"
	"github.com/heyNag/eva-md/internal/paths"
)

// marker is planted in every text, id, path, project and branch name the
// fixtures hold, so a report that leaks one is caught.
const marker = "zqmark"

// codexID is the Codex fixture's session id, a UUID as Codex names its
// files; it holds no marker, so the tests look for it whole.
const codexID = "0a1b2c3d-4e5f-4a6b-8c7d-9e0f1a2b3c4d"

// fixture is a made-up home with an inbox, recovery records and the hosts'
// folders of chats.
type fixture struct {
	t    *testing.T
	home string
	dirs paths.Dirs
	cwd  string
}

func newFixture(t *testing.T) *fixture {
	t.Helper()
	home, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	t.Setenv("HOME", home)
	t.Setenv("GIT_CONFIG_GLOBAL", filepath.Join(home, "gitconfig"))
	t.Setenv("GIT_CONFIG_NOSYSTEM", "1")
	f := &fixture{t: t, home: home, cwd: filepath.Join(home, marker+"-cwd"),
		dirs: paths.Dirs{Config: filepath.Join(home, ".config", "eva-md"), State: filepath.Join(home, ".local", "state", "eva-md"), Share: filepath.Join(home, ".local", "share", "eva-md")}}
	if err := os.MkdirAll(f.cwd, 0o700); err != nil {
		t.Fatal(err)
	}
	return f
}

// at is a time of the fixtures' day, minutes and seconds past 10:00 UTC.
func at(min, sec int) string {
	return time.Date(2026, 11, 2, 10, min, sec, 0, time.UTC).Format(time.RFC3339)
}

// store appends lines to the session's inbox file with v1's own writer.
func (f *fixture) store(host, id string, lines ...inbox.Entry) {
	f.t.Helper()
	for _, e := range lines {
		e.V, e.Host, e.Session = 1, host, id
		if e.Project == "" {
			e.Project = marker + "-project"
		}
		e.Branch, e.Cwd, e.Model = marker+"-branch", marker+"-cwd", marker+"-model"
		if e.Origin == "" {
			e.Origin = "hook"
		}
		if err := (inbox.Store{Root: f.dirs.Inbox()}).Append(e); err != nil {
			f.t.Fatal(err)
		}
	}
}

// transcript writes the host's record of a session in the host's folder of
// chats and returns its path.
func (f *fixture) transcript(host, id string, rows ...any) string {
	f.t.Helper()
	path := filepath.Join(f.home, ".claude", "projects", "-"+marker+"-cwd", id+".jsonl")
	if host == "codex" {
		path = filepath.Join(f.home, ".codex", "sessions", "2026", "11", "02", "rollout-2026-11-02T10-00-00-"+id+".jsonl")
	}
	var b bytes.Buffer
	for _, r := range rows {
		data, err := json.Marshal(r)
		if err != nil {
			f.t.Fatal(err)
		}
		b.Write(data)
		b.WriteByte('\n')
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		f.t.Fatal(err)
	}
	if err := os.WriteFile(path, b.Bytes(), 0o600); err != nil {
		f.t.Fatal(err)
	}
	return path
}

// register makes the session's recovery record as a hook does, naming the
// transcript at path ("" for none).
func (f *fixture) register(host, id, path string) {
	f.t.Helper()
	e := inbox.Entry{V: 1, Host: host, Session: id, Project: marker + "-project", Branch: marker + "-branch", Cwd: marker + "-cwd"}
	if err := catchup.Register(context.Background(), f.dirs, f.home, path, f.cwd, e, false, false, time.Date(2026, 11, 2, 9, 0, 0, 0, time.UTC)); err != nil {
		f.t.Fatal(err)
	}
}

// run audits with o's scope and returns the report and its text.
func (f *fixture) run(session string, last int) (Report, string) {
	f.t.Helper()
	r, err := Run(context.Background(), Options{Dirs: f.dirs, Home: f.home, TempDir: filepath.Join(f.home, "tmp"), Session: session, Last: last})
	if err != nil {
		f.t.Fatal(err)
	}
	var out bytes.Buffer
	if err := r.Write(&out); err != nil {
		f.t.Fatal(err)
	}
	return r, out.String()
}

// Claude Code's rows.
func cUser(turn, ts string, content any) map[string]any {
	return map[string]any{"type": "user", "sessionId": claudeID, "promptId": turn, "timestamp": ts, "cwd": "/" + marker,
		"message": map[string]any{"role": "user", "content": content}}
}

func cAssistant(turn, ts, stop string, content ...map[string]any) map[string]any {
	return map[string]any{"type": "assistant", "sessionId": claudeID, "promptId": turn, "timestamp": ts, "cwd": "/" + marker,
		"message": map[string]any{"role": "assistant", "model": marker + "-model", "stop_reason": stop, "content": content}}
}

func text(s string) map[string]any { return map[string]any{"type": "text", "text": s} }

func toolUse(id, name string) map[string]any {
	return map[string]any{"type": "tool_use", "id": id, "name": name, "input": map[string]any{"command": marker + " command"}}
}

const claudeID = marker + "-claude-1"

// claudeSession is a Claude Code chat with planted losses, extras and
// copies, a range said off the record, a voice hand-over, an interrupt and
// rows that are not counted. The comments give each line's part in the
// counts TestClaudeCounts expects.
func (f *fixture) claudeSession() {
	f.transcript("claude-code", claudeID,
		cUser("T0", "2026-11-02T09:00:00Z", marker+" before the first hook"), // before capture
		cUser("T1", at(0, 5), marker+" first prompt"),
		cAssistant("T1", at(0, 6), "tool_use", toolUse("tu1", "Bash")),
		cUser("T1", at(0, 7), []map[string]any{{"type": "tool_result", "tool_use_id": "tu1", "content": marker}}),
		cAssistant("T1", at(0, 8), "tool_use", toolUse("tu2", "Read")), // missing from the inbox
		cAssistant("T1", at(0, 8), "tool_use", toolUse("tu2", "Read")), // the same call again: counted once
		cAssistant("T1", at(0, 9), "tool_use", toolUse("tu3", "Task")), // a tool the hooks don't take
		cAssistant("T1", at(0, 10), "end_turn", text(marker+" first reply")),
		cUser("T2", at(1, 0), marker+" second prompt"),
		cAssistant("T2", at(1, 1), "tool_use", toolUse("tu4", "Bash")),
		cAssistant("T2", at(1, 2), "end_turn", text(marker+" second reply")), // missing: the stop held none
		cUser("T3", at(2, 0), marker+" third prompt"),
		cUser("T3", at(2, 1), "[Request interrupted by user]"),
		cUser("T4", at(3, 0), "off the record"),
		cAssistant("T4", at(3, 1), "end_turn", text(marker+" said off the record")),
		cUser("T5", at(3, 30), "<realtime_delegation>"+marker+" off the record</realtime_delegation>"),
		cAssistant("T5", at(3, 31), "tool_use", toolUse("tu5", "Bash")),
		cUser("T6", at(5, 0), "back on the record"),
		cAssistant("T6", at(5, 3), "end_turn", text(marker+" reply after the pause")),
		cUser("T7", at(6, 0), " <realtime_delegation>"+marker+" spoken</realtime_delegation>"),
		cAssistant("T7", at(6, 2), "end_turn", text(marker+" spoken reply")),
		cUser("T8", at(7, 0), "<bash-input>"+marker+"</bash-input>"), // the host's wrapper
		cUser("T8", at(7, 1), "/compact "+marker),                    // the host's own command
		map[string]any{"type": "user", "sessionId": claudeID, "isSidechain": true, "timestamp": at(7, 2), // a subagent's
			"message": map[string]any{"role": "user", "content": marker}},
		map[string]any{"type": "assistant", "sessionId": claudeID, "isApiErrorMessage": true, "timestamp": at(7, 3),
			"message": map[string]any{"role": "assistant", "stop_reason": "end_turn", "content": []any{text(marker)}}},
		map[string]any{"type": "user", "sessionId": marker + "-other", "promptId": "T9", "timestamp": at(7, 4), // another session's
			"message": map[string]any{"role": "user", "content": marker}},
	)
	f.register("claude-code", claudeID, filepath.Join(f.home, ".claude", "projects", "-"+marker+"-cwd", claudeID+".jsonl"))
	line := func(ts, turn, role, kind, txt string) inbox.Entry {
		return inbox.Entry{TS: ts, Turn: turn, Role: role, Kind: kind, Text: txt}
	}
	recovered := line(at(0, 12), "T1", "assistant", "reply", marker+" first reply")
	recovered.Origin = "catchup"
	f.store("claude-code", claudeID,
		line(at(0, 0), "", "event", "event", "session_start: startup"),
		line(at(0, 5), "T1", "user", "prompt", marker+" first prompt"),
		line(at(0, 6), "T1", "tool", "tool", "Bash "+marker),
		line(at(0, 11), "T1", "assistant", "reply", marker+" first reply"),
		recovered, // a copy: not counted, a repeat that pairs a hook's line with a recovered one
		line(at(1, 0), "T2", "user", "prompt", marker+" second prompt"),
		line(at(1, 1), "T2", "tool", "tool", "Bash "+marker),
		line(at(1, 1), "T2", "tool", "tool", "Bash "+marker), // extra, and a repeat of two hook lines
		line(at(1, 2), "T2", "event", "event", "gap: missing final reply"),
		line(at(2, 0), "T3", "user", "prompt", marker+" third prompt"), // interrupted, as the host's record shows
		line(at(3, 0), "T4", "event", "event", "off_record"),
		line(at(3, 30), "T5", "user", "prompt", marker+" off the record"), // withheld
		line(at(5, 0), "T6", "event", "event", "on_record"),
		line(at(5, 3), "T6", "assistant", "reply", marker+" reply after the pause"),
		line(at(6, 0), "T7", "user", "prompt", "<realtime_delegation>"+marker+" spoken</realtime_delegation>"),
		line(at(6, 2), "T7", "assistant", "reply", marker+" spoken reply"),
		line(at(8, 0), "T10", "user", "prompt", marker+" never in the host's record"), // extra, and no stop line
	)
}

// Codex's rows.
func cx(ts, typ string, payload map[string]any) map[string]any {
	return map[string]any{"timestamp": ts, "type": typ, "payload": payload}
}

// codexSession is a Codex chat with planted losses, a reply written in
// three rows, tool calls by several names, an interrupt, a failed stop and
// a tool line with no turn id.
func (f *fixture) codexSession() {
	f.transcript("codex", codexID,
		cx(at(0, 0), "session_meta", map[string]any{"id": codexID, "cwd": f.cwd}),
		cx(at(0, 1), "turn_context", map[string]any{"turn_id": "U1", "cwd": f.cwd}),
		cx(at(0, 1), "event_msg", map[string]any{"type": "task_started", "turn_id": "U1"}),
		cx(at(0, 2), "event_msg", map[string]any{"type": "user_message", "message": marker + " codex prompt"}),
		cx(at(0, 3), "response_item", map[string]any{"type": "function_call", "name": "shell", "call_id": "c1", "arguments": marker}),
		cx(at(0, 4), "response_item", map[string]any{"type": "custom_tool_call", "name": "apply_patch", "call_id": "c2", "input": marker}), // missing
		cx(at(0, 5), "response_item", map[string]any{"type": "function_call", "name": "view_image", "call_id": "c3"}),                      // not taken
		cx(at(0, 6), "response_item", map[string]any{"type": "message", "role": "assistant", "phase": "final", "content": []any{map[string]any{"type": "output_text", "text": marker}}}),
		cx(at(0, 6), "event_msg", map[string]any{"type": "agent_message", "phase": "final", "message": marker + " codex reply"}),
		cx(at(0, 7), "event_msg", map[string]any{"type": "task_complete", "turn_id": "U1", "last_agent_message": marker + " codex reply"}),
		cx(at(1, 0), "event_msg", map[string]any{"type": "task_started", "turn_id": "U2"}),
		cx(at(1, 1), "event_msg", map[string]any{"type": "user_message", "message": marker + " codex second"}),
		cx(at(1, 2), "response_item", map[string]any{"type": "function_call", "name": "exec_command", "call_id": "c4"}),
		cx(at(1, 3), "event_msg", map[string]any{"type": "turn_aborted", "turn_id": "U2"}),
		cx(at(2, 0), "event_msg", map[string]any{"type": "task_started", "turn_id": "U3"}),
		cx(at(2, 1), "event_msg", map[string]any{"type": "user_message", "message": "<realtime_delegation>" + marker + "</realtime_delegation>"}),
		cx(at(2, 5), "event_msg", map[string]any{"type": "task_complete", "turn_id": "U3", "last_agent_message": marker + " third reply"}), // missing: the stop failed
	)
	f.register("codex", codexID, filepath.Join(f.home, ".codex", "sessions", "2026", "11", "02", "rollout-2026-11-02T10-00-00-"+codexID+".jsonl"))
	line := func(ts, turn, role, kind, txt string) inbox.Entry {
		return inbox.Entry{TS: ts, Turn: turn, Role: role, Kind: kind, Text: txt}
	}
	f.store("codex", codexID,
		line(at(0, 0), "", "event", "event", "session_start: startup"),
		line(at(0, 2), "U1", "user", "prompt", marker+" codex prompt"),
		line(at(0, 3), "U1", "tool", "tool", "Bash "+marker),
		line(at(0, 7), "U1", "assistant", "reply", marker+" codex reply"),
		line(at(1, 1), "U2", "user", "prompt", marker+" codex second"),
		line(at(1, 2), "", "tool", "tool", "Bash "+marker), // no turn id: pairs with U2's call
		line(at(1, 3), "U2", "event", "event", "interrupt"),
		line(at(2, 1), "U3", "user", "prompt", "<realtime_delegation>"+marker+"</realtime_delegation>"),
		line(at(2, 5), "U3", "event", "event", "stop_failure: rate_limit"),
	)
}

// On a made-up Claude Code transcript and inbox with planted losses,
// extras and copies, the report gives the exact counts; nothing said off
// the record is counted by kind.
func TestClaudeCounts(t *testing.T) {
	f := newFixture(t)
	f.claudeSession()
	r, _ := f.run("", 0)
	want := [kinds]Coverage{
		Prompt: {Found: 4, Missing: 0, Extra: 1},
		Reply:  {Found: 3, Missing: 1, Extra: 0},
		Tool:   {Found: 2, Missing: 1, Extra: 1},
	}
	if r.Coverage != want || len(r.Rows) != 1 || r.Rows[0].Coverage != want || r.Rows[0].Host != "claude-code" {
		t.Errorf("coverage %+v, rows %+v, want %+v", r.Coverage, r.Rows, want)
	}
	if r.BeforeCapture != 1 || r.PausedInbox != 1 || r.PausedHost != 1 || r.VoiceInbox != 1 || r.VoiceHost != 1 || r.TooLong != 0 {
		t.Errorf("before %d, paused %d and %d, voice %d and %d, too long %d", r.BeforeCapture, r.PausedInbox, r.PausedHost, r.VoiceInbox, r.VoiceHost, r.TooLong)
	}
	if r.Repeats != [kinds]Repeats{Reply: {1, 1}, Tool: {1, 0}} {
		t.Errorf("repeats %+v", r.Repeats)
	}
	if r.Gaps != (Gaps{MissingReply: 1}) {
		t.Errorf("gaps %+v", r.Gaps)
	}
	if r.NoReply != (NoReply{Interrupted: 1, NoReplyInPayload: 1, HookNeverRan: 1}) {
		t.Errorf("no reply %+v", r.NoReply)
	}
	if r.Recovery != (Recovery{Paired: 1}) || r.Sessions != 1 || r.Files != 1 || r.Classes[event.ClassUnknown] != 1 {
		t.Errorf("recovery %+v, %d sessions, %d files, classes %v", r.Recovery, r.Sessions, r.Files, r.Classes)
	}
}

// The same for Codex's format: a reply Codex writes in three rows is one,
// a shell call is counted by its record's names, a tool line with no turn
// id pairs with an unpaired call, and the interrupt and failed stop are
// told apart.
func TestCodexCounts(t *testing.T) {
	f := newFixture(t)
	f.codexSession()
	r, _ := f.run("", 0)
	want := [kinds]Coverage{
		Prompt: {Found: 3},
		Reply:  {Found: 1, Missing: 1},
		Tool:   {Found: 2, Missing: 1},
	}
	if r.Coverage != want || len(r.Rows) != 1 || r.Rows[0].Host != "codex" {
		t.Errorf("coverage %+v, rows %+v, want %+v", r.Coverage, r.Rows, want)
	}
	if r.NoReply != (NoReply{Interrupted: 1, Failed: 1}) || r.VoiceInbox != 1 || r.VoiceHost != 1 || r.BeforeCapture != 0 {
		t.Errorf("no reply %+v, voice %d and %d, before %d", r.NoReply, r.VoiceInbox, r.VoiceHost, r.BeforeCapture)
	}
	if r.Repeats != [kinds]Repeats{} || r.Recovery != (Recovery{Paired: 1}) {
		t.Errorf("repeats %+v, recovery %+v", r.Repeats, r.Recovery)
	}
}

// Both sessions audited together add up, newest first, and --session and
// --last narrow the scope without counting the hosts' folders.
func TestScope(t *testing.T) {
	f := newFixture(t)
	f.claudeSession()
	f.codexSession()
	r, _ := f.run("", 0)
	if r.Sessions != 2 || len(r.Rows) != 2 || r.Rows[0].Host != "claude-code" || r.Coverage[Prompt] != (Coverage{Found: 7, Extra: 1}) || r.Stores == nil {
		t.Errorf("both: %d sessions, rows %+v, prompts %+v, stores %v", r.Sessions, r.Rows, r.Coverage[Prompt], r.Stores)
	}
	r, _ = f.run(codexID, 0)
	if r.Sessions != 1 || len(r.Rows) != 1 || r.Rows[0].Host != "codex" || r.Stores != nil {
		t.Errorf("--session: %d sessions, rows %+v, stores %v", r.Sessions, r.Rows, r.Stores)
	}
	r, out := f.run("", 1)
	if r.Sessions != 1 || len(r.Rows) != 1 || r.Rows[0].Host != "claude-code" || r.Stores != nil || !strings.Contains(out, "counted only when every session is audited") {
		t.Errorf("--last 1: %d sessions, rows %+v, stores %v", r.Sessions, r.Rows, r.Stores)
	}
}

// A planted marker in every text, id, path, project, branch and model the
// audit reads is in no line of its report, nor is a session id or the
// home, and the audit writes no file and changes none.
func TestTheReportHoldsNumbersOnly(t *testing.T) {
	f := newFixture(t)
	f.claudeSession()
	f.codexSession()
	f.store("claude-code", marker+"-sub", inbox.Entry{TS: at(9, 0), Turn: "S1", Role: "tool", Kind: "tool", Agent: marker + "-agent", Text: marker})
	refused := filepath.Join(f.home, marker+"-elsewhere", marker+"-refused.jsonl")
	f.register("claude-code", marker+"-refused", refused)
	f.store("claude-code", marker+"-refused", inbox.Entry{TS: at(9, 0), Role: "event", Kind: "event", Text: "session_start"})
	f.transcript("claude-code", marker+"-unknown", cUser("X", at(0, 0), marker))
	before := snapshot(t, f.home)
	_, out := f.run("", 0)
	for _, leak := range []string{marker, codexID, f.home, "realtime", "rate_limit"} {
		if strings.Contains(out, leak) {
			t.Errorf("the report holds %q:\n%s", leak, out)
		}
	}
	if after := snapshot(t, f.home); !slices.Equal(before, after) {
		t.Errorf("the audit changed the home:\n%q\n%q", before, after)
	}
	for _, want := range []string{"capture audit: 4 sessions in 4 inbox files", "session 1 (claude-code)", "session 2 (codex)", "path named and refused 1", "no recovery record 1"} {
		if !strings.Contains(out, want) {
			t.Errorf("the report lacks %q:\n%s", want, out)
		}
	}
}

// snapshot lists every file and folder under root with its size, mode and
// last write.
func snapshot(t *testing.T, root string) []string {
	t.Helper()
	var out []string
	err := filepath.WalkDir(root, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		info, err := d.Info()
		if err != nil {
			return err
		}
		out = append(out, p+" "+info.Mode().String()+" "+info.ModTime().String()+" "+strconv.FormatInt(info.Size(), 10))
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	return out
}
