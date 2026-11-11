package migrate

import (
	"errors"
	"maps"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/heyNag/eva-md/internal/config"
	"github.com/heyNag/eva-md/internal/paths"
	"github.com/heyNag/eva-md/internal/store/profile"
	"github.com/heyNag/eva-md/internal/wikigit"
)

func TestRunCarriesAV1WikiToAStore(t *testing.T) {
	isolate(t)
	dir := v1Wiki(t, v1Files)
	before := gitT(t, dir, "rev-parse", "HEAD")
	o := machine(t, writerA, "laptop", passA)
	r, err := Run(bg(), dir, synthetic, o)
	if err != nil {
		t.Fatal(err)
	}
	head := gitT(t, dir, "rev-parse", "HEAD")
	if !r.Committed || r.Commit != head || r.Before != before || r.Pass != passA || r.Generation != 1 {
		t.Fatalf("result = %+v; head %s, before %s", r, head, before)
	}
	if got := gitT(t, dir, "rev-parse", "HEAD^"); got != before {
		t.Fatalf("the commit's parent is %s, want %s", got, before)
	}

	// The tree: each v1 file but the runner's under legacy/v1/, byte for
	// byte, and the new files.
	want := []string{".eva-md/schema.toml", ".eva-md/store.toml", ".gitattributes", ".gitignore", "README.md", "index.md"}
	for p, s := range v1Files {
		if slices.Contains(owned, p) {
			continue
		}
		want = append(want, "legacy/v1/"+strings.ToLower(p))
		if got := gitT(t, dir, "show", "HEAD:legacy/v1/"+strings.ToLower(p)); got != strings.TrimSpace(s) {
			t.Errorf("legacy/v1/%s = %q, want %q", strings.ToLower(p), got, s)
		}
	}
	slices.Sort(want)
	if got := strings.Split(gitT(t, dir, "ls-tree", "-r", "--name-only", "HEAD"), "\n"); !slices.Equal(got, want) {
		t.Fatalf("tree =\n%v\nwant\n%v", got, want)
	}
	files := map[string]string{
		".eva-md/store.toml":  "schema = 2\nmin_reader = 2\nstore_id = \"" + storeID + "\"\nepoch = 1\n",
		".eva-md/schema.toml": "schema_version = 2\n",
		".gitattributes":      "* -text -filter -ident\n",
		".gitignore":          ".tmp-*\n",
		"index.md":            newIndex,
		"README.md":           newReadme,
	}
	for p, s := range files {
		b, err := os.ReadFile(filepath.Join(dir, p))
		if err != nil || string(b) != s {
			t.Errorf("%s = %q, %v; want %q", p, b, err, s)
		}
		info, err := os.Stat(filepath.Join(dir, p))
		if err != nil || info.Mode().Perm() != 0o600 {
			t.Errorf("%s: mode %v, %v; want 0600", p, info.Mode(), err)
		}
	}

	// The commit.
	msg := gitT(t, dir, "log", "-1", "--format=%B")
	wantMsg := "migrate: - (+10 ~5 -7)\n\nEva-Pass: " + passA + "\nEva-Kind: migrate\nEva-Writer: " + writerA + "\nEva-Ops: create=10 revise=5 refused=0\nEva-Schema: 2"
	if msg != wantMsg {
		t.Errorf("message =\n%s\nwant\n%s", msg, wantMsg)
	}
	if got := gitT(t, dir, "log", "-1", "--format=%an <%ae>|%cn <%ce>"); got != "eva.md on laptop <"+writerA+"@machines.eva-md.invalid>|eva.md on laptop <"+writerA+"@machines.eva-md.invalid>" {
		t.Errorf("author and committer = %s", got)
	}
	if got := gitT(t, dir, "rev-parse", "refs/eva-md/before-schema-2"); got != before {
		t.Errorf("the before ref = %s, want %s", got, before)
	}
	if got := gitT(t, dir, "status", "--porcelain", "--untracked-files=all"); got != "" {
		t.Errorf("the tree is not clean:\n%s", got)
	}
	if _, err := os.Stat(filepath.Join(o.StateDir, journalFile)); !errors.Is(err, os.ErrNotExist) {
		t.Errorf("the journal is still there: %v", err)
	}

	// The result is a store the profile passes, and a v1 binary's writers
	// refuse it: schema 2 and no instance.
	rep, err := profile.Check(dir)
	if err != nil {
		t.Fatal(err)
	}
	for _, f := range rep.Faults {
		t.Errorf("profile: %s", f)
	}
	s, err := config.LoadSchema(filepath.Join(dir, ".eva-md", "schema.toml"))
	if err != nil || s.SchemaVersion != 2 || s.Instance != "" {
		t.Errorf("v1 reads the schema as %+v, %v; want version 2 with no instance", s, err)
	}
	if a, err := Check(dir, Known); err != nil || !a.Read || !a.Write || a.StoreID != storeID {
		t.Errorf("Check = %+v, %v", a, err)
	}

	// A second run is at the step's To: no commit.
	r2, err := Run(bg(), dir, synthetic, machine(t, writerB, "desk", passB))
	if err != nil || r2.Committed || r2.Generation != 2 {
		t.Fatalf("second run = %+v, %v; want no commit at generation 2", r2, err)
	}
	if got := gitT(t, dir, "rev-parse", "HEAD"); got != head {
		t.Fatalf("the second run moved HEAD")
	}
}

