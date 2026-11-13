package guard

import (
	"bytes"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"regexp"
	"slices"
	"strconv"
	"strings"
)

// LedgerFile is the size ledger, from the repository's root: what leaves
// the binary, and when. Its format is stated at its top; in short, three
// tables, each known by its header, whose rows each name one path, and a
// last fenced block, info string "ledger", of the end state's figures.
const LedgerFile = "docs/v2/size-ledger.md"

// ledgerTables are the headers of the ledger's three tables: (a) the v1
// paths the plan deletes, (b) the counters release's instruments, (c) the
// merged code that nothing calls yet. The second column is each row's
// figure.
var ledgerTables = map[string][]string{
	"a": {"Path", "Built lines", "Deleted by", "Stage", "Named", "Scheduled on"},
	"b": {"Path", "Built lines", "Instrument", "Added by", "Fate", "Could take it over"},
	"c": {"Path", "Lines", "Built by", "First caller"},
}

// ledgerKeys are the keys of the end state's block, in order.
var ledgerKeys = []string{"counted-at", "built", "leaves-v1", "leaves-instruments", "leaves", "not-yet-called", "end-state", "ceiling"}

// trackOwn are the words of a row deleted by a track's own package rather
// than ST-13 or DV-26.
var trackOwn = []string{"record's own", "store's own", "organizing's own", "recall's own", "hosts' own", "proof's own"}

var (
	separatorCell = regexp.MustCompile(`^:?-{3,}:?$`)
	figure        = regexp.MustCompile(`^[0-9]+$`)
	signedFigure  = regexp.MustCompile(`^-?[0-9]+$`)
	commitPrefix  = regexp.MustCompile(`^[0-9a-f]{12}$`)
)

// A LedgerRow is one row of the ledger's tables.
type LedgerRow struct {
	Table       string  // "a", "b" or "c"
	Path        Pattern // the files the row holds
	Says        int     // the figure the ledger gives
	Counts      int     // today's figure
	ScheduledOn string  // (a): the card whose deletes block names the path, or "none"
	Frozen      bool    // (a): scheduled on a merged card, so its figure may not move
	Leaves      bool    // (b): its fate is to leave at the cut-over
	cells       []string
}

// Line is the row as it reads with today's figure.
func (r LedgerRow) Line() string {
	cells := slices.Clone(r.cells)
	cells[1] = strconv.Itoa(r.Counts)
	return "| " + strings.Join(cells, " | ") + " |"
}

// A LedgerKey is one figure of the end state.
type LedgerKey struct {
	Name   string
	Says   int // the figure the ledger gives
	Counts int // today's figure
}

// A Difference is a figure the ledger gives that is not today's, with the
// line that gives today's.
type Difference struct {
	Text  string // as "row <path>: says N, counts M"
	Write string // the line to write
}

// A Ledger is the ledger as checked.
type Ledger struct {
	Rows        []LedgerRow
	Keys        []LedgerKey // the end state's figures but counted-at, in order
	CountedAt   string
	Differences []Difference
	Faults      []string
}

// Holds reports whether the ledger gives today's figures and has no fault.
func (l Ledger) Holds() bool { return len(l.Differences) == 0 && len(l.Faults) == 0 }

