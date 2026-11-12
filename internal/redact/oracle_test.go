package redact

import (
	"context"
	"fmt"
	"regexp"
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

// oracleNear reports whether class is the near class of a gap shape: secrets
// just outside the exceptions that make the shape, as oracle_near_<shape> in
// corpus/oracle_near.txt and gen_oracle_near_<shape> from a generator. A gap
// class excuses its whole shape, so without them an exception that widens
// would keep secrets the baseline rules remove and no class would see it.
// They are held to the baseline like any class that is no gap.
func oracleNear(class string) bool {
	return strings.HasPrefix(strings.TrimPrefix(class, "gen_"), "oracle_near_")
}

// oracleNearShapes are, for each gap shape, the edges of the exceptions of
// plainCode that its near class plants secrets on, each with an example: a
// made-up secret just outside the exception, which the baseline rules and
// the current rules alone remove. The value is the part of the example the
// baseline rules replace.
var oracleNearShapes = map[string][]struct{ text, value string }{
	"regex_value": {
		// A shell assignment is no code.
		{"SECRET_KEY=/Qm7Rt2+" + "Wx9Lp4Zk/n", "/Qm7Rt2+" + "Wx9Lp4Zk/n"},
		// No metacharacter.
		{"SECRET_KEY: /Qm7Rt2" + "Wx9Lp4Zk/n", "/Qm7Rt2" + "Wx9Lp4Zk/n"},
		// Flags that repeat.
		{"SECRET_KEY: /Qm7Rt2+" + "Wx9Lp4Zk/gg", "/Qm7Rt2+" + "Wx9Lp4Zk/gg"},
	},
	"yaml_alias": {
		// A random token is no alias name.
		{"password: *Hmyhyb" + "bz82", "*Hmyhyb" + "bz82"},
		// More on the line after the alias.
		{"password: *Summer2" + "Winter # x", "*Summer2" + "Winter"},
		// After '=', not ':'.
		{"password = *Summer2" + "Winter", "*Summer2" + "Winter"},
	},
	"name_index": {
		// A random token before an index or a literal.
		{"password: Xk9mP2" + "vLq7Rt[x9]", "Xk9mP2" + "vLq7Rt"},
		{"password = Xk9mP2" + "vLq7Rt{ab}", "Xk9mP2" + "vLq7Rt"},
		// A member path and an instance variable of random names.
		{"password: Xk9mP2." + "vLq7Rt[2]", "Xk9mP2." + "vLq7Rt"},
		{"password: @Xk9mP2" + "vLq7Rt[i]", "@Xk9mP2" + "vLq7Rt"},
		// Words before brackets that hold no expression.
		{"password: hunter" + "Two[a;b]", "hunter" + "Two"},
		// Words before an index that goes on as no code does.
		{"password: hunter" + "Two[x9]:", "hunter" + "Two"},
		// Words before an index in a shell assignment.
		{"PASSWORD=hunter" + "Two[x9]", "hunter" + "Two"},
	},
	"type_annotation": {
		// No space around the '=' or the '|'.
		{"password: Xk9mP2" + "vLq7Rt=1", "Xk9mP2" + "vLq7Rt"},
		{"secret: aB3dE5" + "fG7hJ2|None", "aB3dE5" + "fG7hJ2"},
		// A shell assignment.
		{"PASSWORD=Xk9mP2" + "vLq7Rt = 1", "Xk9mP2" + "vLq7Rt"},
	},
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
// move to the class of their shape and are held to none. Each gap shape must
// have a near class, whose every secret the baseline rules remove, so the
// oracle holds the current rules on the edges of each gap.
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
	// Each gap shape has a near class, in the corpus and from a generator,
	// whose every secret the baseline rules remove, so the oracle judges each
	// one. A near secret the baseline rules keep would hold the current rules
	// to nothing.
	near := map[string]int{}
	for i, c := range m.cases {
		if !oracleNear(c.class) {
			continue
		}
		near[c.class] += len(c.secrets)
		for _, s := range c.secrets {
			if keeps(m.baseline[i].out, c.value(s)) {
				t.Errorf("the baseline rules keep a secret of near class %s, which the oracle then does not judge: %s", c.class, c)
			}
		}
	}
	for _, g := range oracleGapShapes {
		for _, class := range []string{"oracle_near_" + g.shape, "gen_oracle_near_" + g.shape} {
			if near[class] == 0 {
				t.Errorf("gap shape %s has no near class %s", g.shape, class)
			}
			delete(near, class)
		}
	}
	if len(near) > 0 {
		t.Errorf("near classes of no gap shape: %v", near)
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

// Each near shape is a gap shape's, and on each of its examples the baseline
// rules, v1 and the current rules alone, as Redact runs them with the switch
// on, remove the value: the current rules remove a secret just outside each
// exception today.
func TestOracleNearShapesAreRemoved(t *testing.T) {
	isolate(t)
	currentRulesOnly(t)
	shapes := map[string]bool{}
	for _, g := range oracleGapShapes {
		shapes[g.shape] = true
		if len(oracleNearShapes[g.shape]) == 0 {
			t.Errorf("gap shape %s has no near example", g.shape)
		}
	}
	for shape, examples := range oracleNearShapes {
		if !shapes[shape] {
			t.Errorf("near shape %s is no gap shape", shape)
		}
		for _, e := range examples {
			if !strings.Contains(e.text, e.value) {
				t.Fatalf("%s: example %q does not hold %q", shape, e.text, e.value)
			}
			base, _ := baseline.Redact(e.text)
			v1 := Apply(driver{ctx: context.Background()}.scan(e.text))
			alone, _ := Redact(e.text)
			if keeps(base, e.value) || keeps(v1, e.value) || keeps(alone, e.value) {
				t.Errorf("%s: a rule set keeps the value of %q: baseline %q, v1 %q, the current rules alone %q", shape, e.text, base, v1, alone)
			}
		}
	}
}

// widenedExceptions stand for rule changes that widen an exception of
// plainCode past its gap shape: each keeps the text of every line of that
// shape whole, as the current rules alone would with the exception widened,
// and leaves every other case to the current rules alone.
var widenedExceptions = []struct {
	shape, change string
	line          *regexp.Regexp
}{
	// After ':', any '*' value read as an alias, not only a name of words:
	// aliasName dropped.
	{"yaml_alias", "an alias of any name", regexp.MustCompile(`(?m)^[ \t]*[A-Za-z_][\w.]*[ \t]*:=?[ \t]*\*[A-Za-z_][A-Za-z0-9_.-]*[ \t]*$`)},
	// Any name before an index or a literal, not only one of words:
	// wordLike(v, 2) dropped.
	{"name_index", "an index on any name", regexp.MustCompile(`(?m)^[ \t]*[A-Za-z_][\w.]*(?:[ \t]*:=?[ \t]*|[ \t]+=>?[ \t]+)@?[A-Za-z_][A-Za-z0-9_.]*(?:\[[^\]\n;~#]*[A-Za-z0-9][^\]\n;~#]*\]|\{[^}\n;~#]*[A-Za-z0-9][^}\n;~#]*\}|[ \t]+\{[^}\n]*\})(?:[ \t,;)\]}.\[(]|$)`)},
	// A regular expression literal with no metacharacter.
	{"regex_value", "a slashed value of any characters", regexp.MustCompile(`(?m)^[ \t]*[A-Za-z_][\w.]*[ \t]*(?::|[ \t]=)[ \t]*/[^/\s]+/[a-z]*(?:[ \t;,]|$)`)},
	// A type annotation with no space around its '=' or '|'.
	{"type_annotation", "an unspaced annotation", regexp.MustCompile(`(?m)^[ \t]*[A-Za-z_][\w.]*[ \t]*(?::|[ \t]=)[ \t]*[A-Za-z_][\w.]*[ \t]*[=|][ \t]*\S`)},
}

// The oracle fails a rule change that widens an exception past its gap
// shape: each widened exception keeps secrets of its shape's near class,
// labelled and generated, which the oracle reports. Without the near
// classes the gap class excuses the whole shape and no other class plants a
// secret there, so the oracle would take such a change.
func TestOracleFailsAWidenedException(t *testing.T) {
	isolate(t)
	m := measure(t)
	shapes := map[string]bool{}
	for _, w := range widenedExceptions {
		shapes[w.shape] = true
		t.Run(w.shape, func(t *testing.T) {
			widened := func(i int) string {
				if w.line.MatchString(m.cases[i].text) {
					return m.cases[i].text
				}
				return m.alone[i].out
			}
			if !slices.ContainsFunc(oracleNearShapes[w.shape], func(e struct{ text, value string }) bool { return w.line.MatchString(e.text) }) {
				t.Fatalf("%s matches no near example of %s", w.change, w.shape)
			}
			bad, _, _ := oracleViolations(m, m.cases, widened)
			seen := map[string]bool{}
			for _, b := range bad {
				class, _, _ := strings.Cut(strings.TrimPrefix(b, "class "), ",")
				seen[class] = true
			}
			for _, class := range []string{"oracle_near_" + w.shape, "gen_oracle_near_" + w.shape} {
				if !seen[class] {
					t.Errorf("the oracle took %s: no violation in %s (violations in %v)", w.change, class, seen)
				}
			}
			t.Logf("%s: %d violations, in %v", w.change, len(bad), seen)
		})
	}
	for _, g := range oracleGapShapes {
		if !shapes[g.shape] {
			t.Errorf("gap shape %s has no widened exception", g.shape)
		}
	}
}