func TestTwoMachinesGiveOneTree(t *testing.T) {
	isolate(t)
	one, two := v1Wiki(t, v1Files), v1Wiki(t, v1Files)
	if _, err := Run(bg(), one, synthetic, machine(t, writerA, "laptop", passA)); err != nil {
		t.Fatal(err)
	}
	if _, err := Run(bg(), two, synthetic, machine(t, writerB, "desk", passB)); err != nil {
		t.Fatal(err)
	}
	if a, b := treeID(t, one), treeID(t, two); a != b {
		t.Fatalf("tree ids differ: %s and %s", a, b)
	}
	if a, b := gitT(t, one, "rev-parse", "HEAD"), gitT(t, two, "rev-parse", "HEAD"); a == b {
		t.Fatalf("the commits are the same, so the test does not tell machines apart")
	}
}

// TestStoreIDAndEpochKept runs a step 2 to 3 on a store: the store id and
// epoch are kept, and the minimum reader is the higher of the old and the
// step's.
func TestStoreIDAndEpochKept(t *testing.T) {
	isolate(t)
	for _, tc := range []struct {
		old       string
		minReader int
		want      string
	}{
		{"schema = 2\nmin_reader = 2\nstore_id = \"s01mj780q8r2t7\"\nepoch = 4\n", 3, "schema = 3\nmin_reader = 3\nstore_id = \"s01mj780q8r2t7\"\nepoch = 4\n"},
		{"schema = 2\nmin_reader = 2\nstore_id = \"s01mj780q8r2t7\"\nepoch = 4\n", 1, "schema = 3\nmin_reader = 2\nstore_id = \"s01mj780q8r2t7\"\nepoch = 4\n"},
		{"schema = 2\nmin_reader = 2\nstore_id = \"s01mj780q8r2t7\"\n", 2, "schema = 3\nmin_reader = 2\nstore_id = \"s01mj780q8r2t7\"\nepoch = 1\n"},
	} {
		dir := v1Wiki(t, map[string]string{".eva-md/store.toml": tc.old, ".eva-md/schema.toml": "schema_version = 2\n", "index.md": "# Memory\n"})
		step := Step{From: 2, To: 3, MinReader: tc.minReader, Plan: func(Tree) (Change, error) { return Change{}, nil }}
		if _, err := Run(bg(), dir, step, machine(t, writerA, "laptop", passA)); err != nil {
			t.Fatal(err)
		}
		if got := gitT(t, dir, "show", "HEAD:.eva-md/store.toml") + "\n"; got != tc.want {
			t.Errorf("store.toml =\n%s\nwant\n%s", got, tc.want)
		}
		if got := gitT(t, dir, "show", "HEAD:.eva-md/schema.toml"); got != "schema_version = 3" {
			t.Errorf("schema.toml = %q", got)
		}
	}
}

