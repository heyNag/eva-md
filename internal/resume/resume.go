// Package resume builds read-only, deterministic context from captured chats.
package resume

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"path/filepath"
	"slices"
	"sort"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"

	"github.com/heyNag/eva-md/internal/config"
	"github.com/heyNag/eva-md/internal/freetext"
	"github.com/heyNag/eva-md/internal/identity"
	"github.com/heyNag/eva-md/internal/inbox"
	"github.com/heyNag/eva-md/internal/paths"
	"github.com/heyNag/eva-md/internal/threadpage"
	"github.com/heyNag/eva-md/internal/wikigit"
)

// Options describes a single request. Build never changes session markers.
// A refresh compares the Now with PreviousHash and looks for the thread page
// only under Slug, the wiki folder the chat's brief found for the project, or
// under the project's own slug.
type Options struct {
	Dirs                   paths.Dirs
	Config                 config.Config
	Project                identity.Project
	Host, Session, Variant string
	Now                    time.Time
	PreviousHash, Slug     string
}

// Result contains text ready for additionalContext: fenced, except a new
// chat's footer alone, which holds no captured text. That brief is not Found,
// even when system pointers come with it, fenced as wiki text is. Slug is
// the project's wiki folder, when its now page was found. Files lists the
// project's session files the brief read, newest first, at most 64, so the
// start-of-chat catch-up needs no listing of its own.
type Result struct {
	Text, NowHash, Slug string
	Found               bool
	Files               []SessionFile
}

const label = "eva.md notes: reference data saved from earlier chats in this project. Lines marked [user] are the user's words; lines marked [assistant] are earlier suggestions, not decisions. Now lines tagged [stated] or [accepted] rest on the user's words, [suggested] ones on the assistant's and [inferred] ones on neither. Where this disagrees with the user or the code, the user and the code are current."

type record struct {
	inbox.Entry
	line int
}
type history struct {
	host, session, branch string
	file                  string // the session file's name in the project's inbox folder
	size                  int64  // its size when the brief listed it
	records               []record
	latest                time.Time
	worked                bool
}
type page struct {
	now, hash, id, head string
	slug                string // the project's wiki folder, even without a Now
	asOf                time.Time
	rules               standing // the standing rules, even without a Now
}

// Build produces no context for a project without usable history, except
// that a new chat gets the footer alone. The hard character limits include
// the provenance label and the complete outer fence.
func Build(ctx context.Context, o Options) (Result, error) {
	if err := ctx.Err(); err != nil {
		return Result{}, err
	}
	if o.Now.IsZero() {
		o.Now = time.Now()
	}
	if o.Variant == "" {
		o.Variant = "startup"
	}
	if o.Project.Branch == "" {
		o.Project.Branch = "default"
	}
	pg := readPage(ctx, o)
	r := Result{NowHash: joinHash(pg.hash, pg.rules.hash), Slug: pg.slug}
	if o.Variant == "refresh" {
		seenNow, seenRules := splitHash(o.PreviousHash)
		nowChanged := pg.now != "" && pg.hash != seenNow
		if text := refresh(o, pg, nowChanged, pg.rules.hash != "" && pg.rules.hash != seenRules); text != "" {
			r.Text, r.Found = text, true
		}
		return r, nil
	}
	all, files, err := readHistories(ctx, o)
	if err != nil {
		return r, err
	}
	r.Files = files
	resumed := o.Variant == "resume" || o.Variant == "fork"
	var chosen *history
	var current *history
	for i := range all {
		h := &all[i]
		if h.host == o.Host && h.session == o.Session {
			if h.branch == o.Project.Branch {
				current = h
			}
			if o.Variant == "compact" && h.branch == o.Project.Branch {
				chosen = h
			}
			continue
		}
		if chosen == nil && o.Variant != "compact" && h.branch == o.Project.Branch && useful(*h) {
			chosen = h
		}
	}
	// Lineage serves only a new chat (startup or clear) on a branch with
	// neither a previous chat nor a Now page of its own. A resumed or forked
	// chat already holds its conversation, and another branch's Now must never
	// stand in for this branch's.
	lineage := false
	if chosen == nil && (o.Variant == "startup" || o.Variant == "clear") && pg.now == "" {
		for i := range all {
			h := &all[i]
			if h.host == o.Host && h.session == o.Session {
				continue
			}
			if useful(*h) {
				chosen = h
				lineage = h.branch != o.Project.Branch
				break
			}
		}
	}
	if chosen == nil && pg.now == "" {
		// A resumed or forked chat may still report live git when the project
		// has earlier work. That check uses no other branch's page or chat.
		if !resumed || !earlierWork(all, o) {
			// A new chat with nothing to resume still learns how to search
			// the other projects' chats, and which system pages are the
			// project's.
			if o.Variant == "startup" || o.Variant == "clear" {
				r.Text = clean(firstFooter(o))
				sys, rules := systemPointers(ctx, o), rulesBlock(o, pg.rules, rulesHead)
				if sys != "" || rules != "" {
					cap, _, _ := budgets(o.Host, o.Variant, true)
					r.Text = wrap(join(rules, sys, r.Text), cap)
				}
			}
			return r, nil
		}
	}
	if chosen != nil && lineage {
		po := o
		po.Project.Branch = chosen.branch
		pg = readPage(ctx, po)
		r.NowHash = joinHash(pg.hash, pg.rules.hash)
	}
	if resumed {
		r.Text = changes(ctx, o, all, current, pg)
		r.Found = r.Text != ""
		return r, nil
	}
	r.Text, r.Found = brief(ctx, o, all, chosen, pg, lineage), true
	return r, nil
}

