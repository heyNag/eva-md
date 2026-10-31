package update

import (
	"bufio"
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"regexp"
	"slices"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"

	"github.com/heyNag/eva-md/internal/freetext"
	"github.com/heyNag/eva-md/internal/inbox"
	"github.com/heyNag/eva-md/internal/threadpage"
)

// Entry is an inbox entry with its place in the session file: its physical
// line number, as eva-md show counts lines, and the byte offset just past it.
type Entry struct {
	inbox.Entry
	Line int
	End  int64
}

// maxEntryLine is the longest inbox line decoded, as inbox.Store.ReadFrom
// decodes them; a longer one becomes a gap event.
const maxEntryLine = 2 << 20

// recordMarker is in every off_record and on_record event line, as search
// looks for it.
var recordMarker = []byte(`_record"`)

// Prefix reads the bytes of a session file before the watermark, a byte
// offset. It counts their lines, so the entries after them keep their
// physical line numbers, and reports whether the chat was off the record
// where they end. Only a line holding an off_record or on_record marker is
// decoded, so the job counts the newlines before the watermark once per file
// and the watermark's format stays as it is.
func Prefix(r io.Reader) (lines int, off bool, err error) {
	br := bufio.NewReaderSize(r, 64<<10)
	whole := true // the line so far came in one read; a marker line always does
	for {
		frag, err := br.ReadSlice('\n')
		switch {
		case errors.Is(err, bufio.ErrBufferFull):
			whole = false
			continue
		case errors.Is(err, io.EOF):
			return lines, off, nil
		case err != nil:
			return lines, off, err
		}
		lines++
		if whole && bytes.Contains(frag, recordMarker) {
			var e inbox.Entry
			if json.Unmarshal(frag, &e) == nil && marker(e) {
				off = e.Text == "off_record"
			}
		}
		whole = true
	}
}

// marker reports an off_record or on_record event.
func marker(e inbox.Entry) bool {
	return e.Kind == "event" && (e.Text == "off_record" || e.Text == "on_record")
}

// Decode reads data, the bytes of a session file from byte offset start,
// whose first line is physical line first. Each complete line is one entry;
// one that doesn't decode, or is longer than inbox.Store.ReadFrom reads,
// becomes the gap event ReadFrom makes of it, so line numbers stay physical.
// An unfinished last line is left for a later pass.
//
// It decodes all of data, and Build reads every entry it is given, though a
// pass covers at most 60 KB of trimmed input, so the job hands it a bounded
// window past the watermark rather than the rest of a large file.
func Decode(data []byte, start int64, first int) []Entry {
	var out []Entry
	end := start
	for line := first; ; line++ {
		i := bytes.IndexByte(data, '\n')
		if i < 0 {
			return out
		}
		var e inbox.Entry
		if i+1 > maxEntryLine || json.Unmarshal(data[:i], &e) != nil {
			e = inbox.Entry{V: 1, Role: "event", Kind: "event", Text: "gap: malformed inbox record", Origin: "hook"}
		}
		end += int64(i) + 1
		out = append(out, Entry{Entry: e, Line: line, End: end})
		data = data[i+1:]
	}
}

// Options is what Build needs besides the entries and the page.
type Options struct {
	Branch       string          // the thread's branch
	Commits      []string        // the branch's last commit subjects, newest first; 5 are shown
	AsOf         time.Time       // the page's as_of, for the out-of-order guard; zero for a new page
	Parent       *threadpage.Now // a new thread's parent Now, shown for context; nil otherwise
	ParentBranch string          // the parent thread's branch
	OffRecord    bool            // the chat was off the record where the entries start (see Prefix)
	FullRef      bool            // another session file starts with this session's first 8 characters
	Continued    bool            // the last pass over this file stopped with More: the rest of a group that passed the size check
	Rules        RuleSet         // the standing rules on file
}

// Batch is one pass's input for the model, with what Validate checks the
// output against.
type Batch struct {
	Input         []byte     // the page context and the numbered entries; Rules go separately
	Entries       []Numbered // Entries[n-1] is the entry numbered [n]
	End           int64      // the byte offset just past the last entry this pass covers
	More          bool       // entries past End wait for a later pass; its Options set Continued
	Off           bool       // the chat was off the record at End, for the next pass's Options
	Last          time.Time  // the newest prompt, reply, tool, plan or host summary of all the entries given, not only this pass's
	Host, Session string
	Branch        string
	Commits       []string
	AsOf          time.Time
	FullRef       bool
	Rules         RuleSet
}

