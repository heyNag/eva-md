package main

import (
	"bytes"
	"context"
	"crypto/sha256"
	"debug/buildinfo"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
	"sync"
	"syscall"
	"testing"
	"time"

	"github.com/heyNag/eva-md/internal/install"
	"github.com/heyNag/eva-md/internal/paths"
	"github.com/heyNag/eva-md/internal/wikiinit"
)

// The release stand-in is built with the go command in a home of its own,
// with GOENV=off, so no Go setting of the person running the tests applies.
// It shares Go's build and module caches with go test itself, found once
// from the environment the tests started in, before any test moves HOME.
// GOPROXY=off keeps the build off the network, -buildvcs=false keeps it from
// reading git, and the evamd_testlimits tag starts it with the limits
// TestMain raises (see raiseTimeLimits).
var (
	buildEnv     = os.Environ()
	buildDir, _  = os.Getwd()
	buildTemp    = os.TempDir()
	goTool       = findGo()
	releaseMu    sync.Mutex
	releaseRoot  string
	releaseBuilt = map[string]string{}
)

// goCaches are Go's build and module caches, as go env finds them in the
// environment the tests started in.
var goCaches = sync.OnceValues(func() ([]string, error) {
	ctx, cancel := context.WithTimeout(context.Background(), scriptHangGuard)
	defer cancel()
	cmd := exec.CommandContext(ctx, goTool, "env", "GOCACHE", "GOMODCACHE")
	cmd.Dir = buildDir
	cmd.Env = buildEnv
	out, err := cmd.Output()
	if err != nil {
		return nil, fmt.Errorf("go env: %v", err)
	}
	caches := strings.Split(strings.TrimSpace(string(out)), "\n")
	if len(caches) != 2 || !filepath.IsAbs(caches[0]) || !filepath.IsAbs(caches[1]) {
		return nil, fmt.Errorf("go env gave the caches as %q", out)
	}
	return caches, nil
})

// standInEnv is the environment the stand-in is built in, with home as its
// HOME. A later value of a name wins, as os/exec reads it. Go's telemetry is
// off in home, so no go command run there leaves a process behind that
// writes into it after the command ends.
func standInEnv(home string) ([]string, error) {
	telemetry := filepath.Join(home, "Library", "Application Support", "go", "telemetry")
	if err := os.MkdirAll(telemetry, 0o700); err != nil {
		return nil, err
	}
	if err := os.WriteFile(filepath.Join(telemetry, "mode"), []byte("off\n"), 0o600); err != nil {
		return nil, err
	}
	caches, err := goCaches()
	if err != nil {
		return nil, err
	}
	return append(append([]string{}, buildEnv...), "HOME="+home, "GOENV=off", "GOCACHE="+caches[0], "GOMODCACHE="+caches[1],
		"GOPROXY=off", "GOTOOLCHAIN=local", "GOFLAGS=-mod=readonly"), nil
}

func findGo() string {
	if path, err := exec.LookPath("go"); err == nil {
		return path
	}
	return filepath.Join(runtime.GOROOT(), "bin", "go")
}

// scriptHangGuard is how long a test waits for a script or a release
// binary, which run setup, git and the fake hosts, before it fails naming
// what hung.
const scriptHangGuard = 5 * time.Minute

// releaseBinary is eva-md built as a release is, with main.version set to
// tag through -ldflags. Each tag is built once for all tests.
func releaseBinary(t *testing.T, tag string) string {
	t.Helper()
	releaseMu.Lock()
	defer releaseMu.Unlock()
	if binary, ok := releaseBuilt[tag]; ok {
		return binary
	}
	if releaseRoot == "" {
		dir, err := os.MkdirTemp(buildTemp, "eva-md-release-")
		if err != nil {
			t.Fatal(err)
		}
		releaseRoot = dir
	}
	binary := filepath.Join(releaseRoot, tag, "eva-md")
	home := filepath.Join(releaseRoot, "home")
	if err := os.MkdirAll(home, 0o700); err != nil {
		t.Fatal(err)
	}
	env, err := standInEnv(home)
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), scriptHangGuard)
	defer cancel()
	cmd := exec.CommandContext(ctx, goTool, "build", "-mod=readonly", "-buildvcs=false", "-tags", "evamd_testlimits", "-ldflags", "-X main.version="+tag, "-o", binary, ".")
	cmd.Dir = buildDir
	cmd.Env = env
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("building eva-md %s: %v\n%s", tag, err, out)
	}
	releaseBuilt[tag] = binary
	return binary
}

// removeReleases removes the release stand-ins once every test has run.
func removeReleases() {
	if releaseRoot != "" {
		_ = os.RemoveAll(releaseRoot)
	}
}

