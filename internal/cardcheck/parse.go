package cardcheck

import (
	"fmt"
	"io/fs"
	"path"
	"regexp"
	"sort"
	"strconv"
	"strings"
)

var (
	// planID is a package id as the plan writes it: SEAM-0, LG-10, CUT-4.
	// It never matches the start of a card's id, such as SEAM-0a.
	planID = regexp.MustCompile(`\b[A-Z]{2,}-[0-9]+\b`)
	// cardRef is a plan id or a card's id, which may end in a lowercase
	// part: SEAM-0a, CT-3b.
	cardRef = regexp.MustCompile(`\b[A-Z]{2,}-[0-9]+[a-z]*\b`)
	// cardName splits a card's id into its plan id and its part.
	cardName      = regexp.MustCompile(`^([A-Z]{2,}-[0-9]+)[a-z]*$`)
	stageHeading  = regexp.MustCompile(`^### Stage ([0-9]+)\b`)
	contractsHead = regexp.MustCompile(`^## [0-9]+\. Contracts\b`)
	bracketed     = regexp.MustCompile(`\(([^()]*)\)`)
)

// The words a provider may be instead of plan ids.
const (
	providerV1      = "v1"
	providerLater   = "later"
	providerDropped = "dropped"
	providerOpen    = "open"
)

// noConsumer is the Consumer cell of a need no plan package consumes.
const noConsumer = "none"

// splitRow returns the trimmed cells of a Markdown table row. A pipe inside
// a code span or after a backslash does not end a cell.
func splitRow(line string) []string {
	line = strings.TrimSpace(line)
	line = strings.TrimPrefix(line, "|")
	if strings.HasSuffix(line, "|") && !strings.HasSuffix(line, `\|`) {
		line = line[:len(line)-1]
	}
	var cells []string
	var b strings.Builder
	code := false
	for i := 0; i < len(line); i++ {
		c := line[i]
		switch {
		case c == '\\' && i+1 < len(line) && line[i+1] == '|':
			b.WriteByte('|')
			i++
			continue
		case c == '`':
			code = !code
		case c == '|' && !code:
			cells = append(cells, strings.TrimSpace(b.String()))
			b.Reset()
			continue
		}
		b.WriteByte(c)
	}
	return append(cells, strings.TrimSpace(b.String()))
}

// isSeparator reports whether cells are a table's header separator.
func isSeparator(cells []string) bool {
	for _, c := range cells {
		if strings.Trim(c, "-: ") != "" {
			return false
		}
	}
	return true
}

// plain is a cell without its code marks.
func plain(cell string) string {
	return strings.TrimSpace(strings.ReplaceAll(cell, "`", ""))
}

// table is one Markdown table: the heading it sits under, its header's
// cells, and its rows after the separator.
type table struct {
	heading string // the nearest "## " heading above it
	header  []string
	rows    []tableRow
}

type tableRow struct {
	cells []string
}

var (
	// listItem is a line that starts a list item.
	listItem = regexp.MustCompile(`^ {0,3}([-*+]|[0-9]{1,9}[.)])([ \t]|$)`)
	// fenceOpen is a line that opens a fenced code block: three or more
	// backticks or tildes, after the indentation a list allows.
	fenceOpen = regexp.MustCompile("^[ \t]*(`{3,}|~{3,})")
)

// indent returns the width of a line's leading blanks, a tab counting four.
func indent(line string) int {
	n := 0
	for _, c := range line {
		switch c {
		case ' ':
			n++
		case '\t':
			n += 4
		default:
			return n
		}
	}
	return n
}

