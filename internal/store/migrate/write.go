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
		return fmt.Errorf("migrate: %w", err)
	}
	defer r.Close()
	for _, p := range slices.Sorted(slices.Values(c.Remove)) {
		if err := r.Remove(filepath.FromSlash(p)); err != nil && !errors.Is(err, fs.ErrNotExist) {
			return fmt.Errorf("migrate: remove: %w", err)
		}
		hook("remove")
		pruneDirs(r, p)
	}
	write := func(p string, b []byte) error {
		if dir := path.Dir(p); dir != "." {
			if err := r.MkdirAll(filepath.FromSlash(dir), 0o700); err != nil {
				return fmt.Errorf("migrate: folder: %w", err)
			}
		}
		err := paths.WriteFileAtomicAt(ctx, r, filepath.FromSlash(p), b, 0o600)
		hook("write")
		if err != nil {
			return fmt.Errorf("migrate: %w", err)
		}
		return nil
	}
	for _, p := range owned {
		if err := write(p, ownedFiles[p]); err != nil {
			return err
		}
	}
	for _, p := range slices.Sorted(maps.Keys(c.Write)) {
		if err := write(p, c.Write[p]); err != nil {
			return err
		}
	}
	return nil
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
