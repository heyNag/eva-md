package spec

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"sort"
	"strings"
	"testing"
)

// The audit's register, whose 17 parts each end with a list of what v1 must
// keep, and the least the check reads of it. The ids are computed and never
// written there: an item of the part whose findings are numbered PRD-01 on
// is PRD-K01, PRD-K02 and so on, in its list's order.
const (
	findings  = "docs/v2/findings.md"
	keepParts = 17
	keepItems = 291
)

// The limits of a Must keep table's cells.
const maxItem = 200

// An item that tests hold only in part says, at the end of its Item cell,
// what of it no test holds, as "(untested: <what>)". The check counts it
// as held in part, so the missing half stays on the list of later work.
var partMark = regexp.MustCompile(` \(untested: [^()]+\)$`)

// TestMain sets up a package's tests; it runs none, so it holds no item.
const setUp = "TestMain"

var (
	findingRow = regexp.MustCompile(`^\| ([A-Z]+)-[0-9]+ \|`)
	keepRow    = regexp.MustCompile(`^\| ([A-Z]+-K[0-9]{2,}) \| ([^|]+) \| ([^|]+) \|$`)
	heldBy     = regexp.MustCompile(`^((?:cmd|internal)(?:/[a-z0-9_-]+)+):((?:Test|Benchmark|Fuzz)[A-Za-z0-9_]*)$`)
	// A check that is no Go test, such as the benchmark, stands in a cell
	// as check:<path>, its file's path from the repository's root, which a
	// workflow's run step names.
	heldByCheck = regexp.MustCompile(`^check:((?:scripts|cmd|internal)(?:/[A-Za-z0-9_-][A-Za-z0-9_.-]*)+)$`)
	testFunc    = regexp.MustCompile(`(?m)^func ((?:Test|Benchmark|Fuzz)[A-Za-z0-9_]*)\(`)
)

const (
	keepHeading = "## Must keep"
	keepHead    = "| Id | Item | Tests |"
	keepRule    = "| --- | --- | --- |"
	noTest      = "none yet"
)

// keepList is one part's list of what must be kept.
type keepList struct {
	prefix string
	ids    []string
}

// keepLists reads the must-keep lists of the register's text, in their
// order, and the problems with its shape.
func keepLists(text string) ([]keepList, []string) {
	var lists []keepList
	var problems []string
	prefix, open, seen := "", false, map[string]bool{}
	for i, l := range strings.Split(text, "\n") {
		switch {
		case strings.HasPrefix(l, "### "):
			if open {
				problems = append(problems, fmt.Sprintf("%s:%d: a part starts inside a must-keep list", findings, i+1))
				open = false
			}
			prefix = ""
		case prefix == "" && findingRow.MatchString(l):
			prefix = findingRow.FindStringSubmatch(l)[1]
		case strings.HasPrefix(l, "<details><summary>Must keep"):
			if prefix == "" {
				problems = append(problems, fmt.Sprintf("%s:%d: a must-keep list whose part numbers no finding", findings, i+1))
			} else if seen[prefix] {
				problems = append(problems, fmt.Sprintf("%s:%d: a second must-keep list for %s", findings, i+1, prefix))
			}
			seen[prefix] = true
			lists = append(lists, keepList{prefix: prefix})
			open = true
		case open && strings.HasPrefix(l, "</details>"):
			open = false
		case open && strings.HasPrefix(l, "- "):
			k := &lists[len(lists)-1]
			k.ids = append(k.ids, fmt.Sprintf("%s-K%02d", k.prefix, len(k.ids)+1))
		}
	}
	if open {
		problems = append(problems, findings+": its last must-keep list is never closed")
	}
	items := 0
	for _, k := range lists {
		items += len(k.ids)
	}
	if len(lists) < keepParts || items < keepItems {
		problems = append(problems, fmt.Sprintf("%s: read %d must-keep lists and %d items, fewer than the %d and %d it holds", findings, len(lists), items, keepParts, keepItems))
	}
	return lists, problems
}

