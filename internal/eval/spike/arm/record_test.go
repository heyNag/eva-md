package arm

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math"
	"reflect"
	"slices"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/heyNag/eva-md/internal/eval/results"
	"github.com/heyNag/eva-md/internal/eval/scenario"
	"github.com/heyNag/eva-md/internal/eval/spike"
)

// mixedIDs are the scripts of a run that holds every kind of call.
var mixedIDs = []string{"v1.03", "v1.05", "v1.06", "assent.01", "assent.02", "bounds.01"}

// mixed is an answering function over mixedIDs that gives a faithful
// answer, a repaired one, a repair that fails, an answer that is not JSON,
// one with characters JSON may escape that goes to repair, a failed first
// call and a pass with nothing to keep, with cached input on each repair
// reply.
func mixed(t *testing.T, f spike.Form) *canned {
	span := map[spike.Form]string{spike.FormQuotes: `"q": "Store pages as YAML, OSPREY-Y."`, spike.FormSegments: `"s": 1`}[f]
	note := func(e int) string {
		return "{\"op\": \"note\", \"id\": null, \"kind\": \"found\", \"text\": \"Pages <stay> small & fast,\u2028still.\", \"admit\": null, " +
			"\"ev\": [{\"e\": " + strconv.Itoa(e) + ", " + span + ", \"as\": \"said\"}]}"
	}
	return &canned{t: t, f: f, cached: true,
		names: map[string]string{"v1.05.1": "v1.05.1.ungrounded", "assent.01.1": "assent.01.1.off-by-a-word", "bounds.01.1": ""},
		fail:  map[string]string{"v1.03.2.first": "timeout", "assent.01.1.repair": "rate_limit"},
		raw: map[string][]byte{
			"v1.06.1.first":  []byte(`{"ops": [], "nothing": "none_new",}`),
			"v1.03.1.first":  []byte("{\"ops\": [\n " + note(1) + ",\n " + note(99) + "\n],\n \"nothing\": null}\n\n"),
			"v1.03.1.repair": []byte(`{"fixes": [{"index": 2, "op": null}]}`),
		}}
}

// withRepeat is a with Repeat n.
func withRepeat(a Arm, n int) Arm {
	a.Repeat = n
	return a
}

// timeless is rows with their time cleared.
func timeless(rows []results.Row) []results.Row {
	out := slices.Clone(rows)
	for i := range out {
		out[i].Time = time.Time{}
	}
	return out
}

// TestRecordThenReplay: a run recorded and then replayed, with the live
// answering function failing the test if called, makes the same calls and
// gives the same result, the same rows apart from time, and the same
// recording, byte for byte.
func TestRecordThenReplay(t *testing.T) {
	for _, f := range spike.Forms {
		a := withRepeat(arm(f), 2)
		live := mixed(t, f)
		var rec bytes.Buffer
		res, err := Run(context.Background(), a, scripts(t, mixedIDs...), live.ask, &rec)
		if err != nil {
			t.Fatal(err)
		}
		live.shut = true
		kinds := map[string]int{}
		for _, c := range live.calls {
			kinds[c.Kind]++
		}
		if kinds[KindRepair] < 2*2 || res.Rounds[0].On.Tally.Count("answers_invalid_first") != 1 || res.Rounds[0].On.Calls.Failed != 2 {
			t.Fatalf("%s: the run holds %v calls, not every kind", f, kinds)
		}
		replayArm := a
		replayArm.HostVersion = ""
		ask, version, err := Replay(bytes.NewReader(rec.Bytes()), replayArm)
		if err != nil {
			t.Fatal(err)
		}
		if version != a.HostVersion {
			t.Errorf("%s: the recording's host version is %q", f, version)
		}
		var calls []Call
		spy := func(ctx context.Context, c Call) (Reply, error) {
			calls = append(calls, c)
			return ask(ctx, c)
		}
		var again bytes.Buffer
		res2, err := Run(context.Background(), a, scripts(t, mixedIDs...), spy, &again)
		if err != nil {
			t.Fatal(err)
		}
		if !reflect.DeepEqual(calls, live.calls) {
			t.Errorf("%s: the replay made other calls than the run", f)
		}
		if !reflect.DeepEqual(res2, res) {
			t.Errorf("%s: the replay's result differs from the run's", f)
		}
		before := Rows(res, template())
		later := template()
		later.Time = later.Time.Add(time.Hour)
		after := Rows(res2, later)
		if !reflect.DeepEqual(timeless(after), timeless(before)) || after[0].Time.Equal(before[0].Time) {
			t.Errorf("%s: the replay's rows differ from the run's apart from time", f)
		}
		if !bytes.Equal(again.Bytes(), rec.Bytes()) {
			t.Errorf("%s: the replay records other bytes", f)
		}
	}
}

