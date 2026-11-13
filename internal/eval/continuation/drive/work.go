package drive

import (
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
)

// tempFolders are the folders eva-md's hooks skip as temporary (tempPath,
// internal/hook), so a chat run in one would capture nothing.
var tempFolders = []string{"/tmp", "/var/tmp"}

// checkWork returns --work made absolute with its links resolved, as far
// as it exists, or a *UsageError when it is not absolute, or overlaps the
// invoking home, a temporary folder the hooks skip, the tasks, the
// repositories or the repository the rows are read from: when it is one of
// them, is in one, or holds one. It writes nothing.
func checkWork(o Options) (string, error) {
	if !filepath.IsAbs(o.Work) {
		return "", usagef("--work is not an absolute path")
	}
	work, err := resolve(o.Work)
	if err != nil {
		return "", usagef("--work cannot be read")
	}
	home, err := os.UserHomeDir()
	if err != nil || !filepath.IsAbs(home) {
		return "", usagef("the invoking home is not known, so --work cannot be shown to be outside it")
	}
	repo := o.Repo
	if repo == "" {
		repo = "."
	}
	for _, f := range []struct{ name, path string }{
		{"the invoking home", home}, {"--tasks", o.Tasks}, {"--repos", o.Repos}, {"--repo", repo},
	} {
		dir, err := resolve(f.path)
		if err != nil {
			return "", usagef("%s cannot be read", f.name)
		}
		if within(work, dir) || within(dir, work) {
			return "", usagef("--work overlaps %s", f.name)
		}
	}
	for _, t := range tempFolders {
		for _, dir := range []string{t, resolveOr(t)} {
			if within(work, dir) || within(dir, work) {
				return "", usagef("--work overlaps a temporary folder, whose chats eva-md's hooks skip")
			}
		}
	}
	return work, nil
}

// resolve returns p made absolute with its links resolved, as far as it
// exists.
func resolve(p string) (string, error) {
	p, err := filepath.Abs(p)
	if err != nil {
		return "", err
	}
	real, err := filepath.EvalSymlinks(p)
	switch {
	case err == nil:
		return real, nil
	case !errors.Is(err, fs.ErrNotExist) || filepath.Dir(p) == p:
		return "", err
	}
	parent, err := resolve(filepath.Dir(p))
	if err != nil {
		return "", err
	}
	return filepath.Join(parent, filepath.Base(p)), nil
}

// resolveOr is p resolved, or p when it cannot be.
func resolveOr(p string) string {
	if r, err := resolve(p); err == nil {
		return r
	}
	return p
}

// within reports whether p is dir or inside it; both are absolute.
func within(p, dir string) bool {
	rel, err := filepath.Rel(dir, p)
	return err == nil && rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator))
}