// keepEntry is one row of a spec's Must keep table.
type keepEntry struct {
	file, id, item string
	line           int
	tests          []string // as <package path>:<TestName>; none for "none yet"
	checks         []string // the paths of the checks that are no Go test
}

// held says whether a test or a check holds the row's item.
func (e keepEntry) held() bool { return len(e.tests)+len(e.checks) > 0 }

// cell is the row's tests and checks as its cell names them.
func (e keepEntry) cell() string {
	cell := slices.Clone(e.tests)
	for _, c := range e.checks {
		cell = append(cell, "check:"+c)
	}
	return strings.Join(cell, ", ")
}

// part says whether the row's item says what of it no test holds.
func (e keepEntry) part() bool { return partMark.MatchString(e.item) }

// keepSection gives the rows of a spec's Must keep section, and the
// problems with its place and its rows. A spec that holds v1's blocks has
// one such section, just before its Since the split.
func keepSection(d doc) ([]keepEntry, []string) {
	lines := strings.Split(d.text, "\n")
	var problems []string
	at := -1
	for i, l := range lines {
		if l == keepHeading {
			if at >= 0 {
				problems = append(problems, fmt.Sprintf("%s:%d: a second %s section", d.name, i+1, keepHeading))
				continue
			}
			at = i
		}
	}
	if at < 0 {
		if len(d.blocks) > 0 {
			problems = append(problems, fmt.Sprintf("%s: it has no %s section before its Since the split", d.name, keepHeading))
		}
		return nil, problems
	}
	end := len(lines)
	for i := at + 1; i < len(lines); i++ {
		if strings.HasPrefix(lines[i], "## ") || strings.HasPrefix(lines[i], "# ") {
			end = i
			break
		}
	}
	if end == len(lines) || lines[end] != "## Since the split" {
		problems = append(problems, fmt.Sprintf("%s:%d: its %s section is not just before its Since the split", d.name, at+1, keepHeading))
	}
	var rows []keepEntry
	table := 0 // 0 before the head, 1 after the head, 2 after the rule
	for i := at + 1; i < end; i++ {
		l := lines[i]
		if !strings.HasPrefix(strings.TrimSpace(l), "|") {
			continue
		}
		where := fmt.Sprintf("%s:%d", d.name, i+1)
		switch {
		case table == 0 && l == keepHead:
			table = 1
			continue
		case table == 1 && l == keepRule:
			table = 2
			continue
		case table < 2:
			problems = append(problems, fmt.Sprintf("%s: a row before the table's head, %q, and its rule", where, keepHead))
			continue
		}
		m := keepRow.FindStringSubmatch(l)
		if m == nil {
			problems = append(problems, fmt.Sprintf("%s: a malformed row: a row is | <id> | <item> | <tests> |", where))
			continue
		}
		item, cell := strings.TrimSpace(m[2]), strings.TrimSpace(m[3])
		if item == "" || len([]rune(item)) > maxItem {
			problems = append(problems, fmt.Sprintf("%s: the item of %s is empty or over %d characters", where, m[1], maxItem))
		}
		e := keepEntry{file: d.name, id: m[1], item: item, line: i + 1}
		if cell == noTest && e.part() {
			problems = append(problems, fmt.Sprintf("%s: %s says what of it no test holds, but its tests cell is %q", where, m[1], noTest))
		}
		if cell != noTest {
			named := map[string]bool{}
			for _, t := range strings.Split(cell, ", ") {
				switch {
				case heldByCheck.MatchString(t) && !named[t]:
					e.checks = append(e.checks, heldByCheck.FindStringSubmatch(t)[1])
				case !heldBy.MatchString(t) && !heldByCheck.MatchString(t):
					problems = append(problems, fmt.Sprintf("%s: %s names %q, which is not <package path>:<TestName> or check:<path>, and its cell is not %q", where, m[1], t, noTest))
				case strings.HasSuffix(t, ":"+setUp):
					problems = append(problems, fmt.Sprintf("%s: %s names %s, which sets up its package's tests and holds no item", where, m[1], t))
				case named[t]:
					problems = append(problems, fmt.Sprintf("%s: %s names %s twice", where, m[1], t))
				default:
					e.tests = append(e.tests, t)
				}
				named[t] = true
			}
		}
		rows = append(rows, e)
	}
	if table == 1 {
		problems = append(problems, fmt.Sprintf("%s:%d: its table's head has no rule after it", d.name, at+1))
	}
	return rows, problems
}

