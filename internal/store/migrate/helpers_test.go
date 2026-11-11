package migrate

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/heyNag/eva-md/internal/testgitenv"
	"github.com/heyNag/eva-md/internal/testlimits"
)

func TestMain(m *testing.M) {
	testgitenv.Clear()
	testlimits.Raise()
	if os.Getenv(crashEnv) != "" {
		os.Exit(crashChild())
	}
	os.Exit(m.Run())
}

// Made-up ids for the runs.
const (
	storeID = "s01mj780q8r2t6"
	writerA = "w-k3f9a2bq"
	writerB = "w-7m2pq4zx"
	passA   = "01JD3K8Q2M7V5T9R4N6W0X1Y2Z"
	passB   = "01JD3K9A0B1C2D3E4F5G6H7J8K"
)

// isolate gives the test a home and a git configuration of its own.
func isolate(t *testing.T) string {
	t.Helper()
	home := tempDir(t)
	t.Setenv("HOME", home)
	t.Setenv("GIT_CONFIG_GLOBAL", filepath.Join(home, "gitconfig"))
	t.Setenv("GIT_CONFIG_NOSYSTEM", "1")
	if err := os.WriteFile(filepath.Join(home, "gitconfig"), nil, 0o600); err != nil {
		t.Fatal(err)
	}
	return home
}

// tempDir is t.TempDir with its symlinks resolved, as git reports paths.
func tempDir(t *testing.T) string {
	t.Helper()
	d, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	return d
}

// gitT runs a test's own git in dir with a fixture identity.
func gitT(t *testing.T, dir string, args ...string) string {
	t.Helper()
	cmd := exec.Command("git", append([]string{"-c", "user.name=Fixture", "-c", "user.email=fixture@example.invalid", "-c", "commit.gpgsign=false", "-c", "gc.auto=0", "-c", "maintenance.auto=false", "-C", dir}, args...)...)
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("git %v: %v\n%s", args, err, out)
	}
	return strings.TrimSpace(string(out))
}

// v1Files is a made-up wiki of v1's shape.
var v1Files = map[string]string{
	"README.md":                            "# eva.md wiki\n\nThis wiki is written by eva.md.\n",
	"index.md":                             "# Index\n\n- [acme-orders](projects/acme-orders/now.md)\n",
	"log.md":                               "## [2026-11-01] update | acme-orders\n",
	".gitattributes":                       "log.md merge=union\n*.md text eol=lf\n",
	".gitignore":                           "*.jsonl\n/inbox/\n.eva-md/tmp/\n.tmp-*\n.DS_Store\n",
	".eva-md/schema.toml":                  "schema_version = 1\ninstance = \"testbox-0011223344556677\"\ncreated = \"2026-11-01T10:00:00Z\"\n",
	"general/index.md":                     "# General\n",
	"general/now.md":                       "# Now\n\nNothing yet.\n",
	"projects/acme-orders/now.md":          "---\ntitle: acme-orders\nremote: git.example.invalid/acme/orders\n---\n# Now\n\n- Export moved to the nightly job.\n",
	"projects/acme-orders/threads/main.md": "---\ntitle: main\nlocked: true  # keep\n---\n## Decisions in force\n\n- Orders use port 8443.\n\n## Next steps\n\n- Add a retry to the export.\n",
	"knowledge/k1.md":                      "---\ntype: fact\nstatus: stable\n---\nThe orders service listens on 8443.\n",
	"systems/orders.md":                    "---\ntype: system\ntitle: Orders\n---\nThe orders service.\n",
}

// v1Wiki makes a git repository on main holding files, committed.
func v1Wiki(t *testing.T, files map[string]string) string {
	t.Helper()
	dir := filepath.Join(tempDir(t), "wiki")
	gitT(t, tempDir(t), "init", "-q", "-b", "main", dir)
	for p, s := range files {
		f := filepath.Join(dir, filepath.FromSlash(p))
		if err := os.MkdirAll(filepath.Dir(f), 0o700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(f, []byte(s), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	gitT(t, dir, "add", "-A")
	gitT(t, dir, "commit", "-q", "-m", "v1 final")
	return dir
}

// synthetic is the made-up step 1 to 2: each file but the four the runner
// owns goes to legacy/v1/ (in lower case, as the format names v1's files),
// and the step writes a new index.md and README.md.
var synthetic = Step{From: 1, To: 2, MinReader: 2, Classes: []string{"legacy", "index", "readme"}, Plan: syntheticPlan}

const (
	newIndex  = "# Memory\n\n* [Legacy](legacy/) - v1's files, kept as they were\n"
	newReadme = "---\ntype: readme\ntitle: \"eva.md memory\"\n---\nThis folder is eva.md's memory.\n"
)

func syntheticPlan(t Tree) (Change, error) {
	c := Change{Write: map[string][]byte{}, Counts: Counts{Read: map[string]int{}, Written: map[string]int{}}}
	for _, p := range t.Paths() {
		if slices.Contains(owned, p) {
			continue
		}
		b, err := t.Read(p)
		if err != nil {
			c.Counts.Unreadable++
			c.Counts.Unaccounted++
			continue
		}
		c.Counts.Read["legacy"]++
		c.Write["legacy/v1/"+strings.ToLower(p)] = b
		c.Counts.Written["legacy"]++
		if p != "index.md" && p != "README.md" {
			c.Remove = append(c.Remove, p)
			c.Counts.Removed++
		}
	}
	c.Write["index.md"] = []byte(newIndex)
	c.Counts.Written["index"]++
	c.Write["README.md"] = []byte(newReadme)
	c.Counts.Written["readme"]++
	return c, nil
}

// machine is one run's folders and identity.
func machine(t *testing.T, writer, label, pass string) Options {
	t.Helper()
	d := tempDir(t)
	return Options{StateDir: filepath.Join(d, "state"), LocksDir: filepath.Join(d, "locks"), StoreID: storeID, Writer: writer, Label: label, Pass: pass}
}

// hashDir is a digest of every entry under dir: path, mode, size, time and
// contents, so any write, even of the same bytes, changes it.
func hashDir(t *testing.T, dir string) string {
	t.Helper()
	h := sha256.New()
	err := filepath.WalkDir(dir, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		info, err := os.Lstat(p)
		if err != nil {
			return err
		}
		rel, _ := filepath.Rel(dir, p)
		fmt.Fprintf(h, "%s\x00%v\x00%d\x00%d\x00", rel, info.Mode(), info.Size(), info.ModTime().UnixNano())
		if info.Mode().IsRegular() {
			b, err := os.ReadFile(p)
			if err != nil {
				return err
			}
			h.Write(b)
		}
		return nil
	})
	if os.IsNotExist(err) {
		return "absent"
	}
	if err != nil {
		t.Fatal(err)
	}
	return hex.EncodeToString(h.Sum(nil))
}

func treeID(t *testing.T, dir string) string { return gitT(t, dir, "rev-parse", "HEAD^{tree}") }

func bg() context.Context { return context.Background() }

// execGit is git in dir with nothing of the test's own.
func execGit(dir string, args ...string) *exec.Cmd {
	return exec.Command("git", append([]string{"-C", dir}, args...)...)
}
