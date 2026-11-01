package coderef

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"time"
	"unicode"

	"github.com/heyNag/eva-md/internal/identity"
	"github.com/heyNag/eva-md/internal/redact"
)

// Bounds of a git call's output, in bytes: past maxOut a call fails, and
// only the start of stderr is kept, to sort a failure, never to show.
const (
	maxOut = 64 << 10
	maxErr = 4 << 10
)

var (
	// ErrNoCommit is a commit the clone doesn't have, as one not fetched,
	// rebased away or cut off by a shallow clone.
	ErrNoCommit = errors.New("coderef: the commit is not in the clone")
	// ErrTimeout is a git call that ran out of time; it was killed.
	ErrTimeout = errors.New("coderef: git ran out of time")
)

// WaitDelay bounds the wait for a git call's output once git has exited or
// been killed. Tests raise it before any test runs, so a slow machine can't
// fail a test of what a measurement finds.
var WaitDelay = 100 * time.Millisecond

// exitError is a git call that ran and exited non-zero.
type exitError struct {
	code   int
	stderr string
}

func (e *exitError) Error() string { return fmt.Sprintf("coderef: git exited with %d", e.code) }

// git runs one read-only git command in root, hardened as identity's calls
// are: fsmonitor and hooks off, no optional locks, and the environment of
// identity.GitEnv, which turns lazy fetches off; in a clone that may be
// partial, --no-lazy-fetch too (see Partial). No signature is checked, and
// pathspecs are read literally, so a cited path never acts as a pattern or
// as magic. It returns stdout without its final newline. A call still
// running when ctx ends is killed with any process it started, and returns
// ErrTimeout.
func git(ctx context.Context, root string, args ...string) (string, error) {
	full := []string{
		"-c", "core.fsmonitor=false",
		"-c", "core.hooksPath=/dev/null",
		"-c", "log.showSignature=false",
		"--no-optional-locks",
		"--literal-pathspecs",
	}
	if Partial(root) {
		full = append(full, "--no-lazy-fetch")
	}
	cmd := exec.CommandContext(ctx, "git", append(append(full, "-C", root), args...)...)
	cmd.Env = identity.GitEnv()
	cmd.Dir = "/"
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	cmd.Cancel = func() error { return KillGroup(cmd.Process.Pid) }
	cmd.WaitDelay = WaitDelay
	out := capped{limit: maxOut}
	stderr := capped{limit: maxErr}
	cmd.Stdout, cmd.Stderr = &out, &stderr
	err := cmd.Run()
	switch {
	case err != nil && ctx.Err() != nil:
		return "", ErrTimeout
	case err != nil:
		var ee *exec.ExitError
		if errors.As(err, &ee) {
			return "", &exitError{code: ee.ExitCode(), stderr: stderr.buf.String()}
		}
		return "", fmt.Errorf("coderef: git %s: %w", args[0], err)
	case out.cut:
		return "", fmt.Errorf("coderef: git %s wrote over %d KB", args[0], maxOut>>10)
	}
	return strings.TrimSuffix(out.buf.String(), "\n"), nil
}

// KillGroup kills every process in the group pid leads, as a git call run
// in its own group does at its deadline.
func KillGroup(pid int) error {
	err := syscall.Kill(-pid, syscall.SIGKILL)
	if errors.Is(err, syscall.ESRCH) {
		return os.ErrProcessDone
	}
	return err
}

// maxConfig bounds each file Partial reads, in bytes.
const maxConfig = 64 << 10

