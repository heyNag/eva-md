package spike

import (
	"encoding/json"
	"fmt"
	"regexp"
	"slices"
	"sort"
	"strings"
	"testing"

	"github.com/heyNag/eva-md/internal/eval/scenario"
	"github.com/heyNag/eva-md/internal/host/adapter"
)

// TestCodesTable: the refusal codes are the card's seven, in its order,
// each with a sentence, and only tag_too_low is beyond repair.
func TestCodesTable(t *testing.T) {
	var ids []string
	for _, c := range Codes {
		ids = append(ids, c.ID)
		if strings.TrimSpace(c.Sentence) == "" || c.Repairable != (c.ID != "tag_too_low") {
			t.Errorf("code %s: sentence %q, repairable %v", c.ID, c.Sentence, c.Repairable)
		}
	}
	if want := []string{"bad_shape", "unknown_target", "bad_entry", "quote_not_found", "bad_segment", "ungrounded", "tag_too_low"}; !slices.Equal(ids, want) {
		t.Errorf("codes %v, want %v", ids, want)
	}
}

// strictProblems lists the ways a schema breaks the strict rules both
// hosts want: an object whose required keys are not exactly its
// properties, or that takes other keys, and any maxLength. It also
// returns every property name met.
func strictProblems(node any, at string, names map[string]bool) []string {
	var out []string
	switch n := node.(type) {
	case map[string]any:
		if _, ok := n["maxLength"]; ok {
			out = append(out, at+": maxLength")
		}
		if props, ok := n["properties"].(map[string]any); ok {
			var keys, required []string
			for k := range props {
				keys = append(keys, k)
				names[k] = true
			}
			for _, r := range n["required"].([]any) {
				required = append(required, r.(string))
			}
			sort.Strings(keys)
			sort.Strings(required)
			if !slices.Equal(keys, required) {
				out = append(out, fmt.Sprintf("%s: requires %v of %v", at, required, keys))
			}
			if n["additionalProperties"] != false {
				out = append(out, at+": takes other keys")
			}
		}
		for k, v := range n {
			out = append(out, strictProblems(v, at+"."+k, names)...)
		}
	case []any:
		for i, v := range n {
			out = append(out, strictProblems(v, fmt.Sprintf("%s[%d]", at, i), names)...)
		}
	}
	return out
}

func parseSchema(t *testing.T, data []byte) map[string]any {
	t.Helper()
	var v map[string]any
	if err := json.Unmarshal(data, &v); err != nil {
		t.Fatalf("the schema does not parse: %v", err)
	}
	return v
}

// TestSchemasAreStrict: both forms' schemas and repair schemas parse; each
// object requires all its keys and allows no other; none holds a
// maxLength; the quotes form has no s and the segment form no q.
func TestSchemasAreStrict(t *testing.T) {
	for _, f := range Forms {
		for name, data := range map[string][]byte{"schema": Schema(f), "repair schema": RepairSchema(f)} {
			names := map[string]bool{}
			for _, p := range strictProblems(parseSchema(t, data), "", names) {
				t.Errorf("%s %s: %s", f, name, p)
			}
			has, hasNot := "q", "s"
			if f == FormSegments {
				has, hasNot = "s", "q"
			}
			if !names[has] || names[hasNot] {
				t.Errorf("%s %s: has %s %v, has %s %v", f, name, has, names[has], hasNot, names[hasNot])
			}
		}
	}
	// The check finds each fault it looks for.
	bad := `{"type":"object","properties":{"a":{"type":"string","maxLength":5},"b":{"type":"object","properties":{"c":{"type":"string"}},"required":[]}},"required":["a"],"additionalProperties":false}`
	if got := strictProblems(parseSchema(t, []byte(bad)), "", map[string]bool{}); len(got) != 4 {
		t.Errorf("the strict check finds %q in a schema with four faults", got)
	}
}

