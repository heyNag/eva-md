package registry_test

import (
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"testing"

	"github.com/heyNag/eva-md/internal/ledger/registry"
	"github.com/heyNag/eva-md/internal/store/codec"
)

// The store's fixture, and the README beside it that lists the fixture's
// project ids that do not follow the rule.
var (
	fixture       = filepath.Join("..", "..", "store", "testdata", "fixture-store")
	fixtureReadme = filepath.Join("..", "..", "store", "testdata", "README.md")
)

// reListed is a line of the README's list: one id in backticks.
var reListed = regexp.MustCompile("^- `(p_[^`]*)`$")

// exceptions reads the ids the README lists, one a line.
func exceptions(readme string) []string {
	var out []string
	for _, l := range strings.Split(readme, "\n") {
		if m := reListed.FindStringSubmatch(strings.TrimRight(l, "\r")); m != nil {
			out = append(out, m[1])
		}
	}
	return out
}

// record is what a project record of the fixture says of its identity.
type record struct {
	path, id, firstKey string
}

// records reads every project.md under root through the codec.
func records(t *testing.T, root string) []record {
	t.Helper()
	var out []record
	err := filepath.WalkDir(root, func(p string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() || d.Name() != "project.md" {
			return err
		}
		b, err := os.ReadFile(p)
		if err != nil {
			return err
		}
		doc := codec.Read(b)
		if why := doc.Unreadable(); why != "" {
			t.Fatalf("%s is unreadable: %s", p, why)
		}
		id, err := doc.Value("id")
		if err != nil {
			t.Fatalf("%s: id: %v", p, err)
		}
		keys, err := doc.Value("legacy_keys")
		if err != nil {
			t.Fatalf("%s: legacy_keys: %v", p, err)
		}
		list, _ := keys.([]any)
		if len(list) == 0 {
			t.Fatalf("%s: legacy_keys is %v, not a list of keys", p, keys)
		}
		first, _ := list[0].(string)
		s, _ := id.(string)
		out = append(out, record{path: p, id: s, firstKey: first})
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	return out
}

// faults lists what is wrong between the records and the README's list:
// a record whose id is not ProjectID of its first legacy key and is not
// listed, a listed id that follows the rule, and a listed id no record
// holds.
func faults(rs []record, listed []string) []string {
	var out []string
	held := map[string]bool{}
	for _, r := range rs {
		held[r.id] = true
		want, ok := registry.ProjectID(r.firstKey)
		switch {
		case !ok:
			out = append(out, r.path+" has an empty first legacy key")
		case want == r.id && slices.Contains(listed, r.id):
			out = append(out, "the README lists "+r.id+", which follows the rule")
		case want != r.id && !slices.Contains(listed, r.id):
			out = append(out, r.path+" has the id "+r.id+", not "+want+", and the README does not list it")
		}
	}
	for _, id := range listed {
		if !held[id] {
			out = append(out, "the README lists "+id+", which no project record holds")
		}
	}
	return out
}

func TestFixtureProjectIDs(t *testing.T) {
	rs := records(t, fixture)
	if len(rs) < 2 {
		t.Fatalf("only %d project records in the fixture", len(rs))
	}
	readme, err := os.ReadFile(fixtureReadme)
	if err != nil {
		t.Fatal(err)
	}
	listed := exceptions(string(readme))
	if !slices.Equal(listed, []string{"p_00gb3367a19r"}) {
		t.Errorf("the README lists %v", listed)
	}
	for _, f := range faults(rs, listed) {
		t.Error(f)
	}
}

// The check fails on each kind of fault: an id off the rule that the
// README does not list, a listed id that follows the rule, and a listed
// id no record holds.
func TestFixtureCheckCanFail(t *testing.T) {
	rs := records(t, fixture)
	readme, err := os.ReadFile(fixtureReadme)
	if err != nil {
		t.Fatal(err)
	}
	listed := exceptions(string(readme))
	if got := faults(rs, nil); len(got) != 1 || !strings.Contains(got[0], "p_00gb3367a19r") {
		t.Errorf("with the README's list dropped: %v", got)
	}
	if got := faults(rs, append(slices.Clone(listed), "p_6pbe1q7fxb5h")); len(got) != 1 || !strings.Contains(got[0], "follows the rule") {
		t.Errorf("with a listed id that follows the rule: %v", got)
	}
	if got := faults(rs, append(slices.Clone(listed), "p_0000000000zz")); len(got) != 1 || !strings.Contains(got[0], "no project record") {
		t.Errorf("with a listed id no record holds: %v", got)
	}
	for i, r := range rs {
		if slices.Contains(listed, r.id) {
			continue
		}
		bad := slices.Clone(rs)
		bad[i].firstKey = strings.ToUpper(r.firstKey)
		if got := faults(bad, listed); len(got) != 1 || !strings.Contains(got[0], "does not list it") {
			t.Errorf("with %s's first key upper-cased: %v", r.id, got)
		}
	}
	if got := exceptions("- `p_a`\r\n-  `p_b`\n`p_c`\n- `p_d` and more\n- `p_e`"); !slices.Equal(got, []string{"p_a", "p_e"}) {
		t.Errorf("the README's list reads as %v", got)
	}
}
