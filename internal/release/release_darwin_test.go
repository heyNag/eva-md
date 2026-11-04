package release

import (
	"bytes"
	"context"
	"crypto/sha256"
	"errors"
	"fmt"
	"maps"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"syscall"
	"testing"
	"time"
	"unsafe"
)

// releaseHangGuard is how long a test waits for the release script however
// slow the machine, before it fails naming what hung.
const releaseHangGuard = 2 * time.Minute

// The commits every test repo starts with. The last is made on 4 January
// in Chicago, which is already 5 January in UTC, so its release is 2026.1.5.
var releaseCommits = []struct{ subject, date string }{
	{"Start the project", "2026-01-02T10:00:00Z"},
	{"Add the release flow", "2026-01-03T10:00:00Z"},
	{"Fix a typo", "2026-01-04T22:30:00-06:00"},
}

// releaseEnv is one test's release setup: a checkout of eva-md's release
// files on main with a bare origin, a PATH holding git and a fake gh, and
// git config that signs tags with a fake signing program, so no real key,
// remote or GitHub is used. origin's URL is GitHub's, which git config
// rewrites to the bare origin.
type releaseEnv struct {
	dir     string // the checkout
	origin  string // the bare origin
	state   string // what the fake gh answers and records
	bin     string // the PATH folder with git and gh
	pushLog string // the arguments of every git push, one line each
}

// githubOrigin is the URL a maintainer's clone has for origin.
const githubOrigin = "git@github.com:heyNag/eva-md.git"

// newReleaseEnv makes the setup. CI passed for main's last commit, which is
// pushed, and nothing is released yet.
func newReleaseEnv(t *testing.T) *releaseEnv {
	t.Helper()
	git, err := exec.LookPath("git")
	if err != nil {
		t.Fatal(err)
	}
	home := t.TempDir()
	e := &releaseEnv{
		dir:     filepath.Join(home, "eva-md"),
		origin:  filepath.Join(home, "origin.git"),
		state:   filepath.Join(home, "gh"),
		bin:     filepath.Join(home, "bin"),
		pushLog: filepath.Join(home, "push.log"),
	}
	for _, d := range []string{e.state, filepath.Join(e.state, "assets"), e.bin, filepath.Join(home, "tmp")} {
		if err := os.MkdirAll(d, 0o700); err != nil {
			t.Fatal(err)
		}
	}
	t.Setenv("HOME", home)
	t.Setenv("GIT_CONFIG_GLOBAL", filepath.Join(home, "gitconfig"))
	t.Setenv("GIT_CONFIG_NOSYSTEM", "1")
	t.Setenv("TMPDIR", filepath.Join(home, "tmp"))
	// A zone behind UTC, so a tag named by local time would be a day early.
	t.Setenv("TZ", "America/Chicago")
	t.Setenv("PATH", e.bin+":/usr/bin:/bin:/usr/sbin:/sbin")
	// Nothing from the environment go test runs in may point git at
	// another repository, index or config.
	// Nor may a plan write to the output of a workflow running go test.
	for _, k := range []string{"GH_HOST", "GH_REPO", "GH_TOKEN", "GITHUB_TOKEN", "GITHUB_OUTPUT",
		"GIT_DIR", "GIT_WORK_TREE", "GIT_INDEX_FILE", "GIT_COMMON_DIR", "GIT_OBJECT_DIRECTORY",
		"GIT_ALTERNATE_OBJECT_DIRECTORIES", "GIT_NAMESPACE", "GIT_PREFIX", "GIT_CONFIG",
		"GIT_CONFIG_COUNT", "GIT_CONFIG_PARAMETERS", "GIT_TEMPLATE_DIR"} {
		t.Setenv(k, "")
		if err := os.Unsetenv(k); err != nil {
			t.Fatal(err)
		}
	}
	// git itself, recording each push's arguments.
	writeFile(t, filepath.Join(e.bin, "git"), fmt.Sprintf(`#!/bin/bash
if [ "$1" = push ]; then
	printf '%%s\n' "$*" >> %q
fi
exec %q "$@"
`, e.pushLog, git), 0o700)
	signer := filepath.Join(home, "signer")
	writeFile(t, signer, `#!/bin/bash
# A fake ssh-keygen -Y sign: it records the call and writes a made-up
# signature beside the file it signs.
printf '%s\n' "$*" >> "`+filepath.Join(home, "signer.log")+`"
f=${@: -1}
printf -- '-----BEGIN SSH SIGNATURE-----\nZmFrZQ==\n-----END SSH SIGNATURE-----\n' > "$f.sig"
`, 0o700)
	writeFile(t, filepath.Join(home, "signing-key.pub"), "ssh-ed25519 AAAAC3NzaC1lZDI1NTE5AAAAIMadeUpMadeUpMadeUpMadeUpMadeUpMadeUp you@example.com\n", 0o600)
	writeFile(t, filepath.Join(home, "gitconfig"), fmt.Sprintf(`[user]
	name = You
	email = you@example.com
	signingkey = %s
[gpg]
	format = ssh
[gpg "ssh"]
	program = %s
[init]
	defaultBranch = main
[push]
	followTags = true
[url "%s"]
	insteadOf = %s
`, filepath.Join(home, "signing-key.pub"), signer, e.origin, githubOrigin), 0o600)
	writeFile(t, filepath.Join(e.bin, "gh"), strings.NewReplacer("STATE", e.state, "ORIGIN", e.origin).Replace(fakeGH), 0o700)
	writeFile(t, filepath.Join(e.state, "ci-runs"), "completed/success/101\n", 0o600)

	gitIn(t, home, "init", "-q", "--bare", "-b", "main", e.origin)
	gitIn(t, home, "init", "-q", "-b", "main", e.dir)
	script, err := os.ReadFile(filepath.Join("..", "..", "scripts", "release.sh"))
	if err != nil {
		t.Fatal(err)
	}
	writeFile(t, filepath.Join(e.dir, "go.mod"), "module github.com/heyNag/eva-md\n\ngo 1.26.0\n", 0o644)
	commitAt(t, e.dir, releaseCommits[0].subject, releaseCommits[0].date)
	for name, body := range map[string]string{
		".github/workflows/ci.yml":      "name: CI\n",
		".github/workflows/release.yml": "name: Release\n",
		"scripts/install.sh":            "#!/bin/bash\necho install\n",
		"scripts/uninstall.sh":          "#!/bin/bash\necho uninstall\n",
		"scripts/LICENSES.txt":          "eva-md's licenses\n",
		"scripts/release.sh":            string(script),
	} {
		writeFile(t, filepath.Join(e.dir, name), body, 0o755)
	}
	commitAt(t, e.dir, releaseCommits[1].subject, releaseCommits[1].date)
	writeFile(t, filepath.Join(e.dir, "README.md"), "eva-md\n", 0o644)
	commitAt(t, e.dir, releaseCommits[2].subject, releaseCommits[2].date)
	gitIn(t, e.dir, "remote", "add", "origin", githubOrigin)
	gitIn(t, e.dir, "push", "-q", "origin", "main")
	gitIn(t, e.dir, "fetch", "-q", "origin")
	return e
}

