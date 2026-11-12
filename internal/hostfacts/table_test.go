package hostfacts

import (
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"testing"
)

func readDoc(t *testing.T, rel string) string {
	t.Helper()
	data, err := os.ReadFile(filepath.Join("..", "..", filepath.FromSlash(rel)))
	if err != nil {
		t.Fatal(err)
	}
	return string(data)
}

// tableRows gives the cells of the Markdown table that follows heading in
// doc, header and separator left out.
func tableRows(t *testing.T, doc, heading string) [][]string {
	t.Helper()
	_, rest, ok := strings.Cut(doc, "\n"+heading+"\n")
	if !ok {
		t.Fatalf("no heading %q", heading)
	}
	var rows [][]string
	started := false
	for _, line := range strings.Split(rest, "\n") {
		if !strings.HasPrefix(line, "|") {
			if started {
				break
			}
			continue
		}
		started = true
		cells := strings.Split(strings.Trim(line, "|"), " | ")
		for i := range cells {
			cells[i] = strings.TrimSpace(cells[i])
		}
		if strings.HasPrefix(cells[0], "---") || cells[0] == "Fact" || cells[0] == "Step" {
			continue
		}
		rows = append(rows, cells)
	}
	return rows
}

// The fact map's tables are the run-book's, cell for cell, so summary
// names each fact's steps as the run-book does.
func TestTablesEqualTheDocuments(t *testing.T) {
	book := readDoc(t, "docs/v2/host-sitting-runbook.md")
	var want []Fact
	for _, r := range tableRows(t, book, "## Facts to steps") {
		if len(r) != 5 {
			t.Fatalf("a row of Facts to steps has %d cells: %q", len(r), r)
		}
		id, title, _ := strings.Cut(r[0], " ")
		want = append(want, Fact{ID: id, Title: title, Sitting: r[1], Mark: r[2], Part1: r[3], Part2: r[4]})
	}
	if got := Facts(); !slices.Equal(got, want) {
		for i := range max(len(got), len(want)) {
			switch {
			case i >= len(got):
				t.Errorf("the table lacks %+v", want[i])
			case i >= len(want):
				t.Errorf("the table has %+v, which the run-book does not", got[i])
			case got[i] != want[i]:
				t.Errorf("row %d: the table has %+v, the run-book %+v", i, got[i], want[i])
			}
		}
	}
	var wantSteps []Step
	for _, r := range tableRows(t, book, "## Steps to facts") {
		if len(r) != 4 {
			t.Fatalf("a row of Steps to facts has %d cells: %q", len(r), r)
		}
		id, title, _ := strings.Cut(r[0], " ")
		wantSteps = append(wantSteps, Step{ID: id, Title: title, Where: r[1], Minutes: r[2], Facts: r[3]})
	}
	if got := Steps(); !slices.Equal(got, wantSteps) {
		t.Errorf("the steps' table differs from the run-book's: %d rows against %d", len(got), len(wantSteps))
		for i := range min(len(got), len(wantSteps)) {
			if got[i] != wantSteps[i] {
				t.Errorf("row %d: %+v against %+v", i, got[i], wantSteps[i])
			}
		}
	}
}

var (
	factHeading = regexp.MustCompile(`(?m)^#### (F-\d{3})\. `)
	settledBy   = regexp.MustCompile(`\*\*Settled by\.\*\* ([^*]*)\*\*Mark: ([a-z ]+)\.?\*\*([^\n]*)`)
)