// refused runs step on a wiki and checks that it fails with want and that
// the store, its .git and the state folder are as they were.
func refused(t *testing.T, dir string, step Step, o Options, want error) Result {
	t.Helper()
	store, state := hashDir(t, dir), hashDir(t, o.StateDir)
	r, err := Run(bg(), dir, step, o)
	if !errors.Is(err, want) {
		t.Fatalf("Run = %+v, %v; want %v", r, err, want)
	}
	if r.Committed {
		t.Fatalf("Run committed: %+v", r)
	}
	if hashDir(t, dir) != store || hashDir(t, o.StateDir) != state {
		t.Fatalf("a refused run changed the store or its state")
	}
	return r
}

func TestRefusalsChangeNothing(t *testing.T) {
	isolate(t)
	t.Run("unaccounted", func(t *testing.T) {
		step := synthetic
		step.Plan = func(tr Tree) (Change, error) {
			c, err := syntheticPlan(tr)
			c.Counts.Unaccounted = 1
			return c, err
		}
		r := refused(t, v1Wiki(t, v1Files), step, machine(t, writerA, "laptop", passA), ErrUnaccounted)
		if r.Counts.Unaccounted != 1 {
			t.Errorf("counts = %v", r.Counts)
		}
	})
	t.Run("flagged line", func(t *testing.T) {
		line := "password = hunter2hunter2"
		if len(wikigit.SecretLines(line)) == 0 {
			t.Fatal("the scan does not flag the made-up line")
		}
		files := maps.Clone(v1Files)
		files["general/now.md"] = "# Now\n\n" + line + "\n"
		r := refused(t, v1Wiki(t, files), synthetic, machine(t, writerA, "laptop", passA), ErrHeldBack)
		if r.Counts.HeldBack != 1 {
			t.Errorf("held back = %d, want 1", r.Counts.HeldBack)
		}
	})
	t.Run("busy lock", func(t *testing.T) {
		o := machine(t, writerA, "laptop", passA)
		unlock, busy, err := paths.TryLock(o.LocksDir, "store.lock")
		if err != nil || busy {
			t.Fatal(err, busy)
		}
		defer unlock()
		refused(t, v1Wiki(t, v1Files), synthetic, o, ErrBusy)
	})
	t.Run("changed file", func(t *testing.T) {
		dir := v1Wiki(t, v1Files)
		if err := os.WriteFile(filepath.Join(dir, "log.md"), []byte("edited\n"), 0o600); err != nil {
			t.Fatal(err)
		}
		refused(t, dir, synthetic, machine(t, writerA, "laptop", passA), ErrDirty)
	})
	t.Run("new file", func(t *testing.T) {
		dir := v1Wiki(t, v1Files)
		if err := os.WriteFile(filepath.Join(dir, "general", "draft.md"), []byte("draft\n"), 0o600); err != nil {
			t.Fatal(err)
		}
		refused(t, dir, synthetic, machine(t, writerA, "laptop", passA), ErrDirty)
	})
	t.Run("other generation", func(t *testing.T) {
		files := maps.Clone(v1Files)
		files[".eva-md/schema.toml"] = "schema_version = 3\n"
		refused(t, v1Wiki(t, files), synthetic, machine(t, writerA, "laptop", passA), ErrGeneration)
	})
	t.Run("not main", func(t *testing.T) {
		dir := v1Wiki(t, v1Files)
		gitT(t, dir, "switch", "-q", "-c", "work")
		refused(t, dir, synthetic, machine(t, writerA, "laptop", passA), ErrNotMain)
	})
	t.Run("journal of another step", func(t *testing.T) {
		dir := v1Wiki(t, v1Files)
		o := machine(t, writerA, "laptop", passA)
		if err := writeJournal(o.StateDir, journal{Pass: passB, From: 2, To: 3, Before: gitT(t, dir, "rev-parse", "HEAD")}); err != nil {
			t.Fatal(err)
		}
		refused(t, dir, synthetic, o, ErrJournalStep)
	})
	t.Run("main moved under a journal", func(t *testing.T) {
		dir := v1Wiki(t, v1Files)
		o := machine(t, writerA, "laptop", passA)
		if err := writeJournal(o.StateDir, journal{Pass: passB, From: 1, To: 2, Before: gitT(t, dir, "rev-parse", "HEAD")}); err != nil {
			t.Fatal(err)
		}
		gitT(t, dir, "commit", "-q", "--allow-empty", "-m", "another")
		refused(t, dir, synthetic, o, ErrMoved)
	})
}

