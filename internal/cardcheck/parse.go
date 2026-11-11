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

// tables returns every table of text from the line start (from 0) to the
// line end (exclusive), each with the "## " heading it sits under.
func tables(lines []string, start, end int) []table {
	var out []table
	heading := ""
	var cur *table
	for i := start; i < end; i++ {
		line := lines[i]
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
	start := -1
	for i, line := range lines {
		if strings.HasPrefix(line, "## 8. ") {
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
		if strings.HasPrefix(line, "## ") || strings.HasPrefix(line, "### What runs in parallel") {
			break
		}
		m := stageHeading.FindStringSubmatch(line)
		if m == nil {
			continue
		}
		stage, _ = strconv.Atoi(m[1])
		end := i + 1
		for end < len(lines) && !strings.HasPrefix(lines[end], "#") {
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
// headed "## <n>. Contracts" whose direction holds "need", in order.
func parseDesignNeeds(text string) []string {
	lines := strings.Split(text, "\n")
	start, end := -1, len(lines)
	for i, line := range lines {
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
		for _, r := range t.rows {
			if len(r.cells) < 3 {
				continue
			}
			id := plain(r.cells[0])
			if id == "" || id == "Id" || !strings.Contains(r.cells[2], "need") {
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

// parseCard reads a card: its packages are its id less a trailing
// lowercase part, if the plan lists that, and every plan id in brackets in
// its title line; its dependencies are every id in its "**Depends on:**"
// label, up to the next bold label or the end of the line. A label quoted
// in a code span is not one.
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
	for _, line := range strings.Split(text, "\n") {
		if strings.HasPrefix(line, "# ") {
			for _, b := range bracketed.FindAllStringSubmatch(line, -1) {
				for _, id := range planID.FindAllString(b[1], -1) {
					add(id)
				}
			}
			break
		}
	}
	const label = "**Depends on:**"
	rest := text
	for {
		i := strings.Index(rest, label)
		if i < 0 {
			break
		}
		quoted := i > 0 && rest[i-1] == '`'
		rest = rest[i+len(label):]
		if quoted {
			continue
		}
		span := rest
		if j := strings.IndexByte(span, '\n'); j >= 0 {
			span = span[:j]
		}
		if j := strings.Index(span, "**"); j >= 0 {
			span = span[:j]
		}
		c.depends = append(c.depends, cardRef.FindAllString(span, -1)...)
	}
	return c
}

// readCards reads every file of the cards folder but its README.
func readCards(fsys fs.FS, plan map[string]int) ([]card, error) {
	entries, err := fs.ReadDir(fsys, CardsDir)
	if err != nil {
		return nil, err
	}
	var cards []card
	for _, e := range entries {
		if e.IsDir() || e.Name() == "README.md" {
			continue
		}
		file := path.Join(CardsDir, e.Name())
		data, err := fs.ReadFile(fsys, file)
		if err != nil {
			return nil, err
		}
		cards = append(cards, parseCard(file, string(data), plan))
	}
	sort.Slice(cards, func(i, j int) bool { return cards[i].file < cards[j].file })
	return cards, nil
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
	name  string
	fake  string
	cells int
}

// matrix is the contract matrix as read.
type matrix struct {
	needs      []needRow
	fakes      []fakeRow
	needsTable bool
	fakesTable bool
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

// parseMatrix reads the table headed "Need" under "## Needs" and the table
// headed "Interface" under "## Interfaces and fakes".
func parseMatrix(text string) matrix {
	var m matrix
	lines := strings.Split(text, "\n")
	for _, t := range tables(lines, 0, len(lines)) {
		if len(t.header) == 0 {
			continue
		}
		switch {
		case t.heading == "Needs" && plain(t.header[0]) == "Need":
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
		case t.heading == "Interfaces and fakes" && plain(t.header[0]) == "Interface":
			m.fakesTable = true
			for _, r := range t.rows {
				row := fakeRow{name: plain(r.cells[0]), cells: len(r.cells)}
				if len(r.cells) == 4 {
					row.fake = plain(r.cells[3])
				}
				m.fakes = append(m.fakes, row)
			}
		}
	}
	return m
}
