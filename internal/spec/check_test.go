package spec

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"os"
	"path"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"
)

// The files the checks read, from the repository root.
const (
	specDir           = "docs/spec"
	recordDir         = "docs/decisions"
	designIndex       = "docs/design.md"
	verificationIndex = "docs/verification.md"
	decisionsIndex    = "docs/decisions.md"
	specIndex         = "docs/spec/INDEX.md"
)

// The limits every checked file keeps.
const (
	maxBytes = 24576
	maxLine  = 400
	maxRow   = 2000
)

// base is the commit the split was made from. sums holds, for each source,
// the SHA-256 of its words there, as words gives them. Each is recomputed
// from the source at base with
//
//	git show 3319eef:docs/design.md | python3 -I -c 'import hashlib,re,sys; t=sys.stdin.read(); t=re.sub(r"<!--.*?-->","",t,flags=re.S); t=re.sub(r"\]\([^)\s]*\)","]",t); print(hashlib.sha256(" ".join(w for w in re.split(r"[ \t\r\n]+",t) if w and not re.fullmatch(r"#+",w)).encode()).hexdigest())'
const base = "3319eef"

var sums = map[string]string{
	designIndex:       "3e29e2ebb52a7ae2d8b32adc38b1d692a15fbba0866e8840286eaa952f751ed4",
	verificationIndex: "ad1d3ca782f06e207ac1a46cbb6dd3fd610773cd3ef1e9a3dfd100296d6677ba",
	decisionsIndex:    "7a049c1699fe959cc1087016e5d47d81d596a29025ceb9381f5dd39f04865943",
}

// v2Specs are the specs INDEX.md lists that the split left as they were.
var v2Specs = []string{"docs/spec/store-format.md", "internal/ledger/SPEC.md"}

// recordSections are a record's sections, in their order.
var recordSections = []string{"Context", "Decision", "Consequences", "Reopens", "Reopened by"}

var (
	marker     = regexp.MustCompile(`<!-- (?:v1 (\S+) ([0-9]+)|/v1) -->`)
	comment    = regexp.MustCompile(`(?s)<!--.*?-->`)
	target     = regexp.MustCompile(`\]\([^)\s]*\)`)
	space      = regexp.MustCompile(`[ \t\r\n]+`)
	hashes     = regexp.MustCompile(`^#+$`)
	heading    = regexp.MustCompile(`^(#{1,6}) (.+)$`)
	bullet     = regexp.MustCompile(`^- \*\*([^*]+)\*\*`)
	row        = regexp.MustCompile(`^\| (.+?) \| \[[^\]]*\]\(([^)\s]+)\) \|$`)
	field      = regexp.MustCompile(`^- \*\*([A-Za-z ]+):\*\* (.*)$`)
	recordName = regexp.MustCompile(`^([0-9]{4}-[0-9]{2}-[0-9]{2})-([a-z0-9]+(?:-[a-z0-9]+)*)\.md$`)
	link       = regexp.MustCompile(`\]\(([^)\s]*)\)`)
	scheme     = regexp.MustCompile(`^[A-Za-z][A-Za-z0-9+.-]*:`)
	codePath   = regexp.MustCompile("`([^`]+)`")
)

// words is a text's words as the no-word-changed check compares them:
// comments, link targets and tokens made only of '#' removed, and white
// space collapsed to one space.
func words(text string) string {
	text = comment.ReplaceAllString(text, "")
	text = target.ReplaceAllString(text, "]")
	var out []string
	for _, w := range space.Split(text, -1) {
		if w != "" && !hashes.MatchString(w) {
			out = append(out, w)
		}
	}
	return strings.Join(out, " ")
}

func sum(text string) string {
	s := sha256.Sum256([]byte(words(text)))
	return hex.EncodeToString(s[:])
}

// block is one block of a source, as a spec or a record holds it.
type block struct {
	file       string // the file holding it, from the root
	source     string // the source it came from, as docs/design.md
	ordinal    int
	text       string
	start, end int // its text's bounds in the file
}