// codeLines marks the lines that are code, not prose: a fenced code block,
// its fences included, and an indented code block, lines indented four or
// more outside a list that follow a blank line or another such line. What
// is in code is never read as a table, a heading or a label.
func codeLines(lines []string) []bool {
	code := make([]bool, len(lines))
	fence := ""           // the open fence's marks, or "" outside one
	inList := false       // within a list item, where indentation is no code
	prevBlank := true     // the line before was blank, or there was none
	prevIndented := false // the last line that was not blank was indented code
	for i, line := range lines {
		if fence != "" {
			code[i] = true
			t := strings.TrimSpace(line)
			if strings.HasPrefix(t, fence) && strings.Trim(t, fence[:1]) == "" {
				fence = ""
			}
			continue
		}
		if strings.TrimSpace(line) == "" {
			prevBlank = true
			continue
		}
		ind := indent(line)
		if ind >= 4 && !inList && (prevBlank || prevIndented) {
			code[i] = true
			prevBlank, prevIndented = false, true
			continue
		}
		prevBlank, prevIndented = false, false
		if m := fenceOpen.FindStringSubmatch(line); m != nil && (ind < 4 || inList) {
			// A backtick fence's info string holds no backtick; a line
			// that does is a code span.
			info := line[strings.Index(line, m[1])+len(m[1]):]
			if m[1][0] != '`' || !strings.Contains(info, "`") {
				fence = m[1]
				code[i] = true
				continue
			}
		}
		switch {
		case listItem.MatchString(line):
			inList = true
		case ind == 0:
			inList = false
		}
	}
	return code
}

// quoted reports whether a line is in a block quote.
func quoted(line string) bool {
	return indent(line) < 4 && strings.HasPrefix(strings.TrimLeft(line, " \t"), ">")
}

// headerIs reports whether a table's header is exactly the names given.
func headerIs(header []string, names ...string) bool {
	if len(header) != len(names) {
		return false
	}
	for i, h := range header {
		if plain(h) != names[i] {
			return false
		}
	}
	return true
}

// tables returns every table of text from the line start (from 0) to the
// line end (exclusive), each with the "## " heading it sits under. A table
// is a run of lines that start with "|", outside code; a quoted table
// starts with ">" and is none.
func tables(lines []string, start, end int) []table {
	code := codeLines(lines)
	var out []table
	heading := ""
	var cur *table
	for i := start; i < end; i++ {
		line := lines[i]
		if code[i] {
			cur = nil
			continue
		}
		if strings.HasPrefix(line, "## ") {
			heading = strings.TrimSpace(strings.TrimPrefix(line, "## "))
		}
		if !strings.HasPrefix(strings.TrimSpace(line), "|") {
			cur = nil
			continue
		}
		cells := splitRow(line)
		if cur == nil {
			out = append(out, table{heading: heading, header: cells})
			cur = &out[len(out)-1]
			continue
		}
		if len(cur.rows) == 0 && isSeparator(cells) {
			continue
		}
		cur.rows = append(cur.rows, tableRow{cells: cells})
	}
	return out
}

// parsePlan returns the stage of each plan id: the first cell of each row
// of the tables headed "Id" in section 8, from "### Stage 0" to "### What
// runs in parallel", under the stage heading it sits under. The cut-over's
// table sits under Stage 2.
func parsePlan(text string) (map[string]int, error) {
	lines := strings.Split(text, "\n")
	code := codeLines(lines)
	start := -1
	for i, line := range lines {
		if !code[i] && strings.HasPrefix(line, "## 8. ") {
			start = i + 1
			break
		}
	}
	if start < 0 {
		return nil, fmt.Errorf("no section 8")
	}
	stages := map[string]int{}
	stage := -1
	for i := start; i < len(lines); i++ {
		line := lines[i]
		if code[i] {
			continue
		}
		if strings.HasPrefix(line, "## ") || strings.HasPrefix(line, "### What runs in parallel") {
			break
		}
		m := stageHeading.FindStringSubmatch(line)
		if m == nil {
			continue
		}
		stage, _ = strconv.Atoi(m[1])
		end := i + 1
		for end < len(lines) && (code[end] || !strings.HasPrefix(lines[end], "#")) {
			end++
		}
		for _, t := range tables(lines, i+1, end) {
			if len(t.header) == 0 || plain(t.header[0]) != "Id" {
				continue
			}
			for _, r := range t.rows {
				for _, id := range planID.FindAllString(r.cells[0], -1) {
					if _, ok := stages[id]; !ok {
						stages[id] = stage
					}
				}
			}
		}
	}
	if stage < 0 {
		return nil, fmt.Errorf("no stage heading in section 8")
	}
	return stages, nil
}

// needKey names one need: its id in the design that lists it. Ids repeat
// across designs (the ledger's C-D1 is a need, derive's a promise).
type needKey struct {
	id     string
	design string // the design's file name
}