// The hard limits of the briefs that budgets does not size, the same on both
// hosts.
const (
	resumeCap  = 1500
	refreshCap = 2500
)

// budgets returns a brief's hard cap with its Now and exchange budgets. They
// were set for a 7,000-character brief (4,000 compact) and scale with each
// host's cap: Claude limits hook context to 10,000 characters, and the Codex
// plugin to 3,000 tokens. Without a fresh Now the exchanges get its room.
func budgets(host, variant string, thin bool) (cap, now, exchange int) {
	if variant == "compact" {
		cap = 4500
		if host == "claude-code" {
			cap = 6000
		}
		return cap, cap * 1400 / 4000, cap * 1500 / 4000
	}
	cap = 6000
	if host == "claude-code" {
		cap = 8000
	}
	if thin {
		return cap, cap * 1500 / 7000, cap * 4000 / 7000
	}
	return cap, cap * 2800 / 7000, cap * 2700 / 7000
}

// refresh shows what changed since this chat last saw it: the standing
// rules, then the Now, by priority. It is "" when neither did.
func refresh(o Options, pg page, nowChanged, rulesChanged bool) string {
	rules := ""
	if rulesChanged {
		rules = rulesBlock(o, pg.rules, changedRulesHead)
	}
	if !nowChanged {
		if rules == "" {
			return ""
		}
		return fence(rules, refreshCap)
	}
	head := "Now"
	if !pg.asOf.IsZero() {
		head += " (as of " + pg.asOf.Format(time.RFC3339) + ")"
	}
	head += ", changed since it was last shown in this chat:"
	now := shown(pg.now)
	room := refreshCap - overhead(now) - runes(head) - 1
	if rules != "" {
		room -= runes(rules) + 2
	}
	return fence(join(rules, head+"\n"+renderNow(now, room, pg.id)), refreshCap)
}

// changes is a resumed or forked chat's brief: only what changed since this
// chat last worked, or nothing.
func changes(ctx context.Context, o Options, all []history, current *history, pg page) string {
	git := gitSummary(ctx, o.Project, pg.head)
	var parts []string
	baseline := time.Time{}
	if current != nil {
		for _, entry := range current.records {
			if entry.Kind == "prompt" || entry.Kind == "reply" || entry.Kind == "tool" || entry.Kind == "plan" {
				if ts := parseTime(entry.TS); ts.After(baseline) {
					baseline = ts
				}
			}
		}
	}
	seenNow, seenRules := splitHash(o.PreviousHash)
	if pg.rules.hash != "" && pg.rules.hash != seenRules {
		if rules := rulesBlock(o, pg.rules, changedRulesHead); rules != "" {
			parts = append(parts, rules)
		}
	}
	if pg.now != "" && pg.hash != seenNow && (baseline.IsZero() || pg.asOf.After(baseline)) {
		parts = append(parts, "Updated Now:\n"+renderNow(shown(pg.now), 700, pg.id))
	}
	for _, h := range all {
		if h.host == o.Host && h.session == o.Session || h.branch != o.Project.Branch || !useful(h) {
			continue
		}
		if !baseline.IsZero() && h.latest.After(baseline) {
			parts = append(parts, fmt.Sprintf("Other chat: %s at %s (%s)", h.host, h.latest.Format(time.RFC3339), lastActivity(h)))
		}
	}
	if git.changed {
		parts = append(parts, git.text)
	}
	if len(parts) == 0 {
		return ""
	}
	return fence(unforge(strings.Join(parts, "\n")), resumeCap)
}