// doc is one file the checks read.
type doc struct {
	name   string // from the root, with slashes
	text   string
	blocks []block
}

// parse finds a file's blocks, and the problems with its block comments.
func parse(name, text string) (doc, []string) {
	d := doc{name: name, text: text}
	var problems []string
	var open *block
	for _, m := range marker.FindAllStringSubmatchIndex(text, -1) {
		if open != nil {
			open.end = m[0]
			open.text = text[open.start:open.end]
			d.blocks = append(d.blocks, *open)
			open = nil
		}
		if m[2] < 0 {
			continue
		}
		source, digits := text[m[2]:m[3]], text[m[4]:m[5]]
		n, _ := strconv.Atoi(digits)
		if _, ok := sums[source]; !ok || len(digits) != 3 || n < 1 {
			problems = append(problems, fmt.Sprintf("%s: a block of %s numbered %s: a block names one of the three sources and its order there, from 001", name, source, digits))
			continue
		}
		open = &block{file: name, source: source, ordinal: n, start: m[1]}
	}
	if open != nil {
		problems = append(problems, fmt.Sprintf("%s: block %03d of %s is never closed with <!-- /v1 -->", name, open.ordinal, open.source))
	}
	return d, problems
}

// tree is what the checks read from a repository root.
type tree struct {
	root    string
	specs   []doc // docs/spec/*.md, INDEX.md included
	records []doc // every file of docs/decisions/
	indexes []doc // the three old files
	others  []string
}

func (t *tree) all() []doc {
	out := append(append([]doc(nil), t.specs...), t.records...)
	return append(out, t.indexes...)
}

func (t *tree) find(name string) (doc, bool) {
	for _, d := range t.all() {
		if d.name == name {
			return d, true
		}
	}
	return doc{}, false
}

// load reads the tree, and the problems with its block comments.
func load(root string) (*tree, []string) {
	t := &tree{root: root}
	var problems []string
	read := func(name string) (doc, bool) {
		data, err := os.ReadFile(filepath.Join(root, filepath.FromSlash(name)))
		if err != nil {
			problems = append(problems, err.Error())
			return doc{}, false
		}
		d, p := parse(name, string(data))
		problems = append(problems, p...)
		return d, true
	}
	list := func(dir string) []string {
		entries, err := os.ReadDir(filepath.Join(root, filepath.FromSlash(dir)))
		if err != nil {
			problems = append(problems, err.Error())
			return nil
		}
		var names []string
		for _, e := range entries {
			names = append(names, path.Join(dir, e.Name()))
		}
		sort.Strings(names)
		return names
	}
	for _, name := range list(specDir) {
		if !strings.HasSuffix(name, ".md") {
			t.others = append(t.others, name)
			continue
		}
		if d, ok := read(name); ok {
			t.specs = append(t.specs, d)
		}
	}
	for _, name := range list(recordDir) {
		if d, ok := read(name); ok {
			t.records = append(t.records, d)
		}
	}
	for _, name := range []string{designIndex, verificationIndex, decisionsIndex} {
		if d, ok := read(name); ok {
			t.indexes = append(t.indexes, d)
		}
	}
	return t, problems
}

// check runs every check on the repository at root and returns its problems.
func check(root string) []string {
	t, problems := load(root)
	problems = append(problems, checkWords(t)...)
	for _, d := range t.all() {
		problems = append(problems, checkLimits(d)...)
	}
	for _, name := range t.others {
		problems = append(problems, name+": docs/spec holds only .md files")
	}
	problems = append(problems, checkIndexes(t)...)
	problems = append(problems, checkSpecIndex(t)...)
	for _, d := range t.records {
		problems = append(problems, checkRecord(d)...)
	}
	problems = append(problems, checkRecordIndex(t)...)
	for _, d := range t.all() {
		problems = append(problems, checkLinks(root, d)...)
	}
	return problems
}

