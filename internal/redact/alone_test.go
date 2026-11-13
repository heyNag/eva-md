package redact

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
)

// Tests for the current rules alone, which Redact and Scan run when the
// baseline rules do not run at run time (SetBaselineAtRunTime(false)).

// currentRulesOnly turns the baseline rules off at run time for the test,
// and on again after it.
func currentRulesOnly(t *testing.T) {
	t.Helper()
	if !BaselineAtRunTime() {
		t.Fatal("the baseline rules were off at run time before the test")
	}
	SetBaselineAtRunTime(false)
	t.Cleanup(func() { SetBaselineAtRunTime(true) })
}

// countBaseline counts the calls of the baseline rules the driver makes
// from here to the end of the test.
func countBaseline(t *testing.T) *atomic.Int64 {
	t.Helper()
	var calls atomic.Int64
	run := baselineRedact
	baselineRedact = func(text string) (string, int) {
		calls.Add(1)
		return run(text)
	}
	t.Cleanup(func() { baselineRedact = run })
	return &calls
}

// The baseline rules run at run time until a caller turns them off, and the
// switch reads back what was set.
func TestBaselineAtRunTimeByDefault(t *testing.T) {
	isolate(t)
	var zero atomic.Bool
	if !BaselineAtRunTime() || zero.Load() != baselineOff.Load() {
		t.Fatal("the baseline rules are off at run time by default")
	}
	SetBaselineAtRunTime(false)
	off := BaselineAtRunTime()
	SetBaselineAtRunTime(true)
	if off || !BaselineAtRunTime() {
		t.Fatalf("set off, read %v; set on, read %v", off, BaselineAtRunTime())
	}
}

// With the baseline rules off, Redact runs the current rules alone on 20,000
// generated texts, every 250th a long one (under the race detector, 1,000):
// Apply(Scan(x)) is Redact(x), with the same count; redacting the result
// again changes nothing; every finding is the current rules', and none is
// FloorOnly; and the baseline rules are never called.
func TestCurrentRulesAloneOnGeneratedTexts(t *testing.T) {
	isolate(t)
	currentRulesOnly(t)
	calls := countBaseline(t)
	total := 20000
	if raceOn {
		total = 1000
	}
	texts := make(chan string, 256)
	go func() {
		defer close(texts)
		g := &textSource{seed: 20261111}
		for i := 0; i < total; i++ {
			if i%250 == 0 {
				texts <- g.text(20)
			} else {
				texts <- g.text(2)
			}
		}
	}()
	var mu sync.Mutex
	done, changed, failures := 0, 0, 0
	var wg sync.WaitGroup
	for w := 0; w < runtime.GOMAXPROCS(0); w++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for in := range texts {
				out, problem := aloneProblem(in)
				mu.Lock()
				done++
				if out != clean(in) {
					changed++
				}
				if problem != "" && failures < 5 {
					failures++
					t.Error(problem)
				}
				mu.Unlock()
			}
		}()
	}
	wg.Wait()
	if done != total {
		t.Fatalf("checked %d texts, want %d", done, total)
	}
	if changed < total/4 {
		t.Fatalf("only %d of %d texts changed: the generated texts hold too few secrets to judge the rules by", changed, total)
	}
	if n := calls.Load(); n != 0 {
		t.Fatalf("the baseline rules ran %d times", n)
	}
}

// aloneProblem checks the current rules alone on in, as
// TestCurrentRulesAloneOnGeneratedTexts says. It returns Redact's output and
// what is wrong, or "".
func aloneProblem(in string) (out, problem string) {
	out, n := Redact(in)
	r := Scan(context.Background(), in)
	if Apply(r) != out || r.count != n || r.Through != len(r.Clean) {
		return out, fmt.Sprintf("Scan(%q) gives %q, %d; Redact %q, %d", in, Apply(r), r.count, out, n)
	}
	if bad := badFindings(r); bad != "" {
		return out, fmt.Sprintf("Scan(%q): %s", in, bad)
	}
	for _, f := range r.Findings {
		if f.Rule != RuleCurrent || f.Tier == FloorOnly {
			return out, fmt.Sprintf("Scan(%q) found %+v, which is not the current rules'", in, f)
		}
	}
	if twice, k := Redact(out); twice != out || k != 0 {
		return out, fmt.Sprintf("not idempotent on %q: %q then %q, %d", in, out, twice, k)
	}
	return out, ""
}

// The baseline rules are called zero times while the whole corpus, its raw
// files and its cases, is redacted and scanned with them off; with them on,
// the count sees their calls.
func TestBaselineNeverRunsOnTheCorpus(t *testing.T) {
	isolate(t)
	t.Cleanup(func() { SetBaselineAtRunTime(true) })
	calls := countBaseline(t)
	cases := loadCorpus(t)
	files, err := filepath.Glob(filepath.Join(corpusDir, "*.txt"))
	if err != nil {
		t.Fatal(err)
	}
	var texts []string
	for _, f := range files {
		data, err := os.ReadFile(f)
		if err != nil {
			t.Fatal(err)
		}
		texts = append(texts, string(data))
	}
	for _, c := range cases {
		texts = append(texts, c.text)
	}
	SetBaselineAtRunTime(false)
	for _, text := range texts {
		Redact(text)
		Scan(context.Background(), text)
	}
	SetBaselineAtRunTime(true)
	if n := calls.Load(); n != 0 {
		t.Fatalf("the baseline rules ran %d times on the corpus with them off", n)
	}
	Redact(texts[0])
	if calls.Load() == 0 {
		t.Fatal("the count saw no call of the baseline rules with them on")
	}
}