// fakeGH stands in for gh. It records each call and answers from the files
// in STATE: ci-runs is what gh run list prints for CI; signed-out, a
// watch-exit code and attest-fails change those answers, signed-out also
// gh api's read of the repository. The release
// workflow's run is listed only once origin has its tag, and the release's
// assets are those in STATE/assets.
const fakeGH = `#!/bin/bash
state='STATE'
origin='ORIGIN'
printf '%s\n' "$*" >> "$state/calls"
value() {
	local want=$1
	shift
	while [ $# -gt 0 ]; do
		if [ "$1" = "$want" ]; then
			echo "$2"
			return
		fi
		shift
	done
}
case "$1 $2" in
"auth status" | "api repos/heyNag/eva-md")
	[ -e "$state/signed-out" ] && exit 1
	exit 0
	;;
"run list")
	case " $* " in
	*" --workflow ci.yml "*)
		cat "$state/ci-runs"
		exit 0
		;;
	*" --workflow release.yml "*)
		tag=$(value --branch "$@")
		if git --git-dir="$origin" rev-parse -q --verify "refs/tags/$tag" >/dev/null; then
			echo 4242
		fi
		exit 0
		;;
	esac
	;;
"run watch")
	[ "$3" = 4242 ] || exit 1
	exit "$(cat "$state/watch-exit" 2>/dev/null || echo 0)"
	;;
"release download")
	git --git-dir="$origin" rev-parse -q --verify "refs/tags/$3" >/dev/null || {
		echo "release not found" >&2
		exit 1
	}
	cp "$state/assets/"* "$(value --dir "$@")/"
	exit 0
	;;
"attestation verify")
	[ -e "$state/attest-fails" ] && exit 1
	exit 0
	;;
esac
echo "fake gh: unexpected call: $*" >&2
exit 1
`

func writeFile(t *testing.T, path, body string, mode os.FileMode) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(body), mode); err != nil {
		t.Fatal(err)
	}
}

// gitIn runs git in dir and returns its output, trimmed.
func gitIn(t *testing.T, dir string, args ...string) string {
	t.Helper()
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("git %v: %v\n%s", args, err, out)
	}
	return strings.TrimSpace(string(out))
}

// commitAt commits everything in dir with the subject, made at date.
func commitAt(t *testing.T, dir, subject, date string) {
	t.Helper()
	gitIn(t, dir, "add", "-A")
	cmd := exec.Command("git", "commit", "-q", "--allow-empty", "--no-gpg-sign", "-m", subject)
	cmd.Dir = dir
	cmd.Env = append(os.Environ(), "GIT_AUTHOR_DATE="+date, "GIT_COMMITTER_DATE="+date)
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("commit %q: %v\n%s", subject, err, out)
	}
}

// fixtureAssets puts a release's assets where the fake gh downloads them
// from: binaries that print version as the tag and leave a ran-<name> file
// in the gh state folder, the install scripts, the licenses and their sums.
func (e *releaseEnv) fixtureAssets(t *testing.T, tag string) {
	t.Helper()
	assets := []string{"eva-md-darwin-arm64", "eva-md-darwin-amd64", "install.sh", "uninstall.sh", "LICENSES.txt"}
	var sums strings.Builder
	for _, a := range assets {
		body := "#!/bin/bash\necho " + a + "\n"
		if strings.HasPrefix(a, "eva-md-") {
			body = "#!/bin/sh\n: > '" + filepath.Join(e.state, "ran-"+a) + "'\necho 'eva-md " + tag + "'\necho 'schema_version 1'\n"
		}
		writeFile(t, filepath.Join(e.state, "assets", a), body, 0o644)
		fmt.Fprintf(&sums, "%x  %s\n", sha256.Sum256([]byte(body)), a)
	}
	writeFile(t, filepath.Join(e.state, "assets", "SHA256SUMS"), sums.String(), 0o644)
}

// releaseRun is what one run of the release script did.
type releaseRun struct {
	code                int
	stdout, stderr, tty string
}

// run runs scripts/release.sh from dir with args. With a terminal, the
// script gets a new pseudo-terminal as its controlling terminal, with input
// already typed on it, and tty is what showed on it: the echo of the input
// and what the script printed there. Its stdin then says yes, so only an
// answer typed on the terminal can count. Without one, it has no terminal
// at all.
func (e *releaseEnv) run(t *testing.T, dir string, terminal bool, input string, args ...string) releaseRun {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), releaseHangGuard)
	defer cancel()
	script, _ := filepath.Rel(dir, filepath.Join(e.dir, "scripts", "release.sh"))
	cmd := exec.CommandContext(ctx, "/bin/bash", append([]string{script}, args...)...)
	cmd.Dir = dir
	// Only a hang reaches this: nothing the script starts outlives it.
	cmd.WaitDelay = releaseHangGuard
	var stdout, stderr bytes.Buffer
	cmd.Stdout, cmd.Stderr = &stdout, &stderr
	var master *os.File
	shown := make(chan string, 1)
	if terminal {
		var slave *os.File
		master, slave = openPTY(t)
		defer master.Close()
		defer slave.Close()
		// The terminal is read all along: macOS holds the script's exit
		// until what it printed there has been read.
		go func() {
			var b bytes.Buffer
			buf := make([]byte, 1024)
			for {
				n, err := master.Read(buf)
				b.Write(buf[:n])
				if err != nil {
					shown <- b.String()
					return
				}
			}
		}()
		if input != "" {
			if _, err := master.Write([]byte(input)); err != nil {
				t.Fatalf("type on the terminal: %v", err)
			}
		}
		cmd.Stdin = strings.NewReader("y\n")
		// The terminal is the script's fd 3, made its controlling terminal.
		cmd.ExtraFiles = []*os.File{slave}
		cmd.SysProcAttr = &syscall.SysProcAttr{Setsid: true, Setctty: true, Ctty: 3}
	} else {
		// A new session has no controlling terminal, so /dev/tty can't open.
		cmd.SysProcAttr = &syscall.SysProcAttr{Setsid: true}
	}
	err := cmd.Run()
	if ctx.Err() != nil {
		t.Fatalf("the release script hung: %s\n%s", stdout.String(), stderr.String())
	}
	r := releaseRun{stdout: stdout.String(), stderr: stderr.String()}
	var exit *exec.ExitError
	switch {
	case err == nil:
	case errors.As(err, &exit):
		r.code = exit.ExitCode()
	default:
		t.Fatal(err)
	}
	if master != nil {
		// The script has ended, so all it printed has been read; this
		// only stops the reader.
		master.SetReadDeadline(time.Now())
		select {
		case r.tty = <-shown:
		case <-time.After(releaseHangGuard):
			t.Fatal("reading the terminal hung")
		}
	}
	return r
}