// TestStepContract refuses a change that breaks the step's contract, and
// changes nothing.
func TestStepContract(t *testing.T) {
	isolate(t)
	alter := func(f func(*Change)) Step {
		s := synthetic
		s.Plan = func(tr Tree) (Change, error) {
			c, err := syntheticPlan(tr)
			f(&c)
			return c, err
		}
		return s
	}
	write := func(p string) func(*Change) {
		return func(c *Change) { c.Write[p] = []byte("x\n"); c.Counts.Written["legacy"]++ }
	}
	for name, step := range map[string]Step{
		"writes .gitattributes":   alter(write(".gitattributes")),
		"writes the manifest":     alter(write(".eva-md/store.toml")),
		"writes the marker":       alter(write(".eva-md/Schema.toml")),
		"writes into .git":        alter(write(".git/config")),
		"writes into .GIT":        alter(write("x/.GIT/config")),
		"writes outside":          alter(write("../outside.md")),
		"writes an absolute path": alter(write("/tmp/x.md")),
		"writes a dot segment":    alter(write("a/./b.md")),
		"writes a control byte":   alter(write("a\nb.md")),
		"writes in case only":     alter(write("INDEX.md")),
		"writes into a file":      alter(write("index.md/x.md")),
		"removes an absent path":  alter(func(c *Change) { c.Remove = append(c.Remove, "nowhere.md"); c.Counts.Removed++ }),
		"writes and removes":      alter(func(c *Change) { c.Remove = append(c.Remove, "index.md"); c.Counts.Removed++ }),
		"removes .gitignore":      alter(func(c *Change) { c.Remove = append(c.Remove, ".gitignore"); c.Counts.Removed++ }),
		"counts another class":    alter(func(c *Change) { c.Counts.Read["pages"] = 1 }),
		"miscounts writes":        alter(func(c *Change) { c.Counts.Written["legacy"]-- }),
		"miscounts removals":      alter(func(c *Change) { c.Counts.Removed++ }),
		"counts held back":        alter(func(c *Change) { c.Counts.HeldBack = 1 }),
		"counts below zero":       alter(func(c *Change) { c.Counts.Protected = -1 }),
		"plan fails":              alter(func(*Change) {}),
	} {
		t.Run(name, func(t *testing.T) {
			if name == "plan fails" {
				step.Plan = func(Tree) (Change, error) { return Change{}, errors.New("made-up failure") }
			}
			dir := v1Wiki(t, v1Files)
			o := machine(t, writerA, "laptop", passA)
			store := hashDir(t, dir)
			if r, err := Run(bg(), dir, step, o); err == nil || r.Committed {
				t.Fatalf("Run = %+v, %v; want a refusal", r, err)
			}
			if _, err := DryRun(bg(), dir, step); err == nil {
				t.Fatal("DryRun took the change")
			}
			if hashDir(t, dir) != store || hashDir(t, o.StateDir) != "absent" {
				t.Fatal("a refused change changed the store or its state")
			}
		})
	}
}

func TestOptionsAndStepChecked(t *testing.T) {
	isolate(t)
	dir := v1Wiki(t, v1Files)
	good := machine(t, writerA, "laptop", passA)
	for name, o := range map[string]Options{
		"relative state": {StateDir: "state", LocksDir: good.LocksDir, StoreID: storeID, Writer: writerA, Label: "laptop", Pass: passA},
		"bad store id":   {StateDir: good.StateDir, LocksDir: good.LocksDir, StoreID: "s-1", Writer: writerA, Label: "laptop", Pass: passA},
		"bad writer":     {StateDir: good.StateDir, LocksDir: good.LocksDir, StoreID: storeID, Writer: "Person Name", Label: "laptop", Pass: passA},
		"bad label":      {StateDir: good.StateDir, LocksDir: good.LocksDir, StoreID: storeID, Writer: writerA, Label: "Jane's laptop", Pass: passA},
		"bad pass":       {StateDir: good.StateDir, LocksDir: good.LocksDir, StoreID: storeID, Writer: writerA, Label: "laptop", Pass: "pass"},
	} {
		if _, err := Run(bg(), dir, synthetic, o); err == nil {
			t.Errorf("%s: Run took it", name)
		}
	}
	for name, s := range map[string]Step{
		"backwards":       {From: 2, To: 1, MinReader: 1, Plan: syntheticPlan},
		"reader too high": {From: 1, To: 2, MinReader: 3, Plan: syntheticPlan},
		"no plan":         {From: 1, To: 2, MinReader: 2},
		"repeated class":  {From: 1, To: 2, MinReader: 2, Classes: []string{"a", "a"}, Plan: syntheticPlan},
		"class with path": {From: 1, To: 2, MinReader: 2, Classes: []string{"projects/acme"}, Plan: syntheticPlan},
	} {
		if _, err := Run(bg(), dir, s, good); err == nil {
			t.Errorf("%s: Run took it", name)
		}
	}
	if got := gitT(t, dir, "rev-list", "--count", "HEAD"); got != "1" {
		t.Fatalf("a refused run made a commit")
	}
}

