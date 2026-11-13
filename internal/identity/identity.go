// Package identity works out which project and branch a folder belongs to,
// using hardened, read-only git calls. It also holds the environment every
// git eva-md runs starts from (GitEnviron).
package identity

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"net/url"
	"os"
	"os/exec"
	"path"
	"path/filepath"
	"regexp"
	"strings"
	"time"
	"unicode"

	"github.com/heyNag/eva-md/internal/redact"
)

// Kind says how a project was identified.
type Kind string

// Project kinds.
const (
	KindRepo    Kind = "repo"    // git repo with a remote
	KindLocal   Kind = "local"   // git repo without a remote
	KindGeneral Kind = "general" // not inside a git repo
)

// GeneralKey is the key and slug of the general area.
const GeneralKey = "general"

// DefaultBranch is the branch of the general area and of a repo with no commits.
const DefaultBranch = "default"

// Timeout bounds all git calls of one Resolve. Tests raise it before any
// test runs, so a slow machine can't fail a test of what Resolve finds.
var Timeout = 800 * time.Millisecond

// WaitDelay bounds the wait for a git call's output once git has exited or
// been stopped. Tests raise it before any test runs, as they raise Timeout.
var WaitDelay = 100 * time.Millisecond

const maxFolderLen = 40

// Project identifies the project and branch of a folder.
type Project struct {
	Kind     Kind
	Key      string // "github.com__heynag__eva-md", "local-<folder>-<hash6>" or "general"; "repo-<hash12>" or "local-<hash12>" when plainKey refuses the readable one
	Slug     string // repo name, "<folder>-<hash6>" or "general"
	Remote   string // normalized, e.g. "github.com/heynag/eva-md"; empty without one
	Root     string // worktree top level; empty for general
	Branch   string // current branch; "default" with no commits or in general; empty when detached
	Detached bool
}

// HashedSlug is the wiki folder a project's pages take when another
// project already holds its slug: the slug, a dash and the first 6 hex
// digits of the SHA-256 of its key. The update writes there, and the brief
// and search look there.
func (p Project) HashedSlug() string {
	sum := sha256.Sum256([]byte(p.Key))
	return p.Slug + "-" + hex.EncodeToString(sum[:])[:6]
}

// General returns the general area, used for folders outside any repo.
func General() Project {
	return Project{Kind: KindGeneral, Key: GeneralKey, Slug: GeneralKey, Branch: DefaultBranch}
}

// Resolve identifies the project of cwd, which must be an absolute path. A
// missing folder, or a confirmed folder outside any git repo, is the general
// area with a nil error.
// Git failures such as timeout, invalid config or trust refusal return an error
// so callers can fail closed instead of bypassing remote exclusions.
func Resolve(ctx context.Context, cwd string) (Project, error) {
	if !filepath.IsAbs(cwd) {
		return General(), fmt.Errorf("identity: cwd %q is not an absolute path", cwd)
	}
	ctx, cancel := context.WithTimeout(ctx, Timeout)
	defer cancel()
	if err := ctx.Err(); err != nil {
		return General(), err
	}
	if _, err := os.Stat(cwd); err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return General(), nil
		}
		return General(), fmt.Errorf("identity: inspect cwd: %w", err)
	}

	top, err := git(ctx, cwd, "rev-parse", "--show-toplevel")
	if err != nil {
		var ee *exitError
		if errors.As(err, &ee) && ee.notRepo {
			return General(), nil
		}
		return General(), fmt.Errorf("identity: %w", err)
	}
	if top == "" || !filepath.IsAbs(top) {
		return General(), nil
	}
	root := top
	if r, err := filepath.EvalSymlinks(top); err == nil {
		root = r
	}
	p := Project{Root: root}

	// The full ref name, because --abbrev-ref answers heads/<name> when a tag
	// or another ref shares the branch's name.
	branch, err := git(ctx, root, "rev-parse", "--symbolic-full-name", "HEAD")
	var ee *exitError
	switch {
	case err == nil && branch == "HEAD":
		p.Detached = true
	case err == nil && branch != "":
		p.Branch = strings.TrimPrefix(branch, "refs/heads/")
	case err == nil:
		return General(), errors.New("identity: git returned an empty branch")
	case errors.As(err, &ee):
		// Only a confirmed absent HEAD means an unborn branch. Config,
		// permission and trust failures must not bypass remote exclusions.
		_, headErr := git(ctx, root, "rev-parse", "--verify", "--quiet", "HEAD")
		var headExit *exitError
		if !errors.As(headErr, &headExit) || headExit.code != 1 {
			return General(), fmt.Errorf("identity: resolve branch: %w", err)
		}
		p.Branch = DefaultBranch
	default:
		return General(), fmt.Errorf("identity: %w", err)
	}

	raw, err := git(ctx, root, "config", "--get", "remote.origin.url")
	if err != nil {
		if !errors.As(err, &ee) || ee.code != 1 {
			return General(), fmt.Errorf("identity: %w", err)
		}
	}
	if remote, ok := NormalizeRemote(raw); err == nil && ok {
		p.Kind = KindRepo
		p.Remote = remote
		p.Key = repoKey(remote)
		p.Slug = path.Base(remote)
		return p, nil
	}
	p.Kind = KindLocal
	p.Key, p.Slug = localKey(root)
	return p, nil
}

// plainKey reports whether key passes the inbox's check of identity fields,
// which refuses any field holding a provider token format, because the key
// also names files. A folder or repo name can look like such a token: the key
// local-ops_runbooks_2026-a1b2c3 reads as a 1Password token and
// local-sk-learn-pipeline-a1b2c3 as an OpenAI key, and the inbox would then
// refuse every capture of the project. Such a project is keyed by a hash of
// its remote or folder instead, as repo-<hash12> or local-<hash12>, which is
// as stable and as distinct; its slug keeps the readable name.
func plainKey(key string) bool {
	clean, n := redact.RedactKnown(key)
	return n == 0 && clean == key
}

