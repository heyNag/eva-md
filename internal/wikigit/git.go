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

// WaitDelay bounds the wait for a git command's output once git has exited
// or run out of time. Tests raise it before any test runs.
var WaitDelay = 200 * time.Millisecond

// The classes of a failed git command that a record can name.
const (
	ClassTimeout       = "timeout"        // it ran out of time or was canceled
	ClassSignerMissing = "signer_missing" // git couldn't start the signing program
	ClassSignature     = "signature"      // the signature failed or was declined
)

// The reasons git couldn't read a repo, which setup turns into its own
// words. They are apart from the classes, which name a commit's failure in a
// record, so a reason never reaches a record.
const (
	ReadNotFound       = "not_found"        // the repo isn't there, or the account can't see it
	ReadKeyRefused     = "key_refused"      // the SSH server refused every key
	ReadAgentRefused   = "agent_refused"    // the SSH agent declined to sign with the key
	ReadHostKey        = "host_key"         // ssh couldn't verify the host's key, as when it doesn't know it
	ReadHostKeyChanged = "host_key_changed" // the host's key isn't the one ssh knows
	ReadHostKeyRevoked = "host_key_revoked" // the host's key is one ssh has marked revoked
	ReadNoSignIn       = "no_sign_in"       // git has no sign-in for the host over HTTPS
	ReadUnreachable    = "unreachable"      // the host couldn't be found or reached
)

