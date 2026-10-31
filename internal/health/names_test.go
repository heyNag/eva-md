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
	if err := config.SaveSchema(filepath.Join(wiki, ".eva-md", "schema.toml"), config.WikiSchema{SchemaVersion: 1, OKFSpec: "0.2", OKFModule: "v0.7.2", Instance: "test", Created: "2026-10-29"}); err != nil {
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

// namesCheck runs the doctor's checks of the wiki and returns the names
// check, if any.
func namesCheck(t *testing.T, d paths.Dirs, c config.Config) (Check, bool) {
	t.Helper()
	var r Report
	inspectWiki(context.Background(), &r, d, c, "v0.7.2", true)
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
	systemFile(t, c, "orders", "Orders service", "2026-10-30", "order service")
	systemFile(t, c, "payments", "Payments", "2026-10-29")
	index := filepath.Join(c.WikiPath, filepath.FromSlash(systempage.NamesFile))
	for _, tc := range []struct {
		name   string
		damage func() error
		detail string
	}{
		{"missing", func() error { return nil }, "written again from the system pages: 5 names for 2 system pages"},
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
	if check, found := namesCheck(t, d, c); !found || check.Status != "FAIL" || !strings.Contains(check.Fix, systempage.NamesFile) {
		t.Fatalf("an index that can't be written: %+v", check)
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
	if row := findRow(t, Status(t.Context(), d, "v0.7.2"), "system names"); row.Status != "FAIL" || row.Detail != "the names index of the 1 system page is missing, unreadable or out of date, so prompts may miss pointers to them" || row.Fix != "run eva-md doctor, which writes it again" {
		t.Fatalf("status: %+v", row)
	}
	if _, err := os.Lstat(index); !errors.Is(err, fs.ErrNotExist) {
		t.Fatalf("status wrote the index: %v", err)
	}
	if row := findRow(t, Inspect(t.Context(), d, "v0.7.2", false), "system names"); row.Status != "PASS" || row.Detail != "written again from the system pages: 3 names for 1 system page" {
		t.Fatalf("doctor: %+v", row)
	}
	if row := findRow(t, Status(t.Context(), d, "v0.7.2"), "system names"); row.Status != "PASS" || row.Detail != "3 names for 1 system page" {
		t.Fatalf("status after the doctor: %+v", row)
	}
}

// A doctor that finds the wiki's lock busy, as a long update or commit may
// hold it, says the index was not written again and to run it again.
func TestDoctorWithTheWikiBusy(t *testing.T) {
	d, c := namesWiki(t)
	systemFile(t, c, "orders", "Orders service", "2026-10-30")
	refreshNames = func(context.Context, systempage.Options, bool) (systempage.NamesReport, error) {
		return systempage.NamesReport{}, fmt.Errorf("%w after 1m0s", okfadapter.ErrLockBusy)
	}
	t.Cleanup(func() { refreshNames = systempage.RefreshNames })
	if check, found := namesCheck(t, d, c); !found || check.Status != "UNKNOWN" || !strings.Contains(check.Detail, "the wiki stayed busy") || check.Fix != "run eva-md doctor again" {
		t.Fatalf("%+v", check)
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