// TestTwoRunsGiveTheSameRows: the same answers give the same rows apart
// from time, and the same recording.
func TestTwoRunsGiveTheSameRows(t *testing.T) {
	f := spike.FormQuotes
	var one, two bytes.Buffer
	r1, err := Run(context.Background(), arm(f), scripts(t, mixedIDs...), mixed(t, f).ask, &one)
	if err != nil {
		t.Fatal(err)
	}
	r2, err := Run(context.Background(), arm(f), scripts(t, mixedIDs...), mixed(t, f).ask, &two)
	if err != nil {
		t.Fatal(err)
	}
	later := template()
	later.Time = later.Time.Add(time.Minute)
	if !reflect.DeepEqual(timeless(Rows(r1, template())), timeless(Rows(r2, later))) || !bytes.Equal(one.Bytes(), two.Bytes()) {
		t.Error("two runs on the same answers differ")
	}
}

// TestTheRecordingHoldsOnlyAnswers: each line holds the card's keys in
// its order, the arm's ids, and the answer as kept; no line holds the
// prompt, a rendered input or a repair input.
func TestTheRecordingHoldsOnlyAnswers(t *testing.T) {
	for _, f := range spike.Forms {
		c := mixed(t, f)
		var rec bytes.Buffer
		if _, err := Run(context.Background(), arm(f), scripts(t, mixedIDs...), c.ask, &rec); err != nil {
			t.Fatal(err)
		}
		lines := strings.Split(strings.TrimSuffix(rec.String(), "\n"), "\n")
		if len(lines) != len(c.calls) {
			t.Fatalf("%s: %d lines for %d calls", f, len(lines), len(c.calls))
		}
		for _, banned := range []string{spike.Boundary, "Entries:", "Items in force", spike.RepairBoundary, spike.RepairHead, "user typed"} {
			if strings.Contains(rec.String(), banned) {
				t.Errorf("%s: the recording holds %q", f, banned)
			}
		}
		for i, l := range lines {
			call := c.calls[i]
			if bytes.Contains([]byte(l), call.Input) || strings.Contains(l, call.System) {
				t.Errorf("%s: line %d holds its call's input", f, i+1)
			}
			d := json.NewDecoder(strings.NewReader(l))
			var keys []string
			if _, err := d.Token(); err != nil {
				t.Fatal(err)
			}
			for d.More() {
				tok, _ := d.Token()
				keys = append(keys, tok.(string))
				var v json.RawMessage
				if err := d.Decode(&v); err != nil {
					t.Fatal(err)
				}
			}
			if !slices.Equal(keys, lineKeys) {
				t.Fatalf("%s: line %d has keys %v", f, i+1, keys)
			}
			var got line
			if err := json.Unmarshal([]byte(l), &got); err != nil {
				t.Fatal(err)
			}
			reply, _ := c.reply(call)
			want := keep(reply.Output)
			if reply.Reason != "" {
				want = nil
			}
			if string(got.Output) == "null" {
				got.Output = nil
			}
			if got.Host != "codex" || got.HostVersion != "0.99.1" || got.Tier != TierSmall || got.Effort != "low" || got.Form != string(f) ||
				got.Script != call.Script || got.Pass != call.Pass || got.Round != call.Round || got.Call != call.Kind ||
				!bytes.Equal(got.Output, want) || got.Reason != reply.Reason || got.EffortUsed != "low" {
				t.Errorf("%s: line %d is %s", f, i+1, l)
			}
		}
		if !strings.Contains(rec.String(), "Pages <stay> small & fast,\u2028still.") {
			t.Errorf("%s: an answer's characters were escaped in the recording", f)
		}
	}
}

