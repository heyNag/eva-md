package resume

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"time"
	"unicode"

	"github.com/heyNag/eva-md/internal/config"
	"github.com/heyNag/eva-md/internal/inbox"
	"github.com/heyNag/eva-md/internal/paths"
	"github.com/heyNag/eva-md/internal/threadpage"
)

// ShowOptions chooses the date groups of a page's Timeline that eva-md show
// prints. Without either, it prints the newest 10.
type ShowOptions struct {
	Last  int    // the newest N date groups
	Since string // the groups dated on or after this day, YYYY-MM-DD
}

const (
	// timelineDays is how many date groups #Timeline shows by default.
	timelineDays = 10
	// sectionCap bounds what a Timeline or a day shows; past it, show stops
	// and says how many lines are left.
	sectionCap = 16 << 10
	// pageCap bounds a whole page, which is refused past it, and any other
	// section, which stops there.
	pageCap = 2 << 20
)

var errTimelineOnly = errors.New("resume: --last and --since apply only to a page's #Timeline")

// Show resolves an unambiguous physical source line, or a wiki page or
// section. It is read-only and never follows symlinks or returns
// secret-bearing text. A wiki page keeps the identifiers the daily commit's
// scan accepts (see cleanPage), so a page shown can be written back.
func Show(ctx context.Context, dirs paths.Dirs, cfg config.Config, ref string) (string, error) {
	return ShowWith(ctx, dirs, cfg, ref, ShowOptions{})
}

// ShowWith is Show with the date groups of a Timeline chosen by so. A page
// with a section named is streamed line by line with bounded memory, and
// only that section comes back: #Now, #Timeline (the newest 10 date groups
// unless so chooses others), #<YYYY-MM-DD> (that day's group, or each of its
// groups when an out-of-order update gave it two) or any other heading, by
// its text or its anchor (see anchor). #Now and other named sections stop
// reading at the section's end, while #Timeline and #<YYYY-MM-DD> read the
// page once. Sections end where threadpage.Parse ends them, so a heading in
// a fenced block ends none. A Timeline or a day stops after about 16 KB,
// saying how many lines are left, and any other section after 2 MiB; a
// whole page over 2 MiB is refused.
func ShowWith(ctx context.Context, dirs paths.Dirs, cfg config.Config, ref string, so ShowOptions) (string, error) {
	if err := ctx.Err(); err != nil {
		return "", err
	}
	if so.Last < 0 {
		return "", errors.New("resume: --last must be 1 or more")
	}
	if _, err := time.Parse(time.DateOnly, so.Since); so.Since != "" && err != nil {
		return "", errors.New("resume: --since must be a day in YYYY-MM-DD form")
	}
	timeline := so.Last > 0 || so.Since != ""
	if strings.HasPrefix(ref, "s:") {
		if timeline {
			return "", errTimelineOnly
		}
		return showSource(ctx, dirs, ref)
	}
	page, section, _ := strings.Cut(ref, "#")
	if timeline && !strings.EqualFold(section, "Timeline") {
		return "", errTimelineOnly
	}
	if strings.Contains(page, "\\") || !filepath.IsLocal(page) || page == "." || strings.HasPrefix(page, ".") || strings.Contains(page, "/.") {
		return "", errors.New("resume: invalid page path")
	}
	if !strings.HasSuffix(page, ".md") {
		page += ".md"
	}
	root, err := openWiki(cfg)
	if err != nil {
		return "", err
	}
	defer root.Close()
	if section == "" {
		body, err := pageText(root, page)
		if err != nil {
			return "", err
		}
		return cleanPage(body), nil
	}
	f, err := regular(root, page)
	if err != nil {
		return "", err
	}
	defer f.Close()
	text, err := readSection(ctx, f, section, so)
	if err != nil {
		return "", err
	}
	return cleanPage(text), nil
}

// readSection streams a page and returns the section named name.
func readSection(ctx context.Context, f io.ReaderAt, name string, so ShowOptions) (string, error) {
	if strings.EqualFold(name, "Timeline") {
		return timelineSection(ctx, f, so)
	}
	if _, err := time.Parse(time.DateOnly, name); err == nil {
		return daySections(ctx, f, name)
	}
	return namedSection(ctx, f, name)
}

