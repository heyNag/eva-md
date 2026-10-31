// Package wikigit runs git on the wiki: the hardened runner init uses, and
// the daily commit and push.
package wikigit

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/heyNag/eva-md/internal/identity"
)

// Run runs a Git command in dir within timeout, with hooks and fsmonitor off
// and no prompts, and returns its trimmed output. It keeps the user's signing
// and authentication settings. It suppresses raw Git diagnostics, which may
// echo credential-bearing remotes or output from a signer.
func Run(ctx context.Context, timeout time.Duration, dir string, args ...string) (string, error) {
	var out limitedBuffer
	if err := run(ctx, timeout, dir, &out, args...); err != nil {
		return "", err
	}
	return strings.TrimSpace(out.String()), nil
}

// The classes of a failed git command that a record can name.
const (
	ClassTimeout       = "timeout"        // it ran out of time or was canceled
	ClassSignerMissing = "signer_missing" // git couldn't start the signing program
	ClassSignature     = "signature"      // the signature failed or was declined
)

// Error is a failed git command, with its class, or "" for any other.
type Error struct {
	msg   string
	Class string
}

func (e *Error) Error() string { return e.msg }

// run is Run with the output written to out, which may refuse it. git's
// diagnostics only sort a failure into a class; they are never kept or
// shown.
func run(ctx context.Context, timeout time.Duration, dir string, out io.Writer, args ...string) error {
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	full := append([]string{"-c", "core.fsmonitor=false", "-c", "core.hooksPath=/dev/null", "--no-optional-locks", "-C", dir}, args...)
	cmd := exec.CommandContext(ctx, "git", full...)
	cmd.Env = Environment()
	cmd.WaitDelay = 200 * time.Millisecond
	cmd.Stdout = out
	var diagnostics limitedBuffer
	cmd.Stderr = &diagnostics
	if err := cmd.Run(); err != nil {
		if ctx.Err() != nil {
			return &Error{msg: fmt.Sprintf("wikigit: git %s timed out or was canceled", args[0]), Class: ClassTimeout}
		}
		return &Error{msg: fmt.Sprintf("wikigit: git %s failed; files are preserved, and signing/authentication settings were not changed", args[0]), Class: classify(diagnostics.String())}
	}
	return nil
}

// classify sorts a failed git's diagnostics: a signing program git couldn't
// start, as one missing from a desktop app's short PATH, or a signature
// that failed or was declined.
func classify(diagnostics string) string {
	d := strings.ToLower(diagnostics)
	switch {
	case strings.Contains(d, "cannot exec") || strings.Contains(d, "cannot run"):
		return ClassSignerMissing
	case strings.Contains(d, "sign"):
		return ClassSignature
	}
	return ""
}

// Environment is the environment git runs with: this process's, without the
// variables that could redirect a command to another repository, and with
// terminal prompts off.
func Environment() []string {
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

// RemoteIdentity checks a remote and returns it as git is given it and as
// the normalized identity the config records: "file://" and the real path
// for a local remote, or host/owner/repo. It refuses remote helpers, option
// lookalikes and URLs carrying a password, a query or a fragment, and never
// echoes the URL in an error.
func RemoteIdentity(raw string) (string, string, error) {
	if raw == "" || strings.TrimSpace(raw) != raw || strings.HasPrefix(raw, "-") || strings.ContainsAny(raw, "\x00\r\n") {
		return "", "", errors.New("wikigit: missing or invalid remote")
	}
	if !strings.Contains(raw, "://") && strings.Contains(raw, "::") {
		return "", "", errors.New("wikigit: Git remote helpers are not supported")
	}
	if filepath.IsAbs(raw) {
		real, err := filepath.EvalSymlinks(raw)
		if err != nil {
			return "", "", errors.New("wikigit: local remote does not exist")
		}
		return real, "file://" + filepath.ToSlash(real), nil
	}
	if strings.Contains(raw, "://") {
		u, err := url.Parse(raw)
		if err != nil {
			return "", "", errors.New("wikigit: invalid remote")
		}
		if u.RawQuery != "" || u.Fragment != "" {
			return "", "", errors.New("wikigit: remote must not contain a query or fragment")
		}
		if u.User != nil {
			if _, has := u.User.Password(); has || (u.Scheme != "ssh" && u.Scheme != "git+ssh" && u.Scheme != "ssh+git") {
				return "", "", errors.New("wikigit: use credential-free remote URLs and the configured credential agent")
			}
		}
	}
	id, ok := identity.NormalizeRemote(raw)
	if !ok {
		return "", "", errors.New("wikigit: unsupported remote; use an SSH/HTTPS URL or an absolute local path")
	}
	return raw, id, nil
}