// bySource returns each source's blocks in their order, and the problems
// with their numbering.
func bySource(t *tree) (map[string][]block, []string) {
	out := map[string][]block{}
	for _, d := range append(append([]doc(nil), t.specs...), t.records...) {
		for _, b := range d.blocks {
			out[b.source] = append(out[b.source], b)
		}
	}
	var problems []string
	for source, bs := range out {
		sort.SliceStable(bs, func(i, j int) bool { return bs[i].ordinal < bs[j].ordinal })
		for i, b := range bs {
			if b.ordinal != i+1 {
				problems = append(problems, fmt.Sprintf("%s: block %03d of %s is out of the order 001 to %03d: a block is missing or repeated", b.file, b.ordinal, source, len(bs)))
				break
			}
		}
	}
	return out, problems
}

// checkWords holds each source's blocks to its words at the split's base.
func checkWords(t *tree) []string {
	blocks, problems := bySource(t)
	for _, d := range t.indexes {
		for _, b := range d.blocks {
			problems = append(problems, fmt.Sprintf("%s: an index holds block %03d of %s; blocks live in docs/spec and docs/decisions", d.name, b.ordinal, b.source))
		}
	}
	var sources []string
	for s := range sums {
		sources = append(sources, s)
	}
	sort.Strings(sources)
	for _, source := range sources {
		var texts []string
		for _, b := range blocks[source] {
			texts = append(texts, b.text)
		}
		if got := sum(strings.Join(texts, "\n")); got != sums[source] {
			problems = append(problems, fmt.Sprintf("%s: its %d blocks' words hash to %s; at the split's base, %s, they hash to %s: a word was changed, dropped or added", source, len(texts), got, base, sums[source]))
		}
	}
	return problems
}

// checkLimits holds a file to the size and line limits.
func checkLimits(d doc) []string {
	var problems []string
	if n := len(d.text); n > maxBytes {
		problems = append(problems, fmt.Sprintf("%s: %d bytes, over the limit of %d", d.name, n, maxBytes))
	}
	for i, l := range strings.Split(d.text, "\n") {
		n := utf8.RuneCountInString(l)
		table := strings.HasPrefix(strings.TrimSpace(l), "|")
		switch {
		case n > maxRow:
			problems = append(problems, fmt.Sprintf("%s:%d: a line of %d characters, over the limit of %d", d.name, i+1, n, maxRow))
		case n > maxLine && !table:
			problems = append(problems, fmt.Sprintf("%s:%d: a line of %d characters, over the limit of %d for a line that is not a table row", d.name, i+1, n, maxLine))
		}
	}
	return problems
}

// outsideFences calls f with each line of text that is not in a fenced
// code block, and its index.
func outsideFences(text string, f func(i int, line string)) {
	fence := ""
	for i, l := range strings.Split(text, "\n") {
		s := strings.TrimLeft(l, " ")
		if fence != "" {
			if strings.HasPrefix(s, fence) {
				fence = ""
			}
			continue
		}
		if strings.HasPrefix(s, "```") || strings.HasPrefix(s, "~~~") {
			fence = s[:3]
			continue
		}
		f(i, l)
	}
}

// headings gives the text of each heading of text, outside fences.
func headings(text string) []string {
	var out []string
	outsideFences(text, func(_ int, l string) {
		if m := heading.FindStringSubmatch(l); m != nil {
			out = append(out, strings.TrimSpace(m[2]))
		}
	})
	return out
}

// bullets gives the title of each bold bullet of text, outside fences.
func bullets(text string) []string {
	var out []string
	outsideFences(text, func(_ int, l string) {
		if m := bullet.FindStringSubmatch(l); m != nil {
			out = append(out, m[1])
		}
	})
	return out
}

// entry is a heading or bullet title and the file that holds it.
type entry struct{ title, file string }