// Numbered is one numbered entry of a batch, as a citation of it is checked.
type Numbered struct {
	Session    string
	Line       int       // the physical line its ref names; for a run of tool entries, the last one
	Role, Kind string    // from the entry's fields, never from its text
	At         time.Time // zero when the entry's time can't be read
	Text       string    // the raw redacted text; a run of tool entries, one per line
	report     bool      // the first assistant reply after a tool entry with no prompt between: a report of a result
}

// ErrTooSmall reports a group under one exchange or 600 characters. Its
// entries wait for more.
var ErrTooSmall = errors.New("update: under one exchange or 600 characters")

// Sizes, in bytes of the input as Build writes it, and the group's minimum
// in characters.
const (
	chunkBytes    = 60 << 10 // the entries of one pass
	userBytes     = 6 << 10  // one user entry
	entryBytes    = 8 << 10  // one entry of any other kind
	minChars      = 600      // the prompts, replies, tool and plan lines and host summaries of a group
	commitsShown  = 5
	timelineShown = 5
	contextCap    = 600 // one line of page context
)

// indent starts every line of an entry's text after its first, so only the
// input's own headers start a line.
const indent = "  "

// cutMark replaces the middle of an entry that is over its size.
const cutMark = "\n" + indent + "[… cut …]\n" + indent

// Build turns one (session, branch) group of inbox entries past the
// watermark, oldest first, into the model's input. Off-the-record ranges are
// dropped, starting from o.OffRecord, as capture, catch-up and the brief drop
// them. A group with less than one exchange or under 600 characters returns
// ErrTooSmall, unless o.Continued says it is the rest of one that passed. A
// group that ends with the chat's session_end event needs the exchange only:
// nothing more joins a closed chat, and its last exchange is where a
// decision often lands.
//
// Every entry is numbered "[n] <speaker> <date> <time>: <text>", the speaker
// from its role and the time in its own offset. A run of tool entries is one
// entry, compressed ("edited a.go, b.go; ran go test ./... (exit 1)") and
// ending at its first command; a plan is a checklist; a host summary is
// labelled so. Each text is cleaned as the brief cleans free text, a line
// that would read as an entry is quoted, and every line after the first is
// indented. A user entry is cut to 6 KB as written, any other to 8 KB,
// keeping its head and tail. The batch takes the oldest entries up to 60 KB,
// and at least one, and End says where the next pass starts.
//
// The input opens with the branch and its last 5 commit subjects, the page's
// Now items with their ids, kinds, tags and refs, its last 5 Timeline lines
// with their dates and, for a new thread, the parent's Now items. The page's
// summary and last chat are left out: the model writes them afresh from the
// entries.
func Build(entries []Entry, page threadpage.Page, o Options) (Batch, error) {
	host, session, err := owner(entries)
	if err != nil {
		return Batch{}, err
	}
	kept, off := onRecord(entries, o.OffRecord)
	if !substantial(kept, o.Continued, ended(entries)) {
		return Batch{}, ErrTooSmall
	}
	b := Batch{Host: host, Session: session, Branch: o.Branch, Commits: o.Commits, AsOf: o.AsOf, FullRef: o.FullRef, Rules: o.Rules}
	// Last is the session's newest work, not the chunk's, so the first chunk
	// of a session newer than the page isn't taken for an older session.
	for _, e := range kept {
		if at, err := time.Parse(time.RFC3339Nano, e.TS); err == nil && e.Role != "event" && at.After(b.Last) {
			b.Last = at
		}
	}
	var body strings.Builder
	var last Entry
	report := false
	for i, u := range units(kept) {
		text := u.render(i + 1)
		if i > 0 && body.Len()+len(text) > chunkBytes {
			b.More = true
			break
		}
		body.WriteString(text)
		n := u.numbered(session)
		switch {
		case n.Role == "user" && n.Kind == "prompt", n.Role == "event" && n.Text == "host_prompt":
			report = false
		case n.Role == "tool":
			report = true
		case n.Role == "assistant":
			n.report, report = report, false
		}
		b.Entries = append(b.Entries, n)
		last = u[len(u)-1]
	}
	b.End = last.End
	if !b.More {
		b.End, b.Off = entries[len(entries)-1].End, off
	}
	b.Input = []byte(pageContext(page, o) + "New entries, oldest first:\n" + body.String())
	return b, nil
}