// testsOf names the tests, benchmarks and fuzz tests the _test.go files of
// one package folder define, or nil when it holds none.
func testsOf(root, pkg string) map[string]bool {
	files, _ := filepath.Glob(filepath.Join(root, filepath.FromSlash(pkg), "*_test.go"))
	var names map[string]bool
	for _, f := range files {
		data, err := os.ReadFile(f)
		if err != nil {
			continue
		}
		for _, m := range testFunc.FindAllStringSubmatch(string(data), -1) {
			if names == nil {
				names = map[string]bool{}
			}
			names[m[1]] = true
		}
	}
	return names
}

// workflows holds the workflows whose run steps run a check.
const workflows = ".github/workflows"

// runStep is a line that opens a step's run, inline or as a block.
var runStep = regexp.MustCompile(`^(\s*)(?:- )?run:\s*(.*)$`)

// runByWorkflow says whether a run step of a workflow at root names path,
// as a word of its command, inline or in its block; a comment does not
// count.
func runByWorkflow(root, path string) bool {
	files, _ := filepath.Glob(filepath.Join(root, filepath.FromSlash(workflows), "*.yml"))
	word := regexp.MustCompile(`(^|[\s"'=])` + regexp.QuoteMeta(path) + `($|[\s"';|&)])`)
	for _, f := range files {
		data, err := os.ReadFile(f)
		if err != nil {
			continue
		}
		block := -1 // the indent of the run key whose block the lines are in
		for _, l := range strings.Split(string(data), "\n") {
			indent := len(l) - len(strings.TrimLeft(l, " "))
			if block >= 0 && strings.TrimSpace(l) != "" && indent <= block {
				block = -1
			}
			text := ""
			switch m := runStep.FindStringSubmatch(l); {
			case block >= 0:
				text = l
			case m != nil && strings.HasPrefix(m[2], "|") || m != nil && strings.HasPrefix(m[2], ">"):
				block = len(m[1])
			case m != nil:
				text = m[2]
			}
			if text = strings.TrimSpace(text); !strings.HasPrefix(text, "#") && word.MatchString(text) {
				return true
			}
		}
	}
	return false
}

// mustKeep is what the check read: each list, each spec's rows.
type mustKeep struct {
	lists []keepList
	rows  []keepEntry
}

// readMustKeep reads the register and every spec's Must keep table at
// root, and the problems with either.
func readMustKeep(root string) (mustKeep, []string) {
	var mk mustKeep
	data, err := os.ReadFile(filepath.Join(root, filepath.FromSlash(findings)))
	if err != nil {
		return mk, []string{err.Error()}
	}
	lists, problems := keepLists(string(data))
	mk.lists = lists
	t, p := load(root)
	problems = append(problems, p...)
	for _, d := range t.specs {
		rows, p := keepSection(d)
		mk.rows = append(mk.rows, rows...)
		problems = append(problems, p...)
	}
	return mk, problems
}

