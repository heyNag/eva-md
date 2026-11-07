package systempage

import (
	"cmp"
	"context"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"reflect"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/heyNag/eva-md/internal/wikigit"
)

// payments is a system page that lists two repos and cites code in a third.
const payments = `---
type: system
title: Payments
description: Charges cards and pays out
status: stable
generated: { by: claude-code/claude-opus-5-5 }
sources:
  - resource: "https://docs.example.com/eng/payments/overview"
  - resource: "git@github.com:acme/ledger.git@4e2bf4c:internal/ledger/post.go"
repos: [git@github.com:acme/payments.git, "github.com/acme/shop", github.com/acme/payments]
as_of: 2026-10-29
state: active
---

## Summary
Payments charges cards and pays out.
`

// Every write lists in the repos index each system page that lists two or
// more repos, its own and those of the code it cites, by its id with their
// project keys, each once; a page with one repo has no line, and with no
// such page there is no file. The daily commit stages the index.
func TestReposIndex(t *testing.T) {
	o := fixture(t)
	index := filepath.Join(o.Config.WikiPath, filepath.FromSlash(ReposFile))
	mustWrite(t, o, "orders", valid, false)
	if _, err := os.Lstat(index); !errors.Is(err, fs.ErrNotExist) {
		t.Fatalf("a page with one repo made the index: %v", err)
	}
	mustWrite(t, o, "payments", payments, false)
	want := "# eva-md repos index 1\nsystems/payments\tgithub.com__acme__payments\tgithub.com__acme__shop\tgithub.com__acme__ledger\n"
	if got := file(t, o, ReposFile); got != want {
		t.Fatalf("repos index\n%s", got)
	}
	// A page written again with a second repo joins the index, whose lines
	// are sorted by id.
	mustWrite(t, o, "orders", with(t, "repos: [git@github.com:acme/orders.git]", "repos: [git@github.com:acme/orders.git, github.com/acme/shop]"), true)
	want = "# eva-md repos index 1\nsystems/orders\tgithub.com__acme__orders\tgithub.com__acme__shop\nsystems/payments\tgithub.com__acme__payments\tgithub.com__acme__shop\tgithub.com__acme__ledger\n"
	if got := file(t, o, ReposFile); got != want {
		t.Fatalf("repos index after a second page\n%s", got)
	}
	pages := ParseRepos([]byte(want))
	if !reflect.DeepEqual(pages, [][]string{{"github.com__acme__orders", "github.com__acme__shop"}, {"github.com__acme__payments", "github.com__acme__shop", "github.com__acme__ledger"}}) {
		t.Fatalf("parsed %q", pages)
	}
	if got := Siblings(pages, "github.com__acme__shop"); !reflect.DeepEqual(got, map[string]bool{"github.com__acme__orders": true, "github.com__acme__payments": true, "github.com__acme__ledger": true}) {
		t.Fatalf("siblings of the shop %v", got)
	}
	if got := Siblings(pages, "github.com__acme__billing"); len(got) != 0 {
		t.Fatalf("siblings of a project no page lists %v", got)
	}
	c, err := wikigit.Commit(context.Background(), wikigit.Options{Config: o.Config, Dirs: o.Dirs, Now: func() time.Time { return day }})
	if err != nil || c.Outcome != wikigit.Committed || len(c.Unstaged) != 0 {
		t.Fatalf("commit %+v %v", c, err)
	}
	if staged := git(t, o.Config.WikiPath, "ls-files", ReposFile); strings.TrimSpace(staged) != ReposFile {
		t.Fatalf("the commit left out the repos index: %q", staged)
	}
}

// eva-md doctor writes the repos index again, with the names index, when it
// is missing, damaged or out of date, as after a hand edit of a page's
// repos; eva-md status reports it as stale and leaves it as it is.
func TestRefreshNamesWritesTheReposIndex(t *testing.T) {
	o := fixture(t)
	mustWrite(t, o, "payments", payments, false)
	written := file(t, o, ReposFile)
	index := filepath.Join(o.Config.WikiPath, filepath.FromSlash(ReposFile))
	page := filepath.Join(o.Config.WikiPath, "systems", "payments.md")
	for _, tc := range []struct {
		name   string
		damage func() error
		want   string
	}{
		{"missing", func() error { return os.Remove(index) }, written},
		{"damaged", func() error { return os.WriteFile(index, []byte("systems/payments\tonly one\n"), 0o600) }, written},
		{"out of date", func() error {
			text := file(t, o, "systems/payments.md")
			if !strings.Contains(text, `,"github.com__acme__shop"]`) {
				t.Fatalf("saved page\n%s", text)
			}
			return os.WriteFile(page, []byte(strings.Replace(text, `,"github.com__acme__shop"]`, "]", 1)), 0o644)
		}, strings.Replace(written, "\tgithub.com__acme__shop", "", 1)},
	} {
		if err := tc.damage(); err != nil {
			t.Fatal(err)
		}
		before, _ := os.ReadFile(index)
		r, err := RefreshNames(context.Background(), o, false)
		if after, _ := os.ReadFile(index); err != nil || !r.ReposStale || r.NamesStale || r.ReposGone || r.ReposRebuilt || r.NamesRebuilt || string(after) != string(before) {
			t.Fatalf("%s: status %+v %v, index\n%s", tc.name, r, err, after)
		}
		if r, err := RefreshNames(context.Background(), o, true); err != nil || !r.ReposRebuilt || r.NamesRebuilt || r.ReposStale || r.NamesStale {
			t.Fatalf("%s: %+v %v", tc.name, r, err)
		}
		if got := file(t, o, ReposFile); got != tc.want {
			t.Fatalf("%s: index\n%s", tc.name, got)
		}
	}
	if r, err := RefreshNames(context.Background(), o, true); err != nil || r.ReposRebuilt || r.NamesRebuilt || r.ReposStale || r.NamesStale {
		t.Fatalf("a current index was written again: %+v %v", r, err)
	}
}

