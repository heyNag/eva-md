package health

import (
	"context"
	"os"
	"slices"
	"strings"
	"testing"

	"github.com/heyNag/eva-md/internal/paths"
)

// withProducers lets a test register producers of its own, and leaves the
// package's registry as it was when the test ends.
func withProducers(t *testing.T) {
	t.Helper()
	saved := slices.Clone(producers)
	t.Cleanup(func() { producers = saved })
}

// names lists a report's row names.
func names(r Report) []string {
	var out []string
	for _, c := range r.Checks {
		out = append(out, c.Name)
	}
	return out
}

// A producer registered by a test, at a place between the hooks' rows (600)
// and the MCP manifests' (700), adds its row there, once, in eva-md status
// and in the doctor with and without the hosts, and every other row stays
// where it was.
func TestRegisteredProducerAppearsAtItsPlace(t *testing.T) {
	for _, tc := range []struct {
		name string
		home int
		run  func(context.Context, paths.Dirs) Report
	}{
		{"status", setUpHome, Status},
		{"doctor", setUpHome, doctor},
		{"doctor-local", setUpHome, local},
		{"doctor-bare", bareHome, doctor},
	} {
		t.Run(tc.name, func(t *testing.T) {
			d, _ := goldenHome(t, tc.home)
			before := names(tc.run(t.Context(), d))
			withProducers(t)
			calls := 0
			registerRows(650, "test row", func(r *Report, in *inspection) {
				calls++
				r.add("test row", "PASS", "added by a test", "")
			})
			after := names(tc.run(t.Context(), d))
			if calls != 1 {
				t.Fatalf("the producer ran %d times", calls)
			}
			at := slices.Index(after, "test row")
			if at < 1 || slices.Index(after[at+1:], "test row") >= 0 {
				t.Fatalf("test row at %d in %v", at, after)
			}
			if after[at-1] != "codex hook times" || after[at+1] != "claude mcp manifest" {
				t.Fatalf("test row between %q and %q, not after the hooks' rows and before the MCP manifests'", after[at-1], after[at+1])
			}
			if got := slices.Delete(slices.Clone(after), at, at+1); !slices.Equal(got, before) {
				t.Fatalf("the other rows moved:\nbefore %v\nafter  %v", before, got)
			}
		})
	}
}

// A producer registered before every other, or after every other, comes
// first or last, whatever order the registrations came in.
func TestProducersRunInTheOrderOfTheirPlaces(t *testing.T) {
	d, _ := goldenHome(t, bareHome)
	withProducers(t)
	registerRows(1<<30, "last", func(r *Report, in *inspection) { r.add("last", "PASS", "", "") })
	registerRows(-1, "first", func(r *Report, in *inspection) { r.add("first", "PASS", "", "") })
	got := names(Status(t.Context(), d))
	if got[0] != "first" || got[len(got)-1] != "last" {
		t.Fatalf("rows %v", got)
	}
	for i := 1; i < len(producers); i++ {
		if producers[i-1].place >= producers[i].place {
			t.Fatalf("producers %q at %d and %q at %d are out of order", producers[i-1].name, producers[i-1].place, producers[i].name, producers[i].place)
		}
	}
}

// Two producers at one place fail, naming both and the place, and the
// second is not registered.
func TestTwoProducersAtOnePlaceFail(t *testing.T) {
	withProducers(t)
	before := len(producers)
	defer func() {
		msg, _ := recover().(string)
		if !strings.Contains(msg, `"hooks" and "second hooks" both take place 600`) {
			t.Fatalf("panic %q", msg)
		}
		if len(producers) != before {
			t.Fatalf("%d producers after the failed registration, %d before", len(producers), before)
		}
	}()
	registerRows(600, "second hooks", func(r *Report, in *inspection) {})
	t.Fatal("a second producer at place 600 was registered")
}

// The doctor's rows read the config once; eva-md status's own rows read it
// once more after them, so a config that has become unreadable by then
// leaves them out, as before they were registered.
func TestStatusRowsReadTheConfigAgain(t *testing.T) {
	d, _ := goldenHome(t, setUpHome)
	withProducers(t)
	registerRows(1060, "config gone", func(r *Report, in *inspection) {
		if err := os.Remove(in.d.ConfigFile()); err != nil {
			t.Fatal(err)
		}
	})
	got := names(Status(t.Context(), d))
	if !slices.Contains(got, "codex updates") || !slices.Contains(got, "config keys") {
		t.Fatalf("doctor's rows missing: %v", got)
	}
	for _, name := range []string{"wiki contents", "update backlog"} {
		if slices.Contains(got, name) {
			t.Fatalf("%s row with the config gone: %v", name, got)
		}
	}
}
