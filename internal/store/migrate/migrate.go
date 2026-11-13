// Package migrate turns a store, or a v1 wiki, of one schema generation
// into the next, in one step that survives a crash, and holds the reader
// rule: a binary that meets a store newer than it knows stops rather than
// misreads it. A step is a pure function of the tree committed at HEAD; the
// runner takes the store lock, keeps a journal in the state folder, keeps
// the commit before under a ref, writes the step's files and the four it
// owns, and makes one commit as eva.md, never as the person and never
// signed. Nothing calls it yet.
package migrate

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"maps"
	"path"
	"path/filepath"
	"slices"
	"strings"
	"unicode/utf8"

	"github.com/heyNag/eva-md/internal/paths"
	"github.com/heyNag/eva-md/internal/wikigit"
)

// Tree is what a step sees: the tree committed at HEAD, its paths with
// slashes, sorted, and each file's bytes.
type Tree interface {
	Paths() []string
	Read(path string) ([]byte, error)
}

// Step turns generation From into To. MinReader is the lowest generation a
// binary must know to read the result. Classes name the keys of its
// counts. Plan sees only the tree: no clock, nothing of the machine.
type Step struct {
	From, To, MinReader int
	Classes             []string
	Plan                func(Tree) (Change, error)
}

// Change is what a step does to the tree: files to write, paths to remove,
// and its counts. It may not name the four files the runner owns.
type Change struct {
	Write  map[string][]byte
	Remove []string
	Counts Counts
}

// Counts are a step's numbers. Read and Written are by class, keyed only by
// the step's Classes. HeldBack is the runner's: the files whose written
// lines the secret scan flags.
type Counts struct {
	Read, Written                                                          map[string]int
	Removed, Unreadable, PeoplesSections, Protected, Unaccounted, HeldBack int
}

// String is one line of numbers by class, with no path or text.
func (c Counts) String() string {
	byClass := func(m map[string]int) string {
		var parts []string
		for _, k := range slices.Sorted(maps.Keys(m)) {
			parts = append(parts, fmt.Sprintf("%s=%d", k, m[k]))
		}
		if len(parts) == 0 {
			return "none"
		}
		return strings.Join(parts, " ")
	}
	return fmt.Sprintf("read %s; written %s; removed %d; unreadable %d; peoples_sections %d; protected %d; unaccounted %d; held_back %d",
		byClass(c.Read), byClass(c.Written), c.Removed, c.Unreadable, c.PeoplesSections, c.Protected, c.Unaccounted, c.HeldBack)
}

// Options are a run's machine: where its state and locks are, the store id
// a store without one gets, the writer and label the commit is made by, and
// the pass id, which a journal found at the start replaces.
type Options struct {
	StateDir, LocksDir, StoreID, Writer, Label, Pass string
}

// Result is what a run did.
type Result struct {
	Generation int    // the generation found at the start
	Committed  bool   // the step's commit is on main
	Commit     string // the step's commit
	Before     string // the commit before it
	Pass       string // the pass the commit carries
	Counts     Counts
}

var (
	ErrBusy        = errors.New("migrate: another writer holds the store")
	ErrDirty       = errors.New("migrate: the store has changes that are not committed")
	ErrGeneration  = errors.New("migrate: the store is not of the step's generation")
	ErrUnaccounted = errors.New("migrate: some of the store is unaccounted for")
	ErrHeldBack    = errors.New("migrate: a written file looks like it holds a secret")
	ErrNotMain     = errors.New("migrate: the store is not on main")
	ErrMoved       = errors.New("migrate: main moved during an unfinished step")
	ErrJournalStep = errors.New("migrate: an unfinished step of other generations is recorded")
	ErrUnderWay    = errors.New("migrate: a merge, rebase, cherry-pick or revert is under way in the store")
	ErrBeforeRef   = errors.New("migrate: the ref that keeps the store before a migration holds another commit")
)

// BeforeRefError is the refusal to move refs/eva-md/before-schema-<To>
// when it holds a commit other than the one before: that ref is the only
// way back to a store as it was before a migration, so nothing moves it
// unasked. It is ErrBeforeRef by errors.Is.
type BeforeRefError struct{ Ref string }

func (e *BeforeRefError) Error() string {
	return fmt.Sprintf("migrate: %s already keeps another commit; to let a migration replace it, remove it with git update-ref -d %s", e.Ref, e.Ref)
}

func (e *BeforeRefError) Is(target error) bool { return target == ErrBeforeRef }

// maxWalk bounds the walk back along main for the step's commit.
const maxWalk = 10000