// The current rules find nothing they are certain of in the raw corpus
// files, so no file holds a whole secret in a format they know.
func TestRawCorpusHoldsNoCertainSecret(t *testing.T) {
	isolate(t)
	files, err := filepath.Glob(filepath.Join(corpusDir, "*"))
	if err != nil || len(files) < 6 {
		t.Fatalf("corpus files %v: %v", files, err)
	}
	for _, f := range files {
		data, err := os.ReadFile(f)
		if err != nil {
			t.Fatal(err)
		}
		r := driver{ctx: context.Background(), alone: true}.scan(string(data))
		for _, fd := range r.Findings {
			if fd.Tier == Certain {
				t.Errorf("%s: the current rules are certain of %s at %q", f, fd.Kind, r.Clean[fd.Start:fd.End])
			}
		}
	}
	// And they would be, had a file held one whole.
	r := driver{ctx: context.Background(), alone: true}.scan("Authorization: Bearer " + random)
	if len(r.Findings) != 1 || r.Findings[0].Tier != Certain {
		t.Fatalf("a whole Authorization value found %+v", r.Findings)
	}
}

// A scan of the current rules alone stopped at any point it checks its
// context returns there and keeps nothing.
func TestStoppedAloneScanKeepsNothing(t *testing.T) {
	isolate(t)
	g := &textSource{seed: 11}
	texts := 100
	if raceOn {
		texts = 10
	}
	for i := 0; i < texts; i++ {
		in := g.text(30) + "\n" + pemKey + "\ntoken=" + j("Ab3d", "Ef6hIj9l")
		points := 0
		full := driver{ctx: context.Background(), alone: true, step: func(int) { points++ }}.scan(in)
		if points < 3 || len(full.Findings) == 0 {
			t.Fatalf("a scan of %q checked its context %d times and found %d", in, points, len(full.Findings))
		}
		for k := 0; k < points; k++ {
			ctx, cancel := context.WithCancel(context.Background())
			checks := 0
			r := driver{ctx: ctx, alone: true, step: func(int) {
				checks++
				if checks == k+1 {
					cancel()
				}
			}}.scan(in)
			cancel()
			if checks != k+1 {
				t.Fatalf("the scan went on past the point it was stopped at: %d checks, want %d", checks, k+1)
			}
			checkKeepsNothing(t, in, full, r)
		}
	}
}

// Redact tells the test's oracle, which compares with v1, only of texts it
// redacts with the baseline rules.
func TestRedactedHookOnlyWithBaseline(t *testing.T) {
	isolate(t)
	t.Cleanup(func() { SetBaselineAtRunTime(true) })
	in := "token=" + j("Ab3d", "Ef6hIj9l")
	before := oracleCalls()
	SetBaselineAtRunTime(false)
	Redact(in)
	SetBaselineAtRunTime(true)
	if oracleCalls() != before {
		t.Fatal("Redact told the oracle of a text it redacted with the current rules alone")
	}
	Redact(in)
	if oracleCalls() != before+1 {
		t.Fatal("Redact did not tell the oracle of a text it redacted with the baseline rules")
	}
}

// The engines differ on code the baseline rules take by mistake: v1's
// engine removes a range of it, the current rules alone keep it.
func TestEnginesDifferOnCode(t *testing.T) {
	isolate(t)
	in := "\tcfgSecret := store.readSecret[LevelHandshake]"
	v1, n1 := Redact(in)
	currentRulesOnly(t)
	alone, n := Redact(in)
	if v1 != "\tcfgSecret := [REDACTED:secret_value][LevelHandshake]" || n1 != 1 || alone != in || n != 0 {
		t.Fatalf("v1 %q, %d; alone %q, %d", v1, n1, alone, n)
	}
}

// The switch may be turned while texts are redacted: each text is redacted
// one way or the other, whole, and the oracle TestMain sets, which checks
// each text redacted with the baseline rules against v1, finds nothing
// wrong.
func TestSwitchTurnsWhileRedacting(t *testing.T) {
	isolate(t)
	t.Cleanup(func() { SetBaselineAtRunTime(true) })
	in := "\tcfgSecret := store.readSecret[LevelHandshake] and DB_PASSWORD=" + j("Tr0ub4", "dor3x")
	SetBaselineAtRunTime(true)
	v1, _ := Redact(in)
	SetBaselineAtRunTime(false)
	alone, _ := Redact(in)
	SetBaselineAtRunTime(true)
	if v1 == alone {
		t.Fatalf("the engines agree on %q", in)
	}
	rounds := 400
	if raceOn {
		rounds = 40
	}
	var wg sync.WaitGroup
	var bad atomic.Value
	for w := 0; w < 4; w++ {
		wg.Add(1)
		go func(w int) {
			defer wg.Done()
			for i := 0; i < rounds; i++ {
				if w == 0 {
					SetBaselineAtRunTime(i%2 == 0)
					continue
				}
				_ = BaselineAtRunTime()
				if out, _ := Redact(in); out != v1 && out != alone {
					bad.Store(out)
				}
			}
		}(w)
	}
	wg.Wait()
	if out := bad.Load(); out != nil {
		t.Fatalf("Redact(%q) = %q, neither engine's output", in, out)
	}
	if !strings.Contains(alone, "[REDACTED:secret_value]") {
		t.Fatalf("the current rules alone kept the password: %q", alone)
	}
}