// tables reads an index's tables of entries by their head, as "Heading"
// for a table headed | Heading | Now in |, with each row's link resolved
// from the index's folder.
func tables(d doc) map[string][]entry {
	out := map[string][]entry{}
	head := ""
	for _, l := range strings.Split(d.text, "\n") {
		if !strings.HasPrefix(l, "|") {
			head = ""
			continue
		}
		if strings.HasSuffix(l, " | Now in |") {
			head = strings.TrimSuffix(strings.TrimPrefix(l, "| "), " | Now in |")
			continue
		}
		if m := row.FindStringSubmatch(l); m != nil && head != "" {
			out[head] = append(out[head], entry{m[1], path.Join(path.Dir(d.name), m[2])})
		}
	}
	return out
}

// diff names what want has that got lacks, and what got has that want
// lacks, counting repeats.
func diff(index, kind string, want, got []entry) []string {
	count := map[entry]int{}
	for _, e := range got {
		count[e]++
	}
	var problems []string
	for _, e := range want {
		if count[e] > 0 {
			count[e]--
			continue
		}
		problems = append(problems, fmt.Sprintf("%s names no file for the %s %q, which %s holds", index, kind, e.title, e.file))
	}
	var extra []entry
	for e, n := range count {
		for ; n > 0; n-- {
			extra = append(extra, e)
		}
	}
	sort.Slice(extra, func(i, j int) bool { return extra[i].title+extra[i].file < extra[j].title+extra[j].file })
	for _, e := range extra {
		problems = append(problems, fmt.Sprintf("%s names %s for the %s %q, which no block there holds", index, e.file, kind, e.title))
	}
	return problems
}

// checkIndexes holds each old file's index to the headings, and for
// verification.md the bold bullets, its blocks hold.
func checkIndexes(t *tree) []string {
	blocks, _ := bySource(t)
	var problems []string
	for _, idx := range []struct {
		name  string
		kinds []string
	}{{designIndex, []string{"Heading"}}, {verificationIndex, []string{"Heading", "Bullet"}}, {decisionsIndex, []string{"Heading"}}} {
		d, ok := t.find(idx.name)
		if !ok {
			continue
		}
		got := tables(d)
		for _, kind := range idx.kinds {
			var want []entry
			for _, b := range blocks[idx.name] {
				titles := headings(b.text)
				if kind == "Bullet" {
					titles = bullets(b.text)
				}
				for _, title := range titles {
					want = append(want, entry{title, b.file})
				}
			}
			if len(want) == 0 {
				problems = append(problems, fmt.Sprintf("%s: its blocks hold no %s", idx.name, strings.ToLower(kind)))
			}
			problems = append(problems, diff(idx.name, strings.ToLower(kind), want, got[kind])...)
		}
	}
	return problems
}

// header reads the fields a spec or record gives as - **Name:** value,
// before its first block or section.
func header(text string) map[string]string {
	out := map[string]string{}
	for _, l := range strings.Split(text, "\n") {
		if strings.HasPrefix(l, "<!-- ") || strings.HasPrefix(l, "## ") {
			break
		}
		if m := field.FindStringSubmatch(l); m != nil {
			out[m[1]] = m[2]
		}
	}
	return out
}

// cells splits a table row into its cells.
func cells(l string) []string {
	l = strings.TrimSuffix(strings.TrimPrefix(l, "| "), " |")
	return strings.Split(l, " | ")
}

// checkPaths holds each path a cell names in backticks to the tree.
func checkPaths(root, where, cell string) []string {
	var problems []string
	ms := codePath.FindAllStringSubmatch(cell, -1)
	if len(ms) == 0 {
		problems = append(problems, fmt.Sprintf("%s names no path", where))
	}
	for _, m := range ms {
		if _, err := os.Stat(filepath.Join(root, filepath.FromSlash(m[1]))); err != nil {
			problems = append(problems, fmt.Sprintf("%s names %s, which does not exist", where, m[1]))
		}
	}
	return problems
}

var cellLink = regexp.MustCompile(`^\[[^\]]*\]\(([^)\s]+)\)$`)

var frozen = regexp.MustCompile(`^v1 as built, frozen on [0-9]{4}-[0-9]{2}-[0-9]{2}$`)