// CheckLedger checks the ledger's text, from the repository at repo, with
// the lines built into eva-md as Built gives them, and v1's files as Count
// takes them; a nil v1 skips the faults that need v1's files.
//
// A row's figure is, in (a) and (b), the built lines of the files under
// its path, v1's or not, and in (c) the newlines of the Go files under it
// that are not tests, not under a testdata folder and not built. leaves is
// leaves-v1 plus leaves-instruments, which counts only the (b) rows that
// leave at the cut-over; end-state is built less leaves plus
// not-yet-called; ceiling is the ceiling given.
//
// A fault is a ledger that is wrong whatever today's count: a malformed
// table or block, a path that does not exist, an (a) row naming no file v1
// had or a (b) row naming one, two rows over one file, a merged card's
// deletes entry with no (a) row of that path scheduled on that card, a row
// scheduled on a card that is not merged or whose block lacks the path, a
// sum or ceiling that does not add up, and a row scheduled on a merged
// card, frozen, whose figure is not today's. Every other figure that is
// not today's is a difference. The error is for an index that cannot be
// read, a folder that cannot be walked, or v1's files.
func CheckLedger(repo, text string, built map[string]int, ceiling int, v1 func() (map[string]bool, error)) (Ledger, error) {
	l := Ledger{}
	keys := l.parse(text)

	merged, blocks, err := mergedDeletes(repo, &l.Faults)
	if err != nil {
		return l, err
	}
	var had map[string]bool
	if v1 != nil {
		if had, err = v1(); err != nil {
			return l, err
		}
	}

	for i := range l.Rows {
		r := &l.Rows[i]
		if r.Table == "c" {
			if r.Counts, err = unbuiltLines(repo, r.Path, built); err != nil {
				return l, err
			}
		} else {
			for f, n := range built {
				if r.Path.Match(f) {
					r.Counts += n
				}
			}
		}
		if !exists(repo, r.Path) {
			l.fault("row %s: the path does not exist", r.Path)
		}
		if had != nil && r.Table != "c" {
			v1s := 0
			for f := range had {
				if r.Path.Match(f) {
					v1s++
				}
			}
			switch {
			case r.Table == "a" && v1s == 0:
				l.fault("row %s: it is in (a) and names no file v1 had", r.Path)
			case r.Table == "b" && v1s > 0:
				l.fault("row %s: it is in (b) and names %d files v1 had", r.Path, v1s)
			}
		}
		if r.ScheduledOn != "none" {
			switch {
			case !slices.Contains(merged, r.ScheduledOn):
				l.fault("row %s: it is scheduled on %s, which the index does not list as merged", r.Path, r.ScheduledOn)
			case !slices.Contains(blocks[r.ScheduledOn], r.Path):
				l.fault("row %s: it is scheduled on %s, whose deletes block does not name it", r.Path, r.ScheduledOn)
			default:
				r.Frozen = true
			}
		}
		for _, q := range l.Rows[:i] {
			if q.Path.covers(r.Path) || r.Path.covers(q.Path) {
				l.fault("rows %s and %s: both hold the same files", q.Path, r.Path)
			}
		}
		if r.Says != r.Counts {
			d := fmt.Sprintf("row %s: says %d, counts %d", r.Path, r.Says, r.Counts)
			if r.Frozen {
				l.fault("%s, and the row is frozen: %s's deletes block has scheduled it", d, r.ScheduledOn)
			} else {
				l.Differences = append(l.Differences, Difference{d, r.Line()})
			}
		}
	}
	for _, card := range merged {
		for _, p := range blocks[card] {
			if !slices.ContainsFunc(l.Rows, func(r LedgerRow) bool {
				return r.Table == "a" && r.Path == p && r.ScheduledOn == card
			}) {
				l.fault("%s's deletes block names %s, and no (a) row of that path is scheduled on %s", card, p, card)
			}
		}
	}

	// The sums: says holds what the ledger's own figures add up to, today
	// what today's count gives.
	says := map[string]int{"leaves-v1": 0, "leaves-instruments": 0, "not-yet-called": 0, "ceiling": ceiling}
	today := map[string]int{"ceiling": ceiling}
	for _, n := range built {
		today["built"] += n
	}
	for _, r := range l.Rows {
		switch {
		case r.Table == "a":
			says["leaves-v1"] += r.Says
			today["leaves-v1"] += r.Counts
		case r.Table == "b" && r.Leaves:
			says["leaves-instruments"] += r.Says
			today["leaves-instruments"] += r.Counts
		case r.Table == "c":
			says["not-yet-called"] += r.Says
			today["not-yet-called"] += r.Counts
		}
	}
	today["leaves"] = today["leaves-v1"] + today["leaves-instruments"]
	today["end-state"] = today["built"] - today["leaves"] + today["not-yet-called"]
	given := func(k ...string) bool {
		return !slices.ContainsFunc(k, func(k string) bool { _, ok := keys[k]; return !ok })
	}
	if given("leaves-v1", "leaves-instruments") {
		says["leaves"] = keys["leaves-v1"] + keys["leaves-instruments"]
	}
	if given("built", "leaves", "not-yet-called") {
		says["end-state"] = keys["built"] - keys["leaves"] + keys["not-yet-called"]
	}
	for _, k := range ledgerKeys[1:] {
		n, ok := keys[k]
		if !ok {
			continue
		}
		if want, summed := says[k]; summed && n != want {
			l.fault("%s says %d, and the figures it sums give %d", k, n, want)
		}
		l.Keys = append(l.Keys, LedgerKey{k, n, today[k]})
		if k != "ceiling" && n != today[k] {
			l.Differences = append(l.Differences, Difference{fmt.Sprintf("key %s: says %d, counts %d", k, n, today[k]), fmt.Sprintf("%s: %d", k, today[k])})
		}
	}
	return l, nil
}