// The fact map names every fact docs/v2/host-facts.md settles in a
// sitting, with its mark, and no other.
func TestFactMapNamesEverySittingFactWithItsMark(t *testing.T) {
	sheet := readDoc(t, "docs/v2/host-facts.md")
	locs := factHeading.FindAllStringSubmatchIndex(sheet, -1)
	sheetMarks := map[string]string{}
	for i, l := range locs {
		end := len(sheet)
		if i+1 < len(locs) {
			end = locs[i+1][0]
		}
		id := sheet[l[2]:l[3]]
		m := settledBy.FindStringSubmatch(sheet[l[0]:end])
		if m == nil {
			continue
		}
		where := m[1]
		if !strings.Contains(where, "sitting") && !strings.Contains(where, "Sitting") {
			continue
		}
		sheetMarks[id] = strings.TrimSpace(m[2]) + m[3]
	}
	if len(sheetMarks) != 141 {
		t.Errorf("the sheet settles %d facts in a sitting; its counts say 141", len(sheetMarks))
	}
	seen := map[string]bool{}
	for _, f := range Facts() {
		seen[f.ID] = true
		mark, ok := sheetMarks[f.ID]
		if !ok {
			t.Errorf("%s is in the fact map, and the sheet settles it in no sitting", f.ID)
			continue
		}
		// The table writes the sheet's mark short: its first words, and the
		// group a later fact waits for.
		lead := strings.Fields(strings.NewReplacer(",", " ", ";", " ").Replace(f.Mark))[0]
		if !strings.HasPrefix(mark, lead) {
			t.Errorf("%s: the fact map's mark %q, the sheet's %q", f.ID, f.Mark, mark)
		}
		if g := regexp.MustCompile(`^later, group ([A-Z])$`).FindStringSubmatch(f.Mark); g != nil && !strings.Contains(mark, "group "+g[1]) {
			t.Errorf("%s: the fact map waits for group %s, the sheet says %q", f.ID, g[1], mark)
		}
		if strings.HasPrefix(f.Mark, "core") != strings.HasPrefix(mark, "core") {
			t.Errorf("%s: core in one and not the other: %q, %q", f.ID, f.Mark, mark)
		}
	}
	for id := range sheetMarks {
		if !seen[id] {
			t.Errorf("the sheet settles %s in a sitting, and the fact map does not name it", id)
		}
	}
	var core []string
	for _, f := range Facts() {
		if f.Core() {
			core = append(core, f.ID)
		}
	}
	if want := []string{"F-016", "F-033", "F-121", "F-142", "F-143", "F-144", "F-175"}; !slices.Equal(core, want) {
		t.Errorf("the core facts are %v, the run-book's %v", core, want)
	}
}

// Every step a fact names is a step of the table, and the summaries'
// scopes are the run-book's sittings.
func TestScopesAndRefs(t *testing.T) {
	for _, f := range Facts() {
		for _, r := range f.Refs() {
			if !IsStep(r.ID) {
				t.Errorf("%s names %s, which is not a step", f.ID, r.ID)
			}
		}
	}
	one, ok := Scope("sitting-1")
	if !ok || !slices.Contains(one, "C-04") || !slices.Contains(one, "G-04") || slices.Contains(one, "X-04") {
		t.Errorf("sitting-1 is %v", one)
	}
	two, ok := Scope("sitting-2")
	if !ok || !slices.Contains(two, "X-44") || slices.Contains(two, "C-04") {
		t.Errorf("sitting-2 is %v", two)
	}
	if _, ok := Scope("sitting-3"); ok {
		t.Error("a scope the run-book does not have was found")
	}
	if _, ok := Scope("Z"); ok {
		t.Error("a group the run-book does not have was found")
	}
	var ids []string
	for _, f := range FactsIn(one) {
		if f.Core() {
			ids = append(ids, f.ID)
		}
	}
	if want := []string{"F-016", "F-033", "F-175"}; !slices.Equal(ids, want) {
		t.Errorf("sitting 1's core facts are %v, want %v", ids, want)
	}
}

var sayLine = regexp.MustCompile(`\*\*[Ss]ay:\*\* "([^"]*)"`)

// voice-check compares the hand-overs with the run-book's voice script,
// which VoiceScript holds word for word.
func TestVoiceScriptEqualsTheRunBook(t *testing.T) {
	book := readDoc(t, "docs/v2/host-sitting-runbook.md")
	_, step, ok := strings.Cut(book, "\n#### X-44.")
	if !ok {
		t.Fatal("the run-book has no step X-44")
	}
	step, _, _ = strings.Cut(step, "\n#### ")
	var said []string
	for _, m := range sayLine.FindAllStringSubmatch(step, -1) {
		said = append(said, m[1])
	}
	var script []string
	for _, l := range VoiceScript {
		script = append(script, l.Text)
	}
	if !slices.Equal(said, script) {
		t.Errorf("the script is\n%q\nthe run-book says\n%q", script, said)
	}
	for _, l := range VoiceScript {
		if label, kept, of := ClosestLine(l.Text); label != l.Label || kept != of {
			t.Errorf("line %s is closest to %s (%d of %d words)", l.Label, label, kept, of)
		}
	}
	if label, kept, of := ClosestLine("please have the agent make fig.txt in the orchard folder with the word ripe"); label != "2" || kept == of {
		t.Errorf("a reworded request is closest to %s, %d of %d words", label, kept, of)
	}
}
