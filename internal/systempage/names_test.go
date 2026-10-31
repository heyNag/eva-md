package systempage

import (
	"cmp"
	"context"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/heyNag/eva-md/internal/wikigit"
)

// Every write lists each page's title, slug and aliases in the names index,
// once each and sorted by name, with the page's id and title; the daily
// commit stages it with the pages.
func TestNamesIndex(t *testing.T) {
	o := fixture(t)
	mustWrite(t, o, "orders", with(t, "aliases: [order service]", `aliases: [order service, "  Orders   service ", "LedgerSync", "orders"]`), false)
	mustWrite(t, o, "payments", other("Payments", "Charges cards and pays out", "github.com/acme/payments"), false)
	want := "LedgerSync\tsystems/orders\tOrders service\n" +
		"Orders service\tsystems/orders\tOrders service\n" +
		"Payments\tsystems/payments\tPayments\n" +
		"order service\tsystems/orders\tOrders service\n" +
		"orders\tsystems/orders\tOrders service\n" +
		"payments\tsystems/payments\tPayments\n"
	if got := file(t, o, NamesFile); got != want {
		t.Fatalf("names index\n%s", got)
	}
	if got := ParseNames([]byte(want)); len(got) != 6 || got[0] != (Name{"LedgerSync", "systems/orders", "Orders service"}) {
		t.Fatalf("parsed %+v", got)
	}
	c, err := wikigit.Commit(context.Background(), wikigit.Options{Config: o.Config, Dirs: o.Dirs, Now: func() time.Time { return day }})
	if err != nil || c.Outcome != wikigit.Committed || len(c.Unstaged) != 0 {
		t.Fatalf("commit %+v %v", c, err)
	}
	if staged := git(t, o.Config.WikiPath, "ls-files", NamesFile); strings.TrimSpace(staged) != NamesFile {
		t.Fatalf("the commit left out the names index: %q", staged)
	}
}

// eva-md doctor writes the index again when it is missing, damaged or out of
// date, as after a hand edit of a page, and leaves it alone otherwise.
// eva-md status reports such an index as stale and leaves it as it is.
func TestRefreshNames(t *testing.T) {
	o := fixture(t)
	mustWrite(t, o, "orders", valid, false)
	written := file(t, o, NamesFile)
	index := filepath.Join(o.Config.WikiPath, filepath.FromSlash(NamesFile))
	page := filepath.Join(o.Config.WikiPath, "systems", "orders.md")
	for _, tc := range []struct {
		name   string
		damage func() error
		want   string
	}{
		{"current", func() error { return nil }, written},
		{"missing", func() error { return os.Remove(index) }, written},
		{"damaged", func() error { return os.WriteFile(index, []byte("orders\tnot a page\n\x00"), 0o600) }, written},
		{"out of date", func() error {
			text := file(t, o, "systems/orders.md")
			if !strings.Contains(text, "\ntitle: Orders service\n") {
				t.Fatalf("saved page\n%s", text)
			}
			return os.WriteFile(page, []byte(strings.Replace(text, "\ntitle: Orders service\n", "\ntitle: Orders hub\n", 1)), 0o644)
		}, strings.ReplaceAll(strings.Replace(written, "Orders service\tsystems", "Orders hub\tsystems", 1), "\tOrders service\n", "\tOrders hub\n")},
	} {
		if err := tc.damage(); err != nil {
			t.Fatal(err)
		}
		before, _ := os.ReadFile(index)
		r, err := RefreshNames(context.Background(), o, false)
		if after, _ := os.ReadFile(index); err != nil || r.Stale != (tc.name != "current") || r.Rebuilt || string(after) != string(before) {
			t.Fatalf("%s: status %+v %v, index\n%s", tc.name, r, err, after)
		}
		r, err = RefreshNames(context.Background(), o, true)
		if err != nil || r.Rebuilt != (tc.name != "current") || r.Stale || r.Pages != 1 || r.Names != strings.Count(tc.want, "\n") {
			t.Fatalf("%s: %+v %v", tc.name, r, err)
		}
		if got := file(t, o, NamesFile); got != tc.want {
			t.Fatalf("%s: index\n%s", tc.name, got)
		}
	}
	if r, err := RefreshNames(context.Background(), o, true); err != nil || r.Rebuilt {
		t.Fatalf("a current index was written again: %+v %v", r, err)
	}
}