// fakeMac is a Mac for the install, upgrade and uninstall tests: a home of
// its own, the fake host CLIs, a fake curl that serves the release stand-in,
// and fake id, uname and sysctl that say what the test wants, first on a
// PATH that otherwise holds only git, ~/.local/bin and the system's own
// folders, where no host CLI lives.
type fakeMac struct {
	dirs   paths.Dirs
	home   string
	bin    string // the fake tools
	root   string // the release stand-in, the fakes' answers and their logs
	hosts  string // the fake hosts' registrations
	tmp    string // TMPDIR
	wiki   string // where a test's wiki lives
	remote string // the bare repo it is pushed to
}

func newFakeMac(t *testing.T) *fakeMac {
	t.Helper()
	d := isolated(t)
	home, err := filepath.EvalSymlinks(os.Getenv("HOME"))
	if err != nil {
		t.Fatal(err)
	}
	m := &fakeMac{dirs: d, home: home, bin: filepath.Join(t.TempDir(), "fake"), root: t.TempDir(), tmp: t.TempDir()}
	m.hosts = filepath.Join(m.root, "hosts")
	m.wiki = filepath.Join(home, "eva-wiki")
	m.remote = filepath.Join(home, "remotes", "eva-wiki.git")
	for _, dir := range []string{m.bin, m.hosts, filepath.Join(home, ".local", "bin"), filepath.Dir(m.remote)} {
		if err := os.MkdirAll(dir, 0o700); err != nil {
			t.Fatal(err)
		}
	}
	gitconfig := "[user]\n\tname = Eva Fixture\n\temail = fixture@example.invalid\n[commit]\n\tgpgsign = false\n[init]\n\tdefaultBranch = main\n"
	if err := os.WriteFile(os.Getenv("GIT_CONFIG_GLOBAL"), []byte(gitconfig), 0o600); err != nil {
		t.Fatal(err)
	}
	gitRun(t, home, "init", "--bare", "--quiet", m.remote)
	q := func(s string) string { return "'" + strings.ReplaceAll(s, "'", `'\''`) + "'" }
	root := q(m.root)
	fakes := map[string]string{
		"curl": `out= url=
while [ $# -gt 0 ]; do
	case "$1" in
	-o) out=$2; shift 2 ;;
	-w|--proto|--proto-redir|--connect-timeout|--max-time) shift 2 ;;
	-*) shift ;;
	*) url=$1; shift ;;
	esac
done
echo "$url" >> ` + root + `/curl.log
case "$url" in
https://github.com/heyNag/eva-md/releases/latest)
	[ -f ` + root + `/latest ] || exit 22
	printf 'https://github.com/heyNag/eva-md/releases/tag/%s' "$(cat ` + root + `/latest)"
	exit ;;
https://github.com/heyNag/eva-md/releases/latest/download/*)
	[ -f ` + root + `/latest ] || exit 22
	f=` + root + `/releases/$(cat ` + root + `/latest)/${url##*/} ;;
https://github.com/heyNag/eva-md/releases/download/*)
	f=` + root + `/releases/${url#https://github.com/heyNag/eva-md/releases/download/} ;;
*) exit 6 ;;
esac
[ -f "$f" ] || exit 22
if [ -n "$out" ]; then cat "$f" > "$out"; else cat "$f"; fi
`,
		"id":     `if [ "$1" = -u ]; then cat ` + root + `/uid; else exec /usr/bin/id "$@"; fi`,
		"uname":  `case "$1" in -s) cat ` + root + `/os ;; -m) cat ` + root + `/arch ;; *) exec /usr/bin/uname "$@" ;; esac`,
		"sysctl": `if [ "$*" = "-n sysctl.proc_translated" ]; then cat ` + root + `/translated; else exec /usr/sbin/sysctl "$@"; fi`,
		"claude": fakeHost(m.hosts, "claude", "plugin list --json", `[{"pluginId":"eva-md@eva-md","name":"eva-md","enabled":true,"readFromFolder":"%s/plugins/eva-md","folderVersion":"1","errors":[]}]`, `[]`,
			`[{"name":"eva-md","root":"%s"}]`, `[]`, "plugin install eva-md@eva-md --scope user --json", "plugin uninstall eva-md@eva-md --scope user --json", "plugin marketplace remove eva-md --json"),
		// Codex is asked one marketplace at a time: its list of every
		// plugin also asks chatgpt.com.
		"codex": fakeHost(m.hosts, "codex", "plugin list --json --marketplace=eva-md", `{"installed":[{"pluginId":"eva-md@eva-md","name":"eva-md","enabled":true,"source":{"path":"%s/plugins/eva-md"}}]}`, `{"installed":[]}`,
			`{"marketplaces":[{"name":"eva-md","path":"%s"}]}`, `{"marketplaces":[]}`, "plugin add eva-md@eva-md --json", "plugin remove eva-md@eva-md --json", "plugin marketplace remove eva-md --json"),
	}
	for name, body := range fakes {
		if err := os.WriteFile(filepath.Join(m.bin, name), []byte("#!/bin/sh\n"+body+"\n"), 0o700); err != nil {
			t.Fatal(err)
		}
	}
	m.answer("uid", "501")
	m.answer("os", "Darwin")
	m.answer("arch", "arm64")
	m.answer("translated", "0")
	t.Setenv("PATH", strings.Join([]string{m.bin, pathFolder(), filepath.Join(home, ".local", "bin"), "/usr/bin", "/bin"}, string(filepath.ListSeparator)))
	t.Setenv("TMPDIR", m.tmp)
	t.Setenv("GIT_ALLOW_PROTOCOL", "file")
	t.Setenv("NO_COLOR", "1")
	for _, key := range []string{"EVA_MD_VERSION", "GIT_SSH_COMMAND"} {
		t.Setenv(key, "")
		os.Unsetenv(key)
	}
	return m
}