// reply is what c answers call, without counting it.
func (c *canned) reply(call Call) (Reply, error) {
	saved := c.calls
	defer func() { c.calls = saved }()
	return c.ask(context.Background(), call)
}

// recording is a recording of a run of arm(f) over mixedIDs.
func recording(t *testing.T, f spike.Form, a Arm) []byte {
	t.Helper()
	var rec bytes.Buffer
	if _, err := Run(context.Background(), a, scripts(t, mixedIDs...), mixed(t, f).ask, &rec); err != nil {
		t.Fatal(err)
	}
	return rec.Bytes()
}

// editLine applies edit to line n, from 1, of rec, as a map of its keys.
func editLine(t *testing.T, rec []byte, n int, edit func(m map[string]any)) []byte {
	t.Helper()
	lines := bytes.Split(bytes.TrimSuffix(rec, []byte("\n")), []byte("\n"))
	var m map[string]any
	d := json.NewDecoder(bytes.NewReader(lines[n-1]))
	d.UseNumber()
	if err := d.Decode(&m); err != nil {
		t.Fatal(err)
	}
	edit(m)
	data, err := json.Marshal(m)
	if err != nil {
		t.Fatal(err)
	}
	lines[n-1] = data
	return append(bytes.Join(lines, []byte("\n")), '\n')
}

// TestReplayRefuses: each recording Replay must refuse is refused before
// any call can be made.
func TestReplayRefuses(t *testing.T) {
	f := spike.FormQuotes
	a := arm(f)
	rec := recording(t, f, a)
	set := func(key string, v any) func(map[string]any) { return func(m map[string]any) { m[key] = v } }
	lines := bytes.SplitAfter(rec, []byte("\n"))
	other := func(change func(*Arm)) Arm {
		x := a
		change(&x)
		return x
	}
	cases := []struct {
		name string
		rec  []byte
		arm  Arm
	}{
		{"another host", rec, other(func(x *Arm) { x.Host = "claude-code" })},
		{"another tier", rec, other(func(x *Arm) { x.Tier = TierLarge })},
		{"another effort", rec, other(func(x *Arm) { x.Effort = "high" })},
		{"another form", rec, other(func(x *Arm) { x.Form = spike.FormSegments })},
		{"another host version than the arm's", rec, other(func(x *Arm) { x.HostVersion = "0.99.2" })},
		{"lines of two host versions", editLine(t, rec, 3, set("host_version", "0.99.2")), a},
		{"lines of two host versions, the arm giving none", editLine(t, rec, 3, set("host_version", "0.99.2")), other(func(x *Arm) { x.HostVersion = "" })},
		{"a line of another tier", editLine(t, rec, 2, set("tier", TierMiddle)), a},
		{"a line given twice", append(slices.Clone(rec), lines[1]...), a},
		{"a call given twice with other figures", append(slices.Clone(rec), editLine(t, lines[1], 1, set("tokens_in", 1))...), a},
		{"a reason that is a sentence", editLine(t, rec, 1, set("reason", "the host said: quota spent")), a},
		{"a reason in capitals", editLine(t, rec, 1, set("reason", "Timeout")), a},
		{"a reason too long", editLine(t, rec, 1, set("reason", strings.Repeat("x", 65))), a},
		{"a failed call with an answer", editLine(t, rec, 1, set("reason", "timeout")), a},
		{"an effort used that is a sentence", editLine(t, rec, 1, set("effort_used", "very high")), a},
		{"an unknown key", editLine(t, rec, 1, set("model", "x")), a},
		{"a key missing", editLine(t, rec, 1, func(m map[string]any) { delete(m, "attempts") }), a},
		{"a null count", editLine(t, rec, 1, set("tokens_in", nil)), a},
		{"a null reason", editLine(t, rec, 1, set("reason", nil)), a},
		{"a count as a string", editLine(t, rec, 1, set("tokens_out", "7")), a},
		{"a count with a fraction", editLine(t, rec, 1, set("attempts", 1.5)), a},
		{"a negative count", editLine(t, rec, 1, set("tokens_out", -1)), a},
		{"a negative cached count", editLine(t, rec, 1, set("tokens_cached_in", -1)), a},
		{"negative seconds", editLine(t, rec, 1, set("seconds", -0.5)), a},
		{"another format", editLine(t, rec, 1, set("v", 2)), a},
		{"a round from 0", editLine(t, rec, 1, set("round", 0)), a},
		{"a pass from 0", editLine(t, rec, 1, set("pass", 0)), a},
		{"a third kind of call", editLine(t, rec, 1, set("call", "retry")), a},
		{"a script that is no id", editLine(t, rec, 1, set("script", "V1 03")), a},
		{"a host version that is no version", editLine(t, rec, 1, set("host_version", "0.99 beta")), a},
		{"a key given twice", bytes.Replace(rec, []byte(`"attempts":1,`), []byte(`"attempts":1,"attempts":1,`), 1), a},
		{"data after a line", bytes.Replace(rec, []byte("}\n"), []byte("} {}\n"), 1), a},
		{"a line that is not JSON", append(slices.Clone(rec), []byte("not json\n")...), a},
		{"an empty line", append(slices.Clone(rec), '\n'), a},
		{"a line cut short", rec[:len(rec)-10], a},
		{"no line", nil, a},
		{"a line not UTF-8", bytes.Replace(rec, []byte(`"low"`), []byte("\"lo\xffw\""), 1), a},
		{"a line too long", append(slices.Clone(rec), []byte(`{"output":"`+strings.Repeat("x", maxLine)+`"}`+"\n")...), a},
		{"an arm with an unknown tier", rec, other(func(x *Arm) { x.Tier = "tiny" })},
	}
	for _, c := range cases {
		ask, version, err := Replay(bytes.NewReader(c.rec), c.arm)
		if err == nil || ask != nil || version != "" {
			t.Errorf("%s: replayed (%v)", c.name, err)
		}
	}
	if ask, _, err := Replay(bytes.NewReader(rec), a); err != nil || ask == nil {
		t.Fatalf("the recording itself is refused: %v", err)
	}
}

