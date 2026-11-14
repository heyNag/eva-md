package adapter

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"regexp"
	"slices"
	"strings"
	"testing"
)

// checkTable fails on a row or quirk the table can't stand behind: a host
// eva-md doesn't know, a range that isn't N.N.N to a later N.N.N, rows of a
// host out of order or overlapping, and a quirk with no fixture in dir that
// shows it.
func checkTable(rows []Row, dir string) error {
	ids := map[string]bool{}
	last := map[ID][3]int{}
	for _, r := range rows {
		if _, ok := ByID(string(r.Host)); !ok {
			return fmt.Errorf("row for unknown host %q", r.Host)
		}
		lo, hi, err := versionRange(r.Lowest, r.Highest)
		if err != nil {
			return fmt.Errorf("%s row: %w", r.Host, err)
		}
		if prev, ok := last[r.Host]; ok && compare(lo, prev) <= 0 {
			return fmt.Errorf("%s row from %s is out of order or overlaps the one before", r.Host, r.Lowest)
		}
		last[r.Host] = hi
		if len(r.Events) == 0 || r.Source == "" {
			return fmt.Errorf("%s row from %s has no events or no source", r.Host, r.Lowest)
		}
		for _, q := range r.Quirks {
			if q.ID == "" || ids[q.ID] {
				return fmt.Errorf("quirk id %q is empty or repeated", q.ID)
			}
			ids[q.ID] = true
			if q.Host != r.Host {
				return fmt.Errorf("quirk %s is %s's, in a row of %s", q.ID, q.Host, r.Host)
			}
			qlo, qhi, err := versionRange(q.Lowest, q.Highest)
			if err != nil {
				return fmt.Errorf("quirk %s: %w", q.ID, err)
			}
			if compare(qlo, lo) < 0 || compare(qhi, hi) > 0 {
				return fmt.Errorf("quirk %s's range is outside its row's", q.ID)
			}
			if q.What == "" || strings.Contains(q.What, "\n") {
				return fmt.Errorf("quirk %s doesn't say what it is in one line", q.ID)
			}
			if q.Fixture == "" || q.Shows == "" {
				return fmt.Errorf("quirk %s has no fixture", q.ID)
			}
			data, err := os.ReadFile(filepath.Join(dir, q.Fixture))
			if err != nil {
				return fmt.Errorf("quirk %s's fixture: %w", q.ID, err)
			}
			if !strings.Contains(string(data), q.Shows) {
				return fmt.Errorf("quirk %s's fixture doesn't show it", q.ID)
			}
		}
	}
	return nil
}

func versionRange(lowest, highest string) (lo, hi [3]int, err error) {
	lo, okLo := parseVersion(lowest)
	hi, okHi := parseVersion(highest)
	if !okLo || !okHi || compare(lo, hi) > 0 {
		return lo, hi, fmt.Errorf("range %q to %q is not N.N.N to a later N.N.N", lowest, highest)
	}
	return lo, hi, nil
}

// copyTable copies rows and their quirks, so a change to the copy leaves the
// table alone.
func copyTable(rows []Row) []Row {
	out := make([]Row, len(rows))
	for i, r := range rows {
		r.Events = slices.Clone(r.Events)
		r.Quirks = slices.Clone(r.Quirks)
		out[i] = r
	}
	return out
}

// The real table passes the check, and holds at least the three quirks the
// design names, for the hosts it names.
func TestTablePassesTheQuirkCheck(t *testing.T) {
	if err := checkTable(table, filepath.Join("testdata", "quirks")); err != nil {
		t.Fatal(err)
	}
	have := map[string]ID{}
	for _, q := range Quirks() {
		have[q.ID] = q.Host
	}
	for id, host := range map[string]ID{
		"codex.marketplace-folder-missing":  Codex,
		"codex.marketplace-root-keys":       Codex,
		"claude-code.marketplace-root-keys": ClaudeCode,
		"codex.features-list-hooks-row":     Codex,
	} {
		if have[id] != host {
			t.Errorf("quirk %s of %s is missing", id, host)
		}
	}
}

