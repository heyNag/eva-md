package results

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
)

// Baseline is the accepted set of rows every gate compares with: rows of
// one commit, measured on a clean tree.
type Baseline struct {
	V      int    `json:"v"`
	Commit string `json:"commit"`
	Rows   []Row  `json:"rows"`
}

// Accept makes the baseline of rows and writes it to file, replacing the
// one there. It refuses rows measured on a dirty tree, rows of more than
// one commit, a commit the repository at repo does not hold, rows the
// catalogue does not describe, and a metric measured on more than one
// host or model.
func Accept(ctx context.Context, repo string, cat Catalogue, rows []Row, file string) (Baseline, error) {
	if len(rows) == 0 {
		return Baseline{}, errors.New("baseline: there are no rows")
	}
	b := Baseline{V: Version, Commit: rows[0].Commit, Rows: rows}
	for i, r := range rows {
		if r.Dirty {
			return Baseline{}, fmt.Errorf("baseline: row %d was measured on a dirty tree; a baseline is measured on a commit alone", i+1)
		}
		if r.Commit != b.Commit {
			return Baseline{}, fmt.Errorf("baseline: the rows were measured on more than one commit, %s and %s", short(b.Commit), short(r.Commit))
		}
	}
	if err := b.check(cat); err != nil {
		return Baseline{}, err
	}
	if _, err := git(ctx, repo, "rev-parse", "--verify", "--quiet", b.Commit+"^{commit}"); err != nil {
		return Baseline{}, fmt.Errorf("baseline: the rows' commit %s is not in the repository", short(b.Commit))
	}
	data, err := json.MarshalIndent(b, "", "  ")
	if err != nil {
		return Baseline{}, fmt.Errorf("baseline: %w", err)
	}
	if err := writeFile(file, append(data, '\n')); err != nil {
		return Baseline{}, fmt.Errorf("baseline: %w", err)
	}
	return b, nil
}

// LoadBaseline reads the baseline at file and checks it against the
// catalogue.
func LoadBaseline(file string, cat Catalogue) (Baseline, error) {
	data, err := os.ReadFile(file)
	if err != nil {
		return Baseline{}, fmt.Errorf("baseline: %w", err)
	}
	var b Baseline
	if err := decodeStrict(data, &b); err != nil {
		return Baseline{}, errors.New("baseline: the file is not a baseline")
	}
	if b.V != Version {
		return Baseline{}, fmt.Errorf("baseline: its format is %d, not %d", b.V, Version)
	}
	if !commitPattern.MatchString(b.Commit) {
		return Baseline{}, errors.New("baseline: commit is not a full commit id")
	}
	if len(b.Rows) == 0 {
		return Baseline{}, errors.New("baseline: it holds no rows")
	}
	for i, r := range b.Rows {
		if msg := r.check(); msg != "" {
			return Baseline{}, fmt.Errorf("baseline: row %d: %s", i+1, msg)
		}
		if r.Dirty || r.Commit != b.Commit {
			return Baseline{}, fmt.Errorf("baseline: row %d is not of the baseline's clean commit", i+1)
		}
	}
	return b, b.check(cat)
}

// check checks the rows against the catalogue, and that each metric was
// measured on one host and model.
func (b Baseline) check(cat Catalogue) error {
	if err := cat.Check(b.Rows); err != nil {
		return fmt.Errorf("baseline: %w", err)
	}
	for _, m := range cat.Metrics {
		if _, err := keyOf(of(b.Rows, m.Name)); err != nil {
			return fmt.Errorf("baseline: %s: %w", m.Name, err)
		}
	}
	return nil
}

// Changed returns the first of paths that differs between the commits
// from and to in the repository at repo, or "" when none does: whether a
// baseline measured at from is stale at to. Each path is taken from the
// repository's root, literally, never as a pattern. A path that is in
// neither commit is an error, since it would never go stale: the
// catalogue names it wrongly.
func Changed(ctx context.Context, repo, from, to string, paths []string) (string, error) {
	var commits [2]string
	for i, rev := range []string{from, to} {
		if rev == "" || strings.HasPrefix(rev, "-") {
			return "", errors.New("a revision is empty or starts with -")
		}
		out, err := git(ctx, repo, "rev-parse", "--verify", "--quiet", rev+"^{commit}")
		if err != nil {
			return "", fmt.Errorf("%s is not a commit in the repository", short(rev))
		}
		commits[i] = strings.TrimSpace(out)
	}
	for _, p := range paths {
		_, err := git(ctx, repo, "diff", "--quiet", "--no-renames", "--no-ext-diff", "--no-textconv", commits[0], commits[1], "--", ":(top,literal)"+p)
		var exit *exec.ExitError
		switch {
		case err == nil:
		case errors.As(err, &exit) && exit.ExitCode() == 1:
			return p, nil
		default:
			return "", err
		}
		// Unchanged, so the same in both: in the second, or in neither.
		if _, err := git(ctx, repo, "cat-file", "-e", commits[1]+":"+p); err != nil {
			return "", fmt.Errorf("%s is in neither commit", p)
		}
	}
	return "", nil
}