// owner returns the host and session the entries belong to. A gap event
// from a line that didn't decode names none and belongs to any.
func owner(entries []Entry) (host, session string, err error) {
	if len(entries) == 0 {
		return "", "", ErrTooSmall
	}
	for _, e := range entries {
		switch {
		case e.Session == "" && e.Host == "":
		case session == "":
			host, session = e.Host, e.Session
		case e.Host != host || e.Session != session:
			return "", "", errors.New("update: the entries are from more than one session")
		}
	}
	switch {
	case session == "":
		return "", "", errors.New("update: the entries name no session")
	case host != HostClaude && host != HostCodex:
		return "", "", fmt.Errorf("update: unknown host %q", host)
	case strings.ContainsFunc(session, func(r rune) bool { return unicode.IsControl(r) || unicode.In(r, unicode.Cf) }):
		return "", "", errors.New("update: the session id holds control characters")
	}
	return host, session, nil
}

// onRecord drops the off-the-record ranges and their markers, and returns
// the state after the last entry.
func onRecord(entries []Entry, off bool) ([]Entry, bool) {
	var kept []Entry
	for _, e := range entries {
		switch {
		case marker(e.Entry):
			off = e.Text == "off_record"
		case !off:
			kept = append(kept, e)
		}
	}
	return kept, off
}

// ended reports a group whose last entry is the session_end event the hooks
// write when the chat closes.
func ended(entries []Entry) bool {
	e := entries[len(entries)-1]
	return e.Kind == "event" && e.Text == "session_end"
}

// substantial reports a group of at least one exchange, a prompt and a
// reply, and 600 characters of prompts, replies, tool and plan lines and
// host summaries; a final group, which closes the chat, needs only the
// exchange. The rest of a group an earlier pass stopped in, continued, needs
// only one entry that isn't an event.
func substantial(entries []Entry, continued, final bool) bool {
	prompts, replies, work, chars := 0, 0, 0, 0
	for _, e := range entries {
		switch {
		case e.Role == "user" && e.Kind == "prompt":
			prompts++
		case e.Role == "assistant" && e.Kind == "reply":
			replies++
		}
		if e.Role != "event" {
			work++
			chars += utf8.RuneCountInString(e.Text)
		}
	}
	if continued {
		return work > 0
	}
	return prompts > 0 && replies > 0 && (final || chars >= minChars)
}

// unit is one numbered entry of the input: one inbox entry, or a run of tool
// entries that ends at its first command.
type unit []Entry

func units(entries []Entry) []unit {
	var out []unit
	for i, e := range entries {
		joins := i > 0 && e.Role == "tool" && entries[i-1].Role == "tool" && !command(entries[i-1])
		if joins {
			out[len(out)-1] = append(out[len(out)-1], e)
		} else {
			out = append(out, unit{e})
		}
	}
	return out
}

func command(e Entry) bool {
	name, _, _, _, ok := toolParts(e.Text)
	return ok && name == "Bash"
}

// render writes the unit as entry n, cut to its size.
func (u unit) render(n int) string {
	e := u[len(u)-1]
	head := fmt.Sprintf("[%d] %s %s:", n, speaker(e.Role), when(e.TS))
	text, limit := e.Text, entryBytes
	switch e.Role {
	case "tool":
		text = compress(u)
	case "plan":
		text = checklist(e.Text)
	case "user":
		limit = userBytes
		if pastes(e.Host) {
			text = markPasted(text)
		}
	}
	return emit(head, body(text), limit)
}

// numbered is what the validator knows of the unit as an entry.
func (u unit) numbered(session string) Numbered {
	e := u[len(u)-1]
	texts := make([]string, len(u))
	for i, x := range u {
		texts[i] = x.Text
	}
	at, _ := time.Parse(time.RFC3339Nano, e.TS)
	return Numbered{Session: session, Line: e.Line, Role: e.Role, Kind: e.Kind, At: at, Text: strings.Join(texts, "\n")}
}

