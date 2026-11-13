package task

import (
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/heyNag/eva-md/internal/eval/results"
)

// catalogueFile is the continuation catalogue in the repository.
var catalogueFile = filepath.Join("..", "..", "..", "..", "eval", "continuation", "metrics.json")

func TestFirstPrompt(t *testing.T) {
	x := load(t, "hidden-parse")
	want := map[Condition]string{
		Off:    x.Prompt,
		Eva:    x.Prompt,
		Oracle: x.Note + "\n\n---\n\n" + x.Prompt,
	}
	for c, w := range want {
		got, err := FirstPrompt(x, c)
		if err != nil || got != w {
			t.Errorf("FirstPrompt(%s) = %q, %v, want %q", c, got, err, w)
		}
	}
	if _, err := FirstPrompt(x, "host"); err == nil {
		t.Error("FirstPrompt took an unknown condition")
	}
	if c, err := ParseCondition("oracle"); err != nil || c != Oracle {
		t.Errorf("ParseCondition(oracle) = %q, %v", c, err)
	}
	if _, err := ParseCondition("control"); err == nil {
		t.Error("ParseCondition took an unknown condition")
	}
}

// baseRow is the made-up run a trial's rows take their source from.
var baseRow = results.Row{
	Metric:      "ignored",
	Commit:      strings.Repeat("ab", 20),
	Dirty:       true,
	Host:        "host-a",
	HostVersion: "0.167.0",
	Model:       "model-a",
	Time:        time.Date(2026, 11, 13, 9, 30, 0, 0, time.UTC),
}

func TestRowsExact(t *testing.T) {
	x := load(t, "hidden-parse")
	asked := 2
	o := Outcome{Solved: true, FirstTry: false, Corrections: 1, Rounds: 2, AgentEvents: 14, PromptTokens: 212340, CachedTokens: 181200,
		OutputTokens: 6120, WallMs: 184000, AskedAgain: &asked, Memory: &Spend{Tokens: 9120, Ms: 41000}}
	row := func(measure string, v float64, unit string) results.Row {
		return results.Row{V: 1, Metric: "continuation.hidden.eva." + measure, Value: v, Unit: unit, Commit: baseRow.Commit, Dirty: true,
			Instrument: "continuation", Scenario: "hidden-parse.t3", Host: "host-a", HostVersion: "0.167.0", Model: "model-a", Seed: 3, Time: baseRow.Time}
	}
	all := []results.Row{
		row("solved", 1, "ratio"),
		row("first_try", 0, "ratio"),
		row("corrections", 1, "count"),
		row("rounds", 2, "count"),
		row("agent_events", 14, "count"),
		row("prompt_tokens", 212340, "tokens"),
		row("cached_tokens", 181200, "tokens"),
		row("output_tokens", 6120, "tokens"),
		row("wall_ms", 184000, "ms"),
		row("asked_again", 2, "count"),
		row("memory_tokens", 9120, "tokens"),
		row("memory_ms", 41000, "ms"),
	}
	got, err := Rows(x, Eva, 3, o, baseRow)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(got, all) {
		t.Fatalf("Rows =\n%+v\nwant\n%+v", got, all)
	}

	o.AskedAgain, o.Memory = nil, nil
	got, err = Rows(x, Eva, 3, o, baseRow)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(got, all[:9]) {
		t.Fatalf("Rows with nothing measured =\n%+v\nwant\n%+v", got, all[:9])
	}

	o.AskedAgain = &asked
	got, err = Rows(x, Eva, 3, o, baseRow)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(got, all[:10]) {
		t.Fatalf("Rows with asked-again measured =\n%+v\nwant\n%+v", got, all[:10])
	}

	if _, err := Rows(x, "host", 1, o, baseRow); err == nil {
		t.Error("Rows took an unknown condition")
	}
	if _, err := Rows(x, Off, 0, o, baseRow); err == nil {
		t.Error("Rows took trial 0")
	}
}

func TestCatalogueCoversEveryRow(t *testing.T) {
	cat, err := results.LoadCatalogue(catalogueFile)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(cat, Catalogue()) {
		t.Fatal("eval/continuation/metrics.json is not the catalogue Catalogue gives")
	}
	if n := len(cat.Metrics); n != len(Families)*len(Conditions)*len(Measures) || n != 108 {
		t.Fatalf("the catalogue lists %d metrics", n)
	}
	for _, m := range cat.Metrics {
		eva := strings.Contains(m.Name, ".eva.")
		want := len(CataloguePaths)
		if eva {
			want += len(EvaPaths)
		}
		if len(m.Paths) != want || m.Paths[0] != "eval/continuation/testdata" || m.Paths[1] != "internal/eval/continuation" ||
			eva != (len(m.Paths) > 2 && m.Paths[2] == "cmd/eva-md") {
			t.Fatalf("%s covers %v", m.Name, m.Paths)
		}
	}
	tasks, err := LoadAll(fixtures)
	if err != nil {
		t.Fatal(err)
	}
	asked := 0
	o := Outcome{Solved: true, FirstTry: true, AskedAgain: &asked, Memory: &Spend{}}
	families := map[string]bool{}
	for _, x := range tasks {
		families[x.Family] = true
		for _, c := range Conditions {
			rows, err := Rows(x, c, 1, o, baseRow)
			if err != nil {
				t.Fatal(err)
			}
			if len(rows) != len(Measures) {
				t.Fatalf("%s under %s gave %d rows", x.ID, c, len(rows))
			}
			if err := cat.Check(rows); err != nil {
				t.Fatalf("%s under %s: %v", x.ID, c, err)
			}
			store := results.Store{Dir: tempDir(t), Forbid: Forbid(x)}
			if err := store.Append(rows...); err != nil {
				t.Fatalf("%s under %s: the store refused the rows: %v", x.ID, c, err)
			}
		}
	}
	if len(families) != len(Families) {
		t.Fatalf("the fixtures cover the families %v", families)
	}
}

func TestForbid(t *testing.T) {
	x := load(t, "hidden-parse")
	x.Facts = append(x.Facts, Fact{ID: "f-two", Statement: "s", Values: []string{"tallyhoop", "quillspar"}})
	if got, want := Forbid(x), []string{"ledgerkit", "quillspar", "tallyhoop"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("Forbid = %v, want %v", got, want)
	}
}

func TestAValueInAnIDIsRefusedByValidateAndByTheStore(t *testing.T) {
	x := load(t, "hidden-parse")
	x.ID = "hidden-quillspar"
	if err := x.Validate(); err == nil {
		t.Fatal("Validate took a value in the task's id")
	}
	rows, err := Rows(x, Off, 1, Outcome{}, baseRow)
	if err != nil {
		t.Fatal(err)
	}
	store := results.Store{Dir: tempDir(t), Forbid: Forbid(x)}
	if err := store.Append(rows...); err == nil || !strings.Contains(err.Error(), "scenario holds a word the run forbids") {
		t.Fatalf("the store took a row holding a value: %v", err)
	}
	if _, err := results.Load(store.Dir); err == nil {
		t.Fatal("a refused append left a rows file")
	}
}
