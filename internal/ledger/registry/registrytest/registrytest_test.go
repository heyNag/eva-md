package registrytest_test

import (
	"context"
	"errors"
	"slices"
	"strings"
	"sync"
	"testing"

	"github.com/heyNag/eva-md/internal/ledger/registry"
	"github.com/heyNag/eva-md/internal/ledger/registry/registrytest"
)

// entries are three made-up projects of three kinds, and the general area.
func entries() []registrytest.Entry {
	return []registrytest.Entry{
		{Key: "github.com__acme__orders", Kind: registry.KindRepo, Title: "acme/orders",
			Remotes: []string{"github.com/acme/orders"}, Roots: []string{"/made/up/orders"}},
		{Key: "local-scratch-9f2c1a", Kind: registry.KindLocal, Title: "scratch", Roots: []string{"/made/up/scratch"}},
		{Key: "folder-notes-0a1b2c", Kind: registry.KindFolder, Title: "notes"},
		{Key: "general", Kind: registry.KindGeneral, Title: "general"},
	}
}

func mustNew(t *testing.T, es ...registrytest.Entry) *registrytest.Fake {
	t.Helper()
	f, err := registrytest.New(es...)
	if err != nil {
		t.Fatal(err)
	}
	return f
}

func id(key string) string {
	s, _ := registry.ProjectID(key)
	return s
}

func TestNewRefuses(t *testing.T) {
	if _, err := registrytest.New(entries()...); err != nil {
		t.Fatalf("the entries every case breaks are refused: %v", err)
	}
	for name, change := range map[string]func(es []registrytest.Entry) []registrytest.Entry{
		"an empty key": func(es []registrytest.Entry) []registrytest.Entry {
			es[1].Key = ""
			return es
		},
		"a key given twice": func(es []registrytest.Entry) []registrytest.Entry {
			return append(es, registrytest.Entry{Key: es[0].Key, Kind: registry.KindLocal})
		},
		"a key given twice with one kind": func(es []registrytest.Entry) []registrytest.Entry {
			return append(es, es[2])
		},
		"general twice": func(es []registrytest.Entry) []registrytest.Entry {
			return append(es, es[3])
		},
		"an unknown kind": func(es []registrytest.Entry) []registrytest.Entry {
			es[0].Kind = "remote"
			return es
		},
		"no kind": func(es []registrytest.Entry) []registrytest.Entry {
			es[2].Kind = ""
			return es
		},
		"a kind in another case": func(es []registrytest.Entry) []registrytest.Entry {
			es[0].Kind = "Repo"
			return es
		},
		"general without its kind": func(es []registrytest.Entry) []registrytest.Entry {
			es[3].Kind = registry.KindLocal
			return es
		},
		"general as a folder": func(es []registrytest.Entry) []registrytest.Entry {
			es[3].Kind = registry.KindFolder
			return es
		},
		"the general kind for another key": func(es []registrytest.Entry) []registrytest.Entry {
			es[2].Kind = registry.KindGeneral
			return es
		},
		"the general kind for a key that differs in case": func(es []registrytest.Entry) []registrytest.Entry {
			es[3].Key = "General"
			return es
		},
	} {
		f, err := registrytest.New(change(entries())...)
		if err == nil || f != nil {
			t.Errorf("%s: New gave %v, %v; want a refusal", name, f, err)
		}
	}
	if f, err := registrytest.New(); err != nil {
		t.Errorf("no entries: %v", err)
	} else if ps, err := f.Projects(context.Background()); err != nil || len(ps) != 0 {
		t.Errorf("no entries list %v, %v", ps, err)
	}
}

