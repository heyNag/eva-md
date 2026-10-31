// Package systempage models the wiki's system pages, systems/<slug>.md, one
// per system (a service, a repo, a platform or a major documentation area), and
// the system map that links them. They are global to the wiki: the shared
// understanding that later chats in any project read. An agent writes a page
// through eva-md system write; Write checks it line by line, refusing what
// okf, the brief or the daily commit would trip on, and saves it through
// internal/okfadapter with the map and the folder's index.
//
// A page reads:
//
//	---
//	type: system
//	title: Orders service
//	description: Takes, prices and tracks customer orders
//	status: stable
//	generated: { by: claude-code/claude-opus-5-5 }
//	sources:
//	  - resource: "https://docs.example.com/eng/orders/overview?version=12"
//	  - resource: "github.com/acme/orders@4e2bf4c:internal/orders/service.go"
//	repos: [git@github.com:acme/orders.git]
//	aliases: [order service]
//	as_of: 2026-10-30
//	state: active
//	---
//
//	## Summary
//	Orders takes an order, prices it with Payments and tracks it to delivery.
//
//	## Interfaces
//	- `POST /v1/orders` creates an order; topic `orders.v1.created`
//
//	## Depends on
//	- [Payments](payments.md)
//
// The sections are Summary (required), Responsibilities, Interfaces, Depends
// on, Used by, Decisions and constraints, Open questions and Unverified, in
// that order, with ### subheadings inside them. Saved, repos hold the project
// keys the inbox uses, and the body ends with the line code adds, "Part of
// the [system map](map.md).". The map, systems/map.md, is built from the
// pages' frontmatter on every write, one line per page sorted by slug:
// "- [<title>](<slug>.md): <description> · repos: <key>, <key> · as of
// <YYYY-MM-DD>".
package systempage

import (
	"fmt"
	"net/url"
	"regexp"
	"slices"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"

	"github.com/heyNag/eva-md/internal/freetext"
	"github.com/heyNag/eva-md/internal/wikigit"
)

// Limits of a page, in bytes for the page and characters otherwise.
const (
	maxPage        = 24 << 10
	maxTitle       = 80
	maxDescription = 160
	maxSummary     = 800
	maxAliases     = 10
	minAlias       = 3
	maxAlias       = 60
	// maxProblems bounds what a refusal lists.
	maxProblems = 20
)

// MapLink ends every page, so okf finds none orphaned.
const MapLink = "Part of the [system map](map.md)."

// sections are a page's "## " sections, in their order. The first is
// required.
var sections = []string{"Summary", "Responsibilities", "Interfaces", "Depends on", "Used by", "Decisions and constraints", "Open questions", "Unverified"}

var (
	// fenceRun is a run of backticks or tildes that could open a fence.
	fenceRun = regexp.MustCompile("`{3,}|~{3,}")
	// inlineLink is the target of a markdown link or image.
	inlineLink = regexp.MustCompile(`\]\(\s*<?([^)\s>]*)`)
	// refDef is the target of a link reference definition.
	refDef = regexp.MustCompile(`^ {0,3}\[[^\]]+\]:\s*<?([^\s>]*)`)
	// autolink is a <scheme:...> link.
	autolink = regexp.MustCompile(`<([A-Za-z][A-Za-z0-9+.-]{1,31}:[^\s<>]*)>`)
	// bareURL is an address that markdown renderers link on their own.
	bareURL = regexp.MustCompile(`\b([A-Za-z][A-Za-z0-9+.-]*://[^\s<>]*)`)
	// okfLink is how okf finds a link to a page (bundle.go linkRegex), code
	// spans included.
	okfLink = regexp.MustCompile(`\]\(([^)\s]+\.md)(?:#[^)]*)?\)`)
	// scheme starts an address with a scheme, such as http: or mailto:.
	scheme = regexp.MustCompile(`^[A-Za-z][A-Za-z0-9+.-]*:`)
)

// headingRule names the sections a heading may start.
var headingRule = "a heading that is not one of the page's sections, or out of order: ## " + strings.Join(sections, ", ## ") + ", in that order, with ### inside them"

