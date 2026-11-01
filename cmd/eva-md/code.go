package main

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"

	"github.com/heyNag/eva-md/internal/coderef"
	"github.com/heyNag/eva-md/internal/config"
	"github.com/heyNag/eva-md/internal/identity"
	"github.com/heyNag/eva-md/internal/paths"
	"github.com/heyNag/eva-md/internal/systempage"
)

// Bounds of measuring the code system pages cite: a pass over the pages, as
// system status and the day's maintenance run it, and eva-md show's check of
// one page, with each of its git calls. Tests raise them before any test
// runs, so a slow machine can't fail a test of what a measurement finds.
var (
	passTime = 120 * time.Second
	showTime = 3 * time.Second
	showCall = time.Second
)

// runSystemStatus runs eva-md system status [<slug>...] [--json]: it measures
// the code the system pages cite, or the pages named, in the local clones
// the identity cache knows, the working folder's first, and saves what it
// found as the code status the prompt hook and the doctor read, updating
// only the named pages' entries when slugs are given. It prints a line for
// each repo and commit a page cites, most moved first, a line for each page
// past its review day, then the counts; with --json, the status measured. It
// exits 0 whatever the code did, 1 when the wiki can't be read or the status
// can't be saved, and 2 on a usage error.
func runSystemStatus(ctx context.Context, args []string, out, errOut io.Writer) int {
	f := flag.NewFlagSet("system status", flag.ContinueOnError)
	f.SetOutput(io.Discard)
	asJSON := f.Bool("json", false, "")
	slugs, err := interleaved(f, args)
	if err != nil {
		return usage(errOut, systemUsage)
	}
	for _, slug := range slugs {
		if err := systempage.CheckSlug(slug); err != nil {
			return usage(errOut, err.Error())
		}
	}
	dirs, err := paths.Default()
	if err != nil {
		fmt.Fprintln(errOut, "eva-md: home folder is unavailable")
		return 1
	}
	c, err := config.Load(dirs.ConfigFile())
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			fmt.Fprintln(errOut, "eva-md: no wiki is set up; run eva-md setup first")
		} else {
			fmt.Fprintln(errOut, "eva-md: configuration is invalid")
		}
		return 1
	}
	code, err := systempage.ReadCode(c, dirs, slugs...)
	if err != nil {
		fmt.Fprintln(errOut, "eva-md:", err)
		return 1
	}
	ctx, cancel := context.WithTimeout(ctx, passTime)
	defer cancel()
	cache := identity.Cache{Dir: filepath.Join(dirs.Cache(), "identity")}
	r := coderef.Status(ctx, coderef.Options{Cache: cache, Prefer: here(ctx, cache), Last: !*asJSON}, code.Pages)
	r.Unreadable, r.MapWithout = code.Unreadable, code.MapWithout
	saved := r
	if len(slugs) > 0 {
		if old, ok, err := coderef.Load(dirs.State, coderef.Limit); err == nil && ok {
			saved = coderef.Merge(old, r)
		}
	}
	if err := coderef.Save(dirs.State, saved); err != nil {
		fmt.Fprintln(errOut, "eva-md: the code status can't be saved:", err)
		return 1
	}
	if *asJSON {
		if err := json.NewEncoder(out).Encode(r); err != nil {
			return 1
		}
		return 0
	}
	today := time.Now().Format(time.DateOnly)
	for _, p := range byMovement(r.Pages) {
		for _, line := range pageLines(p, today) {
			fmt.Fprintln(out, p.Slug+": "+line)
		}
	}
	fmt.Fprintln(out, codeSummary(r, today))
	return 0
}

// codeStep takes the day's code status after the lint: it measures the code
// the system pages cite, as system status does, within 120 s, saves it and
// prints one line. A wiki without system pages has nothing to measure and
// gets no line. Only a wiki that can't be read, or a status that can't be
// saved, fails the step; code that moved or couldn't be checked never does.
func codeStep(ctx context.Context, c config.Config, dirs paths.Dirs, out, errOut io.Writer) int {
	code, err := systempage.ReadCode(c, dirs)
	if err != nil {
		fmt.Fprintln(errOut, "eva-md: code status:", err)
		return 1
	}
	if len(code.Pages)+len(code.Unreadable) == 0 {
		return 0
	}
	ctx, cancel := context.WithTimeout(ctx, passTime)
	defer cancel()
	r := coderef.Status(ctx, coderef.Options{Cache: identity.Cache{Dir: filepath.Join(dirs.Cache(), "identity")}}, code.Pages)
	r.Unreadable, r.MapWithout = code.Unreadable, code.MapWithout
	if err := coderef.Save(dirs.State, r); err != nil {
		fmt.Fprintln(errOut, "eva-md: code status:", err)
		return 1
	}
	fmt.Fprintln(out, "maintain: code status: "+codeSummary(r, time.Now().Format(time.DateOnly)))
	return 0
}