// Partial reports whether the clone at root may be a partial clone, whose
// git fetches an object it lacks from a promisor remote when a call reads
// it: its config names a promisor, a partial clone or an include, or its git
// folder is not in the form git writes. It reads the files, never starting
// git. git ignores GIT_NO_LAZY_FETCH before 2.44, so its calls in such a
// clone also pass --no-lazy-fetch, which an older git refuses: the call
// fails rather than fetch.
func Partial(root string) bool {
	gitdir := filepath.Join(root, ".git")
	info, err := os.Lstat(gitdir)
	switch {
	case err != nil:
		return true
	case info.Mode().IsRegular():
		data, err := small(gitdir)
		target, ok := strings.CutPrefix(strings.TrimRight(string(data), "\r\n"), "gitdir: ")
		if err != nil || !ok || target == "" {
			return true
		}
		if !filepath.IsAbs(target) {
			target = filepath.Join(root, target)
		}
		gitdir = target
	case !info.IsDir():
		return true
	}
	common := gitdir
	switch data, err := small(filepath.Join(gitdir, "commondir")); {
	case err == nil:
		if common = strings.TrimRight(string(data), "\r\n"); !filepath.IsAbs(common) {
			common = filepath.Join(gitdir, common)
		}
	case !errors.Is(err, fs.ErrNotExist):
		return true
	}
	for _, file := range []string{filepath.Join(common, "config"), filepath.Join(gitdir, "config.worktree")} {
		data, err := small(file)
		if errors.Is(err, fs.ErrNotExist) {
			continue
		}
		if err != nil || partialConfig(string(data)) {
			return true
		}
	}
	return false
}

// partialConfig reports whether a git config file may make its clone
// partial: it has an include or includeIf section, a promisor or
// partialclonefilter key in any section, or extensions.partialclone, by
// their names, never by a word in a value such as a remote's URL. A line
// continued with a backslash, or a section header it can't read, may too.
// A byte order mark that starts the file is skipped, as git skips it.
func partialConfig(config string) bool {
	config = strings.TrimPrefix(config, "\ufeff")
	section := ""
	for _, line := range strings.Split(config, "\n") {
		line = strings.TrimSpace(line)
		if strings.HasSuffix(line, "\\") {
			return true
		}
		if strings.HasPrefix(line, "[") {
			end := strings.IndexByte(line, ']')
			if quotes := strings.Count(line[:max(end, 0)], "\""); end < 0 || quotes != 0 && quotes != 2 || strings.Contains(line[:end], "\\") {
				return true
			}
			section = strings.ToLower(strings.TrimSpace(line[1:end]))
			if strings.HasPrefix(section, "include") {
				return true
			}
			// A key may follow its header on the same line.
			line = strings.TrimSpace(line[end+1:])
		}
		key := line
		if i := strings.IndexAny(line, " \t=#;"); i >= 0 {
			key = line[:i]
		}
		switch strings.ToLower(key) {
		case "promisor", "partialclonefilter":
			return true
		case "partialclone":
			if section == "extensions" {
				return true
			}
		}
	}
	return false
}

// small reads a regular file of at most maxConfig bytes, never waiting on a
// FIFO.
func small(path string) ([]byte, error) {
	f, err := os.OpenFile(path, os.O_RDONLY|syscall.O_NONBLOCK, 0)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	if info, err := f.Stat(); err != nil || !info.Mode().IsRegular() || info.Size() > maxConfig {
		return nil, errors.New("coderef: not a regular file within its bound")
	}
	data, err := io.ReadAll(io.LimitReader(f, maxConfig+1))
	if err == nil && len(data) > maxConfig {
		err = errors.New("coderef: not a regular file within its bound")
	}
	return data, err
}

// capped keeps what is written up to its limit, and notes a cut.
type capped struct {
	buf   bytes.Buffer
	limit int
	cut   bool
}

func (c *capped) Write(p []byte) (int, error) {
	n := len(p)
	if left := c.limit - c.buf.Len(); len(p) > left {
		p, c.cut = p[:left], true
	}
	c.buf.Write(p)
	return n, nil
}

// HasCommit reports whether the clone at root has commit.
func HasCommit(ctx context.Context, root, commit string) (bool, error) {
	_, err := git(ctx, root, "cat-file", "-e", commit+"^{commit}")
	var ee *exitError
	switch {
	case err == nil:
		return true, nil
	case errors.As(err, &ee) && (ee.code == 1 || ee.code == 128):
		return false, nil
	}
	return false, err
}