// Problems a refusal words the same way wherever they are found.
const (
	linkRule      = "a link that is neither https:// nor to a system page"
	pageLinkRule  = "a link to something other than an existing system page; link systems as [<title>](<slug>.md) once their pages exist"
	sourceRule    = "a source is an https:// URL without a user name, a password or a credential parameter such as sig= or token=, with the version it cites when it has one (a version query parameter, or the URL, a space and v<n>), or <repo>@<commit> or <repo>@<commit>:<path>"
	secretRule    = "what looks like a secret, which the daily commit would leave out; take it out"
	mapSecretRule = "the map's line for the page would hold what looks like a secret; reword the title, the description or the repos"
)

// page is a system page as Write saves it.
type page struct {
	slug, title, description string
	actor                    string // generated.by
	sources, repos, aliases  []string
	asOf                     string
	body                     string // the sections, without the line code adds
}

// check reads a page as an agent wrote it. exists reports whether another
// system page exists, for links. It returns the page and what it refuses,
// each problem naming its lines and never their text.
func check(slug, text string, now time.Time, exists func(string) bool) (page, []string) {
	pg := page{slug: slug}
	var p problems
	if len(text) > maxPage {
		p.add(0, fmt.Sprintf("the page is over %d KB (%d bytes)", maxPage>>10, maxPage))
		return pg, p.list()
	}
	// CRLF line endings are refused once, for the page, and the lines are
	// checked as if they ended in LF.
	if strings.Contains(text, "\r") {
		p.add(0, "carriage returns; write the page with LF line endings")
		text = strings.ReplaceAll(text, "\r\n", "\n")
	}
	lines := strings.Split(text, "\n")
	for i, l := range lines {
		checkLine(i+1, l, &p)
	}
	end := slices.Index(lines[1:], "---") + 1
	if lines[0] != "---" || end == 0 {
		p.add(1, "a page starts with its frontmatter between two --- lines")
		return pg, p.list()
	}
	keys := checkFront(&pg, lines[1:end], 2, now, &p)
	pg.body = checkBody(lines[end+1:], end+2, slug, exists, &p)
	for _, n := range wikigit.SecretLines(text) {
		p.add(n, secretRule)
	}
	checkSaved(pg, keys, now, &p)
	return pg, p.list()
}

// checkSaved scans the rest of what the daily commit scans: the frontmatter
// as okf saves it (written again from the values, quoted its way, with the
// repos as project keys and the generated line it adds) and the page's lines
// on the map and in the folder's index. A flagged frontmatter line is named
// by the line keys gives for its key, and a flagged map or index line by the
// lines of the title, the description and the repos. The body is saved line
// for line as written, so the scan of the page's own lines covers it.
func checkSaved(pg page, keys map[string]int, now time.Time, p *problems) {
	front, _, _ := strings.Cut(strings.TrimPrefix(pg.saved(now), "---\n"), "\n---\n")
	lines := strings.Split(front, "\n")
	for _, n := range wikigit.SecretLines(front) {
		i := n - 1
		for i > 0 && strings.HasPrefix(lines[i], " ") {
			i-- // a list item; its key starts an earlier line
		}
		key, _, _ := strings.Cut(lines[i], ":")
		p.add(keys[key], secretRule)
	}
	e := pg.entry()
	if wikigit.SecretLines(e.line()+"\n"+e.indexLine()) != nil {
		for _, key := range []string{"title", "description", "repos"} {
			p.add(keys[key], mapSecretRule)
		}
	}
}

// checkLine refuses what no line of a page may hold, including the marker
// eva-md show prints for text it redacts, which a page read back and written
// again would otherwise keep in place of that text.
func checkLine(n int, l string, p *problems) {
	if !utf8.ValidString(l) {
		p.add(n, "text that is not UTF-8")
		return
	}
	if strings.Contains(l, "[REDACTED:") {
		p.add(n, "a [REDACTED:...] marker, which eva-md show prints in place of text; put back the text from its source")
	}
	checkText(n, l, p)
}

// checkText refuses characters and markup that would hide text: control and
// format characters, HTML comments, which the brief strips, and code fences.
// A quoted value is checked again once unquoted.
func checkText(n int, s string, p *problems) bool {
	ok := true
	if strings.ContainsFunc(s, hidden) {
		p.add(n, "a control or invisible character, such as a tab, a zero-width space or joiner (some emoji hold one), a soft hyphen or a direction mark")
		ok = false
	}
	if strings.Contains(s, "<!--") {
		p.add(n, "an HTML comment, which the brief hides; write the text plainly")
		ok = false
	}
	if fenceRun.MatchString(s) {
		p.add(n, "a code fence; write identifiers in single backticks")
		ok = false
	}
	return ok
}

