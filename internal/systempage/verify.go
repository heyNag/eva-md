package systempage

import (
	"context"
	"errors"
	"fmt"
	"path/filepath"
	"slices"
	"strings"
	"time"

	"github.com/heyNag/eva-md/internal/coderef"
	"github.com/heyNag/eva-md/internal/identity"
)

// VerifyBudget bounds the check of a page's code sources against local
// clones, and VerifyCall each of its git calls. Tests raise them before any
// test runs, so a slow machine can't fail a test of what a write finds; a
// test of the check's bound passes its own, shorter one in Options.
var (
	VerifyBudget = 3 * time.Second
	VerifyCall   = time.Second
)

// missingRule is the refusal of a code source whose path the cited commit
// lacks.
const missingRule = "a code source names a path that does not exist in its repo at the cited commit (checked in a local clone)"

// verify checks the code a page cites against a local clone of each repo,
// before the write and for at most 3 s: the working folder's clone when it
// holds the repo, then the clones the identity cache knows (see
// coderef.Finder), at most 3 for each commit. A path missing at a commit the
// clone has refuses the page, naming the source's line. Anything it can't
// check comes back as a note: a repo with no clone on this Mac, a commit the
// clones lack, as one not fetched, git failing or the time running out. It
// measures no movement and never fetches, and a write never waits on a
// clone.
func verify(ctx context.Context, o Options, text string, pg page) ([]string, error) {
	code := codeRefs(pg.sources)
	if len(code) == 0 {
		return nil, nil
	}
	budget := VerifyBudget
	if o.verifyBudget > 0 {
		budget = o.verifyBudget
	}
	ctx, cancel := context.WithTimeout(ctx, budget)
	defer cancel()
	cache := identity.Cache{Dir: filepath.Join(o.Dirs.Cache(), "identity")}
	var prefer identity.Project
	if filepath.IsAbs(o.Cwd) {
		prefer, _ = cache.Resolve(ctx, o.Cwd)
	}
	var keys []string
	for _, r := range code {
		if !slices.Contains(keys, r.Key) {
			keys = append(keys, r.Key)
		}
	}
	finder := coderef.NewFinder(cache, keys, prefer)
	var notes []string
	var p problems
	var lines map[coderef.Ref]int
	for _, refs := range coderef.Groups(code) {
		key, commit := refs[0].Key, refs[0].Commit
		at := coderef.Readable(key) + " at " + commit
		var paths []string
		for _, r := range refs {
			if r.Path != "" {
				paths = append(paths, r.Path)
			}
		}
		clones := finder.Clones(ctx, key, 3)
		if ctx.Err() != nil {
			notes = append(notes, "not checked: "+at+": the check ran out of time")
			continue
		}
		if len(clones) == 0 {
			notes = append(notes, "not checked: "+at+": no clone of it is known on this Mac; eva-md learns clones from chats in them")
			continue
		}
		var lacking []string
		checked, failed := false, ""
		for _, c := range clones {
			call, cancel := context.WithTimeout(ctx, VerifyCall)
			has, err := coderef.HasCommit(call, c.Root, commit)
			cancel()
			var missing []string
			if err == nil && has {
				call, cancel = context.WithTimeout(ctx, VerifyCall)
				missing, err = coderef.Missing(call, c.Root, commit, paths)
				cancel()
			}
			switch {
			case err != nil:
				failed = "git failed in " + c.Root
				if errors.Is(err, coderef.ErrTimeout) {
					failed = "the check ran out of time"
				}
				continue
			case !has:
				lacking = append(lacking, c.Root)
				continue
			}
			if len(missing) > 0 && lines == nil {
				lines = codeLines(text)
			}
			for _, path := range missing {
				p.add(lines[coderef.Ref{Key: key, Commit: commit, Path: path}], missingRule)
			}
			notes = append(notes, fmt.Sprintf("checked %d code %s of %s in %s", len(refs), plural(len(refs), "source", "sources"), at, c.Root))
			checked = true
			break
		}
		switch {
		case checked:
		case len(lacking) > 0:
			notes = append(notes, "not checked: "+at+": the commit is not in the clone at "+strings.Join(lacking, " or ")+"; fetch it there to check")
		default:
			notes = append(notes, "not checked: "+at+": "+failed)
		}
	}
	if len(p.msgs) > 0 {
		return nil, &Refusal{Problems: p.list()}
	}
	return notes, nil
}

// codeLines returns the line of each code source in a page's text, by the
// ref it reads as; a ref cited twice keeps its first line.
func codeLines(text string) map[coderef.Ref]int {
	out := map[coderef.Ref]int{}
	lines := strings.Split(text, "\n")
	end := slices.Index(lines[1:], "---") + 1
	if end == 0 {
		return out
	}
	f, ok := parseFront(lines[1:end], 2, &problems{})["sources"]
	if !ok {
		return out
	}
	items, _ := list(f)
	for _, it := range items {
		rest, _ := strings.CutPrefix(it.text, "resource:")
		v, _ := unquote(strings.TrimSpace(rest))
		if r, ok := coderef.Parse(v); ok {
			if _, seen := out[r]; !seen {
				out[r] = it.line
			}
		}
	}
	return out
}

func plural(n int, one, many string) string {
	if n == 1 {
		return one
	}
	return many
}
