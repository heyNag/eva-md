package resume

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"time"

	"github.com/heyNag/eva-md/internal/systempage"
)

// System pages, systems/<slug>.md, are the wiki's shared understanding of the
// systems its projects belong to (see internal/systempage). They are global
// to the wiki: a brief points at the project's from the system map alone,
// and every search scope reads them, one page at a time, as it reads thread
// pages.

const (
	// maxPointers is how many systems a brief points at.
	maxPointers = 3
	// pointerCap bounds one pointer, and systemsCap the pointers and the
	// map's line together, on both hosts.
	pointerCap = 200
	systemsCap = 600
	// mapCap bounds the system map a brief reads.
	mapCap = 256 << 10
	// minDescription is the shortest cut of a description a pointer keeps.
	minDescription = 20
)

// systemPointers returns the brief's pointers to the system pages: a line for
// each of up to 3 systems whose repos include the project, in the map's
// order, then one for the map, as long as the map lists a system. A line
// that cleaning as wiki text would change is left out, so the length counted
// is the length shown, identifiers such as a slug included.
func systemPointers(ctx context.Context, o Options) string {
	entries := readMap(ctx, o)
	if len(entries) == 0 {
		return ""
	}
	bin := filepath.Join(o.Dirs.Share, "bin", "eva-md")
	last := asShown(fmt.Sprintf("System map: %d %s: %s show systems/map", len(entries), plural(len(entries), "system"), bin))
	if last == "" || runes(last) > systemsCap {
		return ""
	}
	left := systemsCap - runes(last)
	var lines []string
	for _, e := range entries {
		if len(lines) == maxPointers {
			break
		}
		if !slices.Contains(e.Repos, o.Project.Key) {
			continue
		}
		if line := asShown(pointer(e, bin, min(pointerCap, left-1))); line != "" {
			lines = append(lines, line)
			left -= runes(line) + 1
		}
	}
	return strings.Join(append(lines, last), "\n")
}

// asShown returns line when cleaning it as wiki text leaves it as it is, and
// "" otherwise.
func asShown(line string) string {
	if cleanPage(line) != line {
		return ""
	}
	return line
}

// pointer is a system's line, "System: <title>: <description> (as of <day>):
// <bin> show systems/<slug>", in at most limit runes. The description is cut
// to fit or left out, then the day; the title is cut last and the command
// never, so a line without room for the command is "".
func pointer(e systempage.MapEntry, bin string, limit int) string {
	title, desc := oneLine(e.Title, pointerCap), oneLine(e.Description, pointerCap)
	if title == "" {
		title = e.Slug
	}
	day := ""
	if _, err := time.Parse(time.DateOnly, e.AsOf); err == nil {
		day = " (as of " + e.AsOf + ")"
	}
	head, tail := "System: "+title, ": "+bin+" show systems/"+e.Slug
	if desc != "" {
		if room := limit - runes(head+": "+day+tail); room >= min(runes(desc), minDescription) {
			return head + ": " + cut(desc, room) + day + tail
		}
	}
	for _, line := range []string{head + day + tail, head + tail} {
		if runes(line) <= limit {
			return line
		}
	}
	if room := limit - runes("System: "+tail); room > 0 {
		return "System: " + cut(title, room) + tail
	}
	return ""
}

// readMap returns the systems the system map lists. It reads systems/map.md
// by its path through the wiki's pinned root, once, and nothing else of the
// systems folder: no listing and no page. A map that is missing, linked,
// not a regular file, unreadable or over 256 KB lists none.
func readMap(ctx context.Context, o Options) []systempage.MapEntry {
	if o.Config.WikiPath == "" || ctx.Err() != nil {
		return nil
	}
	root, err := openWiki(o.Config)
	if err != nil {
		return nil
	}
	defer root.Close()
	f, err := regular(root, filepath.Join("systems", "map.md"))
	if err != nil {
		return nil
	}
	defer f.Close()
	info, err := f.Stat()
	if err != nil || info.Size() > mapCap {
		return nil
	}
	data, err := io.ReadAll(io.LimitReader(f, mapCap+1))
	if err != nil || len(data) > mapCap {
		return nil
	}
	lines := strings.Split(string(data), "\n")
	// The map's own frontmatter is no line of the map.
	if lines[0] == "---" {
		if end := slices.Index(lines[1:], "---"); end >= 0 {
			lines = lines[end+2:]
		}
	}
	var out []systempage.MapEntry
	for _, l := range lines {
		if e, ok := systempage.ParseMapLine(strings.TrimRight(l, "\r")); ok {
			out = append(out, e)
		}
	}
	return out
}

// listSystems returns the system pages, as listPages returns thread pages
// but whatever the window: they hold the wiki's lasting understanding, which
// an old as_of does not make stale.
func (s *scan) listSystems(root *os.Root) []pageSource {
	pages := s.listFiles(root, "systems", false, func(name string) bool { return systempage.CheckSlug(name) == nil })
	for i := range pages {
		pages[i].folder, pages[i].system = "systems", true
	}
	return pages
}

// systemFile scans one system page a line at a time: the description and
// as_of in its frontmatter, then each line of its sections, which end where
// eva-md show ends them, except the map's link code ends every page with. A
// line's ref names its section by its anchor, one word to a shell. A page
// that fails to open or read is named as unreadable.
func (s *scan) systemFile(ctx context.Context, root *os.Root, src pageSource, loc *time.Location) {
	f, err := regular(root, filepath.FromSlash(src.rel))
	if err != nil {
		s.badPages = append(s.badPages, src.id)
		return
	}
	defer f.Close()
	r := bufio.NewReaderSize(f, 64<<10)
	var o outline
	var asOf time.Time
	description, at := "", 0
	front, section := false, ""
	for n := 1; ; n++ {
		if ctx.Err() != nil {
			s.cut = true
			break
		}
		line, ok, size, err := readLine(r, &s.line, maxSearchLine, true)
		s.pageBytes += int64(size)
		if err != nil {
			if !errors.Is(err, io.EOF) {
				s.badPages = append(s.badPages, src.id)
				return
			}
			break
		}
		if !ok {
			continue
		}
		text := string(line)
		switch bare := strings.TrimRight(text, "\r"); {
		case n == 1 && bare == "---":
			front = true
			continue
		case front && bare == "---":
			front = false
			if description != "" {
				s.systemUnit(description, src.id, "system", asOf, src, at)
			}
			continue
		case front:
			switch key, value, _ := frontValue(bare); key {
			case "description":
				description, at = value, n
			case "as_of":
				asOf, _ = time.ParseInLocation(time.DateOnly, value, loc)
			}
			continue
		}
		if level, name, heading := o.heading(text); heading && level <= 2 {
			section = ""
			if level == 2 {
				section = name
			}
			continue
		}
		if t := strings.TrimSpace(text); section == "" || t == "" || t == systempage.MapLink {
			continue
		}
		tag := "system"
		if strings.EqualFold(section, "Unverified") {
			tag = "unverified"
		}
		s.systemUnit(text, src.id+"#"+anchor(section), tag, asOf, src, n)
	}
	s.systemCount++
}

// systemUnit matches one line of a system page, found at ref and dated by
// the page's as_of, which the window does not test. Its tag, system or
// unverified, stands where an inbox hit's speaker does.
func (s *scan) systemUnit(text, ref, tag string, asOf time.Time, src pageSource, n int) {
	s.match(unit{text: text, shown: text, ts: asOf, day: true, role: tag, project: src.folder, ref: ref, file: src.rel, line: n, page: true, system: true})
}
