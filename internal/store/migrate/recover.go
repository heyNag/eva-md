package migrate

import (
	"bytes"
	"context"
	"crypto/sha1"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"hash"
	"io/fs"
	"maps"
	"os"
	"path"
	"path/filepath"
	"slices"
	"strings"
	"syscall"

	"github.com/heyNag/eva-md/internal/paths"
)

// passCommit reports whether commit is the journal's pass's commit: one
// parent, the commit before, and the pass's trailers.
func passCommit(ctx context.Context, root, commit string, j journal) bool {
	out, err := git(ctx, root, nil, nil, "cat-file", "commit", commit)
	if err != nil {
		return false
	}
	header, body, _ := strings.Cut(string(out), "\n\n")
	var parents []string
	for _, line := range strings.Split(header, "\n") {
		if p, ok := strings.CutPrefix(line, "parent "); ok {
			parents = append(parents, p)
		}
	}
	lines := strings.Split(body, "\n")
	return len(parents) == 1 && parents[0] == j.Before &&
		slices.Contains(lines, "Eva-Pass: "+j.Pass) && slices.Contains(lines, "Eva-Kind: migrate") &&
		slices.Contains(lines, fmt.Sprintf("Eva-Schema: %d", j.To))
}

// findPassCommit finds the journal's pass's commit on main's first-parent
// line from head back to the commit before: the commit just after it,
// which must be the pass's. A later commit, a later pass's or a person's,
// may stand above it. Without it main moved under an unfinished step.
func findPassCommit(ctx context.Context, root, head string, j journal) (string, error) {
	out, err := git(ctx, root, nil, nil, "rev-list", "--first-parent", fmt.Sprintf("--max-count=%d", maxWalk), head)
	if err != nil {
		return "", err
	}
	line := strings.Fields(string(out))
	i := slices.Index(line, j.Before)
	if i < 1 || !passCommit(ctx, root, line[i-1], j) {
		return "", ErrMoved
	}
	return line[i-1], nil
}

// checkBeforeRef returns what ref holds now, nothing or the commit before,
// and refuses one that keeps another commit.
func checkBeforeRef(ctx context.Context, root, ref, before string) (string, error) {
	out, err := git(ctx, root, nil, nil, "for-each-ref", "--format=%(refname)%00%(objectname)", ref)
	if err != nil {
		return "", err
	}
	for _, line := range strings.Split(string(out), "\n") {
		name, oid, _ := strings.Cut(line, "\x00")
		if name != ref {
			continue
		}
		if oid != before {
			return "", &BeforeRefError{Ref: ref}
		}
		return oid, nil
	}
	return "", nil
}

// checkIndex refuses a store with a merge, rebase, cherry-pick or revert
// under way, which a commit would fold in, and one whose index hides a
// file's changes from git status: an entry marked skip-worktree or
// assume-unchanged.
func checkIndex(ctx context.Context, root string) error {
	dir, err := gitDir(ctx, root)
	if err != nil {
		return err
	}
	for _, name := range []string{"MERGE_HEAD", "CHERRY_PICK_HEAD", "REVERT_HEAD", "rebase-merge", "rebase-apply", "sequencer"} {
		_, err := os.Lstat(filepath.Join(dir, name))
		if err == nil {
			return ErrUnderWay
		}
		if !errors.Is(err, fs.ErrNotExist) {
			return fsErr("read the git folder", err)
		}
	}
	out, err := git(ctx, root, nil, nil, "ls-files", "-v", "-z")
	if err != nil {
		return err
	}
	for _, entry := range strings.Split(string(out), "\x00") {
		if entry != "" && (entry[0] == 'S' || entry[0] >= 'a' && entry[0] <= 'z') {
			return ErrDirty
		}
	}
	return nil
}

func gitDir(ctx context.Context, root string) (string, error) {
	out, err := git(ctx, root, nil, nil, "rev-parse", "--absolute-git-dir")
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(string(out)), nil
}