// openPTY opens a new pseudo-terminal and returns both its sides.
func openPTY(t *testing.T) (master, slave *os.File) {
	t.Helper()
	master, err := os.OpenFile("/dev/ptmx", os.O_RDWR|syscall.O_NOCTTY, 0)
	if err != nil {
		t.Fatalf("no pseudo-terminal: %v", err)
	}
	var name [128]byte
	ioctl := func(fd, req, arg uintptr) error {
		if _, _, errno := syscall.Syscall(syscall.SYS_IOCTL, fd, req, arg); errno != 0 {
			return errno
		}
		return nil
	}
	rc, err := master.SyscallConn()
	if err != nil {
		t.Fatal(err)
	}
	var ioErr error
	if err := rc.Control(func(fd uintptr) {
		if ioErr = ioctl(fd, syscall.TIOCPTYGRANT, 0); ioErr != nil {
			return
		}
		if ioErr = ioctl(fd, syscall.TIOCPTYUNLK, 0); ioErr != nil {
			return
		}
		ioErr = ioctl(fd, syscall.TIOCPTYGNAME, uintptr(unsafe.Pointer(&name[0])))
	}); err != nil || ioErr != nil {
		t.Fatalf("set up the pseudo-terminal: %v %v", err, ioErr)
	}
	path := string(name[:bytes.IndexByte(name[:], 0)])
	slave, err = os.OpenFile(path, os.O_RDWR|syscall.O_NOCTTY, 0)
	if err != nil {
		master.Close()
		t.Fatalf("open %s: %v", path, err)
	}
	return master, slave
}

// tags lists the tags in a repository.
func tags(t *testing.T, dir string) string {
	t.Helper()
	return gitIn(t, dir, "tag", "-l")
}

func (e *releaseEnv) ghCalls(t *testing.T) string {
	t.Helper()
	data, err := os.ReadFile(filepath.Join(e.state, "calls"))
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		t.Fatal(err)
	}
	return string(data)
}

// failed checks that the script stopped at a check with the problem and
// its fix, and that nothing was pushed: origin still has the tags it had.
func (e *releaseEnv) failed(t *testing.T, r releaseRun, problem, fix, originTags string) {
	t.Helper()
	if want := "  ✗ " + problem + "\n    Fix: " + fix + "\n"; r.code != 1 || r.stderr != want {
		t.Errorf("code %d, stderr %q, want %q\nstdout:\n%s", r.code, r.stderr, want, r.stdout)
	}
	if got := tags(t, e.origin); got != originTags {
		t.Errorf("origin's tags went from %q to %q", originTags, got)
	}
	// Only the question comes after the release is shown.
	if strings.Contains(r.stdout, "\nRelease ") != (problem == "there's no terminal to ask on") {
		t.Errorf("it stopped at the wrong place:\n%s", r.stdout)
	}
}

