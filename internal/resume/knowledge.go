package resume

import (
	"context"
	"errors"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/heyNag/eva-md/internal/coderef"
	"github.com/heyNag/eva-md/internal/knowledgepage"
)

// Knowledge pages, knowledge/k<N>.md, hold the lasting facts, decisions,
// procedures and constraints the background update promoted from earlier
// chats (see internal/knowledgepage). Like system pages they are global to
// the wiki: a new chat's brief lists the project's constraints from the
// recall index alone, and every search scope reads the pages, one at a time.

const (
	// maxConstraints is how many constraints a brief lists.
	maxConstraints = 3
	// constraintsCap bounds the constraint lines together, on both hosts.
	constraintsCap = 600
	// maxKnowledge bounds the read of a knowledge page search reads; a page
	// past it is named as unreadable.
	maxKnowledge = 256 << 10
)

// knowledgeFacts says what a knowledge page's hit is, when one is shown,
// and what its tags rest on.
const knowledgeFacts = "[knowledge] lines are lasting facts the background update promoted from earlier chats; decision and constraint lines rest on the user's words, and fact and procedure lines on them too or, tagged [observed], on a command, file or MCP result the assistant reported."

// constraintsBlock returns the brief's constraint lines: one for each of up
// to 3 constraints that hold for the project, those of every project and
// the project's own, newest as_of first, in at most 600 runes together. It
// reads the recall index by its path alone, never the folder or a page. A
// line that cleaning as wiki text would change is left out, so the length
// counted is the length shown. It is "" without such constraints.
func constraintsBlock(ctx context.Context, o Options) string {
	rows, err := readKnowledge(ctx, o)
	if err != nil {
		return ""
	}
	rows = knowledgepage.Constraints(rows, o.Project.Key)
	bin := filepath.Join(o.Dirs.Share, "bin", "eva-md")
	left := constraintsCap
	var lines []string
	for i, r := range rows {
		if len(lines) == maxConstraints {
			break
		}
		// Each line still to come gets an even share of what is left, so a
		// long one can't crowd out the others; a short one leaves its rest
		// to them.
		slots := min(maxConstraints-len(lines), len(rows)-i)
		if line := AsShown(constraint(r, bin, (left-(slots-1))/slots)); line != "" {
			lines = append(lines, line)
			left -= runes(line) + 1
		}
	}
	return strings.Join(lines, "\n")
}

// constraint is a constraint's line, "Constraint: <title>: <statement>:
// <bin> show knowledge/k<N>", in at most limit runes. The statement is cut
// to fit or left out, then the title is cut; the command never is, so a
// line without room for it is "".
func constraint(r knowledgepage.Row, bin string, limit int) string {
	title, statement := oneLine(r.Title, knowledgepage.MaxTitle), oneLine(r.Statement, knowledgepage.MaxStatement)
	head, tail := "Constraint: "+title, ": "+bin+" show "+r.Ref()
	if statement != "" {
		if room := limit - runes(head+": "+tail); room >= min(runes(statement), minDescription) {
			return head + ": " + cut(statement, room) + tail
		}
	}
	if line := head + tail; runes(line) <= limit {
		return line
	}
	if room := limit - runes("Constraint: "+tail); room > 0 {
		return "Constraint: " + cut(title, room) + tail
	}
	return ""
}

