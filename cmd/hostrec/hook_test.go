package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/heyNag/eva-md/internal/hostfacts"
)

// Everything in the session below is made up.
const (
	plantedSentence = "The planted sentence says the orchard owl sings at noon"
	madeUpModelID   = "made-up-model-qx7"
	sessionID       = "0f1e2d3c-4b5a-4968-8776-655443322110"
	turnID          = "1a2b3c4d-5e6f-4a7b-8c9d-0e1f2a3b4c5d"
	toolUseID       = "toolu_01MadeUpReadCall"
	madeUpAddress   = "git@git.madeup-forge.test:pears/hf-scratch-plum.git"
)

// transcript is a record file that grows between hooks.
type transcript struct {
	t    *testing.T
	path string
}

func newTranscript(t *testing.T, path string) *transcript {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	return &transcript{t, path}
}

func (tr *transcript) add(rows ...map[string]any) {
	tr.t.Helper()
	f, err := os.OpenFile(tr.path, os.O_WRONLY|os.O_APPEND|os.O_CREATE, 0o600)
	if err != nil {
		tr.t.Fatal(err)
	}
	defer f.Close()
	for _, r := range rows {
		r["model"] = madeUpModelID
		data, err := json.Marshal(r)
		if err != nil {
			tr.t.Fatal(err)
		}
		if _, err := f.Write(append(data, '\n')); err != nil {
			tr.t.Fatal(err)
		}
	}
}

// fields gives a snapshot row's fields by path.
func fields(row map[string]any) map[string]map[string]any {
	out := map[string]map[string]any{}
	for _, f := range row["fields"].([]any) {
		m := f.(map[string]any)
		out[m["path"].(string)] = m
	}
	return out
}

func snapshotRows(ev map[string]any) []map[string]any {
	rec, _ := ev["record"].(map[string]any)
	rows, _ := rec["rows"].([]any)
	var out []map[string]any
	for _, r := range rows {
		out = append(out, r.(map[string]any))
	}
	return out
}

