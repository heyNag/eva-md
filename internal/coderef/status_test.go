package coderef

import (
	"context"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/heyNag/eva-md/internal/identity"
)

// testCache is the identity cache of the test's home.
func testCache(home string) identity.Cache {
	return identity.Cache{Dir: filepath.Join(home, "state", "cache", "identity")}
}

// resolve resolves dir through the cache, as a hook in it does, which leaves
// an entry for it.
func resolve(t *testing.T, c identity.Cache, dir string) identity.Project {
	t.Helper()
	p, err := c.Resolve(context.Background(), dir)
	if err != nil {
		t.Fatal(err)
	}
	return p
}

// age sets when the cache's entry for dir was written.
func age(t *testing.T, c identity.Cache, dir string, at time.Time) {
	t.Helper()
	files, err := os.ReadDir(c.Dir)
	if err != nil {
		t.Fatal(err)
	}
	for _, f := range files {
		data, err := os.ReadFile(filepath.Join(c.Dir, f.Name()))
		var e struct{ Path string }
		if err == nil && json.Unmarshal(data, &e) == nil && e.Path == dir {
			if err := os.Chtimes(filepath.Join(c.Dir, f.Name()), at, at); err != nil {
				t.Fatal(err)
			}
			return
		}
	}
	t.Fatalf("no cache entry for %s", dir)
}

func roots(clones []Clone) []string {
	var out []string
	for _, c := range clones {
		out = append(out, c.Root)
	}
	return out
}

const ordersKey = "github.com__acme__orders"

// The finder lists a repo's clones from the identity cache, the newest
// entry first, the working folder's project first of all and at most 8, and
// starts no git to list them or to confirm a worktree unchanged since a hook
// resolved it. A folder that is gone, or that now holds another repo, is
// never used.
func TestFinder(t *testing.T) {
	home := isolate(t)
	real, err := exec.LookPath("git")
	if err != nil {
		t.Fatal(err)
	}
	log := filepath.Join(home, "git.log")
	fakeGit(t, `echo "$*" >> '`+log+`'; exec '`+real+`' "$@"`)
	c := testCache(home)
	var clones []string
	for i := range 10 {
		dir := newRepo(t, filepath.Join(home, "clones", string(rune('a'+i))), "git@github.com:acme/orders.git")
		change(t, dir, "first", "a.go", "a")
		resolve(t, c, dir)
		age(t, c, dir, time.Now().Add(time.Duration(i-20)*time.Minute))
		clones = append(clones, dir)
	}
	billing := newRepo(t, filepath.Join(home, "billing"), "https://github.com/acme/billing.git")
	resolve(t, c, billing)
	outside := filepath.Join(home, "notes")
	if err := os.MkdirAll(outside, 0o700); err != nil {
		t.Fatal(err)
	}
	resolve(t, c, outside)
	if err := os.WriteFile(log, nil, 0o600); err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	f := NewFinder(c, []string{ordersKey}, identity.Project{})
	if got, want := roots(f.Clones(ctx, ordersKey, 3)), []string{clones[9], clones[8], clones[7]}; !reflect.DeepEqual(got, want) {
		t.Fatalf("newest first: %q, want %q", got, want)
	}
	if got := f.Clones(ctx, ordersKey, maxClones+5); len(got) != maxClones {
		t.Fatalf("%d clones listed", len(got))
	}
	if len(f.Clones(ctx, "github.com__acme__billing", 3)) != 0 {
		t.Fatal("a repo not asked for was listed")
	}
	if data, err := os.ReadFile(log); err != nil || len(data) != 0 {
		t.Fatalf("listing and confirming started git: %q %v", data, err)
	}
	prefer := resolve(t, c, clones[2])
	if got := roots(NewFinder(c, []string{ordersKey}, prefer).Clones(ctx, ordersKey, 2)); !reflect.DeepEqual(got, []string{clones[2], clones[9]}) {
		t.Fatalf("the working folder's clone first: %q", got)
	}
	if err := os.RemoveAll(clones[9]); err != nil {
		t.Fatal(err)
	}
	run(t, clones[8], "remote", "set-url", "origin", "git@github.com:acme/payments.git")
	if got := roots(NewFinder(c, []string{ordersKey}, identity.Project{}).Clones(ctx, ordersKey, 2)); !reflect.DeepEqual(got, []string{clones[7], clones[6]}) {
		t.Fatalf("a gone folder and one holding another repo: %q", got)
	}
}