func (k needKey) String() string { return k.id + " (" + k.design + ")" }

// parseDesignNeeds returns the ids of the rows of the design's section
// headed "## <n>. Contracts" whose direction holds "need", in order, once
// for each row. Only a table whose header starts "Id | With | Direction"
// is a contracts table.
func parseDesignNeeds(text string) []string {
	lines := strings.Split(text, "\n")
	code := codeLines(lines)
	start, end := -1, len(lines)
	for i, line := range lines {
		if code[i] {
			continue
		}
		if start < 0 {
			if contractsHead.MatchString(line) {
				start = i + 1
			}
			continue
		}
		if strings.HasPrefix(line, "## ") {
			end = i
			break
		}
	}
	if start < 0 {
		return nil
	}
	var ids []string
	for _, t := range tables(lines, start, end) {
		if len(t.header) < 3 || !headerIs(t.header[:3], "Id", "With", "Direction") {
			continue
		}
		for _, r := range t.rows {
			if len(r.cells) < 3 {
				continue
			}
			id := plain(r.cells[0])
			if id == "" || !strings.Contains(r.cells[2], "need") {
				continue
			}
			ids = append(ids, id)
		}
	}
	return ids
}

// card is one file of the cards folder.
type card struct {
	file     string   // from the repository root
	id       string   // the file's name less its extension
	packages []string // the plan packages it names
	depends  []string // the ids of its "Depends on" label
}

// boldLabel is a bold label such as "**Part of:**": a capitalised word,
// maybe more, and a colon.
var boldLabel = regexp.MustCompile(`\*\*[A-Z][^*\n]*:\*\*`)

// inCodeSpan reports whether the byte at i of line is inside a code span:
// an odd number of backticks comes before it.
func inCodeSpan(line string, i int) bool {
	return strings.Count(line[:i], "`")%2 == 1
}

// parseCard reads a card: its packages are its id less a trailing
// lowercase part, if the plan lists that, and every plan id in brackets in
// its title line; its dependencies are every id in its "**Depends on:**"
// label, up to the next bold label or the end of its paragraph. A label in
// a code span, a code block or a quote is not one.
func parseCard(file, text string, plan map[string]int) card {
	c := card{file: file, id: strings.TrimSuffix(path.Base(file), path.Ext(file))}
	seen := map[string]bool{}
	add := func(id string) {
		if _, ok := plan[id]; ok && !seen[id] {
			seen[id] = true
			c.packages = append(c.packages, id)
		}
	}
	if m := cardName.FindStringSubmatch(c.id); m != nil {
		add(m[1])
	}
	lines := strings.Split(text, "\n")
	code := codeLines(lines)
	for i, line := range lines {
		if !code[i] && strings.HasPrefix(line, "# ") {
			for _, b := range bracketed.FindAllStringSubmatch(line, -1) {
				for _, id := range planID.FindAllString(b[1], -1) {
					add(id)
				}
			}
			break
		}
	}
	const label = "**Depends on:**"
	for i, line := range lines {
		if code[i] || quoted(line) {
			continue
		}
		for from := 0; ; {
			j := strings.Index(line[from:], label)
			if j < 0 {
				break
			}
			j += from
			from = j + len(label)
			if inCodeSpan(line, j) {
				continue
			}
			c.depends = append(c.depends, cardRef.FindAllString(dependsSpan(lines, code, i, from), -1)...)
		}
	}
	return c
}

// dependsSpan returns the text of a label that starts at byte from of line
// i: up to the next bold label, or else the end of its paragraph, where a
// blank line, code, a quote, a heading, a list item or a table starts.
func dependsSpan(lines []string, code []bool, i, from int) string {
	var b strings.Builder
	text := lines[i][from:]
	for {
		if loc := boldLabel.FindStringIndex(text); loc != nil {
			b.WriteString(text[:loc[0]])
			return b.String()
		}
		b.WriteString(text)
		b.WriteByte('\n')
		i++
		if i >= len(lines) || code[i] || quoted(lines[i]) || listItem.MatchString(lines[i]) {
			return b.String()
		}
		if t := strings.TrimSpace(lines[i]); t == "" || strings.HasPrefix(t, "#") || strings.HasPrefix(t, "|") {
			return b.String()
		}
		text = lines[i]
	}
}