// fakeHost is a host CLI that keeps eva-md's registration in files: the
// marketplace's folder once it is added, and a mark once the plugin is. It
// answers its plugin list only when asked the way list says.
func fakeHost(state, host, list, listed, empty, market, noMarket, add, remove, marketRemove string) string {
	s := "'" + filepath.Join(state, host) + "'"
	// The calls are logged beside the state, so a test that compares the
	// registrations sees only them.
	return `echo "$*" >> '` + filepath.Join(filepath.Dir(state), host) + `.calls'
case "$*" in
"` + list + `")
	if [ -f ` + s + `.plugin ]; then printf '` + listed + `\n' "$(cat ` + s + `.market)"; else echo '` + empty + `'; fi ;;
"plugin marketplace list --json")
	if [ -f ` + s + `.market ]; then printf '` + market + `\n' "$(cat ` + s + `.market)"; else echo '` + noMarket + `'; fi ;;
"plugin validate --strict --json "*) echo '{"success":true}' ;;
"features list") echo 'hooks experimental true' ;;
"plugin marketplace add "*" --json") printf '%s' "$4" > ` + s + `.market; echo '{}' ;;
"` + add + `") : > ` + s + `.plugin; echo '{}' ;;
"` + remove + `") rm -f ` + s + `.plugin; echo '{}' ;;
"` + marketRemove + `") rm -f ` + s + `.market; echo '{}' ;;
*) exit 2 ;;
esac`
}

// answer sets what a fake tool says, by the name of its file in m.root.
func (m *fakeMac) answer(name, value string) {
	if err := os.WriteFile(filepath.Join(m.root, name), []byte(value+"\n"), 0o600); err != nil {
		panic(err)
	}
}

// publish puts release tag in the stand-in, as the release workflow does:
// both binaries (this Mac's own build under both names), the scripts and
// SHA256SUMS. The last tag published is the latest.
func (m *fakeMac) publish(t *testing.T, tag, builtAs string) string {
	t.Helper()
	dir := filepath.Join(m.root, "releases", tag)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	binary := releaseBinary(t, builtAs)
	var sums []string
	for name, from := range map[string]string{
		"eva-md-darwin-arm64": binary, "eva-md-darwin-amd64": binary,
		"install.sh": filepath.Join(buildDir, "..", "..", "scripts", "install.sh"), "uninstall.sh": filepath.Join(buildDir, "..", "..", "scripts", "uninstall.sh"),
	} {
		data, err := os.ReadFile(from)
		if err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(dir, name), data, 0o600); err != nil {
			t.Fatal(err)
		}
		sum := sha256.Sum256(data)
		sums = append(sums, hex.EncodeToString(sum[:])+"  "+name)
	}
	sort.Strings(sums)
	if err := os.WriteFile(filepath.Join(dir, "SHA256SUMS"), []byte(strings.Join(sums, "\n")+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	m.answer("latest", tag)
	return dir
}

// script runs one of the release's scripts as curl -fsSL ... | bash -s --
// args runs it: bash reads it from stdin, in a session of its own, so it
// has no terminal, as in CI.
func (m *fakeMac) script(t *testing.T, name string, env []string, args ...string) (int, string, string) {
	t.Helper()
	body, err := os.ReadFile(filepath.Join(buildDir, "..", "..", "scripts", name))
	if err != nil {
		t.Fatal(err)
	}
	return m.bash(t, name, bytes.NewReader(body), env, append([]string{"-s", "--"}, args...)...)
}

// pipeline runs line, a whole command as a person types it, such as
// curl ... | bash, in bash, with no terminal.
func (m *fakeMac) pipeline(t *testing.T, line string) (int, string, string) {
	t.Helper()
	return m.bash(t, line, nil, nil, "-c", line)
}

// bash runs /bin/bash with args, stdin and env added to the test's, in a
// session of its own, so it has no terminal, as in CI.
func (m *fakeMac) bash(t *testing.T, what string, stdin io.Reader, env []string, args ...string) (int, string, string) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), scriptHangGuard)
	defer cancel()
	cmd := exec.CommandContext(ctx, "/bin/bash", args...)
	cmd.Stdin = stdin
	cmd.Dir = m.home
	cmd.Env = append(os.Environ(), env...)
	cmd.SysProcAttr = &syscall.SysProcAttr{Setsid: true}
	var out, errOut bytes.Buffer
	cmd.Stdout, cmd.Stderr = &out, &errOut
	err := cmd.Run()
	if ctx.Err() != nil {
		t.Fatalf("%s hung\nstdout:\n%s\nstderr:\n%s", what, out.String(), errOut.String())
	}
	code := 0
	var exit *exec.ExitError
	if errors.As(err, &exit) {
		code = exit.ExitCode()
	} else if err != nil {
		t.Fatal(err)
	}
	return code, out.String(), errOut.String()
}

