package resume

import (
	"fmt"
	"regexp"
	"slices"
	"strings"
	"unicode/utf8"
)

// comment matches a hidden comment, such as an item id (<!-- i:n3 -->), with
// the spaces before it.
var comment = regexp.MustCompile(`[ \t]*<!--[\s\S]*?-->`)

// stripComments removes every hidden comment from a Now, so item ids and
// dates are never measured or shown. Source refs stay.
func stripComments(s string) string {
	if !strings.Contains(s, "<!--") {
		return s
	}
	lines := strings.Split(comment.ReplaceAllString(s, ""), "\n")
	for i := range lines {
		lines[i] = strings.TrimRight(lines[i], " \t")
	}
	return strings.TrimSpace(strings.Join(lines, "\n"))
}

// The sections of a Now in the thread page format (see design.md, "Thread
// page"). The summary has no heading on the page.
const (
	nowSummary = iota
	nowLastChat
	nowDone
	nowNext
	nowQuestions
	nowDecisions
)

// nowHeadings names each section as the brief shows it.
var nowHeadings = [...]string{"Summary", "Last chat", "Done", "Next", "Open questions", "Decisions in force"}

// nowPriority is the order the brief shows the sections in: what is decided
// and what comes next before the summary, the last chat and what was done.
var nowPriority = [...]int{nowDecisions, nowNext, nowQuestions, nowSummary, nowLastChat, nowDone}

// parseNow splits a Now in the thread page format into lines by section: the
// summary's lines, each heading's items, and the last chat's lines. ok is
// false for any other text, such as a hand-written or placeholder page.
func parseNow(now string) (secs [6][]string, ok bool) {
	at, seen := nowSummary, [6]bool{}
	for _, line := range strings.Split(now, "\n") {
		line = strings.TrimRight(line, " \t")
		if strings.TrimSpace(line) == "" {
			continue
		}
		if strings.HasPrefix(line, "#") {
			name, heading := strings.CutPrefix(line, "### ")
			sec := slices.IndexFunc(nowHeadings[:], func(h string) bool { return strings.EqualFold(h, strings.TrimSpace(name)) })
			if !heading || sec <= nowSummary || seen[sec] {
				return secs, false
			}
			at, seen[sec], ok = sec, true, true
			continue
		}
		// Items are lines of their own. A wrapped item or loose text under a
		// list heading is not the format.
		if at != nowSummary && at != nowLastChat && !strings.HasPrefix(line, "- ") {
			return secs, false
		}
		secs[at] = append(secs[at], line)
	}
	return secs, ok
}

// renderNow shows a Now within budget runes, by priority. Next keeps at least
// a quarter of the budget and open questions 14% when they have items;
// decisions in force take the rest, newest first; room a section leaves flows
// to the next. Sections are trimmed by whole lines, never mid-line, each with
// one "N more" line naming the page. A Now in any other format is cut to the
// budget as it is.
func renderNow(now string, budget int, id string) string {
	secs, ok := parseNow(now)
	if !ok {
		return cut(now, budget)
	}
	// The page lists the newest decision last.
	slices.Reverse(secs[nowDecisions])
	more := func(n int) string { return fmt.Sprintf("%d more: %s#Now", n, id) }
	// size and floor count what a section costs whole, and with its heading
	// and a "more" line only, each with the newline that joins it.
	size := func(sec int) int {
		n := len("### "+nowHeadings[sec]) + 1
		for _, line := range secs[sec] {
			n += runes(line) + 1
		}
		return n
	}
	floor := func(sec int) int { return len("### "+nowHeadings[sec]) + 1 + runes(more(len(secs[sec]))) + 1 }
	var share [6]int
	if len(secs[nowNext]) > 0 {
		share[nowNext] = min(budget/4, size(nowNext))
	}
	if len(secs[nowQuestions]) > 0 {
		share[nowQuestions] = min(budget*14/100, size(nowQuestions))
	}
	rest := budget - share[nowNext] - share[nowQuestions]
	for _, sec := range []int{nowSummary, nowLastChat, nowDone} {
		if len(secs[sec]) > 0 {
			share[sec] = floor(sec)
			rest -= share[sec]
		}
	}
	share[nowDecisions] = max(rest, 0)
	var out []string
	left, spare := budget, 0
	for _, sec := range nowPriority {
		// An empty section passes its share on, as decisions in force do
		// before a thread has any.
		if len(secs[sec]) == 0 {
			spare += share[sec]
			continue
		}
		room := min(share[sec]+spare, left)
		used := 0
		if text := fill(nowHeadings[sec], secs[sec], room, more); text != "" {
			out = append(out, text)
			used = runes(text) + 1
		}
		spare, left = room-used, left-used
	}
	return strings.Join(out, "\n")
}

// fill keeps a section's lines in order while they fit in room runes with its
// heading and the newline that joins it, and says how many it left out. It
// returns nothing when not even the heading and that line fit.
func fill(heading string, lines []string, room int, more func(int) string) string {
	head := "### " + heading
	n := runes(head) + 1
	whole := n
	for _, line := range lines {
		whole += runes(line) + 1
	}
	if whole <= room {
		return head + "\n" + strings.Join(lines, "\n")
	}
	reserve := runes(more(len(lines))) + 1
	kept := []string{head}
	for _, line := range lines {
		if n+runes(line)+1+reserve > room {
			break
		}
		kept = append(kept, line)
		n += runes(line) + 1
	}
	if n+reserve > room {
		return ""
	}
	return strings.Join(append(kept, more(len(lines)-len(kept)+1)), "\n")
}

func runes(s string) int { return utf8.RuneCountInString(s) }