// The fake holds one project for each entry, named by its key alone, and
// joins none.
func TestOneProjectPerKey(t *testing.T) {
	ctx := context.Background()
	es := entries()
	f := mustNew(t, es...)
	ps, err := f.Projects(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(ps) != len(es) {
		t.Fatalf("%d projects for %d entries", len(ps), len(es))
	}
	if !slices.IsSortedFunc(ps, func(a, b registry.Project) int { return strings.Compare(a.ID, b.ID) }) {
		t.Errorf("the listing is not sorted by id")
	}
	for _, e := range es {
		p, ok, err := f.ByKey(ctx, e.Key)
		if err != nil || !ok {
			t.Fatalf("ByKey(%q) = %v, %v", e.Key, ok, err)
		}
		if p.ID != id(e.Key) || p.Kind != e.Kind || p.Title != e.Title || !slices.Equal(p.Keys, []string{e.Key}) ||
			!slices.Equal(p.Remotes, e.Remotes) || !slices.Equal(p.Roots, e.Roots) {
			t.Errorf("ByKey(%q) = %+v, not its entry %+v", e.Key, p, e)
		}
		if p.MergedInto != "" || len(p.Related) != 0 || len(p.RootCommits) != 0 {
			t.Errorf("ByKey(%q) = %+v: the fake joined or related it", e.Key, p)
		}
		if !slices.ContainsFunc(ps, func(q registry.Project) bool { return q.ID == p.ID }) {
			t.Errorf("%s is not listed", p.ID)
		}
	}
	for _, key := range []string{"", "GitHub.com__acme__orders", "github.com/acme/orders", " general", "nobody"} {
		if p, ok, err := f.ByKey(ctx, key); ok || err != nil || p.ID != "" {
			t.Errorf("ByKey(%q) = %+v, %v, %v; want no project and no error", key, p, ok, err)
		}
	}
	if err := registrytest.Check(ctx, f); err != nil {
		t.Errorf("Check on the fake: %v", err)
	}
	if err := registrytest.Check(ctx, mustNew(t)); err != nil {
		t.Errorf("Check on an empty fake: %v", err)
	}
}

// What a caller changes in a returned project, or in the entries it gave
// New, does not change the next call.
func TestReturnsCopies(t *testing.T) {
	ctx := context.Background()
	es := entries()
	f := mustNew(t, es...)
	es[0].Remotes[0] = "changed"
	es[0].Roots[0] = "changed"
	ps, _ := f.Projects(ctx)
	for i := range ps {
		ps[i].ID, ps[i].Title, ps[i].MergedInto = "p_changed", "changed", "p_changed"
		ps[i].Keys[0] = "changed"
		ps[i].Related = append(ps[i].Related, "p_changed")
		if len(ps[i].Remotes) > 0 {
			ps[i].Remotes[0] = "changed"
		}
		if len(ps[i].Roots) > 0 {
			ps[i].Roots[0] = "changed"
		}
	}
	p, _, _ := f.ByKey(ctx, "github.com__acme__orders")
	p.Keys[0], p.Remotes[0], p.Roots[0] = "changed", "changed", "changed"
	again, _ := f.Projects(ctx)
	for _, q := range again {
		if q.Title == "changed" || q.MergedInto != "" || len(q.Related) != 0 || q.Keys[0] == "changed" ||
			slices.Contains(q.Remotes, "changed") || slices.Contains(q.Roots, "changed") {
			t.Errorf("a change reached the fake: %+v", q)
		}
	}
	p, _, _ = f.ByKey(ctx, "github.com__acme__orders")
	if p.Keys[0] != "github.com__acme__orders" || p.Remotes[0] != "github.com/acme/orders" || p.Roots[0] != "/made/up/orders" {
		t.Errorf("a change reached the fake: %+v", p)
	}
	if err := registrytest.Check(ctx, f); err != nil {
		t.Errorf("Check after the changes: %v", err)
	}
}

func TestFaultsAndCancel(t *testing.T) {
	ctx := context.Background()
	f := mustNew(t, entries()...)
	errList := errors.New("made-up listing fault")
	errKey := errors.New("made-up lookup fault")

	f.FailProjects(errList)
	if ps, err := f.Projects(ctx); !errors.Is(err, errList) || ps != nil {
		t.Errorf("Projects with a fault = %v, %v", ps, err)
	}
	if _, ok, err := f.ByKey(ctx, "general"); !ok || err != nil {
		t.Errorf("ByKey failed with Projects' fault: %v, %v", ok, err)
	}
	if err := registrytest.Check(ctx, f); !errors.Is(err, errList) {
		t.Errorf("Check with a listing fault = %v", err)
	}
	f.FailProjects(nil)
	f.FailByKey(errKey)
	if p, ok, err := f.ByKey(ctx, "general"); !errors.Is(err, errKey) || ok || p.ID != "" {
		t.Errorf("ByKey with a fault = %+v, %v, %v", p, ok, err)
	}
	if _, err := f.Projects(ctx); err != nil {
		t.Errorf("Projects failed with ByKey's fault: %v", err)
	}
	if err := registrytest.Check(ctx, f); !errors.Is(err, errKey) {
		t.Errorf("Check with a lookup fault = %v", err)
	}
	f.FailByKey(nil)
	if err := registrytest.Check(ctx, f); err != nil {
		t.Errorf("Check once the faults are undone: %v", err)
	}

	cancelled, cancel := context.WithCancel(ctx)
	cancel()
	if ps, err := f.Projects(cancelled); !errors.Is(err, context.Canceled) || ps != nil {
		t.Errorf("Projects with a cancelled context = %v, %v", ps, err)
	}
	if p, ok, err := f.ByKey(cancelled, "general"); !errors.Is(err, context.Canceled) || ok || p.ID != "" {
		t.Errorf("ByKey with a cancelled context = %+v, %v, %v", p, ok, err)
	}
	if err := registrytest.Check(cancelled, f); !errors.Is(err, context.Canceled) {
		t.Errorf("Check with a cancelled context = %v", err)
	}
}

// Calls and faults from many goroutines at once; the race detector judges.
func TestConcurrentUse(t *testing.T) {
	ctx := context.Background()
	f := mustNew(t, entries()...)
	errMade := errors.New("made-up fault")
	var wg sync.WaitGroup
	for i := range 8 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for j := range 50 {
				if (i+j)%5 == 0 {
					f.FailProjects(errMade)
					f.FailByKey(nil)
				} else if (i+j)%5 == 1 {
					f.FailProjects(nil)
					f.FailByKey(errMade)
				}
				ps, err := f.Projects(ctx)
				if err == nil {
					ps[0].Keys[0] = "changed"
				}
				if p, ok, err := f.ByKey(ctx, "general"); err == nil && ok {
					p.Keys[0] = "changed"
				}
			}
		}()
	}
	wg.Wait()
	f.FailProjects(nil)
	f.FailByKey(nil)
	if err := registrytest.Check(ctx, f); err != nil {
		t.Errorf("Check after concurrent use: %v", err)
	}
}

