package coderef

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"syscall"
	"time"

	"github.com/heyNag/eva-md/internal/identity"
	"github.com/heyNag/eva-md/internal/paths"
)

// File is the status file's name in eva-md's state folder. It is derived,
// local to this Mac and never committed: a pass over the pages writes it
// again whole.
const File = "code-status.json"

// Bounds of a read of the status file, in bytes: HookLimit for the prompt
// hook, about 1,000 entries, and Limit for everything else.
const (
	HookLimit = 256 << 10
	Limit     = 1 << 20
)

// statusVersion is the status file's format.
const statusVersion = 1

// What measuring a page's code in one repo found.
const (
	Unchanged     = "unchanged"      // no commit since the cited one touched its paths
	Moved         = "moved"          // commits since touched them
	NotOnBranch   = "not_on_branch"  // the cited commit is not on HEAD's history: counted since the two have in common
	UnknownCommit = "unknown_commit" // the clones tried lack the commit
	NoClone       = "no_clone"       // no clone of the repo is known on this Mac
	TimedOut      = "timeout"        // git ran out of time
	GitFailed     = "git"            // git failed
	NotRun        = "not_run"        // past the pass's bounds
)

// Report is what a pass over the system pages measured, saved as
// <state>/code-status.json, mode 0600: for each page its as_of and review
// day and, for each repo and commit it cites code of, where that was
// measured and how far it has moved. It holds counts, keys, paths, local
// folders and days, never a commit's subject.
type Report struct {
	V          int          `json:"v"`
	At         string       `json:"at"`
	DurationMS int64        `json:"duration_ms"`
	Pages      []PageStatus `json:"pages"`
	// Unreadable lists the system pages the pass couldn't read, and
	// MapWithout counts the pages that cite code their line on the system
	// map lacks, as a page written before the map carried code does.
	Unreadable []string `json:"unreadable,omitempty"`
	MapWithout int      `json:"map_without_code,omitempty"`
}

// PageStatus is one system page's code: an entry for each repo and commit it
// cites, none for a page that cites no code.
type PageStatus struct {
	Slug     string       `json:"slug"`
	AsOf     string       `json:"as_of,omitempty"`
	ReviewBy string       `json:"review_by,omitempty"`
	Repos    []RepoStatus `json:"repos,omitempty"`
}

// RepoStatus is a page's code in one repo at one commit: the paths it cites
// there, none for the whole repo, and what measuring them found, with the
// clone it was measured in, that clone's branch and HEAD, the commits since
// and the paths HEAD's tree lacks. Last, the day and subject of the newest
// of those commits, is printed and never saved.
type RepoStatus struct {
	Key    string   `json:"key"`
	Commit string   `json:"commit"`
	Paths  []string `json:"paths,omitempty"`
	State  string   `json:"state"`
	Moved  int      `json:"moved,omitempty"`
	Capped bool     `json:"capped,omitempty"`
	Gone   []string `json:"gone,omitempty"`
	Clone  string   `json:"clone,omitempty"`
	Branch string   `json:"branch,omitempty"`
	Head   string   `json:"head,omitempty"`
	Last   string   `json:"-"`
}

// Movement is the entry's movement, and whether it was measured: moved or
// unchanged, but not unchanged citing a path its commit lacks (see Lacks),
// or not on HEAD's history with commits since the commit the two have in
// common.
func (r RepoStatus) Movement() (Movement, bool) {
	m := Movement{Count: r.Moved, Capped: r.Capped}
	switch r.State {
	case Unchanged:
		return m, !r.Lacks()
	case Moved:
		return m, true
	case NotOnBranch:
		return m, r.Moved > 0 || r.Capped
	}
	return Movement{}, false
}

// Lacks reports an entry measured unchanged whose paths HEAD's tree lacks
// some of, in Gone: as its commit is on HEAD's history and a removal or a
// rename would have been counted, the commit lacks them too, as a path
// cited with a typo does.
func (r RepoStatus) Lacks() bool { return r.State == Unchanged && len(r.Gone) > 0 }

// Checked reports whether the entry was measured in a clone, whatever it
// found.
func (r RepoStatus) Checked() bool {
	return r.State == Unchanged || r.State == Moved || r.State == NotOnBranch
}