func TestTheHookKeepsASessionAndNothingOfItsText(t *testing.T) {
	h := newHome(t)
	h.setup(t)
	if err := os.WriteFile(filepath.Join(h.shared, addressFile), []byte(madeUpAddress+"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	h.must(t, "y\n", "remote", "set")
	h.must(t, "", "mark", "C-04")
	h.must(t, "", "arm", "start")
	cwd := filepath.Join(h.home, "scratch", "alpha-orchard")
	tp := filepath.Join(h.home, ".claude", "projects", strings.ReplaceAll(cwd, "/", "-"), sessionID+".jsonl")
	tr := newTranscript(t, tp)
	names := "by ownerquill on Quills-Orchard-Mac for Quill Ownerby in /Users/ownerquill, and hfevaltest as Hazel Evaltest, at git.madeup-forge.test"
	base := func(event string) map[string]any {
		return map[string]any{"session_id": sessionID, "transcript_path": tp, "cwd": cwd, "hook_event_name": event, "model": madeUpModelID}
	}
	start := base("SessionStart")
	start["source"] = "startup"
	if answer := h.hook(t, "claude-code", "SessionStart", start); !strings.Contains(answer, startWord) {
		t.Errorf("the start answered %s", answer)
	}
	prompt := base("UserPromptSubmit")
	prompt["prompt"] = "Made-up test. " + plantedSentence + " " + names
	prompt["prompt_id"] = turnID
	h.hook(t, "claude-code", "UserPromptSubmit", prompt)
	tr.add(map[string]any{"type": "user", "sessionId": sessionID, "promptId": turnID, "cwd": cwd, "gitBranch": "main", "version": "9.9.9",
		"userType": "external", "entrypoint": "cli", "timestamp": "2026-05-01T10:00:00.000Z",
		"message": map[string]any{"role": "user", "content": prompt["prompt"]}})
	tr.add(map[string]any{"type": "assistant", "sessionId": sessionID, "cwd": cwd, "message": map[string]any{"role": "assistant", "content": []any{
		map[string]any{"type": "text", "text": "I will read it. " + plantedSentence},
		map[string]any{"type": "tool_use", "id": toolUseID, "name": "Read", "input": map[string]any{"file_path": cwd + "/orchard/plums.txt"}},
	}}})
	pre := base("PreToolUse")
	pre["tool_name"], pre["tool_use_id"] = "Read", toolUseID
	pre["tool_input"] = map[string]any{"file_path": cwd + "/orchard/plums.txt"}
	h.hook(t, "claude-code", "PreToolUse", pre)
	tr.add(map[string]any{"type": "user", "sessionId": sessionID, "cwd": cwd, "toolUseResult": map[string]any{"file": map[string]any{"filePath": cwd + "/orchard/plums.txt", "content": plantedSentence}},
		"message": map[string]any{"role": "user", "content": []any{map[string]any{"type": "tool_result", "tool_use_id": toolUseID, "content": plantedSentence + " " + names}}}})
	post := base("PostToolUse")
	post["tool_name"], post["tool_use_id"] = "Read", toolUseID
	post["tool_response"] = map[string]any{"content": plantedSentence + " " + names}
	h.hook(t, "claude-code", "PostToolUse", post)
	reply := "Made-up reply. " + plantedSentence + ", twice."
	tr.add(map[string]any{"type": "assistant", "sessionId": sessionID, "message": map[string]any{"role": "assistant", "content": []any{map[string]any{"type": "text", "text": reply}}}})
	stop := base("Stop")
	stop["last_assistant_message"], stop["stop_hook_active"] = reply, false
	h.hook(t, "claude-code", "Stop", stop)

	events := h.events(t)
	if len(events) != 5 {
		t.Fatalf("%d events, want 5", len(events))
	}
	rows := 0
	for i, ev := range events {
		if ev["mark"] != "C-04" || ev["host"] != "claude-code" || ev["record"] == nil || ev["payload_hashes"] == nil || ev["env_names"] == nil {
			t.Errorf("event %d lacks its parts: %v", i, ev["event"])
		}
		rows += len(snapshotRows(ev))
	}
	if rows != 4 {
		t.Errorf("the snapshots describe %d rows, want each of 4 once", rows)
	}
	if events[0]["answer"] != "start-word" || events[0]["arm"] != "start" {
		t.Errorf("the start's arm and answer: %v %v", events[0]["arm"], events[0]["answer"])
	}
	if g, _ := events[1]["git"].(map[string]any); g == nil || g["repo"] != true || g["common"] != "own" || g["detached"] != nil {
		t.Errorf("the git shape: %v", events[1]["git"])
	}
	// The planted sentence is nowhere but inside the payloads that carried
	// it, and the model's name is nowhere at all: a payload keeps it as its
	// keyed hash.
	if strings.Contains(h.recFiles(t)[hostfacts.EventsFile], madeUpModelID) {
		t.Error("a payload holds the model's name")
	}
	for name, text := range h.recFiles(t) {
		if name == hostfacts.EventsFile {
			continue
		}
		for _, bad := range []string{plantedSentence, "orchard owl", madeUpModelID} {
			if strings.Contains(text, bad) {
				t.Errorf("%s holds %q", name, bad)
			}
		}
	}
	for i, ev := range events {
		rest := map[string]any{}
		for k, v := range ev {
			if k != "payload" {
				rest[k] = v
			}
		}
		data, _ := json.Marshal(rest)
		for _, bad := range []string{plantedSentence, "orchard owl", madeUpModelID, "plums.txt"} {
			if strings.Contains(string(data), bad) {
				t.Errorf("event %d holds %q outside its payload", i, bad)
			}
		}
	}
	// Ids and the reply's hash join payloads to rows.
	stopEv := events[4]
	sr := snapshotRows(events[2])
	if len(sr) != 2 || fields(sr[0])["promptId"]["value"] != turnID || fields(sr[1])["message.content.1.id"]["value"] != toolUseID {
		t.Errorf("the ids are not on the rows: %v", sr)
	}
	last := snapshotRows(stopEv)
	if len(last) != 1 {
		t.Fatalf("the stop's snapshot has %d rows", len(last))
	}
	if h := stopEv["payload_hashes"].(map[string]any)["last_assistant_message"]; h == nil || h != fields(last[0])["message.content.0.text"]["hash"] {
		t.Errorf("the reply's hash does not join: %v", h)
	}
	presence := stopEv["record"].(map[string]any)["presence"].(map[string]any)
	if presence["reply_row"] != true || presence["prompt_row"] != true {
		t.Errorf("presence: %v", presence)
	}
	if f := fields(sr[0])["message.content"]; f["value"] != nil || f["hash"] != events[1]["payload_hashes"].(map[string]any)["prompt"] {
		t.Errorf("the prompt row's text: %v", f)
	}
	// No name of this machine, its accounts or the scratch remote is in
	// any file of the recording, payloads included.
	all := ""
	for _, text := range h.recFiles(t) {
		all += strings.ToLower(text) + "\n"
	}
	for _, bad := range []string{fakeEval.Short, "hazel evaltest", strings.ToLower(h.home), strings.ToLower(strings.ReplaceAll(h.home, "/", "-")),
		fakeOther.Short, "quill ownerby", "/users/ownerquill", "quills-orchard-mac", "git.madeup-forge.test", "pears/hf-scratch-plum"} {
		if strings.Contains(all, bad) {
			t.Errorf("the recording holds %q", bad)
		}
	}
	for _, place := range []string{hostfacts.PlaceEvalHome, hostfacts.PlaceOtherUser, hostfacts.PlaceComputer, hostfacts.PlaceHost} {
		if !strings.Contains(all, strings.ToLower(place)) {
			t.Errorf("no %s in the recording: was the name kept out some other way?", place)
		}
	}
	// With the sitting closed, the hook writes nothing.
	h.must(t, "", "close")
	before := h.recFiles(t)
	if answer := h.hook(t, "claude-code", "SessionStart", start); answer != "{}" {
		t.Errorf("the hook answered %s with no sitting open", answer)
	}
	if after := h.recFiles(t); !mapsEqual(before, after) {
		t.Error("the hook wrote with no sitting open")
	}
}

func TestTheHookFailsClosed(t *testing.T) {
	h := newHome(t)
	h.setup(t)
	h.must(t, "", "mark", "C-02")
	h.must(t, "", "arm", "start")
	for _, c := range []struct {
		name, payload string
		args          []string
	}{
		{"a malformed payload", `{"session_id": "made-up", `, []string{"hook", "--host", "claude-code", "--event", "SessionStart"}},
		{"a payload that is not an object", `["startup"]`, []string{"hook", "--host", "codex", "--event", "SessionStart"}},
		{"no host", `{"source":"startup"}`, []string{"hook", "--event", "SessionStart"}},
		{"another host", `{"source":"startup"}`, []string{"hook", "--host", "made-up-host", "--event", "SessionStart"}},
	} {
		code, out, errOut := h.run(t, c.payload, c.args...)
		if code != 0 || strings.TrimSpace(out) != "{}" || errOut != "" {
			t.Errorf("%s: %d %q %q", c.name, code, out, errOut)
		}
	}
	files := h.recFiles(t)
	if files[hostfacts.EventsFile] != "" {
		t.Error("an event was kept from a payload that does not read")
	}
	if n := strings.Count(files[hostfacts.ErrorsFile], "\n"); n != 4 || strings.Contains(files[hostfacts.ErrorsFile], "made-up") {
		t.Errorf("errors.jsonl: %q", files[hostfacts.ErrorsFile])
	}
	// A recording folder the hook cannot write to: it answers {}.
	if err := os.Chmod(h.rec, 0o500); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.Chmod(h.rec, 0o700) })
	for _, name := range []string{hostfacts.EventsFile, hostfacts.ErrorsFile, progressFile, hashesFile, lockFile} {
		os.Chmod(filepath.Join(h.rec, name), 0o400)
	}
	code, out, errOut := h.run(t, `{"source":"startup","session_id":"made-up"}`, "hook", "--host", "claude-code", "--event", "SessionStart")
	if code != 0 || errOut != "" || (strings.TrimSpace(out) != "{}" && os.Geteuid() != 0) {
		t.Errorf("an unwritable recording: %d %q %q", code, out, errOut)
	}
}