// TestCommitIsEvasAndUnsigned sets a person's identity in the environment
// and a configuration that signs every commit through a script that
// leaves a marker: the step's commit is still eva.md's and unsigned.
func TestCommitIsEvasAndUnsigned(t *testing.T) {
	home := isolate(t)
	dir := v1Wiki(t, v1Files)
	marker := filepath.Join(home, "signed")
	script := filepath.Join(home, "sign.sh")
	if err := os.WriteFile(script, []byte("#!/bin/sh\ntouch '"+marker+"'\nexit 1\n"), 0o700); err != nil {
		t.Fatal(err)
	}
	conf := "[user]\n\tname = Some Person\n\temail = person@example.invalid\n\tsigningkey = " + filepath.Join(home, "key") +
		"\n[commit]\n\tgpgsign = true\n[gpg]\n\tprogram = " + script + "\n[gpg \"ssh\"]\n\tprogram = " + script + "\n"
	if err := os.WriteFile(filepath.Join(home, "gitconfig"), []byte(conf), 0o600); err != nil {
		t.Fatal(err)
	}
	// The configuration does sign: a plain commit runs the script.
	control := v1Wiki(t, map[string]string{"a.md": "a\n"})
	if err := os.WriteFile(filepath.Join(control, "b.md"), []byte("b\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	gitT(t, control, "add", "b.md")
	if err := runGit(control, "commit", "-q", "-m", "b"); err == nil {
		t.Fatal("the signing script did not fail a plain commit")
	}
	if _, err := os.Stat(marker); err != nil {
		t.Fatalf("a plain commit did not run the signing script: %v", err)
	}
	if err := os.Remove(marker); err != nil {
		t.Fatal(err)
	}

	for k, v := range map[string]string{
		"GIT_AUTHOR_NAME": "Some Person", "GIT_AUTHOR_EMAIL": "person@example.invalid", "GIT_AUTHOR_DATE": "2001-01-01T00:00:00Z",
		"GIT_COMMITTER_NAME": "Some Person", "GIT_COMMITTER_EMAIL": "person@example.invalid",
		"GIT_CONFIG_COUNT": "1", "GIT_CONFIG_KEY_0": "commit.gpgsign", "GIT_CONFIG_VALUE_0": "true",
		"GIT_CONFIG_PARAMETERS": "'commit.gpgsign'='true'",
	} {
		t.Setenv(k, v)
	}
	if _, err := Run(bg(), dir, synthetic, machine(t, writerA, "laptop", passA)); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(marker); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("the signing script ran: %v", err)
	}
	raw := gitT(t, dir, "cat-file", "commit", "HEAD")
	if strings.Contains(raw, "gpgsig") || strings.Contains(raw, "Some Person") || strings.Contains(raw, "person@example.invalid") || strings.Contains(raw, " 978307200 ") {
		t.Fatalf("the commit carries the person's identity, date or a signature:\n%s", raw)
	}
	if !strings.Contains(raw, "\nauthor eva.md on laptop <"+writerA+"@machines.eva-md.invalid> ") ||
		!strings.Contains(raw, "\ncommitter eva.md on laptop <"+writerA+"@machines.eva-md.invalid> ") {
		t.Fatalf("the commit is not eva.md's:\n%s", raw)
	}
}

// runGit runs git with no settings of the test's own, as a person would.
func runGit(dir string, args ...string) error {
	return execGit(dir, args...).Run()
}
