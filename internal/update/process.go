package update

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

// Size caps. A child that writes more is not stopped for it; what it writes
// past a cap is dropped, and a capped answer counts as invalid.
const (
	maxStdout = 8 << 20  // Claude's envelope
	maxStderr = 64 << 10 // kept only to name a failure
	maxLine   = 4 << 20  // one Codex event
	maxOutput = 4 << 20  // the Codex -o file
	maxDiag   = 64 << 10 // error text kept to name a failure
)

// waitDelay bounds the wait for output pipes after the CLI exits, in case a
// process that left the group still holds them.
const waitDelay = 2 * time.Second

// keepEnv lists the variables a child inherits. Every LC_* is kept too. The
// proxy variables are kept in both letter cases, as tools read either.
// CODEX_HOME and CLAUDE_CONFIG_DIR are kept so a moved config folder still
// finds the login. The CA variables keep a work proxy's private CA trusted:
// claude, on Node, reads NODE_EXTRA_CA_CERTS and not SSL_CERT_FILE, and codex
// reads CODEX_CA_CERTIFICATE before SSL_CERT_FILE.
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
// child doesn't take itself for part of the chat that started the job.
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

// Environ is the environment the hooks start a detached update job with:
// the allowlist its CLI runs get, with EVA_MD_INNER=1, so no eva-md hook
// under it captures or spawns anything.
func Environ(environ []string) []string { return childEnv(environ) }

// child is how one CLI run ended.
type child struct {
	missing  bool   // the CLI is not found or can't be executed
	failed   bool   // it exited non-zero or was killed
	timedOut bool   // the timeout killed it
	stderr   []byte // the first maxStderr bytes
}

// cliFor is the CLI a run starts: the absolute path install recorded while it
// is still an executable file of that name, so a job started with a PATH that
// lacks the CLI, as from an app, still finds it; otherwise the name, looked
// up on PATH.
func cliFor(name, recorded string) string {
	if !filepath.IsAbs(recorded) || filepath.Base(recorded) != name {
		return name
	}
	if info, err := os.Stat(recorded); err != nil || !info.Mode().IsRegular() || info.Mode()&0o111 == 0 {
		return name
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

// spawn runs cli, a name looked up on PATH or an absolute path, with args in
// dir, with stdin as its input and stdout going to out, in a process group of
// its own. When ctx is done or the timeout passes, the whole group is killed;
// whatever is left of the group is killed once the CLI exits. The error is
// only for a CLI that is there but could not be started.
func spawn(ctx context.Context, cli string, args []string, dir string, stdin []byte, out io.Writer, timeout time.Duration) (child, error) {
	path, err := exec.LookPath(cli)
	if err != nil {
		return child{missing: true}, nil
	}
	tctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	cmd := exec.CommandContext(tctx, path, args...)
	cmd.Dir = dir
	cmd.Env = childEnv(os.Environ())
	if filepath.IsAbs(cli) {
		cmd.Env = withDir(cmd.Env, filepath.Dir(cli))
	}
	cmd.Stdin = bytes.NewReader(stdin)
	cmd.Stdout = out
	stderr := &capped{limit: maxStderr}
	cmd.Stderr = stderr
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	cmd.Cancel = func() error { return killGroup(cmd.Process.Pid) }
	cmd.WaitDelay = waitDelay
	if err := cmd.Start(); err != nil {
		if errors.Is(err, fs.ErrNotExist) || errors.Is(err, fs.ErrPermission) {
			return child{missing: true}, nil
		}
		return child{}, fmt.Errorf("update: could not start %s: %w", filepath.Base(cli), err)
	}
	// The exit status comes from ProcessState rather than Wait's error: a
	// process that escaped the group and holds a pipe ends Wait with
	// ErrWaitDelay even after the CLI exited cleanly.
	_ = cmd.Wait()
	_ = killGroup(cmd.Process.Pid)
	exited := cmd.ProcessState != nil && cmd.ProcessState.Success()
	return child{
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