// readCards reads every Markdown file of the cards folder but its README,
// and returns the other files apart, from the repository root. A hidden
// file, such as the .DS_Store macOS's Finder leaves or an editor's swap
// file, is neither: git ignores it or no one meant it as a card.
func readCards(fsys fs.FS, plan map[string]int) (cards []card, others []string, err error) {
	entries, err := fs.ReadDir(fsys, CardsDir)
	if err != nil {
		return nil, nil, err
	}
	for _, e := range entries {
		name := e.Name()
		if e.IsDir() || name == "README.md" || strings.HasPrefix(name, ".") {
			continue
		}
		file := path.Join(CardsDir, name)
		if path.Ext(name) != ".md" {
			others = append(others, file)
			continue
		}
		data, err := fs.ReadFile(fsys, file)
		if err != nil {
			return nil, nil, err
		}
		cards = append(cards, parseCard(file, string(data), plan))
	}
	sort.Slice(cards, func(i, j int) bool { return cards[i].file < cards[j].file })
	sort.Strings(others)
	return cards, others, nil
}

// needRow is one row of the matrix's table of needs.
type needRow struct {
	key       needKey
	consumers []string // the cell's ids; none for noConsumer
	consumer  string   // the cell as written
	providers []string // the cell's ids, when it is not one word
	provider  string   // the cell as written
	where     string
	cells     int
}

func (r needRow) item() string { return r.key.String() }

// fakeRow is one row of the matrix's table of interfaces and fakes.
type fakeRow struct {
	name      string
	providers []string
	fake      string
	cells     int
}

// The headers of the matrix's two tables, which the rules read by column.
var (
	needsHeader = []string{"Need", "Design", "Consumer", "Provider", "Where"}
	fakesHeader = []string{"Interface", "Go name", "Provider", "Fake"}
)

// The headings the two tables sit under.
const (
	needsHeading = "Needs"
	fakesHeading = "Interfaces and fakes"
)

// badTable is a table under one of the matrix's headings whose header is
// not that table's.
type badTable struct {
	heading string
	header  string
}

// matrix is the contract matrix as read.
type matrix struct {
	needs      []needRow
	fakes      []fakeRow
	needsTable bool
	fakesTable bool
	bad        []badTable
}

// list splits a cell into its comma-separated parts.
func list(cell string) []string {
	var out []string
	for _, p := range strings.Split(plain(cell), ",") {
		if p = strings.TrimSpace(p); p != "" {
			out = append(out, p)
		}
	}
	return out
}

// parseMatrix reads the table under "## Needs" and the table under "##
// Interfaces and fakes". Any other table under either heading, such as
// one whose columns are moved, is returned as bad, not read.
func parseMatrix(text string) matrix {
	var m matrix
	lines := strings.Split(text, "\n")
	for _, t := range tables(lines, 0, len(lines)) {
		switch {
		case t.heading == needsHeading && headerIs(t.header, needsHeader...):
			m.needsTable = true
			for _, r := range t.rows {
				row := needRow{cells: len(r.cells)}
				row.key.id = plain(r.cells[0])
				if len(r.cells) > 1 {
					row.key.design = plain(r.cells[1])
				}
				if len(r.cells) == 5 {
					row.consumer = plain(r.cells[2])
					if row.consumer != noConsumer {
						row.consumers = list(r.cells[2])
					}
					row.provider = plain(r.cells[3])
					row.providers = list(r.cells[3])
					row.where = plain(r.cells[4])
				}
				m.needs = append(m.needs, row)
			}
		case t.heading == fakesHeading && headerIs(t.header, fakesHeader...):
			m.fakesTable = true
			for _, r := range t.rows {
				row := fakeRow{name: plain(r.cells[0]), cells: len(r.cells)}
				if len(r.cells) == 4 {
					row.providers = list(r.cells[2])
					row.fake = plain(r.cells[3])
				}
				m.fakes = append(m.fakes, row)
			}
		case t.heading == needsHeading || t.heading == fakesHeading:
			var h []string
			for _, c := range t.header {
				h = append(h, plain(c))
			}
			m.bad = append(m.bad, badTable{t.heading, strings.Join(h, " | ")})
		}
	}
	return m
}