// Fed a Codex rollout and a Claude Code record, the hook keeps the values
// the core facts read, an enum-shaped value at a path off the list on those
// rows, an unlisted leading tag on a user row no payload matches, and no
// free text.
func TestTheHookKeepsTheValuesTheCoreFactsRead(t *testing.T) {
	h := newHome(t)
	h.setup(t)
	h.must(t, "", "mark", "X-04")
	cwd := filepath.Join(h.home, "scratch", "alpha-orchard")
	rollout := newTranscript(t, filepath.Join(h.home, ".codex", "sessions", "2026", "05", "01", "rollout-2026-05-01T10-00-00-"+sessionID+".jsonl"))
	rollout.add(
		map[string]any{"type": "session_meta", "payload": map[string]any{"id": sessionID, "originator": "made_up_originator", "cwd": cwd, "source": "made_up_source", "instructions": plantedSentence}},
		map[string]any{"type": "event_msg", "payload": map[string]any{"type": "task_started", "turn_id": "turn-made-up-2",
			"turn_attribution": map[string]any{"turn_trigger": "made_up_trigger", "parent_turn_id": "turn-made-up-1", "root_turn_id": "turn-made-up-1"},
			"made_up_field":    "made_up_enum"}},
		map[string]any{"type": "realtime_item", "payload": map[string]any{"type": "message", "role": "made_up_role", "outcome": "made_up_outcome", "text": plantedSentence}},
		map[string]any{"type": "event_msg", "payload": map[string]any{"type": "user_message", "message": "[made_up_notice] " + plantedSentence}},
	)
	payload := map[string]any{"session_id": sessionID, "turn_id": "turn-made-up-2", "transcript_path": rollout.path, "cwd": cwd, "hook_event_name": "Stop",
		"last_assistant_message": "made up"}
	h.hook(t, "codex", "Stop", payload)
	claude := newTranscript(t, filepath.Join(h.home, ".claude", "projects", "-made-up", sessionID+".jsonl"))
	claude.add(map[string]any{"type": "user", "entrypoint": "made-up-entrypoint", "userType": "made_up_user", "message": map[string]any{"role": "user", "content": "<made_up_wrapper>" + plantedSentence}})
	h.hook(t, "claude-code", "Stop", map[string]any{"session_id": sessionID, "transcript_path": claude.path, "cwd": cwd})
	events := h.events(t)
	if len(events) != 2 {
		t.Fatalf("%d events", len(events))
	}
	cx := snapshotRows(events[0])
	cc := snapshotRows(events[1])
	if len(cx) != 4 || len(cc) != 1 {
		t.Fatalf("rows: %d and %d", len(cx), len(cc))
	}
	want := []struct {
		row   map[string]any
		path  string
		value any
	}{
		{cx[0], "payload.originator", "made_up_originator"},
		{cx[0], "payload.source", "made_up_source"},
		{cx[1], "payload.turn_attribution.turn_trigger", "made_up_trigger"},
		{cx[1], "payload.made_up_field", "made_up_enum"},
		{cx[2], "payload.role", "made_up_role"},
		{cx[2], "payload.outcome", "made_up_outcome"},
		{cc[0], "entrypoint", "made-up-entrypoint"},
		{cc[0], "userType", "made_up_user"},
	}
	for _, w := range want {
		if got := fields(w.row)[w.path]["value"]; got != w.value {
			t.Errorf("%s: %v, want %v", w.path, got, w.value)
		}
	}
	if d := cx[1]["derived"].(map[string]any); d["parent_turn_id_present"] != true || d["root_turn_id_is_turn_id"] != false {
		t.Errorf("the turn-start row's booleans: %v", d)
	}
	if f := fields(cx[3])["payload.message"]; f["tag"] != "made_up_notice" || f["value"] != nil {
		t.Errorf("an unlisted notice on a user row: %v", f)
	}
	if f := fields(cc[0])["message.content"]; f["tag"] != "made_up_wrapper" || f["value"] != nil {
		t.Errorf("an unlisted wrapper on a user row: %v", f)
	}
	data, _ := json.Marshal([]any{events[0]["record"], events[1]["record"]})
	if strings.Contains(string(data), "orchard owl") {
		t.Error("a row's free text is kept")
	}
}

