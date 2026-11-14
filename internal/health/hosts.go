package health

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/heyNag/eva-md/internal/host/adapter"
	"github.com/heyNag/eva-md/internal/install"
	"github.com/heyNag/eva-md/internal/paths"
)

// Absent reports whether a host's command isn't on PATH, as on a Mac
// without that app, and names the app for a doctor row. host is a plugin
// host (claude, codex) or an update host (claude-code, codex); a host
// eva-md doesn't know is absent, with no app.
func Absent(host string) (app string, absent bool) {
	h, ok := hostOf(host)
	if !ok {
		return "", true
	}
	_, err := exec.LookPath(h.Names.CLI)
	return h.Names.App, err != nil
}

// hostOf finds a host by the name a row is given: its id, as an update host
// (claude-code, codex), or its install name, as a plugin host (claude,
// codex). The two spellings of a host are never another host's.
func hostOf(host string) (adapter.Host, bool) {
	if h, ok := adapter.ByID(host); ok {
		return h, true
	}
	return adapter.ByInstallName(host)
}

// cli is a host's command, by its id or its install name.
func cli(host string) string {
	h, _ := hostOf(host)
	return h.Names.CLI
}

// marketplace is the folder of a host's eva-md marketplace,
// <share>/marketplace/<install name>, by its id or its install name, or ""
// for a host eva-md doesn't know.
func marketplace(d paths.Dirs, host string) string {
	h, ok := hostOf(host)
	if !ok {
		return ""
	}
	return filepath.Join(d.Share, "marketplace", h.Names.InstallName)
}

// hostIDs are every host's id, in the adapter's order: Claude Code, then
// Codex.
func hostIDs() []string {
	var ids []string
	for _, h := range adapter.All() {
		ids = append(ids, string(h.ID))
	}
	return ids
}

// reviewHost is the host whose changed plugin needs a review in it before
// its hooks run, whose hooks' trust the doctor reads, or false when no host
// needs one.
func reviewHost() (adapter.Host, bool) {
	for _, h := range adapter.All() {
		if h.Plugin().NeedsReview {
			return h, true
		}
	}
	return adapter.Host{}, false
}

// trustPending finds the host the needs-trust flag names, by its install
// name, as install writes it: one whose plugin needs a review in it, or
// false.
func trustPending(name string) (adapter.Host, bool) {
	h, ok := adapter.ByInstallName(name)
	if !ok || !h.Plugin().NeedsReview {
		return adapter.Host{}, false
	}
	return h, true
}

// Time limits on the hosts' own commands: each host's native plugin
// inventory, a command such as codex features list, and the read-only hooks
// RPC. Tests raise them before any test runs, so a slow machine can't fail a
// test of what the doctor reports.
var (
	inventoryTimeout = 5 * time.Second
	commandTimeout   = 5 * time.Second
	trustTimeout     = 8 * time.Second
)

func init() {
	registerRows(800, "hosts", func(r *Report, in *inspection) {
		if in.hosts {
			inspectHosts(in.ctx, r, in.d, inventoryTimeout)
		}
	})
}

// inspectHosts adds the rows the hosts' own commands give. inventory is the
// budget of each host's plugin inventory: inventoryTimeout, or a short one in
// the test of a listing that stalls.
func inspectHosts(ctx context.Context, r *Report, d paths.Dirs, inventory time.Duration) {
	enabled := map[adapter.ID]bool{}
	for _, h := range adapter.All() {
		host := h.Names.InstallName
		// Inventory may contact an unrelated remote marketplace. Give each
		// host its own budget so a stalled listing cannot consume doctor's
		// whole deadline and conceal independent local hook-trust evidence.
		inventoryCtx, cancel := context.WithTimeout(ctx, inventory)
		status, err := install.Inspect(inventoryCtx, d, host)
		timedOut := errors.Is(inventoryCtx.Err(), context.DeadlineExceeded)
		cancel()
		if timedOut {
			r.add(host+" plugin", "UNKNOWN", "native plugin inventory timed out", "inspect the native host plugin inventory")
		} else if err != nil && status.Available && errors.Is(err, install.ErrConflict) {
			// Another eva-md registration, apart from a host command or its
			// output that failed, whose text can name eva-md too.
			r.add(host+" plugin", "FAIL", strings.TrimPrefix(err.Error(), "install: "), "remove the conflicting eva-md registration with the host's plugin commands, then run eva-md install --host "+host)
		} else if app, absent := Absent(host); absent {
			r.add(host+" plugin", "UNKNOWN", app+" isn't installed on this Mac", "")
		} else if err != nil || !status.Available {
			r.add(host+" plugin", "UNKNOWN", "native plugin inventory is unavailable", "inspect the native host plugin inventory")
		} else if status.FolderMissing {
			// Install stops there; uninstall clears the registration.
			r.add(host+" plugin", "FAIL", status.Reason, "run eva-md uninstall --apply, then install eva-md again")
		} else if !status.Installed {
			r.add(host+" plugin", "FAIL", "eva-md is not installed", "run eva-md install --host "+host)
		} else if !status.Valid {
			r.add(host+" plugin", "FAIL", status.Reason, "inspect native plugin list before reinstalling")
		} else {
			r.add(host+" plugin", "PASS", "one enabled plugin at the expected path", "")
		}
		if !timedOut && err == nil && status.Installed && status.Enabled {
			enabled[h.ID] = true
		}
	}
	for _, h := range adapter.All() {
		args := h.Plugin().FeaturesArgs
		if args == nil {
			continue
		}
		if data, e := hostCommand(ctx, h.Names.CLI, args...); e == nil {
			addFeatureRow(r, h, data)
		}
	}
	if h, ok := reviewHost(); ok {
		if _, absent := Absent(string(h.ID)); !absent {
			inspectTrust(ctx, r, enabled[h.ID])
		}
	}
	r.add("claude managed hook policy", "UNKNOWN", "policy is judged by hook heartbeats, not settings-file guesses", "confirm SessionStart fires in an enabled session")
}

