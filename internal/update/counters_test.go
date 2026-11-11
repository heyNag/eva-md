package update

import (
	"bytes"
	"context"
	"encoding/json"
	"io/fs"
	"maps"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"
)

// countersMarker is a made-up marker planted in every text the counters
// read, which no record, line or file they write may hold.
const countersMarker = "zq-planted-marker-7f3k"

// runLogLines reads the run log of the env's day as raw lines.
func runLogLines(t *testing.T, e *env) []string {
	t.Helper()
	data, err := os.ReadFile(filepath.Join(e.dirs.State, "updates", e.clock.Format(time.DateOnly)+".jsonl"))
	if err != nil && !os.IsNotExist(err) {
		t.Fatal(err)
	}
	return strings.Split(strings.TrimSuffix(string(data), "\n"), "\n")
}

// The trigger a job was started for reaches every record it writes, and
// the sessions the day's sweep picks are recorded as the sweep's, whatever
// started the job that took it.
func TestTheTriggerReachesTheRecord(t *testing.T) {
	for _, trigger := range []string{TriggerEnd, TriggerCompact, TriggerStop, TriggerGap, TriggerStart, TriggerManual, ""} {
		t.Run("trigger "+trigger, func(t *testing.T) {
			e := newEnv(t)
			c := newChat("3f2a91c0-"+strings.ReplaceAll(trigger, "_", "-"), start)
			talk(c, 2, "amber")
			e.write(c)
			j := e.job()
			j.Trigger = trigger
			s, err := j.CatchUp(context.Background(), []Ref{{Host: c.host, ID: c.session}}, false)
			if err != nil || outcomes(s) != "ok" || s.Records[0].Trigger != trigger {
				t.Fatalf("%q %v %+v\n%s", outcomes(s), err, s.Records, e.log)
			}
			lines := runLogLines(t, e)
			if len(lines) != 1 {
				t.Fatalf("run log %q", lines)
			}
			var r Record
			if err := json.Unmarshal([]byte(lines[0]), &r); err != nil || r.Trigger != trigger {
				t.Fatalf("recorded %q: %v", lines[0], err)
			}
			if trigger == "" && strings.Contains(lines[0], `"trigger"`) {
				t.Fatalf("a job with no trigger wrote one: %s", lines[0])
			}
		})
	}
	e := newEnv(t)
	named := newChat("named", start)
	talk(named, 2, "amber")
	e.write(named)
	idleChat(t, e, "idle", 2*time.Hour)
	j := e.job()
	j.Trigger = TriggerStart
	s, err := j.CatchUp(context.Background(), []Ref{{Host: named.host, ID: named.session}}, true)
	if err != nil || outcomes(s) != "ok, ok" {
		t.Fatalf("%q %v\n%s", outcomes(s), err, e.log)
	}
	got := map[string]string{}
	for _, r := range s.Records {
		got[r.Session] = r.Trigger
	}
	if want := map[string]string{"named": TriggerStart, "idle": TriggerSweep}; !maps.Equal(got, want) {
		t.Fatalf("triggers %v, want %v", got, want)
	}
}

// A name the flag gives that this release doesn't know is recorded as
// other, so no text from a command line reaches the run log, and its
// update still runs.
func TestTriggerName(t *testing.T) {
	for _, tc := range []struct{ in, want string }{
		{"", ""}, {TriggerEnd, TriggerEnd}, {TriggerSweep, TriggerSweep}, {TriggerManual, TriggerManual},
		{"session_start", TriggerOther}, {countersMarker, TriggerOther}, {"Session_End", TriggerOther}, {" compact", TriggerOther},
	} {
		if got := TriggerName(tc.in); got != tc.want {
			t.Errorf("TriggerName(%q) = %q, want %q", tc.in, got, tc.want)
		}
	}
}

// snapshot reads every file under root, by its path from root, but skip.
func snapshot(t *testing.T, root, skip string) map[string]string {
	t.Helper()
	files := map[string]string{}
	err := filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() || path == skip {
			return err
		}
		data, err := os.ReadFile(path)
		files[path] = string(data)
		return err
	})
	if err != nil {
		t.Fatal(err)
	}
	return files
}

