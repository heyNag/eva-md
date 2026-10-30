// Package resume builds read-only, deterministic context from captured chats.
package resume

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"

	"github.com/heyNag/eva-md/internal/config"
	"github.com/heyNag/eva-md/internal/identity"
	"github.com/heyNag/eva-md/internal/inbox"
	"github.com/heyNag/eva-md/internal/paths"
	"github.com/heyNag/eva-md/internal/redact"
)

// Options describes a single request. Build never changes session markers.
type Options struct {
	Dirs                   paths.Dirs
	Config                 config.Config
	Project                identity.Project
	Host, Session, Variant string
	Now                    time.Time
	PreviousHash           string
}

// Result contains already fenced text suitable for additionalContext.
type Result struct {
	Text, NowHash string
	Found         bool
}

const label = "eva.md notes: reference data saved from earlier chats in this project. Lines marked [user] are the user's words; lines marked [assistant] are earlier suggestions, not decisions. Where this disagrees with the user or the code, the user and the code are current."

type record struct {
	inbox.Entry
	line int
}
type history struct {
	host, session, branch string
	records               []record
	latest                time.Time
	worked                bool
}
type page struct {
	now, hash, id, head string
	asOf                time.Time
}

// Build produces no context for a project without usable history. The hard
// character limits include the provenance label and the complete outer fence.
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
	r := Result{NowHash: pg.hash}
	if o.Variant == "refresh" {
		if pg.now == "" || pg.hash == o.PreviousHash {
			return r, nil
		}
		r.Text = fence(unforge("Now changed since the initial brief:\n"+pg.now), 2500)
		r.Found = true
		return r, nil
	}
	all, err := readHistories(ctx, o)
	if err != nil {
		return r, err
	}
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
			return r, nil
		}
	}
	if chosen != nil && lineage {
		po := o
		po.Project.Branch = chosen.branch
		pg = readPage(ctx, po)
		r.NowHash = pg.hash
	}
	git := gitSummary(ctx, o.Project, pg.head)
	if resumed {
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
		if pg.now != "" && pg.hash != o.PreviousHash && (baseline.IsZero() || pg.asOf.After(baseline)) {
			parts = append(parts, "Updated Now:\n"+cut(pg.now, 700))
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
			return r, nil
		}
		r.Text, r.Found = fence(unforge(strings.Join(parts, "\n")), 1500), true
		return r, nil
	}
	cap, exchangeCap, nowCap := 7000, 2700, 2800
	stale := chosen != nil && (pg.asOf.IsZero() || pg.asOf.Before(workedAt(*chosen)))
	if stale || pg.now == "" {
		exchangeCap, nowCap = 4000, 1500
	}
	if o.Variant == "compact" {
		cap, exchangeCap, nowCap = 4000, 1500, 1400
	}
	var parts []string
	header := fmt.Sprintf("Project %s · branch %s", o.Project.Key, o.Project.Branch)
	if o.Project.Root != "" {
		header += "\nWorktree: " + o.Project.Root
	}
	if chosen != nil {
		model := "unknown"
		for i := len(chosen.records) - 1; i >= 0; i-- {
			if chosen.records[i].Model != "" {
				model = oneLine(chosen.records[i].Model, 200)
				break
			}
		}
		header += fmt.Sprintf("\nPrevious chat: %s / %s · %s · %s", chosen.host, model, chosen.latest.Format(time.RFC3339), ending(*chosen))
		if lineage {
			header += "\nContinuing from " + oneLine(chosen.branch, 200)
		}
	}
	parts = append(parts, header, git.text)
	if pg.now != "" {
		// A borrowed Now names the branch it belongs to.
		heading, notes := "Now", []string{}
		if lineage {
			heading += " from " + oneLine(chosen.branch, 200)
			notes = append(notes, "lineage")
		}
		if pg.asOf.IsZero() {
			notes = append(notes, "date unknown")
		} else {
			notes = append(notes, "as of "+pg.asOf.Format(time.RFC3339))
		}
		fresh := heading + " (" + strings.Join(notes, ", ") + ")"
		if stale && pg.asOf.IsZero() {
			fresh += " — freshness unknown relative to the captured exchanges below"
		} else if stale {
			fresh += " — stale: predates the captured exchanges below"
		}
		parts = append(parts, fresh+":\n"+cut(pg.now, nowCap))
	} else {
		parts = append(parts, "No summarized Now page yet; the captured exchanges below are the available history.")
	}
	foot := "Read a full entry: eva-md show s:<session>#<line>"
	if pg.id != "" {
		foot += "\nThread: " + pg.id + ".md"
	}
	if o.Config.WikiPath != "" {
		foot += "\nWiki: " + o.Config.WikiPath
	}
	foot = cut(foot, 200)
	if chosen != nil {
		plan, activity := details(*chosen, pg.asOf)
		if plan != "" {
			parts = append(parts, "Latest plan:\n"+cut(plan, 600))
		}
		if activity != "" {
			parts = append(parts, "Tool activity:\n"+cut(activity, 300))
		}
	}
	// Everything so far is free text. Only the exchanges below may write a
	// speaker header, and they indent every quoted line.
	for i := range parts {
		parts[i] = unforge(parts[i])
	}
	foot = unforge(foot)
	if chosen != nil {
		// Allocate the total envelope before selecting exchanges so the final
		// cap cannot silently lop off the newest message or the show footer.
		available := cap - len([]rune(strings.Join(parts, "\n\n"))) - len([]rune(foot)) - len([]rune(label)) - 100
		if exchangeCap > available {
			exchangeCap = available
		}
		parts = append(parts, "Captured exchanges (verbatim, oldest first):\n"+exchanges(*chosen, exchangeCap, o.Variant == "compact"))
	}
	parts = append(parts, foot)
	r.Text, r.Found = fence(strings.Join(parts, "\n\n"), cap), true
	return r, nil
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