// TestReplayTooManyLines: a recording past MaxLines is refused.
func TestReplayTooManyLines(t *testing.T) {
	f := spike.FormQuotes
	rec := recording(t, f, arm(f))
	short := string(bytes.SplitAfter(rec, []byte("\n"))[1]) // a failed call: a short line
	if !strings.Contains(short, `"round":1,`) {
		t.Fatalf("the line is of another round: %s", short)
	}
	build := func(n int) *bytes.Buffer {
		var b bytes.Buffer
		for i := range n {
			b.WriteString(strings.Replace(short, `"round":1,`, `"round":`+strconv.Itoa(i+1)+`,`, 1))
		}
		return &b
	}
	if _, _, err := Replay(build(MaxLines+1), arm(f)); err == nil || !strings.Contains(err.Error(), "more than") {
		t.Errorf("a recording of %d lines: %v", MaxLines+1, err)
	}
	if ask, _, err := Replay(build(MaxLines), arm(f)); err != nil || ask == nil {
		t.Errorf("a recording of %d lines: %v", MaxLines, err)
	}
}

// TestReplayFailsOnAMissingCallOrAnUnusedLine: a call the recording lacks
// stops the run; a line the run never asks for fails it at the end; and a
// replay serves one run only.
func TestReplayFailsOnAMissingCallOrAnUnusedLine(t *testing.T) {
	f := spike.FormQuotes
	a := arm(f)
	rec := recording(t, f, a)
	lines := bytes.SplitAfter(bytes.TrimSuffix(rec, []byte("\n")), []byte("\n"))
	replay := func(rec []byte, runArm Arm, ids ...string) error {
		ask, _, err := Replay(bytes.NewReader(rec), a)
		if err != nil {
			t.Fatal(err)
		}
		res, err := Run(context.Background(), runArm, scripts(t, ids...), ask, nil)
		if err != nil && !reflect.DeepEqual(res, Result{}) {
			t.Error("a failed run returned a result")
		}
		return err
	}
	var repair int
	for i, l := range lines {
		if bytes.Contains(l, []byte(`"call":"repair"`)) {
			repair = i
			break
		}
	}
	without := append(slices.Clone(lines[:repair]), lines[repair+1:]...)
	if err := replay(bytes.Join(without, nil), a, mixedIDs...); err == nil || !strings.Contains(err.Error(), "no such call") {
		t.Errorf("a recording without a repair line: %v", err)
	}
	if err := replay(bytes.Join(lines[:len(lines)-1], nil), a, mixedIDs...); err == nil || !strings.Contains(err.Error(), "no such call") {
		t.Errorf("a recording without its last line: %v", err)
	}
	if err := replay(rec, a, mixedIDs[:len(mixedIDs)-1]...); err == nil || !strings.Contains(err.Error(), "unused") {
		t.Errorf("a replay over fewer scripts: %v", err)
	}
	twice := recording(t, f, withRepeat(a, 2))
	if err := replay(twice, a, mixedIDs...); err == nil || !strings.Contains(err.Error(), "1 of") && !strings.Contains(err.Error(), "unused") {
		t.Errorf("a recording of two rounds replayed as one: %v", err)
	}
	if err := replay(rec, withRepeat(a, 2), mixedIDs...); err == nil || !strings.Contains(err.Error(), "no such call") {
		t.Errorf("a recording of one round replayed as two: %v", err)
	}
	// One Run per replay.
	ask, _, err := Replay(bytes.NewReader(rec), a)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := Run(context.Background(), a, scripts(t, mixedIDs...), ask, nil); err != nil {
		t.Fatal(err)
	}
	if _, err := Run(context.Background(), a, scripts(t, mixedIDs...), ask, nil); err == nil || !strings.Contains(err.Error(), "asked already") {
		t.Errorf("a replay served a second run: %v", err)
	}
}

