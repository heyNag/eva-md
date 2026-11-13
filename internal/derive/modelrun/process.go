package modelrun

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"syscall"
	"time"
)

// The sealed run is a port of v1's internal/update/process.go: a CLI started
// in a process group of its own, in its run folder, with a cleared
// environment, its input on stdin, and capped writers so it never blocks on a
// pipe.

// Size caps. A child that writes more is not stopped for it; what it writes
// past a cap is dropped.
const (
	maxStderr = 64 << 10 // kept only for a backend to name a failure
	maxList   = 64 << 10 // what a probe step prints
)

// waitDelay bounds the wait for output pipes after the CLI exits, in case a
// process that left the group still holds them. Tests raise it before any
// test runs; a test that needs a short wait gives it to one runner.
var waitDelay = 2 * time.Second

// runTimer starts the time limit of one attempt. Tests replace it before any
// test runs, so a test can end an attempt's time at an exact point.
var runTimer = context.WithTimeout

// keepEnv lists the variables a child inherits. Every LC_* is kept too. The
// proxy variables are kept in both letter cases, as tools read either. The
// hosts' config-folder variables are kept so a moved config folder still
// finds the login. The CA variables keep a custom CA, such as a proxy's,
// trusted: one host, on Node, reads NODE_EXTRA_CA_CERTS and not
// SSL_CERT_FILE, and the other reads CODEX_CA_CERTIFICATE before
// SSL_CERT_FILE.
var keepEnv = map[string]bool{
	"HOME": true, "PATH": true, "USER": true, "LOGNAME": true, "SHELL": true,
	"LANG": true, "TMPDIR": true, "CODEX_HOME": true, "CLAUDE_CONFIG_DIR": true,
	"SSL_CERT_FILE": true, "SSL_CERT_DIR": true, "NODE_EXTRA_CA_CERTS": true, "CODEX_CA_CERTIFICATE": true,
	"HTTP_PROXY": true, "HTTPS_PROXY": true, "ALL_PROXY": true, "NO_PROXY": true,
	"http_proxy": true, "https_proxy": true, "all_proxy": true, "no_proxy": true,
}

// childEnv clears the environment down to the allowlist and marks the run as
// eva-md's own: EVA_MD_INNER=1 makes eva-md's hooks exit at once, even
// managed-policy ones. API keys are dropped so the run uses the subscription
// login, and CLAUDECODE and every CLAUDE_CODE_* variable are dropped so the
// child doesn't take itself for part of the chat that started the job; the
// one added back turns the host's own memory off.
func childEnv(environ []string) []string {
	var env []string
	for _, kv := range environ {
		key, _, ok := strings.Cut(kv, "=")
		if ok && (keepEnv[key] || strings.HasPrefix(key, "LC_")) {
			env = append(env, kv)
		}
	}
	return append(env, "TERM=dumb", "EVA_MD_INNER=1", "CLAUDE_CODE_DISABLE_AUTO_MEMORY=1")
}

// recordedCLI is the CLI install recorded while it is still the absolute path
// of an executable file named name, or "" (v1's cliFor), so a job started
// with a PATH that lacks the CLI, as from an app, still finds it.
func recordedCLI(name, recorded string) string {
	if !filepath.IsAbs(recorded) || filepath.Base(recorded) != name {
		return ""
	}
	if info, err := os.Stat(recorded); err != nil || !info.Mode().IsRegular() || info.Mode()&0o111 == 0 {
		return ""
	}
	return recorded
}

// withDir adds dir to the end of the environment's PATH when it isn't on it,
// so a CLI started by its recorded path finds what its #! line names beside
// it, such as the node an npm install put there.
func withDir(env []string, dir string) []string {
	for i, kv := range env {
		if value, ok := strings.CutPrefix(kv, "PATH="); ok {
			if !slices.Contains(filepath.SplitList(value), dir) {
				env[i] = kv + string(filepath.ListSeparator) + dir
			}
			return env
		}
	}
	return append(env, "PATH="+dir)
}

