package update

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/heyNag/eva-md/internal/config"
	"github.com/heyNag/eva-md/internal/cost"
)

// The cached count comes from claude's envelope in both its forms, and is
// summed over the attempts of a run; tokens_in keeps counting cached input
// in, and a cache write is not a cache read.
func TestCachedInputFromClaudeEnvelopes(t *testing.T) {
	for _, tc := range []struct {
		name                 string
		stdout               map[string]string // by file name, as the fake reads them
		in, cached, out, run int
	}{
		{name: "usage", stdout: map[string]string{"stdout": claudeSuccess}, in: 200, cached: 50, out: 40, run: 1},
		{name: "model usage", stdout: map[string]string{"stdout": `{"type":"result","subtype":"success","is_error":false,` +
			`"usage":{"input_tokens":1,"cache_read_input_tokens":999,"output_tokens":1},` +
			`"modelUsage":{"claude-a":{"inputTokens":100,"outputTokens":10,"cacheReadInputTokens":5,"cacheCreationInputTokens":11},` +
			`"claude-b":{"inputTokens":20,"outputTokens":2,"cacheReadInputTokens":7,"cacheCreationInputTokens":0}},"structured_output":{"summary":"done"}}`},
			in: 143, cached: 12, out: 12, run: 1},
		{name: "cache writes only", stdout: map[string]string{"stdout": `{"type":"result","subtype":"success","is_error":false,` +
			`"usage":{"input_tokens":10,"cache_creation_input_tokens":90,"output_tokens":3},"structured_output":{"summary":"done"}}`},
			in: 100, cached: 0, out: 3, run: 1},
		{name: "no cache fields", stdout: map[string]string{"stdout": `{"type":"result","subtype":"success","is_error":false,` +
			`"usage":{"input_tokens":10,"output_tokens":3},"structured_output":{"summary":"done"}}`},
			in: 10, cached: 0, out: 3, run: 1},
		// An effort refused, then accepted one step down: both attempts count.
		{name: "two attempts", stdout: map[string]string{
			"stdout.1": `{"type":"result","subtype":"success","is_error":true,"api_error_status":400,"result":"API Error: 400 effort max is not supported by this model",` +
				`"usage":{"input_tokens":10,"cache_read_input_tokens":7,"output_tokens":0}}`,
			"exit.1": "1", "stdout": claudeSuccess},
			in: 217, cached: 57, out: 40, run: 2},
	} {
		t.Run(tc.name, func(t *testing.T) {
			isolate(t)
			dump := fakeCLI(t, "")
			for name, content := range tc.stdout {
				put(t, dump, name, content)
			}
			res, err := NewRunner(HostClaude).Run(context.Background(), request(t, HostClaude))
			if err != nil || res.Reason != "" {
				t.Fatalf("result %+v %v", res, err)
			}
			if res.TokensIn != tc.in || res.TokensCachedIn != tc.cached || res.TokensOut != tc.out || res.Attempts != tc.run {
				t.Fatalf("tokens %d in, %d cached, %d out over %d attempts; want %d, %d, %d over %d",
					res.TokensIn, res.TokensCachedIn, res.TokensOut, res.Attempts, tc.in, tc.cached, tc.out, tc.run)
			}
		})
	}
}

