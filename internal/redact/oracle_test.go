package redact

import (
	"context"
	"fmt"
	"strings"
	"testing"
)

// The frozen baseline as an oracle (decision 7 of the plan): for every
// labelled secret of the corpus and every generated one, if the baseline
// rules' output does not keep it, the current rules alone must not keep it.
// A secret is kept when the output holds any 6 consecutive characters of
// it. If this fails, no rule is changed and the measure is not loosened to
// pass it: the class and the case it names are reported.

// oracleViolations returns, for each secret of cases that the baseline
// rules' output, as m measured it, does not keep and engine's output does,
// the class and the case, and how many secrets it judged.
func oracleViolations(m *measured, engine func(i int) string) (bad []string, judged int) {
	for i, c := range m.cases {
		if c.label != labelSecret {
			continue
		}
		var out string
		for _, s := range c.secrets {
			v := c.value(s)
			judged++
			if keeps(m.baseline[i].out, v) {
				continue
			}
			if out == "" {
				out = engine(i)
			}
			if keeps(out, v) {
				bad = append(bad, fmt.Sprintf("class %s, %s: secret %q kept in %q", c.class, c, v, out))
			}
		}
	}
	return bad, judged
}

// TestOracle holds the current rules alone to the baseline rules on every
// labelled secret and on at least 20,000 generated ones (under the race
// detector, a twentieth of them).
func TestOracle(t *testing.T) {
	isolate(t)
	m := measure(t)
	if !raceOn && m.generated < 20000 {
		t.Fatalf("%d generated secrets, want at least 20,000", m.generated)
	}
	labelled := 0
	for _, c := range m.cases[:m.corpusCount] {
		labelled += len(c.secrets)
	}
	bad, judged := oracleViolations(m, func(i int) string { return m.alone[i].out })
	if judged != labelled+m.generated {
		t.Fatalf("judged %d secrets, want %d labelled and %d generated", judged, labelled, m.generated)
	}
	for i, b := range bad {
		if i == 20 {
			t.Errorf("and %d more", len(bad)-20)
			break
		}
		t.Errorf("the current rules alone keep a secret the baseline rules remove: %s", b)
	}
	// v1's engine runs the baseline rules, so it keeps none either.
	if bad, _ := oracleViolations(m, func(i int) string { return m.v1[i].out }); len(bad) > 0 {
		t.Errorf("v1's engine keeps %d secrets the baseline rules remove, as %s", len(bad), bad[0])
	}
}

// The oracle fails an engine that keeps the value after password=, and
// names the cases.
func TestOracleFailsAnEngineThatKeepsAPassword(t *testing.T) {
	isolate(t)
	m := measure(t)
	keepsPasswords := func(i int) string {
		text := m.cases[i].text
		if strings.Contains(text, "password=") {
			return text
		}
		return Apply(driver{ctx: context.Background(), alone: true}.scan(text))
	}
	bad, _ := oracleViolations(m, keepsPasswords)
	if len(bad) == 0 {
		t.Fatal("the oracle took an engine that keeps the value after password=")
	}
	labelled := false
	for _, b := range bad {
		if !strings.Contains(b, "password=") {
			t.Fatalf("a violation where the engine runs the current rules: %s", b)
		}
		labelled = labelled || !strings.Contains(b, "generated:")
	}
	if !labelled {
		t.Fatal("no labelled case among the violations")
	}
	t.Logf("%d violations, as %s", len(bad), bad[0])
}
