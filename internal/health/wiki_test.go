package health

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"github.com/heyNag/eva-md/internal/config"
	"github.com/heyNag/eva-md/internal/wikigit"
)

func TestWikiRemoteAcceptsInitializedCanonicalIdentity(t *testing.T) {
	d := isolated(t)
	wiki := filepath.Join(t.TempDir(), "wiki")
	for _, args := range [][]string{{"init", "-q", wiki}, {"-C", wiki, "remote", "add", "origin", "git@github.com:example/wiki.git"}} {
		if output, err := exec.Command("git", args...).CombinedOutput(); err != nil {
			t.Fatalf("fixture git: %v %s", err, output)
		}
	}
	var err error
	wiki, err = filepath.EvalSymlinks(wiki)
	if err != nil {
		t.Fatal(err)
	}
	put(t, filepath.Join(wiki, "index.md"), []byte("---\nokf_version: \"0.2\"\n---\n# Wiki\n"))
	if e := config.SaveSchema(filepath.Join(wiki, ".eva-md", "schema.toml"), config.WikiSchema{SchemaVersion: 1, Instance: "test", Created: "2026-10-29"}); e != nil {
		t.Fatal(e)
	}
	for _, expected := range []string{"github.com/example/wiki", "git@github.com:example/wiki.git", "https://github.com/example/wiki.git", "github.com/example/other"} {
		c := config.Default()
		c.WikiPath = wiki
		c.ExpectedRemote = expected
		c.Instance = "test"
		var report Report
		inspectWiki(context.Background(), &report, d, c, true)
		found := false
		for _, check := range report.Checks {
			if check.Name == "wiki remote" {
				found = true
				want := "PASS"
				if expected == "github.com/example/other" {
					want = "FAIL"
				}
				if check.Status != want {
					t.Fatalf("%s: %+v", expected, check)
				}
			}
		}
		if !found {
			t.Fatal("remote check missing")
		}
	}
}

// A new wiki's folder is 0700, so only this account can open it; one
// adopted from a clone made by hand keeps git's 0755, which the doctor fails
// with the fix that closes it, the folder quoted as a shell takes it, and so
// is any access for the group or other accounts.
func TestWikiPermissionsRow(t *testing.T) {
	d := isolated(t)
	dir, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	wiki := filepath.Join(dir, "my wiki")
	if err := os.Mkdir(wiki, 0o700); err != nil {
		t.Fatal(err)
	}
	c := config.Default()
	c.WikiPath, c.ExpectedRemote, c.Instance = wiki, "github.com/example/wiki", "test"
	for _, tc := range []struct {
		mode           os.FileMode
		status, detail string
	}{
		{0o755, "FAIL", "the wiki folder is 0755, so other accounts on this Mac can read it"},
		{0o711, "FAIL", "the wiki folder is 0711, so other accounts on this Mac can read it"},
		{0o750, "FAIL", "the wiki folder is 0750, so other accounts on this Mac can read it"},
		{0o700, "PASS", "only your account can open the wiki folder"},
	} {
		if err := os.Chmod(wiki, tc.mode); err != nil {
			t.Fatal(err)
		}
		var report Report
		inspectWiki(context.Background(), &report, d, c, false)
		var row *Check
		for i := range report.Checks {
			if report.Checks[i].Name == "wiki permissions" {
				row = &report.Checks[i]
			}
		}
		fix := ""
		if tc.status == "FAIL" {
			fix = "run chmod 700 '" + wiki + "'"
		}
		if row == nil || row.Status != tc.status || row.Detail != tc.detail || row.Fix != fix {
			t.Fatalf("mode %04o: %+v", tc.mode, row)
		}
	}
}

// A wiki whose remote is a local path, which setup and init accept, passes
// the remote check as the daily commit's guard passes it: init saves its
// identity as a file:// URL. A push URL elsewhere fails it.
func TestWikiRemoteAcceptsALocalRemote(t *testing.T) {
	d := isolated(t)
	dir, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	wiki, bare := filepath.Join(dir, "wiki"), filepath.Join(dir, "remote.git")
	git := func(args ...string) {
		t.Helper()
		if output, err := exec.Command("git", args...).CombinedOutput(); err != nil {
			t.Fatalf("fixture git: %v %s", err, output)
		}
	}
	git("init", "-q", "--bare", bare)
	git("init", "-q", wiki)
	git("-C", wiki, "remote", "add", "origin", bare)
	put(t, filepath.Join(wiki, "index.md"), []byte("---\nokf_version: \"0.2\"\n---\n# Wiki\n"))
	if e := config.SaveSchema(filepath.Join(wiki, ".eva-md", "schema.toml"), config.WikiSchema{SchemaVersion: 1, Instance: "test", Created: "2026-10-29"}); e != nil {
		t.Fatal(e)
	}
	_, expected, err := wikigit.RemoteIdentity(bare)
	if err != nil {
		t.Fatal(err)
	}
	remoteCheck := func() Check {
		t.Helper()
		c := config.Default()
		c.WikiPath, c.ExpectedRemote, c.Instance = wiki, expected, "test"
		var report Report
		inspectWiki(context.Background(), &report, d, c, true)
		for _, check := range report.Checks {
			if check.Name == "wiki remote" {
				return check
			}
		}
		t.Fatal("remote check missing")
		return Check{}
	}
	if check := remoteCheck(); check.Status != "PASS" {
		t.Fatalf("local remote %s: %+v", expected, check)
	}
	git("-C", wiki, "remote", "set-url", "--push", "origin", "git@github.com:example/elsewhere.git")
	if check := remoteCheck(); check.Status != "FAIL" {
		t.Fatalf("push URL elsewhere: %+v", check)
	}
}
