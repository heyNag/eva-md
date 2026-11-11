package hostfacts

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"sort"
	"strings"
	"testing"
	"time"
)

// Values of the made-up recording, each of which no fixture may hold.
const (
	recPrompt   = "Made-up test prompt about plums and pears"
	recReply    = "Made-up reply: the plum list counts twelve"
	recCwd      = "/Users/madeupowner/scratch/alpha-orchard"
	recPathKey  = "/Users/madeupowner/scratch/alpha-orchard/orchard/plums.txt"
	recToolID   = "toolu_01MadeUpToolUse"
	recTurnID   = "7a6b5c4d-3e2f-4a1b-9c8d-7e6f5a4b3c2d"
	recBigCount = 48213
)

// madeUpRecording writes a recording as the hook writes one: two events
// whose payloads carry a session id, a turn id, a tool-use id, the prompt
// and the reply, and whose snapshots hold rows with the same ids and the
// reply's hash.
func madeUpRecording(t *testing.T) string {
	t.Helper()
	key := []byte("the recording's made-up key")
	rec := t.TempDir()
	transcript := writeRows(t,
		map[string]any{"type": "user", "sessionId": sessionUUID, "timestamp": "2026-05-01T10:00:00.250Z", "promptId": recTurnID,
			"message": map[string]any{"role": "user", "content": recPrompt}},
		map[string]any{"type": "assistant", "sessionId": sessionUUID, "timestamp": "2026-05-01T10:00:03.500Z",
			"message": map[string]any{"role": "assistant", "model": madeUpModel, "content": []any{
				map[string]any{"type": "tool_use", "id": recToolID, "name": "Read", "input": map[string]any{"file_path": recPathKey}},
				map[string]any{"type": "text", "text": recReply},
			}}},
	)
	repl := NewReplacer(Names{})
	write := func(name string, v any) {
		line, err := repl.Line(v)
		if err != nil {
			t.Fatal(err)
		}
		f, err := os.OpenFile(filepath.Join(rec, name), os.O_WRONLY|os.O_APPEND|os.O_CREATE, 0o600)
		if err != nil {
			t.Fatal(err)
		}
		defer f.Close()
		if _, err := f.Write(line); err != nil {
			t.Fatal(err)
		}
	}
	write(MarksFile, MarkLine{V: 1, Kind: "mark", Mark: "C-04", Wall: "2026-05-01T09:59:58.125Z", MonoNS: 5_000_000_000, UTCOffsetS: 7200,
		Versions: map[string]string{"claude-code": "9.9.9 (made up)", "codex": "absent"}})
	prompt := map[string]any{"hook_event_name": "UserPromptSubmit", "session_id": sessionUUID, "prompt_id": recTurnID, "prompt": recPrompt,
		"cwd": recCwd, "transcript_path": "/Users/madeupowner/.claude/projects/-Users-madeupowner-scratch/" + sessionUUID + ".jsonl",
		"model": madeUpModel, "permission_mode": "default", "tool_input": map[string]any{recPathKey: recBigCount}, "started": "2026-05-01T12:00:00+02:00",
		"local_time": "2026-05-01T12:00:00", "epoch_ms": json.Number("1777629600123")}
	stop := map[string]any{"hook_event_name": "Stop", "session_id": sessionUUID, "prompt_id": recTurnID, "last_assistant_message": recReply,
		"tool_use_id": recToolID, "stop_hook_active": false, "cwd": recCwd, "list": []any{"a", "bb", recCwd}}
	for i, p := range []map[string]any{prompt, stop} {
		decoded, err := Decode([]byte(mustJSON(t, p)))
		if err != nil {
			t.Fatal(err)
		}
		in := SnapshotInput{Host: "claude-code", Path: transcript, Key: key, MaxBytes: 1 << 20, PayloadHashes: map[string]bool{}}
		if i == 1 {
			in.Reply = Hash(key, recReply)
		}
		snap, _, _, err := TakeSnapshot(in)
		if err != nil {
			t.Fatal(err)
		}
		cost := 12.5
		login := false
		write(EventsFile, Event{V: 1, Mark: "C-04", Wall: fmt.Sprintf("2026-05-01T10:00:0%d.%03dZ", i+1, 100*i+7), MonoNS: 7_000_000_000 + int64(i)*1_500_000_000,
			Host: "claude-code", Event: p["hook_event_name"].(string), Payload: decoded, PayloadHashes: PayloadHashes(key, decoded),
			Proc: &Proc{Parent: "zsh", Login: &login, StartCostMS: &cost}, Git: &GitShape{Repo: true, Common: "own"},
			EnvNames: []string{"HOME", "PATH", "MADE_UP_VARIABLE_NAME"}, FilesAdded: map[string]int{"projects/*/*.jsonl": 1}, Record: &snap})
	}
	return rec
}

func mustJSON(t *testing.T, v any) string {
	t.Helper()
	data, err := Marshal(v)
	if err != nil {
		t.Fatal(err)
	}
	return string(data)
}