// The cached count comes from each turn of codex's stream, summed; input
// still counts it in.
func TestCachedInputFromACodexStream(t *testing.T) {
	const (
		begin   = `{"type":"thread.started","thread_id":"0199a000-0000-7000-8000-000000000003"}` + "\n" + `{"type":"turn.started"}` + "\n"
		message = `{"type":"item.completed","item":{"id":"item_1","type":"agent_message","text":"{\"summary\":\"done\"}"}}` + "\n"
	)
	for _, tc := range []struct {
		name, stream    string
		in, cached, out int
	}{
		{name: "one turn", stream: codexClean, in: 500, cached: 100, out: 60},
		{name: "two turns", stream: begin + message + `{"type":"turn.completed","usage":{"input_tokens":500,"cached_input_tokens":100,"output_tokens":60}}` + "\n" +
			`{"type":"turn.started"}` + "\n" + `{"type":"turn.completed","usage":{"input_tokens":300,"cached_input_tokens":40,"output_tokens":6,"reasoning_output_tokens":4}}` + "\n",
			in: 800, cached: 140, out: 66},
		{name: "no cached field", stream: begin + message + `{"type":"turn.completed","usage":{"input_tokens":70,"output_tokens":7}}` + "\n", in: 70, cached: 0, out: 7},
	} {
		t.Run(tc.name, func(t *testing.T) {
			isolate(t)
			dump := fakeCLI(t, "")
			put(t, dump, "stdout", tc.stream)
			put(t, dump, "output", answer)
			res, err := NewRunner(HostCodex).Run(context.Background(), request(t, HostCodex))
			if err != nil || res.Reason != "" {
				t.Fatalf("result %+v %v", res, err)
			}
			if res.TokensIn != tc.in || res.TokensCachedIn != tc.cached || res.TokensOut != tc.out {
				t.Fatalf("tokens %d in, %d cached, %d out; want %d, %d, %d", res.TokensIn, res.TokensCachedIn, res.TokensOut, tc.in, tc.cached, tc.out)
			}
		})
	}
}

// A pass records the cached count beside tokens_in, which keeps its
// meaning; a pass with none writes no field, as an older release did.
func TestAPassRecordsItsCachedInput(t *testing.T) {
	e := newEnv(t)
	cached := 400
	e.fake.Respond = func(r Request) (Result, error) {
		return Result{Output: reply(t, r, "amber"), Attempts: 1, TokensIn: 1000, TokensCachedIn: cached, TokensOut: 50}, nil
	}
	a, b := newChat("cached-a", start), newChat("cached-b", start.Add(time.Hour))
	b.branch = "other"
	talk(a, 1, "amber")
	talk(b, 1, "cobalt")
	e.write(a)
	e.write(b)
	if s := e.run(a); outcomes(s) != "ok" || s.Records[0].TokensIn != 1000 || s.Records[0].TokensCachedIn != 400 {
		t.Fatalf("%q %+v\n%s", outcomes(s), s.Records, e.log)
	}
	cached = 0
	if s := e.run(b); outcomes(s) != "ok" || s.Records[0].TokensIn != 1000 || s.Records[0].TokensCachedIn != 0 {
		t.Fatalf("%q %+v\n%s", outcomes(s), s.Records, e.log)
	}
	data, err := os.ReadFile(filepath.Join(e.dirs.State, "updates", e.clock.Format(time.DateOnly)+".jsonl"))
	if err != nil {
		t.Fatal(err)
	}
	var lines []string
	for _, line := range strings.Split(strings.TrimSpace(string(data)), "\n") {
		if strings.Contains(line, `"outcome":"ok"`) {
			lines = append(lines, line)
		}
	}
	if len(lines) != 2 || !strings.Contains(lines[0], `"tokens_in":1000,`) || !strings.Contains(lines[0], `"tokens_cached_in":400`) ||
		!strings.Contains(lines[1], `"tokens_in":1000,`) || strings.Contains(lines[1], "tokens_cached_in") {
		t.Fatalf("run log:\n%s", data)
	}
}

// costNow is the day the hand-written run log ends on.
var costNow = time.Date(2026, 1, 14, 18, 0, 0, 0, time.UTC)

