package migrate

import (
	"context"
	"os"
	"path/filepath"
	"strings"
)

// exposed counts the files git hides in the store now that it would show
// once the runner's .gitignore, newIgnore, replaces the old one: such a
// file, a .DS_Store or a capture v1's .gitignore kept out, would leave the
// store dirty after its commit, and a later writer could take it for a
// person's edit. What the person's own exclude files hide stays hidden, so
// those count as still hidden. A .gitignore below the root that the result
// keeps is not read: it could only hide more, so the count errs high,
// never low. It reads and writes nothing but git's listings.
func exposed(ctx context.Context, root string, newIgnore []byte) (int, error) {
	hidden, err := git(ctx, root, nil, nil, "ls-files", "-o", "-i", "-z", "--exclude-standard")
	if err != nil {
		return 0, err
	}
	if len(hidden) == 0 {
		return 0, nil
	}
	args := []string{"ls-files", "-o", "-i", "-z"}
	files, err := excludeFiles(ctx, root)
	if err != nil {
		return 0, err
	}
	for _, f := range files {
		args = append(args, "--exclude-from="+f)
	}
	for _, line := range strings.Split(string(newIgnore), "\n") {
		if line = strings.TrimSpace(line); line != "" && !strings.HasPrefix(line, "#") {
			args = append(args, "--exclude="+line)
		}
	}
	still, err := git(ctx, root, nil, nil, args...)
	if err != nil {
		return 0, err
	}
	kept := map[string]bool{}
	for _, f := range strings.Split(string(still), "\x00") {
		kept[f] = true
	}
	n := 0
	for _, f := range strings.Split(string(hidden), "\x00") {
		if f != "" && !kept[f] {
			n++
		}
	}
	return n, nil
}

// excludeFiles are the exclude files git reads besides .gitignore files:
// the repository's info/exclude and the person's core.excludesFile, or its
// default under the configuration home. Only those that exist are named.
func excludeFiles(ctx context.Context, root string) ([]string, error) {
	var files []string
	out, err := git(ctx, root, nil, nil, "rev-parse", "--git-path", "info/exclude")
	if err != nil {
		return nil, err
	}
	files = append(files, absUnder(root, strings.TrimSpace(string(out))))
	out, err = git(ctx, root, nil, nil, "config", "--path", "--get", "core.excludesFile")
	switch {
	case err == nil:
		if f := strings.TrimSpace(string(out)); f != "" {
			files = append(files, absUnder(root, f))
		}
	case exitCode(err) == 1: // unset: git's default
		env := map[string]string{}
		for _, kv := range gitEnv(nil) {
			k, v, _ := strings.Cut(kv, "=")
			env[k] = v
		}
		if x := env["XDG_CONFIG_HOME"]; x != "" {
			files = append(files, filepath.Join(x, "git", "ignore"))
		} else if h := env["HOME"]; h != "" {
			files = append(files, filepath.Join(h, ".config", "git", "ignore"))
		}
	default:
		return nil, err
	}
	// One that is missing hides nothing from git either.
	var exist []string
	for _, f := range files {
		if info, err := os.Stat(f); err == nil && info.Mode().IsRegular() {
			exist = append(exist, f)
		}
	}
	return exist, nil
}

func absUnder(root, p string) string {
	if filepath.IsAbs(p) {
		return p
	}
	return filepath.Join(root, p)
}