func (l *Ledger) fault(format string, args ...any) {
	l.Faults = append(l.Faults, fmt.Sprintf(format, args...))
}

// parse reads the tables and the end state's block into l, with a fault
// for each thing malformed, and returns the block's figures but
// counted-at, by key.
func (l *Ledger) parse(text string) map[string]int {
	tables := map[string]int{}
	kind := ""     // the table being read: a, b, c, "?" for one not the ledger's, "" outside one
	header := 0    // the line of the table's header
	open := 0      // the open fence's backticks, 0 outside one
	blockAt := 0   // the line of the end state's block
	lastTable := 0 // the last line of a table
	for i, line := range strings.Split(text, "\n") {
		n := i + 1
		trimmed := strings.TrimSpace(line)
		ticks := len(trimmed) - len(strings.TrimLeft(trimmed, "`"))
		switch {
		case open == 0 && ticks >= 3:
			open = ticks
			if strings.TrimSpace(trimmed[ticks:]) == "ledger" {
				if blockAt > 0 {
					l.fault("line %d: a second block of the end state; the ledger has one", n)
				}
				blockAt = n
			}
			kind = ""
			continue
		case open > 0:
			if ticks >= open && ticks == len(trimmed) {
				open = 0
			}
			continue
		case !strings.HasPrefix(trimmed, "|"):
			kind = ""
			continue
		}
		lastTable = n
		cells, ok := tableCells(trimmed)
		switch {
		case kind == "":
			kind, header = "?", n
			for k, h := range ledgerTables {
				if ok && slices.Equal(cells, h) {
					kind = k
				}
			}
			if kind == "?" {
				l.fault("line %d: a table whose header is not one of the ledger's three", n)
			} else if tables[kind]++; tables[kind] == 2 {
				l.fault("line %d: a second (%s) table; the ledger has one", n, kind)
			}
		case kind == "?":
		case n == header+1:
			if !ok || len(cells) != len(ledgerTables[kind]) || slices.ContainsFunc(cells, func(c string) bool { return !separatorCell.MatchString(c) }) {
				l.fault("line %d: the (%s) table's header is not followed by its separator line", n, kind)
			}
		case !ok:
			l.fault("line %d: a row that does not end with |", n)
		default:
			r, ok, err := parseRow(kind, cells)
			if err != nil {
				l.fault("line %d: %v", n, err)
			}
			if ok {
				l.Rows = append(l.Rows, r)
			}
		}
	}
	for _, k := range []string{"a", "b", "c"} {
		if tables[k] == 0 {
			l.fault("the ledger has no (%s) table", k)
		}
	}
	if blockAt > 0 && lastTable > blockAt {
		l.fault("line %d: a table after the end state's block, which comes last", lastTable)
	}

	entries, found, err := Block(text, "ledger")
	switch {
	case err != nil:
		l.fault("the ledger: %v", err)
		return nil
	case !found:
		l.fault("the ledger has no block of its end state, a fenced block whose info string is ledger")
		return nil
	}
	keys := map[string]int{}
	seen := map[string]bool{}
	for _, e := range entries {
		k, v, ok := strings.Cut(e, ":")
		k, v = strings.TrimSpace(k), strings.TrimSpace(v)
		switch {
		case !ok || !slices.Contains(ledgerKeys, k):
			l.fault("the end state's line %q is not one of its keys", e)
			continue
		case seen[k]:
			l.fault("the end state gives %s twice", k)
			continue
		}
		seen[k] = true
		switch {
		case k == "counted-at" && commitPrefix.MatchString(v):
			l.CountedAt = v
		case k == "counted-at":
			l.fault("the end state's counted-at, %q, is not a commit's first 12 hexadecimal digits", v)
		case signedFigure.MatchString(v):
			keys[k], _ = strconv.Atoi(v)
		default:
			l.fault("the end state's %s, %q, is not a whole number", k, v)
		}
	}
	for _, k := range ledgerKeys {
		if !seen[k] {
			l.fault("the end state does not give %s", k)
		}
	}
	return keys
}