// exec runs binary with args as a person would, with no terminal.
func (m *fakeMac) exec(t *testing.T, binary string, args ...string) (int, string, string) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), scriptHangGuard)
	defer cancel()
	cmd := exec.CommandContext(ctx, binary, args...)
	cmd.Dir = m.home
	cmd.SysProcAttr = &syscall.SysProcAttr{Setsid: true}
	var out, errOut bytes.Buffer
	cmd.Stdout, cmd.Stderr = &out, &errOut
	err := cmd.Run()
	if ctx.Err() != nil {
		t.Fatalf("%s %v hung", binary, args)
	}
	var exit *exec.ExitError
	if errors.As(err, &exit) {
		return exit.ExitCode(), out.String(), errOut.String()
	} else if err != nil {
		t.Fatal(err)
	}
	return 0, out.String(), errOut.String()
}

// setUpWiki makes the wiki in m.wiki, pushed to m.remote, and the config,
// as setup leaves them.
func (m *fakeMac) setUpWiki(t *testing.T) {
	t.Helper()
	if _, err := wikiinit.Run(context.Background(), wikiinit.Options{Remote: m.remote, Dir: m.wiki, Dirs: m.dirs, Version: "test", OKFModule: "v0.7.2"}); err != nil {
		t.Fatal(err)
	}
}

// installed sets m up and installs release tag's binary with its own eva-md
// install, which registers the plugins with the fake hosts, writes the
// record and links ~/.local/bin/eva-md.
func (m *fakeMac) installed(t *testing.T, tag string) {
	t.Helper()
	m.setUpWiki(t)
	if code, out, errOut := m.exec(t, releaseBinary(t, tag), "install"); code != 0 {
		t.Fatalf("install: %d\n%s\n%s", code, out, errOut)
	}
	m.wantRegistered(t, true)
}

// stable is the installed binary.
func (m *fakeMac) stable() string { return filepath.Join(m.dirs.Share, "bin", "eva-md") }

// link is the link on PATH.
func (m *fakeMac) link() string { return install.LinkPath(m.dirs) }

// wantRegistered checks that both fake hosts list eva-md, or neither.
func (m *fakeMac) wantRegistered(t *testing.T, want bool) {
	t.Helper()
	for _, host := range []string{"claude", "codex"} {
		for _, mark := range []string{".plugin", ".market"} {
			_, err := os.Stat(filepath.Join(m.hosts, host+mark))
			if (err == nil) != want {
				t.Fatalf("%s%s registered: %v, want %v", host, mark, err == nil, want)
			}
		}
	}
}

// stableVersion is what the installed binary says it is.
func (m *fakeMac) stableVersion(t *testing.T) string {
	t.Helper()
	code, out, _ := m.exec(t, m.stable(), "version")
	if code != 0 {
		t.Fatalf("the installed binary: %d", code)
	}
	line, _, _ := strings.Cut(out, "\n")
	return line
}

// curlLog is every address the fake curl was asked for.
func (m *fakeMac) curlLog() string {
	b, _ := os.ReadFile(filepath.Join(m.root, "curl.log"))
	return string(b)
}