// When no page lists two repos any more, the write removes the repos index,
// leaving no empty file, and the daily commit records the removal; the
// doctor removes an empty one an earlier write left, which status reports.
func TestReposIndexGoesWithTheLastSharedPage(t *testing.T) {
	o := fixture(t)
	index := filepath.Join(o.Config.WikiPath, filepath.FromSlash(ReposFile))
	commit := func() {
		t.Helper()
		if c, err := wikigit.Commit(context.Background(), wikigit.Options{Config: o.Config, Dirs: o.Dirs, Now: func() time.Time { return day }}); err != nil || c.Outcome != wikigit.Committed || len(c.Unstaged) != 0 {
			t.Fatalf("commit %+v %v", c, err)
		}
	}
	mustWrite(t, o, "payments", payments, false)
	commit()
	one := strings.Replace(payments, `repos: [git@github.com:acme/payments.git, "github.com/acme/shop", github.com/acme/payments]`, `repos: [git@github.com:acme/payments.git]`, 1)
	one = strings.Replace(one, "  - resource: \"git@github.com:acme/ledger.git@4e2bf4c:internal/ledger/post.go\"\n", "", 1)
	if one == payments {
		t.Fatal("the page still lists its repos")
	}
	mustWrite(t, o, "payments", one, true)
	if _, err := os.Lstat(index); !errors.Is(err, fs.ErrNotExist) {
		t.Fatalf("the index is still there: %v", err)
	}
	if r, err := RefreshNames(context.Background(), o, false); err != nil || r.ReposStale || r.NamesStale {
		t.Fatalf("no index where no page shares a repo: %+v %v", r, err)
	}
	commit()
	if staged := git(t, o.Config.WikiPath, "ls-files", ReposFile); staged != "" {
		t.Fatalf("the commit kept the index: %q", staged)
	}
	if err := os.WriteFile(index, nil, 0o600); err != nil {
		t.Fatal(err)
	}
	if r, err := RefreshNames(context.Background(), o, false); err != nil || !r.ReposStale || !r.ReposGone || r.NamesStale || r.NamesGone {
		t.Fatalf("status of an empty index: %+v %v", r, err)
	}
	if r, err := RefreshNames(context.Background(), o, true); err != nil || !r.ReposRemoved || r.ReposRebuilt || r.NamesRebuilt || r.NamesRemoved || r.ReposStale {
		t.Fatalf("doctor of an empty index: %+v %v", r, err)
	}
	if _, err := os.Lstat(index); !errors.Is(err, fs.ErrNotExist) {
		t.Fatalf("the doctor left the empty index: %v", err)
	}
}

// Past 64 KB, the pages with the oldest as_of are left out of the repos
// index, a page without one first.
func TestReposBounds(t *testing.T) {
	var entries []system
	for i := range 400 {
		e := system{MapEntry: MapEntry{Slug: fmt.Sprintf("system-%04d", i), AsOf: fmt.Sprintf("2026-%02d-01", 1+i%12)}}
		if i == 7 {
			e.AsOf = ""
		}
		for r := range 4 {
			e.Repos = append(e.Repos, fmt.Sprintf("github.com__acme__repo-%04d-%d-%s", i, r, strings.Repeat("x", 20)))
		}
		entries = append(entries, e)
	}
	text, listed := reposIndex(entries)
	if len(text) > MaxReposSize || listed == 0 || listed == len(entries) || strings.Count(text, "\n") != listed+1 {
		t.Fatalf("%d bytes, %d pages listed", len(text), listed)
	}
	oldest := slices.Clone(entries)
	slices.SortFunc(oldest, func(a, b system) int { return cmp.Or(strings.Compare(a.AsOf, b.AsOf), strings.Compare(a.Slug, b.Slug)) })
	for i, e := range oldest {
		if in := strings.Contains(text, "\nsystems/"+e.Slug+"\t"); in != (i >= len(entries)-listed) {
			t.Fatalf("page %d of the oldest first, %s (as of %q), listed %v, with %d listed", i, e.Slug, e.AsOf, in, listed)
		}
	}
	if text, listed := reposIndex(entries[:3]); listed != 3 || !strings.HasPrefix(text, reposHeader+"\nsystems/system-0000\t") {
		t.Fatalf("%d pages:\n%s", listed, text)
	}
}

// ParseRepos reads nothing without the header, and skips any line that is
// not one of the index's: a page's id and two or more keys.
func TestParseRepos(t *testing.T) {
	data := reposHeader + "\n" +
		"systems/orders\tgithub.com__acme__orders\tgithub.com__acme__shop\n" +
		"systems/orders\tonly-one\n" +
		"projects/orders\ta\tb\n" +
		"systems/map\ta\tb\n" +
		"systems/payments\ta\t\tb\n" +
		"no tabs at all\n" +
		"systems/payments\tgithub.com__acme__payments\tlocal-notes-1a2b3c\trepo-0123456789ab\n"
	want := [][]string{{"github.com__acme__orders", "github.com__acme__shop"}, {"github.com__acme__payments", "local-notes-1a2b3c", "repo-0123456789ab"}}
	if got := ParseRepos([]byte(data)); !reflect.DeepEqual(got, want) {
		t.Fatalf("parsed %q", got)
	}
	if got := ParseRepos([]byte(strings.TrimPrefix(data, reposHeader+"\n"))); got != nil {
		t.Fatalf("no header: %q", got)
	}
}