// goBack puts what a run cut short left back to the commit before, which
// HEAD still is. Lock files a git cut short left are cleared first: only a
// writer holding the store lock runs git here. Then each path the run
// writes or removes, and the four the runner owns, goes back in the index
// and the work tree, but only when both hold what the run could have left
// there: the bytes of the commit before, the bytes the run writes, or
// nothing where the run removes or adds the path. Anything else there is a
// person's, made since the run was cut, and stays: the run then finds the
// tree dirty and refuses, and the journal stays for the next. Temporary
// files a write cut short left are removed. The work tree is put back with
// atomic writes, so a recovery cut short leaves no half-written file.
func goBack(ctx context.Context, root, before string, to int, p planned, ownedBytes map[string][]byte) error {
	dir, err := gitDir(ctx, root)
	if err != nil {
		return err
	}
	for _, name := range []string{"index.lock", "HEAD.lock", "refs/heads/main.lock", fmt.Sprintf("refs/eva-md/before-schema-%d.lock", to)} {
		err := os.Remove(filepath.Join(dir, filepath.FromSlash(name)))
		if err != nil && !errors.Is(err, fs.ErrNotExist) {
			return fsErr("remove a lock", err)
		}
	}
	format, err := git(ctx, root, nil, nil, "rev-parse", "--show-object-format")
	if err != nil {
		return err
	}
	index, err := indexEntries(ctx, root)
	if err != nil {
		return err
	}
	r, err := paths.OpenRoot(root, false)
	if err != nil {
		return fsErr("open the store", err)
	}
	defer r.Close()

	removed := map[string]bool{}
	for _, q := range p.change.Remove {
		removed[q] = true
	}
	var ours, restore []string
	drop := map[string]bool{}
	for _, q := range slices.Concat(owned, slices.Sorted(maps.Keys(p.change.Write)), p.change.Remove) {
		want, writes := ownedBytes[q]
		if !writes {
			want, writes = p.change.Write[q]
		}
		l := left{tree: p.tree, path: q, want: want, writes: writes, removes: removed[q]}
		if !l.inWorkTree(r) || !l.inIndex(index, strings.TrimSpace(string(format))) {
			continue
		}
		ours = append(ours, q)
		if e, ok := p.tree.entries[q]; ok && e.mode != gitlinkMode {
			restore = append(restore, q)
		} else if !ok {
			drop[q] = true
		}
	}
	if len(ours) > 0 {
		if _, err := git(ctx, root, nil, nulList(ours), "reset", "-q", before, "--pathspec-from-file=-", "--pathspec-file-nul"); err != nil {
			return err
		}
	}
	out, err := git(ctx, root, nil, nil, "ls-files", "-o", "-z")
	if err != nil {
		return err
	}
	for _, f := range strings.Split(string(out), "\x00") {
		if f == "" || !drop[f] && !strings.HasPrefix(path.Base(f), paths.TempPrefix) {
			continue
		}
		if err := r.Remove(filepath.FromSlash(f)); err != nil && !errors.Is(err, fs.ErrNotExist) {
			return fsErr("remove", err)
		}
		hook("remove")
		pruneDirs(r, f)
	}
	for _, q := range restore {
		e := p.tree.entries[q]
		info, err := r.Lstat(filepath.FromSlash(q))
		switch {
		case err == nil && info.IsDir():
			// A folder the run made where the file was; what the run
			// put in it is gone.
			if err := r.Remove(filepath.FromSlash(q)); err != nil {
				return fsErr("remove a folder", err)
			}
			hook("remove")
		case err == nil && e.mode == symlinkMode && info.Mode()&fs.ModeSymlink != 0:
			continue // the link as it was checked out
		case err == nil && info.Mode().IsRegular() && sameFile(r, q, info, p.tree.data[q]):
			continue
		}
		// A symlink comes back as a file of its target, as git checks one
		// out with core.symlinks off.
		mode := os.FileMode(0o600)
		if e.mode == "100755" {
			mode = 0o700
		}
		if err := writeAt(ctx, r, q, p.tree.data[q], mode); err != nil {
			return err
		}
	}
	return nil
}