// hidden reports a character a page may not hold: a control character, tabs
// and line breaks included, a format character such as a zero-width joiner
// or a direction mark, or a line or paragraph separator.
func hidden(r rune) bool {
	return unicode.IsControl(r) || unicode.In(r, unicode.Cf, unicode.Zl, unicode.Zp)
}

// checkBody checks the sections, from line first, and returns the body as
// Write saves it: without blank lines around it and without the line code
// adds, which a page read back with eva-md show carries.
func checkBody(lines []string, first int, self string, exists func(string) bool, p *problems) string {
	current, next, at := -1, 0, 0
	var summary, kept []string
	for i, l := range lines {
		n := first + i
		if strings.TrimSpace(l) == MapLink {
			continue
		}
		kept = append(kept, l)
		level, name, heading := atx(l)
		index := slices.Index(sections, name)
		switch {
		case heading && level == 2 && strings.TrimRight(l, " ") == "## "+name && index >= next && (current >= 0 || index == 0):
			current, next = index, index+1
			if index == 0 {
				at = n
			}
		case heading && level == 3 && l[0] == '#' && current >= 0:
		case heading:
			p.add(n, headingRule)
		case current < 0 && strings.TrimSpace(l) != "":
			p.add(n, "text before ## Summary, which the body starts with")
		case current == 0:
			summary = append(summary, l)
		}
		if t := strings.TrimSpace(l); t != "" && (strings.Trim(t, "-") == "" || strings.Trim(t, "=") == "") {
			p.add(n, "a line of only - or =, which reads as a rule or a heading underline")
		}
		if freetext.SpeakerShaped(l) {
			p.add(n, "a line that reads as a speaker label, such as [user]")
		}
		checkLinks(n, l, self, exists, p)
	}
	s := strings.TrimSpace(strings.Join(summary, "\n"))
	switch {
	case at == 0:
		p.add(0, "the body has no ## Summary")
	case s == "":
		p.add(at, "## Summary is empty")
	case utf8.RuneCountInString(s) > maxSummary:
		p.add(at, fmt.Sprintf("## Summary is over %d characters", maxSummary))
	}
	blank := func(l string) bool { return strings.TrimSpace(l) == "" }
	for len(kept) > 0 && blank(kept[0]) {
		kept = kept[1:]
	}
	for len(kept) > 0 && blank(kept[len(kept)-1]) {
		kept = kept[:len(kept)-1]
	}
	return strings.Join(kept, "\n")
}

// atx returns the level and text of a markdown heading: up to 3 spaces, 1
// to 6 '#', then a space or the end of the line.
func atx(l string) (int, string, bool) {
	t := strings.TrimLeft(l, " ")
	n := len(t) - len(strings.TrimLeft(t, "#"))
	if len(l)-len(t) > 3 || n == 0 || n > 6 || len(t) > n && t[n] != ' ' {
		return 0, "", false
	}
	return n, strings.TrimSpace(t[n:]), true
}

// checkLinks refuses a link that is neither https:// nor to this page or a
// system page that exists: okf counts a broken link as an error. Markdown
// reads no link in a code span, but okf does, so its own pattern is checked
// on the whole line.
func checkLinks(n int, l, self string, exists func(string) bool, p *problems) {
	text := codeSpans(l)
	var targets []string
	for _, m := range inlineLink.FindAllStringSubmatch(text, -1) {
		targets = append(targets, m[1])
	}
	if m := refDef.FindStringSubmatch(text); m != nil {
		targets = append(targets, m[1])
	}
	for _, m := range autolink.FindAllStringSubmatch(text, -1) {
		targets = append(targets, m[1])
	}
	for _, t := range targets {
		if msg := judgeTarget(t, self, exists); msg != "" {
			p.add(n, msg)
		}
	}
	for _, m := range bareURL.FindAllStringSubmatch(text, -1) {
		if !strings.HasPrefix(strings.ToLower(m[1]), "https://") {
			p.add(n, linkRule)
		}
	}
	for _, m := range okfLink.FindAllStringSubmatch(l, -1) {
		if !okfExternal(m[1]) && !pageLink(m[1], self, exists) {
			p.add(n, pageLinkRule)
		}
	}
}