// TestSchemaTakesTheTable: the schema's bounds and enums are the
// contract's table.
func TestSchemaTakesTheTable(t *testing.T) {
	enum := func(v any) []string {
		var out []string
		for _, e := range v.(map[string]any)["enum"].([]any) {
			if e != nil {
				out = append(out, e.(string))
			}
		}
		return out
	}
	for _, f := range Forms {
		s := parseSchema(t, Schema(f))["properties"].(map[string]any)
		ops := s["ops"].(map[string]any)
		op := ops["items"].(map[string]any)["properties"].(map[string]any)
		ev := op["ev"].(map[string]any)
		span := ev["items"].(map[string]any)["properties"].(map[string]any)
		for _, c := range []struct {
			name      string
			got, want []string
		}{
			{"op", enum(op["op"]), opNames},
			{"kind", enum(op["kind"]), append(slices.Clone(itemKinds), noteKinds...)},
			{"admit", enum(op["admit"]), admits},
			{"as", enum(span["as"]), roles},
			{"nothing", enum(s["nothing"]), nothings},
		} {
			if !slices.Equal(c.got, c.want) {
				t.Errorf("%s: %s takes %v, want %v", f, c.name, c.got, c.want)
			}
		}
		if ops["maxItems"] != float64(MaxOps) || ev["minItems"] != float64(MinSpans) || ev["maxItems"] != float64(MaxSpans) {
			t.Errorf("%s: bounds %v, %v and %v", f, ops["maxItems"], ev["minItems"], ev["maxItems"])
		}
		if len(itemKinds) != 5 || len(admits) != 7 || len(noteKinds) != 4 || len(opNames) != 8 {
			t.Error("the table's enums are not the card's")
		}
	}
}

// promptLine is the line of the prompt after the line that is label.
func promptLine(t *testing.T, prompt, label string) string {
	t.Helper()
	lines := strings.Split(prompt, "\n")
	i := slices.Index(lines, label)
	if i < 0 || i+1 >= len(lines) {
		t.Fatalf("the prompt has no line %q", label)
	}
	return lines[i+1]
}

var space = regexp.MustCompile(`\s+`)

// TestPromptExampleParses: each prompt's example answer, and its wrong
// operation, parse in that form and not in the other, and every span of
// the good answer stands in the example entries the prompt shows: a quote
// found word for word in its entry, a segment its entry has.
func TestPromptExampleParses(t *testing.T) {
	for _, f := range Forms {
		p := Prompt(f)
		good := promptLine(t, p, "A good answer:")
		wrong := `{"ops":[` + promptLine(t, p, "A wrong operation for the same entries:") + `],"nothing":null}`
		a, err := ParseAnswer([]byte(good), f)
		if err != nil {
			t.Fatalf("%s: the example answer: %v", f, err)
		}
		if _, err := ParseAnswer([]byte(wrong), f); err != nil {
			t.Errorf("%s: the wrong operation: %v", f, err)
		}
		other := FormQuotes
		if f == FormQuotes {
			other = FormSegments
		}
		if _, err := ParseAnswer([]byte(good), other); err == nil {
			t.Errorf("%s's example parses in %s", f, other)
		}
		in := exampleInput(f)
		if !strings.Contains(p, string(in.Bytes)) {
			t.Errorf("%s: the prompt does not show the example's entries", f)
		}
		if len(a.Ops) == 0 || a.Nothing != nil {
			t.Errorf("%s: the example writes nothing", f)
		}
		for i, op := range a.Ops {
			if !slices.Contains(opNames, op.Op) || len(op.Ev) < MinSpans || len(op.Ev) > MaxSpans {
				t.Errorf("%s: example op %d breaks the table", f, i)
			}
			for _, s := range op.Ev {
				if s.E < 1 || s.E > int64(len(in.Entries)) || !slices.Contains(roles, s.As) {
					t.Fatalf("%s: example op %d cites entry %d as %q", f, i, s.E, s.As)
				}
				e := in.Entries[s.E-1]
				switch {
				case f == FormSegments && (s.S < 1 || s.S > int64(len(e.Segments))):
					t.Errorf("segments: example op %d cites %d.%d, which is not shown", i, s.E, s.S)
				case f == FormQuotes && (!strings.Contains(space.ReplaceAllString(e.Text, " "), space.ReplaceAllString(s.Q, " ")) ||
					(len(s.Q) < MinQuote || len(s.Q) > MaxQuote) && s.Q != e.Text):
					t.Errorf("quotes: example op %d quotes %q, not found in entry %d", i, s.Q, s.E)
				}
			}
		}
	}
}

// missingCodes lists the codes whose id or sentence prompt does not hold.
func missingCodes(prompt string, codes []Code) []string {
	var out []string
	for _, c := range codes {
		if !strings.Contains(prompt, "- "+c.ID+": "+c.Sentence) {
			out = append(out, c.ID)
		}
	}
	return out
}