// Movement is what the page's pointer says of its code: its entries
// combined as Tally combines them.
func (p PageStatus) Movement() (Movement, bool) {
	var t Tally
	for _, r := range p.Repos {
		t.Add(r.Movement())
	}
	return t.Result()
}

// Due reports whether the page's review day has come by today, a
// YYYY-MM-DD day.
func (p PageStatus) Due(today string) bool { return p.ReviewBy != "" && p.ReviewBy <= today }

// Matches reports whether the entry was measured at the repos and commits
// refs cite, as the system map lists them, so a page written again since the
// pass gets no note from it.
func (p PageStatus) Matches(refs []Ref) bool {
	var want, have []string
	for _, g := range groups(refs) {
		want = append(want, g.key+"@"+g.commit)
	}
	for _, r := range p.Repos {
		have = append(have, r.Key+"@"+r.Commit)
	}
	slices.Sort(want)
	slices.Sort(have)
	return len(want) > 0 && slices.Equal(slices.Compact(want), slices.Compact(have))
}

// MovementSince is what the pointers, search hits and eva_status say of a
// page's code, by the page's line on the system map, its as_of and the refs
// it cites: the page's movement, and false for a page that cites no code,
// one that couldn't be measured, one whose as_of is not a day (see Note) and
// one written again since the pass, at other commits.
func (p PageStatus) MovementSince(asOf string, refs []Ref) (Movement, bool) {
	m, ok := p.Movement()
	return m, ok && Note(asOf, m) != "" && p.Matches(refs)
}

// Page is what a pass measures of one system page: its slug, as_of and
// review day, and its code sources.
type Page struct {
	Slug, AsOf, ReviewBy string
	Refs                 []Ref
}

// CallTimeout bounds each git call of a pass whose options set no PerCall.
// Tests raise it before any test runs, so a slow machine can't fail a test
// of what a pass measures.
var CallTimeout = 10 * time.Second

// Options are what a pass needs.
type Options struct {
	Cache   identity.Cache   // the identity cache, which knows the clones
	Prefer  identity.Project // the project of the folder the pass runs in, whose worktree is tried first
	PerCall time.Duration    // bounds each git call; CallTimeout, 10 s, when 0
	Pairs   int              // bounds the repo and commit pairs measured; 64 when 0
	Last    bool             // also find the newest change of moved code, which is printed, never saved
	Now     func() time.Time // the clock; time.Now when nil
}

// Status measures the code the pages cite. For each repo and commit a page
// cites, it tries at most 3 clones the identity cache knows, the folder's
// own project first, and in the first that has the commit counts the
// commits since that touch the page's paths there and lists the paths
// HEAD's tree lacks: about 4 git calls for a commit and 1 for a clone. Past
// ctx or past the bound of pairs, the rest is recorded as not run. Apart
// from the identity cache's entry of a clone it resolves again, which a hook
// in that clone would refresh too, it writes nothing; Save writes the status.
func Status(ctx context.Context, o Options, pages []Page) Report {
	o = o.defaults()
	start := time.Now()
	r := Report{V: statusVersion, At: o.Now().Format(time.RFC3339), Pages: []PageStatus{}}
	m := newMeasurer(o, pages)
	for _, p := range pages {
		r.Pages = append(r.Pages, m.page(ctx, p))
	}
	r.DurationMS = time.Since(start).Milliseconds()
	return r
}

// Check measures one page's code as Status does.
func Check(ctx context.Context, o Options, p Page) PageStatus {
	return newMeasurer(o.defaults(), []Page{p}).page(ctx, p)
}

func (o Options) defaults() Options {
	if o.PerCall <= 0 {
		o.PerCall = CallTimeout
	}
	if o.Pairs <= 0 {
		o.Pairs = 64
	}
	if o.Now == nil {
		o.Now = time.Now
	}
	return o
}

// group is a page's refs to one commit of one repo: their paths, none for
// the whole repo.
type group struct {
	key, commit string
	paths       []string
}

