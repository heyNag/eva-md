package redact

import (
	"context"
	"fmt"
	"slices"
	"strings"
	"testing"

	"github.com/heyNag/eva-md/internal/redact/baseline"
)

// The frozen baseline as an oracle (decision 7 of the plan): for every
// labelled secret of the corpus and every generated one, if the baseline
// rules' output does not keep it, the current rules alone must not keep it.
// A secret is kept when the output holds any 6 consecutive characters of
// it. When this fails, no rule is changed and the measure is not loosened to
// pass it: the class and the case it names are reported, and the shape is
// recorded as a gap class (see oracleGap and corpus/oracle_gaps.txt).

// oracleGap reports whether class is a gap the oracle found: a shape in
// which the current rules alone keep secrets the baseline rules remove,
// recorded in corpus/oracle_gaps.txt and by its generator until a rule
// change closes it (LG-26). Its violations are counted apart, and every
// other class is held to none.
func oracleGap(class string) bool {
	return strings.HasPrefix(strings.TrimPrefix(class, "gen_"), "oracle_gap_")
}

// oracleGapShapes are the gap classes, as oracle_gap_<shape>: each shape of
// code that the current rules keep on purpose (plainCode in redact.go, and
// the comparison floorValue leaves alone) and the baseline rules remove,
// where a secret can take that shape. Each has cases in
// corpus/oracle_gaps.txt and a generator, and examples, one for each
// exception it stands for, made up and joined from parts so no line holds a
// whole one. The value is the part of the example the baseline rules
// replace.
var oracleGapShapes = []struct {
	shape    string
	examples []struct{ text, value string }
}{
	{"regex_value", []struct{ text, value string }{
		// After a spaced separator, a regular expression literal.
		{"SECRET_KEY: /Qm7Rt2+" + "Wx9Lp4Zk/n", "/Qm7Rt2+" + "Wx9Lp4Zk/n"},
	}},
	{"yaml_alias", []struct{ text, value string }{
		// After ':', a YAML alias made of words.
		{"password: *Summer2" + "Winter", "*Summer2" + "Winter"},
		// After a spaced "==", a comparison, which floorValue leaves to the
		// other rules: they keep a value of words after a '*'.
		{"token == *Winter0" + "Meadow", "*Winter0" + "Meadow"},
	}},
	{"name_index", []struct{ text, value string }{
		// An index on a member path or an instance variable.
		{"password: Summer." + "Winter[2]", "Summer." + "Winter"},
		{"password: @summer_" + "winter[x9]", "@summer_" + "winter"},
		// A block after a member path.
		{"password: Walnut." + "Cobalt { x }", "Walnut." + "Cobalt"},
		// An index by a quoted name.
		{`password: hunter` + `Two["ab"]`, "hunter" + "Two"},
		// After ':' or a spaced '=', a literal or index on a name of two or
		// more words.
		{"password = hunter" + "Two[x9]", "hunter" + "Two"},
		{"api_key: canyon" + "River{ab}", "canyon" + "River"},
	}},
	{"type_annotation", []struct{ text, value string }{
		// A type annotation, as a union of types.
		{"secret: aB3dE5" + "fG7hJ2 | None", "aB3dE5" + "fG7hJ2"},
		// A chained assignment.
		{"password: Xk9mP2" + "vLq7Rt = 1", "Xk9mP2" + "vLq7Rt"},
	}},
}

// oracleViolations returns, for each secret of cases that the baseline
// rules' output, as m measured it, does not keep and engine's output does,
// the class and the case, outside the gap classes; the number of such
// secrets in each gap class; and how many secrets it judged.
func oracleViolations(m *measured, cases []corpusCase, engine func(i int) string) (bad []string, gaps map[string]int, judged int) {
	gaps = map[string]int{}
	for i, c := range cases {
		if c.label != labelSecret {
			continue
		}
		var out string
		judgedOut := false
		for _, s := range c.secrets {
			v := c.value(s)
			judged++
			if keeps(m.baseline[i].out, v) {
				continue
			}
			if !judgedOut {
				out, judgedOut = engine(i), true
			}
			if !keeps(out, v) {
				continue
			}
			if oracleGap(c.class) {
				gaps[c.class]++
				continue
			}
			bad = append(bad, fmt.Sprintf("class %s, %s: secret %q kept in %q", c.class, c, v, out))
		}
	}
	return bad, gaps, judged
}

