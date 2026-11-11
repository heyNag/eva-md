// Package v1page reads a v1 thread or rules page, as v1 writes it, into
// the scorer's Export, with a small parser of its own: it shares nothing
// with the code that wrote the page, so the scorer judges v1 by what the
// page says. No release contains it.
//
// On a thread page, the summary is a summary item; the items under
// "Decisions in force", "Next" and "Open questions" are active items of
// kind decision, next and open, an item eva-md kept under Open questions
// past the cap of decisions being a decision; Done items are retired next
// items; and each Timeline line is a dated line under its group's day. On
// a rules page, a rule is an active rule and a retired rule a retired one.
// Every item carries its tag, and each ref s:<sid8>#<line> becomes a
// source. Frontmatter is skipped, and so are the title, the last chat and
// any section a person added. A line it cannot read is an error that
// names its line number, never its text.
package v1page

import (
	"errors"
	"fmt"
	"regexp"
	"strings"
	"time"

	"github.com/heyNag/eva-md/internal/eval/scenario"
	"github.com/heyNag/eva-md/internal/eval/score"
	"github.com/heyNag/eva-md/internal/ledger/event"
)

// The page's own words.
const (
	nowStart = "<!-- eva:now:start -->"
	nowEnd   = "<!-- eva:now:end -->"

	proposed     = "Proposed (assistant): "
	unattributed = "Unattributed: "
	ownLine      = "eva-md: "
	overCap      = "decision over cap, kept"
)

// lineError is an error about one line, by its number.
func lineError(n int, why string) error { return fmt.Errorf("v1page: line %d: %s", n, why) }

// lines are a page's lines, numbered from 1, after any frontmatter.
type line struct {
	n    int
	text string
}

func pageLines(body string) ([]line, error) {
	raw := strings.Split(body, "\n")
	var out []line
	start := 0
	for start < len(raw) && strings.TrimSpace(raw[start]) == "" {
		start++
	}
	if start < len(raw) && strings.TrimRight(raw[start], " \t\r") == "---" {
		end := start + 1
		for end < len(raw) && strings.TrimRight(raw[end], " \t\r") != "---" {
			end++
		}
		if end == len(raw) {
			return nil, lineError(start+1, "the frontmatter is not closed")
		}
		start = end + 1
	} else {
		start = 0
	}
	for i := start; i < len(raw); i++ {
		out = append(out, line{n: i + 1, text: strings.TrimRight(raw[i], "\r")})
	}
	return out, nil
}

// heading returns a line's heading of exactly level hashes, trimmed, and
// whether it is one.
func heading(l string, level int) (string, bool) {
	t := strings.TrimRight(l, " \t")
	prefix := strings.Repeat("#", level)
	if t != prefix && !strings.HasPrefix(t, prefix+" ") {
		return "", false
	}
	return strings.TrimSpace(t[level:]), true
}

// ReadThread reads a v1 thread page.
func ReadThread(body string) (score.Export, error) {
	lines, err := pageLines(body)
	if err != nil {
		return score.Export{}, err
	}
	var ex score.Export
	section, sub, date, fence := "", "", "", ""
	inNow, sawNow, sawStart := false, false, false
	var summary []string
	flushSummary := func() {
		if s := strings.TrimSpace(strings.Join(summary, "\n")); s != "" {
			ex.Items = append(ex.Items, score.Item{Kind: score.KindSummary, State: score.StateActive, Text: s})
		}
		summary = nil
	}
	for _, l := range lines {
		t := strings.TrimSpace(l.text)
		switch {
		case inNow && t == nowEnd:
			flushSummary()
			inNow = false
			continue
		case inNow:
			if h, ok := heading(t, 3); ok {
				flushSummary()
				switch h {
				case "Last chat", "Done", "Next", "Open questions", "Decisions in force":
					sub = h
				default:
					return score.Export{}, lineError(l.n, "an unknown heading inside the Now")
				}
				continue
			}
			if anyHeading.MatchString(t) {
				return score.Export{}, lineError(l.n, "a heading inside the Now")
			}
			if sub == "" {
				summary = append(summary, l.text)
				continue
			}
			if t == "" || sub == "Last chat" {
				continue
			}
			it, err := readItem(t)
			if err != nil {
				return score.Export{}, lineError(l.n, err.Error())
			}
			it.Kind, it.State = scenario.KindDecision, score.StateActive
			switch sub {
			case "Done":
				it.Kind, it.State = scenario.KindNext, score.StateRetired
			case "Next":
				it.Kind = scenario.KindNext
			case "Open questions":
				it.Kind = scenario.KindOpen
				if code, text, ok := strings.Cut(strings.TrimPrefix(it.Text, ownLine), ": "); ok && strings.HasPrefix(it.Text, ownLine) && code == overCap {
					it.Kind, it.Text = scenario.KindDecision, text
				}
			}
			it.Text = unlabel(it.Text)
			ex.Items = append(ex.Items, it)
			continue
		}
		if section != "Now" && section != "Timeline" && section != "" {
			if _, ok := heading(l.text, 2); !ok || fence != "" {
				fence = nextFence(fence, l.text)
				continue
			}
		}
		if h, ok := heading(l.text, 2); ok {
			section, date, fence = h, "", ""
			if h == "Now" {
				if sawNow {
					return score.Export{}, lineError(l.n, "the Now is repeated")
				}
				sawNow = true
			}
			continue
		}
		if t == "" {
			continue
		}
		switch section {
		case "":
			if _, ok := heading(l.text, 1); ok {
				continue
			}
			return score.Export{}, lineError(l.n, "text before the first section")
		case "Now":
			if t != nowStart || sawStart {
				return score.Export{}, lineError(l.n, "text outside the Now's markers")
			}
			inNow, sawStart, sub = true, true, ""
		case "Timeline":
			if h, ok := heading(l.text, 3); ok {
				if _, err := time.Parse(time.DateOnly, h); err != nil {
					return score.Export{}, lineError(l.n, "a Timeline group is not a day")
				}
				date = h
				continue
			}
			if date == "" {
				return score.Export{}, lineError(l.n, "a Timeline line before the first day")
			}
			it, err := readTimeline(t)
			if err != nil {
				return score.Export{}, lineError(l.n, err.Error())
			}
			it.Date = date
			ex.Items = append(ex.Items, it)
		}
	}
	if inNow {
		return score.Export{}, errors.New("v1page: the Now's end marker is missing")
	}
	return ex, nil
}