// Commits counts the commits on HEAD's history since commit that touch
// paths, or any path when there are none, up to MaxCount. For a commit that
// is not on HEAD's history, git counts those since the two have in common. A
// commit the clone lacks is ErrNoCommit.
func Commits(ctx context.Context, root, commit string, paths []string) (Movement, error) {
	args := append([]string{"rev-list", "--count", "--max-count=" + strconv.Itoa(MaxCount+1), commit + "..HEAD", "--"}, paths...)
	out, err := git(ctx, root, args...)
	var ee *exitError
	if errors.As(err, &ee) && ee.code == 128 && (strings.Contains(ee.stderr, "bad revision") || strings.Contains(ee.stderr, "unknown revision") || strings.Contains(ee.stderr, "Invalid revision range")) {
		return Movement{}, ErrNoCommit
	}
	if err != nil {
		return Movement{}, err
	}
	n, err := strconv.Atoi(out)
	switch {
	case err != nil || n < 0:
		return Movement{}, errors.New("coderef: git counted no number")
	case n > MaxCount:
		return Movement{Count: MaxCount, Capped: true}, nil
	}
	return Movement{Count: n}, nil
}

// Missing returns the paths rev's tree lacks, read in one call. A file or a
// folder is there when ls-tree lists it or, for a folder, a path inside it.
func Missing(ctx context.Context, root, rev string, paths []string) ([]string, error) {
	if len(paths) == 0 {
		return nil, nil
	}
	out, err := git(ctx, root, append([]string{"ls-tree", "-z", "--name-only", rev, "--"}, paths...)...)
	if err != nil {
		return nil, err
	}
	listed := strings.Split(out, "\x00")
	var missing []string
	for _, p := range paths {
		found := false
		for _, l := range listed {
			if l == p || strings.HasPrefix(l, p+"/") {
				found = true
				break
			}
		}
		if !found {
			missing = append(missing, p)
		}
	}
	return missing, nil
}

// Head returns the commit HEAD names and the branch checked out, "" for a
// detached HEAD.
func Head(ctx context.Context, root string) (commit, branch string, err error) {
	out, err := git(ctx, root, "rev-parse", "HEAD", "--symbolic-full-name", "HEAD")
	if err != nil {
		return "", "", err
	}
	commit, name, ok := strings.Cut(out, "\n")
	if !ok || commit == "" {
		return "", "", errors.New("coderef: git named no HEAD")
	}
	if name == "HEAD" {
		return commit, "", nil
	}
	return commit, strings.TrimPrefix(name, "refs/heads/"), nil
}

// Ancestor reports whether commit is on HEAD's history.
func Ancestor(ctx context.Context, root, commit string) (bool, error) {
	_, err := git(ctx, root, "merge-base", "--is-ancestor", commit, "HEAD")
	var ee *exitError
	if errors.As(err, &ee) && ee.code == 1 {
		return false, nil
	}
	return err == nil, err
}

// Last returns the day and subject of the newest commit since commit that
// touches paths, or "" when none does. The subject is redacted, made one
// line and cut to 80 characters; it is printed, never stored.
func Last(ctx context.Context, root, commit string, paths []string) (day, subject string, err error) {
	args := append([]string{"log", "-1", "--format=%cs%x09%s", commit + "..HEAD", "--"}, paths...)
	out, err := git(ctx, root, args...)
	if err != nil || out == "" {
		return "", "", err
	}
	// The line is the output's last, and its day a day, whatever git
	// printed before it.
	day, subject, _ = strings.Cut(out[strings.LastIndexByte(out, '\n')+1:], "\t")
	if _, err := time.Parse(time.DateOnly, day); err != nil {
		return "", "", nil
	}
	subject, _ = redact.Redact(subject)
	subject = strings.Join(strings.Fields(strings.Map(func(r rune) rune {
		if unicode.IsControl(r) || unicode.In(r, unicode.Cf, unicode.Zl, unicode.Zp) {
			return ' '
		}
		return r
	}, subject)), " ")
	if r := []rune(subject); len(r) > 80 {
		subject = string(r[:79]) + "…"
	}
	return day, subject, nil
}