// proc is one CLI process to start.
type proc struct {
	cli       string    // the CLI's absolute path
	args      []string  // never the input
	dir       string    // the working folder
	stdin     []byte    // the input
	stdout    io.Writer // where stdout goes; it must not block
	timeout   time.Duration
	waitDelay time.Duration
	pathDir   string // added to the end of the child's PATH when set
}

// exit is how one CLI process ended. stderr holds its first maxStderr bytes,
// for a backend to name a failure with; it never reaches an Outcome.
type exit struct {
	started  bool // the process started
	missing  bool // the CLI is not there or can't be executed
	failed   bool // it exited non-zero or was killed
	timedOut bool // its time limit killed it, or ran out before it started
	stderr   []byte
}

// spawn runs p in a process group of its own. When ctx is done or the
// timeout passes, the whole group is killed; whatever is left of the group is
// killed once the CLI exits. A zero timeout leaves the limit to ctx. The
// error is only for a CLI that is there but could not be started; it names
// the CLI's base name and nothing else of p.
func spawn(ctx context.Context, p proc) (exit, error) {
	path, err := exec.LookPath(p.cli)
	if err != nil {
		return exit{missing: true}, nil
	}
	tctx, cancel := ctx, context.CancelFunc(func() {})
	if p.timeout > 0 {
		tctx, cancel = runTimer(ctx, p.timeout)
	}
	defer cancel()
	cmd := exec.CommandContext(tctx, path, p.args...)
	cmd.Dir = p.dir
	cmd.Env = childEnv(os.Environ())
	if p.pathDir != "" {
		cmd.Env = withDir(cmd.Env, p.pathDir)
	}
	cmd.Stdin = bytes.NewReader(p.stdin)
	cmd.Stdout = p.stdout
	stderr := &capped{limit: maxStderr}
	cmd.Stderr = stderr
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	cmd.Cancel = func() error { return killGroup(cmd.Process.Pid) }
	cmd.WaitDelay = p.waitDelay
	if err := cmd.Start(); err != nil {
		if errors.Is(err, fs.ErrNotExist) || errors.Is(err, fs.ErrPermission) {
			return exit{missing: true}, nil
		}
		// A run whose time ran out before the CLI started, as a retry left
		// with almost none of the call's one deadline, timed out as one that
		// ran out later does; Run reports a cancelled call as before.
		if tctx.Err() != nil {
			return exit{failed: true, timedOut: errors.Is(tctx.Err(), context.DeadlineExceeded)}, nil
		}
		return exit{}, fmt.Errorf("modelrun: could not start %s", filepath.Base(p.cli))
	}
	// The exit status comes from ProcessState rather than Wait's error: a
	// process that escaped the group and holds a pipe ends Wait with
	// ErrWaitDelay even after the CLI exited cleanly.
	_ = cmd.Wait()
	_ = killGroup(cmd.Process.Pid)
	exited := cmd.ProcessState != nil && cmd.ProcessState.Success()
	return exit{
		started:  true,
		failed:   !exited,
		timedOut: !exited && errors.Is(tctx.Err(), context.DeadlineExceeded),
		stderr:   stderr.Bytes(),
	}, nil
}

// killGroup kills every process in the group the child leads.
func killGroup(pid int) error {
	err := syscall.Kill(-pid, syscall.SIGKILL)
	if errors.Is(err, syscall.ESRCH) {
		return os.ErrProcessDone
	}
	return err
}

// capped keeps the first limit bytes written and drops the rest without an
// error, so a child never blocks on a full pipe.
type capped struct {
	buf      bytes.Buffer
	limit    int
	overflow bool
}

func (c *capped) Write(p []byte) (int, error) {
	n := len(p)
	if room := c.limit - c.buf.Len(); len(p) > room {
		p = p[:max(room, 0)]
		c.overflow = true
	}
	c.buf.Write(p)
	return n, nil
}

func (c *capped) Bytes() []byte  { return c.buf.Bytes() }
func (c *capped) String() string { return c.buf.String() }
