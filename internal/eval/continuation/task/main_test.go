package task

import (
	"crypto/sha256"
	"fmt"
	"io/fs"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"syscall"
	"testing"

	"github.com/heyNag/eva-md/internal/testgitenv"
)

// The stub runners a test can name as a hidden test's argv: the test binary
// with one of these as its first argument.
const (
	stubPass  = "stub-pass"
	stubFail  = "stub-fail"
	stubBlock = "stub-block"
	// stub-group <pid file> <exit|block> starts a stub-child, writes the
	// child's pid to the pid file once the child runs, then exits 0 or
	// blocks until it is killed.
	stubGroup = "stub-group"
	stubChild = "stub-child" // prints its pid, then blocks until it is killed
)

func TestMain(m *testing.M) {
	if len(os.Args) > 1 {
		switch os.Args[1] {
		case stubPass:
			os.Exit(0)
		case stubFail:
			os.Exit(1)
		case stubBlock:
			block()
		case stubGroup:
			if len(os.Args) != 4 {
				os.Exit(2)
			}
			os.Exit(groupStub(os.Args[2], os.Args[3]))
		case stubChild:
			fmt.Println(os.Getpid())
			block()
		}
	}
	testgitenv.Clear()
	os.Exit(run(m))
}

// block blocks until the process is killed: nothing ever sends SIGUSR1.
func block() {
	c := make(chan os.Signal, 1)
	signal.Notify(c, syscall.SIGUSR1)
	<-c
	os.Exit(0)
}

// groupStub is the stub-group runner: it starts a stub-child, which stays
// in the stub's process group, waits until the child has said its pid,
// writes it to
// pidFile whole (by a rename), and then exits 0 or, when then is "block",
// blocks until it is killed.
func groupStub(pidFile, then string) int {
	child := exec.Command(os.Args[0], stubChild)
	out, err := child.StdoutPipe()
	if err != nil {
		return 2
	}
	if err := child.Start(); err != nil {
		return 2
	}
	var pid int
	if _, err := fmt.Fscan(out, &pid); err != nil {
		return 2
	}
	tmp := pidFile + ".tmp"
	if err := os.WriteFile(tmp, []byte(strconv.Itoa(pid)), 0o644); err != nil {
		return 2
	}
	if err := os.Rename(tmp, pidFile); err != nil {
		return 2
	}
	if then == "block" {
		block()
	}
	return 0
}

// run gives the package's tests a made-up home and git configuration, and
// keeps Go's build cache where it was, so the hidden tests the fixtures run
// do not rebuild the standard library in each test's home.
func run(m *testing.M) int {
	if os.Getenv("GOCACHE") == "" {
		out, err := exec.Command("go", "env", "GOCACHE").Output()
		if err != nil {
			fmt.Fprintln(os.Stderr, "go env GOCACHE:", err)
			return 1
		}
		os.Setenv("GOCACHE", strings.TrimSpace(string(out)))
	}
	home, err := os.MkdirTemp("", "task-home-")
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 1
	}
	defer os.RemoveAll(home)
	// A made-up identity Prepare must not take.
	config := filepath.Join(home, "gitconfig")
	if err := os.WriteFile(config, []byte("[user]\nname = Leaky Person\nemail = leaky@example.invalid\n[init]\ndefaultBranch = leaky\n"), 0o600); err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 1
	}
	os.Setenv("HOME", home)
	os.Setenv("GIT_CONFIG_GLOBAL", config)
	os.Setenv("GIT_CONFIG_NOSYSTEM", "1")
	return m.Run()
}

// fixtures is the folder of the package's tasks, and repos its
// repositories.
var (
	fixtures = filepath.Join("testdata", "tasks")
	repos    = filepath.Join("testdata", "repos")
)

// load loads the fixture task in the folder name.
func load(t *testing.T, name string) Task {
	t.Helper()
	x, err := Load(filepath.Join(fixtures, name))
	if err != nil {
		t.Fatal(err)
	}
	return x
}

// tempDir is t.TempDir with its links resolved, as git and the system
// resolve a path on macOS.
func tempDir(t *testing.T) string {
	t.Helper()
	dir, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	return dir
}

// gitOut runs git in dir with the test's environment and returns its output
// without the final newline.
func gitOut(t *testing.T, dir string, args ...string) string {
	t.Helper()
	out, err := exec.Command("git", append([]string{"-C", dir}, args...)...).CombinedOutput()
	if err != nil {
		t.Fatalf("git %v: %v %s", args, err, out)
	}
	return strings.TrimSuffix(string(out), "\n")
}

// tree lists every path under dir but those in git's own folder, in slash
// form, sorted.
func tree(t *testing.T, dir string) []string {
	t.Helper()
	var paths []string
	err := filepath.WalkDir(dir, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.Name() == ".git" {
			return filepath.SkipDir
		}
		rel, err := filepath.Rel(dir, p)
		if err != nil || rel == "." {
			return err
		}
		if !d.IsDir() {
			paths = append(paths, filepath.ToSlash(rel))
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	slices.Sort(paths)
	return paths
}

// snapshot describes every entry under dir, git's own folder included: its
// path, type, whether it is executable, and its bytes or its link's target.
func snapshot(t *testing.T, dir string) []string {
	t.Helper()
	var out []string
	err := filepath.WalkDir(dir, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel, _ := filepath.Rel(dir, p)
		info, err := d.Info()
		if err != nil {
			return err
		}
		line := fmt.Sprintf("%s %v", filepath.ToSlash(rel), info.Mode())
		switch {
		case info.Mode()&fs.ModeSymlink != 0:
			target, err := os.Readlink(p)
			if err != nil {
				return err
			}
			line += " -> " + target
		case info.Mode().IsRegular():
			data, err := os.ReadFile(p)
			if err != nil {
				return err
			}
			line += fmt.Sprintf(" %x", sha256.Sum256(data))
		}
		out = append(out, line)
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	return out
}

// copyFixture copies the fixture task name into a temporary folder and
// returns that folder.
func copyFixture(t *testing.T, name string) string {
	t.Helper()
	dir := filepath.Join(tempDir(t), name)
	if err := os.Mkdir(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := copyTree(filepath.Join(fixtures, name), dir); err != nil {
		t.Fatal(err)
	}
	return dir
}

// write writes content to the file rel under dir, making its folders.
func write(t *testing.T, dir, rel, content string) {
	t.Helper()
	p := filepath.Join(dir, filepath.FromSlash(rel))
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(p, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}