// addFeatureRow adds the row of a host's hooks feature from what its
// features list printed, by the doctor's rule: on passes, any other state
// fails with the command that turns it on, and a list with no hooks row is
// unknown.
func addFeatureRow(r *Report, h adapter.Host, out []byte) {
	name := h.Names.CLI + " hooks feature"
	known, on := adapter.ReadHooksRows(out).On()
	if known && on {
		r.add(name, "PASS", "enabled", "")
	} else if known {
		r.add(name, "FAIL", "disabled", "run "+h.Names.CLI+" features enable hooks")
	} else {
		r.add(name, "UNKNOWN", "not reported by this version", "")
	}
}

// inspectTrust adds the hook trust row of the host whose plugin needs a
// review in it, Codex, on a Mac that has it. enabled says its plugin is
// installed and enabled.
func inspectTrust(ctx context.Context, r *Report, enabled bool) {
	h, _ := reviewHost()
	name, app := h.Names.InstallName+" hook trust", h.Names.App
	cwd, _ := os.Getwd()
	count, trusted, err := probeTrust(ctx, cwd)
	if err != nil {
		r.add(name, "UNKNOWN", "read-only hooks/list is unavailable", "review /hooks in "+app)
	} else if count == 0 && enabled {
		r.add(name, "FAIL", app+" loads no eva-md hooks for the enabled plugin", "check the "+app+" hooks feature, then run eva-md install --host "+h.Names.InstallName)
	} else if count == 0 {
		r.add(name, "UNKNOWN", "no eva-md hooks loaded", "install eva-md before reviewing /hooks")
	} else if count != 7 || trusted != count {
		r.add(name, "FAIL", fmt.Sprintf("%d of 7 required events trusted and enabled (%d loaded)", trusted, count), "inspect eva-md's seven hooks and approve them in "+app+" /hooks")
	} else {
		r.add(name, "PASS", fmt.Sprintf("%d hooks trusted and enabled", count), "")
	}
}

// cappedBuffer keeps a host command's output and fails the command past
// limit bytes. The buffer is a named field, not an embedded bytes.Buffer,
// whose ReadFrom os/exec would call in place of Write, so the limit would
// never hold.
type cappedBuffer struct {
	buffer bytes.Buffer
	limit  int
}

func (b *cappedBuffer) Write(p []byte) (int, error) {
	if b.buffer.Len()+len(p) > b.limit {
		return 0, errors.New("host output limit")
	}
	return b.buffer.Write(p)
}

// hostOutputLimit is the most a host command, such as codex features list,
// may print before the doctor takes it as unavailable.
const hostOutputLimit = 8 << 20

// commandWaitDelay bounds the wait for a host command's output once it has
// exited or run out of time. Tests raise it before any test runs.
var commandWaitDelay = time.Second

func hostCommand(ctx context.Context, name string, args ...string) ([]byte, error) {
	ctx, cancel := context.WithTimeout(ctx, commandTimeout)
	defer cancel()
	cmd := exec.CommandContext(ctx, name, args...)
	cmd.WaitDelay = commandWaitDelay
	b := &cappedBuffer{limit: hostOutputLimit}
	cmd.Stdout = b
	cmd.Stderr = io.Discard
	if err := cmd.Run(); err != nil {
		return nil, errors.New("host command unavailable")
	}
	return b.buffer.Bytes(), nil
}