// checkMustKeep holds the specs' Must keep tables at root to the register
// and to the tests: every item sits in a spec, every id a spec names is
// the register's, once a spec, and every test it names is defined by a
// _test.go file of the package it names. An item sitting in more than one
// spec reads the same in each, tests included.
func checkMustKeep(root string) []string {
	mk, problems := readMustKeep(root)
	known := map[string]bool{}
	for _, k := range mk.lists {
		for _, id := range k.ids {
			known[id] = true
		}
	}
	first := map[string]keepEntry{}
	placed := map[string]bool{}
	inFile := map[string]bool{}
	defined := map[string]map[string]bool{}
	run := map[string]bool{}
	for _, e := range mk.rows {
		where := fmt.Sprintf("%s:%d", e.file, e.line)
		if !known[e.id] {
			problems = append(problems, fmt.Sprintf("%s: %s names %s, which %s does not have", where, e.file, e.id, findings))
		}
		if inFile[e.file+" "+e.id] {
			problems = append(problems, fmt.Sprintf("%s: %s lists %s twice", where, e.file, e.id))
		}
		inFile[e.file+" "+e.id] = true
		if f, ok := first[e.id]; !ok {
			first[e.id] = e
		} else if f.item != e.item || f.cell() != e.cell() {
			problems = append(problems, fmt.Sprintf("%s: %s reads differently from its row at %s:%d; an item reads the same in every spec", where, e.id, f.file, f.line))
		}
		placed[e.id] = true
		for _, t := range e.tests {
			pkg, name, _ := strings.Cut(t, ":")
			if _, ok := defined[pkg]; !ok {
				defined[pkg] = testsOf(root, pkg)
			}
			switch {
			case defined[pkg] == nil:
				problems = append(problems, fmt.Sprintf("%s: %s names %s, but %s holds no test file", where, e.id, t, pkg))
			case !defined[pkg][name]:
				problems = append(problems, fmt.Sprintf("%s: %s names %s, which no _test.go file of %s defines", where, e.id, t, pkg))
			}
		}
		for _, c := range e.checks {
			if _, ok := run[c]; !ok {
				run[c] = runByWorkflow(root, c)
			}
			if info, err := os.Stat(filepath.Join(root, filepath.FromSlash(c))); err != nil || !info.Mode().IsRegular() {
				problems = append(problems, fmt.Sprintf("%s: %s names check:%s, which is no file of the repository", where, e.id, c))
			} else if !run[c] {
				problems = append(problems, fmt.Sprintf("%s: %s names check:%s, which no run step of a workflow in %s runs", where, e.id, c, workflows))
			}
		}
	}
	for _, k := range mk.lists {
		for _, id := range k.ids {
			if !placed[id] {
				problems = append(problems, fmt.Sprintf("%s sits in no spec's %s table", id, keepHeading))
			}
		}
	}
	return problems
}

// Every must-keep item of the register sits in a spec's table with the
// tests that hold it, and every test named there exists. The log gives,
// for each part, how many items tests hold wholly, the ids they hold only
// in part and the ids no test holds yet.
func TestEachMustKeepItemSitsInASpecWithItsTests(t *testing.T) {
	root := repoRoot(t)
	for _, p := range checkMustKeep(root) {
		t.Error(p)
	}
	mk, _ := readMustKeep(root)
	held, part := map[string]bool{}, map[string]bool{}
	for _, e := range mk.rows {
		if e.held() {
			held[e.id] = true
			part[e.id] = part[e.id] || e.part()
		}
	}
	items, whole, parts := 0, 0, 0
	for _, k := range mk.lists {
		var some, none []string
		for _, id := range k.ids {
			switch {
			case !held[id]:
				none = append(none, id)
			case part[id]:
				some = append(some, id)
			}
		}
		items += len(k.ids)
		whole += len(k.ids) - len(some) - len(none)
		parts += len(some)
		t.Logf("%s: %d of %d items held wholly by tests; in part: %s; none yet: %s", k.prefix, len(k.ids)-len(some)-len(none), len(k.ids), strings.Join(some, ", "), strings.Join(none, ", "))
	}
	t.Logf("%d of %d items held wholly by tests, %d more in part, %d by none yet", whole, items, parts, items-whole-parts)
	if len(mk.lists) != keepParts || items != keepItems {
		t.Errorf("read %d lists and %d items; the register holds %d and %d", len(mk.lists), items, keepParts, keepItems)
	}
}