// TestKeep: an answer is kept compact, exactly once, and not at all when
// it is not one JSON value of valid UTF-8 within MaxOutput bytes.
func TestKeep(t *testing.T) {
	cases := []struct{ in, want string }{
		{"{ \"a\" : [ 1 , 2 ] }\n", `{"a":[1,2]}`},
		{`{"t":"<&> \u2028"}`, `{"t":"<&> \u2028"}`},
		{"{\"t\":\"a\u2028b\"}", "{\"t\":\"a\u2028b\"}"},
		{`{"a":1,"a":2}`, `{"a":1,"a":2}`},
		{`[1]`, `[1]`},
		{`{"a":1}{"b":2}`, ""},
		{`{"a":1,}`, ""},
		{"```json\n{}\n```", ""},
		{"{\"t\":\"\xff\"}", ""},
		{"", ""},
		{`Sure! Here is the answer.`, ""},
	}
	for _, c := range cases {
		got := keep([]byte(c.in))
		if string(got) != c.want || (c.want == "") != (got == nil) {
			t.Errorf("keep(%q) = %q, want %q", c.in, got, c.want)
		}
		if got != nil && !bytes.Equal(keep(got), got) {
			t.Errorf("keep(keep(%q)) changed it", c.in)
		}
	}
	big := []byte(`{"t":"` + strings.Repeat("x", MaxOutput-8) + `"}`)
	if keep(big) == nil || keep(append([]byte(" "), big...)) != nil {
		t.Errorf("keep of an answer of %d bytes is %v, of %d bytes %v", len(big), keep(big) != nil, len(big)+1, keep(append([]byte(" "), big...)) != nil)
	}
	deep := []byte(strings.Repeat("[", 20000) + strings.Repeat("]", 20000))
	if keep(deep) != nil {
		t.Error("an answer nested past JSON's depth was kept")
	}
}