// hook marks a point a crash can stop a run at: after every file write,
// git call and journal write or removal. Tests set it.
var hook = func(string) {}

// Run carries the store at root through step. It takes the store lock
// without waiting. At the step's To it makes no commit, whatever the work
// tree holds; at any generation but From, or with a min_reader above
// Known, it refuses. It refuses a tree with changes it did not leave, a
// merge or the like under way, and a before ref that keeps another
// commit, and makes no commit when the step leaves anything unaccounted
// for, a file v1 hid would show, or a written line looks like a secret. A
// journal found at the start is a run cut short: its commit, found on
// main, is finished, or what the run left goes back to the commit before
// and the step runs again under the journal's pass.
func Run(ctx context.Context, root string, step Step, o Options) (Result, error) {
	if err := checkStep(step); err != nil {
		return Result{}, err
	}
	if err := checkOptions(o); err != nil {
		return Result{}, err
	}
	unlock, busy, err := paths.TryLock(o.LocksDir, "store.lock")
	if err != nil {
		return Result{}, fmt.Errorf("migrate: %w", err)
	}
	if busy {
		return Result{}, ErrBusy
	}
	defer unlock()
	if ref, err := git(ctx, root, nil, nil, "symbolic-ref", "--quiet", "HEAD"); err != nil || strings.TrimSpace(string(ref)) != "refs/heads/main" {
		return Result{}, ErrNotMain
	}
	head, err := revParse(ctx, root, "HEAD")
	if err != nil {
		return Result{}, err
	}
	pass, storeID := o.Pass, o.StoreID
	j, err := readJournal(o.StateDir)
	if err != nil {
		return Result{}, err
	}
	if j != nil {
		if j.From != step.From || j.To != step.To {
			return Result{}, ErrJournalStep
		}
		pass = j.Pass
		if j.StoreID != "" {
			storeID = j.StoreID
		}
		if head != j.Before {
			// The step's commit is on main, though maybe no longer at
			// its tip: the run was cut before it removed the journal.
			commit, err := findPassCommit(ctx, root, head, *j)
			if err != nil {
				return Result{}, err
			}
			p, err := plan(ctx, root, step, j.Before)
			if err != nil {
				return Result{}, err
			}
			if err := removeJournal(o.StateDir); err != nil {
				return Result{}, err
			}
			return Result{Generation: p.gen, Committed: true, Commit: commit, Before: j.Before, Pass: pass, Counts: p.counts}, nil
		}
	}
	p, err := plan(ctx, root, step, head)
	if err != nil {
		return Result{}, err
	}
	if p.gen == step.To {
		// Done already: no commit, whatever a person has changed since.
		return Result{Generation: p.gen}, nil
	}
	if p.old.storeID == "" && storeID == "" {
		return Result{}, errors.New("migrate: a store without an id needs one")
	}
	ref := fmt.Sprintf("refs/eva-md/before-schema-%d", step.To)
	refOld, err := checkBeforeRef(ctx, root, ref, head)
	if err != nil {
		return Result{}, err
	}
	if err := checkIndex(ctx, root); err != nil {
		return Result{}, err
	}
	owned := ownedFiles(p.old, step.To, step.MinReader, storeID)
	if j != nil {
		if err := goBack(ctx, root, head, step.To, p, owned); err != nil {
			return Result{}, err
		}
	}
	if dirty, err := dirty(ctx, root); err != nil {
		return Result{}, err
	} else if dirty {
		return Result{}, ErrDirty
	}
	shown, err := exposed(ctx, root, owned[ignorePath])
	if err != nil {
		return Result{}, err
	}
	p.counts.Unaccounted += shown
	r := Result{Generation: p.gen, Before: head, Pass: pass, Counts: p.counts}
	switch {
	case p.counts.Unaccounted > 0:
		return r, ErrUnaccounted
	case p.counts.HeldBack > 0:
		return r, ErrHeldBack
	}
	if j == nil {
		if err := writeJournal(o.StateDir, journal{Pass: pass, From: step.From, To: step.To, Before: head, StoreID: storeID}); err != nil {
			return Result{}, err
		}
	}
	// With the old value given, git moves the ref only from what was
	// checked: absent, or already the commit before.
	if _, err := git(ctx, root, nil, nil, "update-ref", ref, head, refOld); err != nil {
		return Result{}, err
	}
	if err := writeFiles(ctx, root, p.change, owned); err != nil {
		return Result{}, err
	}
	commit, err := commitStep(ctx, root, p.change, step, o, pass)
	if err != nil {
		return Result{}, err
	}
	if err := removeJournal(o.StateDir); err != nil {
		return Result{}, err
	}
	r.Committed, r.Commit = true, commit
	return r, nil
}

