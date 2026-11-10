package health

import (
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"time"

	"github.com/heyNag/eva-md/internal/coderef"
	"github.com/heyNag/eva-md/internal/config"
	"github.com/heyNag/eva-md/internal/paths"
	"github.com/heyNag/eva-md/internal/systempage"
)

func init() {
	registerRows(1040, "system code", func(r *Report, in *inspection) {
		if c, _ := in.config(); c.WikiPath != "" {
			inspectCode(r, in.d, c, in.now())
		}
	})
}

// inspectCode adds the row of the code the system pages cite, from the code
// status the day's maintenance and eva-md system status take,
// <state>/code-status.json, read with a 1 MiB bound. It is never FAIL: code
// that moved, a repo with no clone on this Mac and a map from before the map
// carried code are no faults of eva-md. A wiki without system pages gets no
// row.
func inspectCode(r *Report, d paths.Dirs, c config.Config, now time.Time) {
	if !hasSystemPages(c) {
		return
	}
	s, found, err := coderef.Load(d.State, coderef.Limit)
	switch {
	case err != nil:
		r.add("system code", "UNKNOWN", "the code status is unreadable", "eva-md system status takes it again")
	case !found:
		r.add("system code", "UNKNOWN", "no code status yet", "it is taken with the day's maintenance at the first chat event of a day; eva-md system status takes it now")
	default:
		status, detail, fix := codeRow(s, now)
		r.add("system code", status, detail, fix)
	}
}

// hasSystemPages reports whether the wiki's systems folder holds a page.
func hasSystemPages(c config.Config) bool {
	entries, _ := os.ReadDir(filepath.Join(c.WikiPath, "systems"))
	for _, e := range entries {
		if slug, ok := strings.CutSuffix(e.Name(), ".md"); ok && e.Type().IsRegular() && systempage.CheckSlug(slug) == nil {
			return true
		}
	}
	return false
}

// uncheckedWords say why a page's code in a repo wasn't measured.
var uncheckedWords = map[string]string{
	coderef.NoClone:       "no clone on this Mac",
	coderef.UnknownCommit: "commit not in the clone",
	coderef.TimedOut:      "git ran out of time",
	coderef.GitFailed:     "git failed",
	coderef.NotRun:        "past the bounds of a pass",
}

// codeRow words a code status as a check: UNKNOWN while a page's code could
// not be measured, a page couldn't be read or the map lacks the code of
// pages written before it carried code, and PASS otherwise, with the facts:
// what moved, is unchanged, cites a path gone at HEAD or one its commit
// lacks, cites a commit off its clone's branch with nothing counted since
// the two forked, or is past its review day.
func codeRow(s coderef.Report, now time.Time) (status, detail, fix string) {
	today := now.Format(time.DateOnly)
	tied, unchanged := 0, 0
	var moved, gone, lacks, off, due, unchecked []string
	why := map[string][]string{}
	var reasons []string
	for _, p := range s.Pages {
		if p.Due(today) {
			due = append(due, p.Slug)
		}
		if len(p.Repos) == 0 {
			continue
		}
		tied++
		measured := true
		for _, rs := range p.Repos {
			if !rs.Checked() {
				measured = false
				reason := uncheckedWords[rs.State]
				if !slices.Contains(reasons, reason) {
					reasons = append(reasons, reason)
				}
				if !slices.Contains(why[reason], p.Slug) {
					why[reason] = append(why[reason], p.Slug)
				}
			}
			switch {
			case rs.Lacks() && !slices.Contains(lacks, p.Slug):
				lacks = append(lacks, p.Slug)
			case !rs.Lacks() && len(rs.Gone) > 0 && !slices.Contains(gone, p.Slug):
				gone = append(gone, p.Slug)
			}
			if _, ok := rs.Movement(); rs.State == coderef.NotOnBranch && !ok && !slices.Contains(off, p.Slug) {
				off = append(off, p.Slug)
			}
		}
		if !measured {
			unchecked = append(unchecked, p.Slug)
		}
		switch m, ok := p.Movement(); {
		case ok && (m.Count > 0 || m.Capped):
			moved = append(moved, p.Slug+": "+strings.TrimPrefix(m.Words(), "code moved "))
		case ok && measured:
			unchanged++
		}
	}
	of := " (status of " + when(s.At) + ")"
	var facts, fixes []string
	if tied == 0 {
		facts = append(facts, fmt.Sprintf("none of the %d system %s cites code%s", len(s.Pages), plural(len(s.Pages), "page", "pages"), of))
	} else {
		head := fmt.Sprintf("%d %s tied to code%s: %d unchanged, %d moved", tied, plural(tied, "page", "pages"), of, unchanged, len(moved))
		if len(moved) > 0 {
			head += " (" + list(moved) + ")"
		}
		facts = append(facts, head)
	}
	if len(gone) > 0 {
		facts = append(facts, fmt.Sprintf("%d %s a path gone at HEAD (%s)", len(gone), plural(len(gone), "cites", "cite"), list(gone)))
	}
	if len(lacks) > 0 {
		facts = append(facts, fmt.Sprintf("%d %s a path its commit lacks (%s)", len(lacks), plural(len(lacks), "cites", "cite"), list(lacks)))
	}
	if len(off) > 0 {
		facts = append(facts, fmt.Sprintf("%d %s a commit off its clone's branch (%s)", len(off), plural(len(off), "cites", "cite"), list(off)))
	}
	if len(unchecked) > 0 {
		var parts []string
		for _, reason := range reasons {
			parts = append(parts, reason+": "+list(why[reason]))
		}
		facts = append(facts, fmt.Sprintf("%d of %d could not be checked (%s)", len(unchecked), tied, strings.Join(parts, "; ")))
		if len(why[uncheckedWords[coderef.NoClone]]) > 0 {
			fixes = append(fixes, "chat once in each repo's clone, then run eva-md system status")
		}
		if len(why[uncheckedWords[coderef.UnknownCommit]]) > 0 {
			fixes = append(fixes, "fetch the cited commits into the clones, then run eva-md system status")
		}
		if len(fixes) == 0 {
			fixes = append(fixes, "run eva-md system status")
		}
	}
	if n := len(s.Unreadable); n > 0 {
		facts = append(facts, fmt.Sprintf("%d %s unreadable: %s", n, plural(n, "page is", "pages are"), list(s.Unreadable)))
		fixes = append(fixes, "fix or remove the unreadable pages by hand")
	}
	if n := s.MapWithout; n > 0 {
		facts = append(facts, fmt.Sprintf("the map lacks the code of %d %s written before it carried code", n, plural(n, "page", "pages")))
		fixes = append(fixes, "write any system page again with eva-md system write <slug> --replace, which rebuilds the map")
	}
	if len(due) > 0 {
		facts = append(facts, fmt.Sprintf("%d past %s review date (%s)", len(due), plural(len(due), "its", "their"), list(due)))
	}
	detail = strings.Join(facts, "; ")
	if len(fixes) > 0 {
		return "UNKNOWN", detail, strings.Join(fixes, "; ")
	}
	if len(moved)+len(gone)+len(lacks)+len(due) > 0 {
		fix = "in a chat, ask to digest those systems again; the skill cites the new commit"
	}
	return "PASS", detail, fix
}