// exchangesHead introduces the quoted messages.
const exchangesHead = "Captured exchanges (verbatim, oldest first):\n"

// brief assembles a startup, clear or compact brief. The footer and the
// newest exchange are reserved first, so the cap can't cut them: the newest
// message keeps its share and the one before it previousShare. The system
// pointers are reserved next, at most systemsCap. The free text is fitted
// into what is left, and older messages take what remains, up to their
// budget.
func brief(ctx context.Context, o Options, all []history, chosen *history, pg page, lineage bool) string {
	compact := o.Variant == "compact"
	stale := chosen != nil && (pg.asOf.IsZero() || pg.asOf.Before(workedAt(*chosen)))
	cap, nowCap, exchangeCap := budgets(o.Host, o.Variant, stale || pg.now == "")
	// First prompts come from bounded head reads: the chosen chat's and, in a
	// new chat, those of up to 3 earlier chats on this branch.
	var peers []*history
	if !compact {
		peers = earlier(all, o, chosen)
	}
	reads := peers
	if chosen != nil {
		reads = append([]*history{chosen}, peers...)
	}
	firsts := firstPrompts(ctx, o, reads)
	git := gitSummary(ctx, o.Project, pg.head)
	// Everything but the exchanges is free text: it may not write a speaker
	// header, and each part is measured as the fence will show it.
	var opening record
	var openText, plan, tools, chats, branches string
	var records []record
	if chosen != nil {
		if opening = firsts[0]; opening.line > 0 {
			openText = shown(openingLine(opening))
		}
		if p := latestPlan(*chosen, pg.asOf); p != "" {
			plan = shown("Latest plan:\n" + cut(p, 600))
		}
		tools = shown(activity(*chosen, o.Project.Root))
		records = quotable(*chosen, compact)
	}
	if !compact {
		chats = shown(earlierChats(peers, firsts[len(reads)-len(peers):]))
		branches = shown(otherBranches(all, o.Project.Branch))
	}
	head, gitText, foot := shown(header(o, chosen, lineage, updatePending(o, chosen, lineage))), shown(git.text), shown(footer(o, pg))
	// The pointers stand just before the footer. They are wiki text, cleaned
	// already, which cleaning again as free text could change. The standing
	// rules come first, and like them and the footer are reserved.
	sys, rules := systemPointers(ctx, o), rulesBlock(o, pg.rules, rulesHead)
	reserved := runes(foot) + 2
	if sys != "" {
		reserved += runes(sys) + 2
	}
	if rules != "" {
		reserved += runes(rules) + 2
	}
	room := cap - overhead("") - reserved
	// newest holds the newest exchange as reserved: the newest message and the
	// one before it, usually the question it answers.
	newestCap, newest := 0, ""
	if len(records) > 0 {
		last := records[len(records)-1]
		newestCap = newestShare(records, exchangeCap)
		newest = quoteHeader(last) + message(last, newestCap)
		if len(records) > 1 {
			prev := records[len(records)-2]
			newest = quoteHeader(prev) + message(prev, previousShare) + "\n\n" + newest
		}
		room -= runes(exchangesHead) + runes(newest) + 2
	}
	// The Now takes its budget after the header, the opening request and live
	// git, and in a compact brief after the tool activity too, which says what
	// this chat was doing. The tool activity of a new chat, the plan, the
	// earlier chats and the other branches share what is left, in that order,
	// each whole or left out.
	core := join(head, openText, gitText)
	if compact {
		core = join(core, tools)
	}
	now := "No summarized Now page yet; the captured exchanges below are the available history."
	if pg.now != "" {
		heading := shown(nowHeading(pg, stale, lineage, chosen))
		now = heading + ":\n" + renderNow(shown(pg.now), max(min(nowCap, room-runes(core)-runes(heading)-4), 0), pg.id)
	}
	left := room - runes(join(core, now))
	keep := func(part string) string {
		if part == "" || runes(part)+2 > left {
			return ""
		}
		left -= runes(part) + 2
		return part
	}
	if !compact {
		tools = keep(tools)
	}
	plan, chats, branches = keep(plan), keep(chats), keep(branches)
	top := func() string {
		// Should the rest still not fit, it is cut from its end.
		return cut(join(head, openText, gitText, now, plan, tools, chats, branches), max(room, 0))
	}
	text := top()
	var exText string
	if len(records) > 0 {
		var lines map[int]bool
		exText, lines = exchanges(records, min(exchangeCap, cap-overhead("")-runes(text)-reserved-runes(exchangesHead)-2), newestCap)
		// The opening request is left out when an exchange already shows it.
		if lines[opening.line] {
			openText = ""
			text = top()
		}
	}
	assemble := func() string {
		s := text
		if len(records) > 0 {
			s = join(s, exchangesHead+exText)
		}
		return join(rules, clean(s), sys, clean(foot))
	}
	content := assemble()
	// Cleaning the whole and a longer fence can still add a little. Older
	// messages give way first, then the free text; never the newest
	// exchange, the standing rules, the system pointers or the footer.
	for over := runes(content) + overhead(content) - cap; over > 0; over = runes(content) + overhead(content) - cap {
		if older := runes(exText) - runes(newest); older > 0 {
			exText, _ = exchanges(records, runes(exText)-min(over, older), newestCap)
		} else if text != "" {
			text = cut(text, max(runes(text)-over, 0))
		} else {
			break
		}
		content = assemble()
	}
	return wrap(content, cap)
}

