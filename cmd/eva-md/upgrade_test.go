package main

import (
	"bytes"
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"github.com/heyNag/eva-md/internal/release"
)

// releaseServer stands in for GitHub: releases/latest redirects to the tag
// of m's latest file, and a download redirects to an asset path that serves
// the stand-in's file, as github.com sends downloads to its asset host. A
// path in fail answers with that status. It returns what was asked for.
func (m *fakeMac) releaseServer(t *testing.T, fail map[string]int) func() []string {
	t.Helper()
	var mu sync.Mutex
	var asked []string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		asked = append(asked, r.URL.Path)
		mu.Unlock()
		if status := fail[r.URL.Path]; status != 0 {
			w.WriteHeader(status)
			return
		}
		switch path := r.URL.Path; {
		case path == "/heyNag/eva-md/releases/latest":
			latest, err := os.ReadFile(filepath.Join(m.root, "latest"))
			if err != nil {
				http.NotFound(w, r)
				return
			}
			http.Redirect(w, r, "/heyNag/eva-md/releases/tag/"+strings.TrimSpace(string(latest)), http.StatusFound)
		case strings.HasPrefix(path, "/heyNag/eva-md/releases/download/"):
			http.Redirect(w, r, "/assets/"+strings.TrimPrefix(path, "/heyNag/eva-md/releases/download/"), http.StatusFound)
		case strings.HasPrefix(path, "/assets/"):
			http.ServeFile(w, r, filepath.Join(m.root, "releases", filepath.FromSlash(strings.TrimPrefix(path, "/assets/"))))
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(srv.Close)
	saved := release.Base
	release.Base = srv.URL + "/heyNag/eva-md"
	t.Cleanup(func() { release.Base = saved })
	return func() []string {
		mu.Lock()
		defer mu.Unlock()
		return append([]string(nil), asked...)
	}
}

// asVersion makes this binary eva-md v for one test.
func asVersion(t *testing.T, v string) {
	t.Helper()
	saved := version
	version = v
	t.Cleanup(func() { version = saved })
}

func upgrade(args ...string) (int, string, string) {
	var out, errOut bytes.Buffer
	code := run(context.Background(), append([]string{"upgrade"}, args...), strings.NewReader(""), &out, &errOut)
	return code, out.String(), errOut.String()
}

// downloads lists the assets asked for.
func downloads(asked []string) []string {
	var d []string
	for _, p := range asked {
		if strings.Contains(p, "/releases/download/") {
			d = append(d, p)
		}
	}
	return d
}

// The latest release, or --version, the same as this binary is up to date,
// with nothing downloaded; an older one needs --force; --check only says
// whether a newer release exists. A source build of the release's day, and
// dev, are replaced only with --force.
func TestUpgradeSaysWhenUpToDate(t *testing.T) {
	m := newFakeMac(t)
	m.publish(t, "2026.11.2", "2026.11.2")
	m.publish(t, "2026.11.3", "2026.11.3")
	asked := m.releaseServer(t, nil)
	for _, c := range []struct {
		current string
		args    []string
		want    string
	}{
		{"2026.11.3", nil, "eva-md 2026.11.3 is up to date\n"},
		{"2026.11.3", []string{"--check"}, "eva-md 2026.11.3 is up to date\n"},
		{"2026.11.3", []string{"--version", "2026.11.2"}, "eva-md 2026.11.3 is up to date; 2026.11.2 is older, and eva-md upgrade --version 2026.11.2 --force installs it.\n"},
		{"2026.11.2", []string{"--check"}, "eva-md 2026.11.3 is available; this is 2026.11.2. Run eva-md upgrade to install it.\n"},
		{"2026.11.2", []string{"--check", "--force"}, "eva-md 2026.11.3 is available; this is 2026.11.2."},
		{"2026.11.3+0915.9e4f609", nil, "eva-md 2026.11.3+0915.9e4f609 is a source build of the same day as 2026.11.3 or later; eva-md upgrade --force replaces it with 2026.11.3.\n"},
		{"2026.11.2+2359.9e4f609.dirty", []string{"--check"}, "eva-md 2026.11.3 is available; this is 2026.11.2+2359.9e4f609.dirty."},
		{"dev", nil, "eva-md dev has no release version; eva-md upgrade --force replaces it with 2026.11.3.\n"},
	} {
		asVersion(t, c.current)
		code, out, errOut := upgrade(c.args...)
		if code != 0 || !strings.Contains(out, c.want) || errOut != "" {
			t.Errorf("%s %v: code %d\nstdout %q\nstderr %q", c.current, c.args, code, out, errOut)
		}
	}
	if d := downloads(asked()); len(d) != 0 {
		t.Fatalf("downloaded %v", d)
	}
	for _, args := range [][]string{{"--version", "v2026.11.3"}, {"--version", "latest"}, {"extra"}} {
		if code, _, errOut := upgrade(args...); code != 2 || !strings.Contains(errOut, "upgrade") {
			t.Errorf("%v: code %d %q", args, code, errOut)
		}
	}
	if _, err := os.Stat(m.dirs.Share); !os.IsNotExist(err) {
		t.Fatalf("the data folder was made: %v", err)
	}
}

// A newer release is downloaded with SHA256SUMS, checked, and its setup
// run: on a Mac that is set up, it keeps the wiki and replaces the stable
// binary, through the fake hosts. The temporary folder goes.
func TestUpgradeInstallsTheNewRelease(t *testing.T) {
	m := newFakeMac(t)
	m.publish(t, "2026.11.2", "2026.11.2")
	m.publish(t, "2026.11.3", "2026.11.3")
	m.installed(t, "2026.11.2")
	wiki := tree(t, m.wiki, m.remote)
	asked := m.releaseServer(t, nil)
	asVersion(t, "2026.11.2")
	code, out, errOut := upgrade()
	if code != 0 {
		t.Fatalf("code %d\n%s\n%s", code, out, errOut)
	}
	says(t, "stdout", out, "Upgrading to eva-md 2026.11.3 for "+upgradeArch()+"\nChecksum verified\n", "eva-md is set up here", "replace stable binary")
	if v := m.stableVersion(t); v != "eva-md 2026.11.3" {
		t.Fatalf("installed %q", v)
	}
	m.wantRegistered(t, true)
	if d := downloads(asked()); len(d) != 2 || !strings.HasSuffix(d[0], "/2026.11.3/"+release.Asset(upgradeArch())) || !strings.HasSuffix(d[1], "/2026.11.3/SHA256SUMS") {
		t.Fatalf("downloaded %v", d)
	}
	if left, _ := filepath.Glob(filepath.Join(m.dirs.Share, ".upgrade-*")); len(left) != 0 {
		t.Fatalf("left %v", left)
	}
	if tree(t, m.wiki, m.remote) != wiki {
		t.Fatal("the wiki or its remote changed")
	}
	// A source build of the same day needs --force, which installs the
	// release over it.
	m.publish(t, "2026.11.3.2", "2026.11.3.2")
	asVersion(t, "2026.11.3+2359.9e4f609")
	if code, out, _ := upgrade(); code != 0 || !strings.Contains(out, "--force") || m.stableVersion(t) != "eva-md 2026.11.3" {
		t.Fatalf("unforced: code %d\n%s", code, out)
	}
	if code, out, errOut := upgrade("--force"); code != 0 || m.stableVersion(t) != "eva-md 2026.11.3.2" {
		t.Fatalf("forced: code %d\n%s\n%s", code, out, errOut)
	}
}

// A checksum that differs, a download or lookup that fails, and a binary
// that says it is another version each stop the upgrade before setup runs:
// the stable binary, the plugins, the record and the link stay as they
// were, and no temporary folder is left.
func TestUpgradeFailuresChangeNothing(t *testing.T) {
	asset := release.Asset(upgradeArch())
	for _, c := range []struct {
		name string
		fail map[string]int
		sums bool
		err  string
	}{
		{name: "checksum", sums: true, err: "the checksum of " + asset + " doesn't match SHA256SUMS"},
		{name: "download", fail: map[string]int{"/heyNag/eva-md/releases/download/2026.11.3/" + asset: http.StatusInternalServerError}, err: "couldn't download " + asset + " 2026.11.3: GitHub answered 500"},
		{name: "sums", fail: map[string]int{"/assets/2026.11.3/SHA256SUMS": http.StatusBadGateway}, err: "couldn't download SHA256SUMS 2026.11.3: GitHub answered 502"},
		{name: "latest", fail: map[string]int{"/heyNag/eva-md/releases/latest": http.StatusServiceUnavailable}, err: "GitHub answered 503"},
		{name: "version", err: "the downloaded binary says it is eva-md 2026.11.2, not 2026.11.3"},
	} {
		t.Run(c.name, func(t *testing.T) {
			m := newFakeMac(t)
			m.publish(t, "2026.11.2", "2026.11.2")
			builtAs := "2026.11.3"
			if c.name == "version" {
				builtAs = "2026.11.2"
			}
			dir := m.publish(t, "2026.11.3", builtAs)
			if c.sums {
				if err := os.WriteFile(filepath.Join(dir, "SHA256SUMS"), []byte(strings.Repeat("0", 64)+"  "+asset+"\n"), 0o600); err != nil {
					t.Fatal(err)
				}
			}
			m.installed(t, "2026.11.2")
			before := tree(t, m.dirs.Share, m.dirs.Config, m.hosts, m.link(), m.wiki, m.remote)
			m.releaseServer(t, c.fail)
			asVersion(t, "2026.11.2")
			code, out, errOut := upgrade()
			if code != 1 || !strings.Contains(errOut, c.err) || strings.Contains(out, "eva.md setup") {
				t.Fatalf("code %d\nstdout %s\nstderr %s", code, out, errOut)
			}
			if !strings.Contains(errOut, "nothing was changed") && !strings.Contains(errOut, "the installed eva-md and its plugins are as they were") {
				t.Fatalf("stderr %s", errOut)
			}
			if after := tree(t, m.dirs.Share, m.dirs.Config, m.hosts, m.link(), m.wiki, m.remote); after != before {
				t.Fatalf("changed:\n%s\nwas:\n%s", after, before)
			}
		})
	}
}

// When the new binary's setup stops after its install put the release in
// place, eva-md upgrade would now say it is up to date, so the closing line
// names setup, which finishes once the problem is fixed. Here setup can't
// set codex_ignore_user_config, as config.env asks, in a config with a key
// eva-md doesn't know.
func TestUpgradeSetupStoppedAfterItsInstall(t *testing.T) {
	m := newFakeMac(t)
	m.publish(t, "2026.11.2", "2026.11.2")
	m.publish(t, "2026.11.3", "2026.11.3")
	m.installed(t, "2026.11.2")
	wiki := tree(t, m.wiki, m.remote)
	if err := os.WriteFile(m.dirs.AnswersFile(), []byte("EVA_MD_CODEX_IGNORE_USER_CONFIG=true\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	config, err := os.ReadFile(m.dirs.ConfigFile())
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(m.dirs.ConfigFile(), append(append([]byte{}, config...), "mystery = 1\n"...), 0o600); err != nil {
		t.Fatal(err)
	}
	m.releaseServer(t, nil)
	asVersion(t, "2026.11.2")
	code, out, errOut := upgrade()
	if code != 1 {
		t.Fatalf("code %d\n%s\n%s", code, out, errOut)
	}
	says(t, "stderr", errOut, "couldn't set codex_ignore_user_config",
		"eva-md: setup stopped after installing eva-md 2026.11.3; when it named a problem, fix it, then run eva-md setup to finish\n")
	if v := m.stableVersion(t); v != "eva-md 2026.11.3" {
		t.Fatalf("installed %q", v)
	}
	asVersion(t, "2026.11.3")
	if _, out, _ := upgrade(); out != "eva-md 2026.11.3 is up to date\n" {
		t.Fatalf("upgrade again: %q", out)
	}
	if err := os.WriteFile(m.dirs.ConfigFile(), config, 0o600); err != nil {
		t.Fatal(err)
	}
	if code, out, errOut := m.exec(t, m.link(), "setup"); code != 0 {
		t.Fatalf("setup: code %d\n%s\n%s", code, out, errOut)
	}
	if b, _ := os.ReadFile(m.dirs.ConfigFile()); !strings.Contains(string(b), "codex_ignore_user_config = true") {
		t.Fatalf("the config:\n%s", b)
	}
	if tree(t, m.wiki, m.remote) != wiki {
		t.Fatal("the wiki or its remote changed")
	}
}

// A setup that stops before its install leaves this binary in place, so the
// closing line says to run eva-md upgrade again.
func TestUpgradeSetupStoppedBeforeItsInstall(t *testing.T) {
	m := newFakeMac(t)
	m.publish(t, "2026.11.2", "2026.11.2")
	m.publish(t, "2026.11.3", "2026.11.3")
	m.installed(t, "2026.11.2")
	if err := os.WriteFile(m.dirs.AnswersFile(), []byte("EVA_MD_HOSTS=nowhere\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	m.releaseServer(t, nil)
	asVersion(t, "2026.11.2")
	code, out, errOut := upgrade()
	if code == 0 {
		t.Fatalf("code %d\n%s\n%s", code, out, errOut)
	}
	says(t, "stderr", errOut, "eva-md: setup stopped; when it named a problem, fix it, then run eva-md upgrade again\n")
	if v := m.stableVersion(t); v != "eva-md 2026.11.2" {
		t.Fatalf("installed %q", v)
	}
}
