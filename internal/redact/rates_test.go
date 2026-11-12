package redact

import (
	"bytes"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"text/tabwriter"
)

// The rates of the two engines over the corpus and the generators, kept in
// testdata/rates.txt: recall for each class of secret, the share of each
// class of negative cases changed, precision over the ranges replaced, the
// rate of facts lost, the rate of 32-character hex keys kept, and the
// oracle's violations.

// ratesFile holds the rates.
const ratesFile = "testdata/rates.txt"

// rewriteRatesFlag is the flag that rewrites ratesFile, as its header says.
const rewriteRatesFlag = "rewrite-rates"

var rewriteRates = flag.Bool(rewriteRatesFlag, false, "rewrite "+ratesFile+" with the rates this run measures")

// ratesHeader starts ratesFile.
var ratesHeader = `# Redaction rates of v1's engine (Redact with the baseline rules at run
# time, column v1) and of the current rules alone (column current), over the
# labelled corpus in corpus/ and the seeded generators of corpus_test.go.
# TestRates fails on any difference from what it measures. After a change to
# the rules, the corpus or the generators, rewrite this file with
#   go test ./internal/redact -run '^TestRates$' -args -` + rewriteRatesFlag + `
# and commit it with the change.
#
# recall.<class>       the share of a class's secrets removed. A secret is
#                      kept when the output holds any 6 consecutive
#                      characters of it. Classes gen_* are generated.
# changed.<class>      the share of a class of negative cases changed at all.
# spans_replaced       the ranges replaced in every case, of the corpus files
#                      and generated.
# spans_on_a_secret    those of them that hold part of a labelled or
#                      generated secret.
# precision            spans_on_a_secret over spans_replaced.
# fact_loss_rate       the share of facts whose value the output no longer
#                      holds whole.
# hex32_kept_rate      the share of the 32-character hex keys after a trigger
#                      phrase (class gen_hex32) kept.
# oracle_violations    secrets, labelled or generated, that the baseline rules
#                      remove and the engine keeps (TestOracle).
# oracle_gap_violations
#                      those of them in the gap classes (*oracle_gap_*), the
#                      shapes recorded in corpus/oracle_gaps.txt, where the
#                      current rules alone are known to keep secrets the
#                      baseline rules remove. TestOracle fails on any other.
#
`

// rateRow is one figure for both engines.
type rateRow struct {
	name    string
	n       string
	v1, cur string
}

// engineFigures are one engine's counts.
type engineFigures struct {
	removed, secrets      map[string]int // by class
	changed, negatives    map[string]int // by class
	spans, onSecret       int
	lost, facts           int
	hexKept, hex          int
	violations, oracleAll int
	gapViolations, gapAll int
}

// figures counts what one engine, whose run of case i is run(i), did to
// cases, as m measured them. cases are m's, perhaps with other labels.
func figures(m *measured, cases []corpusCase, run func(i int) engineRun) engineFigures {
	f := engineFigures{removed: map[string]int{}, secrets: map[string]int{}, changed: map[string]int{}, negatives: map[string]int{}}
	for i, c := range cases {
		e := run(i)
		switch c.label {
		case labelSecret:
			for _, s := range c.secrets {
				v := c.value(s)
				kept := keeps(e.out, v)
				f.secrets[c.class]++
				if !kept {
					f.removed[c.class]++
				}
				if c.class == "gen_hex32" {
					f.hex++
					if kept {
						f.hexKept++
					}
				}
				f.oracleAll++
				violates := kept && !keeps(m.baseline[i].out, v)
				if violates {
					f.violations++
				}
				if oracleGap(c.class) {
					f.gapAll++
					if violates {
						f.gapViolations++
					}
				}
			}
		case labelNegative:
			f.negatives[c.class]++
			if e.out != c.text {
				f.changed[c.class]++
			}
		case labelFact:
			for _, r := range c.facts {
				f.facts++
				if !strings.Contains(e.out, c.value(r)) {
					f.lost++
				}
			}
		}
		for _, sp := range e.spans {
			f.spans++
			for _, s := range c.secrets {
				if c.label == labelSecret && sp.start < s.end && s.start < sp.end {
					f.onSecret++
					break
				}
			}
		}
	}
	return f
}

