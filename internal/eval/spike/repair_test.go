package spike

import (
	"bytes"
	"reflect"
	"slices"
	"strings"
	"testing"
)

// parseRepair reads a repair answer that must be valid.
func parseRepair(t *testing.T, data []byte, f Form) Repair {
	t.Helper()
	r, err := ParseRepair(data, f)
	if err != nil {
		t.Fatalf("%s: %v", f, err)
	}
	return r
}

// TestRepairInputLayout: the repair input is the first call's input byte
// for byte, the boundary line, the model's own answer, the head line and
// one line for each operation refused with a repairable code, in order.
func TestRepairInputLayout(t *testing.T) {
	for _, f := range Forms {
		in, v := checkFaults(t, f)
		answer := canned(t, f, "assent.01.1.faults")
		first := append([]byte(Prompt(f)), in.Bytes...)
		got := RepairInput(first, answer, v)
		if !bytes.HasPrefix(got, first) {
			t.Fatalf("%s: the repair input does not start with the first input", f)
		}
		place := "ev.1.q"
		if f == FormSegments {
			place = "ev.1.s"
		}
		want := string(first) + RepairBoundary + "\n" + strings.TrimRight(string(answer), "\n") + "\n" + RepairHead + "\n" +
			"#2 bad_shape kind\n#3 unknown_target id\n#4 bad_entry ev.1.e\n#5 " + faultCodes[f][4] + " " + place + "\n#6 ungrounded text\n"
		if string(got) != want {
			t.Errorf("%s: the repair input ends\n%s\nwant\n%s", f, got[len(first):], want[len(first):])
		}
		if strings.Contains(string(got[len(first):]), "tag_too_low") {
			t.Errorf("%s: tag_too_low is offered for repair", f)
		}
	}
	// A first input without a final line break gets one after it; an
	// empty one gets none.
	v := Verdict{Ops: []Judged{{Code: "ungrounded", Place: "text"}}}
	if got := string(RepairInput([]byte("data"), []byte("{}\n\n"), v)); got != "data\n"+RepairBoundary+"\n{}\n"+RepairHead+"\n#1 ungrounded text\n" {
		t.Errorf("the repair input is %q", got)
	}
	if got := string(RepairInput(nil, []byte("{}"), v)); !strings.HasPrefix(got, RepairBoundary) {
		t.Errorf("the repair input is %q", got)
	}
	// No line of a valid answer, however laid out, starts as a refusal
	// line does.
	for _, f := range Forms {
		for _, line := range strings.Split(string(canned(t, f, "assent.01.1.faults")), "\n") {
			if strings.HasPrefix(line, "#") {
				t.Errorf("%s: a line of an answer starts with #", f)
			}
		}
	}
}

// TestRepairIsOfferedExactlyForTheRepairableCodes: an operation is sent to
// repair if and only if its code is repairable, tag_too_low never.
func TestRepairIsOfferedExactlyForTheRepairableCodes(t *testing.T) {
	var v Verdict
	var want []int
	for i, c := range Codes {
		v.Ops = append(v.Ops, Judged{Code: c.ID, Place: "text"}, Judged{})
		if c.Repairable {
			want = append(want, 2*i)
		}
		if c.ID == "tag_too_low" && c.Repairable {
			t.Error("tag_too_low is repairable")
		}
	}
	if got := v.ToRepair(); !slices.Equal(got, want) {
		t.Errorf("sent %v, want %v", got, want)
	}
	lines := strings.Count(string(RepairInput(nil, nil, v)), "\n#")
	if lines != len(want) {
		t.Errorf("%d refusal lines, want %d", lines, len(want))
	}
	if !slices.ContainsFunc(Codes, func(c Code) bool { return c.ID == "tag_too_low" }) {
		t.Error("no tag_too_low in Codes")
	}
	if got := (Verdict{Ops: []Judged{{Code: "ungrounded", Sent: true}}}).ToRepair(); len(got) != 0 {
		t.Errorf("an operation already sent is sent again: %v", got)
	}
}

// TestRepairOfTheFaults: the canned repair fixes what it replaces, a
// second refusal and a skip are final, a fix for an operation not sent is
// ignored, and no operation it does not replace changes.
func TestRepairOfTheFaults(t *testing.T) {
	for _, f := range Forms {
		in, first := checkFaults(t, f)
		r := parseRepair(t, canned(t, f, "assent.01.1.faults.repair"), f)
		final := ApplyRepair(in, nil, first, r, f)
		if got, want := codes(final), []string{"", "", "unknown_target", "bad_entry", "", "ungrounded", "tag_too_low", ""}; !slices.Equal(got, want) {
			t.Errorf("%s: final codes %q, want %q", f, got, want)
		}
		for i, j := range final.Ops {
			sent := i >= 1 && i <= 5
			repaired := i == 1 || i == 3 || i == 4
			if j.Sent != sent || j.Repaired != repaired {
				t.Errorf("%s: operation %d sent %v repaired %v", f, i+1, j.Sent, j.Repaired)
			}
			if !sent {
				if !reflect.DeepEqual(j, first.Ops[i]) {
					t.Errorf("%s: operation %d, not sent, changed", f, i+1)
				}
			} else if !repaired {
				k := first.Ops[i]
				k.Sent = true
				if !reflect.DeepEqual(j, k) {
					t.Errorf("%s: operation %d, not replaced, changed", f, i+1)
				}
			}
		}
		if final.Ops[1].Op.Kind == nil || *final.Ops[1].Op.Kind != "found" || final.Ops[1].Tag != TagStated {
			t.Errorf("%s: the repaired note is %+v", f, final.Ops[1])
		}
		if !final.RepairBreaks || first.RepairBreaks || final.Breaks {
			t.Errorf("%s: the repair's fix for #7 breaks the schema: %v", f, final.RepairBreaks)
		}
		if len(final.ToRepair()) != 0 {
			t.Errorf("%s: a second refusal is sent again", f)
		}
		if again := ApplyRepair(in, nil, final, r, f); !reflect.DeepEqual(again, final) {
			t.Errorf("%s: a second round changed the verdict", f)
		}
		if !reflect.DeepEqual(first, Check(in, nil, parse(t, canned(t, f, "assent.01.1.faults"), f), f)) {
			t.Errorf("%s: ApplyRepair changed the verdict it was given", f)
		}
	}
}