// Error is a failed git command, with its class, or "" for any other, and
// the reason it couldn't read a repo, or "" when its diagnostics say none.
type Error struct {
	msg   string
	Class string
	Read  string
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
	cmd.WaitDelay = WaitDelay
	cmd.Stdout = out
	var diagnostics limitedBuffer
	cmd.Stderr = &diagnostics
	if err := cmd.Run(); err != nil {
		if ctx.Err() != nil {
			return &Error{msg: fmt.Sprintf("wikigit: git %s timed out or was canceled", args[0]), Class: ClassTimeout}
		}
		return &Error{msg: fmt.Sprintf("wikigit: git %s failed; files are preserved, and signing/authentication settings were not changed", args[0]), Class: classify(diagnostics.String()), Read: readFailure(diagnostics.String())}
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

// readWordings are the lines of git, ssh, curl and the hosts that say why a
// repo couldn't be read, lower case, checked in this order. Only these
// English wordings are known, so a line worded otherwise gives no reason.
// What the host answered comes before the network's words.
var readWordings = []struct{ reason, text string }{
	// OpenSSH ends an unknown, a changed and a revoked host key alike with
	// sshconnect2.c's fatal("Host key verification failed."), so the last
	// two are told by what sshconnect.c prints first: "@    WARNING: REMOTE
	// HOST IDENTIFICATION HAS CHANGED!     @" and "Host key for %.200s has
	// changed and you have requested strict checking.", or "@       WARNING:
	// REVOKED HOST KEY DETECTED!               @" and "%s host key for
	// %.200s was revoked and you have requested strict checking.". The
	// banner alone, without strict checking, is enough. A key in ssh's
	// RevokedHostKeys file gets only "Host key %s %s revoked by file %s",
	// and a file ssh couldn't check "Error checking host key %s %s in
	// revoked keys file %s", which gives no reason: the key is neither
	// unknown nor known to be revoked.
	{ReadHostKeyChanged, "remote host identification has changed"},
	{ReadHostKeyChanged, "has changed and you have requested strict checking"},
	{ReadHostKeyRevoked, "revoked host key detected"},
	{ReadHostKeyRevoked, "was revoked and you have requested strict checking"},
	{ReadHostKeyRevoked, "revoked by file"},
	{"", "in revoked keys file"},
	{ReadHostKey, "host key verification failed"},
	// An agent that declines to sign, as one whose approval was denied,
	// makes OpenSSH's sshconnect2.c log "signing failed for %s \"%s\"%s: %s"
	// with ssherr.c's "agent refused operation", before the "Permission
	// denied" below: the host accepted the key, but never got a signature.
	{ReadAgentRefused, "agent refused operation"},
	// OpenSSH's sshconnect2.c: fatal("%s@%s: Permission denied (%s).", ...),
	// as in "git@gitlab.example.com: Permission denied (publickey).".
	{ReadKeyRefused, "permission denied (publickey"},
	// git's prompt.c, with GIT_TERMINAL_PROMPT=0: die("could not read
	// %s%s", prompt, "terminal prompts disabled"), as in "could not read
	// Username for 'https://gitlab.example.com': terminal prompts
	// disabled"; git's remote-curl.c: "Authentication failed for '%s'".
	{ReadNoSignIn, "terminal prompts disabled"},
	{ReadNoSignIn, "could not read username"},
	{ReadNoSignIn, "authentication failed"},
	// GitLab's lib/gitlab/git_access.rb: "The project you were looking for
	// could not be found or you don't have permission to view it.";
	// GitHub's "ERROR: Repository not found." (its docs' "Error:
	// Repository not found"); git's upload-pack.c: "'%s' does not appear
	// to be a git repository"; git's remote-curl.c: "repository '%s' not
	// found"; git's clone.c: "repository '%s' does not exist".
	{ReadNotFound, "the project you were looking for could not be found"},
	{ReadNotFound, "repository not found"},
	{ReadNotFound, "does not appear to be a git repository"},
	{ReadNotFound, "' not found"},
	{ReadNotFound, "' does not exist"},
	// OpenSSH's ssh.c: "%s: Could not resolve hostname %.100s: %s", and
	// curl's hostip.c: "Could not resolve %s: %s", with "host"; OpenSSH's
	// sshconnect.c: "ssh: connect to host %s port %s: %s", with the
	// system's words for ECONNREFUSED, ETIMEDOUT (macOS says "Operation
	// timed out", Linux "Connection timed out"), ENETUNREACH and
	// EHOSTUNREACH; curl's connect.c: "Failed to connect to %s port %u
	// after %ld ms: %s".
	{ReadUnreachable, "could not resolve host"},
	{ReadUnreachable, "connection refused"},
	{ReadUnreachable, "connection timed out"},
	{ReadUnreachable, "operation timed out"},
	{ReadUnreachable, "network is unreachable"},
	{ReadUnreachable, "no route to host"},
	{ReadUnreachable, "failed to connect to"},
}

// readFailure sorts a failed git's diagnostics into the reason it couldn't
// read a repo, or "" for any other failure. They are only read here, never
// kept or shown. Runs of white space count as one space, so a wording a
// line break splits is still found. ssh's line for a control socket it
// couldn't use, OpenSSH's mux.c "Control socket connect(%.100s): %s", is
// left out: it is about a socket on this Mac, never the host, and comes
// with "Connection refused" when the socket's master has exited, after
// which ssh connects without it.
func readFailure(diagnostics string) string {
	var lines []string
	for _, line := range strings.Split(strings.ToLower(diagnostics), "\n") {
		if !strings.HasPrefix(strings.TrimSpace(line), "control socket connect(") {
			lines = append(lines, line)
		}
	}
	d := strings.Join(strings.Fields(strings.Join(lines, "\n")), " ")
	for _, w := range readWordings {
		if strings.Contains(d, w.text) {
			return w.reason
		}
	}
	return ""
}

// Environment is the environment git runs with: this process's, without the
// variables that could redirect a command to another repository or change
// how git reads a pathspec, and with terminal prompts off. With
// GIT_LITERAL_PATHSPECS set, git would take each ":(literal)<path>" the
// commit stages, unstages and scans as a path that matches nothing, and
// commit a page its secret scan never read.
func Environment() []string {
	var env []string
	for _, kv := range os.Environ() {
		key, _, _ := strings.Cut(kv, "=")
		switch key {
		case "GIT_DIR", "GIT_WORK_TREE", "GIT_INDEX_FILE", "GIT_COMMON_DIR", "GIT_OBJECT_DIRECTORY", "GIT_ALTERNATE_OBJECT_DIRECTORIES", "GIT_NAMESPACE", "GIT_PREFIX", "GIT_TERMINAL_PROMPT",
			"GIT_LITERAL_PATHSPECS", "GIT_GLOB_PATHSPECS", "GIT_NOGLOB_PATHSPECS", "GIT_ICASE_PATHSPECS":
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