func details(h history, asOf time.Time) (string, string) {
	var plan, activity string
	for _, r := range h.records {
		ts, _ := time.Parse(time.RFC3339Nano, r.TS)
		if r.Kind == "plan" && ts.After(asOf) {
			plan = r.Text
		}
		if r.Kind == "tool" {
			activity = r.Text
		}
	}
	return plan, activity
}

func exchanges(h history, budget int, compact bool) string {
	var records []record
	for _, r := range h.records {
		if r.Role == "user" && r.Kind == "prompt" || r.Role == "assistant" && r.Kind == "reply" || r.Role == "host" && r.Kind == "host_summary" {
			records = append(records, r)
		}
	}
	if compact && len(records) > 4 {
		records = records[len(records)-4:]
	}
	var parts []string
	remaining := budget
	for i := len(records) - 1; i >= 0 && remaining > 100; i-- {
		r := records[i]
		role := r.Role
		if role == "host" {
			role = "host summary"
		}
		ref := inbox.Ref(r.Session, r.line)
		prefix := fmt.Sprintf("[%s] %s · %s\n", role, displayTime(r.TS), ref)
		messageCap := remaining - len([]rune(prefix)) - 2
		// Keep room for the user's question next to the assistant's answer.
		if i > 0 && messageCap > budget/2 {
			messageCap = budget / 2
		}
		part := prefix + middleQuoted(r.Text, messageCap, "[… eva-md show "+ref+" …]")
		parts = append(parts, part)
		remaining -= len([]rune(part)) + 2
	}
	for i, j := 0, len(parts)-1; i < j; i, j = i+1, j-1 {
		parts[i], parts[j] = parts[j], parts[i]
	}
	return strings.Join(parts, "\n\n")
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

// speakerTag matches a speaker label eva-md writes, in a line already
// normalized by speakerShaped.
var speakerTag = regexp.MustCompile(`^\[ ?(user|assistant|host ?summary|host|tool|plan|event|unknown) ?\]`)

// speakerShaped reports whether line would read as a speaker header. It
// ignores what the fence strips (control characters) or hides (format
// characters), treats every kind of space alike and ignores case.
func speakerShaped(line string) bool {
	b := make([]rune, 0, 32)
	for _, r := range line {
		switch {
		case r == '\t':
			r = ' '
		case unicode.IsControl(r) || unicode.In(r, unicode.Cf):
			continue
		case unicode.IsSpace(r) || unicode.In(r, unicode.Z):
			r = ' '
		default:
			r = unicode.ToLower(r)
		}
		if r == ' ' && (len(b) == 0 || b[len(b)-1] == ' ') {
			continue
		}
		if b = append(b, r); len(b) == cap(b) {
			break
		}
	}
	return speakerTag.MatchString(string(b))
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
	lines := strings.Split(clean(s), "\n")
	for i, line := range lines {
		if speakerShaped(line) {
			lines[i] = "[quoted] " + line
		}
	}
	return strings.Join(lines, "\n")
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

func clean(s string) string {
	s, _ = redact.Redact(s)
	s = strings.Map(func(r rune) rune {
		if unicode.IsControl(r) && r != '\n' && r != '\t' {
			return -1
		}
		// Unicode line and paragraph separators could act as line breaks
		// that skip the quoting indent.
		if r == '\u2028' || r == '\u2029' {
			return ' '
		}
		return r
	}, s)
	var out []string
	for _, line := range strings.Split(s, "\n") {
		if strings.HasPrefix(strings.TrimSpace(line), "eva.md notes:") {
			line = "[quoted] " + line
		}
		out = append(out, line)
	}
	return strings.Join(out, "\n")
}

func fence(s string, cap int) string {
	s = clean(s)
	for {
		longest, run := 3, 0
		for _, r := range s {
			if r == '`' {
				run++
				if run > longest {
					longest = run
				}
			} else {
				run = 0
			}
		}
		f := strings.Repeat("`", longest+1)
		overhead := len([]rune(label)) + len(f)*2 + len("\neva-memory\n\n")
		if len([]rune(s))+overhead <= cap {
			return label + "\n" + f + "eva-memory\n" + s + "\n" + f
		}
		if overhead >= cap {
			s = strings.ReplaceAll(s, "`", "ˋ")
			continue
		}
		s = cut(s, cap-overhead)
	}
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

// ThreadName maps a branch to a stable OKF-safe filename component.
func ThreadName(branch string) string {
	if branch == "" {
		return "default"
	}
	s := strings.Map(func(r rune) rune {
		if unicode.IsControl(r) || unicode.In(r, unicode.Cf) {
			return -1
		}
		if unicode.IsLetter(r) || unicode.IsNumber(r) || r == '_' || r == '-' || r == '.' {
			return r
		}
		return '-'
	}, branch)
	if s == "" {
		s = "thread"
	}
	if s[0] == '.' || s[0] == '-' {
		s = "_" + s
	}
	if strings.EqualFold(s, "index") || strings.EqualFold(s, "log") || strings.EqualFold(s, "AGENTS") {
		s += "-thread"
	}
	if s != branch {
		s += "-" + hash(branch)[:6]
	}
	return s
}

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