// tableCells splits a table's line into its cells, trimmed; ok is false
// for a line that does not end with "|".
func tableCells(line string) ([]string, bool) {
	inner, ok := strings.CutSuffix(strings.TrimPrefix(line, "|"), "|")
	if !ok {
		return nil, false
	}
	cells := strings.Split(inner, "|")
	for i, c := range cells {
		cells[i] = strings.TrimSpace(c)
	}
	return cells, true
}

// parseRow reads a row of the table kind. ok is true when its path and
// figure could be read, so the row is checked and counted even if err
// says what else is wrong with it.
func parseRow(kind string, cells []string) (r LedgerRow, ok bool, err error) {
	r = LedgerRow{Table: kind, cells: cells, ScheduledOn: "none"}
	if len(cells) != len(ledgerTables[kind]) {
		return r, false, fmt.Errorf("a row of %d cells in the (%s) table, which has %d", len(cells), kind, len(ledgerTables[kind]))
	}
	inner, opened := strings.CutPrefix(cells[0], "`")
	inner, closed := strings.CutSuffix(inner, "`")
	if !opened || !closed {
		return r, false, fmt.Errorf("the row's path, %s, is not in backticks", cells[0])
	}
	p, err := ParsePattern(inner, false)
	if err != nil {
		return r, false, fmt.Errorf("the row's path: %w", err)
	}
	r.Path = p
	if !figure.MatchString(cells[1]) {
		return r, false, fmt.Errorf("row %s: its figure, %q, is not a whole number", p, cells[1])
	}
	r.Says, _ = strconv.Atoi(cells[1])
	bad := func(column, why string) (LedgerRow, bool, error) {
		return r, true, fmt.Errorf("row %s: its %s, %q, %s", p, column, cells[slices.Index(ledgerTables[kind], column)], why)
	}
	isCard := func(s string) bool { return CardFile(s) != "" }
	switch kind {
	case "a":
		if isCard(cells[5]) {
			r.ScheduledOn = cells[5]
		}
		switch {
		case !isCard(cells[2]) && !slices.Contains(trackOwn, cells[2]):
			return bad("Deleted by", "is neither a plan id nor a track's own, such as hosts' own")
		case !isCard(cells[3]):
			return bad("Stage", "is not a plan id")
		case cells[4] != "file" && cells[4] != "loose":
			return bad("Named", "is neither file nor loose")
		case cells[5] != "none" && !isCard(cells[5]):
			return bad("Scheduled on", "is neither a card's id nor none")
		}
	case "b":
		kept, isKept := strings.CutPrefix(cells[4], "kept by ")
		r.Leaves = cells[4] == "leaves at "+CutOver
		switch {
		case cells[2] == "" || cells[3] == "" || cells[5] == "":
			return r, true, fmt.Errorf("row %s: a cell is empty", p)
		case !r.Leaves && !(isKept && isCard(kept)):
			return bad("Fate", "is neither leaves at "+CutOver+" nor kept by a plan id")
		}
	case "c":
		switch {
		case cells[2] == "":
			return r, true, fmt.Errorf("row %s: a cell is empty", p)
		case !isCard(cells[3]):
			return bad("First caller", "is not a plan id")
		}
	}
	return r, true, nil
}