// groups gathers refs by repo and commit, in the order they first come, each
// path once; a ref to the whole repo makes its group the whole repo.
func groups(refs []Ref) []group {
	var out []group
	whole := map[int]bool{}
	for _, r := range refs {
		i := slices.IndexFunc(out, func(g group) bool { return g.key == r.Key && g.commit == r.Commit })
		if i < 0 {
			i = len(out)
			out = append(out, group{key: r.Key, commit: r.Commit})
		}
		switch {
		case r.Path == "":
			whole[i] = true
		case !slices.Contains(out[i].paths, r.Path):
			out[i].paths = append(out[i].paths, r.Path)
		}
	}
	for i := range whole {
		out[i].paths = nil
	}
	return out
}

// Groups returns refs gathered as a pass measures them: by repo and commit,
// with the paths cited there, none for the whole repo.
func Groups(refs []Ref) [][]Ref {
	var out [][]Ref
	for _, g := range groups(refs) {
		var refs []Ref
		for _, p := range g.paths {
			refs = append(refs, Ref{Key: g.key, Commit: g.commit, Path: p})
		}
		if len(refs) == 0 {
			refs = []Ref{{Key: g.key, Commit: g.commit}}
		}
		out = append(out, refs)
	}
	return out
}

// measurer measures the groups of one run, finding each repo's clones once
// and each clone's HEAD once.
type measurer struct {
	o      Options
	finder *Finder
	heads  map[string]head       // by clone
	pairs  map[string]bool       // the repo and commit pairs measured
	memo   map[string]RepoStatus // by repo, commit and paths
}

type head struct {
	commit, branch string
	err            error
}

func newMeasurer(o Options, pages []Page) *measurer {
	var keys []string
	for _, p := range pages {
		for _, r := range p.Refs {
			if !slices.Contains(keys, r.Key) {
				keys = append(keys, r.Key)
			}
		}
	}
	return &measurer{o: o, finder: NewFinder(o.Cache, keys, o.Prefer), heads: map[string]head{}, pairs: map[string]bool{}, memo: map[string]RepoStatus{}}
}

func (m *measurer) page(ctx context.Context, p Page) PageStatus {
	ps := PageStatus{Slug: p.Slug, AsOf: p.AsOf, ReviewBy: p.ReviewBy}
	for _, g := range groups(p.Refs) {
		ps.Repos = append(ps.Repos, m.measure(ctx, g))
	}
	return ps
}

// measure measures a group in the first of its repo's clones that has its
// commit; a clone that lacks it passes to the next.
func (m *measurer) measure(ctx context.Context, g group) RepoStatus {
	pair := g.key + "@" + g.commit
	id := pair + ":" + strings.Join(g.paths, "\x00")
	if r, ok := m.memo[id]; ok {
		return r
	}
	r := RepoStatus{Key: g.key, Commit: g.commit, Paths: g.paths, State: NotRun}
	if !m.pairs[pair] && len(m.pairs) >= m.o.Pairs || ctx.Err() != nil {
		return r
	}
	m.pairs[pair] = true
	clones := m.finder.Clones(ctx, g.key, maxTries)
	if len(clones) == 0 {
		r.State = NoClone
		if ctx.Err() != nil {
			r.State = NotRun
		}
	}
	for _, c := range clones {
		if r = m.inClone(ctx, c, g); r.State != UnknownCommit {
			break
		}
	}
	m.memo[id] = r
	return r
}

