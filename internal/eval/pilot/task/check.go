package task

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"time"
)

// ownTests are the argv that run a repository's own tests, by language.
var ownTests = map[string][]string{
	LangGo:     {"go", "test", "-count=1", "./..."},
	LangPython: {"python3", "-m", "unittest", "discover"},
}

// Check applies the rules that run tests, to a task Load returned: each
// repository's own tests pass as it stands; every hidden test fails on
// the repositories as they stand and passes with solution/ laid over
// them; and, in a hidden decision, with decoy/ laid over them exactly the
// tests that encode a fact fail. Each run is bounded by limit, in an
// environment built as Grade builds it, under scratch, which must be
// empty or not exist. A rule broken is an *Error; any other error means
// the task could not be checked.
func Check(ctx context.Context, t Task, scratch string, limit time.Duration) error {
	if err := os.MkdirAll(scratch, 0o700); err != nil {
		return t.errorf("cannot make the scratch folder: %s", bare(err))
	}
	if entries, err := os.ReadDir(scratch); err != nil || len(entries) > 0 {
		return t.errorf("the scratch folder is not empty")
	}
	broken := func(rule, format string, args ...any) error {
		return &Error{Task: t.ID, Family: t.Family, Rule: rule, Why: fmt.Sprintf(format, args...)}
	}

	own := ownTests[t.Lang]
	for i, r := range t.Repos {
		s, err := newSandbox(ctx, t, filepath.Join(scratch, "own-"+strconv.Itoa(i+1)), own[:1], t.Dir)
		if err != nil {
			return err
		}
		if err := copyTree(filepath.Join(t.Dir, treeRepos, r), s.repo(r)); err != nil {
			return t.errorf("cannot copy repository %d: %s", i+1, bare(err))
		}
		ok, err := s.run(ctx, s.repo(r), own, limit)
		if err != nil {
			return t.errorf("the own tests of repository %d: %s", i+1, bare(err))
		}
		if !ok {
			return broken(ruleOwnTests, "the own tests of repository %d of %d fail as it stands", i+1, len(t.Repos))
		}
	}

	failed, err := t.gradeWith(ctx, filepath.Join(scratch, "start"), "", limit)
	if err != nil {
		return err
	}
	for _, test := range t.Tests {
		if !slices.Contains(failed, test.ID) {
			return broken(ruleStart, "hidden test %s passes on the repositories as they stand", test.ID)
		}
	}

	if failed, err = t.gradeWith(ctx, filepath.Join(scratch, "solution"), treeSolution, limit); err != nil {
		return err
	}
	if len(failed) > 0 {
		return broken(ruleSolution, "hidden test %s fails with the solution", failed[0])
	}

	if t.Family != FamilyHidden {
		return nil
	}
	if failed, err = t.gradeWith(ctx, filepath.Join(scratch, "decoy"), treeDecoy, limit); err != nil {
		return err
	}
	for _, test := range t.Tests {
		switch encodes, fails := len(test.Facts) > 0, slices.Contains(failed, test.ID); {
		case encodes && !fails:
			return broken(ruleDecoy, "hidden test %s passes with the decoy, though it encodes a fact", test.ID)
		case !encodes && fails:
			return broken(ruleDecoy, "hidden test %s fails with the decoy, though it encodes no fact", test.ID)
		}
	}
	return nil
}

// gradeWith lays the repositories, with the tree layer over them when
// it is not "", out in dir as workdir lays them out, and grades them
// there.
func (t Task) gradeWith(ctx context.Context, dir, layer string, limit time.Duration) ([]string, error) {
	workspace := filepath.Join(dir, "workspace")
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return nil, t.errorf("cannot make the scratch folder: %s", bare(err))
	}
	for i, r := range t.Repos {
		to := workspace
		if t.Workdir == WorkdirOutside {
			if err := os.MkdirAll(workspace, 0o755); err != nil {
				return nil, t.errorf("cannot make the workspace: %s", bare(err))
			}
			to = filepath.Join(workspace, r)
		}
		if err := copyTree(filepath.Join(t.Dir, treeRepos, r), to); err != nil {
			return nil, t.errorf("cannot copy repository %d: %s", i+1, bare(err))
		}
		if layer != "" {
			if err := lay(filepath.Join(t.Dir, layer, r), to); err != nil {
				return nil, t.errorf("cannot lay %s over repository %d: %s", layer, i+1, bare(err))
			}
		}
	}
	return Grade(ctx, t, workspace, filepath.Join(dir, "grade"), limit)
}
