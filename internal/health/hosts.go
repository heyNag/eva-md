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
	"strings"
	"time"

	"github.com/heyNag/eva-md/internal/install"
	"github.com/heyNag/eva-md/internal/paths"
)

func inspectHosts(ctx context.Context, r *Report, d paths.Dirs) {
	codexEnabled := false
	for _, host := range []string{"claude", "codex"} {
		// Inventory may contact an unrelated remote marketplace. Give each
		// host its own budget so a stalled listing cannot consume doctor's
		// whole deadline and conceal independent local hook-trust evidence.
		inventoryCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
		status, err := install.Inspect(inventoryCtx, d, host)
		timedOut := errors.Is(inventoryCtx.Err(), context.DeadlineExceeded)
		cancel()
		if timedOut {
			r.add(host+" plugin", "UNKNOWN", "native plugin inventory timed out", "inspect the native host plugin inventory")
		} else if err != nil && status.Available && strings.Contains(err.Error(), "eva-md") {
			// Only install's conflict errors name eva-md; command and parse
			// failures do not.
			r.add(host+" plugin", "FAIL", strings.TrimPrefix(err.Error(), "install: "), "remove the conflicting eva-md registration with the host's plugin commands, then run eva-md install --host "+host)
		} else if err != nil || !status.Available {
			r.add(host+" plugin", "UNKNOWN", "native plugin inventory is unavailable", "inspect the native host plugin inventory")
		} else if !status.Installed {
			r.add(host+" plugin", "FAIL", "eva-md is not installed", "run eva-md install --host "+host)
		} else if !status.Valid {
			r.add(host+" plugin", "FAIL", status.Reason, "inspect native plugin list before reinstalling")
		} else {
			r.add(host+" plugin", "PASS", "one enabled plugin at the expected path", "")
		}
		if host == "codex" && !timedOut && err == nil && status.Installed && status.Enabled {
			codexEnabled = true
		}
	}
	if data, e := hostCommand(ctx, "codex", "features", "list"); e == nil {
		known, enabled := false, false
		for _, line := range strings.Split(string(data), "\n") {
			fields := strings.Fields(line)
			if len(fields) >= 2 && fields[0] == "hooks" {
				known = true
				enabled = fields[len(fields)-1] == "true"
			}
		}
		if known && enabled {
			r.add("codex hooks feature", "PASS", "enabled", "")
		} else if known {
			r.add("codex hooks feature", "FAIL", "disabled", "enable hooks using Codex settings")
		} else {
			r.add("codex hooks feature", "UNKNOWN", "not reported by this version", "")
		}
	}
	cwd, _ := os.Getwd()
	count, trusted, err := probeTrust(ctx, cwd)
	if err != nil {
		r.add("codex hook trust", "UNKNOWN", "read-only hooks/list is unavailable", "review /hooks in Codex")
	} else if count == 0 && codexEnabled {
		r.add("codex hook trust", "FAIL", "Codex loads no eva-md hooks for the enabled plugin", "check the Codex hooks feature, then run eva-md install --host codex")
	} else if count == 0 {
		r.add("codex hook trust", "UNKNOWN", "no eva-md hooks loaded", "install eva-md before reviewing /hooks")
	} else if count != 7 || trusted != count {
		r.add("codex hook trust", "FAIL", fmt.Sprintf("%d of 7 required events trusted and enabled (%d loaded)", trusted, count), "inspect eva-md's seven hooks and approve them in Codex /hooks")
	} else {
		r.add("codex hook trust", "PASS", fmt.Sprintf("%d hooks trusted and enabled", count), "")
	}
	r.add("claude managed hook policy", "UNKNOWN", "policy is judged by hook heartbeats, not settings-file guesses", "confirm SessionStart fires in an enabled session")
}

type cappedBuffer struct {
	bytes.Buffer
	limit int
}

func (b *cappedBuffer) Write(p []byte) (int, error) {
	if b.Len()+len(p) > b.limit {
		return 0, errors.New("host output limit")
	}
	return b.Buffer.Write(p)
}
func hostCommand(ctx context.Context, name string, args ...string) ([]byte, error) {
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, name, args...)
	cmd.WaitDelay = time.Second
	b := &cappedBuffer{limit: 8 << 20}
	cmd.Stdout = b
	cmd.Stderr = io.Discard
	if err := cmd.Run(); err != nil {
		return nil, errors.New("host command unavailable")
	}
	return b.Bytes(), nil
}

// probeTrust sends only read-only methods, never a thread/turn or approval.
func probeTrust(parent context.Context, cwd string) (count, trusted int, err error) {
	ctx, cancel := context.WithTimeout(parent, 8*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, "codex", "--no-daemon", "app-server", "--stdio")
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
	for _, event := range hostEvents("codex") {
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