// skeleton is a value's form: kept keys, the count of the others, types,
// array lengths and kept values.
func skeleton(v any) string {
	switch t := v.(type) {
	case map[string]any:
		var parts []string
		other := 0
		for k, x := range t {
			if kept(k) {
				parts = append(parts, k+":"+skeleton(x))
			} else {
				other++
				parts = append(parts, "*:"+skeleton(x))
			}
		}
		sort.Strings(parts)
		return fmt.Sprintf("{%d %s}", other, strings.Join(parts, ","))
	case []any:
		var parts []string
		for _, x := range t {
			parts = append(parts, skeleton(x))
		}
		return "[" + strings.Join(parts, ",") + "]"
	case string:
		return "s"
	case json.Number:
		return "n"
	case bool:
		return fmt.Sprint(t)
	case nil:
		return "null"
	}
	return "?"
}

func readJSONLines(t *testing.T, path string) []any {
	t.Helper()
	lines, err := readLines(path)
	if err != nil {
		t.Fatal(err)
	}
	return lines
}

func at(v any, keys ...any) any {
	for _, k := range keys {
		switch k := k.(type) {
		case string:
			v = v.(map[string]any)[k]
		case int:
			v = v.([]any)[k]
		}
	}
	return v
}

// fieldValue finds a snapshot field by its path in a decoded event.
func fieldValue(t *testing.T, ev any, row int, path, what string) any {
	t.Helper()
	for _, f := range at(ev, "record", "rows", row, "fields").([]any) {
		if f.(map[string]any)["path"] == path {
			return f.(map[string]any)[what]
		}
	}
	t.Fatalf("row %d has no field %s", row, path)
	return nil
}

func TestFixturesReplaceEveryValueAndKeepTheShape(t *testing.T) {
	rec := madeUpRecording(t)
	out := filepath.Join(t.TempDir(), "claude-code", "9.9.9")
	if _, err := WriteFixtures(FixtureOptions{Rec: rec, Out: out, Seed: []byte("a made-up seed")}); err != nil {
		t.Fatal(err)
	}
	recorded := readJSONLines(t, filepath.Join(rec, EventsFile))
	fixtures := readJSONLines(t, filepath.Join(out, EventsFile))
	if len(recorded) != len(fixtures) {
		t.Fatalf("%d events against %d", len(recorded), len(fixtures))
	}
	for i := range recorded {
		if a, b := skeleton(recorded[i]), skeleton(fixtures[i]); a != b {
			t.Errorf("event %d's shape changed:\n%s\n%s", i, a, b)
		}
	}
	prompt, stop := fixtures[0], fixtures[1]
	// Ids still join across events and rows.
	sid := at(prompt, "payload", "session_id")
	if sid == sessionUUID || sid != at(stop, "payload", "session_id") || sid != fieldValue(t, stop, 0, "sessionId", "value") {
		t.Errorf("the session id does not join: %v %v", sid, at(stop, "payload", "session_id"))
	}
	if !regexp.MustCompile(`^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$`).MatchString(sid.(string)) {
		t.Errorf("the session id is not a made-up UUID: %v", sid)
	}
	tool := at(stop, "payload", "tool_use_id")
	if tool == recToolID || !strings.HasPrefix(tool.(string), "toolu_") || len(tool.(string)) != len(recToolID) || tool != fieldValue(t, stop, 1, "message.content.0.id", "value") {
		t.Errorf("the tool-use id does not join: %v", tool)
	}
	if at(prompt, "payload", "prompt_id") != fieldValue(t, prompt, 0, "promptId", "value") {
		t.Error("the turn id does not join")
	}
	replyHash := at(stop, "payload_hashes", "last_assistant_message")
	if replyHash != fieldValue(t, stop, 1, "message.content.1.text", "hash") {
		t.Errorf("the reply's hash does not join the payload to its row")
	}
	if at(stop, "record", "presence", "reply_row") != true {
		t.Error("the reply's presence is lost")
	}
	// A key shaped like a path is replaced like any value.
	counts := at(prompt, "payload", "tool_input").(map[string]any)
	for k, v := range counts {
		if k == recPathKey || !strings.HasPrefix(k, "/") || strings.Count(k, "/") != strings.Count(recPathKey, "/") || len(k) != len(recPathKey) {
			t.Errorf("the path key is %q", k)
		}
		if v == json.Number(fmt.Sprint(recBigCount)) || len(v.(json.Number)) != 5 {
			t.Errorf("the number is %v", v)
		}
	}
	// Host words stay, and a model or a mode's value does not, even when it
	// reads as a host word.
	if at(prompt, "payload", "hook_event_name") != "UserPromptSubmit" || at(prompt, "event") != "UserPromptSubmit" || at(stop, "payload", "stop_hook_active") != false {
		t.Error("a host word was replaced")
	}
	if m := at(prompt, "payload", "model"); m == madeUpModel || len(m.(string)) != len(madeUpModel) {
		t.Errorf("the model is %v", m)
	}
	// Times are in UTC with Z from the made-up epoch, and keep their gaps.
	var times []time.Time
	for _, ev := range fixtures {
		walk(ev, nil, func(_ []string, x any) {
			s, ok := x.(string)
			if !ok {
				return
			}
			if _, ts := shapeOf(s); len(ts) > 0 {
				for _, tt := range ts {
					if !strings.HasSuffix(tt, "Z") {
						t.Errorf("a time not in UTC with Z: %s", tt)
					}
					p, ok := parseTime(tt, 0)
					if !ok || p.Before(Epoch) || !p.Before(EpochEnd) {
						t.Errorf("a time off the epoch: %s", tt)
					}
					times = append(times, p)
				}
			}
		})
	}
	if len(times) == 0 {
		t.Fatal("no time found")
	}
	if first := slices.MinFunc(times, func(a, b time.Time) int { return a.Compare(b) }); !first.Equal(Epoch) {
		t.Errorf("the first time maps to %v, not the epoch", first)
	}
	w0, _ := time.Parse(time.RFC3339Nano, at(prompt, "wall").(string))
	w1, _ := time.Parse(time.RFC3339Nano, at(stop, "wall").(string))
	if w1.Sub(w0) != time.Second+100*time.Millisecond {
		t.Errorf("the gap between the events is %v", w1.Sub(w0))
	}
	if m := at(prompt, "mono_ns").(json.Number); m != json.Number(fmt.Sprint(MonoBase)) {
		t.Errorf("the first monotonic reading is %s", m)
	}
	if Epoch.Second() == 0 || Epoch.Year() >= 2026 {
		t.Error("the epoch is a whole minute, or not before any recording")
	}
	survived, err := CheckSurvival(rec, out)
	if err != nil {
		t.Fatal(err)
	}
	made, err := CheckFixtureDir(out)
	if err != nil {
		t.Fatal(err)
	}
	if len(survived)+len(made) != 0 {
		t.Fatalf("the fixtures fail: %v %v", survived, made)
	}
	// Each recorded value put back, as a value or as a key, fails both
	// checks; the repository's check needs no recording.
	data, err := os.ReadFile(filepath.Join(out, EventsFile))
	if err != nil {
		t.Fatal(err)
	}
	madeKey := ""
	for k := range counts {
		madeKey = k
	}
	for _, c := range []struct{ name, from, to string }{
		{"a value", sid.(string), sessionUUID},
		{"a key", madeKey, recPathKey},
		{"inside a longer value", at(prompt, "payload", "prompt").(string), "the reply was " + recReply + " and more"},
	} {
		t.Run(c.name, func(t *testing.T) {
			if !strings.Contains(string(data), c.from) {
				t.Fatalf("the fixture has no %q", c.from)
			}
			bad := t.TempDir()
			for _, name := range []string{EventsFile, "fixture.json"} {
				b, err := os.ReadFile(filepath.Join(out, name))
				if err != nil {
					t.Fatal(err)
				}
				if name == EventsFile {
					b = []byte(strings.Replace(string(b), c.from, c.to, 1))
				}
				if err := os.WriteFile(filepath.Join(bad, name), b, 0o644); err != nil {
					t.Fatal(err)
				}
			}
			if s, err := CheckSurvival(rec, bad); err != nil || len(s) == 0 {
				t.Errorf("check passes with %s put back: %v", c.name, err)
			}
			if m, err := CheckFixtureDir(bad); err != nil || len(m) == 0 {
				t.Errorf("the repository's check passes with %s put back: %v", c.name, err)
			}
		})
	}
}

