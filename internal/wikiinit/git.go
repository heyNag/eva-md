package wikiinit

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"os/exec"
	"strings"
	"time"
)

// git runs a Git command in dir with a two-minute limit, hooks and fsmonitor
// off and no prompts. It keeps the user's signing and authentication settings.
// It suppresses raw Git diagnostics, which may echo credential-bearing remotes
// or output from a signer.
func git(ctx context.Context, dir string, args ...string) (string, error) {
	ctx, cancel := context.WithTimeout(ctx, 120*time.Second)
	defer cancel()
	full := append([]string{"-c", "core.fsmonitor=false", "-c", "core.hooksPath=/dev/null", "--no-optional-locks", "-C", dir}, args...)
	cmd := exec.CommandContext(ctx, "git", full...)
	cmd.Env = gitEnvironment()
	cmd.WaitDelay = 200 * time.Millisecond
	var out limitedBuffer
	cmd.Stdout = &out
	if err := cmd.Run(); err != nil {
		if ctx.Err() != nil {
			return "", fmt.Errorf("wikiinit: git %s timed out or was canceled", args[0])
		}
		return "", fmt.Errorf("wikiinit: git %s failed; files are preserved, and signing/authentication settings were not changed", args[0])
	}
	return strings.TrimSpace(out.String()), nil
}

// Prevent inherited repository-location variables from redirecting a command.
func gitEnvironment() []string {
	var env []string
	for _, kv := range os.Environ() {
		key, _, _ := strings.Cut(kv, "=")
		switch key {
		case "GIT_DIR", "GIT_WORK_TREE", "GIT_INDEX_FILE", "GIT_COMMON_DIR", "GIT_OBJECT_DIRECTORY", "GIT_ALTERNATE_OBJECT_DIRECTORIES", "GIT_NAMESPACE", "GIT_PREFIX", "GIT_TERMINAL_PROMPT":
			continue
		}
		env = append(env, kv)
	}
	return append(env, "GIT_TERMINAL_PROMPT=0")
}

type limitedBuffer struct{ bytes.Buffer }

func (b *limitedBuffer) Write(p []byte) (int, error) {
	if b.Len()+len(p) > 1<<20 {
		return 0, fmt.Errorf("git output exceeds limit")
	}
	return b.Buffer.Write(p)
}