// Each check stops the release with what is wrong and the exact fix, and
// nothing is tagged or pushed.
func TestReleaseStopsAtTheFirstFailedCheck(t *testing.T) {
	for _, c := range []struct {
		name         string
		prepare      func(t *testing.T, e *releaseEnv) string // returns the folder to run from, or ""
		problem, fix string
	}{
		{"run from a subfolder", func(t *testing.T, e *releaseEnv) string { return filepath.Join(e.dir, "scripts") },
			"this isn't the checkout's root", `cd "DIR" and run scripts/release.sh`},
		{"another module", func(t *testing.T, e *releaseEnv) string {
			writeFile(t, filepath.Join(e.dir, "go.mod"), "module example.com/other\n", 0o644)
			commitAt(t, e.dir, "Rename", "2026-01-05T10:00:00Z")
			return ""
		}, "this checkout isn't eva-md", "run it from your eva-md checkout"},
		{"another branch", func(t *testing.T, e *releaseEnv) string {
			gitIn(t, e.dir, "switch", "-q", "-c", "topic")
			return ""
		}, "you're on topic, not main", "git switch main"},
		{"a detached HEAD", func(t *testing.T, e *releaseEnv) string {
			gitIn(t, e.dir, "switch", "-q", "--detach")
			return ""
		}, "you're on a detached HEAD, not main", "git switch main"},
		{"a changed file", func(t *testing.T, e *releaseEnv) string {
			writeFile(t, filepath.Join(e.dir, "README.md"), "changed\n", 0o644)
			return ""
		}, "the tree has changes or untracked files", "commit, stash or remove them (git status lists them), then run it again"},
		{"an untracked file", func(t *testing.T, e *releaseEnv) string {
			writeFile(t, filepath.Join(e.dir, "notes", "draft.txt"), "draft\n", 0o644)
			return ""
		}, "the tree has changes or untracked files", "commit, stash or remove them (git status lists them), then run it again"},
		{"no install script", func(t *testing.T, e *releaseEnv) string {
			gitIn(t, e.dir, "rm", "-q", "scripts/install.sh")
			commitAt(t, e.dir, "Drop the installer", "2026-01-05T10:00:00Z")
			return ""
		}, "main has no scripts/install.sh, which a release needs", "add it on main, push, and run it again"},
		{"no signing key", func(t *testing.T, e *releaseEnv) string {
			gitIn(t, e.dir, "config", "--global", "--unset", "user.signingkey")
			return ""
		}, "tag signing isn't set up: user.signingkey is empty", "git config --global user.signingkey <your key>, as docs/releasing.md says"},
		{"no signing program", func(t *testing.T, e *releaseEnv) string {
			gitIn(t, e.dir, "config", "--global", "gpg.ssh.program", filepath.Join(e.bin, "missing-keygen"))
			return ""
		}, "git signs ssh tags with BIN/missing-keygen, which isn't installed", "install it, or point gpg.ssh.program at the program you sign with"},
		{"an unknown signing format", func(t *testing.T, e *releaseEnv) string {
			gitIn(t, e.dir, "config", "--global", "gpg.format", "pgp2")
			return ""
		}, "gpg.format is pgp2, which git can't sign with", "git config --global gpg.format ssh, or openpgp"},
		{"no origin", func(t *testing.T, e *releaseEnv) string {
			gitIn(t, e.dir, "remote", "remove", "origin")
			return ""
		}, "this checkout has no origin", "git remote add origin git@github.com:heyNag/eva-md.git, then run it again"},
		{"origin elsewhere", func(t *testing.T, e *releaseEnv) string {
			// A mirror with the same commits, so only its URL tells.
			gitIn(t, e.dir, "config", "--global", "url."+e.origin+".insteadOf", "https://git.example.com/you/eva-md.git")
			gitIn(t, e.dir, "remote", "set-url", "origin", "https://git.example.com/you/eva-md.git")
			return ""
		}, "origin isn't github.com/heyNag/eva-md, where releases are made",
			"git remote set-url origin git@github.com:heyNag/eva-md.git (git remote -v shows where it points now), then run it again"},
		{"origin pushing elsewhere", func(t *testing.T, e *releaseEnv) string {
			gitIn(t, e.dir, "config", "remote.origin.pushurl", "git@git.example.com:you/eva-md.git")
			return ""
		}, "origin pushes somewhere other than github.com/heyNag/eva-md", "git config --unset-all remote.origin.pushurl, then run it again"},
		{"main ahead of origin", func(t *testing.T, e *releaseEnv) string {
			commitAt(t, e.dir, "Unpushed", "2026-01-05T10:00:00Z")
			return ""
		}, "main has commits origin/main doesn't", "git push origin main, wait for CI to pass, then run it again"},
		{"main behind origin", func(t *testing.T, e *releaseEnv) string {
			other := filepath.Join(filepath.Dir(e.dir), "other")
			gitIn(t, filepath.Dir(e.dir), "clone", "-q", e.origin, other)
			commitAt(t, other, "Elsewhere", "2026-01-05T10:00:00Z")
			gitIn(t, other, "push", "-q", "origin", "main")
			return ""
		}, "main is behind origin/main", "git pull --ff-only, then run it again"},
		{"main diverged from origin", func(t *testing.T, e *releaseEnv) string {
			other := filepath.Join(filepath.Dir(e.dir), "other")
			gitIn(t, filepath.Dir(e.dir), "clone", "-q", e.origin, other)
			commitAt(t, other, "Elsewhere", "2026-01-05T10:00:00Z")
			gitIn(t, other, "push", "-q", "origin", "main")
			commitAt(t, e.dir, "Here", "2026-01-05T11:00:00Z")
			return ""
		}, "main and origin/main have diverged", "bring main in line with origin/main (git pull --rebase), push, then run it again"},
		{"no gh", func(t *testing.T, e *releaseEnv) string {
			if err := os.Remove(filepath.Join(e.bin, "gh")); err != nil {
				t.Fatal(err)
			}
			return ""
		}, "gh, the GitHub CLI, isn't installed", "install it from https://cli.github.com, then gh auth login --hostname github.com"},
		{"gh signed out", func(t *testing.T, e *releaseEnv) string {
			writeFile(t, filepath.Join(e.state, "signed-out"), "", 0o600)
			return ""
		}, "gh isn't signed in to github.com", "gh auth login --hostname github.com"},
		{"no CI run", func(t *testing.T, e *releaseEnv) string {
			writeFile(t, filepath.Join(e.state, "ci-runs"), "", 0o600)
			return ""
		}, "CI hasn't run for SHORT", "push main and wait for CI: gh run list --repo heyNag/eva-md --workflow ci.yml"},
		{"CI running", func(t *testing.T, e *releaseEnv) string {
			writeFile(t, filepath.Join(e.state, "ci-runs"), "completed/failure/101\nin_progress//102\n", 0o600)
			return ""
		}, "CI is still running for SHORT", "gh run watch 102 --repo heyNag/eva-md, then run it again"},
		{"CI failed", func(t *testing.T, e *releaseEnv) string {
			writeFile(t, filepath.Join(e.state, "ci-runs"), "completed/failure/101\ncompleted/cancelled/100\n", 0o600)
			return ""
		}, "CI failed for SHORT", "fix it on main and push; release once CI passes"},
		{"already released", func(t *testing.T, e *releaseEnv) string {
			gitIn(t, e.dir, "tag", "-a", "--no-sign", "-m", "- Fix a typo", "2026.1.5")
			gitIn(t, e.dir, "push", "-q", "--no-follow-tags", "origin", "refs/tags/2026.1.5")
			return ""
		}, "SHORT is already released as 2026.1.5", "nothing to release; merge new work to main first"},
		{"tagged here only", func(t *testing.T, e *releaseEnv) string {
			// As after a push that failed once the tag was made.
			gitIn(t, e.dir, "tag", "-a", "--no-sign", "-m", "- Fix a typo", "2026.1.5")
			return ""
		}, "SHORT is tagged 2026.1.5 here, but origin doesn't have the tag",
			"git push --no-follow-tags origin refs/tags/2026.1.5, then check the release as docs/releasing.md says"},
		{"tagged here, and elsewhere on origin", func(t *testing.T, e *releaseEnv) string {
			gitIn(t, e.dir, "tag", "-a", "--no-sign", "-m", "- Fix a typo", "2026.1.5")
			tree := gitIn(t, e.origin, "rev-parse", "main^{tree}")
			elsewhere := gitIn(t, e.origin, "commit-tree", "-m", "Elsewhere", tree)
			gitIn(t, e.origin, "tag", "2026.1.5", elsewhere)
			return ""
		}, "the tag 2026.1.5 is on SHORT here but on another commit on origin",
			"a tag is never moved; look at both (git ls-remote --tags origin 2026.1.5), then merge new work to main and release that"},
		{"no terminal to ask on", func(t *testing.T, e *releaseEnv) string { return "" },
			"there's no terminal to ask on", "run it in a terminal, or pass --dry-run to only check"},
	} {
		t.Run(c.name, func(t *testing.T) {
			e := newReleaseEnv(t)
			dir := c.prepare(t, e)
			if dir == "" {
				dir = e.dir
			}
			short := gitIn(t, e.dir, "rev-parse", "--short", "HEAD")
			root := gitIn(t, e.dir, "rev-parse", "--show-toplevel")
			fix := strings.NewReplacer("DIR", root, "BIN", e.bin).Replace(c.fix)
			problem := strings.NewReplacer("SHORT", short, "BIN", e.bin).Replace(c.problem)
			before, beforeOrigin := tags(t, e.dir), tags(t, e.origin)
			r := e.run(t, dir, false, "")
			e.failed(t, r, problem, fix, beforeOrigin)
			if got := tags(t, e.dir); got != before {
				t.Errorf("tags here went from %q to %q", before, got)
			}
		})
	}
}

