package task

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

// The made-up identity, date and message of the commit Prepare makes, so
// that two builds of a workspace give the same commit ids.
const (
	commitName    = "Continuation Fixture"
	commitEmail   = "fixture@example.invalid"
	commitDate    = "2026-01-01T00:00:00+00:00"
	commitMessage = "Start"
)

// Prepare builds the workspace a new chat of task t starts from in dst,
// which must not exist or be an empty folder: a copy of each repository in
// reposDir (dst itself under WorkdirRepo, dst/<name>/ under
// WorkdirOutside), the task's start/ overlay laid over it, committed once.
// No file of the hidden, solution or decoy overlays is ever in it.
func Prepare(ctx context.Context, t Task, reposDir, dst string) error {
	if err := emptyDir(dst); err != nil {
		return fmt.Errorf("task %s: the workspace: %w", t.ID, err)
	}
	for _, name := range t.Repos {
		root := repoRoot(t, dst, name)
		if err := os.MkdirAll(root, 0o755); err != nil {
			return fmt.Errorf("task %s: %w", t.ID, err)
		}
		if err := layOver(filepath.Join(reposDir, name), root); err != nil {
			return fmt.Errorf("task %s: repository %s: %w", t.ID, name, err)
		}
		if err := layOverlay(t, OverlayStart, name, root); err != nil {
			return fmt.Errorf("task %s: %w", t.ID, err)
		}
		if err := commit(ctx, root); err != nil {
			return fmt.Errorf("task %s: repository %s: %w", t.ID, name, err)
		}
	}
	return nil
}

// repoRoot is where the repository name stands in a workspace or its copy
// at dir.
func repoRoot(t Task, dir, name string) string {
	if t.Workdir == WorkdirRepo {
		return dir
	}
	return filepath.Join(dir, name)
}

// emptyDir makes dir, which must not exist or be an empty folder.
func emptyDir(dir string) error {
	entries, err := os.ReadDir(dir)
	switch {
	case errors.Is(err, fs.ErrNotExist):
		return os.MkdirAll(dir, 0o755)
	case err != nil:
		return err
	case len(entries) > 0:
		return errors.New("the folder is not empty")
	}
	return nil
}

// layOverlay lays the overlay o of task t for the repository name over
// root, if the task has one.
func layOverlay(t Task, o, name, root string) error {
	src := filepath.Join(t.Dir, o, name)
	if _, err := os.Lstat(src); errors.Is(err, fs.ErrNotExist) {
		return nil
	}
	if err := layOver(src, root); err != nil {
		return fmt.Errorf("overlay %s/%s: %w", o, name, err)
	}
	return nil
}

// layOver copies each regular file under the folder src to the same path
// under root, replacing whatever stands at that path or on the way to it,
// a link among them, so that nothing is written through a link out of
// root. src may hold only folders and regular files, at clean paths out of
// git's own folder.
func layOver(src, root string) error {
	if err := realDir(root); err != nil {
		return err
	}
	return walkOverlay(src, func(rel string) error {
		return writeOver(root, rel, filepath.Join(src, filepath.FromSlash(rel)))
	})
}

// writeOver writes the file src at the slash path rel under root, making
// each folder on the way a real folder.
func writeOver(root, rel, src string) error {
	parts := strings.Split(rel, "/")
	dir := root
	for _, p := range parts[:len(parts)-1] {
		dir = filepath.Join(dir, p)
		if err := realDir(dir); err != nil {
			return err
		}
	}
	info, err := os.Stat(src)
	if err != nil {
		return err
	}
	data, err := os.ReadFile(src)
	if err != nil {
		return err
	}
	dst := filepath.Join(dir, parts[len(parts)-1])
	if err := os.RemoveAll(dst); err != nil {
		return err
	}
	return os.WriteFile(dst, data, fileMode(info.Mode()))
}

// realDir makes sure dir is a folder and not a link, replacing whatever
// else stands there.
func realDir(dir string) error {
	info, err := os.Lstat(dir)
	if err == nil && info.IsDir() {
		return nil
	}
	if err == nil {
		if err := os.Remove(dir); err != nil {
			return err
		}
	} else if !errors.Is(err, fs.ErrNotExist) {
		return err
	}
	return os.Mkdir(dir, 0o755)
}

// fileMode is the mode a copy gets: executable or not, as git records it.
func fileMode(m fs.FileMode) fs.FileMode {
	if m.Perm()&0o111 != 0 {
		return 0o755
	}
	return 0o644
}

// commit makes root a repository with one commit of every file in it, by
// a made-up identity at a fixed date. Git's environment is built, not
// inherited, so no configuration or identity of the caller's reaches it.
func commit(ctx context.Context, root string) error {
	steps := [][]string{
		{"init", "-q", "-b", "main", "--template="},
		{"add", "-A"},
		{"commit", "-q", "--no-verify", "-m", commitMessage},
	}
	for _, args := range steps {
		if err := git(ctx, root, args...); err != nil {
			return err
		}
	}
	return nil
}

// gitConfig is the configuration every git Prepare runs is given.
var gitConfig = []string{
	"user.name=" + commitName,
	"user.email=" + commitEmail,
	"commit.gpgsign=false",
	"core.autocrlf=false",
	"gc.auto=0",
	"maintenance.auto=false",
}

// gitEnv is git's whole environment: PATH, to find it, an empty global
// configuration outside any workspace, no system configuration, and the
// fixed dates.
func gitEnv() []string {
	return []string{
		"PATH=" + os.Getenv("PATH"),
		"GIT_CONFIG_GLOBAL=" + os.DevNull,
		"GIT_CONFIG_NOSYSTEM=1",
		"GIT_AUTHOR_DATE=" + commitDate,
		"GIT_COMMITTER_DATE=" + commitDate,
		"LC_ALL=C",
	}
}

// git runs git in dir. Its error says how git ended and never what it
// printed.
func git(ctx context.Context, dir string, args ...string) error {
	full := []string{"-C", dir}
	for _, c := range gitConfig {
		full = append(full, "-c", c)
	}
	cmd := exec.CommandContext(ctx, "git", append(full, args...)...)
	cmd.Dir = dir
	cmd.Env = gitEnv()
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("git %s: %w", args[0], err)
	}
	return nil
}