// A job that finds the update lock held leaves exactly one record, busy,
// with its trigger and its sessions as hashes the ids can't be read back
// from, and changes nothing else: no watermark, page, sweep or other file.
// The record names no host, so the budget, holds and the doctor's rows
// pass over it, and a later job runs as if it weren't there.
func TestABusyExitLeavesOneRecord(t *testing.T) {
	e := newEnv(t)
	c := newChat("3f2a91c0-busy-"+countersMarker, start)
	talk(c, 2, "amber")
	file := e.write(c)
	idleChat(t, e, "idle", 2*time.Hour)
	home := os.Getenv("HOME")
	runLog := filepath.Join(e.dirs.State, "updates", e.clock.Format(time.DateOnly)+".jsonl")
	unlock, held, err := TryLock(e.dirs.Locks(), "update.lock")
	if err != nil || held {
		t.Fatalf("lock: %v %v", held, err)
	}
	before := snapshot(t, home, runLog)
	j := e.job()
	j.Trigger = TriggerStart
	refs := []Ref{{Host: c.host, ID: c.session}, {Host: HostCodex, ID: "other-" + countersMarker}}
	s, err := j.CatchUp(context.Background(), refs, true)
	unlock()
	if err != nil || !s.Stopped(StopBusy) || len(s.Records) != 0 || calls(e.fake) != 0 {
		t.Fatalf("busy: %+v %v", s, err)
	}
	if after := snapshot(t, home, runLog); !maps.Equal(before, after) {
		for path := range after {
			if before[path] != after[path] {
				t.Errorf("%s changed", path)
			}
		}
		t.Fatal("a busy job changed more than the run log")
	}
	lines := runLogLines(t, e)
	if len(lines) != 1 || strings.Contains(lines[0], countersMarker) || strings.Contains(lines[0], "3f2a91c0") {
		t.Fatalf("run log %q", lines)
	}
	var r Record
	if err := json.Unmarshal([]byte(lines[0]), &r); err != nil {
		t.Fatal(err)
	}
	want := []string{sessionHash(c.host, c.session), sessionHash(HostCodex, "other-"+countersMarker)}
	if r.Outcome != OutcomeBusy || r.Host != "" || r.Session != "" || r.Trigger != TriggerStart || !slices.Equal(r.SessionHashes, want) || len(want[0]) != 16 || want[0] == want[1] {
		t.Fatalf("busy record %+v, want hashes %q", r, want)
	}
	// Each reader of the run log v1 has passes over it.
	records, err := Recent(e.dirs.State, e.clock, 1<<20)
	if err != nil || len(records) != 0 || Host(records, c.host, e.clock).Last.At != "" {
		t.Fatalf("Recent read %+v %v", records, err)
	}
	if l, err := readRunLog(e.dirs.State, e.clock); err != nil || len(l) != 0 {
		t.Fatalf("the job's own reading: %+v %v", l, err)
	}
	all, err := Days(e.dirs.State, e.clock, 7)
	if err != nil || len(all) != 1 || all[0].Outcome != OutcomeBusy {
		t.Fatalf("Days: %+v %v", all, err)
	}
	// The next job runs as if the busy one had not been: the session and
	// the sweep.
	s, err = j.CatchUp(context.Background(), refs[:1], true)
	if err != nil || outcomes(s) != "ok, ok" || !drained(t, e, file) {
		t.Fatalf("after: %q %v\n%s", outcomes(s), err, e.log)
	}
	// A job for the sweep alone records the sweep as its trigger, and a
	// pending job none.
	unlock, _, _ = TryLock(e.dirs.Locks(), "update.lock")
	j.Trigger = ""
	_, _ = j.CatchUp(context.Background(), nil, true)
	_, _ = j.Pending(context.Background())
	unlock()
	lines = runLogLines(t, e)
	var sweep, pending Record
	if len(lines) != 5 || json.Unmarshal([]byte(lines[3]), &sweep) != nil || json.Unmarshal([]byte(lines[4]), &pending) != nil {
		t.Fatalf("run log %q", lines)
	}
	if sweep.Outcome != OutcomeBusy || sweep.Trigger != TriggerSweep || sweep.SessionHashes != nil || pending.Outcome != OutcomeBusy || pending.Trigger != "" {
		t.Fatalf("sweep %+v, pending %+v", sweep, pending)
	}
}