// The tag is the day of main's last commit in UTC, without zero padding,
// then .2 when that day's tag is taken here and .3 when .2 is taken on
// origin; the notes are the commits since the last release. A dry run shows
// it all and stops before asking or tagging.
func TestReleaseNamesTheTag(t *testing.T) {
	for _, c := range []struct {
		name    string
		prepare func(t *testing.T, e *releaseEnv)
		tag     string
		notes   string
	}{
		{"the first release", func(t *testing.T, e *releaseEnv) {}, "2026.1.5",
			"  notes, the first release:\n    - Fix a typo\n    - Add the release flow\n    - Start the project\n"},
		{"the day's tag taken here", func(t *testing.T, e *releaseEnv) {
			gitIn(t, e.dir, "tag", "-a", "--no-sign", "-m", "- Add the release flow", "2026.1.5", "HEAD~1")
		}, "2026.1.5.2", "  notes, since 2026.1.5:\n    - Fix a typo\n"},
		{"the day's .2 taken on origin", func(t *testing.T, e *releaseEnv) {
			gitIn(t, e.dir, "tag", "-a", "--no-sign", "-m", "- Start the project", "2026.1.5", "HEAD~2")
			// A commit only origin has, so fetching doesn't bring its tag here.
			tree := gitIn(t, e.origin, "rev-parse", "main^{tree}")
			elsewhere := gitIn(t, e.origin, "commit-tree", "-m", "Elsewhere", tree)
			gitIn(t, e.origin, "tag", "2026.1.5.2", elsewhere)
		}, "2026.1.5.3", "  notes, since 2026.1.5:\n    - Fix a typo\n    - Add the release flow\n"},
		{"a signed commit with log.showSignature on", func(t *testing.T, e *releaseEnv) {
			// git log and git show then print the signature check before
			// what they format, unless it is turned off.
			gitIn(t, e.dir, "config", "--global", "log.showSignature", "true")
			last := releaseCommits[len(releaseCommits)-1]
			cmd := exec.Command("git", "commit", "-q", "--amend", "--no-edit", "-S")
			cmd.Dir = e.dir
			cmd.Env = append(os.Environ(), "GIT_AUTHOR_DATE="+last.date, "GIT_COMMITTER_DATE="+last.date)
			if out, err := cmd.CombinedOutput(); err != nil {
				t.Fatalf("sign the commit: %v\n%s", err, out)
			}
			gitIn(t, e.dir, "push", "-q", "--force", "origin", "main")
			gitIn(t, e.dir, "fetch", "-q", "origin")
			if out := gitIn(t, e.dir, "log", "-1", "--format=%s"); !strings.Contains(out, "No signature") {
				t.Fatalf("git log shows no signature check: %q", out)
			}
			// Only the dry run's signing calls count.
			if err := os.Remove(filepath.Join(filepath.Dir(e.dir), "signer.log")); err != nil {
				t.Fatal(err)
			}
		}, "2026.1.5", "  notes, the first release:\n    - Fix a typo\n    - Add the release flow\n    - Start the project\n"},
	} {
		t.Run(c.name, func(t *testing.T) {
			e := newReleaseEnv(t)
			c.prepare(t, e)
			beforeHere, beforeOrigin := tags(t, e.dir), tags(t, e.origin)
			r := e.run(t, e.dir, true, "", "--dry-run")
			short := gitIn(t, e.dir, "rev-parse", "--short", "HEAD")
			want := "\nRelease " + c.tag + "\n  commit: " + short + " Fix a typo\n" + c.notes + "\nDry run: nothing was tagged or pushed.\n"
			if r.code != 0 || r.stderr != "" || !strings.HasSuffix(r.stdout, want) {
				t.Fatalf("code %d, stderr %q, stdout:\n%s\nwant it to end:\n%s", r.code, r.stderr, r.stdout, want)
			}
			if r.tty != "" {
				t.Errorf("a dry run asked: %q", r.tty)
			}
			if got := tags(t, e.dir); got != beforeHere {
				t.Errorf("tags here went from %q to %q", beforeHere, got)
			}
			if got := tags(t, e.origin); got != beforeOrigin {
				t.Errorf("origin's tags went from %q to %q", beforeOrigin, got)
			}
			if calls := e.ghCalls(t); strings.Contains(calls, "release.yml") || strings.Contains(calls, "run watch") || strings.Contains(calls, "release download") {
				t.Errorf("a dry run went on to the release:\n%s", calls)
			}
			if _, err := os.Stat(filepath.Join(filepath.Dir(e.dir), "signer.log")); err == nil {
				t.Error("a dry run signed something")
			}

			// A plan names the same tag and notes, and writes them for the
			// workflow.
			out := filepath.Join(t.TempDir(), "output")
			t.Setenv("GITHUB_OUTPUT", out)
			r = e.run(t, e.dir, false, "", "--plan")
			if want := strings.TrimSuffix(want, "Dry run: nothing was tagged or pushed.\n") + "Plan: nothing was tagged or pushed.\n"; r.code != 0 || r.stderr != "" || !strings.HasSuffix(r.stdout, want) {
				t.Fatalf("plan: code %d, stderr %q, stdout:\n%s\nwant it to end:\n%s", r.code, r.stderr, r.stdout, want)
			}
			lines := strings.Split(strings.TrimSuffix(c.notes, "\n"), "\n")
			since := strings.TrimSuffix(strings.TrimPrefix(lines[0], "  notes, "), ":")
			for i := range lines[1:] {
				lines[i+1] = strings.TrimPrefix(lines[i+1], "    ")
			}
			planned(t, out, map[string]string{"tag": c.tag, "commit": gitIn(t, e.dir, "rev-parse", "HEAD"),
				"since": since, "notes": strings.Join(lines[1:], "\n")})
			if got := tags(t, e.dir); got != beforeHere {
				t.Errorf("a plan made tags here: %q, not %q", got, beforeHere)
			}
			if got := tags(t, e.origin); got != beforeOrigin {
				t.Errorf("a plan made tags on origin: %q, not %q", got, beforeOrigin)
			}
		})
	}
}