// inClone measures a group in one clone.
func (m *measurer) inClone(ctx context.Context, c Clone, g group) RepoStatus {
	r := RepoStatus{Key: g.key, Commit: g.commit, Paths: g.paths, Clone: c.Root}
	failed := func(err error) RepoStatus {
		r.State = GitFailed
		if errors.Is(err, ErrTimeout) {
			r.State = TimedOut
		}
		return r
	}
	h, ok := m.heads[c.Root]
	if !ok {
		call, cancel := context.WithTimeout(ctx, m.o.PerCall)
		h.commit, h.branch, h.err = Head(call, c.Root)
		cancel()
		m.heads[c.Root] = h
	}
	if h.err != nil {
		return failed(h.err)
	}
	r.Head, r.Branch = h.commit, h.branch
	call, cancel := context.WithTimeout(ctx, m.o.PerCall)
	has, err := HasCommit(call, c.Root, g.commit)
	cancel()
	if err != nil {
		return failed(err)
	}
	if !has {
		r.State = UnknownCommit
		return r
	}
	call, cancel = context.WithTimeout(ctx, m.o.PerCall)
	onBranch, err := Ancestor(call, c.Root, g.commit)
	cancel()
	if err != nil {
		return failed(err)
	}
	call, cancel = context.WithTimeout(ctx, m.o.PerCall)
	mv, err := Commits(call, c.Root, g.commit, g.paths)
	cancel()
	if err != nil {
		return failed(err)
	}
	r.Moved, r.Capped = mv.Count, mv.Capped
	switch {
	case !onBranch:
		r.State = NotOnBranch
	case mv.Count > 0:
		r.State = Moved
	default:
		r.State = Unchanged
	}
	call, cancel = context.WithTimeout(ctx, m.o.PerCall)
	r.Gone, err = Missing(call, c.Root, "HEAD", g.paths)
	cancel()
	if err != nil {
		return failed(err)
	}
	if m.o.Last && mv.Count > 0 {
		call, cancel = context.WithTimeout(ctx, m.o.PerCall)
		day, subject, err := Last(call, c.Root, g.commit, g.paths)
		cancel()
		if err == nil && day != "" {
			r.Last = day + ": " + subject
		}
	}
	return r
}

// Save writes r as <state>/code-status.json, mode 0600, replacing the file
// whole.
func Save(state string, r Report) error {
	data, err := json.Marshal(r)
	if err != nil {
		return err
	}
	if err := paths.EnsureDir(state); err != nil {
		return err
	}
	return paths.WriteFileAtomic(filepath.Join(state, File), append(data, '\n'), 0o600)
}

// Load reads <state>/code-status.json: a regular file, never through a link,
// of at most limit bytes, in this format. It reports false, with no error,
// when there is none.
func Load(state string, limit int64) (Report, bool, error) {
	root, err := paths.OpenRoot(state, false)
	if errors.Is(err, os.ErrNotExist) {
		return Report{}, false, nil
	}
	if err != nil {
		return Report{}, false, err
	}
	defer root.Close()
	f, err := root.OpenFile(File, os.O_RDONLY|syscall.O_NOFOLLOW|syscall.O_NONBLOCK, 0)
	if errors.Is(err, os.ErrNotExist) {
		return Report{}, false, nil
	}
	if err != nil {
		return Report{}, false, err
	}
	defer f.Close()
	if info, err := f.Stat(); err != nil || !info.Mode().IsRegular() || info.Size() > limit {
		return Report{}, false, errors.New("coderef: the status file is not a regular file within its bound")
	}
	data, err := io.ReadAll(io.LimitReader(f, limit+1))
	if err != nil || int64(len(data)) > limit {
		return Report{}, false, errors.New("coderef: the status file can't be read within its bound")
	}
	var r Report
	if err := json.Unmarshal(data, &r); err != nil || r.V != statusVersion || r.At == "" {
		return Report{}, false, fmt.Errorf("coderef: the status file is not of format %d", statusVersion)
	}
	return r, true, nil
}

// Merge returns old with r's pages in place of its own, as a pass over some
// of the pages leaves the file: old's time, its other pages and its counts
// of the whole wiki stay. A page r couldn't read loses its old entry and is
// listed unreadable.
func Merge(old, r Report) Report {
	passed := func(slug string) bool {
		return slices.Contains(r.Unreadable, slug) || slices.ContainsFunc(r.Pages, func(q PageStatus) bool { return q.Slug == slug })
	}
	out := old
	out.Pages = nil
	for _, p := range old.Pages {
		if !passed(p.Slug) {
			out.Pages = append(out.Pages, p)
		}
	}
	out.Pages = append(out.Pages, r.Pages...)
	slices.SortFunc(out.Pages, func(a, b PageStatus) int { return strings.Compare(a.Slug, b.Slug) })
	out.Unreadable = append(slices.DeleteFunc(slices.Clone(old.Unreadable), passed), r.Unreadable...)
	slices.Sort(out.Unreadable)
	out.Unreadable = slices.Compact(out.Unreadable)
	return out
}
