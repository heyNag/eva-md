package config

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/heyNag/eva-md/internal/redact"
)

// redactionText is a line of code the baseline redaction rules take by
// mistake and the current rules keep, so the two ways of redacting differ
// on it.
const redactionText = "\tcfgSecret := store.readSecret[LevelHandshake]"

// v1Redaction is redactionText as v1 redacts it, with the baseline rules.
const v1Redaction = "\tcfgSecret := [REDACTED:secret_value][LevelHandshake]"

// A text on which the two ways of redacting differ is redacted each way as
// the config file config.Load read says: v1's union by default and with the
// switch off, the current rules alone with it on, and v1's again once a
// config without it is loaded.
func TestRedactionFollowsTheConfig(t *testing.T) {
	isolate(t)
	t.Cleanup(func() { redact.SetBaselineAtRunTime(true) })
	dir := t.TempDir()
	write := func(name, body string) string {
		file := filepath.Join(dir, name)
		if err := os.WriteFile(file, []byte(body), 0o600); err != nil {
			t.Fatal(err)
		}
		return file
	}
	for _, c := range []struct {
		name, body string
		want       string
	}{
		{"on.toml", "[switches]\nredaction_current_rules_only = true\n", redactionText},
		{"none.toml", "notice = \"start\"\n", v1Redaction},
		{"on-again.toml", "[switches]\nredaction_current_rules_only = true\n", redactionText},
		{"off.toml", "[switches]\nredaction_current_rules_only = false\n", v1Redaction},
	} {
		cfg, err := Load(write(c.name, c.body))
		if err != nil {
			t.Fatalf("%s: %v", c.name, err)
		}
		on := RedactionCurrentRulesOnly.On(cfg)
		if out, _ := redact.Redact(redactionText); out != c.want || redact.BaselineAtRunTime() == on {
			t.Fatalf("%s: switch %v, baseline at run time %v, Redact = %q; want %q", c.name, on, redact.BaselineAtRunTime(), out, c.want)
		}
	}
}

// Every config LoadContext returns sets the switch, Default() included: a
// missing file, a file that is no TOML and one that fails to validate each
// give Default(), and with it v1's redaction, after a config that had
// turned the baseline rules off.
func TestRedactionFollowsDefaultOnError(t *testing.T) {
	isolate(t)
	t.Cleanup(func() { redact.SetBaselineAtRunTime(true) })
	dir := t.TempDir()
	on := filepath.Join(dir, "on.toml")
	if err := os.WriteFile(on, []byte("[switches]\nredaction_current_rules_only = true\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	bad := map[string]string{
		"not toml":       "[switches\n",
		"invalid notice": "notice = \"loud\"\n[switches]\nredaction_current_rules_only = true\n",
		"bad switch":     "[switches]\nredaction_current_rules_only = \"yes\"\n",
	}
	cases := []string{filepath.Join(dir, "missing.toml")}
	for name, body := range bad {
		file := filepath.Join(dir, name+".toml")
		if err := os.WriteFile(file, []byte(body), 0o600); err != nil {
			t.Fatal(err)
		}
		cases = append(cases, file)
	}
	for _, file := range cases {
		if _, err := Load(on); err != nil || redact.BaselineAtRunTime() {
			t.Fatalf("the switch on did not turn the baseline rules off: %v", err)
		}
		cfg, err := Load(file)
		if err == nil {
			t.Fatalf("%s loaded", file)
		}
		if RedactionCurrentRulesOnly.On(cfg) || !redact.BaselineAtRunTime() {
			t.Fatalf("%s: Default() left the baseline rules off", file)
		}
		if out, _ := redact.Redact(redactionText); out != v1Redaction {
			t.Fatalf("%s: Redact = %q, want v1's %q", file, out, v1Redaction)
		}
	}
}

// The switch is off by default.
func TestRedactionSwitchOffByDefault(t *testing.T) {
	if RedactionCurrentRulesOnly.On(Default()) {
		t.Fatal("redaction_current_rules_only is on by default")
	}
}