// TestAnAnswerTooBigOrDeepIsInvalid: an answer the arm does not keep is
// recorded as null and counted as invalid, and so is it on replay.
func TestAnAnswerTooBigOrDeepIsInvalid(t *testing.T) {
	f := spike.FormQuotes
	for name, out := range map[string][]byte{
		"too big":   []byte(`{"ops":[],"nothing":"` + strings.Repeat("x", MaxOutput) + `"}`),
		"deep":      []byte(`{"ops":[` + strings.Repeat("[", 20000) + strings.Repeat("]", 20000) + `],"nothing":null}`),
		"key twice": []byte(`{"ops":[],"nothing":null,"ops":[]}`),
	} {
		c := &canned{t: t, f: f, raw: map[string][]byte{"v1.05.1.first": out}}
		var rec bytes.Buffer
		res := mustRun(t, arm(f), c, &rec, "v1.05")
		if res.Rounds[0].On.Tally.Count("answers_invalid_first") != 1 {
			t.Errorf("%s: not counted as invalid", name)
		}
		if name != "key twice" && !strings.Contains(rec.String(), `"output":null`) {
			t.Errorf("%s: recorded", name)
		}
		if rec.Len() > 4096+len(`{"ops":[],"nothing":null,"ops":[]}`) {
			t.Errorf("%s: the recording holds %d bytes", name, rec.Len())
		}
		ask, _, err := Replay(&rec, arm(f))
		if err != nil {
			t.Fatal(err)
		}
		again, err := Run(context.Background(), arm(f), scripts(t, "v1.05"), ask, nil)
		if err != nil || !reflect.DeepEqual(again, res) {
			t.Errorf("%s: the replay differs: %v", name, err)
		}
	}
}

// mustRun runs a over ids with c, recording to rec.
func mustRun(t *testing.T, a Arm, c *canned, rec io.Writer, ids ...string) Result {
	t.Helper()
	res, err := Run(context.Background(), a, scripts(t, ids...), c.ask, rec)
	if err != nil {
		t.Fatal(err)
	}
	return res
}

// failingWriter fails every write.
type failingWriter struct{}

func (failingWriter) Write([]byte) (int, error) { return 0, errors.New("disk full") }