// broken wraps a registry and changes what it returns.
type broken struct {
	r        registry.Registry
	projects func([]registry.Project) []registry.Project
	byKey    func(key string, p registry.Project, ok bool) (registry.Project, bool)
}

func (b broken) Projects(ctx context.Context) ([]registry.Project, error) {
	ps, err := b.r.Projects(ctx)
	if err != nil || b.projects == nil {
		return ps, err
	}
	return b.projects(ps), nil
}

func (b broken) ByKey(ctx context.Context, key string) (registry.Project, bool, error) {
	p, ok, err := b.r.ByKey(ctx, key)
	if err != nil || b.byKey == nil {
		return p, ok, err
	}
	p, ok = b.byKey(key, p, ok)
	return p, ok, nil
}

// sorted sorts a listing by id, so a change breaks only the clause it
// means to.
func sorted(ps []registry.Project) []registry.Project {
	slices.SortFunc(ps, func(a, b registry.Project) int { return strings.Compare(a.ID, b.ID) })
	return ps
}

// at is the index of the project a key named.
func at(ps []registry.Project, key string) int {
	return slices.IndexFunc(ps, func(p registry.Project) bool { return p.Keys[0] == key })
}

// For each clause of the contract, a registry that breaks it alone fails
// Check, which names the fault.
func TestCheckNamesEachFault(t *testing.T) {
	ctx := context.Background()
	f := mustNew(t, entries()...)
	orders, general := "github.com__acme__orders", "general"
	for _, c := range []struct {
		name string
		b    broken
		want string
	}{
		{"an unsorted listing", broken{projects: func(ps []registry.Project) []registry.Project {
			ps[0], ps[1] = ps[1], ps[0]
			return ps
		}}, "not sorted"},
		{"an id listed twice", broken{projects: func(ps []registry.Project) []registry.Project {
			return slices.Insert(ps, 1, ps[0])
		}}, "listed twice"},
		{"an id that is not of the first key", broken{projects: func(ps []registry.Project) []registry.Project {
			ps[at(ps, orders)].ID = id(strings.ToUpper(orders))
			return sorted(ps)
		}}, "is not the id of its first key"},
		{"an id of the second key", broken{projects: func(ps []registry.Project) []registry.Project {
			i := at(ps, orders)
			ps[i].Keys = []string{"github.com__acme__orders-old", orders}
			return ps
		}}, "is not the id of its first key"},
		{"a project with no key", broken{projects: func(ps []registry.Project) []registry.Project {
			ps[at(ps, orders)].Keys = nil
			return ps
		}}, "has no key"},
		{"a project named by the empty key, merged into a listed one", broken{projects: func(ps []registry.Project) []registry.Project {
			i := at(ps, orders)
			ps[i] = registry.Project{ID: "", Kind: registry.KindLocal, Keys: []string{""}, MergedInto: id(general)}
			return sorted(ps)
		}}, "has an empty first key"},
		{"a project named by the empty key alone", broken{projects: func(ps []registry.Project) []registry.Project {
			i := at(ps, orders)
			ps[i] = registry.Project{ID: "", Kind: registry.KindLocal, Keys: []string{""}}
			return sorted(ps)
		}}, "has an empty first key"},
		{"an empty key after the first", broken{projects: func(ps []registry.Project) []registry.Project {
			i := at(ps, orders)
			ps[i].Keys = append(ps[i].Keys, "")
			return ps
		}}, "has an empty key"},
		{"a key listed twice in one project", broken{projects: func(ps []registry.Project) []registry.Project {
			i := at(ps, orders)
			ps[i].Keys = append(ps[i].Keys, orders)
			return ps
		}}, "is listed twice in"},
		{"an unknown kind", broken{projects: func(ps []registry.Project) []registry.Project {
			ps[at(ps, orders)].Kind = "remote"
			return ps
		}}, "unknown kind"},
		{"a key that names two projects", broken{projects: func(ps []registry.Project) []registry.Project {
			i := at(ps, orders)
			ps[i].Keys = append(ps[i].Keys, general)
			return ps
		}}, "names two projects"},
		{"a key ByKey does not find", broken{byKey: func(key string, p registry.Project, ok bool) (registry.Project, bool) {
			if key == general {
				return registry.Project{}, false
			}
			return p, ok
		}}, "finds no project"},
		{"a key ByKey gives another project for", broken{byKey: func(key string, p registry.Project, ok bool) (registry.Project, bool) {
			if key == general {
				q, _, _ := f.ByKey(ctx, orders)
				return q, true
			}
			return p, ok
		}}, "not its listed project"},
		{"ByKey that follows a join the listing does not have", broken{byKey: func(key string, p registry.Project, ok bool) (registry.Project, bool) {
			if key == orders {
				p.MergedInto = id(general)
			}
			return p, ok
		}}, "not its listed project"},
		{"ByKey that gives other remotes", broken{byKey: func(key string, p registry.Project, ok bool) (registry.Project, bool) {
			if key == orders {
				p.Remotes = append(p.Remotes, "github.com/acme/other")
			}
			return p, ok
		}}, "not its listed project"},
		{"a project merged into itself", broken{projects: func(ps []registry.Project) []registry.Project {
			i := at(ps, orders)
			ps[i].MergedInto = ps[i].ID
			return ps
		}}, "merged into itself"},
		{"a project merged into one not listed", broken{projects: func(ps []registry.Project) []registry.Project {
			ps[at(ps, orders)].MergedInto = id("github.com__acme__gone")
			return ps
		}}, "which is not listed"},
		{"a project related to itself", broken{projects: func(ps []registry.Project) []registry.Project {
			i := at(ps, orders)
			ps[i].Related = []string{id(general), ps[i].ID}
			return ps
		}}, "related to itself"},
		{"a project related to one not listed", broken{projects: func(ps []registry.Project) []registry.Project {
			ps[at(ps, orders)].Related = []string{id(general), id("github.com__acme__gone")}
			return ps
		}}, "which is not listed"},
	} {
		c.b.r = f
		if c.b.byKey == nil && c.b.projects != nil {
			// ByKey gives what the changed listing holds, so only the
			// clause the change means to break is broken.
			c.b.byKey = func(key string, p registry.Project, ok bool) (registry.Project, bool) {
				ps, _ := c.b.Projects(ctx)
				for _, q := range ps {
					if slices.Contains(q.Keys, key) {
						return q, true
					}
				}
				return p, ok
			}
		}
		err := registrytest.Check(ctx, c.b)
		if !errors.Is(err, registrytest.ErrContract) || !strings.Contains(err.Error(), c.want) {
			t.Errorf("%s: Check = %v; want a fault naming %q", c.name, err, c.want)
		}
	}
}

