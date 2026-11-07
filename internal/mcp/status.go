package mcp

import (
	"context"
	"fmt"
	"strings"

	"github.com/heyNag/eva-md/internal/coderef"
	"github.com/heyNag/eva-md/internal/health"
	"github.com/heyNag/eva-md/internal/identity"
	"github.com/heyNag/eva-md/internal/redact"
	"github.com/heyNag/eva-md/internal/resume"
)

// status is eva_status: in about 1 KB, what eva.md holds and does for this
// project, from readers that exist, with no model, no host CLI and nothing
// written: the scope, the thread page and its Now, the pending update, the
// project's systems, the knowledge pages, the hooks, the updates and the
// daily commit, the chat-start notice, and what to read next. A part that
// the deadline cut off says unknown.
func status(ctx context.Context, s *server, a arguments) result {
	c, r := s.load(ctx)
	if r != nil {
		return *r
	}
	cwd, err := a.folder()
	if err != nil {
		return failure(err.Error())
	}
	p, folder, r := s.project(ctx, cwd)
	if r != nil {
		return *r
	}
	if c.Excluded(folder, p.Remote) {
		return text("This folder is excluded in eva-md's config; eva.md keeps nothing for it.")
	}
	if p.Branch != "" {
		// The thread key the hooks store with each entry (see redact.Identifier).
		p.Branch, _ = redact.Identifier(p.Branch, 1024)
	}
	if p.Branch == "" {
		p.Branch = identity.DefaultBranch
	}
	o := resume.Options{Dirs: s.Dirs, Config: c, Project: p, Host: s.Host, Now: s.Now()}
	page := resume.ProjectPage(ctx, o)
	var lines []string
	if p.Kind == identity.KindGeneral {
		lines = append(lines, scope(folder, false))
	} else {
		lines = append(lines, "scope: "+p.Key+" · branch "+p.Branch+" · thread "+page.Page)
	}
	lines = append(lines, pageLine(ctx, s, page))

	sessions, size, err := health.Backlog(ctx, s.Dirs, p)
	pending := "pending update: none"
	switch {
	case err != nil:
		pending = "pending update: unknown"
	case c.WikiPath == "":
		pending = "pending update: no wiki is set up, so no update runs"
	case sessions > 0:
		pending = fmt.Sprintf("pending update: %d %s, about %d KB unprocessed", sessions, plural(sessions, "session", "sessions"), kb(int(size)))
	}
	if !c.Updates {
		pending += "; updates are off (updates = false)"
	}
	lines = append(lines, pending)

	systems, total := resume.SystemsFor(ctx, o)
	switch {
	case ctx.Err() != nil:
		lines = append(lines, "systems: unknown")
	case total == 0:
		lines = append(lines, "systems: no system pages")
	case len(systems) == 0:
		lines = append(lines, fmt.Sprintf("systems for this project: none (the map lists %d)", total))
	default:
		// Each says how far the code it cites moved, as the pointers say it.
		moves := resume.CodeMoves(ctx, o)
		var slugs []string
		for _, e := range systems[:min(len(systems), 10)] {
			if m, ok := moves[e.Slug]; ok {
				slugs = append(slugs, e.Slug+" ("+coderef.Note(e.AsOf, m)+")")
				continue
			}
			slugs = append(slugs, e.Slug)
		}
		if n := len(systems) - len(slugs); n > 0 {
			slugs = append(slugs, fmt.Sprintf("and %d more", n))
		}
		lines = append(lines, fmt.Sprintf("systems for this project: %s (the map lists %d)", strings.Join(slugs, ", "), total))
	}
	know := resume.KnowledgeFor(ctx, o)
	lines = append(lines, knowledgeLine(know))

	if ctx.Err() != nil {
		lines = append(lines, "hooks: unknown", "updates: unknown")
	} else {
		lines = append(lines, health.Summary(ctx, s.Dirs, c, s.Now())...)
	}
	if notice := health.QuickNotice(ctx, s.Dirs, c); notice != "" {
		lines = append(lines, "notice: "+strings.TrimPrefix(notice, "eva.md: "))
	}

	var reads []string
	if page.Found {
		reads = append(reads, "eva_show "+page.Page+"#Now", "eva_show "+page.Page+"#Timeline")
	}
	if total > 0 {
		// The index lists each system in a line of its title and
		// description, where the map adds its repos and every code source.
		reads = append(reads, "eva_show systems/index")
	}
	if know.Map {
		reads = append(reads, "eva_show knowledge/map")
	}
	if len(reads) == 0 {
		reads = append(reads, "eva_search with a few words finds earlier chats")
	}
	lines = append(lines, "read: "+strings.Join(reads, " · "))
	return text(capText(strings.Join(lines, "\n"), statusCap, func(string) string { return "eva-md status shows the rest" }))
}

// pageLine words the thread page: its Now's date and items and the standing
// rules, or why there is none.
func pageLine(ctx context.Context, s *server, page resume.PageInfo) string {
	if ctx.Err() != nil {
		return "thread page: unknown"
	}
	if page.Problem != "" {
		return "thread page: " + page.Problem
	}
	line := "thread page: none yet for this branch"
	if page.Found {
		asOf := "an unknown date"
		if !page.AsOf.IsZero() {
			asOf = page.AsOf.In(s.Now().Location()).Format("2006-01-02 15:04")
		}
		line = "thread page: " + page.Page + ", Now as of " + asOf
		if page.Counted {
			line += fmt.Sprintf(" (%d %s in force, %d next %s, %d open %s)", page.Decisions, plural(page.Decisions, "decision", "decisions"),
				page.Next, plural(page.Next, "step", "steps"), page.Open, plural(page.Open, "question", "questions"))
		}
	}
	if page.ProjectRules+page.GeneralRules == 0 {
		return line + "; no standing rules"
	}
	return line + fmt.Sprintf("; standing rules: %d for this project, %d for all projects", page.ProjectRules, page.GeneralRules)
}

// knowledgeLine words what the recall index lists: the knowledge pages and
// the constraints that hold for the project, or why it says nothing.
func knowledgeLine(k resume.KnowledgeInfo) string {
	switch {
	case k.Problem != "":
		return "knowledge: " + k.Problem
	case !k.Found:
		return "knowledge: none yet"
	}
	return fmt.Sprintf("knowledge: %d %s, %d %s for this project", k.Pages, plural(k.Pages, "page", "pages"), k.Constraints, plural(k.Constraints, "constraint", "constraints"))
}

func plural(n int, one, many string) string {
	if n == 1 {
		return one
	}
	return many
}