// TestRunRefuses: an arm, a set of scripts or an answering function Run
// cannot run is refused before any call; a reply out of range, an error
// from ask or a recording it cannot write stops the run with no result.
func TestRunRefuses(t *testing.T) {
	f := spike.FormQuotes
	never := func(context.Context, Call) (Reply, error) {
		t.Error("a call was made")
		return Reply{}, nil
	}
	bad := func(change func(*Arm)) Arm {
		a := arm(f)
		change(&a)
		return a
	}
	long := scripts(t, "v1.05")[0]
	long.Scenario.ID = "v1.05-" + strings.Repeat("x", 50)
	broken := scripts(t, "v1.05")[0]
	broken.Scenario.Passes = nil
	for name, c := range map[string]struct {
		arm     Arm
		scripts []spike.Script
		ask     Ask
	}{
		"no host":           {bad(func(a *Arm) { a.Host = "" }), scripts(t, "v1.05"), never},
		"a host in words":   {bad(func(a *Arm) { a.Host = "Claude Code" }), scripts(t, "v1.05"), never},
		"no host version":   {bad(func(a *Arm) { a.HostVersion = "" }), scripts(t, "v1.05"), never},
		"a bad version":     {bad(func(a *Arm) { a.HostVersion = "1.0 (beta)" }), scripts(t, "v1.05"), never},
		"a model's name":    {bad(func(a *Arm) { a.Tier = "some-model-4" }), scripts(t, "v1.05"), never},
		"an unknown effort": {bad(func(a *Arm) { a.Effort = "extreme" }), scripts(t, "v1.05"), never},
		"an unknown form":   {bad(func(a *Arm) { a.Form = "prose" }), scripts(t, "v1.05"), never},
		"a negative repeat": {bad(func(a *Arm) { a.Repeat = -1 }), scripts(t, "v1.05"), never},
		"no script":         {arm(f), nil, never},
		"a script twice":    {arm(f), scripts(t, "v1.05", "v1.03", "v1.05"), never},
		"a script too long": {arm(f), []spike.Script{long}, never},
		"a broken script":   {arm(f), []spike.Script{broken}, never},
		"no ask":            {arm(f), scripts(t, "v1.05"), nil},
	} {
		if res, err := Run(context.Background(), c.arm, c.scripts, c.ask, nil); err == nil || !reflect.DeepEqual(res, Result{}) {
			t.Errorf("%s: ran", name)
		}
	}
	nan := math.NaN()
	neg := -1
	for name, tweak := range map[string]func(*Reply){
		"a sentence for a reason":  func(r *Reply) { r.Reason = "the host said no" },
		"a sentence for an effort": func(r *Reply) { r.Effort = "as high as it goes" },
		"negative tokens in":       func(r *Reply) { r.TokensIn = -1 },
		"negative tokens out":      func(r *Reply) { r.TokensOut = -1 },
		"negative attempts":        func(r *Reply) { r.Attempts = -1 },
		"negative cached input":    func(r *Reply) { r.CachedIn = &neg },
		"seconds not a figure":     func(r *Reply) { r.Seconds = nan },
		"infinite seconds":         func(r *Reply) { r.Seconds = math.Inf(1) },
		"negative seconds":         func(r *Reply) { r.Seconds = -1 },
	} {
		c := &canned{t: t, f: f}
		ask := func(ctx context.Context, call Call) (Reply, error) {
			r, err := c.ask(ctx, call)
			if call.Script == "v1.03" && call.Pass == 2 {
				tweak(&r)
			}
			return r, err
		}
		var rec bytes.Buffer
		res, err := Run(context.Background(), arm(f), scripts(t, "v1.05", "v1.03", "v1.06"), ask, &rec)
		if err == nil || !reflect.DeepEqual(res, Result{}) || len(c.calls) != 3 || strings.Count(rec.String(), "\n") != 2 {
			t.Errorf("%s: the run went on (%v), %d calls, %d lines", name, err, len(c.calls), strings.Count(rec.String(), "\n"))
		}
	}
	stop := errors.New("stopped")
	c := &canned{t: t, f: f}
	ask := func(ctx context.Context, call Call) (Reply, error) {
		if call.Script == "v1.03" {
			return Reply{}, stop
		}
		return c.ask(ctx, call)
	}
	if res, err := Run(context.Background(), arm(f), scripts(t, "v1.05", "v1.03", "v1.06"), ask, nil); !errors.Is(err, stop) || !reflect.DeepEqual(res, Result{}) || len(c.calls) != 1 {
		t.Errorf("an error from ask: %v", err)
	}
	if _, err := Run(context.Background(), arm(f), scripts(t, "v1.05"), (&canned{t: t, f: f}).ask, failingWriter{}); err == nil {
		t.Error("a recording that cannot be written did not stop the run")
	}
}

// TestRepeat: each round runs every script afresh, and gives each
// scenario one row of each metric a round; 0 runs one round.
func TestRepeat(t *testing.T) {
	f := spike.FormQuotes
	for _, n := range []int{0, 1, 3} {
		c := &canned{t: t, f: f}
		res := run(t, withRepeat(arm(f), n), c, "v1.03", "v1.05")
		want := max(n, 1)
		if len(res.Rounds) != want || len(c.calls) != 3*want {
			t.Fatalf("repeat %d: %d rounds, %d calls", n, len(res.Rounds), len(c.calls))
		}
		for i, rd := range res.Rounds {
			if !reflect.DeepEqual(rd, res.Rounds[0]) {
				t.Errorf("repeat %d: round %d differs from the first on the same answers", n, i+1)
			}
		}
		for i, call := range c.calls {
			if call.Round != i/3+1 {
				t.Errorf("repeat %d: call %d is of round %d", n, i+1, call.Round)
			}
		}
		count := map[string]int{}
		for _, r := range Rows(res, template()) {
			count[r.Scenario+" "+r.Metric]++
		}
		for k, v := range count {
			if v != want {
				t.Errorf("repeat %d: %s has %d rows", n, k, v)
			}
		}
	}
}

