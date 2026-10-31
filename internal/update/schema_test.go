package update

import (
	"encoding/json"
	"errors"
	"slices"
	"strings"
	"testing"

	"github.com/heyNag/eva-md/internal/threadpage"
)

// TestSchemaIsStrict checks the schema both hosts take: every object lists
// all its keys as required and takes no others, the keys and enums are
// those of docs/design.md "Output format", and it uses no keyword Codex's
// strict mode refuses.
func TestSchemaIsStrict(t *testing.T) {
	isolate(t)
	var root map[string]any
	if err := json.Unmarshal(Schema(threadpage.Page{}), &root); err != nil || !isObject(Schema(threadpage.Page{})) {
		t.Fatalf("the schema is not one JSON object: %v", err)
	}
	if strings.ContainsAny(string(Schema(threadpage.Page{})), "\n\t") {
		t.Error("the schema is not compact")
	}
	var objects func(where string, node any)
	count := 0
	objects = func(where string, node any) {
		m, ok := node.(map[string]any)
		if !ok {
			return
		}
		for _, banned := range []string{"maxLength", "minLength", "pattern", "format", "$schema", "anyOf", "oneOf"} {
			if _, ok := m[banned]; ok {
				t.Errorf("%s uses %s", where, banned)
			}
		}
		if m["type"] == "object" {
			count++
			props, _ := m["properties"].(map[string]any)
			var keys, required []string
			for k := range props {
				keys = append(keys, k)
			}
			for _, r := range m["required"].([]any) {
				required = append(required, r.(string))
			}
			slices.Sort(keys)
			slices.Sort(required)
			if !slices.Equal(keys, required) || m["additionalProperties"] != false {
				t.Errorf("%s: keys %v, required %v, additionalProperties %v", where, keys, required, m["additionalProperties"])
			}
			for k, p := range props {
				objects(where+"."+k, p)
			}
		}
		if items, ok := m["items"]; ok {
			objects(where+"[]", items)
		}
	}
	objects("output", root)
	if count != 5 {
		t.Errorf("%d objects, want 5: the output, a done line, an item, a Timeline line and a rule", count)
	}
	props := root["properties"].(map[string]any)
	item := props["items"].(map[string]any)["items"].(map[string]any)["properties"].(map[string]any)
	line := props["timeline"].(map[string]any)["items"].(map[string]any)["properties"].(map[string]any)
	rule := props["rules"].(map[string]any)["items"].(map[string]any)["properties"].(map[string]any)
	for _, key := range []string{"id", "text", "reason"} {
		if got := rule[key].(map[string]any)["type"]; !slices.Equal(toStrings(got), []string{"string", "null"}) {
			t.Errorf("rules %s type = %v, want string or null", key, got)
		}
	}
	if refs := rule["refs"].(map[string]any); refs["minItems"] != float64(1) || refs["maxItems"] != float64(maxRefs) {
		t.Errorf("rule refs bounds %v", refs)
	}
	enum := func(m map[string]any, key string) []string {
		var out []string
		for _, v := range m[key].(map[string]any)["enum"].([]any) {
			out = append(out, v.(string))
		}
		return out
	}
	for _, c := range []struct {
		got, want []string
	}{
		{enum(item, "kind"), itemKinds}, {enum(item, "action"), actions}, {enum(item, "tag"), tagNames},
		{enum(line, "kind"), lineKinds}, {enum(line, "tag"), tagNames},
		{enum(rule, "action"), ruleActions}, {enum(rule, "tag"), tagNames},
	} {
		if !slices.Equal(c.got, c.want) {
			t.Errorf("enum %v, want %v", c.got, c.want)
		}
	}
	bounds := map[string]float64{"done": maxDone, "items": maxAdds, "timeline": maxTimeline, "unknowns": maxUnknowns, "rules": maxRules}
	for key, want := range bounds {
		if got := props[key].(map[string]any)["maxItems"]; got != want {
			t.Errorf("%s maxItems = %v, want %v", key, got, want)
		}
	}
	// The items bound grows with the page: one action for each id, and room
	// for the adds. An item without an id, or a second one with an id, can't
	// be named.
	page := fullPage()
	page.Now.Next = append(page.Now.Next, threadpage.Item{Text: "Written by hand"}, page.Now.Next[0])
	var full map[string]any
	if err := json.Unmarshal(Schema(page), &full); err != nil {
		t.Fatal(err)
	}
	if got := full["properties"].(map[string]any)["items"].(map[string]any)["maxItems"]; got != float64(61+maxAdds) {
		t.Errorf("items maxItems for 61 ids = %v, want %d", got, 61+maxAdds)
	}
	for _, key := range []string{"id", "text", "reason", "replacement"} {
		if got := item[key].(map[string]any)["type"]; !slices.Equal(toStrings(got), []string{"string", "null"}) {
			t.Errorf("items %s type = %v, want string or null", key, got)
		}
	}
}