// copyMustKeep copies what the check reads into a folder of the test's
// own: the specs, the register, the test files of every package the tables
// name, the checks they name and the workflows, so the copy passes as the
// repository does.
func copyMustKeep(t *testing.T) string {
	t.Helper()
	root, dst := repoRoot(t), copyTree(t)
	if _, err := os.Stat(filepath.Join(dst, filepath.FromSlash(findings))); err != nil {
		copyFile(t, root, dst, findings)
	}
	mk, _ := readMustKeep(root)
	pkgs := map[string]bool{}
	for _, e := range mk.rows {
		for _, test := range e.tests {
			pkg, _, _ := strings.Cut(test, ":")
			pkgs[pkg] = true
		}
		for _, c := range e.checks {
			copyFile(t, root, dst, c)
		}
	}
	files, _ := filepath.Glob(filepath.Join(root, filepath.FromSlash(workflows), "*.yml"))
	for _, f := range files {
		copyFile(t, root, dst, workflows+"/"+filepath.Base(f))
	}
	for pkg := range pkgs {
		files, _ := filepath.Glob(filepath.Join(root, filepath.FromSlash(pkg), "*_test.go"))
		for _, f := range files {
			rel, err := filepath.Rel(root, f)
			if err != nil {
				t.Fatal(err)
			}
			copyFile(t, root, dst, filepath.ToSlash(rel))
		}
	}
	if problems := checkMustKeep(dst); len(problems) > 0 {
		t.Fatalf("the copy fails before any fault is seeded: %q", problems)
	}
	return dst
}

// rowOf returns the first row of a spec at dir that names id, and its text.
func rowOf(t *testing.T, dir, id string) (keepEntry, string) {
	t.Helper()
	mk, _ := readMustKeep(dir)
	for _, e := range mk.rows {
		if e.id == id {
			return e, strings.Split(read(t, dir, e.file), "\n")[e.line-1]
		}
	}
	t.Fatalf("no spec names %s", id)
	return keepEntry{}, ""
}

