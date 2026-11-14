package v1arm

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/heyNag/eva-md/internal/eval/continuation/drive"
	"github.com/heyNag/eva-md/internal/eval/continuation/drive/fakehost"
	"github.com/heyNag/eva-md/internal/eval/continuation/task"
	"github.com/heyNag/eva-md/internal/host/adapter"
	"github.com/heyNag/eva-md/internal/host/hostfake"
)

// The evaluation build is built from this checkout with the go command in
// a home of its own, as cmd/eva-md's release stand-in is: GOENV=off, Go's
// telemetry off in that home, Go's caches as go env finds them in the
// environment the tests started in, captured before any test moves HOME,
// no network and no VCS stamp.
var (
	startEnv   []string
	buildHang  = 5 * time.Minute
	evalBinary = sync.OnceValues(buildEvalBinary)
	buildRoot  string
)

// goEnv is the environment the build runs in, with home as its HOME.
func goEnv(home string) ([]string, error) {
	config := filepath.Join(home, ".config")
	if runtime.GOOS == "darwin" {
		config = filepath.Join(home, "Library", "Application Support")
	}
	if err := os.MkdirAll(filepath.Join(config, "go", "telemetry"), 0o700); err != nil {
		return nil, err
	}
	if err := os.WriteFile(filepath.Join(config, "go", "telemetry", "mode"), []byte("off\n"), 0o600); err != nil {
		return nil, err
	}
	ctx, cancel := context.WithTimeout(context.Background(), buildHang)
	defer cancel()
	cmd := exec.CommandContext(ctx, goTool(), "env", "GOCACHE", "GOMODCACHE")
	cmd.Env = startEnv
	out, err := cmd.Output()
	if err != nil {
		return nil, errors.New("go env failed")
	}
	caches := strings.Split(strings.TrimSpace(string(out)), "\n")
	if len(caches) != 2 || !filepath.IsAbs(caches[0]) || !filepath.IsAbs(caches[1]) {
		return nil, errors.New("go env gave no caches")
	}
	return append(append([]string{}, startEnv...), "HOME="+home, "XDG_CONFIG_HOME="+filepath.Join(home, ".config"), "GOENV=off",
		"GOCACHE="+caches[0], "GOMODCACHE="+caches[1], "GOPROXY=off", "GOTOOLCHAIN=local", "GOFLAGS=-mod=readonly"), nil
}

func goTool() string {
	if path, err := exec.LookPath("go"); err == nil {
		return path
	}
	return filepath.Join(runtime.GOROOT(), "bin", "go")
}

// buildEvalBinary builds eva-md with the evaluation build's tag and the
// tests' raised limits, once for the package's tests, into a folder that
// TestMain removes.
func buildEvalBinary() (string, error) {
	dir, err := os.MkdirTemp("", "v1arm-eva-md-")
	if err != nil {
		return "", err
	}
	buildRoot = dir
	env, err := goEnv(filepath.Join(dir, "home"))
	if err != nil {
		return "", err
	}
	binary := filepath.Join(dir, "eva-md")
	ctx, cancel := context.WithTimeout(context.Background(), buildHang)
	defer cancel()
	cmd := exec.CommandContext(ctx, goTool(), "build", "-mod=readonly", "-buildvcs=false", "-tags", "evamd_eval,evamd_testlimits", "-o", binary, ".")
	cmd.Dir = filepath.Join("..", "..", "..", "..", "cmd", "eva-md")
	cmd.Env = env
	if out, err := cmd.CombinedOutput(); err != nil {
		return "", errors.New("building eva-md: " + err.Error() + "\n" + string(out))
	}
	return binary, nil
}