// judgeTarget words what is wrong with a link's target, if anything.
func judgeTarget(t, self string, exists func(string) bool) string {
	switch {
	case strings.HasPrefix(strings.ToLower(t), "https://"):
		u, err := url.Parse(t)
		if err != nil || u.Host == "" {
			return "a link whose https:// address can't be read"
		}
		if u.User != nil {
			return "a link with a user name or password in it"
		}
		return ""
	case t == "" || strings.HasPrefix(t, "/") || scheme.MatchString(t):
		return linkRule
	case !pageLink(t, self, exists):
		return pageLinkRule
	}
	return ""
}

// pageLink reports whether a relative link names this page or a system page
// that exists: <slug>.md, perhaps with a #section, or a #section alone.
func pageLink(t, self string, exists func(string) bool) bool {
	file, _, _ := strings.Cut(t, "#")
	if file == "" {
		return true
	}
	slug, ok := strings.CutSuffix(file, ".md")
	return ok && CheckSlug(slug) == nil && (slug == self || exists(slug))
}

// okfExternal reports a link okf never resolves (scope.go IsExternalLink).
func okfExternal(href string) bool {
	return strings.Contains(href, "://") || strings.HasPrefix(href, "@") || strings.HasPrefix(href, "user:") ||
		strings.HasPrefix(href, "system:") || strings.HasPrefix(href, "mailto:")
}

// codeSpans blanks out inline code, text between two equal runs of
// backticks, in which markdown reads no link.
func codeSpans(l string) string {
	b := []byte(l)
	run := func(i int) int {
		j := i
		for j < len(b) && b[j] == '`' {
			j++
		}
		return j - i
	}
	for i := 0; i < len(b); {
		if b[i] != '`' {
			i++
			continue
		}
		open := run(i)
		end := -1
		for k := i + open; k < len(b); {
			if b[k] != '`' {
				k++
				continue
			}
			if r := run(k); r == open {
				end = k + r
				break
			} else {
				k += r
			}
		}
		if end < 0 {
			i += open
			continue
		}
		for k := i; k < end; k++ {
			b[k] = ' '
		}
		i = end
	}
	return string(b)
}

// problems collects what a check refuses: each problem once, with the lines
// it was found on and never their text.
type problems struct {
	msgs  []string
	lines map[string][]int
}

// add records msg on line n, or on the page as a whole when n is 0.
func (p *problems) add(n int, msg string) {
	if p.lines == nil {
		p.lines = map[string][]int{}
	}
	lines, seen := p.lines[msg]
	if !seen {
		p.msgs = append(p.msgs, msg)
	}
	if n > 0 && !slices.Contains(lines, n) {
		lines = append(lines, n)
	}
	p.lines[msg] = lines
}

// list words the problems, those of the whole page first and the others in
// the order of their first line, at most maxProblems of them.
func (p *problems) list() []string {
	first := func(msg string) int {
		if lines := p.lines[msg]; len(lines) > 0 {
			return slices.Min(lines)
		}
		return 0
	}
	msgs := slices.Clone(p.msgs)
	slices.SortStableFunc(msgs, func(a, b string) int { return first(a) - first(b) })
	var out []string
	for _, msg := range msgs {
		lines := slices.Sorted(slices.Values(p.lines[msg]))
		switch len(lines) {
		case 0:
			out = append(out, msg)
		case 1:
			out = append(out, fmt.Sprintf("line %d: %s", lines[0], msg))
		default:
			out = append(out, fmt.Sprintf("lines %s: %s", numbers(lines), msg))
		}
	}
	if len(out) > maxProblems {
		out = append(out[:maxProblems], fmt.Sprintf("and %d more problems", len(out)-maxProblems))
	}
	return out
}

// numbers lists line numbers, at most 10 of them.
func numbers(lines []int) string {
	var parts []string
	for _, n := range lines[:min(len(lines), 10)] {
		parts = append(parts, fmt.Sprint(n))
	}
	s := strings.Join(parts, ", ")
	if len(lines) > 10 {
		s += fmt.Sprintf(" and %d more", len(lines)-10)
	}
	return s
}