// left is one path a run writes or removes, and what the run could have
// left at it.
type left struct {
	tree    *tree
	path    string
	want    []byte // the bytes the run writes, when writes
	writes  bool
	removes bool
}

// absentOK is whether the run could have left nothing at the path: it
// adds the path, or removes it.
func (l left) absentOK() bool {
	_, inBefore := l.tree.entries[l.path]
	return !inBefore || l.removes
}

// inWorkTree reports whether the work tree holds at the path what the run
// could have left.
func (l left) inWorkTree(r *os.Root) bool {
	e, inBefore := l.tree.entries[l.path]
	name := filepath.FromSlash(l.path)
	info, err := r.Lstat(name)
	switch {
	case errors.Is(err, fs.ErrNotExist) || errors.Is(err, syscall.ENOTDIR):
		return l.absentOK()
	case err != nil:
		return false
	case info.IsDir():
		// The run made a folder where the file was, or there was none;
		// a submodule's folder stays as it was.
		return l.absentOK() || inBefore && e.mode == gitlinkMode
	case info.Mode()&fs.ModeSymlink != 0:
		target, err := r.Readlink(name)
		return err == nil && inBefore && e.mode == symlinkMode && target == string(l.tree.data[l.path])
	case info.Mode().IsRegular():
		return inBefore && e.mode != gitlinkMode && sameFile(r, l.path, info, l.tree.data[l.path]) ||
			l.writes && sameFile(r, l.path, info, l.want)
	}
	return false
}

// inIndex reports whether the index holds at the path what the run could
// have staged: the commit before's entry, the run's bytes as a plain file,
// or nothing where the run removes or adds the path.
func (l left) inIndex(index map[string]indexEntry, format string) bool {
	ie, ok := index[l.path]
	if !ok {
		return l.absentOK()
	}
	if ie.stage != "0" {
		return false
	}
	if e, inBefore := l.tree.entries[l.path]; inBefore && ie.mode == e.mode && ie.oid == e.oid {
		return true
	}
	return l.writes && ie.mode == "100644" && ie.oid == blobID(format, l.want)
}

type indexEntry struct{ mode, oid, stage string }

// indexEntries lists the index, by path; a path in conflict keeps a stage
// above 0.
func indexEntries(ctx context.Context, root string) (map[string]indexEntry, error) {
	out, err := git(ctx, root, nil, nil, "ls-files", "-s", "-z")
	if err != nil {
		return nil, err
	}
	index := map[string]indexEntry{}
	for _, entry := range strings.Split(string(out), "\x00") {
		if entry == "" {
			continue
		}
		meta, p, ok := strings.Cut(entry, "\t")
		f := strings.Fields(meta)
		if !ok || len(f) != 3 {
			return nil, errors.New("migrate: unexpected index listing")
		}
		if prev, seen := index[p]; seen && prev.stage != "0" {
			continue
		}
		index[p] = indexEntry{mode: f[0], oid: f[1], stage: f[2]}
	}
	return index, nil
}

// sameFile reports whether the regular file at p holds exactly b.
func sameFile(r *os.Root, p string, info fs.FileInfo, b []byte) bool {
	if info.Size() != int64(len(b)) {
		return false
	}
	got, err := r.ReadFile(filepath.FromSlash(p))
	return err == nil && bytes.Equal(got, b)
}

// blobID is the object id git gives b as a blob, in the repository's
// object format.
func blobID(format string, b []byte) string {
	var h hash.Hash = sha1.New()
	if format == "sha256" {
		h = sha256.New()
	}
	fmt.Fprintf(h, "blob %d\x00", len(b))
	h.Write(b)
	return hex.EncodeToString(h.Sum(nil))
}

// nulList is paths as git reads them with --pathspec-file-nul.
func nulList(list []string) []byte {
	var b strings.Builder
	for _, p := range list {
		b.WriteString(p)
		b.WriteByte(0)
	}
	return []byte(b.String())
}
