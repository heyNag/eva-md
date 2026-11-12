package migrate

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"maps"
	"os"
	"path"
	"path/filepath"
	"slices"
	"strings"

	"github.com/heyNag/eva-md/internal/paths"
)

// writeFiles removes the change's paths, then writes its files and the
// runner's, each atomically with mode 0600, the runner's .gitattributes
// first, so no attribute of the old tree changes the bytes git takes in.
func writeFiles(ctx context.Context, root string, c Change, ownedFiles map[string][]byte) error {
	r, err := paths.OpenRoot(root, false)
	if err != nil {
		return fsErr("open the store", err)
	}
	defer r.Close()
	for _, p := range slices.Sorted(slices.Values(c.Remove)) {
		if err := r.Remove(filepath.FromSlash(p)); err != nil && !errors.Is(err, fs.ErrNotExist) {
			return fsErr("remove", err)
		}
		hook("remove")
		pruneDirs(r, p)
	}
	for _, p := range owned {
		if err := writeAt(ctx, r, p, ownedFiles[p], 0o600); err != nil {
			return err
		}
	}
	for _, p := range slices.Sorted(maps.Keys(c.Write)) {
		if err := writeAt(ctx, r, p, c.Write[p], 0o600); err != nil {
			return err
		}
	}
	return nil
}

// writeAt writes b to the store path p atomically, making its folders.
func writeAt(ctx context.Context, r *os.Root, p string, b []byte, mode os.FileMode) error {
	if dir := path.Dir(p); dir != "." {
		if err := r.MkdirAll(filepath.FromSlash(dir), 0o700); err != nil {
			return fsErr("make a folder", err)
		}
	}
	err := paths.WriteFileAtomicAt(ctx, r, filepath.FromSlash(p), b, mode)
	hook("write")
	if err != nil {
		return fsErr("write", err)
	}
	return nil
}

// fsErr is a file error without the store path it names: a path can carry
// a project's name or other words of the memory, and an error reaches the
// run log. Only the operation and the system's reason are kept.
func fsErr(op string, err error) error {
	var pe *fs.PathError
	var le *os.LinkError
	switch {
	case errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded):
		return fmt.Errorf("migrate: %s: %w", op, err)
	case errors.As(err, &pe):
		return fmt.Errorf("migrate: %s: %w", op, pe.Err)
	case errors.As(err, &le):
		return fmt.Errorf("migrate: %s: %w", op, le.Err)
	}
	return fmt.Errorf("migrate: %s failed", op)
}

// pruneDirs removes the folders above the removed path p that are left
// empty, nearest first, so a store keeps no folder that holds nothing.
func pruneDirs(r *os.Root, p string) {
	for dir := path.Dir(p); dir != "."; dir = path.Dir(dir) {
		if r.Remove(filepath.FromSlash(dir)) != nil {
			return
		}
	}
}

// commitStep stages exactly the paths the step and the runner changed and
// commits them on main as eva.md on the machine's label, unsigned, with a
// message of ids, counts and enums only.
func commitStep(ctx context.Context, root string, c Change, step Step, o Options, pass string) (string, error) {
	spec := nulList(slices.Concat(owned, slices.Sorted(maps.Keys(c.Write)), c.Remove))
	if _, err := git(ctx, root, nil, spec, "add", "-A", "-f", "--pathspec-from-file=-", "--pathspec-file-nul"); err != nil {
		return "", err
	}
	out, err := git(ctx, root, nil, nil, "diff", "--cached", "--name-status", "-z", "--no-renames", "--no-ext-diff", "HEAD")
	if err != nil {
		return "", err
	}
	var added, changed, removed int
	fields := strings.Split(string(out), "\x00")
	for i := 0; i+1 < len(fields); i += 2 {
		switch fields[i] {
		case "A":
			added++
		case "D":
			removed++
		default:
			changed++
		}
	}
	msg := fmt.Sprintf("migrate: - (+%d ~%d -%d)\n\nEva-Pass: %s\nEva-Kind: migrate\nEva-Writer: %s\nEva-Ops: create=%d revise=%d refused=0\nEva-Schema: %d\n",
		added, changed, removed, pass, o.Writer, added, changed, step.To)
	a := &author{name: "eva.md on " + o.Label, email: o.Writer + "@machines.eva-md.invalid"}
	if _, err := git(ctx, root, a, []byte(msg), "commit", "-q", "--no-verify", "--no-gpg-sign", "--cleanup=verbatim", "-F", "-"); err != nil {
		return "", err
	}
	return revParse(ctx, root, "HEAD")
}