// ratesTable returns the rates of both engines on cases as the text of
// ratesFile.
func ratesTable(m *measured, cases []corpusCase) string {
	v1 := figures(m, cases, func(i int) engineRun { return m.v1[i] })
	cur := figures(m, cases, func(i int) engineRun { return m.alone[i] })
	ratio := func(a, b int) string {
		if b == 0 {
			return "-"
		}
		return fmt.Sprintf("%.4f", float64(a)/float64(b))
	}
	var rows []rateRow
	for _, class := range slices.Sorted(mapsKeys(v1.secrets)) {
		rows = append(rows, rateRow{"recall." + class, fmt.Sprint(v1.secrets[class]), ratio(v1.removed[class], v1.secrets[class]), ratio(cur.removed[class], cur.secrets[class])})
	}
	for _, class := range slices.Sorted(mapsKeys(v1.negatives)) {
		rows = append(rows, rateRow{"changed." + class, fmt.Sprint(v1.negatives[class]), ratio(v1.changed[class], v1.negatives[class]), ratio(cur.changed[class], cur.negatives[class])})
	}
	rows = append(rows,
		rateRow{"spans_replaced", "-", fmt.Sprint(v1.spans), fmt.Sprint(cur.spans)},
		rateRow{"spans_on_a_secret", "-", fmt.Sprint(v1.onSecret), fmt.Sprint(cur.onSecret)},
		rateRow{"precision", "-", ratio(v1.onSecret, v1.spans), ratio(cur.onSecret, cur.spans)},
		rateRow{"fact_loss_rate", fmt.Sprint(v1.facts), ratio(v1.lost, v1.facts), ratio(cur.lost, cur.facts)},
		rateRow{"hex32_kept_rate", fmt.Sprint(v1.hex), ratio(v1.hexKept, v1.hex), ratio(cur.hexKept, cur.hex)},
		rateRow{"oracle_violations", fmt.Sprint(v1.oracleAll), fmt.Sprint(v1.violations), fmt.Sprint(cur.violations)},
		rateRow{"oracle_gap_violations", fmt.Sprint(v1.gapAll), fmt.Sprint(v1.gapViolations), fmt.Sprint(cur.gapViolations)},
	)
	var b bytes.Buffer
	b.WriteString(ratesHeader)
	w := tabwriter.NewWriter(&b, 0, 0, 2, ' ', 0)
	fmt.Fprintln(w, "figure\tn\tv1\tcurrent")
	for _, r := range rows {
		fmt.Fprintf(w, "%s\t%s\t%s\t%s\n", r.name, r.n, r.v1, r.cur)
	}
	w.Flush()
	return b.String()
}

func mapsKeys(m map[string]int) func(func(string) bool) {
	return func(yield func(string) bool) {
		for k := range m {
			if !yield(k) {
				return
			}
		}
	}
}

// checkRates returns an error that shows both tables when got is not want.
func checkRates(got, want string) error {
	if got == want {
		return nil
	}
	return fmt.Errorf("the rates differ from %s; if the change is meant, rewrite it with -%s and commit it.\ncommitted:\n%s\nmeasured:\n%s", ratesFile, rewriteRatesFlag, want, got)
}

// TestRates measures both engines and compares the figures with
// ratesFile, or rewrites it when the flag its header names is set. Under the
// race detector it draws a twentieth of the generated cases and compares
// nothing: that run looks for data races.
func TestRates(t *testing.T) {
	isolate(t)
	m := measure(t)
	got := ratesTable(m, m.cases)
	if raceOn {
		t.Logf("under the race detector the rates are not compared:\n%s", got)
		return
	}
	if *rewriteRates {
		if err := os.WriteFile(filepath.FromSlash(ratesFile), []byte(got), 0o644); err != nil {
			t.Fatal(err)
		}
		t.Logf("rewrote %s:\n%s", ratesFile, got)
		return
	}
	want, err := os.ReadFile(filepath.FromSlash(ratesFile))
	if err != nil {
		t.Fatal(err)
	}
	if err := checkRates(got, string(want)); err != nil {
		t.Fatal(err)
	}
	t.Log(got)
}

// The rates see a change of one label: relabelling one case changes the
// table, so TestRates fails on it.
func TestRatesSeeALabelChange(t *testing.T) {
	isolate(t)
	m := measure(t)
	committed := ratesTable(m, m.cases)
	if !raceOn {
		data, err := os.ReadFile(filepath.FromSlash(ratesFile))
		if err != nil {
			t.Fatal(err)
		}
		committed = string(data)
	}
	first := func(label string) int {
		for i, c := range m.cases[:m.corpusCount] {
			if c.label == label {
				return i
			}
		}
		t.Fatalf("no %s case", label)
		return 0
	}
	for name, change := range map[string]func(c *corpusCase){
		"a secret relabelled negative": func(c *corpusCase) { c.label, c.secrets = labelNegative, nil },
		"a negative in another class":  func(c *corpusCase) { c.class = "docs" },
		"a secret in another class":    func(c *corpusCase) { c.class = "audit_kept" },
	} {
		t.Run(name, func(t *testing.T) {
			cases := slices.Clone(m.cases)
			i := first(labelSecret)
			if strings.Contains(name, "negative in") {
				i = first(labelNegative)
			}
			before := cases[i]
			change(&cases[i])
			if cases[i].label == before.label && cases[i].class == before.class {
				t.Fatalf("the change left %s as it was", before)
			}
			if err := checkRates(ratesTable(m, cases), committed); err == nil {
				t.Fatalf("the rates did not change when %s did: %s", name, before)
			}
		})
	}
}

// The spans and precision count every case, the generated ones too, not
// only the corpus files' cases.
func TestRatesCountTheSpansOfEveryCase(t *testing.T) {
	isolate(t)
	m := measure(t)
	if m.generated == 0 {
		t.Fatal("no generated case")
	}
	for name, run := range map[string]func(i int) engineRun{
		"v1":      func(i int) engineRun { return m.v1[i] },
		"current": func(i int) engineRun { return m.alone[i] },
	} {
		all := figures(m, m.cases, run)
		corpus := figures(m, m.cases[:m.corpusCount], run)
		want, onSecret := 0, 0
		for i, c := range m.cases {
			for _, sp := range run(i).spans {
				want++
				for _, s := range c.secrets {
					if sp.start < s.end && s.start < sp.end {
						onSecret++
						break
					}
				}
			}
		}
		if all.spans != want || all.onSecret != onSecret || all.spans <= corpus.spans || all.onSecret <= corpus.onSecret {
			t.Errorf("%s: %d spans, %d on a secret, want %d and %d; the corpus files' cases alone have %d and %d", name, all.spans, all.onSecret, want, onSecret, corpus.spans, corpus.onSecret)
		}
	}
}