// A copy of the table with any one quirk's fixture taken away, emptied of
// what shows the quirk, or never named, fails the check.
func TestAQuirkWithoutItsFixtureFails(t *testing.T) {
	dir := filepath.Join("testdata", "quirks")
	// A copy of the fixtures, unchanged, passes, so what fails below fails
	// for the one change made.
	if err := checkTable(table, copyFixtures(t, dir, Quirk{})); err != nil {
		t.Fatalf("the copied fixtures fail before any change: %v", err)
	}
	n := 0
	for i := range table {
		for j, q := range table[i].Quirks {
			n++
			unnamed := copyTable(table)
			unnamed[i].Quirks[j].Fixture = ""
			if err := checkTable(unnamed, dir); err == nil {
				t.Errorf("quirk %s with no fixture named passed", q.ID)
			}
			gone := copyTable(table)
			gone[i].Quirks[j].Fixture = "no-such-fixture.txt"
			if err := checkTable(gone, dir); !errors.Is(err, os.ErrNotExist) {
				t.Errorf("quirk %s whose fixture is missing: %v", q.ID, err)
			}
			if err := checkTable(table, copyFixtures(t, dir, q)); err == nil {
				t.Errorf("quirk %s whose fixture no longer shows it passed", q.ID)
			}
		}
	}
	if n < 3 {
		t.Fatalf("the table has %d quirks", n)
	}
}

// copyFixtures copies the fixtures in dir to a new folder, with the text
// that shows quirk q taken out of q's fixture.
func copyFixtures(t *testing.T, dir string, q Quirk) string {
	t.Helper()
	other := t.TempDir()
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	for _, e := range entries {
		data, err := os.ReadFile(filepath.Join(dir, e.Name()))
		if err != nil {
			t.Fatal(err)
		}
		if q.Fixture != "" && e.Name() == q.Fixture {
			data = []byte(strings.ReplaceAll(string(data), q.Shows, ""))
		}
		if err = os.WriteFile(filepath.Join(other, e.Name()), data, 0o600); err != nil {
			t.Fatal(err)
		}
	}
	return other
}

// Each host's rows declare the events v1's plugin declares on it, as
// install's goldens hold them.
func TestRowsHoldTheEventsV1Declares(t *testing.T) {
	for id, golden := range map[ID]string{ClaudeCode: "claude-hooks.golden.json", Codex: "codex-hooks.golden.json"} {
		data, err := os.ReadFile(filepath.Join("..", "..", "install", "testdata", golden))
		if err != nil {
			t.Fatal(err)
		}
		var hooks struct {
			Hooks map[string]json.RawMessage `json:"hooks"`
		}
		if err = json.Unmarshal(data, &hooks); err != nil {
			t.Fatal(err)
		}
		var want []string
		for event := range hooks.Hooks {
			want = append(want, event)
		}
		slices.Sort(want)
		rows := Rows(id)
		if len(rows) == 0 {
			t.Fatalf("%s has no rows", id)
		}
		for _, r := range rows {
			got := slices.Sorted(slices.Values(r.Events))
			if !slices.Equal(got, want) {
				t.Errorf("%s row from %s declares %v, v1 declares %v", id, r.Lowest, got, want)
			}
		}
	}
}

// The newest row of each host ends at the newest version eva-md was checked
// against.
func TestTestedMaximum(t *testing.T) {
	for id, want := range map[ID]string{ClaudeCode: "2.1.316", Codex: "0.166.0"} {
		rows := Rows(id)
		if len(rows) == 0 || rows[len(rows)-1].Highest != want {
			t.Errorf("%s's tested maximum: rows %+v, want %s", id, rows, want)
		}
	}
}

func TestFindVersion(t *testing.T) {
	for out, want := range map[string]string{
		"2.1.316 (Claude Code)\n":    "2.1.316",
		"codex-cli 0.166.0\n":        "0.166.0",
		"tool 1..3 then 4.5.6 7.8.9": "4.5.6",
		"version 1.2\n":              "",
		"v1.2.3":                     "",
		"1.2.3.4":                    "",
		"1.2.3-beta.1":               "",
		"-1.2.3":                     "",
		"":                           "",
	} {
		if got := findVersion([]byte(out)); got != want {
			t.Errorf("findVersion(%q) = %q, want %q", out, got, want)
		}
	}
}