// Every scratch hook ends well inside the limit its plugin declares: its
// budget is half that limit at most, its wait for the lock a third of its
// budget, and only the recorder's own SessionStart hook, whose limit is
// long, adds the start word's pause, in the deadline main sets for it.
// Codex's Interrupt, declared at one second, ends in half of one.
func TestEveryHookEndsWellInsideItsLimit(t *testing.T) {
	tools := "/made/up/hf-tools"
	n := 0
	for host, hooks := range map[string]map[string]any{hostClaude: claudeHooks(tools), hostCodex: codexHooks(tools)} {
		for event, groups := range hooks["hooks"].(map[string]any) {
			for _, g := range groups.([]hookGroup) {
				for _, h := range g["hooks"].([]hookEntry) {
					cmd := h["command"].(string)
					via := ""
					if strings.HasSuffix(cmd, "hf-exec-check") {
						via = "exec-check"
					} else {
						flags, _, err := parseArgs(strings.Fields(strings.ReplaceAll(cmd[strings.Index(cmd, " hook ")+6:], "'", "")), []string{"host", "event", "via"}, nil)
						if err != nil || flags["host"] != host || flags["event"] != event {
							t.Fatalf("%s %s: %q does not read: %v", host, event, cmd, err)
						}
						via = flags["via"]
					}
					limit := time.Duration(h["timeout"].(int)) * time.Second
					if got := declaredLimit(host, event, via); got != limit {
						t.Errorf("%s %s %s: the hook takes its limit as %v, the plugin declares %v", host, event, via, got, limit)
					}
					budget := budgetFor(host, event, via)
					// The deadline main sets, with the start word's pause
					// as a sitting has it: only the recorder's own
					// SessionStart hook waits it.
					end := hookDeadline([]string{"--host", host, "--event", event, "--via", via}, startWordPause)
					want := budget
					if event == "SessionStart" && via == "" {
						want += startWordPause
					}
					if end != want {
						t.Errorf("%s %s %s: main ends the hook at %v, want %v", host, event, via, end, want)
					}
					if budget > limit/2 || end >= limit || lockShare(budget) >= budget {
						t.Errorf("%s %s %s: budget %v, end %v, lock wait %v, for a limit of %v", host, event, via, budget, end, lockShare(budget), limit)
					}
					n++
				}
			}
		}
	}
	if n < len(claudeEvents)+len(codexEvents) {
		t.Errorf("only %d hooks read", n)
	}
	if b := budgetFor(hostCodex, "Interrupt", ""); b >= time.Second || b > 500*time.Millisecond {
		t.Errorf("Codex's Interrupt has a budget of %v", b)
	}
}

