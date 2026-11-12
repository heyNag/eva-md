package score

import (
	"slices"
	"testing"

	"github.com/heyNag/eva-md/internal/eval/scenario"
)

// The normalizers read a day in any of its written forms, and no day
// that does not exist.
func TestDates(t *testing.T) {
	for in, want := range map[string][]string{
		"The freeze is on 3 March 2026.":  {"2026-03-03"},
		"on March 3, 2026":                {"2026-03-03"},
		"on 3rd Mar 2026 and 2026-03-10":  {"2026-03-10", "2026-03-03"},
		"Cut it yesterday, ship tomorrow": {"yesterday", "tomorrow"},
		"on 31 February 2026":             nil,
		"2026-13-01 and 2026-02-30":       nil,
		"KITE-5 and 1.26":                 nil,
	} {
		if got := dates(in); !slices.Equal(got, want) {
			t.Errorf("dates(%q) = %q, want %q", in, got, want)
		}
	}
}

// Quantities are read by number and unit, in a unit's base, so 5 min and
// 300 s are one; a number that ends a word, as a codename's, is not one.
func TestQuantities(t *testing.T) {
	five := quantity{"time", 300}
	for in, want := range map[string][]quantity{
		"Cache entries for 5 minutes":  {five},
		"5 min, or 300 s, or 300000ms": {five, five, five},
		"costs 3 s at startup":         {{"time", 3}},
		"KITE-5 s and v2.3 s":          nil,
		"3 March 2026":                 nil,
		"under 2 MiB, 50%":             {{"size", 2 << 20}, {"ratio", 0.5}},
	} {
		got := quantities(in)
		if len(got) != len(want) || !slices.EqualFunc(got, want, quantity.equal) {
			t.Errorf("quantities(%q) = %v, want %v", in, got, want)
		}
	}
}

// A word is read as a name in code, a path or a version by its shape.
func TestShapes(t *testing.T) {
	for w, want := range map[string]bool{"KITE-5": true, "EVA_TOKEN": true, "YAML": true, "SQLite": true, "BenchmarkBrief": true,
		"flock": false, "Store": false, "Go": false, "per-session": false, "2026-03-03": false, "I": false} {
		if identLike(w) != want {
			t.Errorf("identLike(%q) = %v", w, !want)
		}
	}
	for w, want := range map[string]bool{"scripts/benchmark.py": true, "lock.go": true, "internal/session": true, "1.26": false, "e.g": false, "a/1": true, "1/2": false} {
		if pathLike(w) != want {
			t.Errorf("pathLike(%q) = %v", w, !want)
		}
	}
}