// planned checks what a plan wrote to the workflow's output file, read as
// GitHub reads it: name=value lines, and name<<delimiter, the lines of the
// value, then the delimiter alone on a line. The notes' delimiter is random,
// so no note can end them early.
func planned(t *testing.T, path string, want map[string]string) {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	got := map[string]string{}
	lines := strings.Split(string(data), "\n")
	if lines[len(lines)-1] != "" {
		t.Fatalf("the output doesn't end with a newline:\n%s", data)
	}
	for i := 0; i < len(lines)-1; i++ {
		if name, delim, ok := strings.Cut(lines[i], "<<"); ok {
			if !regexp.MustCompile(`^EVA_MD_NOTES_[0-9a-f]{32}$`).MatchString(delim) {
				t.Errorf("the delimiter %q isn't random", delim)
			}
			var value []string
			for i++; i < len(lines)-1 && lines[i] != delim; i++ {
				value = append(value, lines[i])
			}
			if i == len(lines)-1 {
				t.Fatalf("%s's value doesn't end:\n%s", name, data)
			}
			got[name] = strings.Join(value, "\n")
		} else if name, value, ok := strings.Cut(lines[i], "="); ok {
			got[name] = value
		} else {
			t.Fatalf("GitHub can't read the line %q in:\n%s", lines[i], data)
		}
	}
	if !maps.Equal(got, want) {
		t.Errorf("the plan wrote %q, not %q", got, want)
	}
}

// A plan, for the Cut a release workflow, needs no signing setup and no
// terminal: it makes every other check, shows the release and writes it for
// the workflow, then stops with nothing tagged, pushed or signed. Without a
// workflow's output file it only shows the release.
func TestReleasePlanNeedsNoSigningOrTerminal(t *testing.T) {
	e := newReleaseEnv(t)
	gitIn(t, e.dir, "config", "--global", "--unset", "user.signingkey")
	gitIn(t, e.dir, "config", "--global", "gpg.format", "pgp2")
	out := filepath.Join(t.TempDir(), "output")
	// What the workflow's earlier steps wrote stays.
	writeFile(t, out, "earlier=step\n", 0o600)
	t.Setenv("GITHUB_OUTPUT", out)
	r := e.run(t, e.dir, false, "", "--plan")
	head := gitIn(t, e.dir, "rev-parse", "HEAD")
	want := "  ✓ gh can read github.com/heyNag/eva-md\n  ✓ CI passed for " + head[:7] + "\n  ✓ " + head[:7] + " isn't released yet\n" +
		"\nRelease 2026.1.5\n  commit: " + head[:7] + " Fix a typo\n  notes, the first release:\n    - Fix a typo\n    - Add the release flow\n    - Start the project\n" +
		"\nPlan: nothing was tagged or pushed.\n"
	if r.code != 0 || r.stderr != "" || !strings.HasSuffix(r.stdout, want) || strings.Contains(r.stdout, "signing") {
		t.Fatalf("code %d, stderr %q, stdout:\n%s\nwant it to end:\n%s", r.code, r.stderr, r.stdout, want)
	}
	planned(t, out, map[string]string{"earlier": "step", "tag": "2026.1.5", "commit": head, "since": "the first release",
		"notes": "- Fix a typo\n- Add the release flow\n- Start the project"})
	if here, there := tags(t, e.dir), tags(t, e.origin); here != "" || there != "" {
		t.Errorf("tags here %q, on origin %q", here, there)
	}
	if _, err := os.Stat(filepath.Join(filepath.Dir(e.dir), "signer.log")); err == nil {
		t.Error("a plan signed something")
	}
	calls := e.ghCalls(t)
	if want := "api repos/heyNag/eva-md --silent\nrun list --repo heyNag/eva-md --workflow ci.yml --commit " + head + " "; !strings.HasPrefix(calls, want) || strings.Count(calls, "\n") != 2 {
		t.Errorf("gh calls:\n%s\nwant two, starting:\n%s", calls, want)
	}

	t.Setenv("GITHUB_OUTPUT", "")
	if err := os.Unsetenv("GITHUB_OUTPUT"); err != nil {
		t.Fatal(err)
	}
	r = e.run(t, e.dir, false, "", "--plan")
	if r.code != 0 || r.stderr != "" || !strings.HasSuffix(r.stdout, want) {
		t.Errorf("without an output file: code %d, stderr %q, stdout:\n%s", r.code, r.stderr, r.stdout)
	}
}

// A plan stops at the checks a release makes, with the same problem and fix,
// and writes nothing for the workflow; only the check that gh works is its
// own, since a workflow's gh signs in with a token. A plan is never also a
// dry run.
func TestReleasePlanStopsAtTheFirstFailedCheck(t *testing.T) {
	for _, c := range []struct {
		name         string
		prepare      func(t *testing.T, e *releaseEnv)
		problem, fix string
	}{
		{"another branch", func(t *testing.T, e *releaseEnv) { gitIn(t, e.dir, "switch", "-q", "-c", "topic") },
			"you're on topic, not main", "git switch main"},
		{"main behind origin", func(t *testing.T, e *releaseEnv) {
			other := filepath.Join(filepath.Dir(e.dir), "other")
			gitIn(t, filepath.Dir(e.dir), "clone", "-q", e.origin, other)
			commitAt(t, other, "Elsewhere", "2026-01-05T10:00:00Z")
			gitIn(t, other, "push", "-q", "origin", "main")
		}, "main is behind origin/main", "git pull --ff-only, then run it again"},
		{"gh can't read the repository", func(t *testing.T, e *releaseEnv) {
			writeFile(t, filepath.Join(e.state, "signed-out"), "", 0o600)
		}, "gh can't read github.com/heyNag/eva-md", "set GH_TOKEN to a token that can read heyNag/eva-md and its Actions runs"},
		{"CI failed", func(t *testing.T, e *releaseEnv) {
			writeFile(t, filepath.Join(e.state, "ci-runs"), "completed/failure/101\n", 0o600)
		}, "CI failed for SHORT", "fix it on main and push; release once CI passes"},
		{"already released", func(t *testing.T, e *releaseEnv) {
			gitIn(t, e.dir, "tag", "-a", "--no-sign", "-m", "- Fix a typo", "2026.1.5")
			gitIn(t, e.dir, "push", "-q", "--no-follow-tags", "origin", "refs/tags/2026.1.5")
		}, "SHORT is already released as 2026.1.5", "nothing to release; merge new work to main first"},
	} {
		t.Run(c.name, func(t *testing.T) {
			e := newReleaseEnv(t)
			c.prepare(t, e)
			out := filepath.Join(t.TempDir(), "output")
			t.Setenv("GITHUB_OUTPUT", out)
			short := gitIn(t, e.dir, "rev-parse", "--short", "HEAD")
			r := e.run(t, e.dir, false, "", "--plan")
			e.failed(t, r, strings.ReplaceAll(c.problem, "SHORT", short), c.fix, tags(t, e.origin))
			if _, err := os.Stat(out); !errors.Is(err, os.ErrNotExist) {
				t.Errorf("a failed plan wrote the output: %v", err)
			}
		})
	}
	e := newReleaseEnv(t)
	for _, args := range [][]string{{"--plan", "--dry-run"}, {"--dry-run", "--plan"}, {"--plan", "--plan"}} {
		r := e.run(t, e.dir, false, "", args...)
		if r.code != 2 || !strings.HasPrefix(r.stderr, "usage: scripts/release.sh [--dry-run | --plan]\n") || r.stdout != "" {
			t.Errorf("%v: code %d, stderr %q, stdout %q", args, r.code, r.stderr, r.stdout)
		}
	}
}