// TestPromptWritesEveryCode: every code's sentence is in each prompt, and a
// code with no sentence there fails the check.
func TestPromptWritesEveryCode(t *testing.T) {
	for _, f := range Forms {
		if m := missingCodes(Prompt(f), Codes); len(m) > 0 {
			t.Errorf("%s: the prompt writes no sentence for %v", f, m)
		}
		extra := append(slices.Clone(Codes), Code{ID: "read_only_target", Sentence: "Its target may not be changed."})
		if m := missingCodes(Prompt(f), extra); !slices.Equal(m, []string{"read_only_target"}) {
			t.Errorf("%s: the check misses a code with no sentence: %v", f, m)
		}
	}
}

// named finds, in text, any of words as a whole word in any case.
func named(text string, words []string) []string {
	var out []string
	for _, w := range words {
		if regexp.MustCompile(`(?i)(^|[^a-z0-9])` + regexp.QuoteMeta(w) + `($|[^a-z0-9])`).MatchString(text) {
			out = append(out, w)
		}
	}
	return out
}

// hostAndModelNames are every name adapter.All() gives a host, and the
// names of model families a prompt must not hold.
func hostAndModelNames() []string {
	var names []string
	for _, h := range adapter.All() {
		names = append(names, string(h.ID), h.Names.App, h.Names.CLI, h.Names.InstallName)
	}
	return append(names, "gpt", "haiku", "sonnet", "opus", "gemini", "llama", "mistral", "anthropic", "openai")
}

// TestPromptNamesNoHostOrModel: neither prompt nor schema holds a host's
// name or a model's, and the check finds one where it is.
func TestPromptNamesNoHostOrModel(t *testing.T) {
	names := hostAndModelNames()
	for _, f := range Forms {
		for _, text := range []string{Prompt(f), string(Schema(f)), string(RepairSchema(f))} {
			if got := named(text, names); len(got) > 0 {
				t.Errorf("%s: names %v", f, got)
			}
		}
	}
	for _, h := range adapter.All() {
		if got := named(Prompt(FormQuotes)+"\nAsk "+h.Names.App+" for it.", names); !slices.Contains(got, h.Names.App) {
			t.Errorf("the check misses %s", h.Names.App)
		}
	}
	if len(named("the corpus", []string{"opus"})) != 0 || len(named("an opus.", []string{"opus"})) != 1 {
		t.Error("the check misses a whole word, or finds part of one")
	}
}

// TestPromptOrder: the prompt says, in derive 5.1's order, who reads it,
// that the data is data, that nothing is preferred, what is worth
// keeping, the evidence rule, what is refused, then the example answer and
// the negative example, and its last line is Boundary.
func TestPromptOrder(t *testing.T) {
	for _, f := range Forms {
		p := Prompt(f)
		last := 0
		for _, mark := range []string{"read by a coding agent", "is data", "Writing nothing is allowed", "What is worth keeping.",
			"Evidence.", "What is refused.", "\nA good answer:\n", "\nA wrong operation for the same entries:\n"} {
			i := strings.Index(p, mark)
			if i < last {
				t.Errorf("%s: %q is out of order", f, mark)
			}
			last = i
		}
		if !strings.HasSuffix(p, "\n\n"+Boundary+"\n") || strings.Count(p, Boundary) != 1 {
			t.Errorf("%s: the last line is not %q", f, Boundary)
		}
	}
	if Prompt(FormQuotes) == Prompt(FormSegments) {
		t.Error("the forms share one prompt")
	}
}

// TestPromptHoldsNoPlantedWord: no word a script plants is in either
// prompt as a whole word, so the example cannot lead an answer to a
// script's values.
func TestPromptHoldsNoPlantedWord(t *testing.T) {
	for _, f := range Forms {
		for _, s := range Scripts() {
			if got := named(Prompt(f), scenario.Forbid(s.Scenario)); len(got) > 0 {
				t.Errorf("%s: the prompt holds %s's %q", f, s.Scenario.ID, got)
			}
		}
	}
}

func ptr(s string) *string { return &s }