// A slot is held by its value or a form, as a run of whole words in any
// case; a condition by every word that matters in it, and its quantities
// and dates in any form, after a marker and within the clause it opens.
func TestHolds(t *testing.T) {
	cond := scenario.Slot{Class: scenario.SlotCond, Value: "If BenchmarkBrief in scripts/benchmark.py passes 300 ms"}
	for _, tc := range []struct {
		text string
		slot scenario.Slot
		want bool
	}{
		{"Use flock for the lock", scenario.Slot{Class: scenario.SlotIdent, Value: "flock"}, true},
		{"Use flocks for the lock", scenario.Slot{Class: scenario.SlotIdent, Value: "flock"}, false},
		{"Use FLOCK, KESTREL-41.", scenario.Slot{Class: scenario.SlotIdent, Value: "kestrel-41"}, true},
		{"Use Go 1.26", scenario.Slot{Class: scenario.SlotVersion, Value: "Go 1.26", Forms: []string{"1.26"}}, true},
		{"Use version 1.26", scenario.Slot{Class: scenario.SlotVersion, Value: "Go 1.26", Forms: []string{"1.26"}}, true},
		{"Use Go 1.2", scenario.Slot{Class: scenario.SlotVersion, Value: "Go 1.26"}, false},
		{"Freeze on 2026-03-03", scenario.Slot{Class: scenario.SlotDate, Value: "3 March 2026"}, true},
		{"Freeze on 2026-03-04", scenario.Slot{Class: scenario.SlotDate, Value: "3 March 2026"}, false},
		{"Cache for 300 s", scenario.Slot{Class: scenario.SlotQuantity, Value: "5 minutes"}, true},
		{"switch when BenchmarkBrief in scripts/benchmark.py passes 300 ms", cond, true},
		{"switch: BenchmarkBrief in scripts/benchmark.py passes 300 ms", cond, false},
		{"if scripts/benchmark.py passes 300 ms", cond, false},
		{"If BenchmarkBrief in scripts/benchmark.py passes 300ms, switch", cond, true},
		{"If BenchmarkBrief in scripts/benchmark.py passes 0.3 s, switch", cond, true},
		{"If BenchmarkBrief in scripts/benchmark.py passes 400 ms, switch", cond, false},
		{"If the build fails, switch to the streaming parser; BenchmarkBrief in scripts/benchmark.py passes 300 ms", cond, false},
		{"Switch to the streaming parser (BenchmarkBrief in scripts/benchmark.py passes 300 ms if you ask me)", cond, false},
		{"BenchmarkBrief in scripts/benchmark.py passes 300 ms. If so, switch", cond, false},
		{"When the freeze ends on 2026-03-03, ship", scenario.Slot{Class: scenario.SlotCond, Value: "once the freeze ends on 3 March 2026"}, true},
		{"When the freeze ends on 2026-03-04, ship", scenario.Slot{Class: scenario.SlotCond, Value: "once the freeze ends on 3 March 2026"}, false},
		{"Ship when the tests pass, the docs build", scenario.Slot{Class: scenario.SlotCond, Value: "when the tests pass, the docs build"}, true},
		{"Ship when the freeze ends on March 3, 2026, not before", scenario.Slot{Class: scenario.SlotCond, Value: "once the freeze ends on 3 March 2026"}, true},
		{"Ship when the tests pass, then tag it. The docs build", scenario.Slot{Class: scenario.SlotCond, Value: "when the tests pass, the docs build"}, false},
		{"Keep it for now", scenario.Slot{Class: scenario.SlotLimit, Value: "for now"}, true},
		{"Keep it now", scenario.Slot{Class: scenario.SlotLimit, Value: "for now"}, false},
	} {
		if got := readText(tc.text).holds(tc.slot); got != tc.want {
			t.Errorf("%q holds %+v: %v", tc.text, tc.slot, got)
		}
	}
}

// A slot is wrong when the text holds another value of its class that no
// slot of the fact holds, and only then.
func TestWrong(t *testing.T) {
	f := scenario.Fact{Slots: []scenario.Slot{{Class: scenario.SlotIdent, Value: "WREN-5"}, {Class: scenario.SlotIdent, Value: "EVA_MD_TOKEN"},
		{Class: scenario.SlotQuantity, Value: "5 minutes"}, {Class: scenario.SlotPath, Value: "scripts/benchmark.py"},
		{Class: scenario.SlotDate, Value: "2026-03-03"}, {Class: scenario.SlotVersion, Value: "1.26"}, {Class: scenario.SlotInt, Value: "200"}}}
	for _, tc := range []struct {
		text string
		slot int
		want bool
	}{
		{"The token is EVA_TOKEN, WREN-5", 1, true},
		{"The token is the old one, WREN-5", 1, false},
		{"for 7 minutes", 2, true},
		{"for 7 KiB", 2, false},
		{"in scripts/bench.py", 3, true},
		{"on 2026-03-10", 4, true},
		{"on the third", 4, false},
		{"Go 1.27", 5, true},
		{"under 300 items", 6, true},
		{"under 5 minutes and 200 items", 6, false},
	} {
		if got := readText(tc.text).wrong(f.Slots[tc.slot], f); got != tc.want {
			t.Errorf("%q: slot %d wrong: %v", tc.text, tc.slot, got)
		}
	}
}
