package retrieval

import (
	"context"
	"fmt"
	"regexp"
	"slices"
	"sort"
)

// System is a retrieval core under test: for each typed prompt it returns
// the ids of the items it would add to the chat, best first, or none for
// silence. It reads the event's id, project, first and text, never its
// labels. An error stops the run: it is a fault of the system, not a
// figure.
type System interface {
	Add(ctx context.Context, e Event) ([]string, error)
}

// Builder makes a system over a set's items and projects.
type Builder func(items []Item, projects []Project) (System, error)

var (
	systems    = map[string]Builder{}
	systemName = regexp.MustCompile(`^[a-z0-9][a-z0-9-]{0,31}$`)
)

// Register adds a system by name, from its file's init, so that a new core
// is scored by adding a file. It panics on a name that is taken or is not
// lower-case letters, digits and dashes, or on no builder: each is a
// mistake in the program.
func Register(name string, build func([]Item, []Project) (System, error)) {
	if _, taken := systems[name]; taken || !systemName.MatchString(name) || build == nil {
		panic(fmt.Sprintf("retrieval: cannot register the system %q: its name is taken or malformed, or it has no builder", name))
	}
	systems[name] = build
}

// Systems returns the registered systems' names, sorted.
func Systems() []string {
	names := make([]string, 0, len(systems))
	for name := range systems {
		names = append(names, name)
	}
	sort.Strings(names)
	return names
}

// Build makes the named system over the set's items and projects. Each
// gets copies, so no system can change the set another is scored on, and
// each project's pages are read from the set's groups, the one place that
// says which projects share a system page.
func Build(name string, s *Set) (System, error) {
	build, ok := systems[name]
	if !ok {
		return nil, fmt.Errorf("retrieval: there is no system %s", name)
	}
	items := make([]Item, len(s.Items))
	for i, it := range s.Items {
		it.Cues, it.Paths = slices.Clone(it.Cues), slices.Clone(it.Paths)
		items[i] = it
	}
	projects := make([]Project, len(s.Projects))
	for i, p := range s.Projects {
		p.Pages = nil
		for _, g := range s.Groups {
			if slices.Contains(g.Projects, p.ID) {
				p.Pages = append(p.Pages, g.Page)
			}
		}
		projects[i] = p
	}
	sys, err := build(items, projects)
	if err != nil {
		return nil, fmt.Errorf("retrieval: %s: %w", name, err)
	}
	return sys, nil
}

func init() {
	Register("silent", func([]Item, []Project) (System, error) { return silent{}, nil })
	Register("oracle", func([]Item, []Project) (System, error) { return oracle{}, nil })
}

// silent adds nothing: the floor of recall, and the ceiling of silence.
type silent struct{}

func (silent) Add(context.Context, Event) ([]string, error) { return nil, nil }

// oracle adds exactly the relevant items, from the labels: the ceiling a
// test arm compares with.
type oracle struct{}

func (oracle) Add(_ context.Context, e Event) ([]string, error) { return slices.Clone(e.Relevant), nil }