// End to end, on each host: the evaluation build passes the arm's check;
// setup runs the host's plugin commands on HO-14's fake CLIs, and the
// plugin it installs is one a chat loads; the replayed chat is captured,
// so the brief of a new chat in the workspace holds a word of it, as
// v1 gives a chat of three exchanges; and the
// update, which no model answers, is update_failed. The test ends once no
// job eva-md's hooks started holds a lock.
func TestEndToEnd(t *testing.T) {
	base := tempDir(t)
	for _, tmp := range []string{"/tmp", "/private/tmp", "/var/tmp", "/private/var/tmp"} {
		if base == tmp || strings.HasPrefix(base, tmp+"/") {
			t.Fatalf("this test needs a TMPDIR outside %s, whose chats eva-md's hooks skip: scripts/linux-check.sh gives one", tmp)
		}
	}
	binary, err := evalBinary()
	if err != nil {
		t.Fatal(err)
	}
	skipTagCheck = false
	err = eva.check(drive.Options{EvaMD: binary})
	skipTagCheck = true
	if err != nil {
		t.Fatalf("the evaluation build: %v", err)
	}
	for _, h := range adapter.All() {
		t.Run(string(h.ID), func(t *testing.T) {
			tr := endToEndTrial(t, h, binary)
			fake := hostfake.New(filepath.Join(tr.base, "hosts"))
			if err := fake.WriteCLIs(tr.Dirs.Bin); err != nil {
				t.Fatal(err)
			}
			a := testArm()
			// However the test ends, it waits for the jobs the hooks started
			// to end, and then removes the trial's folder.
			t.Cleanup(func() { settle(t, a, tr) })
			_, err := a.prepare(t.Context(), tr.Trial)
			var u *drive.Unjudged
			if !errors.As(err, &u) || u.Code != drive.CodeUpdateFailed {
				t.Fatalf("prepare: %v", err)
			}
			calls, err := fake.Calls()
			if err != nil {
				t.Fatal(err)
			}
			plugin := false
			for _, c := range calls {
				plugin = plugin || c.CLI == h.Names.CLI && strings.Contains(c.String(), "plugin ")
			}
			if !plugin {
				t.Fatalf("setup ran no plugin command of %s: %v", h.Names.CLI, calls)
			}
			if err := drive.PluginReady(tr.Dirs.Home, h); err != nil {
				t.Fatalf("the plugin setup installed: %v", err)
			}
			ctx, cancel := context.WithTimeout(t.Context(), HookTimeout)
			defer cancel()
			cmd := exec.CommandContext(ctx, filepath.Join(tr.Dirs.Bin, "eva-md"), "brief", "--cwd", tr.Dirs.Workspace, "--host", string(h.ID))
			cmd.Dir, cmd.Env = tr.Dirs.Home, tr.Env
			out, err := cmd.Output()
			if err != nil || !strings.Contains(string(out), briefWord) {
				t.Fatalf("the brief: %v\n%s", err, out)
			}
		})
	}
}

// settleHang bounds settle's waits: a hang guard, which no job of a working
// eva-md comes near.
const settleHang = 10 * time.Minute

// settle waits for the jobs eva-md's hooks started in tr to end, then
// removes the trial's test folder, so that no job writes into it while the
// testing package removes it. A job holds its lock while it works, but one
// that has not yet taken it, or found it held and is writing that to the
// run log, holds none: so settle waits for both locks to be free and then
// for no process to run a program under the test's folder, which is where
// the hooks' jobs run eva-md from (<home>/.local/share/eva-md/bin). The
// folder's removal is then tried again while it fails. Each wait is
// bounded by a hang guard alone.
func settle(t *testing.T, a *arm, tr aTrial) {
	ctx, cancel := context.WithTimeout(context.Background(), settleHang)
	defer cancel()
	if err := a.waitFree(ctx, tr.Trial, updateLock, maintainLock); err != nil {
		t.Errorf("a job of eva-md's still holds a lock: %v", err)
		return
	}
	pause := func() bool {
		select {
		case <-ctx.Done():
			return false
		case <-time.After(50 * time.Millisecond):
			return true
		}
	}
	for {
		jobs, err := processesUnder(tr.base)
		if err != nil {
			t.Errorf("the processes cannot be listed: %v", err)
			return
		}
		if jobs == 0 {
			break
		}
		if !pause() {
			t.Errorf("%d jobs of eva-md's still run", jobs)
			return
		}
	}
	for {
		err := os.RemoveAll(tr.base)
		if err == nil {
			return
		}
		if !pause() {
			t.Errorf("the trial's folder cannot be removed: %v", err)
			return
		}
	}
}

// processesUnder counts the processes that run a program under dir: those
// whose first argument, the path they were started by, is in it. On Linux
// it reads /proc, elsewhere what ps lists. A process that has ended and not
// yet been waited for runs nothing and is not counted.
func processesUnder(dir string) (int, error) {
	prefix := dir + string(filepath.Separator)
	n := 0
	if runtime.GOOS == "linux" {
		entries, err := os.ReadDir("/proc")
		if err != nil {
			return 0, err
		}
		for _, e := range entries {
			if strings.Trim(e.Name(), "0123456789") != "" {
				continue
			}
			data, err := os.ReadFile(filepath.Join("/proc", e.Name(), "cmdline"))
			if err != nil {
				continue // gone, or not ours to read
			}
			argv0, _, _ := strings.Cut(string(data), "\x00")
			if strings.HasPrefix(argv0, prefix) {
				n++
			}
		}
		return n, nil
	}
	out, err := exec.Command("/bin/ps", "-axww", "-o", "command=").Output()
	if err != nil {
		return 0, err
	}
	for _, line := range strings.Split(string(out), "\n") {
		if strings.HasPrefix(strings.TrimSpace(line), prefix) {
			n++
		}
	}
	return n, nil
}