func toStrings(v any) []string {
	var out []string
	list, _ := v.([]any)
	for _, s := range list {
		out = append(out, s.(string))
	}
	return out
}

// TestParseOutputIsStrict checks that anything outside the schema rejects
// the run, and that what the schema allows parses.
func TestParseOutputIsStrict(t *testing.T) {
	isolate(t)
	good := `{"summary":"s","last_chat":"l","done":[{"text":"d","refs":[1]}],"items":[{"id":null,"kind":"next","action":"add","text":"t","tag":"stated","refs":[1,2],"reason":null,"replacement":null},{"id":"n3","kind":"decision","action":"keep","text":null,"tag":"stated","refs":[],"reason":null,"replacement":null}],"timeline":[{"kind":"finding","text":"f","tag":"observed","refs":[2]}],"unknowns":["u"]}`
	out, err := parseOutput([]byte(good), maxAdds)
	if err != nil {
		t.Fatalf("good output rejected: %v", err)
	}
	if out.Summary != "s" || out.LastChat != "l" || len(out.Done) != 1 || out.Items[0].ID != nil || *out.Items[1].ID != "n3" ||
		out.Items[1].Text != nil || !slices.Equal(out.Items[0].Refs, []int{1, 2}) || out.Timeline[0].Kind != "finding" || out.Unknowns[0] != "u" {
		t.Fatalf("parsed %+v", out)
	}
	item := `{"id":null,"kind":"next","action":"add","text":"t","tag":"stated","refs":[1],"reason":null,"replacement":null}`
	wrap := func(items string) string {
		return `{"summary":"s","last_chat":"l","done":[],"items":[` + items + `],"timeline":[],"unknowns":[]}`
	}
	many := func(s string, n int) string { return strings.TrimSuffix(strings.Repeat(s+",", n), ",") }
	for name, bad := range map[string]string{
		"not json":          `{"summary":`,
		"not an object":     `["summary"]`,
		"null":              `null`,
		"trailing data":     good + ` {}`,
		"missing key":       `{"summary":"s","last_chat":"l","done":[],"items":[],"timeline":[]}`,
		"extra key":         `{"summary":"s","last_chat":"l","done":[],"items":[],"timeline":[],"unknowns":[],"notes":""}`,
		"null summary":      `{"summary":null,"last_chat":"l","done":[],"items":[],"timeline":[],"unknowns":[]}`,
		"null array":        `{"summary":"s","last_chat":"l","done":null,"items":[],"timeline":[],"unknowns":[]}`,
		"number summary":    `{"summary":1,"last_chat":"l","done":[],"items":[],"timeline":[],"unknowns":[]}`,
		"unknown kind":      wrap(strings.Replace(item, `"next"`, `"todo"`, 1)),
		"unknown action":    wrap(strings.Replace(item, `"add"`, `"merge"`, 1)),
		"unknown tag":       wrap(strings.Replace(item, `"stated"`, `"Stated"`, 1)),
		"null tag":          wrap(strings.Replace(item, `"stated"`, `null`, 1)),
		"number id":         wrap(strings.Replace(item, `"id":null`, `"id":3`, 1)),
		"fractional ref":    wrap(strings.Replace(item, `[1]`, `[1.5]`, 1)),
		"ref as string":     wrap(strings.Replace(item, `[1]`, `["1"]`, 1)),
		"ref as float":      wrap(strings.Replace(item, `[1]`, `[1.0]`, 1)),
		"item missing key":  wrap(strings.Replace(item, `,"replacement":null`, ``, 1)),
		"item extra key":    wrap(strings.Replace(item, `"reason":null`, `"reason":null,"note":null`, 1)),
		"41 items":          wrap(many(item, 41)),
		"done without refs": `{"summary":"s","last_chat":"l","done":[{"text":"d","refs":[]}],"items":[],"timeline":[],"unknowns":[]}`,
		"done with 6 refs":  `{"summary":"s","last_chat":"l","done":[{"text":"d","refs":[1,2,3,4,5,6]}],"items":[],"timeline":[],"unknowns":[]}`,
		"9 done lines":      `{"summary":"s","last_chat":"l","done":[` + many(`{"text":"d","refs":[1]}`, 9) + `],"items":[],"timeline":[],"unknowns":[]}`,
		"21 lines":          `{"summary":"s","last_chat":"l","done":[],"items":[],"timeline":[` + many(`{"kind":"done","text":"d","tag":"observed","refs":[1]}`, 21) + `],"unknowns":[]}`,
		"line kind":         `{"summary":"s","last_chat":"l","done":[],"items":[],"timeline":[{"kind":"next","text":"d","tag":"observed","refs":[1]}],"unknowns":[]}`,
		"6 unknowns":        `{"summary":"s","last_chat":"l","done":[],"items":[],"timeline":[],"unknowns":["a","b","c","d","e","f"]}`,
		"unknown not text":  `{"summary":"s","last_chat":"l","done":[],"items":[],"timeline":[],"unknowns":[null]}`,
	} {
		if _, err := parseOutput([]byte(bad), maxAdds); !errors.Is(err, ErrRejected) {
			t.Errorf("%s: err = %v, want a rejection", name, err)
		}
	}
	if _, err := parseOutput([]byte(wrap(many(item, 40))), maxAdds); err != nil {
		t.Errorf("40 items rejected: %v", err)
	}
	if _, err := parseOutput([]byte(wrap(many(item, 41))), 41); err != nil {
		t.Errorf("41 items within a bound of 41 rejected: %v", err)
	}
	// An error names the place, never the text.
	_, err = parseOutput([]byte(wrap(strings.Replace(item, `"next"`, `"SECRET-VALUE"`, 1))), maxAdds)
	if err == nil || strings.Contains(err.Error(), "SECRET-VALUE") || !strings.Contains(err.Error(), "items[0].kind") {
		t.Errorf("error %q", err)
	}
	// The rules list is as strict as the rest, and only an answer shaped by
	// rules version 1, as good is, may leave it out.
	rule := `{"id":null,"action":"add","text":"Never mock the database","tag":"stated","refs":[1],"reason":null}`
	rules := func(list string) string { return strings.TrimSuffix(good, "}") + `,"rules":[` + list + `]}` }
	out, err = parseOutput([]byte(rules(rule+`,{"id":"r2","action":"drop","text":null,"tag":"stated","refs":[2,3],"reason":"the user changed it"}`)), maxAdds)
	if err != nil || len(out.Rules) != 2 || out.Rules[0].ID != nil || *out.Rules[0].Text != "Never mock the database" || *out.Rules[1].ID != "r2" ||
		out.Rules[1].Action != "drop" || *out.Rules[1].Reason != "the user changed it" || !slices.Equal(out.Rules[1].Refs, []int{2, 3}) {
		t.Fatalf("rules parsed %+v, %v", out.Rules, err)
	}
	for name, bad := range map[string]string{
		"null rules":         strings.TrimSuffix(good, "}") + `,"rules":null}`,
		"rules not a list":   strings.TrimSuffix(good, "}") + `,"rules":{}}`,
		"unknown action":     rules(strings.Replace(rule, `"add"`, `"keep"`, 1)),
		"unknown tag":        rules(strings.Replace(rule, `"stated"`, `"said"`, 1)),
		"rule without refs":  rules(strings.Replace(rule, `[1]`, `[]`, 1)),
		"rule with 6 refs":   rules(strings.Replace(rule, `[1]`, `[1,2,3,4,5,6]`, 1)),
		"rule missing key":   rules(strings.Replace(rule, `,"reason":null`, ``, 1)),
		"rule extra key":     rules(strings.Replace(rule, `"reason":null`, `"reason":null,"scope":"all"`, 1)),
		"number text":        rules(strings.Replace(rule, `"Never mock the database"`, `7`, 1)),
		"11 rules":           rules(many(rule, 11)),
		"other key left out": strings.Replace(rules(rule), `"unknowns":["u"],`, ``, 1),
	} {
		if _, err := parseOutput([]byte(bad), maxAdds); !errors.Is(err, ErrRejected) {
			t.Errorf("%s: err = %v, want a rejection", name, err)
		}
	}
}