// The same recording and seed give the same fixtures, value for value.
func TestFixturesAreSeeded(t *testing.T) {
	rec := madeUpRecording(t)
	a, b := t.TempDir(), t.TempDir()
	for _, out := range []string{a, b} {
		if _, err := WriteFixtures(FixtureOptions{Rec: rec, Out: out, Seed: []byte("one seed")}); err != nil {
			t.Fatal(err)
		}
	}
	x, _ := os.ReadFile(filepath.Join(a, EventsFile))
	y, _ := os.ReadFile(filepath.Join(b, EventsFile))
	if string(x) != string(y) {
		t.Error("one seed gave two fixtures")
	}
}

// A drawn value never holds a short recorded string or a recorded number.
func TestDrawnValuesHoldNoRecordedValue(t *testing.T) {
	fb := forbidden{byLen: map[int]map[string]bool{4: {"plum": true}}, long: map[string][]string{}, nums: map[string]bool{"4821": true}}
	if !fb.holds("xxplumxx") || !fb.holds("a4821b") || fb.holds("pear 482") {
		t.Error("holds is wrong")
	}
	if got := fb.within("the plum"); !slices.Equal(got, []string{"plum"}) {
		t.Errorf("within: %v", got)
	}
}

func TestInsideCheckout(t *testing.T) {
	root := t.TempDir()
	if err := os.Mkdir(filepath.Join(root, ".git"), 0o755); err != nil {
		t.Fatal(err)
	}
	inner := filepath.Join(root, "a", "b")
	if err := os.MkdirAll(inner, 0o755); err != nil {
		t.Fatal(err)
	}
	if in, err := InsideCheckout(inner); err != nil || !in {
		t.Errorf("a folder in a checkout: %v %v", in, err)
	}
	if in, err := InsideCheckout(t.TempDir()); err != nil || in {
		t.Errorf("a folder outside: %v %v", in, err)
	}
}
