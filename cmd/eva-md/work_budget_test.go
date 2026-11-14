package main

import (
	"encoding/json"
	"maps"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/heyNag/eva-md/internal/config"
	"github.com/heyNag/eva-md/internal/update"
	"github.com/heyNag/eva-md/internal/work"
)

// budgetMarker is planted in the budget fixture's session ids and
// entries; no line eva-md work --status prints may hold it.
const budgetMarker = "zqbudgetplanted"

// budgetFixture makes a home with a wiki, the worker on, eva-md work
// --status's clock at workNow, a Claude Code chat and a Codex chat waiting
// for an update, and today's run log: Claude Code's one pass spent 2,000
// weighted tokens, Codex's 10. change sets the rest of the config. It
// returns the run log's file.
func budgetFixture(t *testing.T, change func(*config.Config)) string {
	t.Helper()
	d, _ := updateFixture(t, tangerine)
	saved := workClock
	workClock = func() time.Time { return workNow }
	t.Cleanup(func() { workClock = saved })
	workerOn(t, d, change)
	var log []byte
	for host, tokens := range map[string]int{update.HostClaude: 2000, update.HostCodex: 10} {
		id := "3f2a91c0-" + host + "-" + budgetMarker
		chatFile(t, d, host, id, "main", workNow.Add(-3*time.Hour))
		line, err := json.Marshal(update.Record{At: workNow.Add(-time.Hour).Format(time.RFC3339), Host: host, Session: id, Project: budgetMarker,
			Thread: budgetMarker, Outcome: update.OutcomeOK, Calls: 1, TokensIn: tokens})
		if err != nil {
			t.Fatal(err)
		}
		log = append(append(log, line...), '\n')
	}
	file := filepath.Join(d.State, "updates", workNow.Format(time.DateOnly)+".jsonl")
	if err := os.MkdirAll(filepath.Dir(file), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(file, log, 0o600); err != nil {
		t.Fatal(err)
	}
	return file
}

// reasons is each host's reason among the update job's waits at now.
func reasons(t *testing.T, now time.Time) map[string]work.WaitReason {
	t.Helper()
	out := map[string]work.WaitReason{}
	for _, w := range updateWaiting(t.Context(), now) {
		if _, dup := out[w.Host]; dup {
			t.Fatalf("two waits for %s", w.Host)
		}
		out[w.Host] = w.Reason
	}
	return out
}

// With the worker and the switch token_budget on, the update job's waits
// are deferred by the day's budget for a host whose weighted tokens today
// reached daily_token_budget, and wait for the size or quiet rule the next
// day; a host under its budget waits for the rule. Off, only daily_cap
// defers them, however far past the budget the tokens are. eva-md work
// --status says so, and prints no planted marker.
func TestTheWorkerWaitsOnTheTokenBudget(t *testing.T) {
	budget := func(on bool, tokens int64, dailyCap int) func(*config.Config) {
		return func(c *config.Config) {
			c.Switches["token_budget"] = on
			c.DailyTokenBudget = tokens
			c.DailyCap = dailyCap
		}
	}
	nextDay := workNow.AddDate(0, 0, 1)
	for _, tc := range []struct {
		name          string
		change        func(*config.Config)
		today, toward map[string]work.WaitReason
	}{
		{"budget on, reached by claude", budget(true, 2000, 40),
			map[string]work.WaitReason{update.HostClaude: work.WaitBudget, update.HostCodex: work.WaitRule},
			map[string]work.WaitReason{update.HostClaude: work.WaitRule, update.HostCodex: work.WaitRule}},
		{"budget on, by neither", budget(true, 2001, 40),
			map[string]work.WaitReason{update.HostClaude: work.WaitRule, update.HostCodex: work.WaitRule},
			map[string]work.WaitReason{update.HostClaude: work.WaitRule, update.HostCodex: work.WaitRule}},
		{"budget on, the cap reached", budget(true, 1<<40, 1),
			map[string]work.WaitReason{update.HostClaude: work.WaitBudget, update.HostCodex: work.WaitBudget},
			map[string]work.WaitReason{update.HostClaude: work.WaitRule, update.HostCodex: work.WaitRule}},
		{"budget off, far past it", budget(false, 1, 40),
			map[string]work.WaitReason{update.HostClaude: work.WaitRule, update.HostCodex: work.WaitRule},
			map[string]work.WaitReason{update.HostClaude: work.WaitRule, update.HostCodex: work.WaitRule}},
		{"budget off, the cap reached", budget(false, 1, 1),
			map[string]work.WaitReason{update.HostClaude: work.WaitBudget, update.HostCodex: work.WaitBudget},
			map[string]work.WaitReason{update.HostClaude: work.WaitRule, update.HostCodex: work.WaitRule}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			budgetFixture(t, tc.change)
			if got := reasons(t, workNow); !maps.Equal(got, tc.today) {
				t.Fatalf("today %v, want %v", got, tc.today)
			}
			if got := reasons(t, nextDay); !maps.Equal(got, tc.toward) {
				t.Fatalf("the next day %v, want %v", got, tc.toward)
			}
			code, out, errOut := evaWork(t, "work", "--status")
			if code != 0 || errOut != "" || strings.Contains(strings.ToLower(out), budgetMarker) {
				t.Fatalf("status: %d %q\n%s", code, errOut, out)
			}
			budgets := 0
			for _, r := range tc.today {
				if r == work.WaitBudget {
					budgets++
				}
			}
			if budgets > 0 && !strings.Contains(out, "work: deferred by the day's budget: "+strconv.Itoa(budgets)+", ") ||
				budgets == 0 && strings.Contains(out, "budget") {
				t.Fatalf("status:\n%s", out)
			}
			workClock = func() time.Time { return nextDay }
			if _, out, _ := evaWork(t, "work", "--status"); strings.Contains(out, "budget") || !strings.Contains(out, "work: waiting for the size or quiet rule: 2, ") {
				t.Fatalf("status the next day:\n%s", out)
			}
		})
	}
}

