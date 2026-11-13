package spike

import (
	"bytes"
	"fmt"
	"slices"
)

// The repair input's two fixed lines: the boundary after the first call's
// input, and the head of the list of refusals after the model's answer.
const (
	RepairBoundary = "The data ends here. Your answer to it was:"
	RepairHead     = "These operations of your answer were refused for a reason that may be repaired, one a line as #n code place: n counts your operations from 1, and place is where the fault lies, ev.k being the operation's k-th span, counted from 1."
)

// repairable reports a refusal code one repair round may fix.
func repairable(code string) bool {
	return slices.ContainsFunc(Codes, func(c Code) bool { return c.ID == code && c.Repairable })
}

// unfixable are the places of a bad_shape that no replacement can mend,
// since a replacement is judged at the same index in the same form: an
// operation past MaxOps, and an answer in a form the contract lacks.
var unfixable = []string{"ops", "form"}

// ToRepair lists the indexes, from 0, of the operations of v refused with
// a code one repair round may fix, at a place a replacement can mend, and
// not yet sent to one: the operations a repair call is made for. An
// answer over MaxOps operations with nothing else wrong makes no repair
// call.
func (v Verdict) ToRepair() []int {
	var out []int
	for i, j := range v.Ops {
		if repairable(j.Code) && !slices.Contains(unfixable, j.Place) && !j.Sent {
			out = append(out, i)
		}
	}
	return out
}

// RepairInput is the repair call's input: first, the first call's input,
// byte for byte; a line break if first does not end with one; the line
// RepairBoundary; the model's own answer as it gave it, less white space at
// its end; the line RepairHead; then "#<n> <code> <place>" for each
// operation of v that ToRepair lists, n its index counted from 1. A valid
// answer is JSON, no line of which can start with "#", so no line of the
// answer can pass for a refusal.
func RepairInput(first, answer []byte, v Verdict) []byte {
	var b bytes.Buffer
	b.Write(first)
	if len(first) > 0 && first[len(first)-1] != '\n' {
		b.WriteByte('\n')
	}
	b.WriteString(RepairBoundary + "\n")
	b.Write(bytes.TrimRight(answer, " \t\r\n"))
	b.WriteString("\n" + RepairHead + "\n")
	for _, i := range v.ToRepair() {
		fmt.Fprintf(&b, "#%d %s %s\n", i+1, v.Ops[i].Code, v.Ops[i].Place)
	}
	return b.Bytes()
}

// ApplyRepair applies the repair answer r to v, the first check of the
// pass rendered as in with the items shown, in form f. Each operation
// ToRepair lists is marked Sent. A fix whose index, counted from 1, names
// one of them and no other fix names replaces it: the replacement is
// judged as Check judges an operation at that index, with the handles of
// the operations v accepted, and of the replacements accepted before it,
// taken. A skip, a second refusal, an index not sent and an index two
// fixes name are final: the operation keeps its first refusal, or takes
// the replacement's. No other operation changes, and a verdict already
// repaired is not repaired again.
func ApplyRepair(in Input, shown []Shown, v Verdict, r Repair, f Form) Verdict {
	out := v
	out.Ops = slices.Clone(v.Ops)
	sent := v.ToRepair()
	if len(sent) == 0 || slices.ContainsFunc(v.Ops, func(j Judged) bool { return j.Sent }) {
		return out
	}
	out.RepairBreaks = len(r.Fixes) > MaxOps
	named := map[int64]int{}
	fix := map[int64]*Op{}
	for _, fx := range r.Fixes {
		named[fx.Index]++
		fix[fx.Index] = fx.Op
		if fx.Op != nil && breaks(*fx.Op) {
			out.RepairBreaks = true
		}
	}
	claimed := map[string]bool{}
	for _, j := range v.Ops {
		if j.Accepted() && j.Op.ID != nil {
			claimed[*j.Op.ID] = true
		}
	}
	c := newChecker(in, shown, f)
	for _, i := range sent {
		out.Ops[i].Sent = true
		n := int64(i + 1)
		if named[n] != 1 || fix[n] == nil {
			continue
		}
		j := c.judge(i, *fix[n], claimed)
		j.Sent, j.Repaired = true, true
		out.Ops[i] = j
	}
	return out
}
