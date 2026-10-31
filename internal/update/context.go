package update

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"path"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	"github.com/heyNag/eva-md/internal/identity"
	"github.com/heyNag/eva-md/internal/session"
)

// gitTimeout bounds the git call for a branch's commit subjects.
const gitTimeout = 2 * time.Second

// objectID is a commit's object id as git prints it.
var objectID = regexp.MustCompile(`^[0-9a-f]{7,64}$`)

// identify works out the project a session file belongs to, from its key:
// by the chat's folder, as the hooks did, when that still resolves to the
// key, and otherwise from the key itself. dir is the folder, or "".
func identify(ctx context.Context, key string, entries []Entry) (p identity.Project, dir string, err error) {
	if key == identity.GeneralKey {
		return identity.General(), "", nil
	}
	if dir = chatFolder(entries); dir != "" {
		if p, err := identity.Resolve(ctx, dir); err == nil && p.Key == key {
			return p, dir, nil
		}
	}
	p, err = projectFromKey(key)
	return p, dir, err
}

// chatFolder is the newest entry's folder, which the inbox keeps relative to
// the home folder, as an absolute path, or "".
func chatFolder(entries []Entry) string {
	home, err := os.UserHomeDir()
	if err != nil || !filepath.IsAbs(home) {
		return ""
	}
	for i := len(entries) - 1; i >= 0; i-- {
		switch cwd := entries[i].Cwd; {
		case cwd == "":
		case filepath.IsAbs(cwd):
			return filepath.Clean(cwd)
		default:
			return filepath.Join(home, cwd)
		}
	}
	return ""
}

// projectFromKey reads a project back from its inbox key, for a chat whose
// folder is gone or has moved: a repo key holds its normalized remote, with
// "/" written "__" and "_" written "_u", and a local key its slug. A key
// made from a hash holds neither.
func projectFromKey(key string) (identity.Project, error) {
	if slug, ok := strings.CutPrefix(key, "local-"); ok && slug != "" {
		return identity.Project{Kind: identity.KindLocal, Key: key, Slug: slug}, nil
	}
	if strings.HasPrefix(key, "repo-") || !strings.Contains(key, "__") {
		return identity.Project{}, errors.New("update: the project can't be told from its inbox folder, and the chat's folder no longer shows it")
	}
	remote := strings.ReplaceAll(strings.ReplaceAll(key, "__", "/"), "_u", "_")
	return identity.Project{Kind: identity.KindRepo, Key: key, Remote: remote, Slug: path.Base(remote)}, nil
}

// commitsOf returns the branch's last 5 commit subjects, newest first, and
// its newest commit, from git in the chat's worktree, with the hardened
// flags the identity package uses and a short timeout. On any failure the
// subjects are "unknown".
func commitsOf(ctx context.Context, root, branch string) (subjects []string, head string) {
	unknown := []string{"unknown"}
	if root == "" || branch == "" {
		return unknown, ""
	}
	ctx, cancel := context.WithTimeout(ctx, gitTimeout)
	defer cancel()
	out, err := gitRead(ctx, root, "log", "-5", "--format=%H%x09%s", "refs/heads/"+branch, "--")
	if err != nil {
		return unknown, ""
	}
	for i, line := range strings.Split(out, "\n") {
		sha, subject, ok := strings.Cut(line, "\t")
		if !ok {
			continue
		}
		if i == 0 && objectID.MatchString(sha) {
			head = sha
		}
		subjects = append(subjects, subject)
	}
	if len(subjects) == 0 {
		return unknown, head
	}
	return subjects, head
}

// gitRead runs one read-only git command in dir with fsmonitor and hooks
// off, no optional locks and no inherited git variable that could point it
// at another repo, and returns its stdout without the final newline.
func gitRead(ctx context.Context, dir string, args ...string) (string, error) {
	argv := append([]string{"-c", "core.fsmonitor=false", "-c", "core.hooksPath=/dev/null", "--no-optional-locks", "-C", dir}, args...)
	cmd := exec.CommandContext(ctx, "git", argv...)
	cmd.Dir = "/"
	cmd.WaitDelay = 100 * time.Millisecond
	for _, kv := range os.Environ() {
		key, _, _ := strings.Cut(kv, "=")
		if strings.HasPrefix(key, "GIT_") && key != "GIT_CONFIG_GLOBAL" && key != "GIT_CONFIG_NOSYSTEM" || key == "LC_ALL" {
			continue
		}
		cmd.Env = append(cmd.Env, kv)
	}
	cmd.Env = append(cmd.Env, "GIT_TERMINAL_PROMPT=0", "GIT_OPTIONAL_LOCKS=0", "LC_ALL=C")
	out := &capped{limit: 64 << 10}
	cmd.Stdout = out
	if err := cmd.Run(); err != nil {
		return "", err
	}
	if out.overflow {
		return "", errors.New("update: git printed more than expected")
	}
	return strings.TrimSuffix(out.String(), "\n"), nil
}

// chatModel is the chat's model: the newest entry's that names one, else the
// session's model marker, which holds the last PostModelSwitch or
// SessionStart model. A name a CLI flag can't take counts as unknown, so the
// run uses the host default.
func chatModel(markers session.Markers, host, sid string, entries []Entry) string {
	model := ""
	for i := len(entries) - 1; i >= 0 && model == ""; i-- {
		model = entries[i].Model
	}
	if model == "" {
		model, _, _ = markers.Get(host, sid, session.Model)
	}
	if !modelName.MatchString(model) {
		return ""
	}
	return model
}