// TestParseAnswerReadsWhatItIsGiven: a well-formed answer is read field for
// field, nulls as nil, and nothing beyond its shape is checked: an unknown
// op, an empty ev, more than 24 ops and an unknown nothing all parse.
func TestParseAnswerReadsWhatItIsGiven(t *testing.T) {
	q := ` { "nothing" : null, "ops": [{"op":"item.supersede","id":"h3","kind":"decision","text":"Use \"x\" now.","admit":null,` +
		`"ev":[{"e":4,"q":"use x","as":"said"},{"as":"proposal","e":-2,"q":""}]}] } `
	a, err := ParseAnswer([]byte(q), FormQuotes)
	if err != nil {
		t.Fatal(err)
	}
	want := Answer{Ops: []Op{{Op: "item.supersede", ID: ptr("h3"), Kind: ptr("decision"), Text: ptr(`Use "x" now.`),
		Ev: []Span{{E: 4, Q: "use x", As: "said"}, {E: -2, As: "proposal"}}}}}
	if fmt.Sprintf("%+v", deref(a)) != fmt.Sprintf("%+v", deref(want)) {
		t.Errorf("read %+v, want %+v", deref(a), deref(want))
	}
	s := `{"ops":[{"op":"note","id":null,"kind":"did","text":null,"admit":null,"ev":[{"e":1,"s":2,"as":"result"}]}],"nothing":"only_progress"}`
	a, err = ParseAnswer([]byte(s), FormSegments)
	if err != nil || a.Ops[0].Ev[0].S != 2 || a.Ops[0].Ev[0].Q != "" || a.Ops[0].Text != nil || a.Nothing == nil || *a.Nothing != "only_progress" {
		t.Errorf("the segment answer reads %+v (%v)", a, err)
	}
	many := strings.Repeat(`{"op":"made.up","id":null,"kind":"other","text":"t","admit":"why","ev":[]},`, 30)
	for _, data := range []string{
		`{"ops":[],"nothing":"none_new"}`,
		`{"ops":[],"nothing":"zzz"}`,
		`{"ops":[` + strings.TrimSuffix(many, ",") + `],"nothing":null}`,
	} {
		if _, err := ParseAnswer([]byte(data), FormQuotes); err != nil {
			t.Errorf("%.60s: %v", data, err)
		}
	}
}

// deref is an answer with its pointers written out, for comparison.
func deref(a Answer) string {
	s := func(p *string) string {
		if p == nil {
			return "<nil>"
		}
		return *p
	}
	var b strings.Builder
	for _, op := range a.Ops {
		fmt.Fprintf(&b, "%s|%s|%s|%s|%s|%v;", op.Op, s(op.ID), s(op.Kind), s(op.Text), s(op.Admit), op.Ev)
	}
	return b.String() + s(a.Nothing)
}

