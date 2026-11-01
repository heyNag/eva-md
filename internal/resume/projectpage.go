package resume

import (
	"context"
	"errors"
	"slices"
	"time"

	"github.com/heyNag/eva-md/internal/systempage"
)

// PageInfo is what eva_status says of a project's thread page: where it
// is, how old its Now is and what it holds, and how many standing rules
// apply.
type PageInfo struct {
	// Problem says why no page could be looked for: no wiki is set up, or
	// the wiki is unreadable or another instance's; "" when it was read.
	Problem string
	Page    string    // the thread page's id, or the id it would have
	Found   bool      // the page has a Now
	AsOf    time.Time // the Now's as_of; zero when unknown
	// Counted reports a Now in the thread page format, whose items are
	// counted: decisions in force, next steps and open questions.
	Counted               bool
	Decisions, Next, Open int
	ProjectRules          int // the standing rules for this project
	GeneralRules          int // ... and for all projects
}

// ProjectPage finds the thread page of o's project and branch as a new
// chat's brief does, through the project's now page, so a folder under
// another name is found too, and counts what its Now and the rules pages
// hold. It writes nothing.
func ProjectPage(ctx context.Context, o Options) PageInfo {
	if o.Project.Branch == "" {
		o.Project.Branch = "default"
	}
	info := PageInfo{Page: threadID(o.Project)}
	root, err := openWiki(o.Config)
	switch {
	case o.Config.WikiPath == "":
		info.Problem = "no wiki is set up"
		return info
	case errors.Is(err, errInstance):
		info.Problem = "the wiki is not the instance the config names"
		return info
	case err != nil:
		info.Problem = "the wiki is unreadable"
		return info
	}
	root.Close()
	o.Variant = "startup"
	pg := readPage(ctx, o)
	if pg.slug != "" {
		p := o.Project
		p.Slug = pg.slug
		info.Page = threadID(p)
	}
	info.Found, info.AsOf = pg.now != "", pg.asOf
	info.ProjectRules, info.GeneralRules = len(pg.rules.project), len(pg.rules.general)
	if secs, ok := parseNow(pg.now); ok {
		info.Counted = true
		info.Decisions, info.Next, info.Open = len(secs[nowDecisions]), len(secs[nowNext]), len(secs[nowQuestions])
	}
	return info
}

// SystemsFor returns the systems the system map lists whose repos include
// o's project, in the map's order, and how many systems it lists. It reads
// systems/map.md alone, as the brief's pointers do.
func SystemsFor(ctx context.Context, o Options) ([]systempage.MapEntry, int) {
	entries := readMap(ctx, o)
	var out []systempage.MapEntry
	for _, e := range entries {
		if slices.Contains(e.Repos, o.Project.Key) {
			out = append(out, e)
		}
	}
	return out, len(entries)
}