// scpRe matches scp-style remotes such as git@github.com:owner/repo.git.
var scpRe = regexp.MustCompile(`^(?:[^@/\s]+@)?([^@/:\s]+):(.*)$`)

// NormalizeRemote turns a git remote URL into host/path form: credentials,
// ports, a trailing .git and trailing slashes are removed and the result is
// lower case. It accepts scp-style, ssh://, git+ssh://, ssh+git://, https://,
// http:// and git:// forms. It returns false for local paths, file:// URLs and values it
// can't parse.
func NormalizeRemote(raw string) (string, bool) {
	s := strings.TrimSpace(raw)
	if s == "" || hasControl(s) {
		return "", false
	}
	var host, p string
	if strings.Contains(s, "://") {
		u, err := url.Parse(s)
		if err != nil {
			return "", false
		}
		switch strings.ToLower(u.Scheme) {
		case "ssh", "git+ssh", "ssh+git", "https", "http", "git":
		default:
			return "", false
		}
		host, p = u.Hostname(), u.Path
	} else if m := scpRe.FindStringSubmatch(s); m != nil && len(m[1]) > 1 && !strings.EqualFold(m[1], "file") {
		host, p = m[1], m[2]
	} else {
		return "", false
	}
	host = strings.ToLower(strings.TrimSuffix(host, "."))
	if host == "" || strings.ContainsAny(host, "/\\ ?#%") {
		return "", false
	}
	if strings.ContainsAny(p, "\\") || strings.IndexFunc(p, unicode.IsSpace) >= 0 {
		return "", false
	}
	p = path.Clean("/" + strings.ReplaceAll(p, "\\", "/"))
	p = strings.ToLower(strings.Trim(p, "/"))
	p = strings.Trim(strings.TrimSuffix(p, ".git"), "/")
	if p == "" || p == "." || hasControl(p) {
		return "", false
	}
	return host + "/" + p, true
}

func hasControl(s string) bool {
	for _, r := range s {
		if unicode.IsControl(r) || unicode.Is(unicode.Cf, r) {
			return true
		}
	}
	return false
}

// folderName makes a folder name safe for a key: lower case, letters, digits,
// '.', '_' and '-' only, at most 40 bytes.
func folderName(base string) string {
	var b strings.Builder
	dash := false
	for _, r := range strings.ToLower(base) {
		ok := r >= 'a' && r <= 'z' || r >= '0' && r <= '9' || r == '.' || r == '_' || r == '-'
		if !ok {
			r = '-'
		}
		if r == '-' {
			if dash {
				continue
			}
			dash = true
		} else {
			dash = false
		}
		b.WriteRune(r)
	}
	out := b.String()
	if len(out) > maxFolderLen {
		out = out[:maxFolderLen]
	}
	out = strings.Trim(out, "-.")
	if out == "" {
		return "folder"
	}
	return out
}

// exitError is a git call that ran and exited non-zero, or that a signal
// ended, which Go reports as code -1.
type exitError struct {
	code    int
	arg     string
	notRepo bool
	state   string // with code -1, what ended git, as "signal: killed"
}

func (e *exitError) Error() string {
	if e.code == -1 {
		return fmt.Sprintf("git %s ended by %s", e.arg, e.state)
	}
	return fmt.Sprintf("git %s exited with %d", e.arg, e.code)
}

// git runs one read-only git command in dir with fsmonitor and hooks off and
// no optional locks, and returns its stdout without the final newline.
func git(ctx context.Context, dir string, args ...string) (string, error) {
	full := append([]string{
		"-c", "core.fsmonitor=false",
		"-c", "core.hooksPath=/dev/null",
		"--no-optional-locks",
		"-C", dir,
	}, args...)
	cmd := exec.CommandContext(ctx, "git", full...)
	cmd.Env = gitEnv()
	cmd.Dir = "/"
	cmd.WaitDelay = WaitDelay
	out := cappedOutput{limit: 64 << 10}
	stderr := cappedOutput{limit: 4096}
	cmd.Stdout = &out
	cmd.Stderr = &stderr
	err := cmd.Run()
	if err != nil {
		if ctx.Err() != nil {
			return "", fmt.Errorf("git %s: %w", args[0], ctx.Err())
		}
		var ee *exec.ExitError
		if errors.As(err, &ee) {
			e := &exitError{code: ee.ExitCode(), arg: args[0], notRepo: ee.ExitCode() == 128 && strings.HasPrefix(stderr.String(), "fatal: not a git repository")}
			if e.code == -1 {
				// ProcessState names only the signal, never a path or output.
				e.state = ee.ProcessState.String()
			}
			return "", e
		}
		return "", fmt.Errorf("git %s: %w", args[0], err)
	}
	if out.truncated {
		return "", errors.New("git output exceeds identity limit")
	}
	// Trim only the newline: a top level folder name can end in a space.
	return strings.TrimSuffix(out.String(), "\n"), nil
}

type cappedOutput struct {
	buffer    bytes.Buffer
	limit     int
	truncated bool
}

func (b *cappedOutput) Write(p []byte) (int, error) {
	n := len(p)
	if remaining := b.limit - b.buffer.Len(); n > remaining {
		p = p[:remaining]
		b.truncated = true
	}
	_, _ = b.buffer.Write(p)
	return n, nil
}

func (b *cappedOutput) String() string { return b.buffer.String() }
