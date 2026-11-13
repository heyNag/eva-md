package modelrun

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"regexp"
	"slices"
	"sync"
	"time"

	"github.com/heyNag/eva-md/internal/host/adapter"
)

// Caps is what a probe learned of a host's CLI, with no model call.
type Caps struct {
	Host adapter.ID
	// CLI is the CLI's absolute path, its folder's symlinks resolved, or ""
	// when there is none.
	CLI string
	// Version is the CLI's N.N.N, or "" when it can't be read.
	Version string
	// Known says a row of the adapter's table holds the version; Untested
	// says it is above the newest row.
	Known, Untested bool
	// Efforts are the values the backend's effort flag takes, lowest first.
	Efforts []string
	// Features are what the backend's probe step found.
	Features []string
}

// probeStepTimeout bounds a backend's probe step, apart from any call's
// deadline (v1's limit on codex features list). Tests raise it before any
// test runs.
var probeStepTimeout = 15 * time.Second

// probeStepTimer starts the probe step's limit. Tests replace it before any
// test runs, so a test can end the step's time at an exact point.
var probeStepTimer = context.WithTimeout

// probeCLI is the adapter's probe. A test replaces it to see the Env a probe
// gives the adapter.
var probeCLI = adapter.Probe

// stepWaits is called when a probe starts to wait for a step another probe
// is running for the same entry. A test replaces it to know a probe waits.
var stepWaits = func() {}

// featureName is the shape a feature must have to reach Caps: the host's
// text of any other shape is dropped.
var featureName = regexp.MustCompile(`^[a-z][a-z0-9_.-]{0,63}$`)

// Probe finds the CLI, the one install recorded while it is an executable
// file of the CLI's name and PATH's otherwise, reads its version through the
// adapter, and runs the backend's probe step once per cache entry: the CLI's
// path, size, modification time and version. An error means ctx ended.
func (r *runner) Probe(ctx context.Context) (Caps, error) {
	if err := ctx.Err(); err != nil {
		return Caps{}, err
	}
	p := probeCLI(ctx, r.host.ID, r.env())
	if err := ctx.Err(); err != nil {
		return Caps{}, err
	}
	caps := Caps{
		Host: r.host.ID, CLI: p.CLI, Version: p.Version, Known: p.Known, Untested: p.Untested,
		Efforts: slices.Clone(r.b.efforts),
	}
	if caps.CLI == "" || r.b.probe == nil {
		return caps, nil
	}
	features, err := r.features(ctx, caps.CLI, caps.Version)
	if err != nil {
		return Caps{}, err
	}
	if err := ctx.Err(); err != nil {
		return Caps{}, err
	}
	caps.Features = features
	return caps, nil
}

// env is the options' Env with a LookPath that gives the CLI install
// recorded while it is still an executable file of the CLI's name, and,
// when it has no Run, the sealed run, so the CLI is never started with
// more of the environment than a call gets.
func (r *runner) env() adapter.Env {
	env := r.opts.Env
	if env.Run == nil {
		env.Run = r.sealedRun
	}
	look := env.LookPath
	if look == nil {
		look = exec.LookPath
	}
	name, recorded := r.host.Names.CLI, r.opts.CLI
	env.LookPath = func(file string) (string, error) {
		if file == name {
			if cli := recordedCLI(name, recorded); cli != "" {
				return cli, nil
			}
		}
		return look(file)
	}
	return env
}

// stepKey is one entry of the probe cache.
type stepKey struct {
	host    adapter.ID
	path    string
	size    int64
	mod     int64 // UnixNano
	version string
}

// stepEntry is a probe step's features, once done is closed; ok says they
// may be used.
type stepEntry struct {
	done     chan struct{}
	features []string
	ok       bool
}

var steps = struct {
	sync.Mutex
	byKey map[stepKey]*stepEntry
}{byKey: map[stepKey]*stepEntry{}}

// features gives the probe step's features for the CLI as it is now: from
// the cache, by waiting for a step another probe is running for the same
// entry, or by running the step. A step that gives no usable list is not
// kept, so the next probe runs it again.
func (r *runner) features(ctx context.Context, cli, version string) ([]string, error) {
	info, err := os.Stat(cli)
	if err != nil {
		features, _ := r.step(ctx, cli)
		return features, nil
	}
	key := stepKey{host: r.host.ID, path: cli, size: info.Size(), mod: info.ModTime().UnixNano(), version: version}
	for {
		steps.Lock()
		e, found := steps.byKey[key]
		if !found {
			e = &stepEntry{done: make(chan struct{})}
			steps.byKey[key] = e
			steps.Unlock()
			r.fill(ctx, cli, key, e)
			return slices.Clone(e.features), nil
		}
		steps.Unlock()
		stepWaits()
		select {
		case <-e.done:
		case <-ctx.Done():
			return nil, ctx.Err()
		}
		if e.ok {
			return slices.Clone(e.features), nil
		}
	}
}

// fill runs the step for the entry e it added under key, drops the entry
// unless the step's features may be kept, and closes done, whatever the
// step does.
func (r *runner) fill(ctx context.Context, cli string, key stepKey, e *stepEntry) {
	defer close(e.done)
	defer func() {
		if !e.ok {
			steps.Lock()
			if steps.byKey[key] == e {
				delete(steps.byKey, key)
			}
			steps.Unlock()
		}
	}()
	e.features, e.ok = r.step(ctx, cli)
}

// step runs the backend's probe step under its own limit, through the
// options' Env.Run when set, else as a sealed run, and keeps of what it
// found only names of a feature's shape.
func (r *runner) step(ctx context.Context, cli string) ([]string, bool) {
	features, ok := r.rawStep(ctx, cli)
	var kept []string
	for _, f := range features {
		if featureName.MatchString(f) {
			kept = append(kept, f)
		}
	}
	return kept, ok
}

func (r *runner) rawStep(ctx context.Context, cli string) ([]string, bool) {
	ctx, cancel := probeStepTimer(ctx, probeStepTimeout)
	defer cancel()
	envRun := r.env().Run
	run := func(ctx context.Context, args ...string) ([]byte, error) {
		out, err := envRun(ctx, cli, args...)
		if err != nil || len(out) > maxList {
			return nil, errStep
		}
		return out, nil
	}
	return r.b.probe(ctx, run)
}

var errStep = errors.New("modelrun: the probe command failed")

// sealedRun runs the CLI with args as a call's run is run, but in a private
// folder of its own, with nothing on stdin and under ctx's limit, and gives
// what it printed on stdout, at most maxList bytes. It is what probes run
// through when the options' Env has no Run: the adapter's --version and the
// backend's probe step.
func (r *runner) sealedRun(ctx context.Context, cli string, args ...string) ([]byte, error) {
	dir, err := os.MkdirTemp("", "eva-md-probe-")
	if err != nil {
		return nil, errStep
	}
	defer os.RemoveAll(dir)
	out := &capped{limit: maxList}
	e, err := spawn(ctx, proc{cli: cli, args: args, dir: dir, stdout: out, waitDelay: r.wait(), pathDir: r.pathDir(cli)})
	if err != nil || e.missing || e.failed || out.overflow {
		return nil, errStep
	}
	return out.Bytes(), nil
}