// A fault seeded in a copy fails the check with a problem that names it,
// and with no problem the case does not expect.
func TestEachMustKeepFaultIsCaught(t *testing.T) {
	cases := []struct {
		name string
		seed func(t *testing.T, dir string)
		want []string
	}{
		{"an id removed from every spec", func(t *testing.T, dir string) {
			mk, _ := readMustKeep(dir)
			for _, e := range mk.rows {
				if e.id == "CAP-K05" {
					lines := strings.Split(read(t, dir, e.file), "\n")
					lines = append(lines[:e.line-1], lines[e.line:]...)
					write(t, dir, e.file, strings.Join(lines, "\n"))
				}
			}
		}, []string{`^CAP-K05 sits in no spec's ## Must keep table$`}},
		{"an id findings.md does not have", func(t *testing.T, dir string) {
			e, row := rowOf(t, dir, "RED-K13")
			edit(t, dir, e.file, row, row+"\n"+strings.Replace(row, "RED-K13", "RED-K14", 1))
		}, []string{`^docs/spec/[a-z-]+\.md:[0-9]+: docs/spec/[a-z-]+\.md names RED-K14, which docs/v2/findings.md does not have$`}},
		{"a test that does not exist", func(t *testing.T, dir string) {
			e, row := rowOf(t, dir, "CAP-K07")
			edit(t, dir, e.file, row, strings.Replace(row, "internal/hook:TestToolLines", "internal/hook:TestToolLinesKeepNothing", 1))
		}, []string{`: CAP-K07 names internal/hook:TestToolLinesKeepNothing, which no _test.go file of internal/hook defines$`}},
		{"a test named in the wrong package", func(t *testing.T, dir string) {
			e, row := rowOf(t, dir, "CAP-K07")
			edit(t, dir, e.file, row, strings.Replace(row, "internal/hook:TestToolLines", "internal/inbox:TestToolLines", 1))
		}, []string{`: CAP-K07 names internal/inbox:TestToolLines, which no _test.go file of internal/inbox defines$`}},
		{"a test in a package with no test file", func(t *testing.T, dir string) {
			e, row := rowOf(t, dir, "CAP-K07")
			edit(t, dir, e.file, row, strings.Replace(row, "internal/hook:TestToolLines", "internal/nowhere:TestToolLines", 1))
		}, []string{`: CAP-K07 names internal/nowhere:TestToolLines, but internal/nowhere holds no test file$`}},
		{"a test deleted from its package", func(t *testing.T, dir string) {
			name := "internal/redact/values_test.go"
			text := read(t, dir, name)
			if !strings.Contains(text, "\nfunc TestValueMarkersAreOpaque(") {
				t.Fatalf("%s doesn't define TestValueMarkersAreOpaque", name)
			}
			write(t, dir, name, strings.Replace(text, "\nfunc TestValueMarkersAreOpaque(", "\nfunc TestValueMarkersStayOpaque(", 1))
		}, []string{`: RED-K05 names internal/redact:TestValueMarkersAreOpaque, which no _test.go file of internal/redact defines$`}},
		{"a findings copy cut by one item", func(t *testing.T, dir string) {
			// The second item of the redaction list goes; its last, RED-K13,
			// is no longer the register's.
			text := read(t, dir, findings)
			i := strings.Index(text, "- Fail closed: a redactor panic stores no text")
			if i < 0 {
				t.Fatalf("%s doesn't hold RED-K02", findings)
			}
			j := strings.Index(text[i:], "\n")
			write(t, dir, findings, text[:i]+text[i+j+1:])
		}, []string{
			`^docs/v2/findings.md: read 17 must-keep lists and 290 items, fewer than the 17 and 291 it holds$`,
			`names RED-K13, which docs/v2/findings.md does not have$`,
		}},
		{"a findings copy cut by one list", func(t *testing.T, dir string) {
			text := read(t, dir, findings)
			i := strings.LastIndex(text, "<details><summary>Must keep")
			j := strings.Index(text[i:], "</details>")
			write(t, dir, findings, text[:i]+text[i+j+len("</details>"):])
		}, []string{
			`^docs/v2/findings.md: read 16 must-keep lists and 276 items, fewer than the 17 and 291 it holds$`,
			`names EVL-K[0-9]{2}, which docs/v2/findings.md does not have$`,
		}},
		{"a malformed row", func(t *testing.T, dir string) {
			e, row := rowOf(t, dir, "OKF-K12")
			edit(t, dir, e.file, row, strings.TrimSuffix(row, " |"))
		}, []string{`^docs/spec/okf-adapter.md:[0-9]+: a malformed row`, `^OKF-K12 sits in no spec's`}},
		{"a cell that names no test", func(t *testing.T, dir string) {
			e, row := rowOf(t, dir, "OKF-K12")
			edit(t, dir, e.file, row, strings.Replace(row, "internal/okfadapter:TestActorAndIndexLine", "TestActorAndIndexLine", 1))
		}, []string{`: OKF-K12 names "TestActorAndIndexLine", which is not <package path>:<TestName>`}},
		{"a test named twice in a row", func(t *testing.T, dir string) {
			e, row := rowOf(t, dir, "OKF-K12")
			edit(t, dir, e.file, row, strings.Replace(row, "internal/okfadapter:TestActorAndIndexLine", "internal/okfadapter:TestActorAndIndexLine, internal/okfadapter:TestActorAndIndexLine", 1))
		}, []string{`: OKF-K12 names internal/okfadapter:TestActorAndIndexLine twice$`}},
		{"an id twice in one spec", func(t *testing.T, dir string) {
			e, row := rowOf(t, dir, "OKF-K12")
			edit(t, dir, e.file, row, row+"\n"+row)
		}, []string{`^docs/spec/okf-adapter.md:[0-9]+: docs/spec/okf-adapter.md lists OKF-K12 twice$`}},
		{"a row before the table's head", func(t *testing.T, dir string) {
			e, row := rowOf(t, dir, "OKF-K12")
			edit(t, dir, e.file, keepHead+"\n"+keepRule+"\n", row+"\n"+keepHead+"\n"+keepRule+"\n")
		}, []string{`^docs/spec/okf-adapter.md:[0-9]+: a row before the table's head`}},
		{"a test that only sets up the package's tests", func(t *testing.T, dir string) {
			e, row := rowOf(t, dir, "CAP-K07")
			edit(t, dir, e.file, row, strings.Replace(row, "internal/hook:TestToolLines", "internal/hook:TestMain", 1))
		}, []string{`: CAP-K07 names internal/hook:TestMain, which sets up its package's tests and holds no item$`}},
		{"an item over the limit", func(t *testing.T, dir string) {
			e, row := rowOf(t, dir, "OKF-K12")
			edit(t, dir, e.file, row, "| OKF-K12 | "+strings.Repeat("x", maxItem+1)+" | "+strings.Join(e.tests, ", ")+" |")
		}, []string{`^docs/spec/okf-adapter.md:[0-9]+: the item of OKF-K12 is empty or over 200 characters$`}},
		{"a blank item", func(t *testing.T, dir string) {
			e, row := rowOf(t, dir, "OKF-K12")
			edit(t, dir, e.file, row, "| OKF-K12 |   | "+strings.Join(e.tests, ", ")+" |")
		}, []string{`^docs/spec/okf-adapter.md:[0-9]+: the item of OKF-K12 is empty or over 200 characters$`}},
		{"an untested part on a row no test holds", func(t *testing.T, dir string) {
			e, row := rowOf(t, dir, "CAP-K05")
			edit(t, dir, e.file, row, strings.Replace(row, " | none yet |", " (untested: all of it) | none yet |", 1))
		}, []string{`^docs/spec/hooks.md:[0-9]+: CAP-K05 says what of it no test holds, but its tests cell is "none yet"$`}},
		{"an item that reads differently in a second spec", func(t *testing.T, dir string) {
			mk, _ := readMustKeep(dir)
			var rows []keepEntry
			for _, e := range mk.rows {
				if e.id == "CAP-K03" {
					rows = append(rows, e)
				}
			}
			if len(rows) < 2 {
				t.Fatalf("CAP-K03 sits in %d specs, not two", len(rows))
			}
			row := strings.Split(read(t, dir, rows[1].file), "\n")[rows[1].line-1]
			edit(t, dir, rows[1].file, row, strings.Replace(row, rows[1].item, rows[1].item+" (untested: the job's own runs)", 1))
		}, []string{`^docs/spec/[a-z-]+\.md:[0-9]+: CAP-K03 reads differently from its row at docs/spec/[a-z-]+\.md:[0-9]+; an item reads the same in every spec$`}},
		{"a table head with no rule", func(t *testing.T, dir string) {
			edit(t, dir, "docs/spec/identity.md", keepHead+"\n"+keepRule+"\n", keepHead+"\n")
		}, []string{`^docs/spec/identity.md:[0-9]+: its table's head has no rule after it$`, `^docs/spec/identity.md:[0-9]+: a row before the table's head`, `^SET-K1[4-7] sits in no spec's`}},
		{"a spec without the section", func(t *testing.T, dir string) {
			edit(t, dir, "docs/spec/identity.md", keepHeading+"\n", "## Kept\n")
		}, []string{`^docs/spec/identity.md: it has no ## Must keep section before its Since the split$`, `^SET-K1[4-7] sits in no spec's`}},
		{"a section away from Since the split", func(t *testing.T, dir string) {
			edit(t, dir, "docs/spec/identity.md", "## Since the split\n", "## Later\n\nText.\n\n## Since the split\n")
		}, []string{`^docs/spec/identity.md:[0-9]+: its ## Must keep section is not just before its Since the split$`}},
		{"a second section", func(t *testing.T, dir string) {
			edit(t, dir, "docs/spec/identity.md", "## Since the split\n", keepHeading+"\n\n## Since the split\n")
		}, []string{`^docs/spec/identity.md:[0-9]+: a second ## Must keep section$`, `^docs/spec/identity.md:[0-9]+: its ## Must keep section is not just before its Since the split$`}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			t.Parallel()
			dir := copyMustKeep(t)
			c.seed(t, dir)
			problems := checkMustKeep(dir)
			for _, w := range c.want {
				found := false
				for _, p := range problems {
					if regexp.MustCompile(w).MatchString(p) {
						found = true
					}
				}
				if !found {
					t.Errorf("no problem says %q: %q", w, problems)
				}
			}
			for _, p := range problems {
				expected := false
				for _, w := range c.want {
					if regexp.MustCompile(w).MatchString(p) {
						expected = true
					}
				}
				if !expected {
					t.Errorf("a problem the seed does not explain: %q", p)
				}
			}
		})
	}
}