// tree is a digest of every name, mode, link and file under the given
// folders and files, to show that something changed nothing.
func tree(t *testing.T, roots ...string) string {
	t.Helper()
	var b strings.Builder
	for _, root := range roots {
		err := filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
			if errors.Is(err, fs.ErrNotExist) {
				fmt.Fprintf(&b, "%s missing\n", path)
				return nil
			}
			if err != nil {
				return err
			}
			info, err := d.Info()
			if err != nil {
				return err
			}
			fmt.Fprintf(&b, "%s %v", path, info.Mode())
			switch {
			case info.Mode()&fs.ModeSymlink != 0:
				to, _ := os.Readlink(path)
				fmt.Fprintf(&b, " -> %s", to)
			case info.Mode().IsRegular():
				f, err := os.Open(path)
				if err != nil {
					return err
				}
				h := sha256.New()
				_, err = io.Copy(h, f)
				f.Close()
				if err != nil {
					return err
				}
				fmt.Fprintf(&b, " %x", h.Sum(nil))
			}
			b.WriteByte('\n')
			return nil
		})
		if err != nil {
			t.Fatal(err)
		}
	}
	return b.String()
}

// says fails when s lacks any of wants.
func says(t *testing.T, what, s string, wants ...string) {
	t.Helper()
	for _, want := range wants {
		if !strings.Contains(s, want) {
			t.Errorf("%s lacks %q:\n%s", what, want, s)
		}
	}
}

// tmpLeft fails when a script's temporary folder is left in TMPDIR.
func (m *fakeMac) tmpLeft(t *testing.T) {
	t.Helper()
	if left, _ := filepath.Glob(filepath.Join(m.tmp, "eva-md-*")); len(left) != 0 {
		t.Fatalf("temporary folders left: %v", left)
	}
}