// TestRepairFixesAChangedNumber: a number changed from what the user said
// is ungrounded, and a repair that restores it is accepted.
func TestRepairFixesAChangedNumber(t *testing.T) {
	s := scriptByID(t, "v1.05")
	for _, f := range Forms {
		in := renderPass(t, s, 1, nil, f)
		v := Check(in, nil, parse(t, canned(t, f, "v1.05.1.ungrounded"), f), f)
		if got := codes(v); !slices.Equal(got, []string{"ungrounded"}) || v.Ops[0].Place != "text" {
			t.Fatalf("%s: %q", f, got)
		}
		final := ApplyRepair(in, nil, v, parseRepair(t, canned(t, f, "v1.05.1.ungrounded.repair"), f), f)
		if j := final.Ops[0]; !j.Accepted() || !j.Repaired || j.Tag != TagStated || !strings.Contains(*j.Op.Text, "300 ms") {
			t.Errorf("%s: the repaired operation is %+v", f, j)
		}
		var st Store
		if err := st.Apply(passSession(s, 1), final); err != nil || len(st.Items()) != 1 {
			t.Errorf("%s: the store holds %v, %v", f, st.Items(), err)
		}
	}
}

// TestRepairChangesNoMoreThanItMay: a fix for an operation not sent, for
// an index two fixes name, or for an index out of range changes nothing;
// a replacement may not take a handle an accepted operation holds; an
// empty repair, as after a failed call or an invalid answer, only marks
// what was sent.
func TestRepairChangesNoMoreThanItMay(t *testing.T) {
	in, err := Render(madeUp(), nil, FormQuotes)
	if err != nil {
		t.Fatal(err)
	}
	shown := []Shown{{Handle: "h1", Kind: "decision", Tag: TagStated, Text: "The relay is small."}}
	said := func(e int64) Span { return sq(e, in.Entries[e-1].Text, "said") }
	retire := func(e int64) Op {
		return Op{Op: "item.retire", ID: ptr("h1"), Text: ptr("It is gone."), Ev: []Span{said(e)}}
	}
	note := func(e int64) Op { return Op{Op: "note", Kind: ptr("found"), Text: ptr("Noted."), Ev: []Span{said(e)}} }
	bad := Op{Op: "note", Kind: ptr("found"), Text: ptr("Noted."), Ev: []Span{sq(1, "not in the entry", "said")}}
	a := Answer{Ops: []Op{retire(1), bad, bad, bad, bad}}
	first := Check(in, shown, a, FormQuotes)
	if got := codes(first); !slices.Equal(got, []string{"", "quote_not_found", "quote_not_found", "quote_not_found", "quote_not_found"}) {
		t.Fatalf("first codes %q", got)
	}
	r := Repair{Fixes: []Fix{
		{Index: 1, Op: ptrOp(note(1))}, // not sent
		{Index: 2, Op: ptrOp(retire(1))},
		{Index: 3, Op: ptrOp(note(1))}, {Index: 3, Op: ptrOp(note(2))},
		{Index: 4, Op: ptrOp(note(3))},
		{Index: 0, Op: ptrOp(note(1))}, {Index: -4, Op: ptrOp(note(1))}, {Index: 6, Op: ptrOp(note(1))}, {Index: 1 << 40, Op: ptrOp(note(1))},
	}}
	final := ApplyRepair(in, shown, first, r, FormQuotes)
	if got := codes(final); !slices.Equal(got, []string{"", "unknown_target", "quote_not_found", "", "quote_not_found"}) {
		t.Errorf("final codes %q", got)
	}
	if !reflect.DeepEqual(final.Ops[0], first.Ops[0]) {
		t.Error("a fix for an operation not sent changed it")
	}
	if final.RepairBreaks {
		t.Error("a repair within the schema breaks it")
	}
	empty := ApplyRepair(in, shown, first, Repair{}, FormQuotes)
	if got := codes(empty); !slices.Equal(got, codes(first)) || !empty.Ops[1].Sent || empty.Ops[0].Sent || empty.Ops[1].Repaired {
		t.Errorf("an empty repair: %q", got)
	}
	// Over the bound, a repair answer breaks its schema.
	over := Repair{Fixes: slices.Repeat([]Fix{{Index: 9}}, MaxOps+1)}
	if !ApplyRepair(in, shown, first, over, FormQuotes).RepairBreaks {
		t.Error("a repair over the bound does not break the schema")
	}
	// Two replacements naming the same handle: the first in index order
	// takes it.
	two := Check(in, shown, Answer{Ops: []Op{bad, bad}}, FormQuotes)
	got := ApplyRepair(in, shown, two, Repair{Fixes: []Fix{{Index: 2, Op: ptrOp(retire(1))}, {Index: 1, Op: ptrOp(retire(1))}}}, FormQuotes)
	if c := codes(got); !slices.Equal(c, []string{"", "unknown_target"}) {
		t.Errorf("two replacements on one handle: %q", c)
	}
}

func ptrOp(op Op) *Op { return &op }