// processesUnder counts a process started by a path under the folder while
// it runs, and no longer once it has ended: settle's wait sees a job.
func TestProcessesUnder(t *testing.T) {
	dir := tempDir(t)
	link := filepath.Join(dir, "bin", "made-up-job")
	mkdir(t, filepath.Dir(link))
	if err := os.Symlink(testBinary(t), link); err != nil {
		t.Fatal(err)
	}
	g := grandchild{Pipe: fifo(t, dir, "held"), PID: filepath.Join(dir, "job.pid")}
	go func() {
		if f, err := os.Open(g.Pipe); err == nil {
			_, _ = io.Copy(io.Discard, f)
			f.Close()
		}
	}()
	spec, err := json.Marshal(g)
	if err != nil {
		t.Fatal(err)
	}
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	defer r.Close()
	cmd := exec.Command(link)
	cmd.Env = []string{grandchildVar + "=" + string(spec)}
	cmd.ExtraFiles = []*os.File{w}
	err = cmd.Start()
	w.Close()
	if err != nil {
		t.Fatal(err)
	}
	var b [1]byte
	if n, _ := r.Read(b[:]); n != 1 {
		_ = cmd.Process.Kill()
		_ = cmd.Wait()
		t.Fatal("the job did not start")
	}
	running, err := processesUnder(dir)
	sibling, serr := processesUnder(filepath.Join(dir, "bi"))
	_ = cmd.Process.Kill()
	_ = cmd.Wait()
	if err != nil || running != 1 {
		t.Fatalf("while it runs: %d, %v", running, err)
	}
	if serr != nil || sibling != 0 {
		t.Fatalf("a folder whose name begins its folder's: %d, %v", sibling, serr)
	}
	if n, err := processesUnder(dir); err != nil || n != 0 {
		t.Fatalf("once it has ended: %d, %v", n, err)
	}
}

// briefWord is a made-up word every turn of the end-to-end trial's earlier
// chat holds.
const briefWord = "quillfeatherx"

// endToEndTrial is a trial built by hand for the evaluation build: the
// fixture task's workspace, prepared as the driver prepares it, and the
// build as eva-md in its bin/.
func endToEndTrial(t *testing.T, h adapter.Host, binary string) aTrial {
	t.Helper()
	base := tempDir(t)
	tasks, repos, err := fakehost.Fixture(filepath.Join(base, "data"), 1)
	if err != nil {
		t.Fatal(err)
	}
	x, err := task.Load(filepath.Join(tasks, fakehost.FixtureID))
	if err != nil {
		t.Fatal(err)
	}
	// v1's brief resumes a chat of three exchanges or more, so the earlier
	// chat is the fixture's, three times over, each turn with the word.
	var history []task.Turn
	for i := 0; i < 3; i++ {
		for _, turn := range x.History {
			turn.Text += " The made-up word is " + briefWord + "."
			history = append(history, turn)
		}
	}
	x.History = history
	if err := x.Validate(); err != nil {
		t.Fatal(err)
	}
	root := filepath.Join(base, "work", "run", x.ID, string(task.Eva), "t1")
	d := drive.Dirs{Root: root, Home: filepath.Join(root, "home"), Tmp: filepath.Join(root, "tmp"), Bin: filepath.Join(root, "bin"),
		Workspace: filepath.Join(root, "workspace"), Grade: filepath.Join(root, "grade")}
	for _, dir := range []string{d.Home, d.Tmp, d.Bin} {
		mkdir(t, dir)
	}
	write(t, filepath.Join(d.Home, ".gitconfig"), "[user]\n\tname = Continuation Trial\n\temail = trial@example.invalid\n[commit]\n\tgpgsign = false\n[maintenance]\n\tauto = false\n")
	if err := task.GoTelemetryOff(d.Home); err != nil {
		t.Fatal(err)
	}
	t.Setenv("HOME", d.Home)
	t.Setenv("GIT_CONFIG_GLOBAL", filepath.Join(d.Home, ".gitconfig"))
	t.Setenv("GIT_CONFIG_NOSYSTEM", "1")
	if err := task.Prepare(t.Context(), x, repos, d.Workspace); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(binary, filepath.Join(d.Bin, "eva-md")); err != nil {
		t.Fatal(err)
	}
	env := []string{"HOME=" + d.Home, "PATH=" + d.Bin + string(filepath.ListSeparator) + os.Getenv("PATH"), "TMPDIR=" + d.Tmp, "GIT_CONFIG_NOSYSTEM=1"}
	return aTrial{Trial: drive.Trial{Task: x, Condition: task.Eva, Number: 1, Dirs: d, Host: h, Env: env}, base: base}
}