// Anything but yes at the question, Enter and Control-D included, releases
// nothing.
func TestReleaseDeclinedMakesNothing(t *testing.T) {
	for _, input := range []string{"n\n", "\n", "nope\n", "\x04"} {
		e := newReleaseEnv(t)
		r := e.run(t, e.dir, true, input)
		if r.code != 1 || r.stderr != "" || !strings.HasSuffix(r.stdout, "Not released: nothing was tagged or pushed.\n") {
			t.Errorf("%q: code %d, stderr %q, stdout:\n%s", input, r.code, r.stderr, r.stdout)
		}
		short := gitIn(t, e.dir, "rev-parse", "--short", "HEAD")
		if !strings.Contains(r.tty, "Release 2026.1.5 from "+short+"? [y/N] ") {
			t.Errorf("%q: the terminal showed %q", input, r.tty)
		}
		if here, there := tags(t, e.dir), tags(t, e.origin); here != "" || there != "" {
			t.Errorf("%q: tags here %q, on origin %q", input, here, there)
		}
		if calls := e.ghCalls(t); strings.Contains(calls, "release.yml") {
			t.Errorf("%q: it looked for the release workflow:\n%s", input, calls)
		}
	}
}

// Yes makes the signed, annotated tag with the notes as its message, pushes
// only that tag, watches the release workflow's run for it and checks what
// it published: the sums, the binary's version and the build provenance.
func TestReleasePublishesTheTag(t *testing.T) {
	e := newReleaseEnv(t)
	// A tag of another kind, which push.followTags would otherwise send too.
	gitIn(t, e.dir, "tag", "-a", "--no-sign", "-m", "a note", "local-note", "HEAD~1")
	e.fixtureAssets(t, "2026.1.5")
	head := gitIn(t, e.dir, "rev-parse", "HEAD")
	originMain := gitIn(t, e.origin, "rev-parse", "main")
	// origin records every ref update it receives from here on, even one
	// that leaves a ref where it was.
	updates := filepath.Join(filepath.Dir(e.dir), "updates")
	writeFile(t, filepath.Join(e.origin, "hooks", "pre-receive"), "#!/bin/bash\ncat >> '"+updates+"'\n", 0o700)
	if err := os.Remove(e.pushLog); err != nil {
		t.Fatal(err)
	}
	r := e.run(t, e.dir, true, "y\n")
	want := "\nReleased 2026.1.5: https://github.com/heyNag/eva-md/releases/tag/2026.1.5\nInstall it with:\n  curl -fsSL https://github.com/heyNag/eva-md/releases/latest/download/install.sh | bash\n"
	if r.code != 0 || r.stderr != "" || !strings.HasSuffix(r.stdout, want) {
		t.Fatalf("code %d, stderr %q, stdout:\n%s", r.code, r.stderr, r.stdout)
	}
	for _, line := range []string{"  ✓ made the signed tag 2026.1.5\n", "  ✓ pushed 2026.1.5 to origin\n", "  ✓ the release workflow passed\n",
		"  ✓ SHA256SUMS matches the assets\n", "  ✓ the build provenance verifies\n"} {
		if !strings.Contains(r.stdout, line) {
			t.Errorf("no %q in:\n%s", line, r.stdout)
		}
	}
	if !strings.Contains(r.tty, "Release 2026.1.5 from "+head[:7]+"? [y/N] ") {
		t.Errorf("the terminal showed %q", r.tty)
	}

	// Only the release tag reached origin, and main there didn't move.
	if got := gitIn(t, e.origin, "for-each-ref", "--format=%(refname)", "refs/tags"); got != "refs/tags/2026.1.5" {
		t.Errorf("origin's tags: %q", got)
	}
	if got := gitIn(t, e.origin, "rev-parse", "main"); got != originMain {
		t.Errorf("origin's main moved to %s", got)
	}
	// One push, of the tag alone and without force: main can't be sent along
	// even where it wouldn't move.
	if pushes, err := os.ReadFile(e.pushLog); err != nil || string(pushes) != "push --quiet --no-follow-tags origin refs/tags/2026.1.5\n" {
		t.Errorf("pushes %q %v", pushes, err)
	}
	tagObject := gitIn(t, e.origin, "rev-parse", "refs/tags/2026.1.5")
	if got, err := os.ReadFile(updates); err != nil || string(got) != strings.Repeat("0", len(head))+" "+tagObject+" refs/tags/2026.1.5\n" {
		t.Errorf("origin received %q %v", got, err)
	}
	tag := gitIn(t, e.origin, "cat-file", "-p", "2026.1.5")
	wantTag := "object " + head + "\ntype commit\ntag 2026.1.5\n"
	if !strings.HasPrefix(tag, wantTag) || !strings.Contains(tag, "\n\n- Fix a typo\n- Add the release flow\n- Start the project\n-----BEGIN SSH SIGNATURE-----\n") {
		t.Errorf("the tag:\n%s", tag)
	}
	signed, err := os.ReadFile(filepath.Join(filepath.Dir(e.dir), "signer.log"))
	if err != nil || strings.Count(string(signed), "-Y sign -n git") != 1 {
		t.Errorf("signing calls %q %v", signed, err)
	}

	calls := e.ghCalls(t)
	for _, call := range []string{
		"run list --repo heyNag/eva-md --workflow ci.yml --commit " + head + " ",
		"run list --repo heyNag/eva-md --workflow release.yml --branch 2026.1.5 --commit " + head + " --event push ",
		"run watch 4242 --repo heyNag/eva-md --exit-status\n",
		"release download 2026.1.5 --repo heyNag/eva-md --dir ",
	} {
		if !strings.Contains(calls, call) {
			t.Errorf("no gh %q in:\n%s", call, calls)
		}
	}
	for _, a := range []string{"eva-md-darwin-arm64", "eva-md-darwin-amd64", "install.sh", "uninstall.sh", "LICENSES.txt"} {
		if !strings.Contains(calls, "/"+a+" --repo heyNag/eva-md --signer-workflow heyNag/eva-md/.github/workflows/release.yml\n") {
			t.Errorf("%s's provenance wasn't verified:\n%s", a, calls)
		}
	}
	bin := "eva-md-darwin-arm64"
	if out, _ := exec.Command("uname", "-m").Output(); strings.TrimSpace(string(out)) == "x86_64" {
		bin = "eva-md-darwin-amd64"
	}
	if _, err := os.Stat(filepath.Join(e.state, "ran-"+bin)); err != nil {
		t.Errorf("%s's version wasn't run: %v", bin, err)
	}
	if entries, err := os.ReadDir(os.Getenv("TMPDIR")); err != nil || len(entries) != 0 {
		t.Errorf("left in the temporary folder: %v %v", entries, err)
	}
}