// pageLines reads a page's lines, each without its newline.
type pageLines struct {
	ctx context.Context
	r   *bufio.Reader
	buf []byte
	off int64 // where the next line starts
}

// linesFrom reads f's lines from its start.
func linesFrom(ctx context.Context, f io.ReaderAt) *pageLines {
	return &pageLines{ctx: ctx, r: bufio.NewReaderSize(io.NewSectionReader(f, 0, 1<<62), 64<<10)}
}

// span makes p read the lines in the n bytes of f from off, and no further,
// with the buffer it has.
func (p *pageLines) span(f io.ReaderAt, off, n int64) {
	p.r.Reset(io.NewSectionReader(f, off, n))
	p.off = off
}

// next returns the next line, where it starts, and whether it is short
// enough to show; io.EOF follows the last line.
func (p *pageLines) next() (line string, start int64, ok bool, err error) {
	if err := p.ctx.Err(); err != nil {
		return "", 0, false, err
	}
	b, ok, size, err := readLine(p.r, &p.buf, pageCap, true)
	start, p.off = p.off, p.off+int64(size)
	return string(b), start, ok, err
}

// mdHeading returns the level and text of a markdown heading line: 1 to 6
// '#', then a space or the end of the line, as threadpage reads one.
func mdHeading(line string) (int, string, bool) {
	t := strings.TrimRight(line, " \t\r")
	n := len(t) - len(strings.TrimLeft(t, "#"))
	if n == 0 || n > 6 || len(t) > n && t[n] != ' ' {
		return 0, "", false
	}
	return n, strings.TrimSpace(t[n:]), true
}

// outline finds a page's headings as threadpage.Parse and its Scanner do, so
// that show ends a section where they end it and a ref search prints opens
// the lines it found. In a Timeline date group or a section a person added,
// a line in a fenced block is no heading, except that a "### " line in the
// Timeline always starts a group and ends any fence there. Before the first
// "## " heading, in the Now and before the Timeline's first group, which
// hold no fence eva-md writes, fences are not tracked.
type outline struct {
	section string // the "## " heading of the section being read
	grouped bool   // a Timeline date group has started
	fence   string // the fence open, if any
}

// heading returns the level and text of line when it is a heading.
func (o *outline) heading(line string) (int, string, bool) {
	n, text, ok := mdHeading(line)
	switch {
	case ok && n == 3 && o.section == "## Timeline":
		o.grouped, o.fence = true, ""
	case ok && o.fence == "":
		if n == 2 {
			o.section, o.grouped = strings.TrimRight(line, " \t\r"), false
		}
	default:
		if o.section != "" && o.section != "## Now" && (o.section != "## Timeline" || o.grouped) {
			o.fence = threadpage.NextFence(o.fence, line)
		}
		return 0, "", false
	}
	return n, text, true
}

// clip keeps whole lines while they fit in limit bytes. From the first line
// that doesn't, it keeps nothing more and counts the lines left that aren't
// blank.
type clip struct {
	lines       []string
	size, limit int
	more        int
}

func (c *clip) add(line string, ok bool) {
	if c.more == 0 && ok && c.size+len(line)+1 <= c.limit {
		c.lines = append(c.lines, line)
		c.size += len(line) + 1
		return
	}
	if !ok || strings.TrimSpace(line) != "" {
		c.more++
	}
}

// text joins the lines kept, without blank lines at the end, and says how
// many were left out, followed by hint.
func (c *clip) text(hint string) string {
	lines := c.lines
	for len(lines) > 0 && strings.TrimSpace(lines[len(lines)-1]) == "" {
		lines = lines[:len(lines)-1]
	}
	out := strings.Join(lines, "\n")
	if c.more > 0 {
		out += fmt.Sprintf("\n%d more %s%s", c.more, plural(c.more, "line"), hint)
	}
	return out
}

// anchor is a section's name with a hyphen for each run of spaces, as the
// refs search prints name a section: one word to a shell.
func anchor(name string) string { return strings.Join(strings.Fields(name), "-") }

