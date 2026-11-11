package update

import (
	"encoding/json"
	"errors"
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"maps"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"testing"

	"github.com/heyNag/eva-md/internal/threadpage"
)

// fullAnswer is a valid answer to rules version 4 with every list filled,
// so every place of the schema is in it.
const fullAnswer = `{"summary":"s","last_chat":"l","done":[{"text":"d","refs":[1]}],` +
	`"items":[{"id":null,"kind":"next","action":"add","text":"t","tag":"stated","refs":[1,2],"reason":null,"replacement":null}],` +
	`"timeline":[{"kind":"finding","text":"f","tag":"observed","refs":[2]}],"unknowns":["u"],` +
	`"rules":[{"id":null,"action":"add","text":"Never mock the database","tag":"stated","refs":[1],"reason":null}],` +
	`"knowledge":[{"id":null,"action":"add","kind":"decision","title":"Inbox format","text":"Use JSONL for the inbox","tag":"stated","refs":[1],"scope":"project","reason":null,"paths":["internal/inbox"]}]}`

// Every way an answer can break the schema is refused by a rule of
// RefusalRules at a place KnownPlace takes, so the report of eva-md status
// --updates shows each, and each of the schema's rules is met: the answer
// is broken at each of its places in turn, by a value of each other type,
// a key taken away or added, and a list emptied or overfilled.
func TestEveryRefusalNamesAKnownRuleAndPlace(t *testing.T) {
	isolate(t)
	if _, err := parseOutput([]byte(fullAnswer), maxAdds); err != nil {
		t.Fatalf("the full answer is refused: %v", err)
	}
	var answer any
	if err := json.Unmarshal([]byte(fullAnswer), &answer); err != nil {
		t.Fatal(err)
	}
	seen := map[string]bool{}
	tried := 0
	try := func(broken any, how string) {
		t.Helper()
		data, err := json.Marshal(broken)
		if err != nil {
			t.Fatal(err)
		}
		_, err = parseOutput(data, maxAdds)
		if err == nil {
			return
		}
		tried++
		if !errors.Is(err, ErrRejected) {
			t.Fatalf("%s: not a refusal: %v", how, err)
		}
		r := rejectionOf(err)
		if r.Rule == RuleOther || !slices.Contains(RefusalRules, r.Rule) || !KnownPlace(r.Place) {
			t.Errorf("%s: refused as %+v", how, *r)
		}
		seen[r.Rule] = true
	}
	// each calls with the answer as a whole and a setter for each place in
	// it, by its path.
	var each func(v any, path string, set func(any))
	each = func(v any, path string, set func(any)) {
		for _, other := range []any{map[string]any{"zq": 1}, "zq", 7.0, []any{}, nil} {
			set(other)
			try(answer, fmt.Sprintf("%s as %v", path, other))
		}
		set(v)
		switch v := v.(type) {
		case map[string]any:
			for _, key := range slices.Sorted(maps.Keys(v)) {
				value := v[key]
				delete(v, key)
				try(answer, path+" without "+key)
				v[key] = value
				each(value, path+"."+key, func(x any) { v[key] = x })
			}
			v["zq_extra"] = 1
			try(answer, path+" with a key of its own")
			delete(v, "zq_extra")
		case []any:
			many := make([]any, 300)
			for i := range many {
				many[i] = v[0]
			}
			set(many)
			try(answer, path+" overfilled")
			set(v)
			for i := range v {
				each(v[i], fmt.Sprintf("%s[%d]", path, i), func(x any) { v[i] = x })
			}
		}
	}
	each(answer, "output", func(x any) { answer = x })
	for _, rule := range []string{RuleNotObject, RuleMissingKey, RuleExtraKey, RuleNotString, RuleNotNull, RuleNotEnum, RuleNotArray, RuleArraySize, RuleNotInteger} {
		if !seen[rule] {
			t.Errorf("no broken answer was refused by %s", rule)
		}
	}
	if tried < 100 {
		t.Fatalf("only %d broken answers", tried)
	}
}

// The checker's own refusals of an answer whole, too many uncited dates and
// most of the page's items carried with no action, name their rules.
func TestTheCheckersRefusalsNameTheirRules(t *testing.T) {
	isolate(t)
	b := build(t, work(), threadpage.Page{}, Options{})
	dated := output{}
	for i := 0; i < 4; i++ {
		dated.Items = append(dated.Items, opAdd("next", fmt.Sprintf("Ship on 2026-02-0%d", i+1), "stated", wUser))
	}
	if _, _, err := Validate(encode(t, dated), b, threadpage.Page{}); !errors.Is(err, ErrRejected) || *rejectionOf(err) != (Rejection{RuleDates, "output"}) {
		t.Fatalf("4 uncited dates: %v, %+v", err, rejectionOf(err))
	}
	page := accounting()
	b = build(t, work(), page, Options{})
	carried := output{Items: []itemOp{opKeep("n1", "next", "stated")}}
	if _, _, err := Validate(encode(t, carried), b, page); !errors.Is(err, ErrRejected) || *rejectionOf(err) != (Rejection{RuleCarried, "items"}) {
		t.Fatalf("three of four carried: %v, %+v", err, rejectionOf(err))
	}
}

// The lists of names hold every outcome, reason and refusal rule the
// package declares, so none is shown as other by the report, and the
// triggers every trigger.
func TestTheNameListsHoldEveryName(t *testing.T) {
	files, err := filepath.Glob("*.go")
	if err != nil {
		t.Fatal(err)
	}
	declared := map[string][]string{}
	fset := token.NewFileSet()
	for _, name := range files {
		if strings.HasSuffix(name, "_test.go") {
			continue
		}
		f, err := parser.ParseFile(fset, name, nil, 0)
		if err != nil {
			t.Fatal(err)
		}
		for _, d := range f.Decls {
			g, ok := d.(*ast.GenDecl)
			if !ok || g.Tok != token.CONST {
				continue
			}
			for _, spec := range g.Specs {
				v := spec.(*ast.ValueSpec)
				for i, id := range v.Names {
					for _, prefix := range []string{"Outcome", "Reason", "Rule", "Trigger"} {
						// A name of the kind, as RuleDates, and not Rules,
						// the update's prompt, or the trigger's flag.
						rest, ok := strings.CutPrefix(id.Name, prefix)
						if !ok || rest == "" || rest[0] < 'A' || rest[0] > 'Z' || id.Name == "TriggerFlag" || i >= len(v.Values) {
							continue
						}
						lit, ok := v.Values[i].(*ast.BasicLit)
						if !ok || lit.Kind != token.STRING {
							continue
						}
						s, _ := strconv.Unquote(lit.Value)
						declared[prefix] = append(declared[prefix], s)
					}
				}
			}
		}
	}
	for prefix, list := range map[string][]string{"Outcome": Outcomes, "Reason": Reasons, "Rule": RefusalRules, "Trigger": Triggers} {
		got, want := slices.Sorted(slices.Values(list)), slices.Sorted(slices.Values(declared[prefix]))
		if !slices.Equal(got, want) {
			t.Errorf("%s list %q, declared %q", prefix, got, want)
		}
	}
}