// A version below the lowest row, or that can't be read, has no row; one in
// a row's range has that row, both ends included; one above the tested
// maximum has the newest row and is untested. Numbers compare as numbers.
func TestMatch(t *testing.T) {
	codex, claude := &Rows(Codex)[0], &Rows(ClaudeCode)[0]
	for _, c := range []struct {
		id              ID
		version         string
		row             *Row
		known, untested bool
	}{
		{Codex, "0.163.9", nil, false, false},
		{Codex, "0.164.0", codex, true, false},
		{Codex, "0.165.12", codex, true, false},
		{Codex, "0.166.0", codex, true, false},
		{Codex, "0.166.1", codex, false, true},
		{Codex, "0.1000.0", codex, false, true},
		{Codex, "1.0.0", codex, false, true},
		{Codex, "", nil, false, false},
		{ClaudeCode, "2.1.310", nil, false, false},
		{ClaudeCode, "2.1.311", claude, true, false},
		{ClaudeCode, "2.1.316", claude, true, false},
		{ClaudeCode, "2.1.317", claude, false, true},
		{ClaudeCode, "0.166.0", nil, false, false},
		{"generic", "1.0.0", nil, false, false},
	} {
		row, known, untested := match(c.id, c.version)
		if known != c.known || untested != c.untested || (row == nil) != (c.row == nil) ||
			(row != nil && (row.Host != c.row.Host || row.Lowest != c.row.Lowest)) {
			t.Errorf("match(%s, %q) = %+v, %v, %v", c.id, c.version, row, known, untested)
		}
	}
}

// A version between two rows of a host has no row, and the newest row is
// given only above the newest.
func TestMatchBetweenRows(t *testing.T) {
	saved := table
	t.Cleanup(func() { table = saved })
	table = []Row{
		{Host: Codex, Lowest: "1.0.0", Highest: "1.2.0", Events: []string{"Stop"}, Source: "made up"},
		{Host: Codex, Lowest: "1.4.0", Highest: "1.9.0", Events: []string{"Stop"}, Source: "made up"},
	}
	if err := checkTable(table, t.TempDir()); err != nil {
		t.Fatal(err)
	}
	for version, want := range map[string]string{"1.1.0": "1.0.0", "1.3.0": "", "1.5.0": "1.4.0", "2.0.0": "1.4.0"} {
		row, _, _ := match(Codex, version)
		got := ""
		if row != nil {
			got = row.Lowest
		}
		if got != want {
			t.Errorf("match(%s) gave the row from %q, want %q", version, got, want)
		}
	}
	table = []Row{table[1], table[0]}
	if err := checkTable(table, t.TempDir()); err == nil {
		t.Error("rows out of order passed the check")
	}
}

// A third host registered only here, in a test, is found by All and ByID and
// gets its spec row, with no other change.
func TestAThirdHostIsARowAndAName(t *testing.T) {
	savedRegistry, savedTable := registry, table
	t.Cleanup(func() { registry, table = savedRegistry, savedTable })
	third := Host{ID: "example-host", Names: Names{App: "Example Host", CLI: "example-host-cli", InstallName: "example"}}
	registry = append(slices.Clone(registry), third)
	table = append(copyTable(table), Row{
		Host: third.ID, Lowest: "3.0.0", Highest: "3.4.0", Events: []string{"SessionStart", "Stop"}, Source: "made up for this test",
	})
	if err := checkTable(table, filepath.Join("testdata", "quirks")); err != nil {
		t.Fatal(err)
	}
	all := All()
	if len(all) != 3 || all[2] != third || all[0].ID != ClaudeCode || all[1].ID != Codex {
		t.Fatalf("All() = %+v", all)
	}
	if h, ok := ByID("example-host"); !ok || h != third {
		t.Fatalf("ByID = %+v, %v", h, ok)
	}
	if h, ok := ByInstallName("example"); !ok || h != third {
		t.Fatalf("ByInstallName = %+v, %v", h, ok)
	}
	dir := t.TempDir()
	fakeCLI(t, dir, "example-host-cli", "echo 'Example Host 3.2.0'")
	p := Probe(context.Background(), third.ID, Env{LookPath: lookIn(dir)})
	if p.Version != "3.2.0" || !p.Known || p.Untested || p.Row == nil || p.Row.Host != third.ID || p.Row.Lowest != "3.0.0" {
		t.Fatalf("probe of the third host: %+v", p)
	}
}

// A change to the rows Rows gives, or to the row a probe's match gives,
// leaves the table alone.
func TestRowsAreCopies(t *testing.T) {
	want := copyTable(table)
	r := Rows(Codex)[0]
	r.Events[0] = "Changed"
	r.Quirks[0].Fixture = ""
	row, known, _ := match(ClaudeCode, "2.1.314")
	if row == nil || !known {
		t.Fatalf("match gave no row: %+v", row)
	}
	row.Events[0] = "Changed"
	row.Quirks[0].Shows = ""
	if !reflect.DeepEqual(table, want) {
		t.Fatalf("the table changed: %+v", table)
	}
	if err := checkTable(table, filepath.Join("testdata", "quirks")); err != nil {
		t.Fatal(err)
	}
}