// showCode prints on errOut, after eva-md show printed a system page or a
// section of it, what the code the page cites has done since: a line for
// each repo and commit it cites, and one once its review day has come, each
// starting "eva.md: systems/<slug>: ". It measures within 3 s, 1 s a git
// call, in at most 3 of each repo's clones the identity cache knows, the
// working folder's first. The map, other refs and a page that cites no code
// and has no review due get no line, so stdout stays the page alone, as
// system write takes it back. It saves no code status.
func showCode(ctx context.Context, d paths.Dirs, c config.Config, ref string, errOut io.Writer) {
	page, _, _ := strings.Cut(ref, "#")
	slug, ok := strings.CutPrefix(strings.TrimSuffix(page, ".md"), "systems/")
	if !ok || systempage.CheckSlug(slug) != nil {
		return
	}
	code, err := systempage.ReadCode(c, d, slug)
	if err != nil || len(code.Pages) != 1 {
		return
	}
	p, today := code.Pages[0], time.Now().Format(time.DateOnly)
	if len(p.Refs) == 0 && !(coderef.PageStatus{ReviewBy: p.ReviewBy}).Due(today) {
		return
	}
	ctx, cancel := context.WithTimeout(ctx, showTime)
	defer cancel()
	cache := identity.Cache{Dir: filepath.Join(d.Cache(), "identity")}
	ps := coderef.Check(ctx, coderef.Options{Cache: cache, Prefer: here(ctx, cache), PerCall: showCall, Last: true}, p)
	for _, line := range pageLines(ps, today) {
		fmt.Fprintln(errOut, "eva.md: systems/"+slug+": "+line)
	}
}

// here is the project of the working folder, through the identity cache, or
// none.
func here(ctx context.Context, cache identity.Cache) identity.Project {
	cwd, err := os.Getwd()
	if err != nil {
		return identity.Project{}
	}
	p, err := cache.Resolve(ctx, cwd)
	if err != nil {
		return identity.Project{}
	}
	return p
}

// pageLines says what measuring a page's code found: a line for each repo
// and commit it cites, and one when its review day has come by today.
func pageLines(p coderef.PageStatus, today string) []string {
	var lines []string
	for _, r := range p.Repos {
		lines = append(lines, repoLine(p.AsOf, r))
	}
	if p.Due(today) {
		lines = append(lines, "review due since "+p.ReviewBy)
	}
	return lines
}

// repoLine says what measuring a page's code in one repo found, as show and
// system status print it: how far the code moved since the page's as_of, or
// "not counted" for code whose count says no movement (a path its commit
// lacks, a commit off the branch with nothing counted), with the repo,
// commit and paths it cites, the clone and branch it was measured in, the
// paths HEAD's tree lacks, or the commit does, and the newest change; or why
// it wasn't checked. Paths, the clone and the branch
// are printed with any control, format or space character but a plain space
// as "?", as a status file an older eva-md wrote may hold them.
func repoLine(asOf string, r coderef.RepoStatus) string {
	r.Clone, r.Branch = shown(r.Clone), shown(r.Branch)
	r.Paths, r.Gone = shownAll(r.Paths), shownAll(r.Gone)
	at := coderef.Readable(r.Key) + " at " + r.Commit
	switch r.State {
	case coderef.NoClone:
		return "not checked: no clone of " + coderef.Readable(r.Key) + " is known on this Mac; eva-md learns clones from chats in them"
	case coderef.UnknownCommit:
		return "not checked: commit " + r.Commit + " of " + coderef.Readable(r.Key) + " is not in the clone at " + r.Clone + "; fetch it there to check"
	case coderef.TimedOut:
		return "not checked: git ran out of time in " + r.Clone + " (" + at + ")"
	case coderef.GitFailed:
		return "not checked: git failed in " + r.Clone + " (" + at + ")"
	case coderef.NotRun:
		return "not checked: " + at + " is past the bounds of a pass"
	}
	m, measured := r.Movement()
	words := coderef.Note(asOf, m)
	switch {
	case !measured:
		words = "not counted"
	case words == "":
		words = m.Words() + " since " + r.Commit
	}
	if len(r.Paths) > 0 {
		at += ": " + strings.Join(r.Paths, ", ")
	}
	facts := []string{at, "measured in " + r.Clone + onBranch(r.Branch)}
	switch {
	case r.State == coderef.NotOnBranch && !measured:
		facts = append(facts, "commit "+r.Commit+" is not on this branch, with nothing counted since the commit the two have in common")
	case r.State == coderef.NotOnBranch:
		facts = append(facts, "commit "+r.Commit+" is not on this branch, so counted since the commit the two have in common")
	}
	switch {
	case r.Lacks():
		facts = append(facts, "cites a path its commit lacks: "+strings.Join(r.Gone, ", "))
	case len(r.Gone) > 0:
		facts = append(facts, "gone at HEAD: "+strings.Join(r.Gone, ", "))
	}
	if r.Last != "" {
		facts = append(facts, "last change "+r.Last)
	}
	return words + " (" + strings.Join(facts, "; ") + ")"
}