// Past 2,000 names or 128 KB, the aliases of the pages with the oldest as_of
// go first, and only then, if need be, those pages' other names.
func TestNamesBounds(t *testing.T) {
	page := func(i int, aliases int) system {
		e := system{MapEntry: MapEntry{Slug: fmt.Sprintf("system-%04d", i), Title: fmt.Sprintf("System %04d", i), AsOf: fmt.Sprintf("2026-%02d-01", 1+i%12)}}
		for a := range aliases {
			e.aliases = append(e.aliases, fmt.Sprintf("alias %04d %02d %s", i, a, strings.Repeat("x", 40)))
		}
		return e
	}
	var entries []system
	for i := range 200 {
		entries = append(entries, page(i, 10))
	}
	text, r := names(entries)
	if len(text) > MaxNamesSize || r.Names > MaxNames || r.Names != strings.Count(text, "\n") || r.NoAliases == 0 || r.LeftOut != 0 || r.Pages != 200 {
		t.Fatalf("report %+v, %d bytes", r, len(text))
	}
	listed := func(name string) bool { return strings.Contains("\n"+text, "\n"+name+"\t") }
	// Every page keeps its title and slug, and the pages that lost their
	// aliases are the oldest, by as_of and then slug.
	oldest := slices.Clone(entries)
	slices.SortFunc(oldest, func(a, b system) int { return cmp.Or(strings.Compare(a.AsOf, b.AsOf), strings.Compare(a.Slug, b.Slug)) })
	for i, e := range oldest {
		if !listed(e.Title) || !listed(e.Slug) || listed(e.aliases[0]) != (i >= r.NoAliases) {
			t.Fatalf("page %d of the oldest first, %s (as of %s), of %d that lost their aliases", i, e.Slug, e.AsOf, r.NoAliases)
		}
	}
	entries = nil
	for i := range MaxNames {
		entries = append(entries, page(i, 0))
	}
	text, r = names(entries)
	if r.Names > MaxNames || len(text) > MaxNamesSize || r.LeftOut == 0 || r.NoAliases != 0 || listed("System 0000") || !listed("System 1999") {
		t.Fatalf("report %+v, %d bytes", r, len(text))
	}
}

// A name lists on one line, with its case and the inside of it kept and
// anything other than a letter or a digit taken off its ends.
func TestNormalName(t *testing.T) {
	for in, want := range map[string]string{
		"  Orders \t  service ": "Orders service",
		"“Orders”":              "Orders",
		"auth.v2":               "auth.v2",
		"(beta) Ledger, Sync.":  "beta) Ledger, Sync",
		"C++":                   "C",
		"--":                    "",
	} {
		if got := normalName(in); got != want {
			t.Errorf("normalName(%q) = %q, want %q", in, got, want)
		}
	}
}

// ParseNames skips any line that is not one of the index's.
func TestParseNames(t *testing.T) {
	data := "LedgerSync\tsystems/orders\tOrders service\n" +
		"no tabs at all\n" +
		"\tsystems/orders\tno name\n" +
		"orders\tprojects/orders\tnot a system page\n" +
		"orders\tsystems/map\tthe map\n" +
		"orders\tsystems/orders\ttitle\textra field\n" +
		"payments-api\tsystems/payments\tPayments\n"
	want := []Name{{"LedgerSync", "systems/orders", "Orders service"}, {"payments-api", "systems/payments", "Payments"}}
	if got := ParseNames([]byte(data)); !reflect.DeepEqual(got, want) {
		t.Fatalf("parsed %+v", got)
	}
}
