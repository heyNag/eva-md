// Package identity works out which project and branch a folder belongs to,
// using hardened, read-only git calls.
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

// Timeout bounds all git calls of one Resolve.
const Timeout = 800 * time.Millisecond

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

	branch, err := git(ctx, root, "rev-parse", "--abbrev-ref", "HEAD")
	var ee *exitError
	switch {
	case err == nil && branch == "HEAD":
		p.Detached = true
	case err == nil && branch != "":
		p.Branch = branch
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
		// Escape literal underscores before joining path components. Otherwise
		// team__sub/repo and team/sub__repo would share the same inbox key.
		p.Key = strings.ReplaceAll(strings.ReplaceAll(remote, "_", "_u"), "/", "__")
		if !plainKey(p.Key) {
			sum := sha256.Sum256([]byte(remote))
			p.Key = "repo-" + hex.EncodeToString(sum[:])[:12]
		}
		p.Slug = path.Base(remote)
		return p, nil
	}
	sum := sha256.Sum256([]byte(root))
	name := folderName(filepath.Base(root)) + "-" + hex.EncodeToString(sum[:])[:6]
	p.Kind = KindLocal
	p.Key = "local-" + name
	if !plainKey(p.Key) {
		p.Key = "local-" + hex.EncodeToString(sum[:])[:12]
	}
	p.Slug = name
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

// exitError is a git call that ran and exited non-zero.
type exitError struct {
	code    int
	arg     string
	notRepo bool
}

func (e *exitError) Error() string { return fmt.Sprintf("git %s exited with %d", e.arg, e.code) }

// locatingVars are the git environment variables that would point a git call
// at a different repo than the folder being resolved.
var locatingVars = map[string]bool{
	"GIT_DIR": true, "GIT_WORK_TREE": true, "GIT_INDEX_FILE": true, "GIT_COMMON_DIR": true,
	"GIT_OBJECT_DIRECTORY": true, "GIT_ALTERNATE_OBJECT_DIRECTORIES": true,
	"GIT_NAMESPACE": true, "GIT_PREFIX": true,
}

func gitEnv() []string {
	env := make([]string, 0, len(os.Environ())+2)
	for _, kv := range os.Environ() {
		name, _, _ := strings.Cut(kv, "=")
		if locatingVars[name] || name == "GIT_TERMINAL_PROMPT" || name == "GIT_OPTIONAL_LOCKS" || name == "LC_ALL" ||
			name == "GIT_CONFIG" || name == "GIT_CONFIG_PARAMETERS" || name == "GIT_CONFIG_COUNT" ||
			strings.HasPrefix(name, "GIT_CONFIG_KEY_") || strings.HasPrefix(name, "GIT_CONFIG_VALUE_") {
			continue
		}
		env = append(env, kv)
	}
	return append(env, "GIT_TERMINAL_PROMPT=0", "GIT_OPTIONAL_LOCKS=0", "LC_ALL=C")
}

// git runs one read-only git command in dir with fsmonitor and hooks off and
// no optional locks, and returns its trimmed stdout.
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
	cmd.WaitDelay = 100 * time.Millisecond
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
			return "", &exitError{code: ee.ExitCode(), arg: args[0], notRepo: ee.ExitCode() == 128 && strings.HasPrefix(stderr.String(), "fatal: not a git repository")}
		}
		return "", fmt.Errorf("git %s: %w", args[0], err)
	}
	if out.truncated {
		return "", errors.New("git output exceeds identity limit")
	}
	return strings.TrimSpace(out.String()), nil
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
