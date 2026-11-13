package modelrun

import (
	"context"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
)

func TestChildEnvironmentIsAnAllowlist(t *testing.T) {
	got := childEnv([]string{
		"HOME=/h", "PATH=/bin", "USER=u", "LOGNAME=u", "SHELL=/bin/zsh", "LANG=en_GB.UTF-8",
		"LC_ALL=C", "LC_CTYPE=UTF-8", "TMPDIR=/t", "CODEX_HOME=/c", "CLAUDE_CONFIG_DIR=/cc",
		"SSL_CERT_FILE=/s", "SSL_CERT_DIR=/sd", "NODE_EXTRA_CA_CERTS=/n", "CODEX_CA_CERTIFICATE=/x",
		"HTTP_PROXY=hp", "http_proxy=hp", "HTTPS_PROXY=sp", "https_proxy=sp", "ALL_PROXY=ap", "all_proxy=ap", "NO_PROXY=np", "no_proxy=np",
		"ANTHROPIC_API_KEY=fake-anthropic-value", "OPENAI_API_KEY=fake-openai-value",
		"CLAUDECODE=1", "CLAUDE_CODE_ENTRYPOINT=cli", "CLAUDE_CODE_DISABLE_AUTO_MEMORY=0",
		"TERM=xterm-256color", "EVA_MD_INNER=0", "FTP_PROXY=fp", "NODE_OPTIONS=--require=/x.js", "GIT_DIR=/g", "XLC_FAKE=1", "malformed",
		"home=/lower", "Path=/mixed",
	})
	want := []string{
		"HOME=/h", "PATH=/bin", "USER=u", "LOGNAME=u", "SHELL=/bin/zsh", "LANG=en_GB.UTF-8",
		"LC_ALL=C", "LC_CTYPE=UTF-8", "TMPDIR=/t", "CODEX_HOME=/c", "CLAUDE_CONFIG_DIR=/cc",
		"SSL_CERT_FILE=/s", "SSL_CERT_DIR=/sd", "NODE_EXTRA_CA_CERTS=/n", "CODEX_CA_CERTIFICATE=/x",
		"HTTP_PROXY=hp", "http_proxy=hp", "HTTPS_PROXY=sp", "https_proxy=sp", "ALL_PROXY=ap", "all_proxy=ap", "NO_PROXY=np", "no_proxy=np",
		"TERM=dumb", "EVA_MD_INNER=1", "CLAUDE_CODE_DISABLE_AUTO_MEMORY=1",
	}
	if !slices.Equal(got, want) {
		t.Fatalf("environment\n got %q\nwant %q", got, want)
	}
	if len(keepEnv) != 21 {
		t.Fatalf("the allowlist holds %d names, want the 21 above", len(keepEnv))
	}
}

func TestWithDirAddsTheCLIsFolderOnce(t *testing.T) {
	for _, tc := range []struct {
		env  []string
		want []string
	}{
		{[]string{"HOME=/h", "PATH=/usr/bin:/bin"}, []string{"HOME=/h", "PATH=/usr/bin:/bin:/opt/cli"}},
		{[]string{"PATH=/opt/cli:/bin"}, []string{"PATH=/opt/cli:/bin"}},
		{[]string{"HOME=/h"}, []string{"HOME=/h", "PATH=/opt/cli"}},
	} {
		if got := withDir(slices.Clone(tc.env), "/opt/cli"); !slices.Equal(got, tc.want) {
			t.Errorf("%q: got %q, want %q", tc.env, got, tc.want)
		}
	}
}

// The CLI install recorded is used while it is the absolute path of an
// executable file of the CLI's name, a link to one too.
func TestRecordedCLI(t *testing.T) {
	dir, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	cli, other := filepath.Join(dir, "cli"), filepath.Join(dir, "other")
	plain, folder, link := filepath.Join(dir, "plain", "cli"), filepath.Join(dir, "folder", "cli"), filepath.Join(dir, "link", "cli")
	for path, mode := range map[string]os.FileMode{cli: 0o700, other: 0o700, plain: 0o600} {
		if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte("#!/bin/sh\n"), mode); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.MkdirAll(folder, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Dir(link), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(cli, link); err != nil {
		t.Fatal(err)
	}
	for _, ok := range []string{cli, link} {
		if got := recordedCLI("cli", ok); got != ok {
			t.Errorf("%s: %q", ok, got)
		}
	}
	for _, bad := range []string{"", "cli", "bin/cli", filepath.Join(dir, "gone", "cli"), folder, plain, other} {
		if got := recordedCLI("cli", bad); got != "" {
			t.Errorf("%q was taken: %q", bad, got)
		}
	}
}

// spawn's own words: a CLI that is not there, or not executable, is
// missing, and an error names no more of the CLI than its base name.
func TestSpawnOfAMissingCLI(t *testing.T) {
	dir := t.TempDir()
	plain := filepath.Join(dir, "plain")
	if err := os.WriteFile(plain, []byte("#!/bin/sh\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	for _, cli := range []string{filepath.Join(dir, "gone"), plain, dir} {
		e, err := spawn(context.Background(), proc{cli: cli, dir: dir, stdout: &capped{limit: 1}})
		if err != nil || !e.missing || e.started {
			t.Errorf("%s: %+v %v, want missing", cli, e, err)
		}
	}
	// A run folder that is gone, one the child can't enter and a file fail
	// the start, but the CLI is there: the call could not run, with an error
	// that names no path, and the CLI is not missing.
	shut := filepath.Join(dir, "shut")
	if err := os.Mkdir(shut, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(shut, 0); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chmod(shut, 0o700) })
	for _, runDir := range []string{filepath.Join(dir, "gone"), shut, plain} {
		e, err := spawn(context.Background(), proc{cli: "/bin/sh", dir: runDir, stdout: &capped{limit: 1}})
		if err != errRunDir || e.missing || e.started {
			t.Errorf("run folder %s: %+v %v, want the run folder's error", runDir, e, err)
		}
		if err != nil && strings.Contains(err.Error(), dir) {
			t.Errorf("the error names a path: %v", err)
		}
	}
}

func TestOutputIsCapped(t *testing.T) {
	c := &capped{limit: 4}
	for _, part := range []string{"abc", "def"} {
		if n, err := c.Write([]byte(part)); n != len(part) || err != nil {
			t.Fatalf("write %q: %d %v", part, n, err)
		}
	}
	if c.String() != "abcd" || !c.overflow {
		t.Fatalf("kept %q, overflow %v", c.String(), c.overflow)
	}
	// A child that writes past the cap is not blocked, and its stderr is
	// capped too.
	isolate(t)
	dump := fakeCLI(t, `head -c 200000 /dev/zero; head -c 200000 /dev/zero >&2`)
	out := &capped{limit: 10}
	e, err := spawn(context.Background(), proc{cli: "/bin/sh", args: []string{filepath.Join(dump, "extra")}, dir: dump, stdout: out, waitDelay: waitDelay})
	if err != nil || e.failed || !out.overflow || out.buf.Len() != 10 || len(e.stderr) != maxStderr {
		t.Fatalf("exit %+v %v, stdout %d bytes, stderr %d bytes", e.failed, err, out.buf.Len(), len(e.stderr))
	}
}