// checkSpecIndex holds INDEX.md to the specs present and their headers.
func checkSpecIndex(t *tree) []string {
	index, ok := t.find(specIndex)
	if !ok {
		return []string{specIndex + " is missing"}
	}
	var problems []string
	listed := map[string][]string{}
	for i, l := range strings.Split(index.text, "\n") {
		c := cells(l)
		if !strings.HasPrefix(l, "| [") || len(c) != 4 {
			continue
		}
		m := cellLink.FindStringSubmatch(c[0])
		if m == nil {
			problems = append(problems, fmt.Sprintf("%s:%d: a row whose first cell is not a link", specIndex, i+1))
			continue
		}
		name := path.Join(specDir, m[1])
		if _, dup := listed[name]; dup {
			problems = append(problems, fmt.Sprintf("%s lists %s twice", specIndex, name))
		}
		listed[name] = c[1:]
		problems = append(problems, checkPaths(t.root, fmt.Sprintf("%s:%d", specIndex, i+1), c[2])...)
	}
	want := map[string]bool{}
	for _, name := range v2Specs {
		want[name] = true
	}
	for _, d := range t.specs {
		if d.name == specIndex {
			continue
		}
		want[d.name] = true
		if len(d.blocks) == 0 {
			continue
		}
		problems = append(problems, checkSpec(t.root, d)...)
		h := header(d.text)
		if got, ok := listed[d.name]; ok && (got[0] != h["Component"] || got[1] != h["Paths"] || got[2] != h["Status"]) {
			problems = append(problems, fmt.Sprintf("%s gives %s as %q; its header says %q", specIndex, d.name, got, []string{h["Component"], h["Paths"], h["Status"]}))
		}
	}
	for name := range want {
		if _, ok := listed[name]; !ok {
			problems = append(problems, fmt.Sprintf("%s doesn't list %s", specIndex, name))
		}
	}
	for name := range listed {
		if !want[name] {
			problems = append(problems, fmt.Sprintf("%s lists %s, which is not a spec there", specIndex, name))
		}
	}
	sort.Strings(problems)
	return problems
}

// checkSpec holds a v1 spec to its header and its last section.
func checkSpec(root string, d doc) []string {
	var problems []string
	h := header(d.text)
	if !strings.HasPrefix(d.text, "# ") {
		problems = append(problems, d.name+": a spec opens with its title")
	}
	if h["Component"] == "" {
		problems = append(problems, d.name+": its header names no component")
	}
	problems = append(problems, checkPaths(root, d.name+"'s header", h["Paths"])...)
	if !frozen.MatchString(h["Status"]) {
		problems = append(problems, fmt.Sprintf("%s: its status %q is not \"v1 as built, frozen on <date>\"", d.name, h["Status"]))
	}
	last := d.blocks[len(d.blocks)-1]
	if !strings.Contains(d.text[last.end:], "\n## Since the split\n") {
		problems = append(problems, d.name+": it doesn't end with ## Since the split, after its blocks")
	}
	return problems
}

// slug is a record's name for its title: lower case, with each run of
// other characters than letters and digits a hyphen.
func slug(title string) string {
	return strings.Trim(regexp.MustCompile(`[^a-z0-9]+`).ReplaceAllString(strings.ToLower(title), "-"), "-")
}

// inBlock says whether offset i of d's text is inside a block.
func (d doc) inBlock(i int) bool {
	for _, b := range d.blocks {
		if i >= b.start && i < b.end {
			return true
		}
	}
	return false
}