// The hooks' progress through a record file moves on only once the event
// that describes its rows is written: a hook that could not write its event
// leaves the rows for the next.
func TestTheHookMovesOnOnlyOnceTheEventIsWritten(t *testing.T) {
	h := newHome(t)
	h.setup(t)
	h.must(t, "", "mark", "C-02")
	// The mark is put an hour back, so a file's time, which a file system
	// may keep coarser than the clock, is after it whatever the machine.
	st := filepath.Join(h.rec, hostfacts.StateFile)
	data, err := os.ReadFile(st)
	if err != nil {
		t.Fatal(err)
	}
	var s state
	if err := json.Unmarshal(data, &s); err != nil {
		t.Fatal(err)
	}
	s.MarkWall = time.Now().Add(-time.Hour).UTC().Format(time.RFC3339Nano)
	if data, err = json.Marshal(s); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(st, data, 0o600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("HF_PROGRAM_MARK", "made-up")
	tr := newTranscript(t, filepath.Join(h.home, ".claude", "projects", "-made-up", sessionID+".jsonl"))
	tr.add(map[string]any{"type": "user", "message": map[string]any{"role": "user", "content": "made up"}})
	payload := map[string]any{"session_id": sessionID, "transcript_path": tr.path, "hook_event_name": "PreToolUse"}
	// An events file that cannot be written: a folder in its place, which
	// stops root too.
	events := filepath.Join(h.rec, hostfacts.EventsFile)
	if err := os.Mkdir(events, 0o700); err != nil {
		t.Fatal(err)
	}
	if answer := h.hook(t, "claude-code", "PreToolUse", payload); answer != "{}" {
		t.Errorf("the hook answered %s", answer)
	}
	if err := os.Remove(events); err != nil {
		t.Fatal(err)
	}
	h.hook(t, "claude-code", "PreToolUse", payload)
	evs := h.events(t)
	if len(evs) != 1 || len(snapshotRows(evs[0])) != 1 {
		t.Fatalf("the row the first hook could not keep is not in the second's event: %d events", len(evs))
	}
	// The transcript, added under the host's folder after the mark, is
	// counted by its name pattern, and the made-up mark is named.
	if fa, _ := evs[0]["files_added"].(map[string]any); fa["projects/*/*.jsonl"] != float64(1) {
		t.Errorf("files added: %v", evs[0]["files_added"])
	}
	if marks, _ := evs[0]["env_marks"].([]any); len(marks) != 1 || marks[0] != "HF_PROGRAM_MARK" {
		t.Errorf("env marks: %v", evs[0]["env_marks"])
	}
}

// The hook's answer is written once, whether its own or the budget's.
func TestFirstWriteOnly(t *testing.T) {
	var b strings.Builder
	f := &firstWrite{w: &b}
	for _, s := range []string{"{\"a\":1}\n", "{}\n"} {
		if n, err := f.Write([]byte(s)); err != nil || n != len(s) {
			t.Fatal(n, err)
		}
	}
	if b.String() != "{\"a\":1}\n" {
		t.Errorf("%q", b.String())
	}
}

// The hook's parent process is read from the system's process table.
func TestParentProcess(t *testing.T) {
	p, err := parent(os.Getpid())
	if err != nil {
		t.Fatal(err)
	}
	if p.name == "" || p.born.IsZero() || !p.born.Before(time.Now().Add(time.Minute)) {
		t.Errorf("this process: %+v", p)
	}
	for _, c := range []struct {
		args  []string
		login bool
	}{{[]string{"-zsh"}, true}, {[]string{"/bin/zsh", "-l", "-c", "x"}, true}, {[]string{"bash", "-lc", "x"}, true}, {[]string{"sh", "-c", "x -l"}, false}, {[]string{"/bin/sh", "-c", "hostrec hook"}, false}} {
		if got := loginArgs(c.args); got != c.login {
			t.Errorf("%v: login %v", c.args, got)
		}
	}
	if a, b := nanotime(), nanotime(); b < a || a <= 0 {
		t.Errorf("the monotonic clock: %d then %d", a, b)
	}
}

func TestNamePatternAndGitShape(t *testing.T) {
	for in, want := range map[string]string{
		"projects/-Users-x/abc.jsonl":                    "projects/*/*.jsonl",
		"sessions/2026/05/01/rollout-2026.jsonl":         "sessions/*/*/*/*.jsonl",
		"history.jsonl":                                  "*.jsonl",
		"Weird Dir/file.TXT":                             "*/*.txt",
		"todos/" + strings.Repeat("a", 3) + ".json-made": "todos/*",
	} {
		if got := namePattern(in); got != want {
			t.Errorf("%s: %s, want %s", in, got, want)
		}
	}
	h := newHome(t)
	h.setup(t)
	repo := filepath.Join(h.home, "scratch", "alpha-orchard")
	if g := gitShape(filepath.Join(repo, "orchard")); !g.Repo || g.Detached || g.Common != "own" {
		t.Errorf("a folder in a repository: %+v", g)
	}
	wt := filepath.Join(h.home, "made-up-worktree")
	if out, err := gitIn(repo, "worktree", "add", "--detach", wt); err != nil {
		t.Fatalf("%v %s", err, out)
	}
	if g := gitShape(wt); !g.Repo || !g.Detached || g.Common != "shared" {
		t.Errorf("a detached worktree: %+v", g)
	}
	if g := gitShape(filepath.Join(h.home, "scratch", "outside")); g.Repo {
		t.Errorf("a folder outside any repository: %+v", g)
	}
}