// With the switch token_budget on and the budget reached by both hosts, a
// wait for another reason keeps it: a host held for sign-in still waits
// for sign-in, one in a backoff is still held, and with updates off both
// are off. Only a wait for the size or quiet rule is deferred by the
// budget.
func TestTheBudgetLeavesOtherWaitsTheirReason(t *testing.T) {
	reached := func(c *config.Config) {
		c.Switches["token_budget"] = true
		c.DailyTokenBudget = 10
	}
	t.Run("held and backed off", func(t *testing.T) {
		file := budgetFixture(t, reached)
		var lines []byte
		for host, reason := range map[string]string{update.HostClaude: update.ReasonAuth, update.HostCodex: update.ReasonRateLimit} {
			line, err := json.Marshal(update.Record{At: workNow.Add(-time.Minute).Format(time.RFC3339), Host: host, Session: "4a3b92d1-" + budgetMarker,
				Outcome: update.OutcomeFailed, Reason: reason, Calls: 1})
			if err != nil {
				t.Fatal(err)
			}
			lines = append(append(lines, line...), '\n')
		}
		f, err := os.OpenFile(file, os.O_APPEND|os.O_WRONLY, 0)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := f.Write(lines); err != nil {
			t.Fatal(err)
		}
		if err := f.Close(); err != nil {
			t.Fatal(err)
		}
		want := map[string]work.WaitReason{update.HostClaude: work.WaitSignIn, update.HostCodex: work.WaitHeld}
		if got := reasons(t, workNow); !maps.Equal(got, want) {
			t.Fatalf("waits %v, want %v", got, want)
		}
	})
	t.Run("updates off", func(t *testing.T) {
		budgetFixture(t, func(c *config.Config) {
			reached(c)
			c.Updates = false
		})
		want := map[string]work.WaitReason{update.HostClaude: work.WaitOff, update.HostCodex: work.WaitOff}
		if got := reasons(t, workNow); !maps.Equal(got, want) {
			t.Fatalf("waits %v, want %v", got, want)
		}
	})
}