// checkRecord holds a decision record's name, title, fields and sections
// to each other.
func checkRecord(d doc) []string {
	var problems []string
	base := path.Base(d.name)
	m := recordName.FindStringSubmatch(base)
	if m == nil {
		return []string{fmt.Sprintf("%s: a record is named <YYYY-MM-DD>-<slug>.md", d.name)}
	}
	if _, err := time.Parse("2006-01-02", m[1]); err != nil {
		problems = append(problems, fmt.Sprintf("%s: its name's date %s is not a day", d.name, m[1]))
	}
	title, _, _ := strings.Cut(d.text, "\n")
	title, ok := strings.CutPrefix(title, "# ")
	if !ok || slug(title) != m[2] {
		problems = append(problems, fmt.Sprintf("%s: its title %q gives the name %s-%s.md", d.name, title, m[1], slug(title)))
	}
	h := header(d.text)
	if h["Date"] != m[1] {
		problems = append(problems, fmt.Sprintf("%s: its date %q is not its name's, %s", d.name, h["Date"], m[1]))
	}
	if h["Status"] != "Accepted" && h["Status"] != "Reopened" {
		problems = append(problems, fmt.Sprintf("%s: its status %q is not Accepted or Reopened", d.name, h["Status"]))
	}
	// Its sections: the level-2 headings outside its blocks.
	var names []string
	var starts []int
	offset := 0
	for _, l := range strings.SplitAfter(d.text, "\n") {
		if s, ok := strings.CutPrefix(strings.TrimRight(l, "\n"), "## "); ok && !d.inBlock(offset) {
			names = append(names, s)
			starts = append(starts, offset)
		}
		offset += len(l)
	}
	if strings.Join(names, "|") != strings.Join(recordSections, "|") {
		return append(problems, fmt.Sprintf("%s: its sections are %q, not %q", d.name, names, recordSections))
	}
	body := map[string]string{}
	for i, name := range names {
		end := len(d.text)
		if i+1 < len(starts) {
			end = starts[i+1]
		}
		_, text, _ := strings.Cut(d.text[starts[i]:end], "\n")
		body[name] = strings.TrimSpace(text)
		if body[name] == "" {
			problems = append(problems, fmt.Sprintf("%s: its section %s is empty", d.name, name))
		}
	}
	if none := body["Reopened by"] == "None."; none != (h["Status"] == "Accepted") {
		problems = append(problems, fmt.Sprintf("%s: its status %q and its Reopened by %q disagree: a record is Reopened when, and only when, something reopens it", d.name, h["Status"], body["Reopened by"]))
	}
	return problems
}

var recordRow = regexp.MustCompile(`^\| \[([^\]]+)\]\(([^)\s]+)\) \| ([^|]+) \| ([^|]+) \|$`)

// checkRecordIndex holds docs/decisions.md's list of records to the
// records present.
func checkRecordIndex(t *tree) []string {
	index, ok := t.find(decisionsIndex)
	if !ok {
		return nil
	}
	var problems []string
	listed := map[string]bool{}
	for _, l := range strings.Split(index.text, "\n") {
		m := recordRow.FindStringSubmatch(l)
		if m == nil {
			continue
		}
		name := path.Join(path.Dir(decisionsIndex), m[2])
		listed[name] = true
		r, ok := t.find(name)
		if !ok || !strings.HasPrefix(name, recordDir+"/") {
			problems = append(problems, fmt.Sprintf("%s lists %s, which is not a record there", decisionsIndex, name))
			continue
		}
		h := header(r.text)
		if title, _, _ := strings.Cut(r.text, "\n"); "# "+m[1] != title || h["Date"] != m[3] || h["Status"] != m[4] {
			problems = append(problems, fmt.Sprintf("%s gives %s as %q, %s, %s; the record says %q, %s, %s", decisionsIndex, name, m[1], m[3], m[4], title, h["Date"], h["Status"]))
		}
	}
	for _, r := range t.records {
		if !listed[r.name] {
			problems = append(problems, fmt.Sprintf("%s doesn't list %s", decisionsIndex, r.name))
		}
	}
	return problems
}