// TestParseAnswerRefusesAnythingElse: data that is not one JSON object of
// the form's keys and types is an error, each way.
func TestParseAnswerRefusesAnythingElse(t *testing.T) {
	op := func(span string) string {
		return `{"ops":[{"op":"note","id":null,"kind":"did","text":"t","admit":null,"ev":[` + span + `]}],"nothing":null}`
	}
	for name, c := range map[string]struct {
		data string
		form Form
	}{
		"not JSON":          {`ops`, FormQuotes},
		"empty":             {``, FormQuotes},
		"an array":          {`[]`, FormQuotes},
		"two objects":       {`{"ops":[],"nothing":null}{"ops":[],"nothing":null}`, FormQuotes},
		"trailing text":     {`{"ops":[],"nothing":null} ok`, FormQuotes},
		"cut short":         {`{"ops":[],"nothing":null`, FormQuotes},
		"missing key":       {`{"ops":[]}`, FormQuotes},
		"extra key":         {`{"ops":[],"nothing":null,"summary":"x"}`, FormQuotes},
		"key twice":         {`{"ops":[],"nothing":null,"ops":[]}`, FormQuotes},
		"key in caps":       {`{"OPS":[],"nothing":null}`, FormQuotes},
		"ops null":          {`{"ops":null,"nothing":null}`, FormQuotes},
		"ops an object":     {`{"ops":{},"nothing":null}`, FormQuotes},
		"nothing a number":  {`{"ops":[],"nothing":3}`, FormQuotes},
		"nothing a list":    {`{"ops":[],"nothing":[]}`, FormQuotes},
		"op a string":       {`{"ops":["note"],"nothing":null}`, FormQuotes},
		"op missing ev":     {`{"ops":[{"op":"note","id":null,"kind":"did","text":"t","admit":null}],"nothing":null}`, FormQuotes},
		"op extra key":      {`{"ops":[{"op":"note","id":null,"kind":"did","text":"t","admit":null,"ev":[],"why":null}],"nothing":null}`, FormQuotes},
		"op null":           {`{"ops":[{"op":null,"id":null,"kind":"did","text":"t","admit":null,"ev":[]}],"nothing":null}`, FormQuotes},
		"id a number":       {`{"ops":[{"op":"note","id":3,"kind":"did","text":"t","admit":null,"ev":[]}],"nothing":null}`, FormQuotes},
		"text true":         {`{"ops":[{"op":"note","id":null,"kind":"did","text":true,"admit":null,"ev":[]}],"nothing":null}`, FormQuotes},
		"ev null":           {`{"ops":[{"op":"note","id":null,"kind":"did","text":"t","admit":null,"ev":null}],"nothing":null}`, FormQuotes},
		"s in quotes":       {op(`{"e":1,"s":1,"as":"said"}`), FormQuotes},
		"q and s":           {op(`{"e":1,"q":"x","s":1,"as":"said"}`), FormQuotes},
		"q in segments":     {op(`{"e":1,"q":"words","as":"said"}`), FormSegments},
		"no e":              {op(`{"q":"words","as":"said"}`), FormQuotes},
		"e a fraction":      {op(`{"e":1.5,"q":"words","as":"said"}`), FormQuotes},
		"e written 1.0":     {op(`{"e":1.0,"q":"words","as":"said"}`), FormQuotes},
		"e an exponent":     {op(`{"e":1e2,"q":"words","as":"said"}`), FormQuotes},
		"e a string":        {op(`{"e":"1","q":"words","as":"said"}`), FormQuotes},
		"e past int64":      {op(`{"e":9223372036854775808,"q":"words","as":"said"}`), FormQuotes},
		"s a string":        {op(`{"e":1,"s":"1","as":"said"}`), FormSegments},
		"q null":            {op(`{"e":1,"q":null,"as":"said"}`), FormQuotes},
		"as null":           {op(`{"e":1,"q":"words","as":null}`), FormQuotes},
		"span a list":       {op(`[1,"words","said"]`), FormQuotes},
		"not UTF-8":         {"{\"ops\":[],\"nothing\":\"\xff\"}", FormQuotes},
		"a byte order mark": {"\xef\xbb\xbf{\"ops\":[],\"nothing\":null}", FormQuotes},
		"unknown form":      {`{"ops":[],"nothing":null}`, "words"},
	} {
		if _, err := ParseAnswer([]byte(c.data), c.form); err == nil {
			t.Errorf("%s: no error", name)
		}
	}
}

// TestParseRepair: a repair answer is read with a null op as a skip, and
// anything not of its keys and types is an error.
func TestParseRepair(t *testing.T) {
	r, err := ParseRepair([]byte(`{"fixes":[{"index":2,"op":null},{"op":{"op":"ask.add","id":null,"kind":null,"text":"Why?","admit":null,"ev":[{"e":3,"s":1,"as":"report"}]},"index":0}]}`), FormSegments)
	if err != nil {
		t.Fatal(err)
	}
	if len(r.Fixes) != 2 || r.Fixes[0].Index != 2 || r.Fixes[0].Op != nil || r.Fixes[1].Index != 0 || r.Fixes[1].Op == nil ||
		r.Fixes[1].Op.Op != "ask.add" || r.Fixes[1].Op.Ev[0].S != 1 {
		t.Errorf("read %+v", r)
	}
	if r, err := ParseRepair([]byte(`{"fixes":[]}`), FormQuotes); err != nil || len(r.Fixes) != 0 {
		t.Errorf("an empty repair: %+v, %v", r, err)
	}
	for _, data := range []string{
		`{"fixes":null}`,
		`{"fixes":[],"ops":[]}`,
		`{"fixes":[{"index":1}]}`,
		`{"fixes":[{"index":1.0,"op":null}]}`,
		`{"fixes":[{"index":1,"op":"skip"}]}`,
		`{"fixes":[{"index":1,"op":null,"code":"bad_entry"}]}`,
		`{"fixes":[{"index":1,"op":{"op":"note"}}]}`,
		`{"fixes":[{"index":1,"op":{"op":"note","id":null,"kind":"did","text":"t","admit":null,"ev":[{"e":1,"s":1,"as":"said"}]}}]}`,
		`{"fixes":[]} {}`,
	} {
		if _, err := ParseRepair([]byte(data), FormQuotes); err == nil {
			t.Errorf("%s: no error", data)
		}
	}
	if _, err := ParseRepair([]byte(`{"fixes":[]}`), ""); err == nil {
		t.Error("an unknown form: no error")
	}
}