// speaker names an entry's speaker from its role; a host entry is a summary
// the host wrote.
func speaker(role string) string {
	switch role {
	case "user", "assistant", "tool", "plan", "event":
		return role
	case "host":
		return "host summary"
	}
	return "unknown"
}

// when shows an entry's time in its own offset, to the minute.
func when(ts string) string {
	t, err := time.Parse(time.RFC3339Nano, ts)
	if err != nil {
		return "unknown time"
	}
	return t.Format("2006-01-02 15:04")
}

// toolParts splits a stored tool line, "<tool>[ (subagent)][ <detail>] (exit
// <code>)" as the hook writes it, into its parts.
func toolParts(s string) (name, detail, exit string, sub, ok bool) {
	i := strings.LastIndex(s, " (exit ")
	if i < 0 || !strings.HasSuffix(s, ")") {
		return "", "", "", false, false
	}
	exit = s[i+len(" (exit ") : len(s)-1]
	name, detail, _ = strings.Cut(s[:i], " ")
	detail, sub = strings.CutPrefix(detail, "(subagent)")
	return name, strings.TrimSpace(detail), exit, sub, true
}

// editTools are the tools whose lines name the files they wrote.
var editTools = map[string]bool{"Edit": true, "Write": true, "MultiEdit": true, "NotebookEdit": true, "apply_patch": true}

// compress writes a run of tool entries as one line, in order: files edited
// in a row are listed once, each command with its exit code, other tools
// counted, and a failure shown with its code.
func compress(u unit) string {
	var parts, files, tools []string
	counts := map[string]int{}
	group := "" // the kind of the group being built: edit or other, with the subagent mark
	flush := func() {
		switch {
		case len(files) > 0:
			parts = append(parts, verb(group, "edited")+strings.Join(files, ", "))
		case len(tools) > 0:
			var named []string
			for _, t := range tools {
				if counts[t] > 1 {
					t += fmt.Sprintf(" ×%d", counts[t])
				}
				named = append(named, t)
			}
			parts = append(parts, verb(group, "used")+strings.Join(named, ", "))
		}
		files, tools, counts, group = nil, nil, map[string]int{}, ""
	}
	for _, e := range u {
		name, detail, exit, sub, ok := toolParts(e.Text)
		mark := ""
		if sub {
			mark = "subagent "
		}
		failed := exit != "0" && exit != "unknown"
		switch {
		case !ok:
			flush()
			parts = append(parts, e.Text)
		case name == "Bash":
			flush()
			if detail == "" {
				detail = "a command"
			}
			parts = append(parts, mark+"ran "+detail+" (exit "+exit+")")
		case failed:
			flush()
			if f := short(detail, e.Cwd); f != "" {
				name += " " + f
			}
			parts = append(parts, mark+name+" failed (exit "+exit+")")
		case editTools[name] && detail != "":
			if group != "edit "+mark {
				flush()
				group = "edit " + mark
			}
			for _, f := range strings.Split(detail, ", ") {
				if f = short(f, e.Cwd); !slices.Contains(files, f) {
					files = append(files, f)
				}
			}
		default:
			if group != "other "+mark {
				flush()
				group = "other " + mark
			}
			if counts[name]++; counts[name] == 1 {
				tools = append(tools, name)
			}
		}
	}
	flush()
	return strings.Join(parts, "; ")
}

// verb starts a group's text, with the subagent mark its key holds.
func verb(group, word string) string {
	if strings.HasSuffix(group, "subagent ") {
		return "subagent " + word + " "
	}
	return word + " "
}

// short shows a file relative to the chat's folder, cwd, which the inbox
// keeps relative to the home folder, and keeps the end of a long one.
func short(file, cwd string) string {
	file = strings.TrimSpace(file)
	if cwd != "" {
		if i := strings.Index(file, "/"+cwd+"/"); i >= 0 {
			file = file[i+len(cwd)+2:]
		}
	}
	if r := []rune(file); len(r) > 80 {
		file = "…" + string(r[len(r)-79:])
	}
	return file
}

// stepStart finds the steps of a stored plan line, "<tool> [x] step; [ ]
// step (exit <code>)" as the hook writes it.
var stepStart = regexp.MustCompile(`(?:^|; )\[[x~ ?]\] `)