// When the workflow fails or what it published doesn't check out, the
// script stops with the fix, and the pushed tag stays where it is.
func TestReleaseStopsWhenThePublishedReleaseIsWrong(t *testing.T) {
	draft := "make the release a draft (gh release edit 2026.1.5 --repo heyNag/eva-md --draft=true) and release a fix"
	for _, c := range []struct {
		name         string
		prepare      func(t *testing.T, e *releaseEnv)
		problem, fix string
	}{
		{"the workflow failed", func(t *testing.T, e *releaseEnv) {
			writeFile(t, filepath.Join(e.state, "watch-exit"), "1", 0o600)
		}, "the release workflow failed for 2026.1.5",
			"read its log (gh run view 4242 --repo heyNag/eva-md --log-failed); the tag stays, so fix main and release again under the next tag"},
		{"a missing asset", func(t *testing.T, e *releaseEnv) {
			if err := os.Remove(filepath.Join(e.state, "assets", "uninstall.sh")); err != nil {
				t.Fatal(err)
			}
		}, "the release 2026.1.5 has no uninstall.sh", draft},
		{"a sum missing", func(t *testing.T, e *releaseEnv) {
			sums := filepath.Join(e.state, "assets", "SHA256SUMS")
			data, err := os.ReadFile(sums)
			if err != nil {
				t.Fatal(err)
			}
			var kept []string
			for _, line := range strings.SplitAfter(string(data), "\n") {
				if !strings.HasSuffix(line, "  uninstall.sh\n") {
					kept = append(kept, line)
				}
			}
			writeFile(t, sums, strings.Join(kept, ""), 0o644)
		}, "the release's SHA256SUMS doesn't match its assets", draft},
		{"a sum too many", func(t *testing.T, e *releaseEnv) {
			// The extra file is there too, so its sum checks out.
			body := "extra\n"
			writeFile(t, filepath.Join(e.state, "assets", "extra.txt"), body, 0o644)
			sums := filepath.Join(e.state, "assets", "SHA256SUMS")
			data, err := os.ReadFile(sums)
			if err != nil {
				t.Fatal(err)
			}
			writeFile(t, sums, string(data)+fmt.Sprintf("%x  extra.txt\n", sha256.Sum256([]byte(body))), 0o644)
		}, "the release's SHA256SUMS doesn't match its assets", draft},
		{"a changed asset", func(t *testing.T, e *releaseEnv) {
			writeFile(t, filepath.Join(e.state, "assets", "install.sh"), "#!/bin/bash\necho changed\n", 0o644)
		}, "the release's SHA256SUMS doesn't match its assets", draft},
		{"another version", func(t *testing.T, e *releaseEnv) {
			e.fixtureAssets(t, "2026.1.4")
		}, `BINARY says "eva-md 2026.1.4", not "eva-md 2026.1.5"`, draft},
		{"no provenance", func(t *testing.T, e *releaseEnv) {
			writeFile(t, filepath.Join(e.state, "attest-fails"), "", 0o600)
		}, "eva-md-darwin-arm64's build provenance doesn't verify",
			"gh attestation verify eva-md-darwin-arm64 --repo heyNag/eva-md shows why; make the release a draft and release a fix"},
	} {
		t.Run(c.name, func(t *testing.T) {
			e := newReleaseEnv(t)
			e.fixtureAssets(t, "2026.1.5")
			c.prepare(t, e)
			r := e.run(t, e.dir, true, "y\n")
			bin := "eva-md-darwin-arm64"
			if out, _ := exec.Command("uname", "-m").Output(); strings.TrimSpace(string(out)) == "x86_64" {
				bin = "eva-md-darwin-amd64"
			}
			want := "  ✗ " + strings.ReplaceAll(c.problem, "BINARY", bin) + "\n    Fix: " + c.fix + "\n"
			if r.code != 1 || r.stderr != want {
				t.Errorf("code %d, stderr %q, want %q\nstdout:\n%s", r.code, r.stderr, want, r.stdout)
			}
			if got := tags(t, e.origin); got != "2026.1.5" {
				t.Errorf("origin's tags: %q", got)
			}
			if got := tags(t, e.dir); got != "2026.1.5" {
				t.Errorf("tags here: %q", got)
			}
			// Nothing downloaded runs before its provenance verifies.
			if ran, _ := filepath.Glob(filepath.Join(e.state, "ran-*")); c.name == "no provenance" && len(ran) != 0 {
				t.Errorf("ran before its provenance verified: %v", ran)
			}
		})
	}
}

// The git environment go test runs in, as in a git hook, reaches neither the
// test repos nor the script: not an index, not config given with git -c.
func TestReleaseIgnoresTheOuterGitEnvironment(t *testing.T) {
	outer := filepath.Join(t.TempDir(), "outer-index")
	t.Setenv("GIT_INDEX_FILE", outer)
	t.Setenv("GIT_CONFIG_COUNT", "1")
	t.Setenv("GIT_CONFIG_KEY_0", "gpg.format")
	t.Setenv("GIT_CONFIG_VALUE_0", "pgp2")
	t.Setenv("GIT_CONFIG_PARAMETERS", "'user.signingkey'=''")
	e := newReleaseEnv(t)
	r := e.run(t, e.dir, false, "", "--dry-run")
	if r.code != 0 || r.stderr != "" || !strings.HasSuffix(r.stdout, "Dry run: nothing was tagged or pushed.\n") {
		t.Errorf("code %d, stderr %q, stdout:\n%s", r.code, r.stderr, r.stdout)
	}
	if _, err := os.Stat(outer); !errors.Is(err, os.ErrNotExist) {
		t.Errorf("git wrote the outer index: %v", err)
	}
}