// The identity cache holds an entry for a worktree under each git build and
// environment that resolved it, and the finder lists the worktree once, so
// one clone that lacks a commit can't take every try from another that has
// it.
func TestFinderListsAWorktreeOnce(t *testing.T) {
	for _, how := range []string{"environments", "git upgrades"} {
		t.Run(how, func(t *testing.T) {
			home := isolate(t)
			c := testCache(home)
			real, err := exec.LookPath("git")
			if err != nil {
				t.Fatal(err)
			}
			bin := filepath.Join(home, "bin")
			if err := os.MkdirAll(bin, 0o700); err != nil {
				t.Fatal(err)
			}
			// A build of git: a file of its own, of its own size.
			install := func(version string) {
				os.Remove(filepath.Join(bin, "git"))
				if err := os.WriteFile(filepath.Join(bin, "git"), []byte("#!/bin/sh\n# git "+version+"\nexec '"+real+"' \"$@\"\n"), 0o700); err != nil {
					t.Fatal(err)
				}
			}
			install("2.55")
			t.Setenv("PATH", bin+string(os.PathListSeparator)+os.Getenv("PATH"))
			// Clone b has the commit and was last chatted in an hour ago;
			// clone a, chatted in since, lacks it and is resolved three ways.
			b := newRepo(t, filepath.Join(home, "b"), "git@github.com:acme/orders.git")
			cited := change(t, b, "feature", "internal/orders/a.go", "b")
			change(t, b, "more", "internal/orders/a.go", "c")
			resolve(t, c, b)
			age(t, c, b, time.Now().Add(-time.Hour))
			a := newRepo(t, filepath.Join(home, "a"), "git@github.com:acme/orders.git")
			change(t, a, "unrelated", "README.md", "r")
			for i, pager := range []string{"cat", "less", ""} {
				if how == "environments" {
					t.Setenv("GIT_PAGER", pager)
				}
				resolve(t, c, a)
				if how == "git upgrades" && i < 2 {
					install([]string{"2.56.1", "2.57.10"}[i])
				}
			}
			n := 0
			for _, e := range c.Entries(maxListed) {
				if e.Path == a {
					n++
				}
			}
			if n != 3 {
				t.Fatalf("%d cache entries for clone a, want 3", n)
			}
			ctx := context.Background()
			if got := roots(NewFinder(c, []string{ordersKey}, identity.Project{}).Clones(ctx, ordersKey, maxTries)); !reflect.DeepEqual(got, []string{a, b}) {
				t.Fatalf("the clones tried: %q", got)
			}
			r := Status(ctx, Options{Cache: c}, []Page{{Slug: "orders", Refs: []Ref{{Key: ordersKey, Commit: cited[:7], Path: "internal/orders"}}}})
			if st := r.Pages[0].Repos[0]; st.State != Moved || st.Clone != b {
				t.Fatalf("measured %s in %s", st.State, st.Clone)
			}
		})
	}
}

// A path HEAD lacks that no commit since the cited one touched, as a path
// cited with a typo, was never at that commit: its entry is unchanged but
// gives no movement. A path removed since is gone at HEAD, and moved.
func TestAPathTheCommitLacks(t *testing.T) {
	home := isolate(t)
	c := testCache(home)
	orders := newRepo(t, filepath.Join(home, "orders"), "git@github.com:acme/orders.git")
	first := change(t, orders, "first", "internal/orders/a.go", "a", "api/orders.proto", "p")
	change(t, orders, "second", "internal/orders/a.go", "b")
	run(t, orders, "rm", "-q", "api/orders.proto")
	change(t, orders, "Drop the proto")
	resolve(t, c, orders)
	typo := []Ref{{Key: ordersKey, Commit: first[:7], Path: "internal/order"}}
	removed := []Ref{{Key: ordersKey, Commit: first[:7], Path: "api/orders.proto"}}
	r := Status(context.Background(), Options{Cache: c}, []Page{{Slug: "typo", AsOf: "2026-10-30", Refs: typo}, {Slug: "removed", AsOf: "2026-10-30", Refs: removed}})
	st := r.Pages[0].Repos[0]
	if st.State != Unchanged || !reflect.DeepEqual(st.Gone, []string{"internal/order"}) || !st.Lacks() || !st.Checked() {
		t.Fatalf("typo: %+v", st)
	}
	if m, ok := r.Pages[0].MovementSince("2026-10-30", typo); ok {
		t.Fatalf("a path the commit lacks reads as %q", Note("2026-10-30", m))
	}
	st = r.Pages[1].Repos[0]
	if m, ok := r.Pages[1].MovementSince("2026-10-30", removed); st.State != Moved || st.Lacks() || !ok || m.Count != 1 {
		t.Fatalf("removed: %+v, %+v %v", st, m, ok)
	}
}