// On a new Mac the install command stops at setup's answers file, with the
// template written and the closing line; filled in, with a local bare repo
// as the remote, the same command finishes setup: the stable binary of the
// release, the plugins registered with both hosts, the record and the link
// on PATH. Run again, it keeps the wiki. Its temporary folder never stays.
func TestInstallScriptOnANewMac(t *testing.T) {
	m := newFakeMac(t)
	m.publish(t, "2026.11.3", "2026.11.3")
	code, out, errOut := m.script(t, "install.sh", nil)
	if code != 2 {
		t.Fatalf("first run: code %d\n%s\n%s", code, out, errOut)
	}
	says(t, "stdout", out, "Installing eva-md 2026.11.3 for arm64\nChecksum verified\n", "eva.md setup", "Wrote "+m.dirs.AnswersFile())
	says(t, "stderr", errOut, "fill it in and run setup again", "Setup stopped. When it named a problem, fix it; then run the install command again.\n")
	if b, err := os.ReadFile(m.dirs.AnswersFile()); err != nil || !strings.Contains(string(b), "EVA_MD_WIKI_DIR=") {
		t.Fatalf("the template: %q %v", b, err)
	}
	m.tmpLeft(t)
	if err := os.WriteFile(m.dirs.AnswersFile(), []byte("EVA_MD_WIKI_DIR="+m.wiki+"\nEVA_MD_WIKI_REMOTE="+m.remote+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	code, out, errOut = m.script(t, "install.sh", nil)
	if code != 0 {
		t.Fatalf("second run: code %d\n%s\n%s", code, out, errOut)
	}
	says(t, "stdout", out, "Installing eva-md 2026.11.3 for arm64", "Creating the wiki", "Installing the plugins", "link "+m.link()+" to the stable binary")
	if v := m.stableVersion(t); v != "eva-md 2026.11.3" {
		t.Fatalf("installed %q", v)
	}
	m.wantRegistered(t, true)
	if to, err := os.Readlink(m.link()); err != nil || to != m.stable() {
		t.Fatalf("the link: %q %v", to, err)
	}
	record, err := os.ReadFile(filepath.Join(m.dirs.Share, "install-record.json"))
	if err != nil {
		t.Fatal(err)
	}
	if info, _ := os.Stat(filepath.Join(m.dirs.Share, "install-record.json")); info.Mode().Perm() != 0o600 {
		t.Fatalf("the record's mode %v", info.Mode())
	}
	says(t, "the record", string(record), `"kind": "binary"`, `"kind": "path_link"`, `"kind": "answers"`, `"kind": "config"`,
		`"kind": "wiki",`+"\n      \"path\": "+jsonQuote(m.wiki)+",\n      \"kept\": true", `"kind": "plugin",`+"\n      \"host\": \"codex\"")
	if gitRun(t, m.remote, "rev-parse", "main") == "" {
		t.Fatal("the wiki wasn't pushed")
	}
	m.tmpLeft(t)
	// The same command on a Mac that is set up keeps the wiki and its
	// remote as they are.
	wiki := tree(t, m.wiki, m.remote)
	code, out, errOut = m.script(t, "install.sh", nil)
	if code != 0 {
		t.Fatalf("third run: code %d\n%s\n%s", code, out, errOut)
	}
	says(t, "stdout", out, "eva-md is set up here")
	if tree(t, m.wiki, m.remote) != wiki {
		t.Fatal("the wiki or its remote changed")
	}
}

// On a Mac set up before there was a config.env, setup writes one after its
// install has written the record, and setup's command adds it to the record.
func TestInstallScriptRecordsALateConfigEnv(t *testing.T) {
	m := newFakeMac(t)
	m.publish(t, "2026.11.3", "2026.11.3")
	m.setUpWiki(t)
	code, out, errOut := m.script(t, "install.sh", nil)
	if code != 0 {
		t.Fatalf("code %d\n%s\n%s", code, out, errOut)
	}
	says(t, "stdout", out, "Wrote "+m.dirs.AnswersFile()+" with this Mac's answers")
	r, ok := install.ReadRecord(m.dirs)
	if !ok {
		t.Fatal("no record")
	}
	if item, has := r.Find(install.KindAnswers, ""); !has || item.Path != m.dirs.AnswersFile() {
		t.Fatalf("the record lacks config.env: %+v", r)
	}
}

// The way README pins a release, the tag given to bash after the pipe,
// installs that release when the whole command runs as a person types it,
// though a newer one is the latest. install.sh's header and design.md show
// the same command.
func TestInstallScriptVersionPinThroughThePipe(t *testing.T) {
	read := func(name string) string {
		b, err := os.ReadFile(filepath.Join(buildDir, "..", "..", filepath.FromSlash(name)))
		if err != nil {
			t.Fatal(err)
		}
		return string(b)
	}
	command := ""
	for _, line := range strings.Split(read("README.md"), "\n") {
		if strings.HasPrefix(line, "curl ") && strings.Contains(line, "| EVA_MD_VERSION=2026.11.3 bash") {
			command = line
		}
	}
	if command == "" {
		t.Fatal("README shows no command that pins a release")
	}
	says(t, "install.sh", read("scripts/install.sh"), command)
	says(t, "design.md", read("docs/design.md"), command)
	m := newFakeMac(t)
	m.publish(t, "2026.11.3", "2026.11.3")
	m.publish(t, "2026.11.4", "2026.11.3")
	code, out, errOut := m.pipeline(t, command)
	if code != 2 {
		t.Fatalf("code %d\n%s\n%s", code, out, errOut)
	}
	says(t, "stdout", out, "Installing eva-md 2026.11.3 for arm64\n")
	if log := m.curlLog(); strings.Contains(log, "/releases/latest\n") || !strings.Contains(log, "/releases/latest/download/install.sh\n") {
		t.Fatalf("curl: %s", log)
	}
}

// The release stand-in is built with the evamd_testlimits tag, so the
// setup, install and uninstall it runs in child processes have the limits
// the tests raise, and in a home of its own with no Go settings file, so
// nothing of the real home's Go settings applies.
func TestReleaseStandInBuild(t *testing.T) {
	info, err := buildinfo.ReadFile(releaseBinary(t, "2026.11.3"))
	if err != nil {
		t.Fatal(err)
	}
	tags := ""
	for _, setting := range info.Settings {
		if setting.Key == "-tags" {
			tags = setting.Value
		}
	}
	if tags != "evamd_testlimits" {
		t.Fatalf("built with tags %q", tags)
	}
	home := t.TempDir()
	env, err := standInEnv(home)
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), scriptHangGuard)
	defer cancel()
	// go env names no settings file once GOENV is off, and telemetry is off.
	cmd := exec.CommandContext(ctx, goTool, "env", "GOENV", "GOTELEMETRY")
	cmd.Dir, cmd.Env = buildDir, env
	if out, err := cmd.Output(); err != nil || string(out) != "\noff\n" {
		t.Fatalf("go env GOENV GOTELEMETRY: %q %v", out, err)
	}
	last := map[string]string{}
	for _, kv := range env {
		k, v, _ := strings.Cut(kv, "=")
		last[k] = v
	}
	if last["HOME"] != home {
		t.Fatalf("built with HOME %q", last["HOME"])
	}
}

// A sum that differs, or SHA256SUMS without the binary's line, stops the
// script before anything runs: setup writes no answers file.
func TestInstallScriptChecksumMismatch(t *testing.T) {
	for _, c := range []struct{ name, sums, err string }{
		{"mismatch", strings.Repeat("0", 64) + "  eva-md-darwin-arm64\n", "the checksum of eva-md-darwin-arm64 doesn't match SHA256SUMS; nothing was run"},
		{"missing line", strings.Repeat("0", 64) + "  eva-md-darwin-amd64\n", "SHA256SUMS of eva-md 2026.11.3 has no line for eva-md-darwin-arm64; nothing was run"},
	} {
		t.Run(c.name, func(t *testing.T) {
			m := newFakeMac(t)
			dir := m.publish(t, "2026.11.3", "2026.11.3")
			if err := os.WriteFile(filepath.Join(dir, "SHA256SUMS"), []byte(c.sums), 0o600); err != nil {
				t.Fatal(err)
			}
			code, out, errOut := m.script(t, "install.sh", nil)
			if code != 1 {
				t.Fatalf("code %d\n%s\n%s", code, out, errOut)
			}
			says(t, "stderr", errOut, c.err, "then run the install command again")
			if _, err := os.Stat(m.dirs.Config); !errors.Is(err, fs.ErrNotExist) {
				t.Fatalf("setup ran: %v", err)
			}
			m.tmpLeft(t)
		})
	}
}