// join joins the parts that are not empty with blank lines between them.
func join(parts ...string) string {
	var kept []string
	for _, p := range parts {
		if p != "" {
			kept = append(kept, p)
		}
	}
	return strings.Join(kept, "\n\n")
}

func useful(h history) bool {
	if h.worked {
		return true
	}
	u, a := 0, 0
	for _, r := range h.records {
		if r.Kind == "tool" || r.Kind == "plan" {
			return true
		}
		if r.Kind == "prompt" && r.Role == "user" {
			u++
		}
		if r.Kind == "reply" && r.Role == "assistant" {
			a++
		}
	}
	return u > 2 && a > 2
}

func ending(h history) string {
	for i := len(h.records) - 1; i >= 0; i-- {
		r := h.records[i]
		switch {
		case r.Kind == "event" && strings.HasPrefix(r.Text, "stop_failure"):
			return "no reply (" + oneLine(r.Text, 200) + ")"
		case r.Kind == "event" && r.Text == "interrupt":
			return "interrupted"
		case r.Kind == "event" && r.Text == "compact":
			return "compacted"
		case r.Kind == "reply":
			return "reply recorded"
		case r.Kind == "prompt":
			return "no reply recorded"
		}
	}
	return "no reply recorded"
}

// workedAt is the time of the newest exchange or piece of work. Events such
// as the session_start written just before a brief don't count.
func workedAt(h history) time.Time {
	var latest time.Time
	for _, r := range h.records {
		if r.Kind == "prompt" || r.Kind == "reply" || r.Kind == "tool" || r.Kind == "plan" {
			if ts := parseTime(r.TS); ts.After(latest) {
				latest = ts
			}
		}
	}
	return latest
}

// lastActivity describes another chat in one line. Its message text is
// collapsed, so a quoted line can never start a line of the brief.
func lastActivity(h history) string {
	for i := len(h.records) - 1; i >= 0; i-- {
		r := h.records[i]
		if r.Kind == "prompt" || r.Kind == "reply" || r.Kind == "tool" {
			return cut(r.Kind+": "+oneLine(r.Text, 160), 160)
		}
	}
	return "activity recorded"
}

// quotable returns the messages a brief quotes, oldest first: the user's
// prompts, the assistant's replies and host summaries. A compact brief keeps
// the last 4, this chat's last 2 exchanges.
func quotable(h history, compact bool) []record {
	var records []record
	for _, r := range h.records {
		if r.Role == "user" && r.Kind == "prompt" || r.Role == "assistant" && r.Kind == "reply" || r.Role == "host" && r.Kind == "host_summary" {
			records = append(records, r)
		}
	}
	if compact && len(records) > 4 {
		records = records[len(records)-4:]
	}
	return records
}

// newestShare is how many runes the newest message keeps: half the exchange
// budget, leaving room for the question before it, or all of it when it is
// the only message.
func newestShare(records []record, budget int) int {
	n := budget - runes(quoteHeader(records[len(records)-1])) - 2
	if len(records) > 1 && n > budget/2 {
		n = budget / 2
	}
	return n
}

// previousShare is how many runes the message before the newest keeps
// whatever the budget, so the newest exchange keeps both its sides.
const previousShare = 300