// readKnowledge reads the recall index through the wiki's pinned root, by
// its path alone (see knowledgepage.ReadIndex). A missing index is an error
// that wraps fs.ErrNotExist.
func readKnowledge(ctx context.Context, o Options) ([]knowledgepage.Row, error) {
	if o.Config.WikiPath == "" {
		return nil, errors.New("resume: no wiki is set up")
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	root, err := openWiki(o.Config)
	if err != nil {
		return nil, err
	}
	defer root.Close()
	return knowledgepage.ReadIndex(root)
}

// KnowledgeInfo is what eva_status says of the knowledge pages: how many
// the recall index lists, how many of them are constraints that hold for
// the project, and whether the knowledge map exists.
type KnowledgeInfo struct {
	// Problem says why the recall index could not be read: no wiki is set
	// up, the wiki is unreadable or another instance's, or the index is
	// unreadable, or missing beside the knowledge folder or map; "" when it
	// was read, or is missing with no knowledge yet.
	Problem     string
	Found       bool // the recall index exists and was read
	Pages       int  // the active pages it lists
	Constraints int  // ... of which constraints of the project or of every project
	Map         bool // knowledge/map.md exists
}

// KnowledgeFor reads the recall index by its path and looks for the
// knowledge map and folder, for eva_status. It reads nothing else: no
// listing and no page. A missing index is no problem when the knowledge
// folder and the map are missing too, as the wiki has no knowledge yet; with
// either of them, the index is missing.
func KnowledgeFor(ctx context.Context, o Options) KnowledgeInfo {
	var k KnowledgeInfo
	root, err := openWiki(o.Config)
	switch {
	case o.Config.WikiPath == "":
		k.Problem = "no wiki is set up"
		return k
	case errors.Is(err, errInstance):
		k.Problem = "the wiki is not the instance the config names"
		return k
	case err != nil:
		k.Problem = "the wiki is unreadable"
		return k
	}
	defer root.Close()
	if info, err := root.Lstat(filepath.FromSlash(knowledgepage.MapRef + ".md")); err == nil {
		k.Map = info.Mode().IsRegular()
	}
	if ctx.Err() != nil {
		k.Problem = "unknown"
		return k
	}
	rows, err := knowledgepage.ReadIndex(root)
	switch {
	case errors.Is(err, fs.ErrNotExist):
		// Without the index, knowledge pages or their map mean it is gone,
		// not that the wiki has no knowledge yet.
		if _, err := root.Lstat(knowledgepage.Folder); err == nil || k.Map {
			k.Problem = "the recall index is missing; eva-md doctor builds it again"
		}
	case err != nil:
		k.Problem = "the recall index is unreadable; eva-md doctor builds it again"
	default:
		k.Found, k.Pages, k.Constraints = true, len(rows), len(knowledgepage.Constraints(rows, o.Project.Key))
	}
	return k
}

// listKnowledge returns the knowledge pages, k<N>.md, whatever the window,
// as listSystems does: they hold what lasts, which an old as_of does not
// make stale. The map and the folder's index are no pages to search.
func (s *scan) listKnowledge(root *os.Root) []pageSource {
	pages := s.listFiles(root, knowledgepage.Folder, false, func(name string) bool {
		_, ok := knowledgepage.ParseID(name)
		return ok
	})
	for i := range pages {
		pages[i].folder, pages[i].knowledge = knowledgepage.Folder, true
	}
	return pages
}

// knowledgeFile reads one knowledge page whole, at most maxKnowledge bytes,
// with knowledgepage.Lenient, no okf call, and matches two lines of it at
// the page's ref, dated by its as_of: "<kind> [<tag>]: <title>:
// <statement>", and, when it cites code, "code: <source>, <source>", which a
// path also matches as a system page's code line does (see codeScope). A
// retired page's hits are tagged [retired], the others' [knowledge]. A page
// that fails to open or read, or that Lenient can't read, is named as
// unreadable.
func (s *scan) knowledgeFile(root *os.Root, src pageSource, loc *time.Location) {
	f, err := regular(root, filepath.FromSlash(src.rel))
	if err != nil {
		s.badPages = append(s.badPages, src.id)
		return
	}
	defer f.Close()
	data, err := io.ReadAll(io.LimitReader(f, maxKnowledge+1))
	s.pageBytes += int64(len(data))
	if err != nil || len(data) > maxKnowledge {
		s.badPages = append(s.badPages, src.id)
		return
	}
	e, ok := knowledgepage.Lenient(data)
	if !ok {
		s.badPages = append(s.badPages, src.id)
		return
	}
	asOf, _ := time.ParseInLocation(time.DateOnly, e.AsOf, loc)
	tag := "knowledge"
	if e.State == knowledgepage.Retired {
		tag = "retired"
	}
	head := e.Kind
	if e.Tag != "" {
		head = strings.TrimSpace(head + " [" + e.Tag + "]")
	}
	text := e.Title + ": " + e.Statement
	if head != "" {
		text = head + ": " + text
	}
	page := unit{ts: asOf, day: true, role: tag, project: src.folder, ref: src.id, file: src.rel, page: true, knowledge: true}
	header := page
	header.text, header.shown, header.line = text, text, max(lineOf(data, "\n## Statement\n")+1, 1)
	s.match(header)
	var code []citedPath
	text = "code:"
	for _, source := range e.Code {
		r, ok := coderef.Parse(source)
		if !ok {
			continue
		}
		if len(code) > 0 {
			text += ","
		}
		text += " "
		code = append(code, cite(len(strings.ToLower(text)), source, r))
		text += source
	}
	if len(code) > 0 {
		cited := page
		cited.text, cited.shown, cited.line, cited.code = text, text, lineOf(data, "\nsources:\n"), code
		s.match(cited)
	}
	s.knowledgeCount++
}

// lineOf is the 1-based number of the line that starts after marker's
// leading line break, or 0 when data doesn't hold marker.
func lineOf(data []byte, marker string) int {
	i := strings.Index(string(data), marker)
	if i < 0 {
		return 0
	}
	return strings.Count(string(data[:i]), "\n") + 2
}