// mergedDeletes reads the cards the index lists as merged and the entries
// of their deletes blocks, by card, adding a fault for a card that cannot
// be read, as Count does. With no index nothing is merged.
func mergedDeletes(repo string, faults *[]string) ([]string, map[string][]Pattern, error) {
	index, err := os.ReadFile(filepath.Join(repo, filepath.FromSlash(CardsDir), "README.md"))
	if errors.Is(err, fs.ErrNotExist) {
		return nil, nil, nil
	} else if err != nil {
		return nil, nil, fmt.Errorf("guard: %w", err)
	}
	merged := Merged(string(index))
	blocks := map[string][]Pattern{}
	for _, card := range merged {
		file := CardFile(card)
		if file == "" {
			*faults = append(*faults, fmt.Sprintf("the index lists %q as merged, which is not a card's id", card))
			continue
		}
		text, err := os.ReadFile(filepath.Join(repo, filepath.FromSlash(file)))
		if err != nil {
			*faults = append(*faults, fmt.Sprintf("%s: the index lists it as merged and %s cannot be read", card, file))
			continue
		}
		ps, _, err := Patterns(string(text), "deletes")
		if err != nil {
			*faults = append(*faults, card+": "+err.Error())
			continue
		}
		blocks[card] = ps
	}
	return merged, blocks, nil
}

// exists reports whether the path a row names is in the repository: a
// folder for a "/**" entry, a file for any other.
func exists(repo string, p Pattern) bool {
	name, folder := p.folder()
	if !folder {
		name = string(p)
	}
	info, err := os.Lstat(filepath.Join(repo, filepath.FromSlash(name)))
	return err == nil && info.IsDir() == folder
}

// unbuiltLines counts the newlines of the Go files under p that are not
// tests, not under a testdata folder and not built.
func unbuiltLines(repo string, p Pattern, built map[string]int) (int, error) {
	start, folder := p.folder()
	if !folder {
		start = string(p)
	}
	total := 0
	err := filepath.WalkDir(filepath.Join(repo, filepath.FromSlash(start)), func(file string, d fs.DirEntry, err error) error {
		if errors.Is(err, fs.ErrNotExist) {
			return nil // a path that does not exist is a fault of its own
		} else if err != nil {
			return err
		}
		rel, err := filepath.Rel(repo, file)
		if err != nil {
			return err
		}
		rel = filepath.ToSlash(rel)
		if d.IsDir() || !d.Type().IsRegular() || !strings.HasSuffix(rel, ".go") || strings.HasSuffix(rel, "_test.go") ||
			slices.Contains(strings.Split(path.Dir(rel), "/"), "testdata") || !p.Match(rel) {
			return nil
		}
		if _, ok := built[rel]; ok {
			return nil
		}
		data, err := os.ReadFile(file)
		if err != nil {
			return err
		}
		total += bytes.Count(data, []byte("\n"))
		return nil
	})
	if err != nil {
		return 0, fmt.Errorf("guard: %w", err)
	}
	return total, nil
}

// Report writes each row and figure with today's count, then the
// differences, each with the line to write, and the faults.
func (l Ledger) Report() string {
	var b strings.Builder
	for _, r := range l.Rows {
		note := ""
		switch {
		case r.Frozen:
			note = " (frozen: scheduled on " + r.ScheduledOn + ")"
		case r.Table == "b" && r.Leaves:
			note = " (leaves at " + CutOver + ")"
		case r.Table == "b":
			note = " (kept)"
		}
		fmt.Fprintf(&b, "(%s) %s: %d%s\n", r.Table, r.Path, r.Counts, note)
	}
	for _, k := range l.Keys {
		fmt.Fprintf(&b, "%s: %d\n", k.Name, k.Counts)
	}
	for _, d := range l.Differences {
		fmt.Fprintf(&b, "Difference: %s\n  Write: %s\n", d.Text, d.Write)
	}
	for _, f := range l.Faults {
		fmt.Fprintf(&b, "Fault: %s\n", f)
	}
	if l.Holds() {
		b.WriteString("The ledger gives today's figures and has no fault.\n")
	} else {
		fmt.Fprintf(&b, "The ledger has %d differences and %d faults.\n", len(l.Differences), len(l.Faults))
	}
	return b.String()
}