// checklist shows a plan as one step a line, under the entry's header. A
// line in another form is shown as stored.
func checklist(s string) string {
	_, steps, _ := strings.Cut(s, " ")
	if i := strings.LastIndex(steps, " (exit "); i >= 0 && strings.HasSuffix(steps, ")") {
		steps = steps[:i]
	}
	locs := stepStart.FindAllStringIndex(steps, -1)
	if len(locs) == 0 || locs[0][0] != 0 {
		return s
	}
	var lines []string
	for i, loc := range locs {
		start, end := loc[0], len(steps)
		if i > 0 {
			start += len("; ")
		}
		if i+1 < len(locs) {
			end = locs[i+1][0]
		}
		lines = append(lines, steps[start:end])
	}
	return "\n" + strings.Join(lines, "\n")
}

// body cleans an entry's text as the brief cleans free text, quotes every
// line that would read as an entry, and indents every line after the first,
// so only the input's own headers start a line.
func body(s string) string {
	lines := strings.Split(freetext.Shown(s), "\n")
	for i := range lines {
		lines[i] = quote(lines[i])
	}
	return strings.Join(lines, "\n"+indent)
}

// entryHeader matches a squashed line that would read as an entry of the
// input.
var entryHeader = regexp.MustCompile(`^\[ ?[0-9]+ ?\] ?(user|assistant|tool|plan|host summary|host|event|unknown)\b`)

// quote marks a line that would read as an entry of the input.
func quote(line string) string {
	if entryHeader.MatchString(squash(line)) {
		return "[quoted] " + line
	}
	return line
}

// squash is the start of a line as a reader sees it: without control or
// format characters, every run of spaces one plain space, lowercased, and
// without leading space.
func squash(line string) string {
	var b []rune
	for _, r := range line {
		switch {
		case unicode.IsSpace(r):
			r = ' '
		case unicode.IsControl(r) || unicode.In(r, unicode.Cf):
			continue
		default:
			r = unicode.ToLower(r)
		}
		if r == ' ' && (len(b) == 0 || b[len(b)-1] == ' ') {
			continue
		}
		if b = append(b, r); len(b) >= 48 {
			break
		}
	}
	return string(b)
}

// emit writes one numbered entry within limit bytes: its header, its text
// and a newline. A longer text keeps its head and tail around cutMark; the
// tail's first line is quoted again, as the cut may have left out its mark.
func emit(head, text string, limit int) string {
	sep := " "
	if text == "" || strings.HasPrefix(text, "\n") {
		sep = ""
	}
	if whole := head + sep + text + "\n"; len(whole) <= limit {
		return whole
	}
	room := limit - len(head) - len(sep) - len(cutMark) - len("[quoted] ") - 1
	h, t := headOf(text, room/2), tailOf(text, room-room/2)
	first, rest, more := strings.Cut(t, "\n")
	if t = quote(first); more {
		t += "\n" + rest
	}
	return head + sep + h + cutMark + t + "\n"
}

// headOf keeps at most n bytes of s, ending on a whole character.
func headOf(s string, n int) string {
	if n >= len(s) {
		return s
	}
	for n > 0 && !utf8.RuneStart(s[n]) {
		n--
	}
	return s[:max(n, 0)]
}

// tailOf keeps at most the last n bytes of s, starting on a whole character.
func tailOf(s string, n int) string {
	if n >= len(s) {
		return s
	}
	i := len(s) - max(n, 0)
	for i < len(s) && !utf8.RuneStart(s[i]) {
		i++
	}
	return s[i:]
}

// hidden is a hidden comment of a page line, such as an item's id.
var hidden = regexp.MustCompile(`<!--.*?-->`)