// DryRun plans step on the tree committed at root's HEAD and returns its
// counts, with the files v1 hid that the step's run would show counted as
// unaccounted, as Run counts them. It takes no lock and writes nothing
// anywhere. At the step's To the counts are zero.
func DryRun(ctx context.Context, root string, step Step) (Counts, error) {
	if err := checkStep(step); err != nil {
		return Counts{}, err
	}
	head, err := revParse(ctx, root, "HEAD")
	if err != nil {
		return Counts{}, err
	}
	p, err := plan(ctx, root, step, head)
	if err != nil || p.gen == step.To {
		return Counts{}, err
	}
	shown, err := exposed(ctx, root, ownedFiles(p.old, step.To, step.MinReader, "")[ignorePath])
	if err != nil {
		return Counts{}, err
	}
	p.counts.Unaccounted += shown
	return p.counts, nil
}

type planned struct {
	gen    int
	old    manifest
	tree   *tree
	change Change
	counts Counts
}

// plan reads commit's tree, its generation, and, at From, the step's
// change, checked, with the files the secret scan holds back counted.
func plan(ctx context.Context, root string, step Step, commit string) (planned, error) {
	t, err := readTree(ctx, root, commit)
	if err != nil {
		return planned{}, err
	}
	gen, old, err := generation(t)
	if err != nil {
		return planned{}, err
	}
	// The reader rule holds for the runner too: a store that needs a
	// newer reader is neither read for a step nor written.
	if old.minReader > Known {
		return planned{}, &TooOldError{StoreID: old.storeID, Schema: old.schema, MinReader: old.minReader, Known: Known}
	}
	if gen == step.To {
		return planned{gen: gen}, nil
	}
	if gen != step.From {
		return planned{}, ErrGeneration
	}
	c, err := step.Plan(t)
	if err != nil {
		return planned{}, fmt.Errorf("migrate: the step's plan failed: %w", err)
	}
	if err := checkChange(step, t, c); err != nil {
		return planned{}, err
	}
	counts := c.Counts
	counts.Read, counts.Written = maps.Clone(c.Counts.Read), maps.Clone(c.Counts.Written)
	for _, b := range c.Write {
		if len(wikigit.SecretLines(string(b))) > 0 {
			counts.HeldBack++
		}
	}
	return planned{gen: gen, old: old, tree: t, change: c, counts: counts}, nil
}

// generation is store.toml's schema, else v1's schema_version.
func generation(t *tree) (int, manifest, error) {
	if b, err := t.Read(manifestPath); err == nil {
		m, err := parseManifest(b)
		if err != nil {
			return 0, manifest{}, fmt.Errorf("migrate: store.toml: %w", err)
		}
		return m.schema, m, nil
	} else if !errors.Is(err, fs.ErrNotExist) {
		return 0, manifest{}, fmt.Errorf("migrate: store.toml: %w", err)
	}
	b, err := t.Read(markerPath)
	if err != nil {
		return 0, manifest{}, errors.New("migrate: the store names no generation")
	}
	n, err := parseMarker(b)
	if err != nil {
		return 0, manifest{}, fmt.Errorf("migrate: schema.toml: %w", err)
	}
	return n, manifest{}, nil
}

func checkStep(s Step) error {
	switch {
	case s.From < 1 || s.To <= s.From:
		return errors.New("migrate: a step goes from a generation to a later one")
	case s.MinReader < 1 || s.MinReader > s.To:
		return errors.New("migrate: a step's minimum reader is from 1 to its To")
	case s.Plan == nil:
		return errors.New("migrate: a step has no plan")
	}
	seen := map[string]bool{}
	for _, c := range s.Classes {
		if !reClass.MatchString(c) || seen[c] {
			return errors.New("migrate: a step's classes are distinct names of a-z0-9_")
		}
		seen[c] = true
	}
	return nil
}

func checkOptions(o Options) error {
	switch {
	case !filepath.IsAbs(o.StateDir) || !filepath.IsAbs(o.LocksDir):
		return errors.New("migrate: the state and lock folders must be absolute")
	case o.StoreID != "" && !reStoreID.MatchString(o.StoreID):
		return errors.New("migrate: the store id is not an s id")
	case !reWriter.MatchString(o.Writer):
		return errors.New("migrate: the writer is not a writer id")
	case !reLabel.MatchString(o.Label):
		return errors.New("migrate: the label is not 1 to 32 of a-z0-9-")
	case !rePass.MatchString(o.Pass):
		return errors.New("migrate: the pass is not a pass id")
	}
	return nil
}

