package health

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/heyNag/eva-md/internal/config"
	"github.com/heyNag/eva-md/internal/okfadapter"
	"github.com/heyNag/eva-md/internal/paths"
	"github.com/heyNag/eva-md/internal/systempage"
)

// namesWiki makes a wiki with its schema and the config's instance, and no
// system page yet.
func namesWiki(t *testing.T) (paths.Dirs, config.Config) {
	t.Helper()
	d := isolated(t)
	wiki, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	put(t, filepath.Join(wiki, "index.md"), []byte("---\nokf_version: \"0.2\"\n---\n# Wiki\n"))
	if err := config.SaveSchema(filepath.Join(wiki, ".eva-md", "schema.toml"), config.WikiSchema{SchemaVersion: 1, Instance: "test", Created: "2026-10-29"}); err != nil {
		t.Fatal(err)
	}
	c := config.Default()
	c.WikiPath, c.Instance = wiki, "test"
	return d, c
}

// systemFile writes a system page by hand, as a person might.
func systemFile(t *testing.T, c config.Config, slug, title, asOf string, aliases ...string) {
	t.Helper()
	list := `[]`
	if len(aliases) > 0 {
		list = `["` + strings.Join(aliases, `", "`) + `"]`
	}
	page := "---\ntype: system\ntitle: " + title + "\ndescription: " + title + " in short\nstatus: stable\naliases: " + list + "\nas_of: \"" + asOf + "\"\nstate: active\n---\n\n## Summary\n" + title + ".\n"
	if err := os.MkdirAll(filepath.Join(c.WikiPath, "systems"), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(c.WikiPath, "systems", slug+".md"), []byte(page), 0o644); err != nil {
		t.Fatal(err)
	}
}

// sharedFile writes by hand a system page that lists two repos, which the
// repos index lists.
func sharedFile(t *testing.T, c config.Config) {
	t.Helper()
	page := "---\ntype: system\ntitle: Billing\ndescription: Billing in short\nstatus: stable\naliases: []\n" +
		"repos: [\"github.com__acme__billing\", \"github.com__acme__ledger\"]\nas_of: \"2026-10-30\"\nstate: active\n---\n\n## Summary\nBilling.\n"
	if err := os.MkdirAll(filepath.Join(c.WikiPath, "systems"), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(c.WikiPath, "systems", "billing.md"), []byte(page), 0o644); err != nil {
		t.Fatal(err)
	}
}

// namesCheck runs the doctor's checks of the wiki and returns the names
// check, if any.
func namesCheck(t *testing.T, d paths.Dirs, c config.Config) (Check, bool) {
	t.Helper()
	var r Report
	inspectWiki(context.Background(), &r, d, c, true)
	for _, check := range r.Checks {
		if check.Name == "system names" {
			return check, true
		}
	}
	return Check{}, false
}