// Each host's lowest row starts at the lowest version of it the docs record
// eva-md being checked on, so a version the docs name always has a row, and
// its source says so. A version is recorded as the host's app or CLI name,
// then N.N.N, in the docs, the specs v1's design was split into, the
// decision records or README.md; a line break between the name and the
// number, as a spec's rewrapped line may have, is read as a space.
func TestLowestRowIsTheLowestVersionRecorded(t *testing.T) {
	root := filepath.Join("..", "..", "..")
	var files []string
	for _, dir := range []string{"docs", filepath.Join("docs", "spec"), filepath.Join("docs", "decisions")} {
		found, err := filepath.Glob(filepath.Join(root, dir, "*.md"))
		if err != nil || len(found) == 0 {
			t.Fatalf("no docs in %s: %v", dir, err)
		}
		files = append(files, found...)
	}
	files = append(files, filepath.Join(root, "README.md"))
	var text strings.Builder
	for _, f := range files {
		data, err := os.ReadFile(f)
		if err != nil {
			t.Fatal(err)
		}
		text.Write(data)
		text.WriteByte('\n')
	}
	for _, h := range All() {
		re := regexp.MustCompile(`(?i)\b(?:` + regexp.QuoteMeta(h.Names.App) + `|` + regexp.QuoteMeta(h.Names.CLI) + `)\s+(\d+\.\d+\.\d+)\b`)
		lowest, seen := "", [3]int{}
		for _, m := range re.FindAllStringSubmatch(text.String(), -1) {
			if v, ok := parseVersion(m[1]); ok && (lowest == "" || compare(v, seen) < 0) {
				lowest, seen = m[1], v
			}
		}
		rows := Rows(h.ID)
		if lowest == "" || len(rows) == 0 {
			t.Fatalf("%s: the docs record no version of it, or it has no rows", h.ID)
		}
		if rows[0].Lowest != lowest {
			t.Errorf("%s's lowest row starts at %s; the lowest version the docs record is %s", h.ID, rows[0].Lowest, lowest)
		}
		if !strings.Contains(rows[0].Source, "Lowest: the lowest version the repository records eva-md being checked on, "+lowest) {
			t.Errorf("%s's lowest row doesn't say which rule picked its lowest version: %q", h.ID, rows[0].Source)
		}
	}
}

// The quirks v1 matches on every version span all of their host's rows, not
// one version of them.
func TestQuirksV1ReadsEverywhereSpanTheirRows(t *testing.T) {
	for _, id := range []string{"claude-code.marketplace-root-keys", "codex.marketplace-root-keys", "codex.features-list-hooks-row"} {
		var q Quirk
		for _, each := range Quirks() {
			if each.ID == id {
				q = each
			}
		}
		rows := Rows(q.Host)
		if q.ID == "" || len(rows) == 0 || q.Lowest != rows[0].Lowest || q.Highest != rows[len(rows)-1].Highest {
			t.Errorf("quirk %s covers %q to %q, not all of its host's rows", id, q.Lowest, q.Highest)
		}
	}
}

// The hooks row of codex features list, in the quirk's fixture, has the
// form the repository records for the versions before Codex 0.167, "hooks
// experimental <state>", not 0.167's "hooks stable <state>", and the quirk
// checks the middle column. Read as v1 reads it, by its first and last
// column, it gives the state.
func TestCodexHooksRowFixture(t *testing.T) {
	data, err := os.ReadFile(filepath.Join("testdata", "quirks", codexHooksFeature.Fixture))
	if err != nil {
		t.Fatal(err)
	}
	var rows [][]string
	for _, line := range strings.Split(string(data), "\n") {
		if f := strings.Fields(line); len(f) > 0 && f[0] == "hooks" {
			rows = append(rows, f)
		}
	}
	if len(rows) != 1 || len(rows[0]) != 3 || rows[0][1] != "experimental" || rows[0][2] != "true" {
		t.Fatalf("the fixture's hooks rows: %q", rows)
	}
	stable := strings.Replace(string(data), "hooks experimental", "hooks stable", 1)
	if !strings.Contains(string(data), codexHooksFeature.Shows) || strings.Contains(stable, codexHooksFeature.Shows) {
		t.Errorf("the quirk's Shows %q doesn't tell the form before 0.167 from 0.167's", codexHooksFeature.Shows)
	}
}