// pageContext writes what the model sees besides the entries: the branch and
// its commits, the page's Now items, its last Timeline lines and a new
// thread's parent Now items.
func pageContext(p threadpage.Page, o Options) string {
	var b strings.Builder
	b.WriteString(oneLine("Branch: "+o.Branch) + "\n")
	commits := o.Commits[:min(len(o.Commits), commitsShown)]
	section(&b, "Last commits on this branch, newest first", commits, func(s string) string { return "- " + s })
	var items []string
	n := p.Now
	for _, s := range []struct {
		list []threadpage.Item
		kind string
	}{{n.Next, "next"}, {n.Open, "question"}, {n.Decisions, "decision"}} {
		for _, it := range s.list {
			items = append(items, nowItem(it, s.kind, true))
		}
	}
	section(&b, "Now items on this page, by id (give each id an action)", items, nil)
	section(&b, "Standing rules on file, by id", ruleLines(o.Rules), nil)
	section(&b, "Last Timeline lines, oldest first, with their dates", lastLines(p.Timeline, timelineShown), func(s string) string {
		return "- " + strings.TrimSpace(hidden.ReplaceAllString(s, ""))
	})
	if o.Parent != nil {
		var parent []string
		for _, s := range []struct {
			list []threadpage.Item
			kind string
		}{{o.Parent.Next, "next"}, {o.Parent.Open, "question"}, {o.Parent.Decisions, "decision"}} {
			for _, it := range s.list {
				parent = append(parent, nowItem(it, s.kind, false))
			}
		}
		section(&b, "Parent thread "+o.ParentBranch+", its Now items, for context only: they are not on this page", parent, nil)
	}
	return b.String()
}

// section writes a heading and its lines, each on one cleaned line, or the
// heading and "none".
func section(b *strings.Builder, heading string, lines []string, format func(string) string) {
	if len(lines) == 0 {
		b.WriteString(oneLine(heading+": none.") + "\n")
		return
	}
	b.WriteString(oneLine(heading+":") + "\n")
	for _, l := range lines {
		if format != nil {
			l = format(l)
		}
		b.WriteString(oneLine(l) + "\n")
	}
}

// nowItem writes a Now item as "- <id> <kind> [<tag>] <refs>: <text>"; a
// line eva-md wrote names its code, and a decision kept past the cap is a
// decision. Without ids, for the parent's items, it leaves out the id and
// refs.
func nowItem(it threadpage.Item, kind string, ids bool) string {
	if it.Code == threadpage.OverCapCode {
		kind = "decision"
	}
	parts := []string{"-"}
	if ids {
		id := it.ID
		if id == "" {
			id = "(no id)"
		}
		parts = append(parts, id)
	}
	parts = append(parts, kind)
	if it.Tag != "" {
		parts = append(parts, "["+it.Tag+"]")
	}
	if it.Code != "" {
		parts = append(parts, "(eva-md line: "+it.Code+")")
	}
	if ids {
		parts = append(parts, it.Refs...)
	}
	return strings.Join(parts, " ") + ": " + it.Text
}

// ruleLines are the standing rules on file, the general area's first, each
// as "- <id> for all projects: <text>", or "for this project", without the
// tag every rule shares and the refs the model can't cite. A rule without an
// id an operation can name shows "(no id)".
func ruleLines(s RuleSet) []string {
	var out []string
	for _, general := range []bool{true, false} {
		scope := "for this project"
		if general {
			scope = "for all projects"
		}
		held := map[string]bool{}
		for _, it := range threadpage.HeldRules(s.page(general), general) {
			held[it.ID] = true
		}
		for _, it := range s.page(general).Rules {
			id := "(no id)"
			if held[it.ID] {
				id = it.ID
				delete(held, it.ID)
			}
			out = append(out, "- "+id+" "+scope+": "+it.Text)
		}
	}
	return out
}

// lastLines returns the Timeline's last n lines, oldest first, each as
// "<date>: <line>" without the line's "- ", so the model sees when it
// happened.
func lastLines(groups []threadpage.Group, n int) []string {
	var out []string
	for i := len(groups) - 1; i >= 0 && len(out) < n; i-- {
		lines := groups[i].Lines
		for j := len(lines) - 1; j >= 0 && len(out) < n; j-- {
			line := strings.TrimPrefix(strings.TrimSpace(lines[j]), "- ")
			if groups[i].Date != "" {
				line = groups[i].Date + ": " + line
			}
			out = append(out, line)
		}
	}
	slices.Reverse(out)
	return out
}

// oneLine is page context on one line, cleaned as the brief cleans free
// text, at most contextCap characters.
func oneLine(s string) string {
	return cutRunes(strings.Join(strings.Fields(freetext.Shown(s)), " "), contextCap)
}

// cutRunes keeps at most n characters of s, ending in "…" when it cuts.
func cutRunes(s string, n int) string {
	if utf8.RuneCountInString(s) <= n {
		return s
	}
	return string([]rune(s)[:n-1]) + "…"
}