// A record an older release wrote, with no trigger, rejection or reported
// model, still reads, as does one of this release in an older reader's
// eyes: the fields are only added.
func TestAnOldRecordStillReads(t *testing.T) {
	old := `{"at":"2026-01-14T09:00:00+01:00","host":"claude-code","session":"s1","project":"p","thread":"main","outcome":"ok","chunk_bytes":10,"pages":1,"duration_ms":5,"tokens_in":1,"tokens_out":1,"model":"","effort":"max","effort_used":"max","rules_version":4,"boundary_to_saved_ms":60000,"calls":1}`
	busy := `{"at":"2026-01-14T09:01:00+01:00","host":"","session":"","project":"","thread":"","outcome":"busy","chunk_bytes":0,"pages":0,"duration_ms":0,"tokens_in":0,"tokens_out":0,"model":"","effort":"","effort_used":"","rules_version":0,"boundary_to_saved_ms":0,"calls":0,"trigger":"chat_start","session_hashes":["0123456789abcdef"]}`
	records := parseRecords([]byte(old + "\n" + busy + "\n"))
	if len(records) != 1 || records[0].Trigger != "" || records[0].Rejection != nil || records[0].ModelReported != "" || records[0].BoundaryToSavedMS != 60000 {
		t.Fatalf("parsed %+v", records)
	}
	if all := parseLines([]byte(old+"\n"+busy+"\n"), true); len(all) != 2 || all[1].Trigger != TriggerStart || all[1].SessionHashes[0] != "0123456789abcdef" {
		t.Fatalf("with busy records: %+v", all)
	}
	// What an older release reads of a new record: the fields it knows.
	type v1Record struct {
		Host    string `json:"host"`
		Outcome string `json:"outcome"`
		Calls   int    `json:"calls"`
	}
	data, _ := json.Marshal(Record{Host: HostClaude, Outcome: OutcomeFailed, Reason: ReasonInvalidOutput, Calls: 1, Trigger: TriggerGap, ModelReported: "claude-x", Rejection: &Rejection{Rule: RuleMissingKey, Place: "items[].kind"}})
	var v1 v1Record
	if err := json.Unmarshal(data, &v1); err != nil || v1 != (v1Record{HostClaude, OutcomeFailed, 1}) {
		t.Fatalf("an older reader read %+v: %v", v1, err)
	}
}

// Days reads the days asked for, oldest first, and no other.
func TestDaysReadsTheWeek(t *testing.T) {
	e := newEnv(t)
	for i := range 9 {
		at := e.clock.AddDate(0, 0, -i)
		if err := appendRecord(e.dirs.State, Record{At: at.Format(time.RFC3339), Host: HostCodex, Session: "s", Outcome: OutcomeOK}); err != nil {
			t.Fatal(err)
		}
	}
	records, err := Days(e.dirs.State, e.clock, 7)
	if err != nil || len(records) != 7 {
		t.Fatalf("%d records: %v", len(records), err)
	}
	if day(records[0].At) != "2026-01-08" || day(records[6].At) != "2026-01-14" {
		t.Fatalf("days %s to %s", records[0].At, records[6].At)
	}
}

// An answer the checker refuses whole is recorded as invalid_output with
// the rule that refused it and where, by the schema's own names, never the
// answer's text, even where the answer holds a key of its own.
func TestAWholeRefusalNamesItsRule(t *testing.T) {
	for _, tc := range []struct {
		name   string
		answer func(r Request) []byte
		want   Rejection
	}{
		{"not JSON", func(Request) []byte { return []byte(`{"summary":"` + countersMarker) }, Rejection{RuleNotObject, "output"}},
		{"a key missing", func(Request) []byte { return []byte(`{"summary":"` + countersMarker + `"}`) }, Rejection{RuleMissingKey, "output.last_chat"}},
		{"a key of its own", func(r Request) []byte {
			var m map[string]any
			_ = json.Unmarshal(reply(t, r, "amber"), &m)
			m[countersMarker] = countersMarker
			data, _ := json.Marshal(m)
			return data
		}, Rejection{RuleExtraKey, "output"}},
		{"an item's kind", func(r Request) []byte {
			o := keeps(r)
			o.Summary, o.LastChat = countersMarker, countersMarker
			o.Items = append(o.Items, opAdd(countersMarker, "Finish the "+countersMarker, "stated", 1))
			return encode(t, o)
		}, Rejection{RuleNotEnum, "items[].kind"}},
		{"a ref", func(r Request) []byte {
			o := keeps(r)
			o.Summary, o.LastChat = countersMarker, countersMarker
			data := encode(t, o)
			var m map[string]any
			_ = json.Unmarshal(data, &m)
			m["timeline"] = []any{map[string]any{"kind": "decision", "text": countersMarker, "tag": "stated", "refs": []any{"one"}}}
			data, _ = json.Marshal(m)
			return data
		}, Rejection{RuleNotInteger, "timeline[].refs[]"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			e := newEnv(t)
			c := newChat("3f2a91c0-refused", start)
			talk(c, 2, "amber")
			e.write(c)
			e.fake.Respond = func(r Request) (Result, error) {
				return Result{Output: tc.answer(r), Attempts: 1, ModelReported: "claude-test-reported"}, nil
			}
			s := e.run(c)
			if outcomes(s) != "failed (invalid_output)" || s.Records[0].Rejection == nil || *s.Records[0].Rejection != tc.want {
				t.Fatalf("%q %+v, want %+v\n%s", outcomes(s), s.Records, tc.want, e.log)
			}
			lines := runLogLines(t, e)
			if len(lines) != 1 || strings.Contains(lines[0], countersMarker) || !strings.Contains(lines[0], `"model_reported":"claude-test-reported"`) {
				t.Fatalf("run log %q", lines)
			}
		})
	}
	// An accepted answer has no rejection, and its record keeps the model
	// the host reported beside the one asked for.
	e := newEnv(t)
	c := newChat("3f2a91c0-accepted", start)
	talk(c, 2, "amber")
	e.write(c)
	e.fake.Respond = func(r Request) (Result, error) {
		return Result{Output: reply(t, r, "amber"), Attempts: 1, Model: r.Model, ModelReported: "claude-test-reported"}, nil
	}
	if s := e.run(c); outcomes(s) != "ok" || s.Records[0].Rejection != nil || s.Records[0].ModelReported != "claude-test-reported" || s.Records[0].Validation == nil {
		t.Fatalf("%q %+v", outcomes(s), s.Records)
	}
}