// ByKey that gives the listed project with any one field changed fails
// Check, which names the clause "ByKey of every key gives its project".
func TestCheckComparesEveryFieldOfByKey(t *testing.T) {
	ctx := context.Background()
	f := mustNew(t, entries()...)
	orders, general := "github.com__acme__orders", "general"
	for name, change := range map[string]func(p *registry.Project){
		"ID":          func(p *registry.Project) { p.ID = id(general) },
		"Kind":        func(p *registry.Project) { p.Kind = registry.KindLocal },
		"Title":       func(p *registry.Project) { p.Title = "acme/other" },
		"Keys":        func(p *registry.Project) { p.Keys = append(p.Keys, "github.com__acme__orders-old") },
		"Remotes":     func(p *registry.Project) { p.Remotes = append(p.Remotes, "github.com/acme/other") },
		"RootCommits": func(p *registry.Project) { p.RootCommits = []string{"0000000000000000000000000000000000000001"} },
		"Roots":       func(p *registry.Project) { p.Roots = append(p.Roots, "/made/up/other") },
		"MergedInto":  func(p *registry.Project) { p.MergedInto = id(general) },
		"Related":     func(p *registry.Project) { p.Related = []string{id(general)} },
	} {
		b := broken{r: f, byKey: func(key string, p registry.Project, ok bool) (registry.Project, bool) {
			if key == orders {
				change(&p)
			}
			return p, ok
		}}
		err := registrytest.Check(ctx, b)
		if !errors.Is(err, registrytest.ErrContract) || !strings.Contains(err.Error(), "not its listed project") {
			t.Errorf("ByKey with another %s: Check = %v; want a fault naming %q", name, err, "not its listed project")
		}
	}
}

// A registry whose joins and relations name listed projects, the same in
// its listing and its lookups, passes.
func TestCheckPassesJoinsThatHold(t *testing.T) {
	ctx := context.Background()
	f := mustNew(t, entries()...)
	orders, scratch := "github.com__acme__orders", "local-scratch-9f2c1a"
	change := func(p registry.Project) registry.Project {
		switch p.Keys[0] {
		case scratch:
			p.Keys = append(p.Keys, "local-scratch-old-000000")
			p.MergedInto = id(orders)
		case orders:
			p.Related = []string{id(scratch), id("general")}
			p.RootCommits = []string{"0000000000000000000000000000000000000001"}
		}
		return p
	}
	b := broken{r: f,
		projects: func(ps []registry.Project) []registry.Project {
			for i := range ps {
				ps[i] = change(ps[i])
			}
			return ps
		},
		byKey: func(key string, p registry.Project, ok bool) (registry.Project, bool) {
			if key == "local-scratch-old-000000" {
				q, _, _ := f.ByKey(ctx, scratch)
				return change(q), true
			}
			if ok {
				p = change(p)
			}
			return p, ok
		},
	}
	if err := registrytest.Check(ctx, b); err != nil {
		t.Errorf("Check: %v", err)
	}
}