// ReadRules reads a v1 rules page.
func ReadRules(body string) (score.Export, error) {
	lines, err := pageLines(body)
	if err != nil {
		return score.Export{}, err
	}
	var ex score.Export
	section, fence := "", ""
	for _, l := range lines {
		if h, ok := heading(l.text, 2); ok && fence == "" {
			section = h
			continue
		}
		if section != "" && section != "Rules" && section != "Retired rules" {
			fence = nextFence(fence, l.text)
			continue
		}
		t := strings.TrimSpace(l.text)
		if t == "" {
			continue
		}
		state := score.StateActive
		switch section {
		case "":
			if _, ok := heading(l.text, 1); ok {
				continue
			}
			return score.Export{}, lineError(l.n, "text before the first section")
		case "Rules":
		case "Retired rules":
			state = score.StateRetired
		}
		it, err := readItem(t)
		if err != nil {
			return score.Export{}, lineError(l.n, err.Error())
		}
		it.Kind, it.State = scenario.KindRule, state
		ex.Items = append(ex.Items, it)
	}
	return ex, nil
}

// unlabel drops the label v1 writes before an item's text.
func unlabel(s string) string {
	for _, p := range []string{proposed, unattributed} {
		if rest, ok := strings.CutPrefix(s, p); ok {
			return rest
		}
	}
	if rest, ok := strings.CutPrefix(s, ownLine); ok {
		if _, text, ok := strings.Cut(rest, ": "); ok {
			return text
		}
	}
	return s
}

// dropComment removes the hidden comment that ends a line, if any.
func dropComment(s string) string {
	if a := strings.LastIndex(s, "<!--"); a >= 0 && strings.HasSuffix(s, "-->") {
		return strings.TrimSpace(s[:a])
	}
	return s
}

var anyHeading = regexp.MustCompile(`^#{1,6}(?:\s|$)`)

// nextFence is the fence open after line l, given the one open before it:
// a person's section may hold a fenced block, inside which a "## " line
// starts no section.
func nextFence(open, l string) string {
	t := strings.TrimLeft(l, " ")
	if len(l)-len(t) > 3 || len(t) < 3 || (t[0] != '`' && t[0] != '~') {
		return open
	}
	n := 0
	for n < len(t) && t[n] == t[0] {
		n++
	}
	switch {
	case n < 3:
		return open
	case open == "":
		return t[:n]
	case t[0] == open[0] && n >= len(open) && strings.TrimSpace(t[n:]) == "":
		return ""
	}
	return open
}

var tagToken = regexp.MustCompile(`^\[([\pL\pN_-]+)\]$`)

// readItem reads "- <text> [<tag>] <refs> <comment>".
func readItem(t string) (score.Item, error) {
	rest, ok := strings.CutPrefix(t, "- ")
	if !ok {
		return score.Item{}, errors.New("not an item")
	}
	fields := strings.Fields(dropComment(rest))
	at := -1
	for i := len(fields) - 1; i >= 0; i-- {
		if tagToken.MatchString(fields[i]) {
			at = i
			break
		}
	}
	if at < 0 {
		return score.Item{}, errors.New("an item without a tag")
	}
	it := score.Item{Authority: tagToken.FindStringSubmatch(fields[at])[1], Text: strings.Join(fields[:at], " ")}
	for _, f := range fields[at+1:] {
		src, err := source(f)
		if err != nil {
			return score.Item{}, err
		}
		it.Sources = append(it.Sources, src)
	}
	return it, nil
}

var lineHead = regexp.MustCompile(`^- ([\pL\pN_-]*) ?(?:\[([\pL\pN_-]+)\])?:(?: |$)`)

// readTimeline reads "- <word> [<tag>]: <text> <refs> <comment>".
func readTimeline(t string) (score.Item, error) {
	m := lineHead.FindStringSubmatchIndex(t)
	if m == nil || m[2] == m[3] && m[4] < 0 {
		return score.Item{}, errors.New("not a Timeline line")
	}
	it := score.Item{Kind: scenario.KindLog, State: score.StateLog}
	if m[4] >= 0 {
		it.Authority = t[m[4]:m[5]]
	}
	fields := strings.Fields(dropComment(t[m[1]:]))
	end := len(fields)
	for end > 0 {
		if _, err := source(fields[end-1]); err != nil {
			break
		}
		end--
	}
	for _, f := range fields[end:] {
		src, err := source(f)
		if err != nil {
			return score.Item{}, err
		}
		it.Sources = append(it.Sources, src)
	}
	it.Text = strings.Join(fields[:end], " ")
	return it, nil
}

// source reads a ref s:<sid8>#<line>.
func source(f string) (score.Source, error) {
	r, err := event.ParseRef(f)
	if err != nil || !r.Legacy || r.First < 1 {
		return score.Source{}, errors.New("a ref is not s:<session>#<line>")
	}
	return score.Source{Session: r.Session, Seq: r.First}, nil
}