// The place a refusal names keeps the schema's path and drops each index.
func TestRejectionPlace(t *testing.T) {
	for _, tc := range []struct{ where, want string }{
		{"the output", "output"}, {"", "output"}, {"the output.summary", "output.summary"},
		{"items[3]", "items[]"}, {"items[12].kind", "items[].kind"}, {"knowledge[0].paths[4]", "knowledge[].paths[]"},
	} {
		if got := place(tc.where); got != tc.want {
			t.Errorf("place(%q) = %q, want %q", tc.where, got, tc.want)
		}
	}
	if r := rejectionOf(os.ErrNotExist); *r != (Rejection{RuleOther, "output"}) {
		t.Errorf("an error with no rule: %+v", r)
	}
}

// The model claude reports is the one modelUsage credits with the most
// output, a name a model flag could take, and never any other text of the
// envelope; Codex's events name none.
func TestClaudeReportsItsModel(t *testing.T) {
	for _, tc := range []struct{ usage, want string }{
		{``, ""},
		{`"modelUsage":{}`, ""},
		{`"modelUsage":{"claude-a":{"outputTokens":10},"claude-b":{"outputTokens":200}}`, "claude-b"},
		{`"modelUsage":{"claude-b":{"outputTokens":7},"claude-a":{"outputTokens":7}}`, "claude-a"},
		{`"modelUsage":{"` + countersMarker + ` said this":{"outputTokens":900},"claude-a":{"outputTokens":1}}`, "claude-a"},
		{`"modelUsage":"claude-a"`, ""},
	} {
		stdout := `{"type":"result","subtype":"success","is_error":false,"result":"` + countersMarker + `",` + tc.usage
		stdout = strings.TrimSuffix(stdout, ",") + `,"structured_output":{"summary":"` + countersMarker + `"}}`
		var e envelope
		if err := json.Unmarshal([]byte(stdout), &e); err != nil {
			t.Fatalf("%s: %v", stdout, err)
		}
		if got := claudeModel(e); got != tc.want {
			t.Errorf("%s: %q, want %q", tc.usage, got, tc.want)
		}
	}
	isolate(t)
	dump := fakeCLI(t, "")
	put(t, dump, "stdout", `{"type":"result","subtype":"success","is_error":false,"modelUsage":{"claude-test-answered":{"inputTokens":1,"outputTokens":2}},"structured_output":{"summary":"done"}}`)
	res, err := NewRunner(HostClaude).Run(context.Background(), request(t, HostClaude))
	if err != nil || res.Reason != "" || res.ModelReported != "claude-test-answered" || res.Model != "claude-test-model" {
		t.Fatalf("result %+v %v", res, err)
	}
}

// A busy job's record holds no session id, and its hash is the same for
// each busy exit of a session and differs between hosts.
func TestSessionHash(t *testing.T) {
	a, b := sessionHash(HostClaude, "3f2a91c0-aaaa"), sessionHash(HostCodex, "3f2a91c0-aaaa")
	if a == b || a != sessionHash(HostClaude, "3f2a91c0-aaaa") || len(a) != 16 || strings.Contains(a, "3f2a91c0") {
		t.Fatalf("%q %q", a, b)
	}
	var buf bytes.Buffer
	j := Job{Dirs: newEnv(t).dirs, Log: &buf, Now: func() time.Time { return time.Date(2026, 1, 14, 9, 0, 0, 0, paris) }, Trigger: TriggerEnd}
	j.busy([]Ref{{Host: HostClaude, ID: "3f2a91c0-aaaa"}}, false)
	data, err := os.ReadFile(filepath.Join(j.Dirs.State, "updates", "2026-01-14.jsonl"))
	if err != nil || strings.Contains(string(data), "3f2a91c0") || !strings.Contains(string(data), a) {
		t.Fatalf("%s %v", data, err)
	}
}