// The doctor checks the names index once there are system pages, writes it
// again when it is missing or damaged and says so, and fails, naming the
// fix, when it can't.
func TestDoctorWritesTheNamesIndexAgain(t *testing.T) {
	d, c := namesWiki(t)
	if check, found := namesCheck(t, d, c); found {
		t.Fatalf("a wiki without system pages got %+v", check)
	}
	// An index that would list nothing, as one left after the last system
	// page was removed by hand, is removed, and the row says so.
	index := filepath.Join(c.WikiPath, filepath.FromSlash(systempage.NamesFile))
	repos := filepath.Join(c.WikiPath, filepath.FromSlash(systempage.ReposFile))
	if err := os.MkdirAll(filepath.Dir(index), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(index, []byte("Orders\tsystems/orders\tOrders\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if check, found := namesCheck(t, d, c); !found || check != (Check{"system names", "PASS", "0 names for 0 system pages; the names index was removed, since it would list no name", ""}) {
		t.Fatalf("a names index with no system page: %+v", check)
	}
	if _, err := os.Lstat(index); !errors.Is(err, fs.ErrNotExist) {
		t.Fatalf("the names index is still there: %v", err)
	}
	if check, found := namesCheck(t, d, c); found {
		t.Fatalf("a wiki without system pages got %+v once its index was removed", check)
	}
	systemFile(t, c, "orders", "Orders service", "2026-10-30", "order service")
	systemFile(t, c, "payments", "Payments", "2026-10-29")
	if err := os.WriteFile(repos, nil, 0o600); err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		name   string
		damage func() error
		detail string
	}{
		{"missing", func() error { return nil }, "written again from the system pages: 5 names for 2 system pages; the repos index was removed, since it would list no page"},
		{"current", func() error { return nil }, "5 names for 2 system pages"},
		{"damaged", func() error { return os.WriteFile(index, []byte("orders\x00\n"), 0o600) }, "written again from the system pages: 5 names for 2 system pages"},
	} {
		if err := tc.damage(); err != nil {
			t.Fatal(err)
		}
		if check, found := namesCheck(t, d, c); !found || check.Status != "PASS" || check.Detail != tc.detail {
			t.Fatalf("%s: %+v", tc.name, check)
		}
	}
	if data, err := os.ReadFile(index); err != nil || !strings.HasPrefix(string(data), "Orders service\tsystems/orders\tOrders service\n") {
		t.Fatalf("index %q %v", data, err)
	}
	if err := os.Remove(index); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(index, 0o700); err != nil {
		t.Fatal(err)
	}
	if check, found := namesCheck(t, d, c); !found || check != (Check{"system names", "FAIL", "the names index of the system pages can't be checked or written again",
		"check that " + systempage.NamesFile + " in the wiki is a regular file eva-md can write, or remove it, then run eva-md doctor again"}) {
		t.Fatalf("an index that can't be written: %+v", check)
	}
	if err := os.Remove(index); err != nil {
		t.Fatal(err)
	}
	// A repos index that can't be written is named alone.
	sharedFile(t, c)
	if check, found := namesCheck(t, d, c); !found || check.Status != "PASS" {
		t.Fatalf("a page that lists two repos: %+v", check)
	}
	if err := os.Remove(repos); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(repos, 0o700); err != nil {
		t.Fatal(err)
	}
	if check, found := namesCheck(t, d, c); !found || check.Status != "FAIL" || check.Detail != "the repos index of the system pages can't be checked or written again" ||
		!strings.Contains(check.Fix, systempage.ReposFile) || strings.Contains(check.Fix, systempage.NamesFile) {
		t.Fatalf("a repos index that can't be written: %+v", check)
	}
	if err := os.Remove(repos); err != nil {
		t.Fatal(err)
	}
	if check, found := namesCheck(t, d, c); !found || check.Status != "PASS" {
		t.Fatalf("the repos index written again: %+v", check)
	}
	// A wiki of another instance is never written to.
	c.Instance = "other"
	if check, found := namesCheck(t, d, c); found {
		t.Fatalf("another instance's wiki got %+v", check)
	}
}

// eva-md status says when the names index is missing or out of date, naming
// the doctor, which writes it again, and never writes it itself.
func TestStatusLeavesTheNamesIndex(t *testing.T) {
	d, c := namesWiki(t)
	if err := config.Save(d.ConfigFile(), c); err != nil {
		t.Fatal(err)
	}
	systemFile(t, c, "orders", "Orders service", "2026-10-30", "order service")
	index := filepath.Join(c.WikiPath, filepath.FromSlash(systempage.NamesFile))
	if row := findRow(t, Status(t.Context(), d), "system names"); row.Status != "FAIL" || row.Detail != "the names index of the 1 system page is missing, unreadable or out of date, so prompts may miss pointers to them" || row.Fix != "run eva-md doctor, which writes it again" {
		t.Fatalf("status: %+v", row)
	}
	if _, err := os.Lstat(index); !errors.Is(err, fs.ErrNotExist) {
		t.Fatalf("status wrote the index: %v", err)
	}
	if row := findRow(t, Inspect(t.Context(), d, false), "system names"); row.Status != "PASS" || row.Detail != "written again from the system pages: 3 names for 1 system page" {
		t.Fatalf("doctor: %+v", row)
	}
	if row := findRow(t, Status(t.Context(), d), "system names"); row.Status != "PASS" || row.Detail != "3 names for 1 system page" {
		t.Fatalf("status after the doctor: %+v", row)
	}
}

// Status tells the repos index from the names index: when only the repos
// index is missing, as in a wiki an older eva-md wrote, it names that index
// and what recall may show or leave out, never the pointers; the doctor's
// row says which index it wrote again.
func TestStatusNamesTheReposIndex(t *testing.T) {
	d, c := namesWiki(t)
	if err := config.Save(d.ConfigFile(), c); err != nil {
		t.Fatal(err)
	}
	sharedFile(t, c)
	names := filepath.Join(c.WikiPath, filepath.FromSlash(systempage.NamesFile))
	repos := filepath.Join(c.WikiPath, filepath.FromSlash(systempage.ReposFile))
	for _, tc := range []struct {
		name, status, fix, doctor string
		gone                      []string
	}{
		{"both", "the names and repos indexes of the 1 system page are missing, unreadable or out of date, so prompts may miss pointers to them, and recall may show, or leave out, the knowledge of other projects",
			"run eva-md doctor, which writes them again", "written again from the system pages: 2 names for 1 system page; so was the repos index", nil},
		{"repos", "the repos index of the 1 system page is missing, unreadable or out of date, so recall may show, or leave out, the knowledge of other projects",
			"run eva-md doctor, which writes it again", "2 names for 1 system page; the repos index was written again from them", []string{repos}},
		{"names", "the names index of the 1 system page is missing, unreadable or out of date, so prompts may miss pointers to them",
			"run eva-md doctor, which writes it again", "written again from the system pages: 2 names for 1 system page", []string{names}},
	} {
		for _, file := range tc.gone {
			if err := os.Remove(file); err != nil {
				t.Fatal(err)
			}
		}
		if row := findRow(t, Status(t.Context(), d), "system names"); row != (Check{"system names", "FAIL", tc.status, tc.fix}) {
			t.Fatalf("%s: status %+v", tc.name, row)
		}
		if row := findRow(t, Inspect(t.Context(), d, false), "system names"); row != (Check{"system names", "PASS", tc.doctor, ""}) {
			t.Fatalf("%s: doctor %+v", tc.name, row)
		}
	}
}

// A doctor that finds the wiki's lock busy, as a long update or commit may
// hold it, says which index was not written again, or not removed when it
// would list nothing, and to run it again; status, given the same report,
// words each index the same way, with what it costs and what the doctor
// does, also when one index is written again and the other removed.
func TestDoctorWithTheWikiBusy(t *testing.T) {
	d, c := namesWiki(t)
	systemFile(t, c, "orders", "Orders service", "2026-10-30")
	t.Cleanup(func() { refreshNames = systempage.RefreshNames })
	const (
		namesLeft = "no system page is left, but the names index is still there"
		reposLeft = "no system page lists two repos any more, but the repos index is still there"
		reposCost = "recall may show the knowledge of projects that no longer share a system page"
	)
	for _, tc := range []struct {
		report      systempage.NamesReport
		detail      string // the busy doctor's
		status, fix string
	}{
		{systempage.NamesReport{Pages: 2, NamesStale: true}, "the names index of the system pages is missing, unreadable or out of date, and the wiki stayed busy, so it was not written again",
			"the names index of the 2 system pages is missing, unreadable or out of date, so prompts may miss pointers to them", "run eva-md doctor, which writes it again"},
		{systempage.NamesReport{Pages: 2, ReposStale: true}, "the repos index of the system pages is missing, unreadable or out of date, and the wiki stayed busy, so it was not written again",
			"the repos index of the 2 system pages is missing, unreadable or out of date, so recall may show, or leave out, the knowledge of other projects", "run eva-md doctor, which writes it again"},
		{systempage.NamesReport{Pages: 2, NamesStale: true, ReposStale: true}, "the names and repos indexes of the system pages are missing, unreadable or out of date, and the wiki stayed busy, so neither was written again",
			"the names and repos indexes of the 2 system pages are missing, unreadable or out of date, so prompts may miss pointers to them, and recall may show, or leave out, the knowledge of other projects", "run eva-md doctor, which writes them again"},
		{systempage.NamesReport{NamesStale: true, NamesGone: true}, namesLeft + ", and the wiki stayed busy, so it was not removed",
			namesLeft + ", so the memory reminder may still name the system index", "run eva-md doctor, which removes it"},
		{systempage.NamesReport{Pages: 2, ReposStale: true, ReposGone: true}, reposLeft + ", and the wiki stayed busy, so it was not removed",
			reposLeft + ", so " + reposCost, "run eva-md doctor, which removes it"},
		{systempage.NamesReport{NamesStale: true, NamesGone: true, ReposStale: true, ReposGone: true},
			"no system page is left, but the names and repos indexes are still there, and the wiki stayed busy, so neither was removed",
			"no system page is left, but the names and repos indexes are still there, so the memory reminder may still name the system index, and " + reposCost, "run eva-md doctor, which removes them"},
		{systempage.NamesReport{Pages: 2, NamesStale: true, ReposStale: true, ReposGone: true},
			"the names index of the system pages is missing, unreadable or out of date; " + reposLeft + "; the wiki stayed busy, so the names index was not written again, nor the repos index removed",
			"the names index of the 2 system pages is missing, unreadable or out of date, so prompts may miss pointers to them; " + reposLeft + ", so " + reposCost,
			"run eva-md doctor, which writes the names index again and removes the repos index"},
		// Past the names index's bounds, a page's names alone can be too
		// long to list: the index is then out of date, and removed.
		{systempage.NamesReport{Pages: 1, NamesStale: true, NamesGone: true}, "the names index of the system pages is missing, unreadable or out of date, and the wiki stayed busy, so it was not removed",
			"the names index of the 1 system page is missing, unreadable or out of date, so prompts may miss pointers to them", "run eva-md doctor, which removes it"},
		{systempage.NamesReport{Pages: 1, NamesStale: true, NamesGone: true, ReposStale: true},
			"the names index of the system pages is missing, unreadable or out of date; the repos index of the system pages is missing, unreadable or out of date; the wiki stayed busy, so the names index was not removed, nor the repos index written again",
			"the names index of the 1 system page is missing, unreadable or out of date, so prompts may miss pointers to them; the repos index of the 1 system page is missing, unreadable or out of date, so recall may show, or leave out, the knowledge of other projects",
			"run eva-md doctor, which removes the names index and writes the repos index again"},
	} {
		var err error
		refreshNames = func(context.Context, systempage.Options, bool) (systempage.NamesReport, error) { return tc.report, err }
		err = fmt.Errorf("%w after 1m0s", okfadapter.ErrLockBusy)
		if check, found := namesCheck(t, d, c); !found || check != (Check{"system names", "UNKNOWN", tc.detail, "run eva-md doctor again"}) {
			t.Fatalf("%+v: %+v", tc.report, check)
		}
		err = nil
		if check, found := namesCheck(t, d, c); !found || check != (Check{"system names", "FAIL", tc.status, tc.fix}) {
			t.Fatalf("%+v, as status finds it: %+v", tc.report, check)
		}
	}
}

// When an index would list nothing, as the names index once no system page
// is left, or the repos index once no page lists two repos, status says so,
// with what it costs, and names the doctor, which removes it rather than
// writes it again; status leaves it as it is, and the doctor's row says it
// removed it. Each index is worded on its own, also beside the other when
// that one is written again.
func TestStatusOfAnIndexTheDoctorRemoves(t *testing.T) {
	d, c := namesWiki(t)
	if err := config.Save(d.ConfigFile(), c); err != nil {
		t.Fatal(err)
	}
	names := filepath.Join(c.WikiPath, filepath.FromSlash(systempage.NamesFile))
	repos := filepath.Join(c.WikiPath, filepath.FromSlash(systempage.ReposFile))
	billing := filepath.Join(c.WikiPath, "systems", "billing.md")
	const (
		staleNames = "Orders\tsystems/orders\tOrders\n"
		staleRepos = "# eva-md repos index 1\nsystems/billing\tgithub.com__acme__billing\tgithub.com__acme__ledger\n"
		namesLeft  = "no system page is left, but the names index is still there"
		reposLeft  = "no system page lists two repos any more, but the repos index is still there, so recall may show the knowledge of projects that no longer share a system page"
	)
	for _, tc := range []struct {
		name        string
		set         func() // leaves the wiki as a hand edit would
		left        []string
		status, fix string
		doctor      string
	}{
		{"the last system page removed", func() { put(t, names, []byte(staleNames)) }, []string{names},
			namesLeft + ", so the memory reminder may still name the system index", "run eva-md doctor, which removes it",
			"0 names for 0 system pages; the names index was removed, since it would list no name"},
		{"both indexes left after the last page", func() {
			put(t, names, []byte(staleNames))
			put(t, repos, []byte(staleRepos))
		}, []string{names, repos},
			"no system page is left, but the names and repos indexes are still there, so the memory reminder may still name the system index, and recall may show the knowledge of projects that no longer share a system page",
			"run eva-md doctor, which removes them",
			"0 names for 0 system pages; the names index was removed, since it would list no name; the repos index was removed, since it would list no page"},
		{"the last shared page edited to list one repo", func() {
			sharedFile(t, c)
			if row := findRow(t, Inspect(t.Context(), d, false), "system names"); row.Status != "PASS" {
				t.Fatalf("doctor: %+v", row)
			}
			data, err := os.ReadFile(billing)
			one := strings.Replace(string(data), `repos: ["github.com__acme__billing", "github.com__acme__ledger"]`, `repos: ["github.com__acme__billing"]`, 1)
			if err != nil || one == string(data) {
				t.Fatalf("the page still lists two repos: %v", err)
			}
			if err := os.WriteFile(billing, []byte(one), 0o644); err != nil {
				t.Fatal(err)
			}
		}, []string{repos},
			reposLeft, "run eva-md doctor, which removes it",
			"2 names for 1 system page; the repos index was removed, since it would list no page"},
		{"and the names index missing", func() {
			put(t, repos, []byte(staleRepos))
			if err := os.Remove(names); err != nil {
				t.Fatal(err)
			}
		}, []string{repos},
			"the names index of the 1 system page is missing, unreadable or out of date, so prompts may miss pointers to them; " + reposLeft,
			"run eva-md doctor, which writes the names index again and removes the repos index",
			"written again from the system pages: 2 names for 1 system page; the repos index was removed, since it would list no page"},
	} {
		tc.set()
		before := map[string]string{}
		for _, file := range tc.left {
			data, err := os.ReadFile(file)
			if err != nil {
				t.Fatalf("%s: %v", tc.name, err)
			}
			before[file] = string(data)
		}
		if row := findRow(t, Status(t.Context(), d), "system names"); row != (Check{"system names", "FAIL", tc.status, tc.fix}) {
			t.Fatalf("%s: status %+v", tc.name, row)
		}
		for file, text := range before {
			if data, err := os.ReadFile(file); err != nil || string(data) != text {
				t.Fatalf("%s: status changed %s: %q %v", tc.name, filepath.Base(file), data, err)
			}
		}
		if row := findRow(t, Inspect(t.Context(), d, false), "system names"); row != (Check{"system names", "PASS", tc.doctor, ""}) {
			t.Fatalf("%s: doctor %+v", tc.name, row)
		}
		for _, file := range tc.left {
			if _, err := os.Lstat(file); !errors.Is(err, fs.ErrNotExist) {
				t.Fatalf("%s: the doctor left %s: %v", tc.name, filepath.Base(file), err)
			}
		}
	}
}

// Past the index's bounds, the doctor says how many of the oldest pages lost
// their aliases.
func TestDoctorSaysWhatTheBoundsLeaveOut(t *testing.T) {
	d, c := namesWiki(t)
	for i := range 120 {
		var aliases []string
		for a := range 10 {
			aliases = append(aliases, fmt.Sprintf("alias %03d %02d %s", i, a, strings.Repeat("y", 90)))
		}
		systemFile(t, c, fmt.Sprintf("system-%03d", i), fmt.Sprintf("System %03d", i), fmt.Sprintf("2026-%02d-01", 1+i%12), aliases...)
	}
	check, found := namesCheck(t, d, c)
	if !found || check.Status != "PASS" || !strings.Contains(check.Detail, " for 120 system pages; past 2000 names or 128 KB, the aliases of the ") || !strings.HasSuffix(check.Detail, " oldest pages are left out") {
		t.Fatalf("%+v", check)
	}
}