// The script refuses root, and any OS but macOS, before it downloads
// anything.
func TestInstallScriptRefusesRootAndOtherSystems(t *testing.T) {
	for _, c := range []struct{ name, file, value, err string }{
		{"root", "uid", "0", "this runs as root\nFix: run it as yourself, without sudo, then run the"},
		{"Linux", "os", "Linux", "eva-md runs on macOS only"},
	} {
		t.Run(c.name, func(t *testing.T) {
			m := newFakeMac(t)
			m.publish(t, "2026.11.3", "2026.11.3")
			m.answer(c.file, c.value)
			for _, script := range []string{"install.sh", "uninstall.sh"} {
				code, _, errOut := m.script(t, script, nil)
				if code != 1 || !strings.Contains(errOut, c.err) {
					t.Fatalf("%s: code %d\n%s", script, code, errOut)
				}
			}
			if m.curlLog() != "" {
				t.Fatalf("downloaded: %s", m.curlLog())
			}
		})
	}
}

// An Apple silicon Mac gets the arm64 binary, an Intel Mac the amd64 one,
// and a shell Rosetta runs on Apple silicon the arm64 one.
func TestInstallScriptPicksTheMacsBinary(t *testing.T) {
	for _, c := range []struct{ arch, translated, want string }{
		{"arm64", "0", "arm64"}, {"x86_64", "0", "amd64"}, {"x86_64", "1", "arm64"},
	} {
		t.Run(c.arch+"-"+c.translated, func(t *testing.T) {
			m := newFakeMac(t)
			m.publish(t, "2026.11.3", "2026.11.3")
			m.answer("arch", c.arch)
			m.answer("translated", c.translated)
			code, out, errOut := m.script(t, "install.sh", nil)
			if code != 2 {
				t.Fatalf("code %d\n%s\n%s", code, out, errOut)
			}
			says(t, "stdout", out, "Installing eva-md 2026.11.3 for "+c.want+"\n")
			says(t, "curl", m.curlLog(), "/releases/download/2026.11.3/eva-md-darwin-"+c.want+"\n")
		})
	}
}

// EVA_MD_VERSION installs that release without asking for the latest; a
// malformed one is refused before any download, and a binary that says it
// is another version than its tag is refused before setup.
func TestInstallScriptVersionPin(t *testing.T) {
	m := newFakeMac(t)
	m.publish(t, "2026.11.2", "2026.11.2")
	m.publish(t, "2026.11.3", "2026.11.3")
	code, out, errOut := m.script(t, "install.sh", []string{"EVA_MD_VERSION=2026.11.2"})
	if code != 2 {
		t.Fatalf("code %d\n%s\n%s", code, out, errOut)
	}
	says(t, "stdout", out, "Installing eva-md 2026.11.2 for arm64")
	if log := m.curlLog(); strings.Contains(log, "/releases/latest") || !strings.Contains(log, "/download/2026.11.2/eva-md-darwin-arm64") {
		t.Fatalf("curl: %s", log)
	}
	if err := os.Remove(filepath.Join(m.root, "curl.log")); err != nil {
		t.Fatal(err)
	}
	for _, bad := range []string{"v2026.11.2", "2026.11.02", "latest"} {
		code, _, errOut := m.script(t, "install.sh", []string{"EVA_MD_VERSION=" + bad})
		if code != 1 || !strings.Contains(errOut, "EVA_MD_VERSION="+bad+" isn't a release tag") {
			t.Fatalf("%s: code %d\n%s", bad, code, errOut)
		}
	}
	if m.curlLog() != "" {
		t.Fatalf("downloaded: %s", m.curlLog())
	}
	m.publish(t, "2026.11.5", "2026.11.3")
	if err := os.RemoveAll(m.dirs.Config); err != nil {
		t.Fatal(err)
	}
	code, _, errOut = m.script(t, "install.sh", []string{"EVA_MD_VERSION=2026.11.5"})
	if code != 1 || !strings.Contains(errOut, "the downloaded binary says it is eva-md 2026.11.3, not eva-md 2026.11.5") {
		t.Fatalf("code %d\n%s", code, errOut)
	}
	if _, err := os.Stat(m.dirs.Config); !errors.Is(err, fs.ErrNotExist) {
		t.Fatalf("setup ran: %v", err)
	}
}