// exchanges quotes messages newest first within budget runes and returns them
// oldest first, with the lines it quoted. The newest message keeps newest
// runes and the one before it at least previousShare, whatever the budget;
// older ones share what is left.
func exchanges(records []record, budget, newest int) (string, map[int]bool) {
	lines := map[int]bool{}
	var parts []string
	remaining := budget
	for i := len(records) - 1; i >= 0; i-- {
		r := records[i]
		head := quoteHeader(r)
		n := newest
		if i < len(records)-1 {
			least := 0
			if i == len(records)-2 {
				least = previousShare
			}
			if remaining <= 100 && least == 0 {
				break
			}
			// Keep room for the user's question next to the assistant's answer.
			n = remaining - runes(head) - 2
			if i > 0 && n > budget/2 {
				n = budget / 2
			}
			n = max(n, least)
		}
		part := head + message(r, n)
		parts = append(parts, part)
		lines[r.line] = true
		remaining -= runes(part) + 2
	}
	slices.Reverse(parts)
	return strings.Join(parts, "\n\n"), lines
}

// quoteHeader is the speaker line above a quoted message. The speaker comes
// from the entry's role, never from its text.
func quoteHeader(r record) string {
	role := r.Role
	if role == "host" {
		role = "host summary"
	}
	return fmt.Sprintf("[%s] %s · %s\n", role, displayTime(r.TS), inbox.Ref(r.Session, r.line))
}

// message quotes r's text in at most n runes, cut in the middle around an
// eva-md show ref when it is longer. It is cleaned as the fence would clean
// it, so the length counted is the length shown.
func message(r record, n int) string {
	mark := "[… eva-md show " + inbox.Ref(r.Session, r.line) + " …]"
	q := clean(middleQuoted(r.Text, n, mark))
	// Cleaning can lengthen it, so cut that much more.
	for m := n; runes(q) > n && m > 0; {
		m -= runes(q) - n
		q = clean(middleQuoted(r.Text, m, mark))
	}
	return q
}

// indent starts every quoted message line, so only the brief's own speaker
// headers begin at column 0.
const indent = "  "

func quoted(s string) string {
	lines := strings.Split(s, "\n")
	for i := range lines {
		lines[i] = indent + lines[i]
	}
	return strings.Join(lines, "\n")
}

// The brief cleans free text as the update input does (internal/freetext):
// clean redacts it and removes control characters, escape quotes each line
// that would read as a speaker header, shown does both, and speakerShaped
// finds such a line.
func clean(s string) string          { return freetext.Clean(s) }
func escape(s string) string         { return freetext.Escape(s) }
func shown(s string) string          { return freetext.Shown(s) }
func speakerShaped(line string) bool { return freetext.SpeakerShaped(line) }

// redactedMark starts the marker redaction puts in place of a secret.
const redactedMark = "[REDACTED:"

// cleanPage cleans wiki page text as clean does, except that it judges
// identifiers as the daily commit's scan does (wikigit.SecretLines): a line
// that clean would redact, but that the scan passes, keeps what the scan
// reads as identifiers, such as a long branch name, so a page eva-md show
// prints can be written back. That holds only for a line that cleaning alone
// redacts as cleaning the whole did: the entropy rule reads words across
// line breaks, so a value a word on the line before makes suspect stays
// redacted. A line the scan flags is redacted, and so is the whole text when
// a redaction would span lines.
func cleanPage(s string) string {
	whole := clean(s)
	if whole == s {
		return s
	}
	lines, cleaned := strings.Split(s, "\n"), strings.Split(whole, "\n")
	if len(lines) != len(cleaned) {
		return whole
	}
	for i, l := range lines {
		if strings.Count(cleaned[i], redactedMark) > strings.Count(l, redactedMark) && !strings.ContainsAny(l, "\x1b\u009b") && clean(l) == cleaned[i] && wikigit.SecretLines(l) == nil {
			cleaned[i] = tidy(l)
		}
	}
	return strings.Join(cleaned, "\n")
}

// tidy cleans a line as clean does, without redacting it: it removes
// control characters other than tabs and invisible format characters, as
// redaction does first, turns line and paragraph separators into spaces, and
// quotes a line that starts like the brief's label. Clean removes an escape
// sequence whole, which tidy can't, so cleanPage tidies no line holding one.
func tidy(line string) string {
	line = strings.Map(func(r rune) rune {
		switch {
		case r == '\t':
		case unicode.IsControl(r) || unicode.In(r, unicode.Cf) || r == '\u034f':
			return -1
		case r == '\u2028' || r == '\u2029':
			return ' '
		}
		return r
	}, line)
	if strings.HasPrefix(strings.TrimSpace(line), "eva.md notes:") {
		line = "[quoted] " + line
	}
	return line
}

