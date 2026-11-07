package resume

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/heyNag/eva-md/internal/coderef"
	"github.com/heyNag/eva-md/internal/identity"
)

type gitInfo struct {
	text    string
	changed bool
	head    string // the commit HEAD names, when the summary read it
}

var objectID = regexp.MustCompile(`^[a-fA-F0-9]{7,64}$`)

// gitReserve is the time left after live git for the rest of the brief, the
// briefed marker and the hook's output. Tests raise it before any test runs,
// so a slow machine can't fail a test of a brief that must beat its deadline.
var gitReserve = 50 * time.Millisecond

// GitTimeout bounds live git for one brief. Tests raise it before any test
// runs, so a slow machine can't fail a test of what the brief says.
var GitTimeout = 800 * time.Millisecond

// GitWaitDelay bounds the wait for a git call's output once git has exited
// or been stopped. Tests raise it before any test runs, as they raise
// GitTimeout.
var GitWaitDelay = 100 * time.Millisecond

func gitSummary(parent context.Context, p identity.Project, head string) gitInfo {
	if p.Root == "" {
		return gitInfo{text: "Live git: not a repository"}
	}
	// Slow git is reported as unknown. It must not use up the time the hook
	// needs to deliver the brief.
	timeout := GitTimeout
	if deadline, ok := parent.Deadline(); ok && time.Until(deadline)-gitReserve < timeout {
		timeout = time.Until(deadline) - gitReserve
	}
	ctx, cancel := context.WithTimeout(parent, timeout)
	defer cancel()
	status, err := git(ctx, p.Root, "status", "--porcelain=v2", "--branch", "--untracked-files=normal")
	if err != nil {
		return gitInfo{text: "Live git: unknown (unavailable or deadline)"}
	}
	parts := []string{"Live git:"}
	var changed []string
	for _, line := range strings.Split(status, "\n") {
		if strings.HasPrefix(line, "? ") && len(changed) < 15 {
			changed = append(changed, strings.TrimPrefix(line, "? "))
		}
		if strings.HasPrefix(line, "# branch.head ") {
			parts = append(parts, "branch "+strings.TrimPrefix(line, "# branch.head "))
		}
		if strings.HasPrefix(line, "# branch.ab ") {
			parts = append(parts, "ahead/behind "+strings.TrimPrefix(line, "# branch.ab "))
		}
		if strings.HasPrefix(line, "1 ") || strings.HasPrefix(line, "2 ") || strings.HasPrefix(line, "u ") {
			n := 9
			if line[0] == '2' {
				n = 10
			}
			if line[0] == 'u' {
				n = 11
			}
			fields := strings.SplitN(line, " ", n)
			if len(fields) == n && len(changed) < 15 {
				changed = append(changed, fields[n-1])
			}
		}
	}
	r := gitInfo{changed: len(changed) > 0}
	if len(changed) > 0 {
		parts = append(parts, "changed paths: "+strings.Join(changed, ", "))
	} else {
		parts = append(parts, "tracked working tree clean")
	}
	if objectID.MatchString(head) {
		current, e := git(ctx, p.Root, "rev-parse", "--verify", "HEAD")
		if e == nil {
			r.head = current
		}
		if e == nil && !strings.HasPrefix(current, head) {
			count, e := git(ctx, p.Root, "rev-list", "--count", head+"..HEAD")
			if e == nil {
				r.changed = true
				parts = append(parts, "HEAD moved "+count+" commits since the page was written")
				subjects, e := git(ctx, p.Root, "log", "-3", "--format=%s", head+"..HEAD")
				if e == nil && subjects != "" {
					// A subject is free text, so it never starts a line.
					for _, subject := range strings.Split(subjects, "\n") {
						parts = append(parts, indent+"- "+subject)
					}
				}
			} else {
				parts = append(parts, "HEAD changed; previous commit unavailable")
			}
		}
	}
	for _, name := range []string{"rebase-merge", "rebase-apply", "MERGE_HEAD", "CHERRY_PICK_HEAD", "BISECT_LOG"} {
		path, e := git(ctx, p.Root, "rev-parse", "--git-path", name)
		if e != nil {
			parts = append(parts, "operation in progress: unknown")
			break
		}
		if !filepath.IsAbs(path) {
			path = filepath.Join(p.Root, path)
		}
		if _, e = os.Lstat(path); e == nil {
			parts = append(parts, "operation in progress: "+name)
			r.changed = true
		} else if !errors.Is(e, fs.ErrNotExist) {
			parts = append(parts, "operation in progress: unknown")
			break
		}
	}
	// git counts the stashes itself, in one short line, so the cap on what
	// a call prints never cuts the count, as it would cut a list of them.
	// "--" keeps a file named refs/stash from making the name ambiguous.
	stashes, e := git(ctx, p.Root, "rev-list", "--walk-reflogs", "--count", "refs/stash", "--")
	if n, err := strconv.Atoi(stashes); e == nil && err == nil && n > 0 {
		parts = append(parts, fmt.Sprintf("stashes: %d", n))
		r.changed = true
	} else {
		parts = append(parts, "stashes: 0 or unavailable")
	}
	r.text = cut(strings.Join(parts, "\n"), 900)
	return r
}

// No hook, fsmonitor, optional locks or inherited locator can redirect a
// read, no signature is checked, and a partial clone never fetches what it
// lacks (see coderef.Partial). git runs with the environment of every
// read-only git eva-md runs (identity.GitEnv). A call killed at its
// deadline takes any process it started with it.
func git(ctx context.Context, root string, args ...string) (string, error) {
	argv := []string{"-c", "core.fsmonitor=false", "-c", "core.hooksPath=/dev/null", "-c", "log.showSignature=false", "--no-optional-locks"}
	if coderef.Partial(root) {
		argv = append(argv, "--no-lazy-fetch")
	}
	cmd := exec.CommandContext(ctx, "git", append(append(argv, "-C", root), args...)...)
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	cmd.Cancel = func() error { return coderef.KillGroup(cmd.Process.Pid) }
	cmd.WaitDelay = GitWaitDelay
	cmd.Env = identity.GitEnv()
	var out limitBuffer
	cmd.Stdout = &out
	cmd.Stderr = &limitBuffer{}
	err := cmd.Run()
	return strings.TrimSpace(out.String()), err
}

// gitOutputLimit is how much of what one live git call prints the brief
// keeps.
const gitOutputLimit = 64 << 10

// limitBuffer keeps the first gitOutputLimit bytes git prints and drops the
// rest, never failing the call. The buffer is a named field, not an
// embedded bytes.Buffer, whose ReadFrom os/exec would call in place of
// Write, so the limit would never hold.
type limitBuffer struct{ buffer bytes.Buffer }

func (b *limitBuffer) Write(p []byte) (int, error) {
	if room := gitOutputLimit - b.buffer.Len(); room > 0 {
		_, _ = b.buffer.Write(p[:min(len(p), room)])
	}
	return len(p), nil
}

func (b *limitBuffer) String() string { return b.buffer.String() }