// blankCode returns text with its fenced code blocks and code spans
// replaced by spaces, so no link inside them is read.
func blankCode(text string) string {
	b := []byte(text)
	lines := strings.SplitAfter(text, "\n")
	offset := 0
	fence := ""
	for _, l := range lines {
		s := strings.TrimLeft(l, " ")
		in := fence != ""
		switch {
		case fence != "" && strings.HasPrefix(s, fence):
			fence = ""
		case fence == "" && (strings.HasPrefix(s, "```") || strings.HasPrefix(s, "~~~")):
			fence, in = s[:3], true
		}
		if in || fence != "" {
			for i := offset; i < offset+len(l); i++ {
				if b[i] != '\n' {
					b[i] = ' '
				}
			}
		}
		offset += len(l)
	}
	// Code spans: a run of n backticks up to the next run of exactly n,
	// within one paragraph.
	for i := 0; i < len(b); {
		if b[i] != '`' {
			i++
			continue
		}
		j := i
		for j < len(b) && b[j] == '`' {
			j++
		}
		n, close := j-i, -1
		for k := j; k < len(b); {
			if b[k] == '\n' && k+1 < len(b) && b[k+1] == '\n' {
				break
			}
			if b[k] != '`' {
				k++
				continue
			}
			e := k
			for e < len(b) && b[e] == '`' {
				e++
			}
			if e-k == n {
				close = e
				break
			}
			k = e
		}
		if close < 0 {
			i = j
			continue
		}
		for k := i; k < close; k++ {
			if b[k] != '\n' {
				b[k] = ' '
			}
		}
		i = close
	}
	return string(b)
}

// anchors gives the anchors a host such as GitHub makes for a Markdown
// file's headings: lower case, with letters, digits, hyphens and
// underscores kept, spaces made hyphens and the rest dropped, and a repeat
// numbered from -1.
func anchors(text string) map[string]bool {
	out := map[string]bool{}
	seen := map[string]int{}
	for _, h := range headings(text) {
		var s strings.Builder
		for _, r := range strings.ToLower(h) {
			switch {
			case r == ' ':
				s.WriteByte('-')
			case r == '-' || r == '_' || unicode.IsLetter(r) || unicode.IsDigit(r):
				s.WriteRune(r)
			}
		}
		a := s.String()
		if n := seen[a]; n > 0 {
			out[fmt.Sprintf("%s-%d", a, n)] = true
		} else {
			out[a] = true
		}
		seen[a]++
	}
	return out
}

// ref is a relative link outside code: where it is, and what it names.
type ref struct {
	line          int
	dest          string // as written
	name          string // the file or folder it names, from the root
	anchor        string
	leavesTheRepo bool
}

// refs gives d's relative links outside comments and code.
func refs(d doc) []ref {
	var out []ref
	text := blankCode(comment.ReplaceAllStringFunc(d.text, func(c string) string { return strings.Repeat(" ", len(c)) }))
	for _, m := range link.FindAllStringSubmatchIndex(text, -1) {
		dest := text[m[2]:m[3]]
		if dest == "" || scheme.MatchString(dest) {
			continue
		}
		r := ref{line: strings.Count(text[:m[0]], "\n") + 1, dest: dest, name: d.name}
		p, anchor, _ := strings.Cut(dest, "#")
		r.anchor = anchor
		if p != "" {
			r.name = path.Join(path.Dir(d.name), p)
		}
		r.leavesTheRepo = r.name == ".." || strings.HasPrefix(r.name, "../")
		out = append(out, r)
	}
	return out
}

// checkLinks holds each relative link of d outside code to a file or
// folder that exists, and a link's anchor to a heading of its file.
func checkLinks(root string, d doc) []string {
	var problems []string
	for _, r := range refs(d) {
		if r.leavesTheRepo {
			problems = append(problems, fmt.Sprintf("%s:%d: the link %s leaves the repository", d.name, r.line, r.dest))
			continue
		}
		file := filepath.Join(root, filepath.FromSlash(r.name))
		info, err := os.Stat(file)
		if err != nil {
			problems = append(problems, fmt.Sprintf("%s:%d: the link %s leads nowhere", d.name, r.line, r.dest))
			continue
		}
		if r.anchor == "" {
			continue
		}
		data, err := os.ReadFile(file)
		if info.IsDir() || err != nil || !strings.HasSuffix(r.name, ".md") || !anchors(string(data))[r.anchor] {
			problems = append(problems, fmt.Sprintf("%s:%d: the link %s names no heading of %s", d.name, r.line, r.dest, r.name))
		}
	}
	return problems
}