// shown is s with each control, format or space character but a plain
// space, and each byte that is not UTF-8, printed as "?".
func shown(s string) string {
	return strings.Map(func(r rune) rune {
		if r == utf8.RuneError || unicode.IsControl(r) || unicode.Is(unicode.Cf, r) || unicode.IsSpace(r) && r != ' ' {
			return '?'
		}
		return r
	}, s)
}

func shownAll(list []string) []string {
	out := make([]string, len(list))
	for i, s := range list {
		out[i] = shown(s)
	}
	return out
}

func onBranch(branch string) string {
	if branch == "" {
		return " at a detached HEAD"
	}
	return " on " + branch
}

// byMovement orders pages as system status prints them: the most moved
// first, then those that couldn't be checked or whose count says no
// movement (a path its commit lacks, a commit off the branch with nothing
// counted), then the unchanged, then those that cite no code; pages tie by
// slug.
func byMovement(pages []coderef.PageStatus) []coderef.PageStatus {
	rank := func(p coderef.PageStatus) (int, int) {
		m, ok := p.Movement()
		switch {
		case len(p.Repos) == 0:
			return 3, 0
		case ok && (m.Count > 0 || m.Capped):
			return 0, -m.Count
		case !ok || slices.ContainsFunc(p.Repos, func(r coderef.RepoStatus) bool { return !r.Checked() }):
			return 1, 0
		}
		return 2, 0
	}
	out := slices.Clone(pages)
	slices.SortStableFunc(out, func(a, b coderef.PageStatus) int {
		ra, ma := rank(a)
		rb, mb := rank(b)
		if ra != rb {
			return ra - rb
		}
		if ma != mb {
			return ma - mb
		}
		return strings.Compare(a.Slug, b.Slug)
	})
	return out
}

// codeSummary counts what a code status found: the pages tied to code, how
// many moved, are unchanged or couldn't be checked, those citing a path
// gone at HEAD, one their commit lacks or a commit off their clone's branch
// with nothing counted, those past their review day, the unreadable ones
// and those whose line on the map lacks their code.
func codeSummary(r coderef.Report, today string) string {
	tied, moved, unchanged, unchecked, gone, lacks, off, due := 0, 0, 0, 0, 0, 0, 0, 0
	for _, p := range r.Pages {
		if p.Due(today) {
			due++
		}
		if len(p.Repos) == 0 {
			continue
		}
		tied++
		m, ok := p.Movement()
		measured := !slices.ContainsFunc(p.Repos, func(r coderef.RepoStatus) bool { return !r.Checked() })
		switch {
		case ok && (m.Count > 0 || m.Capped):
			moved++
		case ok && measured:
			unchanged++
		}
		if !measured {
			unchecked++
		}
		if slices.ContainsFunc(p.Repos, func(r coderef.RepoStatus) bool { return len(r.Gone) > 0 && !r.Lacks() }) {
			gone++
		}
		if slices.ContainsFunc(p.Repos, coderef.RepoStatus.Lacks) {
			lacks++
		}
		if slices.ContainsFunc(p.Repos, func(r coderef.RepoStatus) bool { _, ok := r.Movement(); return r.State == coderef.NotOnBranch && !ok }) {
			off++
		}
	}
	s := fmt.Sprintf("%d of %d system %s tied to code: %d moved, %d unchanged, %d not checked", tied, len(r.Pages), plural(len(r.Pages), "page", "pages"), moved, unchanged, unchecked)
	if gone > 0 {
		s += fmt.Sprintf("; %d %s a path gone at HEAD", gone, plural(gone, "cites", "cite"))
	}
	if lacks > 0 {
		s += fmt.Sprintf("; %d %s a path its commit lacks", lacks, plural(lacks, "cites", "cite"))
	}
	if off > 0 {
		s += fmt.Sprintf("; %d %s a commit off its clone's branch", off, plural(off, "cites", "cite"))
	}
	if due > 0 {
		s += fmt.Sprintf("; %d past %s review date", due, plural(due, "its", "their"))
	}
	if n := len(r.Unreadable); n > 0 {
		s += fmt.Sprintf("; %d %s unreadable", n, plural(n, "page", "pages"))
	}
	if r.MapWithout > 0 {
		s += fmt.Sprintf("; the map lacks the code of %d %s, until a system write builds it again", r.MapWithout, plural(r.MapWithout, "page", "pages"))
	}
	return s
}