// probeTrust asks the host whose plugin needs a review in it, Codex, which
// of eva-md's hooks it loads and trusts. It sends only read-only methods,
// never a thread/turn or approval.
func probeTrust(parent context.Context, cwd string) (count, trusted int, err error) {
	reviewer, ok := reviewHost()
	if !ok {
		return 0, 0, errors.New("no host reviews its hooks")
	}
	ctx, cancel := context.WithTimeout(parent, trustTimeout)
	defer cancel()
	cmd := exec.CommandContext(ctx, reviewer.Names.CLI, "--no-daemon", "app-server", "--stdio")
	cmd.Stderr = io.Discard
	cmd.WaitDelay = time.Second
	in, err := cmd.StdinPipe()
	if err != nil {
		return 0, 0, err
	}
	out, err := cmd.StdoutPipe()
	if err != nil {
		return 0, 0, err
	}
	if err = cmd.Start(); err != nil {
		return 0, 0, err
	}
	defer func() { in.Close(); cancel(); _ = cmd.Wait() }()
	type reply struct {
		ID     int             `json:"id"`
		Result json.RawMessage `json:"result"`
		Error  json.RawMessage `json:"error"`
	}
	lines := make(chan reply, 8)
	go func() {
		defer close(lines)
		s := bufio.NewScanner(io.LimitReader(out, 8<<20))
		s.Buffer(make([]byte, 4096), 4<<20)
		for s.Scan() {
			var v reply
			if json.Unmarshal(s.Bytes(), &v) == nil && v.ID > 0 {
				select {
				case lines <- v:
				case <-ctx.Done():
					return
				}
			}
		}
	}()
	send := func(id int, method string, params any) error {
		return json.NewEncoder(in).Encode(map[string]any{"id": id, "method": method, "params": params})
	}
	recv := func(id int) (json.RawMessage, error) {
		for {
			select {
			case v, ok := <-lines:
				if !ok {
					return nil, errors.New("host closed")
				}
				if v.ID == id {
					if len(v.Error) > 0 && string(v.Error) != "null" {
						return nil, errors.New("rpc failed")
					}
					return v.Result, nil
				}
			case <-ctx.Done():
				return nil, ctx.Err()
			}
		}
	}
	if err = send(1, "initialize", map[string]any{"clientInfo": map[string]string{"name": "eva_md_doctor", "version": "1"}, "capabilities": map[string]bool{"experimentalApi": true}}); err != nil {
		return
	}
	if _, err = recv(1); err != nil {
		return
	}
	if err = json.NewEncoder(in).Encode(map[string]any{"method": "initialized", "params": map[string]any{}}); err != nil {
		return
	}
	if err = send(2, "hooks/list", map[string]any{"cwds": []string{cwd}}); err != nil {
		return
	}
	var raw json.RawMessage
	raw, err = recv(2)
	if err != nil {
		return
	}
	var result struct {
		Errors   []json.RawMessage
		Warnings []json.RawMessage
		Data     []struct {
			Errors   []json.RawMessage
			Warnings []json.RawMessage
			Hooks    []struct {
				EventName string
				PluginID  string `json:"pluginId"`
				Enabled   bool   `json:"enabled"`
				Trust     string `json:"trustStatus"`
			} `json:"hooks"`
		} `json:"data"`
	}
	if err = json.Unmarshal(raw, &result); err != nil {
		return
	}
	if len(result.Errors)+len(result.Warnings) > 0 {
		return 0, 0, errors.New("host reported hook loading diagnostics")
	}
	expected := map[string]bool{}
	for _, event := range reviewer.Events() {
		// hooks.json uses PascalCase; the app-server wire enum uses camelCase.
		expected[strings.ToLower(event[:1])+event[1:]] = true
	}
	seen := map[string]bool{}
	for _, group := range result.Data {
		if len(group.Errors)+len(group.Warnings) > 0 {
			return 0, 0, errors.New("host reported hook loading diagnostics")
		}
		for _, h := range group.Hooks {
			if h.PluginID == "eva-md@eva-md" {
				if !expected[h.EventName] || seen[h.EventName] {
					return 0, 0, errors.New("unexpected or duplicate eva-md hook event")
				}
				seen[h.EventName] = true
				count++
				if h.Enabled && (h.Trust == "trusted" || h.Trust == "managed") {
					trusted++
				}
			}
		}
	}
	return
}