// A pass measures each repo and commit a page cites in a clone that has the
// commit, and records what it found: unchanged, moved with its count and the
// paths gone at HEAD, a commit off HEAD's history, a commit no clone has, a
// repo with no clone, a clone git fails in, and the pairs past its bound and
// everything past its time as not run. The newest change is kept for
// printing and never saved.
func TestStatus(t *testing.T) {
	home := isolate(t)
	c := testCache(home)
	orders := newRepo(t, filepath.Join(home, "orders"), "git@github.com:acme/orders.git")
	first := change(t, orders, "first", "internal/orders/a.go", "a", "api/orders.proto", "p", "README.md", "r")
	change(t, orders, "orders", "internal/orders/a.go", "b")
	run(t, orders, "checkout", "--quiet", "-b", "side")
	side := change(t, orders, "side", "internal/orders/side.go", "s")
	run(t, orders, "checkout", "--quiet", "main")
	change(t, orders, "more orders", "internal/orders/a.go", "c")
	run(t, orders, "mv", "api/orders.proto", "api/v2.proto")
	head := change(t, orders, "Rename the proto")
	resolve(t, c, orders)
	empty := newRepo(t, filepath.Join(home, "empty"), "git@github.com:acme/empty.git")
	resolve(t, c, empty)
	ref := func(s string) Ref {
		r, ok := Parse(s)
		if !ok {
			t.Fatalf("%q is no ref", s)
		}
		return r
	}
	pages := []Page{
		{Slug: "orders", AsOf: "2026-10-30", Refs: []Ref{ref("github.com/acme/orders@" + first[:7] + ":internal/orders/"), ref("github.com/acme/orders@" + first[:7] + ":api/orders.proto")}},
		{Slug: "current", AsOf: "2026-10-31", Refs: []Ref{ref("github.com/acme/orders@" + head)}},
		{Slug: "side", Refs: []Ref{ref("github.com/acme/orders@" + side + ":internal/orders")}},
		{Slug: "ghost", Refs: []Ref{ref("github.com/acme/orders@0123456789ab")}},
		{Slug: "billing", Refs: []Ref{ref("github.com/acme/billing@abcdef1")}},
		{Slug: "unborn", Refs: []Ref{ref("github.com/acme/empty@abcdef1")}},
		{Slug: "docs", AsOf: "2026-09-01", ReviewBy: "2026-09-30"},
	}
	r := Status(context.Background(), Options{Cache: c, Last: true}, pages)
	if r.V != 1 || r.At == "" || len(r.Pages) != len(pages) {
		t.Fatalf("report %+v", r)
	}
	got := map[string]RepoStatus{}
	for _, p := range r.Pages {
		if len(p.Repos) > 0 {
			got[p.Slug] = p.Repos[0]
		}
	}
	o := got["orders"]
	if o.State != Moved || o.Moved != 3 || o.Commit != first[:7] || !reflect.DeepEqual(o.Paths, []string{"internal/orders", "api/orders.proto"}) ||
		!reflect.DeepEqual(o.Gone, []string{"api/orders.proto"}) || o.Clone != orders || o.Branch != "main" || o.Head != head || !strings.HasSuffix(o.Last, ": Rename the proto") {
		t.Errorf("orders: %+v", o)
	}
	if g := got["current"]; g.State != Unchanged || g.Moved != 0 || g.Paths != nil || g.Last != "" {
		t.Errorf("current: %+v", g)
	}
	if g := got["side"]; g.State != NotOnBranch || g.Moved != 1 {
		t.Errorf("side: %+v", g)
	}
	if g := got["ghost"]; g.State != UnknownCommit || g.Clone != orders {
		t.Errorf("ghost: %+v", g)
	}
	if g := got["billing"]; g.State != NoClone || g.Clone != "" {
		t.Errorf("billing: %+v", g)
	}
	if g := got["unborn"]; g.State != GitFailed || g.Clone != empty {
		t.Errorf("unborn: %+v", g)
	}
	if docs := r.Pages[6]; docs.Slug != "docs" || docs.Repos != nil || !docs.Due("2026-10-31") || docs.Due("2026-09-29") {
		t.Errorf("docs: %+v", docs)
	}
	if data, _ := json.Marshal(r); strings.Contains(string(data), "Rename the proto") {
		t.Fatal("a commit subject would be saved")
	}
	// A page's movement and whether a map line's code matches its entry.
	if m, ok := r.Pages[0].Movement(); !ok || m.Count != 3 {
		t.Errorf("orders movement %+v %v", m, ok)
	}
	if m, ok := r.Pages[1].Movement(); !ok || m.Count != 0 {
		t.Errorf("current movement %+v %v", m, ok)
	}
	if _, ok := r.Pages[3].Movement(); ok {
		t.Error("an unknown commit has a movement")
	}
	if !r.Pages[0].Matches(pages[0].Refs) || r.Pages[0].Matches(pages[1].Refs) || r.Pages[6].Matches(nil) {
		t.Error("Matches")
	}
	// Past the bound of pairs, and past the pass's time, nothing runs.
	bounded := Status(context.Background(), Options{Cache: c, Pairs: 2}, pages)
	if s := bounded.Pages[2].Repos[0].State; bounded.Pages[0].Repos[0].State != Moved || s != NotRun {
		t.Errorf("past the pairs: %+v", bounded.Pages[:3])
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	for _, p := range Status(ctx, Options{Cache: c}, pages).Pages {
		for _, rs := range p.Repos {
			if rs.State != NotRun {
				t.Errorf("past the pass's time: %s %+v", p.Slug, rs)
			}
		}
	}
}

// The status file is saved private and read back as saved; one missing
// reads as none, and one damaged, of another format, over its bound or
// behind a link as an error. A pass over some pages updates their entries
// and keeps the rest.
func TestSaveLoadMerge(t *testing.T) {
	home := isolate(t)
	state := filepath.Join(home, "state")
	if _, ok, err := Load(state, Limit); ok || err != nil {
		t.Fatalf("no file: %v %v", ok, err)
	}
	r := Report{V: 1, At: "2026-10-31T03:10:00+01:00", Pages: []PageStatus{
		{Slug: "billing", Repos: []RepoStatus{{Key: "github.com__acme__billing", Commit: "abcdef1", State: NoClone}}},
		{Slug: "orders", AsOf: "2026-10-30", Repos: []RepoStatus{{Key: ordersKey, Commit: "4e2bf4c", Paths: []string{"internal/orders"}, State: Moved, Moved: 14, Last: "2026-10-29: Split pricing"}}},
	}, MapWithout: 1, Unreadable: []string{"ledger"}}
	if err := Save(state, r); err != nil {
		t.Fatal(err)
	}
	file := filepath.Join(state, File)
	if info, err := os.Stat(file); err != nil || info.Mode().Perm() != 0o600 {
		t.Fatalf("mode %v %v", info.Mode(), err)
	}
	got, ok, err := Load(state, Limit)
	r.Pages[1].Repos[0].Last = ""
	if err != nil || !ok || !reflect.DeepEqual(got, r) {
		t.Fatalf("read back %+v %v %v", got, ok, err)
	}
	if _, _, err := Load(state, 100); err == nil {
		t.Fatal("a file over its bound was read")
	}
	partial := Report{V: 1, At: "2026-10-31T09:00:00+01:00", Pages: []PageStatus{
		{Slug: "orders", AsOf: "2026-10-31", Repos: []RepoStatus{{Key: ordersKey, Commit: "9c1d2e3", State: Unchanged}}},
		{Slug: "ledger", Repos: []RepoStatus{{Key: "github.com__acme__ledger", Commit: "1234567", State: NoClone}}},
	}}
	merged := Merge(got, partial)
	if merged.At != r.At || merged.MapWithout != 1 || len(merged.Unreadable) != 0 || len(merged.Pages) != 3 ||
		merged.Pages[0].Slug != "billing" || merged.Pages[1].Slug != "ledger" || merged.Pages[2].AsOf != "2026-10-31" {
		t.Fatalf("merged %+v", merged)
	}
	for _, bad := range []string{"{not json", `{"v":2,"at":"2026-10-31T03:10:00Z","pages":[]}`, `{"v":1,"pages":[]}`} {
		if err := os.WriteFile(file, []byte(bad), 0o600); err != nil {
			t.Fatal(err)
		}
		if _, ok, err := Load(state, Limit); ok || err == nil {
			t.Fatalf("%q read: %v %v", bad, ok, err)
		}
	}
	other := filepath.Join(home, "other.json")
	data, _ := json.Marshal(r)
	if err := os.WriteFile(other, data, 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(file); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(other, file); err != nil {
		t.Fatal(err)
	}
	if _, ok, err := Load(state, Limit); ok || err == nil {
		t.Fatalf("read through a link: %v %v", ok, err)
	}
}