// TestRowsPassTheStore: the rows of a run over every script go through
// results.Store with every script's planted words forbidden, and load
// back as they were; each scenario has every metric the card names.
func TestRowsPassTheStore(t *testing.T) {
	for _, f := range spike.Forms {
		all := spike.Scripts()
		var ids, forbid []string
		names := map[string]string{}
		for _, s := range all {
			ids = append(ids, s.Scenario.ID)
			forbid = append(forbid, scenario.Forbid(s.Scenario)...)
			for n := range s.Scenario.Passes {
				names[fmt.Sprintf("%s.%d", s.Scenario.ID, n+1)] = ""
			}
		}
		c := mixed(t, f)
		delete(c.fail, "assent.01.1.repair")
		for _, id := range []string{"v1.03.2", "v1.08.1", "v1.08.2", "v1.06.1", "assent.01.1", "assent.02.1"} {
			delete(names, id)
		}
		for k, v := range c.names {
			names[k] = v
		}
		c.names = names
		res := run(t, arm(f), c, ids...)
		rows := Rows(res, template())
		store := results.Store{Dir: t.TempDir(), Forbid: forbid}
		if err := store.Append(rows...); err != nil {
			t.Fatal(err)
		}
		loaded, err := results.Load(store.Dir)
		if err != nil {
			t.Fatal(err)
		}
		if len(loaded) != len(rows) {
			t.Fatalf("%d rows loaded of %d", len(loaded), len(rows))
		}
		want := template()
		want.V, want.Instrument, want.Model, want.Host, want.HostVersion = results.Version, "spike", TierSmall, "codex", "0.99.1"
		metrics := map[string]map[string]string{}
		for _, r := range loaded {
			unit := r.Unit
			if metrics[r.Scenario] == nil {
				metrics[r.Scenario] = map[string]string{}
			}
			metrics[r.Scenario][r.Metric] = unit
			r.Metric, r.Unit, r.Value, r.Scenario = "", "", 0, ""
			if !reflect.DeepEqual(r, want) {
				t.Fatalf("a row is %+v", r)
			}
		}
		if len(metrics) != 2*(len(all)+1) {
			t.Errorf("%d scenarios, want %d", len(metrics), 2*(len(all)+1))
		}
		units := map[string]string{"spike.calls_first": "count", "spike.calls_repair": "count", "spike.calls_failed": "count",
			"spike.tokens_in": "tokens", "spike.tokens_out": "tokens", "spike.repair_tokens_in": "tokens", "spike.seconds": "seconds",
			"fidelity.fact_recall": "ratio", "fidelity.fact_holds": "ratio", "fidelity.wrong_write": "count", "fidelity.misattributed": "count"}
		for _, id := range spike.CountIDs {
			units["spike."+id] = "count"
		}
		for sc, m := range metrics {
			if !strings.HasPrefix(sc, string(f)+".low.on.") && !strings.HasPrefix(sc, string(f)+".low.off.") {
				t.Errorf("scenario %s", sc)
			}
			for metric, unit := range units {
				if m[metric] != unit {
					t.Errorf("%s: %s in %q, want %q", sc, metric, m[metric], unit)
				}
			}
			for metric, unit := range m {
				if strings.HasPrefix(metric, "spike.") && !slices.Contains(spikeMetrics(), metric) {
					t.Errorf("%s: a metric the card does not name: %s", sc, metric)
				}
				if slices.Contains([]string{"spike.honest_refused", "spike.quote_miss_first", "spike.quote_miss_final", "spike.repair_yield"}, metric) && unit != "ratio" {
					t.Errorf("%s: %s in %s", sc, metric, unit)
				}
			}
		}
		on := metrics[scenarioID(arm(f), "on", "all")]
		for _, metric := range []string{"spike.repair_tokens_cached_in", "spike.honest_refused", "spike.repair_yield", "spike.quote_miss_final"} {
			if _, ok := on[metric]; !ok {
				t.Errorf("on over all has no %s", metric)
			}
		}
	}
}

// spikeMetrics are the spike's metrics the card names.
func spikeMetrics() []string {
	var out []string
	for _, id := range append(slices.Clone(spike.CountIDs), spike.RatioIDs...) {
		out = append(out, "spike."+id)
	}
	for _, id := range []string{"calls_first", "calls_repair", "calls_failed", "tokens_in", "tokens_out", "repair_tokens_in",
		"repair_tokens_cached_in", "seconds", "honest_refused"} {
		out = append(out, "spike."+id)
	}
	return out
}