// MainLines are the refs the main line is read from, the first that the
// repository has: the local main branch, then the remote's.
var MainLines = []string{"refs/heads/main", "refs/remotes/origin/main"}

// StartOf returns the commit the change the rows measure starts from: the
// merge base of the rows' commit and the main line, in full. That is the
// commit a baseline must still describe. A change of a covered path on
// the rows' own branch is what a gate judges, so it never makes the
// baseline stale; one on the main line since the baseline was accepted
// does. The rows must share one commit, and the repository at repo must
// hold it.
func StartOf(ctx context.Context, repo string, rows []Row) (string, error) {
	commits := commitsOf(rows)
	switch {
	case len(commits) == 0:
		return "", errors.New("there are no rows")
	case len(commits) > 1:
		return "", fmt.Errorf("the rows were measured on %d commits", len(commits))
	}
	if _, err := git(ctx, repo, "rev-parse", "--verify", "--quiet", commits[0]+"^{commit}"); err != nil {
		return "", fmt.Errorf("the rows' commit %s is not in the repository", short(commits[0]))
	}
	for _, ref := range MainLines {
		if _, err := git(ctx, repo, "rev-parse", "--verify", "--quiet", ref+"^{commit}"); err != nil {
			continue
		}
		out, err := git(ctx, repo, "merge-base", commits[0], ref)
		if err != nil {
			return "", fmt.Errorf("the rows' commit %s shares no history with %s", short(commits[0]), ref)
		}
		return strings.TrimSpace(out), nil
	}
	return "", errors.New("the repository has no main branch")
}

// commitsOf returns the commits the rows were measured on, in the order
// they first appear.
func commitsOf(rows []Row) []string {
	var out []string
	for _, r := range rows {
		if !slices.Contains(out, r.Commit) {
			out = append(out, r.Commit)
		}
	}
	return out
}

// Header says what a gate judges, in one line: the rows' commit and
// whether its tree was dirty, the baseline's commit, and base, the commit
// the baseline must still describe.
func Header(rows []Row, b Baseline, base string) string {
	measured := "no rows"
	switch commits := commitsOf(rows); {
	case len(commits) == 1:
		tree := "a clean tree"
		if slices.ContainsFunc(rows, func(r Row) bool { return r.Dirty }) {
			tree = "a dirty tree"
		}
		measured = fmt.Sprintf("rows of commit %s on %s", short(commits[0]), tree)
	case len(commits) > 1:
		measured = fmt.Sprintf("rows of %d commits", len(commits))
	}
	return fmt.Sprintf("%s, against the baseline of commit %s, which must still describe %s", measured, short(b.Commit), short(base))
}

// short shortens a full commit id for a message, and leaves any other
// revision as it is.
func short(commit string) string {
	if commitPattern.MatchString(commit) {
		return commit[:12]
	}
	return commit
}

// writeFile writes data to file through a temporary file beside it, so a
// reader sees the old file or the new one, never a part.
func writeFile(file string, data []byte) error {
	if err := os.MkdirAll(filepath.Dir(file), 0o755); err != nil {
		return err
	}
	tmp, err := os.CreateTemp(filepath.Dir(file), "."+filepath.Base(file)+".tmp-")
	if err != nil {
		return err
	}
	_, err = tmp.Write(data)
	if cerr := tmp.Close(); err == nil {
		err = cerr
	}
	if err == nil {
		err = os.Chmod(tmp.Name(), 0o644)
	}
	if err == nil {
		err = os.Rename(tmp.Name(), file)
	}
	if err != nil {
		os.Remove(tmp.Name())
	}
	return err
}

// of returns the rows of the metric named name.
func of(rows []Row, name string) []Row {
	var out []Row
	for _, r := range rows {
		if r.Metric == name {
			out = append(out, r)
		}
	}
	return out
}

// key is what a metric's rows must share to be compared with a baseline's.
type key struct{ instrument, host, model string }

func (k key) String() string {
	s := k.instrument
	if k.host != "" {
		s += " on " + k.host
	}
	if k.model != "" {
		s += " with " + k.model
	}
	return s
}

// keyOf returns the key the rows share, the zero key for none, or an
// error when they do not share one.
func keyOf(rows []Row) (key, error) {
	var k key
	for i, r := range rows {
		rk := key{r.Instrument, r.Host, r.Model}
		if i > 0 && rk != k {
			return key{}, fmt.Errorf("the rows mix %s and %s", k, rk)
		}
		k = rk
	}
	return k, nil
}