// The register's lists are read in their order, each with the prefix of
// its part's first finding, and a list whose part numbers no finding, a
// second list for one part, and a list never closed are each a problem.
func TestKeepListsReadTheRegistersShape(t *testing.T) {
	part := func(prefix string, items ...string) string {
		s := "### A part\n\n| Id | Severity |\n| --- | --- |\n| " + prefix + "-01 | high |\n| " + prefix + "-02 | low |\n\n<details><summary>Must keep (2 invariants)</summary>\n\n"
		for _, it := range items {
			s += "- " + it + "\n"
		}
		return s + "\n</details>\n\n"
	}
	lists, problems := keepLists(part("AB", "one", "two") + part("CD", "three"))
	var got []string
	for _, k := range lists {
		got = append(got, k.prefix+": "+strings.Join(k.ids, " "))
	}
	if strings.Join(got, "; ") != "AB: AB-K01 AB-K02; CD: CD-K01" {
		t.Errorf("read %q", got)
	}
	if len(problems) != 1 || !strings.Contains(problems[0], "read 2 must-keep lists and 3 items, fewer than") {
		t.Errorf("%q", problems)
	}
	for _, c := range []struct{ text, want string }{
		{"### A part\n\n<details><summary>Must keep</summary>\n\n- one\n\n</details>\n", "a must-keep list whose part numbers no finding"},
		{part("AB", "one") + "<details><summary>Must keep</summary>\n\n- two\n</details>\n", "a second must-keep list for AB"},
		{part("AB", "one") + "<details><summary>Must keep</summary>\n\n- two\n", "its last must-keep list is never closed"},
		{"| AB-01 | x |\n<details><summary>Must keep</summary>\n- one\n### Next\n", "a part starts inside a must-keep list"},
	} {
		_, problems := keepLists(c.text)
		found := false
		for _, p := range problems {
			found = found || strings.Contains(p, c.want)
		}
		if !found {
			t.Errorf("%q: no problem says %q: %q", c.text, c.want, problems)
		}
	}
}

// Ids count from K01 in each list and take a third digit past 99, so a
// long list never repeats an id.
func TestKeepIDsCountFromOne(t *testing.T) {
	text := "### P\n\n| ZZ-01 | x |\n\n<details><summary>Must keep</summary>\n\n" + strings.Repeat("- x\n", 100) + "</details>\n"
	lists, _ := keepLists(text)
	if len(lists) != 1 || len(lists[0].ids) != 100 {
		t.Fatalf("read %+v", lists)
	}
	ids := lists[0].ids
	if ids[0] != "ZZ-K01" || ids[8] != "ZZ-K09" || ids[99] != "ZZ-K100" {
		t.Errorf("ids %q, %q, %q", ids[0], ids[8], ids[99])
	}
	sorted := append([]string(nil), ids...)
	sort.Strings(sorted)
	for i := 1; i < len(sorted); i++ {
		if sorted[i] == sorted[i-1] {
			t.Errorf("%s twice", sorted[i])
		}
	}
	if !keepRow.MatchString("| ZZ-K100 | x | none yet |") {
		t.Error("a row of a three-digit id is not read")
	}
}