// TestOracle holds the current rules alone to the baseline rules on every
// labelled secret and on at least 20,000 generated ones (under the race
// detector, a twentieth of them), but for the gap classes. Each gap class
// must still have a violation: when a rule change closes one, its cases
// move to the class of their shape and are held to none.
func TestOracle(t *testing.T) {
	isolate(t)
	m := measure(t)
	if !raceOn && m.generated < 20000 {
		t.Fatalf("%d generated secrets, want at least 20,000", m.generated)
	}
	labelled := 0
	gapClasses := map[string]bool{}
	for i, c := range m.cases {
		if i < m.corpusCount {
			labelled += len(c.secrets)
		}
		if oracleGap(c.class) {
			gapClasses[c.class] = true
		}
	}
	bad, gaps, judged := oracleViolations(m, m.cases, func(i int) string { return m.alone[i].out })
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
	known := map[string]bool{}
	for _, g := range oracleGapShapes {
		class := "oracle_gap_" + g.shape
		known[class], known["gen_"+class] = true, true
		if !gapClasses[class] || !gapClasses["gen_"+class] {
			t.Errorf("gap class %s is not in the corpus and the generators: %v", class, gapClasses)
		}
	}
	for _, class := range slices.Sorted(boolKeys(gapClasses)) {
		if !known[class] {
			t.Errorf("gap class %s is not in oracleGapShapes", class)
		}
		if gaps[class] == 0 {
			t.Errorf("gap class %s has no violation left: move its cases to the class of their shape", class)
		}
	}
	t.Logf("violations in the gap classes: %v", gaps)
	// v1's engine runs the baseline rules, so it keeps none either, in the
	// gap classes too.
	if bad, gaps, _ := oracleViolations(m, m.cases, func(i int) string { return m.v1[i].out }); len(bad)+len(gaps) > 0 {
		t.Errorf("v1's engine keeps secrets the baseline rules remove: %v, gaps %v", bad, gaps)
	}
}

func boolKeys(m map[string]bool) func(func(string) bool) {
	return func(yield func(string) bool) {
		for k := range m {
			if !yield(k) {
				return
			}
		}
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
		return m.alone[i].out
	}
	bad, _, _ := oracleViolations(m, m.cases, keepsPasswords)
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

// The gap classes excuse only themselves: the same secrets in any other
// class are violations, and an engine that keeps none leaves no gap.
func TestOracleExcusesOnlyTheGapClasses(t *testing.T) {
	isolate(t)
	m := measure(t)
	alone := func(i int) string { return m.alone[i].out }
	cases := slices.Clone(m.cases)
	moved := map[string]bool{}
	for i := range cases {
		if oracleGap(cases[i].class) {
			cases[i].class = strings.Replace(cases[i].class, "oracle_gap_", "key_value_", 1)
			moved[cases[i].class] = true
		}
	}
	if len(moved) != 2*len(oracleGapShapes) {
		t.Fatalf("gap classes moved: %v, want the corpus's and the generators' of %d shapes", moved, len(oracleGapShapes))
	}
	bad, gaps, _ := oracleViolations(m, cases, alone)
	if len(bad) == 0 || len(gaps) != 0 {
		t.Fatalf("with the gap classes renamed: %d violations, gaps %v", len(bad), gaps)
	}
	seen := map[string]bool{}
	for _, b := range bad {
		class, _, _ := strings.Cut(strings.TrimPrefix(b, "class "), ",")
		if !moved[class] {
			t.Fatalf("a violation outside the gaps' cases: %s", b)
		}
		seen[class] = true
	}
	if len(seen) != len(moved) {
		t.Fatalf("violations in %v, want one in each of %v", seen, moved)
	}
	if bad, gaps, _ := oracleViolations(m, m.cases, func(int) string { return "" }); len(bad)+len(gaps) != 0 {
		t.Fatalf("an engine that keeps nothing: %v, gaps %v", bad, gaps)
	}
}

// Each gap class stands for exceptions of plainCode that a secret can take:
// on each example, the baseline rules and v1 remove the value, and the
// current rules alone, as Redact runs them with the switch on, keep it. When
// a rule change closes an exception, this fails on its example: drop the
// example, and once a shape has none left, move its cases to the class of
// their shape and drop the shape. An exception of plainCode with no example
// here is a gap the oracle may not see.
func TestOracleGapShapesAreWhatPlainCodeKeeps(t *testing.T) {
	isolate(t)
	currentRulesOnly(t)
	for _, g := range oracleGapShapes {
		if len(g.examples) == 0 {
			t.Errorf("gap shape %s has no example", g.shape)
		}
		for _, e := range g.examples {
			if !strings.Contains(e.text, e.value) {
				t.Fatalf("%s: example %q does not hold %q", g.shape, e.text, e.value)
			}
			base, _ := baseline.Redact(e.text)
			v1 := Apply(driver{ctx: context.Background()}.scan(e.text))
			if keeps(base, e.value) || keeps(v1, e.value) {
				t.Errorf("%s: the baseline rules or v1 keep the value of %q: %q, %q", g.shape, e.text, base, v1)
			}
			if alone, _ := Redact(e.text); !keeps(alone, e.value) {
				t.Errorf("%s: the current rules alone now remove the value of %q (%q): this exception is closed", g.shape, e.text, alone)
			}
		}
	}
}
