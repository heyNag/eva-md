package coderef

import (
	"context"
	"os"
	"slices"
	"time"

	"github.com/heyNag/eva-md/internal/identity"
)

// Bounds of finding clones.
const (
	// maxListed bounds the identity cache entries listed.
	maxListed = 4096
	// maxClones is how many clones of one repo are kept, and maxTries how
	// many of them one measurement tries.
	maxClones = 8
	maxTries  = 3
)

// Clone is a local worktree of a repo: its top level, and when a chat last
// resolved it with a change since, which orders the clones newest first.
type Clone struct {
	Root string
	Seen time.Time
}

// Finder finds the local clones of repos through the identity cache, which
// holds an entry for each worktree a chat ran in, and confirms each one
// before it is used. A repo never chatted in on this Mac has none. One finder
// serves one run: a pass over the pages, a show or a write. Hooks never use
// one.
type Finder struct {
	cache  identity.Cache
	listed map[string][]Clone // by key: the preferred clone, then the newest first
	held   map[string]bool    // by key and root, once resolved: whether the folder still holds the repo
}

// NewFinder lists the clones of the repos keys name, at most 8 a repo:
// prefer's worktree first when it is one of them, then the newest entries of
// the cache whose folder still exists, each worktree once, though the cache
// holds an entry for it under each git build and environment that resolved
// it.
func NewFinder(cache identity.Cache, keys []string, prefer identity.Project) *Finder {
	f := &Finder{cache: cache, listed: map[string][]Clone{}, held: map[string]bool{}}
	want := map[string]bool{}
	for _, k := range keys {
		want[k] = true
	}
	if want[prefer.Key] && prefer.Root != "" {
		f.listed[prefer.Key] = []Clone{{Root: prefer.Root}}
		f.held[prefer.Key+"\x00"+prefer.Root] = true
	}
	for _, e := range cache.Entries(maxListed) {
		key := e.Project.Key
		if !want[key] || len(f.listed[key]) >= maxClones || slices.ContainsFunc(f.listed[key], func(c Clone) bool { return c.Root == e.Path }) {
			continue
		}
		if info, err := os.Stat(e.Path); err != nil || !info.IsDir() {
			continue
		}
		f.listed[key] = append(f.listed[key], Clone{Root: e.Path, Seen: e.Seen})
	}
	return f
}

// Clones returns key's clones that still hold its repo, at most n of them,
// in order. Each folder is resolved again, through the cache, which starts
// no git for a worktree unchanged since a hook resolved it, so one that is
// gone or now holds another repo is never measured.
func (f *Finder) Clones(ctx context.Context, key string, n int) []Clone {
	var out []Clone
	for _, c := range f.listed[key] {
		if len(out) == n || ctx.Err() != nil {
			break
		}
		id := key + "\x00" + c.Root
		held, seen := f.held[id]
		if !seen {
			p, err := f.cache.Resolve(ctx, c.Root)
			held = err == nil && p.Key == key && p.Root == c.Root
			f.held[id] = held
		}
		if held {
			out = append(out, c)
		}
	}
	return out
}
