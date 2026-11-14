// Package registrytest is the project registry's fake, for other
// packages' tests: one project for each key it is given, and no join.
// Check holds any registry, this fake or a real one, to the registry's
// contract. Only tests import it.
package registrytest

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"strings"
	"sync"

	"github.com/heyNag/eva-md/internal/ledger/registry"
)

// Entry is one project of the fake, named by one key.
type Entry struct {
	Key     string
	Kind    registry.Kind
	Title   string
	Remotes []string
	Roots   []string
}

// Fake is a registry.Registry that holds one project for each entry,
// whose Keys hold its key alone, and joins none. It is safe for
// concurrent use, and every project it returns is a copy.
type Fake struct {
	projects []registry.Project // sorted by id
	byKey    map[string]int     // a key to its project's index

	mu          sync.Mutex
	projectsErr error
	byKeyErr    error
}

var _ registry.Registry = (*Fake)(nil)

// New makes a fake of the entries. It refuses an empty key, a key given
// twice, an unknown kind, the key "general" with any kind but
// KindGeneral, and KindGeneral with any other key.
func New(es ...Entry) (*Fake, error) {
	f := &Fake{byKey: map[string]int{}}
	seen := map[string]bool{}
	for i, e := range es {
		id, ok := registry.ProjectID(e.Key)
		switch {
		case !ok:
			return nil, fmt.Errorf("registrytest: entry %d has no key", i)
		case seen[e.Key]:
			return nil, fmt.Errorf("registrytest: the key %q is given twice", e.Key)
		case !e.Kind.Known():
			return nil, fmt.Errorf("registrytest: the key %q has the unknown kind %q", e.Key, e.Kind)
		case (e.Key == generalKey) != (e.Kind == registry.KindGeneral):
			return nil, fmt.Errorf("registrytest: the key %q has the kind %q; the general area alone has the key %q and that kind", e.Key, e.Kind, generalKey)
		}
		seen[e.Key] = true
		f.projects = append(f.projects, registry.Project{
			ID:      id,
			Kind:    e.Kind,
			Title:   e.Title,
			Keys:    []string{e.Key},
			Remotes: slices.Clone(e.Remotes),
			Roots:   slices.Clone(e.Roots),
		})
	}
	slices.SortFunc(f.projects, func(a, b registry.Project) int { return strings.Compare(a.ID, b.ID) })
	for i, p := range f.projects {
		if i > 0 && f.projects[i-1].ID == p.ID {
			return nil, fmt.Errorf("registrytest: the keys %q and %q give one id", f.projects[i-1].Keys[0], p.Keys[0])
		}
		f.byKey[p.Keys[0]] = i
	}
	return f, nil
}

// generalKey is the general area's key, v1's identity.GeneralKey.
const generalKey = "general"

// FailProjects makes every later call of Projects return err; nil
// undoes it.
func (f *Fake) FailProjects(err error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.projectsErr = err
}

// FailByKey makes every later call of ByKey return err; nil undoes it.
func (f *Fake) FailByKey(err error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.byKeyErr = err
}

// Projects lists every project, sorted by id.
func (f *Fake) Projects(ctx context.Context) ([]registry.Project, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	f.mu.Lock()
	err := f.projectsErr
	f.mu.Unlock()
	if err != nil {
		return nil, err
	}
	out := make([]registry.Project, len(f.projects))
	for i, p := range f.projects {
		out[i] = clone(p)
	}
	return out, nil
}

// ByKey gives the project a key named, or false.
func (f *Fake) ByKey(ctx context.Context, key string) (registry.Project, bool, error) {
	if err := ctx.Err(); err != nil {
		return registry.Project{}, false, err
	}
	f.mu.Lock()
	err := f.byKeyErr
	f.mu.Unlock()
	if err != nil {
		return registry.Project{}, false, err
	}
	i, ok := f.byKey[key]
	if !ok {
		return registry.Project{}, false, nil
	}
	return clone(f.projects[i]), true, nil
}

// clone copies a project, its lists too.
func clone(p registry.Project) registry.Project {
	p.Keys = slices.Clone(p.Keys)
	p.Remotes = slices.Clone(p.Remotes)
	p.RootCommits = slices.Clone(p.RootCommits)
	p.Roots = slices.Clone(p.Roots)
	p.Related = slices.Clone(p.Related)
	return p
}

// ErrContract is the error Check wraps for a fault of the contract.
var ErrContract = errors.New("registrytest: the registry breaks its contract")

// Check names the first fault of a registry against the contract: the
// listing is sorted by id with no id twice; every id is ProjectID of the
// project's first key, and every kind is known; no key names two
// projects; ByKey of every key gives its project; MergedInto and Related
// name listed projects, and never the project itself. An error of the
// registry's own is returned as it is.
func Check(ctx context.Context, r registry.Registry) error {
	ps, err := r.Projects(ctx)
	if err != nil {
		return err
	}
	listed := map[string]bool{}
	for i, p := range ps {
		if i > 0 && ps[i-1].ID >= p.ID {
			if ps[i-1].ID == p.ID {
				return fault("the id %s is listed twice", p.ID)
			}
			return fault("the listing is not sorted by id: %s comes before %s", ps[i-1].ID, p.ID)
		}
		listed[p.ID] = true
	}
	owner := map[string]string{} // a key to the id of the project it named
	for _, p := range ps {
		if len(p.Keys) == 0 {
			return fault("the project %s has no key", p.ID)
		}
		if id, _ := registry.ProjectID(p.Keys[0]); id != p.ID {
			return fault("the project %s is not the id of its first key %q, which is %q", p.ID, p.Keys[0], id)
		}
		if !p.Kind.Known() {
			return fault("the project %s has the unknown kind %q", p.ID, p.Kind)
		}
		for _, k := range p.Keys {
			if other, ok := owner[k]; ok {
				return fault("the key %q names two projects, %s and %s", k, other, p.ID)
			}
			owner[k] = p.ID
		}
	}
	for _, p := range ps {
		for _, k := range p.Keys {
			got, ok, err := r.ByKey(ctx, k)
			if err != nil {
				return err
			}
			if !ok {
				return fault("ByKey of %q, a key of %s, finds no project", k, p.ID)
			}
			if !same(got, p) {
				return fault("ByKey of %q gives %s, not its listed project %s", k, got.ID, p.ID)
			}
		}
	}
	for _, p := range ps {
		if p.MergedInto == p.ID {
			return fault("the project %s is merged into itself", p.ID)
		}
		if p.MergedInto != "" && !listed[p.MergedInto] {
			return fault("the project %s is merged into %s, which is not listed", p.ID, p.MergedInto)
		}
		for _, id := range p.Related {
			if id == p.ID {
				return fault("the project %s is related to itself", p.ID)
			}
			if !listed[id] {
				return fault("the project %s is related to %s, which is not listed", p.ID, id)
			}
		}
	}
	return nil
}

func fault(format string, args ...any) error {
	return fmt.Errorf("%w: "+format, append([]any{ErrContract}, args...)...)
}

// same reports whether two projects hold the same fields, an empty list
// and none alike.
func same(a, b registry.Project) bool {
	return a.ID == b.ID && a.Kind == b.Kind && a.Title == b.Title && a.MergedInto == b.MergedInto &&
		slices.Equal(a.Keys, b.Keys) && slices.Equal(a.Remotes, b.Remotes) &&
		slices.Equal(a.RootCommits, b.RootCommits) && slices.Equal(a.Roots, b.Roots) &&
		slices.Equal(a.Related, b.Related)
}