// unforge escapes each line of free text that would read as a speaker
// header. Speaker labels come only from entry fields: the brief writes them
// at column 0 and indents every quoted message line, so no other text inside
// the fence may begin a line with one. It checks the text as the fence will
// show it, so nothing the fence strips later (control, format or ANSI
// characters) can hide a tag; cleaning again in fence changes nothing.
func unforge(s string) string {
	if !strings.Contains(s, "[") {
		return s
	}
	return escape(clean(s))
}

// oneLine collapses every run of whitespace, line breaks included, to one
// space and keeps at most n runes.
func oneLine(s string, n int) string {
	var b strings.Builder
	runes := 0
	for f := range strings.FieldsSeq(s) {
		if runes > n {
			break
		}
		if b.Len() > 0 {
			b.WriteByte(' ')
			runes++
		}
		b.WriteString(f)
		runes += utf8.RuneCountInString(f)
	}
	return cut(b.String(), n)
}

func fence(s string, cap int) string { return wrap(clean(s), cap) }

// wrap fences text that is already clean under the label, in at most cap
// runes. Text that does not fit is cut at its end.
func wrap(s string, cap int) string {
	for {
		extra := overhead(s)
		if runes(s)+extra <= cap {
			f := strings.Repeat("`", longestRun(s)+1)
			return label + "\n" + f + "eva-memory\n" + s + "\n" + f
		}
		if extra >= cap {
			s = strings.ReplaceAll(s, "`", "ˋ")
			continue
		}
		s = cut(s, cap-extra)
	}
}

// longestRun is the longest run of backticks in s, at least 3, so the fence,
// one longer, is at least 4.
func longestRun(s string) int {
	longest, run := 3, 0
	for _, r := range s {
		if r == '`' {
			run++
			longest = max(longest, run)
		} else {
			run = 0
		}
	}
	return longest
}

// overhead is what wrap adds around s: the label, the two fence lines, the
// info string and the newlines.
func overhead(s string) int {
	return runes(label) + 2*(longestRun(s)+1) + len("\neva-memory\n\n")
}

func cut(s string, n int) string {
	r := []rune(s)
	if n <= 0 {
		return ""
	}
	if len(r) <= n {
		return s
	}
	if n == 1 {
		return "…"
	}
	return string(r[:n-1]) + "…"
}

// middleQuoted quotes text and, when that is longer than n runes, keeps its
// head and tail around mark, which gets a line of its own. The tail always
// starts the line after the mark, so it is indented there by position: a cut
// just after a quoted line's indent can't leave that line at column 0, where
// it would read as a speaker header.
func middleQuoted(text string, n int, mark string) string {
	q := quoted(text)
	r := []rune(q)
	if len(r) <= n {
		return q
	}
	join := []rune("\n" + mark + "\n" + indent)
	if n < len(join)+2 {
		return cut(q, n)
	}
	keep := n - len(join)
	return string(r[:keep/2]) + string(join) + string(r[len(r)-(keep-keep/2):])
}

// earlierWork reports whether any other chat in the project did real work.
func earlierWork(all []history, o Options) bool {
	for _, h := range all {
		if !(h.host == o.Host && h.session == o.Session) && useful(h) {
			return true
		}
	}
	return false
}
func hash(s string) string { sum := sha256.Sum256([]byte(s)); return hex.EncodeToString(sum[:]) }

// ThreadName maps a branch to its thread page's file name, as
// internal/threadpage names it.
func ThreadName(branch string) string { return threadpage.ThreadName(branch) }

func sortHistories(all []history) {
	sort.SliceStable(all, func(i, j int) bool {
		if all[i].latest.Equal(all[j].latest) {
			return all[i].host+all[i].session < all[j].host+all[j].session
		}
		return all[i].latest.After(all[j].latest)
	})
}
func safeProject(p identity.Project) string { name, _ := paths.SafeName(p.Key); return name }
func threadID(p identity.Project) string {
	if p.Kind == identity.KindGeneral {
		return "general/threads/default"
	}
	return filepath.ToSlash(filepath.Join("projects", p.Slug, "threads", ThreadName(p.Branch)))
}
