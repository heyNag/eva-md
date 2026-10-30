package resume

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	"github.com/heyNag/eva-md/internal/identity"
)

type gitInfo struct {
	text    string
	changed bool
}

var objectID = regexp.MustCompile(`^[a-fA-F0-9]{7,64}$`)

func gitSummary(parent context.Context, p identity.Project, head string) gitInfo {
	if p.Root == "" {
		return gitInfo{text: "Live git: not a repository"}
	}
	ctx, cancel := context.WithTimeout(parent, 800*time.Millisecond)
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
			continue
		}
		if !filepath.IsAbs(path) {
			path = filepath.Join(p.Root, path)
		}
		if _, e = os.Lstat(path); e == nil {
			parts = append(parts, "operation in progress: "+name)
			r.changed = true
		}
	}
	stashes, e := git(ctx, p.Root, "reflog", "show", "--format=%H", "refs/stash")
	if e == nil && stashes != "" {
		parts = append(parts, fmt.Sprintf("stashes: %d", len(strings.Split(stashes, "\n"))))
		r.changed = true
	} else {
		parts = append(parts, "stashes: 0 or unavailable")
	}
	r.text = cut(strings.Join(parts, "\n"), 900)
	return r
}

// No hook, fsmonitor, optional locks or inherited locator can redirect a read.
func git(ctx context.Context, root string, args ...string) (string, error) {
	argv := append([]string{"-c", "core.fsmonitor=false", "-c", "core.hooksPath=/dev/null", "--no-optional-locks", "-C", root}, args...)
	cmd := exec.CommandContext(ctx, "git", argv...)
	cmd.WaitDelay = 100 * time.Millisecond
	for _, e := range os.Environ() {
		key := strings.SplitN(e, "=", 2)[0]
		if strings.HasPrefix(key, "GIT_") && key != "GIT_CONFIG_GLOBAL" && key != "GIT_CONFIG_NOSYSTEM" {
			continue
		}
		cmd.Env = append(cmd.Env, e)
	}
	cmd.Env = append(cmd.Env, "GIT_TERMINAL_PROMPT=0", "GIT_OPTIONAL_LOCKS=0", "LC_ALL=C")
	var out limitBuffer
	cmd.Stdout = &out
	cmd.Stderr = &limitBuffer{}
	err := cmd.Run()
	return strings.TrimSpace(out.String()), err
}

type limitBuffer struct{ bytes.Buffer }

func (b *limitBuffer) Write(p []byte) (int, error) {
	n := len(p)
	if b.Len() < 64<<10 {
		left := (64 << 10) - b.Len()
		if len(p) > left {
			p = p[:left]
		}
		_, _ = b.Buffer.Write(p)
	}
	return n, nil
}
