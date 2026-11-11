package migrate

import (
	"bufio"
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"strconv"
	"strings"

	"github.com/heyNag/eva-md/internal/identity"
	"github.com/heyNag/eva-md/internal/wikigit"
)

// gitPins are set on every git the migrator runs: wikigit.Run's (no
// fsmonitor, no hooks, no optional locks), and, since wikigit.Run keeps a
// person's signing, none here: no signing, no symlinks in the work tree, no
// line-ending conversion, no garbage collection or maintenance, which git
// would start in the background after a commit, and objects and references
// on disk before a command returns.
var gitPins = []string{
	"-c", "core.fsmonitor=false", "-c", "core.hooksPath=/dev/null", "--no-optional-locks",
	"-c", "commit.gpgsign=false", "-c", "core.symlinks=false", "-c", "core.autocrlf=false",
	"-c", "gc.auto=0", "-c", "maintenance.auto=false", "-c", "core.fsync=committed", "-c", "core.fsyncMethod=fsync",
	"--literal-pathspecs",
}

// maxOutput is the most a git command's output may take, apart from the
// tree's contents, which are read as a stream.
const maxOutput = 16 << 20

// maxTree is the most the contents of a tree may take in memory.
const maxTree = 1 << 30

// git runs one git command in dir with the pins, on identity.GitEnviron's
// environment with any commit identity it carries replaced by eva.md's
// when author is set, and returns its output. git's diagnostics are never
// kept or shown.
func git(ctx context.Context, dir string, author *author, stdin []byte, args ...string) ([]byte, error) {
	ctx, cancel := context.WithTimeout(ctx, wikigit.GitTimeout)
	defer cancel()
	cmd := exec.CommandContext(ctx, "git", append(append(append([]string{}, gitPins...), "-C", dir), args...)...)
	cmd.Env = gitEnv(author)
	cmd.WaitDelay = wikigit.WaitDelay
	if stdin != nil {
		cmd.Stdin = bytes.NewReader(stdin)
	}
	out := &capped{limit: maxOutput}
	cmd.Stdout = out
	cmd.Stderr = io.Discard
	err := cmd.Run()
	hook("git " + args[0])
	if err != nil {
		if ctx.Err() != nil {
			return nil, fmt.Errorf("migrate: git %s timed out or was canceled", args[0])
		}
		return nil, &gitError{cmd: args[0], err: err}
	}
	return out.Bytes(), nil
}

type gitError struct {
	cmd string
	err error
}

func (e *gitError) Error() string { return "migrate: git " + e.cmd + " failed" }

// exitCode is a failed git's exit code, or -1.
func exitCode(err error) int {
	var g *gitError
	var x *exec.ExitError
	if errors.As(err, &g) && errors.As(g.err, &x) {
		return x.ExitCode()
	}
	return -1
}

// author is the identity a commit is made with.
type author struct{ name, email string }

func gitEnv(a *author) []string {
	var env []string
	for _, kv := range identity.GitEnviron(os.Environ()) {
		name, _, _ := strings.Cut(kv, "=")
		if strings.HasPrefix(name, "GIT_AUTHOR_") || strings.HasPrefix(name, "GIT_COMMITTER_") {
			continue
		}
		env = append(env, kv)
	}
	if a != nil {
		env = append(env, "GIT_AUTHOR_NAME="+a.name, "GIT_AUTHOR_EMAIL="+a.email,
			"GIT_COMMITTER_NAME="+a.name, "GIT_COMMITTER_EMAIL="+a.email)
	}
	return env
}

type capped struct {
	bytes.Buffer
	limit int
}

func (c *capped) Write(p []byte) (int, error) {
	if c.Len()+len(p) > c.limit {
		return 0, errors.New("git output exceeds limit")
	}
	return c.Buffer.Write(p)
}

// tree is the tree of one commit, read whole: a step sees nothing else.
type tree struct {
	paths []string
	data  map[string][]byte
	bad   map[string]bool // entries that are no file, such as a submodule
}

func (t *tree) Paths() []string { return append([]string(nil), t.paths...) }

func (t *tree) Read(path string) ([]byte, error) {
	if t.bad[path] {
		return nil, errors.New("migrate: not a file")
	}
	b, ok := t.data[path]
	if !ok {
		return nil, os.ErrNotExist
	}
	return append([]byte(nil), b...), nil
}

// readTree reads every entry of commit's tree and its contents.
func readTree(ctx context.Context, dir, commit string) (*tree, error) {
	list, err := git(ctx, dir, nil, nil, "ls-tree", "-r", "-z", "--full-tree", commit)
	if err != nil {
		return nil, err
	}
	t := &tree{data: map[string][]byte{}, bad: map[string]bool{}}
	var oids []string
	var blobs []string
	for _, entry := range strings.Split(strings.TrimSuffix(string(list), "\x00"), "\x00") {
		if entry == "" {
			continue
		}
		meta, path, ok := strings.Cut(entry, "\t")
		f := strings.Fields(meta)
		if !ok || len(f) != 3 {
			return nil, errors.New("migrate: unexpected tree listing")
		}
		t.paths = append(t.paths, path)
		if f[1] != "blob" {
			t.bad[path] = true
			continue
		}
		oids = append(oids, f[2])
		blobs = append(blobs, path)
	}
	if len(oids) == 0 {
		return t, nil
	}
	contents, err := catBlobs(ctx, dir, oids)
	if err != nil {
		return nil, err
	}
	for i, p := range blobs {
		t.data[p] = contents[i]
	}
	return t, nil
}

// catBlobs reads the blobs named by oids, in order, with one git.
func catBlobs(ctx context.Context, dir string, oids []string) ([][]byte, error) {
	ctx, cancel := context.WithTimeout(ctx, wikigit.GitTimeout)
	defer cancel()
	cmd := exec.CommandContext(ctx, "git", append(append(append([]string{}, gitPins...), "-C", dir), "cat-file", "--batch")...)
	cmd.Env = gitEnv(nil)
	cmd.WaitDelay = wikigit.WaitDelay
	cmd.Stdin = strings.NewReader(strings.Join(oids, "\n") + "\n")
	cmd.Stderr = io.Discard
	pipe, err := cmd.StdoutPipe()
	if err != nil {
		return nil, err
	}
	if err := cmd.Start(); err != nil {
		return nil, errors.New("migrate: git cat-file failed")
	}
	out, readErr := readBatch(bufio.NewReader(pipe), len(oids))
	if readErr != nil {
		_ = cmd.Process.Kill()
	}
	err = cmd.Wait()
	hook("git cat-file")
	if readErr != nil {
		return nil, readErr
	}
	if err != nil {
		return nil, errors.New("migrate: git cat-file failed")
	}
	return out, nil
}

func readBatch(r *bufio.Reader, n int) ([][]byte, error) {
	out := make([][]byte, 0, n)
	total := 0
	for range n {
		header, err := r.ReadString('\n')
		if err != nil {
			return nil, errors.New("migrate: short git cat-file output")
		}
		f := strings.Fields(header)
		if len(f) != 3 || f[1] != "blob" {
			return nil, errors.New("migrate: git cat-file found no blob")
		}
		size, err := strconv.Atoi(f[2])
		if err != nil || size < 0 {
			return nil, errors.New("migrate: unexpected git cat-file output")
		}
		if total += size; total > maxTree {
			return nil, errors.New("migrate: the tree is too large")
		}
		b := make([]byte, size+1)
		if _, err := io.ReadFull(r, b); err != nil || b[size] != '\n' {
			return nil, errors.New("migrate: short git cat-file output")
		}
		out = append(out, b[:size])
	}
	return out, nil
}