// costLog writes a run log of 8 days for two hosts, with and without the
// cached field, a busy exit and a line that doesn't decode, into state.
func costLog(t *testing.T, state string) {
	t.Helper()
	at := func(daysAgo, hour int) string {
		return costNow.AddDate(0, 0, -daysAgo).Add(time.Duration(hour-18) * time.Hour).Format(time.RFC3339)
	}
	line := func(daysAgo, hour int, host, session, outcome, reason string, calls, in, cached, out int, ms int64, effort, used string) string {
		r := map[string]any{"at": at(daysAgo, hour), "host": host, "session": session, "project": "p", "thread": "main", "outcome": outcome,
			"chunk_bytes": 10, "pages": 0, "duration_ms": ms, "tokens_in": in, "tokens_out": out, "model": "", "effort": effort, "effort_used": used,
			"rules_version": RulesVersion, "boundary_to_saved_ms": 0, "calls": calls}
		if reason != "" {
			r["reason"] = reason
		}
		if cached >= 0 {
			r["tokens_cached_in"] = cached
		}
		data, err := json.Marshal(r)
		if err != nil {
			t.Fatal(err)
		}
		return string(data)
	}
	days := map[int][]string{
		0: {
			line(0, 9, HostClaude, "sA", OutcomeOK, "", 1, 1000, 400, 100, 2000, "max", "max"),
			// Without the field, as an older release wrote it: no cached input.
			line(0, 10, HostClaude, "sB", OutcomeNoChange, "", 2, 500, -1, 50, 1000, "max", "high"),
			line(0, 11, HostClaude, "sC", OutcomeFailed, ReasonRateLimit, 1, 0, -1, 0, 300, "max", ""),
			line(0, 12, HostClaude, "sC", OutcomeSkipped, ReasonSmall, 0, 0, -1, 0, 0, "", ""),
			line(0, 13, HostCodex, "sX", OutcomeOK, "", 1, 800, 200, 80, 1500, "ultra", "max"),
			"{\"at\":\"" + at(0, 14),
		},
		1: {line(1, 9, HostCodex, "sY", OutcomeNoChange, "", 1, 100, -1, 10, 500, "low", "low")},
		3: {line(3, 9, HostClaude, "sA", OutcomeOK, "", 1, 2000, 1500, 200, 4000, "high", "")},
		6: {line(6, 9, HostClaude, "sD", OutcomeFailed, ReasonInvalidOutput, 1, 300, 0, 30, 700, "max", "max")},
		// The 8th day, outside the week.
		7: {line(7, 9, HostClaude, "sE", OutcomeOK, "", 1, 99999, 0, 9999, 9999, "max", "max"),
			line(7, 10, HostCodex, "sZ", OutcomeOK, "", 1, 99999, 0, 9999, 9999, "max", "max")},
	}
	dir := filepath.Join(state, "updates")
	if err := os.MkdirAll(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	for ago, lines := range days {
		date := costNow.AddDate(0, 0, -ago).Format(time.DateOnly)
		if err := os.WriteFile(filepath.Join(dir, date+".jsonl"), []byte(strings.Join(lines, "\n")+"\n"), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	busy := `{"at":"` + at(0, 15) + `","host":"","session":"","project":"","thread":"","outcome":"busy","calls":0,"tokens_in":5000}` + "\n"
	if err := os.WriteFile(filepath.Join(dir, busyFile(costNow.Format(time.DateOnly))), []byte(busy), 0o600); err != nil {
		t.Fatal(err)
	}
}

// A hand-written run log of 8 days and two hosts, with and without the
// cached field, gives exact figures for the day and the week; the 8th day
// is left out, by the reader and by the week's own dates.
func TestTheRunLogsCost(t *testing.T) {
	isolate(t)
	state := filepath.Join(t.TempDir(), "state")
	costLog(t, state)
	records, err := Days(state, costNow, 8)
	if err != nil {
		t.Fatal(err)
	}
	if got, want := Spent(records, HostClaude, costNow), (cost.Usage{In: 1100, CachedIn: 400, Out: 150, Calls: 4, MS: 3300}); got != want {
		t.Fatalf("claude today %+v, want %+v", got, want)
	}
	if got := Spent(records, HostClaude, costNow).Weighted(); got != 1890 {
		t.Fatalf("claude today weighs %d", got)
	}
	if got, want := Spent(records, HostCodex, costNow), (cost.Usage{In: 600, CachedIn: 200, Out: 80, Calls: 1, MS: 1500}); got != want || got.Weighted() != 1020 {
		t.Fatalf("codex today %+v weighing %d, want %+v", got, got.Weighted(), want)
	}
	if got := Spent(records, HostCodex, costNow.AddDate(0, 0, -1)); got != (cost.Usage{In: 100, Out: 10, Calls: 1, MS: 500}) {
		t.Fatalf("codex yesterday %+v", got)
	}
	wantClaude := cost.Week{Usage: cost.Usage{In: 1900, CachedIn: 1900, Out: 380, Calls: 6, MS: 8000}, Passes: 5, Organized: 2, NoChange: 1, BelowEffort: 2}
	wantCodex := cost.Week{Usage: cost.Usage{In: 700, CachedIn: 200, Out: 90, Calls: 2, MS: 2000}, Passes: 2, Organized: 2, NoChange: 1, BelowEffort: 1}
	for host, want := range map[string]cost.Week{HostClaude: wantClaude, HostCodex: wantCodex} {
		got, err := Week(state, host, costNow)
		if err != nil || got != want {
			t.Errorf("%s week %+v %v, want %+v", host, got, err, want)
		}
		// The 8 days read whole: the week's dates still leave the 8th out.
		if got := WeekOf(records, host, costNow); got != want {
			t.Errorf("%s week of 8 days' records %+v, want %+v", host, got, want)
		}
	}
	if w := WeekOf(records, HostClaude, costNow); w.Weighted() != 3990 || w.PerChat() != 1995 {
		t.Fatalf("claude's week weighs %d, %d a chat", w.Weighted(), w.PerChat())
	}
	if w := WeekOf(records, HostCodex, costNow); w.Weighted() != 1170 || w.PerChat() != 585 {
		t.Fatalf("codex's week weighs %d, %d a chat", w.Weighted(), w.PerChat())
	}
	if w, err := Week(filepath.Join(t.TempDir(), "none"), HostClaude, costNow); err != nil || w != (cost.Week{}) {
		t.Fatalf("no run log: %+v %v", w, err)
	}
}

// OverBudget is daily_cap's test, and with the switch token_budget on the
// budget's too: reached at the budget, not below it.
func TestOverBudget(t *testing.T) {
	isolate(t)
	state := filepath.Join(t.TempDir(), "state")
	costLog(t, state)
	records, err := Days(state, costNow, 2)
	if err != nil {
		t.Fatal(err)
	}
	on := func(c config.Config) config.Config {
		c.Switches = map[string]any{"token_budget": true}
		return c
	}
	base := config.Default()
	for _, tc := range []struct {
		name   string
		c      config.Config
		host   string
		now    time.Time
		reason bool
	}{
		{"under the cap", base, HostClaude, costNow, false},
		{"at the cap", withCap(base, 3), HostClaude, costNow, true},
		{"one call past the other host's cap", withCap(base, 2), HostCodex, costNow, false},
		{"switch off, a budget of one", withBudget(base, 1), HostClaude, costNow, false},
		{"budget reached", on(withBudget(base, 1890)), HostClaude, costNow, true},
		{"budget not reached", on(withBudget(base, 1891)), HostClaude, costNow, false},
		{"the other host's budget", on(withBudget(base, 1020)), HostCodex, costNow, true},
		{"the default budget", on(base), HostClaude, costNow, false},
		{"the next day", on(withBudget(base, 1)), HostClaude, costNow.AddDate(0, 0, 1), false},
	} {
		if got := OverBudget(tc.c, records, tc.host, tc.now); got != tc.reason {
			t.Errorf("%s: over budget %v", tc.name, got)
		}
	}
}

func withCap(c config.Config, n int) config.Config {
	c.DailyCap = n
	return c
}

func withBudget(c config.Config, n int64) config.Config {
	c.DailyTokenBudget = n
	return c
}

// estimateOf is the estimate of the pass that made request r: its rules,
// batch and schema, as gate takes it.
func estimateOf(r Request) int64 {
	return cost.Estimate(len(r.Rules) + len(r.Input) + len(r.Schema)).Weighted()
}

// With the switch token_budget on, a pass runs while the day's weighted
// tokens and its estimate stay within the budget, a pass landing exactly
// on it included; the next is stopped with budget, deferred with no record
// and its watermark where it was, and a person's run too; once the day's
// tokens reach the budget even the smallest pass waits; the next day it
// runs. daily_cap still applies.
func TestTheTokenBudgetDefersPasses(t *testing.T) {
	e := newEnv(t)
	e.cfg.Switches = map[string]any{"token_budget": true}
	e.cfg.DailyTokenBudget = 1 << 40
	spend := 1000
	e.fake.Respond = func(r Request) (Result, error) {
		return Result{Output: reply(t, r, "amber"), Attempts: 1, TokensIn: spend, TokensCachedIn: 0, TokensOut: 0}, nil
	}
	var chats []*chat
	var files []string
	for i, b := range []string{"budget-a", "budget-b", "budget-c"} {
		c := newChat("tokens-"+b, start.Add(time.Duration(i)*time.Hour))
		c.branch = b
		talk(c, 1, "amber")
		files = append(files, e.write(c))
		chats = append(chats, c)
	}
	if s := e.run(chats[0]); outcomes(s) != "ok" {
		t.Fatalf("first: %q\n%s", outcomes(s), e.log)
	}
	est := estimateOf(e.fake.Requests()[0])
	if est < 100 {
		t.Fatalf("an estimate of %d tokens", est)
	}
	// One token short of what the next pass needs.
	e.cfg.DailyTokenBudget = 1000 + est - 1
	s := e.run(chats[1])
	if len(s.Records) != 0 || !s.Stopped(StopBudget) || calls(e.fake) != 1 || e.mark(files[1]).Offset != 0 {
		t.Fatalf("over the estimate: %q %v, %d calls\n%s", outcomes(s), s.Stops, calls(e.fake), e.log)
	}
	if want := fmt.Sprintf("claude-code: stopped: budget (about 1 of %d thousand weighted tokens today; the next pass needs about %d thousand)",
		thousands(1000+est-1), thousands(est)); !strings.Contains(e.log.String(), want) {
		t.Fatalf("log lacks %q:\n%s", want, e.log)
	}
	manual := e.job()
	manual.Manual = true
	if s, err := manual.Session(context.Background(), chats[1].host, chats[1].session); err != nil || len(s.Records) != 0 || !s.Stopped(StopBudget) || calls(e.fake) != 1 {
		t.Fatalf("a person's run over the estimate: %+v %v", s, err)
	}
	// Exactly on it: the pass runs, and spends just what is left.
	e.cfg.DailyTokenBudget = 1000 + est
	spend = int(est)
	if s := e.run(chats[1]); outcomes(s) != "ok" || calls(e.fake) != 2 {
		t.Fatalf("landing on the budget: %q %v\n%s", outcomes(s), s.Stops, e.log)
	}
	if got := estimateOf(e.fake.Requests()[1]); got != est {
		t.Fatalf("the second pass's estimate is %d, the first's %d", got, est)
	}
	// The budget is reached: the host stops before any pass is planned.
	e.log.Reset()
	if s := e.run(chats[2]); len(s.Records) != 0 || !s.Stopped(StopBudget) || calls(e.fake) != 2 || e.mark(files[2]).Offset != 0 {
		t.Fatalf("budget reached: %q %v\n%s", outcomes(s), s.Stops, e.log)
	}
	if want := fmt.Sprintf("claude-code: stopped: budget (about %d of %d thousand weighted tokens today)\n", thousands(1000+est), thousands(1000+est)); !strings.Contains(e.log.String(), want) {
		t.Fatalf("log lacks %q:\n%s", want, e.log)
	}
	// No pass that was deferred left a record.
	records, err := Days(e.dirs.State, e.clock, 1)
	if err != nil || len(records) != 2 || outcomes(Summary{Records: records}) != "ok, ok" {
		t.Fatalf("run log %+v %v", records, err)
	}
	e.clock = e.clock.Add(24 * time.Hour)
	if s := e.run(chats[2]); outcomes(s) != "ok" || calls(e.fake) != 3 {
		t.Fatalf("next day: %q %v\n%s", outcomes(s), s.Stops, e.log)
	}
	// daily_cap still stops the host, with the budget far off.
	e.cfg.DailyCap = 1
	more := newChat("tokens-more", e.clock.Add(-time.Hour))
	more.branch = "budget-d"
	talk(more, 1, "amber")
	e.write(more)
	if s := e.run(more); len(s.Records) != 0 || !s.Stopped(StopBudget) || calls(e.fake) != 3 {
		t.Fatalf("past the cap: %q %v\n%s", outcomes(s), s.Stops, e.log)
	}
}

// Over several sessions in one job, passes run until the next one's
// estimate would pass the budget; the rest of the host's sessions wait.
func TestTheTokenBudgetStopsAJobPartWay(t *testing.T) {
	e := newEnv(t)
	e.cfg.Switches = map[string]any{"token_budget": true}
	const budget = 1 << 20
	e.cfg.DailyTokenBudget = budget
	var files []string
	for i, b := range []string{"part-a", "part-b", "part-c"} {
		c := newChat("part-"+b, start.Add(time.Duration(i)*time.Hour))
		c.branch = b
		talk(c, 1, "amber")
		files = append(files, e.write(c))
	}
	// The first pass leaves room for the second's estimate and half as much
	// again; the second spends one and a half estimates, so the third, of a
	// chat the same size, no longer fits.
	var est int64
	e.fake.Respond = func(r Request) (Result, error) {
		spend := est + est/2
		if est == 0 {
			est = estimateOf(r)
			spend = budget - 2*est
		}
		return Result{Output: reply(t, r, "amber"), Attempts: 1, TokensIn: int(spend)}, nil
	}
	s, err := e.job().Pending(context.Background())
	if err != nil || outcomes(s) != "ok, ok" || !s.Stopped(StopBudget) || calls(e.fake) != 2 {
		t.Fatalf("pending: %q %v %v, %d calls\n%s", outcomes(s), s.Stops, err, calls(e.fake), e.log)
	}
	if !strings.Contains(e.log.String(), "the next pass needs about") {
		t.Fatalf("the third pass was not stopped by its estimate:\n%s", e.log)
	}
	moved := 0
	for _, f := range files {
		if e.mark(f).Offset > 0 {
			moved++
		}
	}
	if moved != 2 {
		t.Fatalf("%d watermarks moved, want 2", moved)
	}
	records, err := Days(e.dirs.State, e.clock, 1)
	if err != nil {
		t.Fatal(err)
	}
	if got := Spent(records, HostClaude, e.clock).Weighted(); got != budget-est+est/2 {
		t.Fatalf("spent %d of %d, the estimate %d", got, budget, est)
	}
}

// With both switches off, as by default, no budget of tokens stops a
// pass, however small daily_token_budget is and however much a pass
// spends: only daily_cap does.
func TestSwitchesOffOnlyTheCapStops(t *testing.T) {
	e := newEnv(t)
	e.cfg.DailyTokenBudget = 1
	e.cfg.DailyCap = 2
	e.fake.Respond = func(r Request) (Result, error) {
		return Result{Output: reply(t, r, "amber"), Attempts: 1, TokensIn: 1 << 30, TokensOut: 1 << 20}, nil
	}
	var got []string
	for i, b := range []string{"off-a", "off-b", "off-c"} {
		c := newChat("off-"+b, start.Add(time.Duration(i)*time.Hour))
		c.branch = b
		talk(c, 1, "amber")
		e.write(c)
		s := e.run(c)
		got = append(got, outcomes(s)+fmt.Sprint(s.Stops))
	}
	if strings.Join(got, "|") != "ok[]|ok[]|[budget]" || calls(e.fake) != 2 {
		t.Fatalf("runs %q\n%s", got, e.log)
	}
	if !strings.Contains(e.log.String(), "stopped: budget (2 of 2 model calls today)") || strings.Contains(e.log.String(), "weighted") {
		t.Fatalf("log:\n%s", e.log)
	}
}

// effortFor gives the [effort] entry where there is one, else low with
// the switch low_effort_default on, else highest.
func TestEffortFor(t *testing.T) {
	c := config.Default()
	c.Effort = map[string]string{"codex": "medium", "claude-code/claude-test-model": "high"}
	low := c
	low.Switches = map[string]any{"low_effort_default": true}
	for _, tc := range []struct {
		c                 config.Config
		host, model, want string
	}{
		{c, HostClaude, "", config.EffortHighest},
		{c, HostClaude, "claude-test-model", "high"},
		{c, HostCodex, "", "medium"},
		{low, HostClaude, "", config.EffortLow},
		{low, HostClaude, "claude-other-model", config.EffortLow},
		{low, HostClaude, "claude-test-model", "high"},
		{low, HostCodex, "gpt-test-model", "medium"},
	} {
		if got := effortFor(tc.c, tc.host, tc.model); got != tc.want {
			t.Errorf("effortFor(%q, %q) with switches %v = %q, want %q", tc.host, tc.model, tc.c.Switches, got, tc.want)
		}
	}
}

// With the switch low_effort_default on, a pass and doctor --live ask
// each host for low where the config names no effort, and for the entry
// where it names one; off, both ask for the highest, as v1 does.
func TestTheLowEffortDefault(t *testing.T) {
	highest := map[string]string{HostClaude: "max", HostCodex: "ultra"}
	for _, tc := range []struct {
		name   string
		on     bool
		effort map[string]string
		want   map[string]string
	}{
		{"off", false, nil, highest},
		{"on", true, nil, map[string]string{HostClaude: "low", HostCodex: "low"}},
		{"on, with an entry for codex", true, map[string]string{"codex": "medium"}, map[string]string{HostClaude: "low", HostCodex: "medium"}},
		{"off, with an entry for codex", false, map[string]string{"codex": "medium"}, map[string]string{HostClaude: "max", HostCodex: "medium"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			e := newEnv(t)
			if tc.on {
				e.cfg.Switches = map[string]any{"low_effort_default": true}
			}
			e.cfg.Effort = tc.effort
			for i, host := range hosts {
				c := newChat("effort-"+strconv.Itoa(i), start.Add(time.Duration(i)*time.Hour))
				c.host, c.branch = host, "effort-"+host
				talk(c, 1, "amber")
				e.write(c)
				if s := e.run(c); outcomes(s) != "ok" || s.Records[0].Effort != tc.want[host] {
					t.Fatalf("%s: %q %+v\n%s", host, outcomes(s), s.Records, e.log)
				}
				reqs := e.fake.Requests()
				if got := reqs[len(reqs)-1].Effort; got != tc.want[host] {
					t.Fatalf("a pass of %s asked for %q, want %q", host, got, tc.want[host])
				}
				live := &Fake{Result: Result{Reason: ReasonAuth}}
				res, err := Live(context.Background(), live, host, e.cfg, e.dirs.State)
				if err != nil || res.Asked != tc.want[host] || len(live.Requests()) != 1 || live.Requests()[0].Effort != tc.want[host] {
					t.Fatalf("live %s: %+v %v %+v", host, res, err, live.Requests())
				}
			}
		})
	}
}
