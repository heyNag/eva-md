package hostfake

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"os"
	"os/exec"
)

// What became of a hook Fire was asked to run.
const (
	Ran     = "ran"
	Skipped = "skipped"
	Failed  = "failed"
)

// Result is what became of one hook of the event Fire fired.
type Result struct {
	// Key is the hook's positional key; PluginID is "" for a managed hook.
	Key      string
	PluginID string
	Managed  bool
	// Status is Ran, Skipped or Failed, and Reason says why it was skipped
	// or failed.
	Status string
	Reason string
	// Stdout, Stderr and Exit are what the command printed and how it
	// ended, when it ran.
	Stdout, Stderr []byte
	Exit           int
	// Timeout is the timeout the hook declares. It is recorded, not
	// enforced.
	Timeout int
}

// Fire fires event on cli with payload on stdin, as the host would: each
// hook of the event that would run runs its declared command, Codex's from
// the plugin's copy and Claude Code's from the plugin's folder, read again
// now. A Codex hook that isn't trusted is skipped; one whose plugin's copy
// has changed since its approval, from 0.164.0, fails with "plugin contents
// changed; hook approval required". Policy skips what it blocks. Matchers
// are not applied: every handler of the event is fired. The command runs in
// the folder the payload's cwd names, or in /, with PLUGIN_ROOT,
// PLUGIN_DATA, CLAUDE_PLUGIN_ROOT and CLAUDE_PLUGIN_DATA set.
func (f *Fake) Fire(ctx context.Context, cli, event string, payload []byte) ([]Result, error) {
	if err := checkCLI(cli); err != nil {
		return nil, err
	}
	var results []Result
	var runs []loaded
	err := f.withState(false, func(st *state) error {
		hooks, errs := f.all(st, cli)
		if len(errs) > 0 {
			return errors.Join(errs...)
		}
		c := contents{}
		for _, h := range hooks {
			if h.Event != event {
				continue
			}
			r := Result{Key: h.Key, PluginID: h.Plugin.id(), Managed: h.Managed, Timeout: h.Timeout}
			if h.Managed {
				r.PluginID = ""
			}
			if why := blocked(st, cli, h); why != "" {
				r.Status, r.Reason = Skipped, why
			} else if cli == Codex {
				if _, skip, failure := trust(st, h, c); skip != "" {
					r.Status, r.Reason = Skipped, skip
				} else if failure != "" {
					r.Status, r.Reason = Failed, failure
				}
			}
			results = append(results, r)
			runs = append(runs, h)
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	dir := "/"
	var p struct {
		Cwd string `json:"cwd"`
	}
	if json.Unmarshal(payload, &p) == nil && p.Cwd != "" {
		dir = p.Cwd
	}
	for i := range results {
		if results[i].Status != "" {
			continue
		}
		run(ctx, runs[i], dir, payload, &results[i])
	}
	return results, nil
}

// run runs one hook's command: a shell command, or a program with its args.
func run(ctx context.Context, h loaded, dir string, payload []byte, r *Result) {
	var cmd *exec.Cmd
	if h.Args != nil {
		cmd = exec.CommandContext(ctx, h.Command, h.Args...)
	} else {
		cmd = exec.CommandContext(ctx, "/bin/sh", "-c", h.Command)
	}
	cmd.Dir = dir
	cmd.Stdin = bytes.NewReader(payload)
	cmd.Env = append(os.Environ(), "PLUGIN_ROOT="+h.Root, "CLAUDE_PLUGIN_ROOT="+h.Root, "PLUGIN_DATA="+h.Data, "CLAUDE_PLUGIN_DATA="+h.Data)
	var stdout, stderr bytes.Buffer
	cmd.Stdout, cmd.Stderr = &stdout, &stderr
	err := cmd.Run()
	r.Stdout, r.Stderr = stdout.Bytes(), stderr.Bytes()
	var exit *exec.ExitError
	switch {
	case err == nil:
		r.Status = Ran
	case errors.As(err, &exit) && exit.ExitCode() >= 0:
		r.Status, r.Exit = Ran, exit.ExitCode()
	default:
		r.Status, r.Reason = Failed, "the command could not run: "+err.Error()
	}
}