// namedSection returns the first section headed name, or its anchor, in any
// case, up to the next heading of its level or above, and reads no further.
func namedSection(ctx context.Context, f io.ReaderAt, name string) (string, error) {
	lines := linesFrom(ctx, f)
	var o outline
	c := clip{limit: pageCap}
	level := 0
	for {
		line, _, ok, err := lines.next()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			return "", err
		}
		n, text, heading := o.heading(line)
		if level > 0 && heading && n <= level {
			break
		}
		if level == 0 && heading && (strings.EqualFold(text, name) || strings.EqualFold(anchor(text), name)) {
			level = n
		}
		if level > 0 {
			c.add(line, ok)
		}
	}
	if level == 0 {
		return "", errors.New("resume: section not found")
	}
	return c.text(""), nil
}

// daySections returns every section headed by day: a day's Timeline group,
// or each of them when an out-of-order update started a second one later.
// That can be anywhere, so it reads the whole page.
func daySections(ctx context.Context, f io.ReaderAt, day string) (string, error) {
	lines := linesFrom(ctx, f)
	var o outline
	c := clip{limit: sectionCap}
	found, level := false, 0
	for {
		line, _, ok, err := lines.next()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			return "", err
		}
		n, text, heading := o.heading(line)
		if level > 0 && heading && n <= level {
			level = 0
		}
		if level == 0 && heading && text == day {
			found, level = true, n
		}
		if level > 0 {
			c.add(line, ok)
		}
	}
	if !found {
		return "", errors.New("resume: section not found")
	}
	return c.text(""), nil
}

// dayGroup is a date group of a Timeline, as the first pass finds it.
type dayGroup struct {
	day        string // its heading's text
	start, end int64  // its bytes, heading included
	lines      int    // its lines that aren't blank
}

// timelineSection returns the Timeline's heading and the date groups so
// chooses, in page order. A first pass reads the Timeline once and keeps
// only where each group starts and ends; a second reads the chosen groups,
// each up to its end.
func timelineSection(ctx context.Context, f io.ReaderAt, so ShowOptions) (string, error) {
	lines := linesFrom(ctx, f)
	var o outline
	head, level := "", 0
	var intro dayGroup // the lines between the heading and the first group
	var groups []dayGroup
	for {
		line, start, ok, err := lines.next()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			return "", err
		}
		n, text, heading := o.heading(line)
		if level == 0 {
			if heading && strings.EqualFold(text, "Timeline") {
				head, level = line, n
				intro = dayGroup{start: lines.off, end: lines.off}
			}
			continue
		}
		if heading && n <= level {
			break
		}
		if heading && n == level+1 {
			groups = append(groups, dayGroup{day: text, start: start})
		}
		g := &intro
		if len(groups) > 0 {
			g = &groups[len(groups)-1]
		}
		g.end = lines.off
		if !ok || strings.TrimSpace(line) != "" {
			g.lines++
		}
	}
	if level == 0 {
		return "", errors.New("resume: section not found")
	}
	chosen, total := chooseDays(groups, so)
	c := clip{limit: sectionCap}
	c.add(head, true)
	if len(chosen) < total {
		c.add(fmt.Sprintf("Showing %d of %d date groups; use --since YYYY-MM-DD or --last N for others.", len(chosen), total), true)
	}
	var shown []dayGroup
	if intro.lines > 0 {
		shown = append(shown, intro)
	}
	for _, g := range groups {
		if chosen[g.day] {
			shown = append(shown, g)
		}
	}
	for _, g := range shown {
		if c.more > 0 {
			c.more += g.lines
			continue
		}
		lines.span(f, g.start, g.end-g.start)
		for {
			line, _, ok, err := lines.next()
			if errors.Is(err, io.EOF) {
				break
			}
			if err != nil {
				return "", err
			}
			c.add(line, ok)
		}
	}
	return c.text("; use --since or --last"), nil
}