// uninstall.sh runs the installed binary's uninstall, with no download:
// a preview, then --apply. With the binary gone, it downloads the latest
// release's, checks it, and runs its uninstall, here --apply --all --yes.
// The wiki and its remote stay as they were.
func TestUninstallScript(t *testing.T) {
	m := newFakeMac(t)
	m.publish(t, "2026.11.3", "2026.11.3")
	m.installed(t, "2026.11.3")
	wiki := tree(t, m.wiki, m.remote)
	code, out, errOut := m.script(t, "uninstall.sh", nil)
	if code != 0 {
		t.Fatalf("preview: code %d\n%s\n%s", code, out, errOut)
	}
	says(t, "stdout", out, "Preview; nothing was changed.", "Your wiki is kept in "+m.wiki)
	m.wantRegistered(t, true)
	code, out, errOut = m.script(t, "uninstall.sh", nil, "--apply")
	if code != 0 {
		t.Fatalf("apply: code %d\n%s\n%s", code, out, errOut)
	}
	says(t, "stdout", out, "Removed:", "the link "+m.link())
	m.wantRegistered(t, false)
	if m.curlLog() != "" {
		t.Fatalf("downloaded: %s", m.curlLog())
	}
	if _, err := os.Stat(m.dirs.ConfigFile()); err != nil {
		t.Fatalf("the config: %v", err)
	}
	// The binary is gone now, so the script downloads one.
	code, out, errOut = m.script(t, "uninstall.sh", nil, "--apply", "--all", "--yes")
	if code != 0 {
		t.Fatalf("--all: code %d\n%s\n%s", code, out, errOut)
	}
	says(t, "stdout", out, "The installed eva-md is missing or doesn't run; using eva-md 2026.11.3 for arm64 to uninstall\nChecksum verified\n", "Removed:")
	says(t, "curl", m.curlLog(), "/download/2026.11.3/eva-md-darwin-arm64")
	for _, dir := range []string{m.dirs.Config, m.dirs.State, m.dirs.Share} {
		if _, err := os.Stat(dir); !errors.Is(err, fs.ErrNotExist) {
			t.Fatalf("%s kept: %v", dir, err)
		}
	}
	if tree(t, m.wiki, m.remote) != wiki {
		t.Fatal("the wiki or its remote changed")
	}
	m.tmpLeft(t)
}

// An installed eva-md from before uninstall took --all answers it with a
// usage error, so uninstall.sh downloads and checks the latest release's
// and runs that one's uninstall. A newer installed binary previews --all
// itself, with no download.
func TestUninstallScriptWithAnOlderBinary(t *testing.T) {
	m := newFakeMac(t)
	m.publish(t, "2026.11.3", "2026.11.3")
	m.installed(t, "2026.11.3")
	wiki := tree(t, m.wiki, m.remote)
	code, out, errOut := m.script(t, "uninstall.sh", nil, "--all")
	if code != 0 || m.curlLog() != "" {
		t.Fatalf("preview: code %d, curl %q\n%s\n%s", code, m.curlLog(), out, errOut)
	}
	says(t, "stdout", out, "Preview; nothing was changed.", "Add --apply to remove all of this.")
	calls := filepath.Join(m.root, "old.calls")
	old := `#!/bin/sh
echo "$*" >> '` + calls + `'
case "$*" in
version) echo 'eva-md 2026.11.2+1530.abcdef0' ;;
uninstall | "uninstall --apply") echo 'the old uninstall ran' ;;
*) echo 'eva-md: invalid arguments for uninstall' >&2; exit 2 ;;
esac
`
	if err := os.WriteFile(m.stable(), []byte(old), 0o700); err != nil {
		t.Fatal(err)
	}
	code, out, errOut = m.script(t, "uninstall.sh", nil, "--apply", "--all", "--yes")
	if code != 0 {
		t.Fatalf("code %d\n%s\n%s", code, out, errOut)
	}
	says(t, "stdout", out, "The installed eva-md is too old for --all and --yes; using eva-md 2026.11.3 for arm64 to uninstall\nChecksum verified\n", "Removed:")
	if b, _ := os.ReadFile(calls); string(b) != "version\nuninstall --all\n" {
		t.Fatalf("the old binary ran %q", b)
	}
	m.wantRegistered(t, false)
	for _, dir := range []string{m.dirs.Config, m.dirs.State, m.dirs.Share} {
		if _, err := os.Stat(dir); !errors.Is(err, fs.ErrNotExist) {
			t.Fatalf("%s kept: %v", dir, err)
		}
	}
	if tree(t, m.wiki, m.remote) != wiki {
		t.Fatal("the wiki or its remote changed")
	}
	m.tmpLeft(t)
}