// checkChange holds a step to its contract: paths that are plain and
// relative, none of the runner's, removals of files the tree has, no path
// both written and removed, a resulting tree in which no two paths differ
// only in case and no file is another's folder, and counts keyed by the
// step's classes that agree with the change.
func checkChange(step Step, t *tree, c Change) error {
	inTree := map[string]bool{}
	for _, p := range t.paths {
		inTree[p] = true
	}
	final := maps.Clone(inTree)
	removed := map[string]bool{}
	for _, p := range c.Remove {
		if err := checkPath(p); err != nil {
			return err
		}
		if !inTree[p] || removed[p] {
			return errors.New("migrate: the step removes a path the tree does not hold, or one twice")
		}
		if _, ok := c.Write[p]; ok {
			return errors.New("migrate: the step both writes and removes a path")
		}
		removed[p] = true
		delete(final, p)
	}
	for p := range c.Write {
		if err := checkPath(p); err != nil {
			return err
		}
		final[p] = true
	}
	// A git settings file of the old tree that the result keeps, but the
	// runner's two, would change the bytes git takes in beside it, or
	// what it hides: the step removes it, or carries it under another name.
	for p := range final {
		if !slices.Contains(owned, p) && slices.ContainsFunc(gitFiles, func(g string) bool { return strings.EqualFold(path.Base(p), g) }) {
			return errors.New("migrate: the result keeps a git settings file of the old tree")
		}
	}
	for _, p := range owned {
		final[p] = true
	}
	folded := map[string]string{}
	for p := range final {
		k := strings.ToLower(p)
		if q, ok := folded[k]; ok && q != p {
			return errors.New("migrate: two paths of the result differ only in case")
		}
		folded[k] = p
	}
	for p := range final {
		for dir := path.Dir(p); dir != "."; dir = path.Dir(dir) {
			if _, ok := folded[strings.ToLower(dir)]; ok {
				return errors.New("migrate: a file of the result is another's folder")
			}
		}
	}
	classes := map[string]bool{}
	for _, k := range step.Classes {
		classes[k] = true
	}
	written := 0
	for _, m := range []map[string]int{c.Counts.Read, c.Counts.Written} {
		for k, n := range m {
			if !classes[k] || n < 0 {
				return errors.New("migrate: a count's key is not one of the step's classes")
			}
		}
	}
	for _, n := range c.Counts.Written {
		written += n
	}
	k := c.Counts
	switch {
	case k.Removed < 0 || k.Unreadable < 0 || k.PeoplesSections < 0 || k.Protected < 0 || k.Unaccounted < 0:
		return errors.New("migrate: a count is negative")
	case k.HeldBack != 0:
		return errors.New("migrate: only the runner counts what it holds back")
	case written != len(c.Write) || k.Removed != len(c.Remove):
		return errors.New("migrate: the counts do not agree with the change")
	}
	return nil
}

// checkPath refuses a path that is not plain and relative, names a folder
// .git in any case, or is one of the runner's. checkChange refuses a git
// settings file at any other path of the result.
func checkPath(p string) error {
	if p == "" || !utf8.ValidString(p) || strings.HasPrefix(p, "/") || strings.ContainsFunc(p, func(r rune) bool { return r < 0x20 || r == 0x7f || r == '\\' }) {
		return errors.New("migrate: the step names a path that is not plain")
	}
	for _, seg := range strings.Split(p, "/") {
		if seg == "" || seg == "." || seg == ".." || strings.EqualFold(seg, ".git") {
			return errors.New("migrate: the step names a path that is not plain")
		}
	}
	for _, q := range owned {
		if strings.EqualFold(p, q) {
			return errors.New("migrate: the step names a file the runner owns")
		}
	}
	return nil
}

func revParse(ctx context.Context, root, rev string) (string, error) {
	out, err := git(ctx, root, nil, nil, "rev-parse", "--verify", "--quiet", rev+"^{commit}")
	if err != nil {
		return "", errors.New("migrate: the store has no commit")
	}
	return strings.TrimSpace(string(out)), nil
}

// dirty reports a change git status sees, tracked or not.
func dirty(ctx context.Context, root string) (bool, error) {
	out, err := git(ctx, root, nil, nil, "status", "--porcelain", "-z", "--untracked-files=all", "--ignore-submodules=none")
	if err != nil {
		return false, err
	}
	return len(out) > 0, nil
}