// chooseDays returns the days whose groups a Timeline shows, and how many
// days it has: those on or after so.Since when it is set, then the newest
// so.Last of them, or the newest 10 when neither is set. A heading that is
// not a day counts as older than every day, and a later one as newer.
func chooseDays(groups []dayGroup, so ShowOptions) (map[string]bool, int) {
	type day struct {
		name string
		at   time.Time
		ok   bool
		last int // the position of its last group
	}
	index := map[string]int{}
	var days []day
	for i, g := range groups {
		if j, ok := index[g.day]; ok {
			days[j].last = i
			continue
		}
		at, err := time.Parse(time.DateOnly, g.day)
		index[g.day] = len(days)
		days = append(days, day{name: g.day, at: at, ok: err == nil, last: i})
	}
	slices.SortFunc(days, func(a, b day) int {
		switch {
		case a.ok != b.ok:
			if a.ok {
				return -1
			}
			return 1
		case a.ok && !a.at.Equal(b.at):
			return b.at.Compare(a.at)
		}
		return b.last - a.last
	})
	limit := so.Last
	if limit == 0 && so.Since == "" {
		limit = timelineDays
	}
	since, _ := time.Parse(time.DateOnly, so.Since)
	chosen := map[string]bool{}
	for _, d := range days {
		if limit > 0 && len(chosen) == limit {
			break
		}
		if so.Since == "" || d.ok && !d.at.Before(since) {
			chosen[d.name] = true
		}
	}
	return chosen, len(days)
}

func showSource(ctx context.Context, dirs paths.Dirs, ref string) (string, error) {
	sid, num, ok := strings.Cut(strings.TrimPrefix(ref, "s:"), "#")
	line, err := strconv.Atoi(num)
	if !ok || err != nil || line < 1 || len([]rune(sid)) < 1 || len(sid) > 128 || strings.ContainsAny(sid, "/\\\x00") {
		return "", errors.New("resume: invalid source reference")
	}
	for _, r := range sid {
		if unicode.IsControl(r) || unicode.In(r, unicode.Cf) {
			return "", errors.New("resume: invalid source reference")
		}
	}
	root, err := paths.OpenRoot(dirs.Inbox(), false)
	if err != nil {
		return "", err
	}
	defer root.Close()
	d, err := root.Open(".")
	if err != nil {
		return "", err
	}
	projects, err := d.ReadDir(-1)
	d.Close()
	if err != nil {
		return "", err
	}
	var found []string
	for _, p := range projects {
		if err := ctx.Err(); err != nil {
			return "", err
		}
		if !p.IsDir() {
			continue
		}
		d, err := root.Open(p.Name())
		if err != nil {
			continue
		}
		files, _ := d.ReadDir(-1)
		d.Close()
		for _, f := range files {
			if !f.Type().IsRegular() || !strings.HasSuffix(f.Name(), ".jsonl") {
				continue
			}
			name := f.Name()
			for _, host := range []string{"claude-code-", "codex-"} {
				if strings.HasPrefix(name, host) {
					name = strings.TrimPrefix(name, host)
					break
				}
			}
			if !strings.HasPrefix(name, sid) {
				continue
			}
			file, err := regular(root, filepath.Join(p.Name(), f.Name()))
			if err != nil {
				continue
			}
			entry, err := sourceLine(ctx, file, line)
			file.Close()
			if err != nil {
				continue
			}
			// A ref names a session by its first 8 characters or in full.
			if inbox.Ref(entry.Session, line) != ref && entry.Session != sid {
				continue
			}
			if filepath.Base((inbox.Store{Root: dirs.Inbox()}).Path(entry.Project, entry.Host, entry.Session)) != f.Name() {
				continue
			}
			if project, _ := paths.SafeName(entry.Project); project != p.Name() {
				continue
			}
			found = append(found, sourceDescription(record{Entry: entry, line: line}))
		}
	}
	if len(found) == 0 {
		return "", errors.New("resume: source not found")
	}
	if len(found) > 1 {
		return "", errors.New("resume: ambiguous source reference; use the full session id, as in s:<session>#<line>")
	}
	return found[0], nil
}

func sourceLine(ctx context.Context, f *os.File, line int) (inbox.Entry, error) {
	s := bufio.NewScanner(f)
	s.Buffer(make([]byte, 64<<10), 2<<20)
	for n := 1; s.Scan(); n++ {
		if err := ctx.Err(); err != nil {
			return inbox.Entry{}, err
		}
		if n != line {
			continue
		}
		var e inbox.Entry
		if err := json.Unmarshal(s.Bytes(), &e); err != nil {
			return e, err
		}
		return e, nil
	}
	return inbox.Entry{}, fmt.Errorf("resume: source line %d unavailable", line)
}
