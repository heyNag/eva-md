package v1arm

import (
	"context"
	"errors"
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
// so the brief of a new chat in the workspace holds a word of it; and the
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
			// However the test ends, it waits for the jobs the hooks started.
			t.Cleanup(func() {
				ctx, cancel := context.WithTimeout(context.Background(), 10*time.Minute)
				defer cancel()
				if err := a.waitFree(ctx, tr.Trial, updateLock, maintainLock); err != nil {
					t.Errorf("a job of eva-md's still holds a lock: %v", err)
				}
			})
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
			if err != nil || !strings.Contains(string(out), "layout of the banner") {
				t.Fatalf("the brief: %v\n%s", err, out)
			}
		})
	}
}

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
