package config

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// daily_token_budget is read as written; 0 or absent gives the default
// budget, and is never written by a save, so a config saved without it
// is the same as before the key; a negative one makes the config invalid.
func TestDailyTokenBudget(t *testing.T) {
	isolate(t)
	file := filepath.Join(t.TempDir(), "config.toml")
	for _, tc := range []struct {
		body       string
		field, use int64
	}{
		{"notice = \"start\"\n", 0, DefaultDailyTokenBudget},
		{"daily_token_budget = 0\n", 0, DefaultDailyTokenBudget},
		{"daily_token_budget = 250000\n", 250000, 250000},
		{"daily_token_budget = 1\n", 1, 1},
	} {
		if err := os.WriteFile(file, []byte(tc.body), 0o600); err != nil {
			t.Fatal(err)
		}
		c, err := Load(file)
		if err != nil {
			t.Fatalf("%q: %v", tc.body, err)
		}
		if c.DailyTokenBudget != tc.field || c.TokenBudget() != tc.use || len(c.Unknown) != 0 {
			t.Errorf("%q: field %d, budget %d, unknown %q; want %d and %d", tc.body, c.DailyTokenBudget, c.TokenBudget(), c.Unknown, tc.field, tc.use)
		}
	}
	if DefaultDailyTokenBudget != 1_000_000 || Default().TokenBudget() != DefaultDailyTokenBudget {
		t.Fatalf("the default budget is %d", Default().TokenBudget())
	}
	for _, body := range []string{"daily_token_budget = -1\n", "daily_token_budget = \"lots\"\n"} {
		if err := os.WriteFile(file, []byte(body), 0o600); err != nil {
			t.Fatal(err)
		}
		if c, err := Load(file); err == nil || c.DailyTokenBudget != 0 {
			t.Errorf("%q loaded: %+v %v", body, c, err)
		}
	}
	bad := Default()
	bad.DailyTokenBudget = -5
	if err := bad.Validate(); err == nil || !strings.Contains(err.Error(), "daily_token_budget") {
		t.Fatalf("Validate: %v", err)
	}
	// A save keeps a budget that is set, and writes no key for one that
	// is not.
	saved := filepath.Join(t.TempDir(), "saved.toml")
	if err := Save(saved, Default()); err != nil {
		t.Fatal(err)
	}
	if data, _ := os.ReadFile(saved); bytes.Contains(data, []byte("daily_token_budget")) {
		t.Fatalf("a save without a budget wrote one:\n%s", data)
	}
	set := Default()
	set.DailyTokenBudget = 300000
	if err := Save(saved, set); err != nil {
		t.Fatal(err)
	}
	if c, err := Load(saved); err != nil || c.DailyTokenBudget != 300000 {
		t.Fatalf("saved budget read back as %d: %v", c.DailyTokenBudget, err)
	}
}

// EffortEntry tells an [effort] entry from none: the "<host>/<model>"
// entry first, then the "<host>" one, trimmed, and no entry gives false.
// EffortFor keeps its meaning: the entry, else highest.
func TestEffortEntry(t *testing.T) {
	c := Default()
	c.Effort = map[string]string{"codex": " medium ", "claude-code/claude-test-model": "high", "claude-code/blank-model": "  "}
	for _, tc := range []struct {
		host, model, want string
		ok                bool
	}{
		{"codex", "", "medium", true},
		{"codex", "codex-test-model", "medium", true},
		{"claude-code", "claude-test-model", "high", true},
		{"claude-code", "claude-other-model", "", false},
		{"claude-code", "", "", false},
		{"claude-code", "blank-model", "", false},
	} {
		got, ok := c.EffortEntry(tc.host, tc.model)
		if got != tc.want || ok != tc.ok {
			t.Errorf("EffortEntry(%q, %q) = %q, %v; want %q, %v", tc.host, tc.model, got, ok, tc.want, tc.ok)
		}
		want := tc.want
		if !tc.ok {
			want = EffortHighest
		}
		if got := c.EffortFor(tc.host, tc.model); got != want {
			t.Errorf("EffortFor(%q, %q) = %q, want %q", tc.host, tc.model, got, want)
		}
	}
	if EffortLow != "low" {
		t.Fatalf("EffortLow is %q", EffortLow)
	}
}
