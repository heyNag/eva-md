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

	"github.com/heyNag/eva-md/internal/config"
	"github.com/heyNag/eva-md/internal/identity"
	"github.com/heyNag/eva-md/internal/inbox"
	"github.com/heyNag/eva-md/internal/install"
	"github.com/heyNag/eva-md/internal/paths"
	"github.com/heyNag/eva-md/internal/update"
)

// mcpTools are the tools the MCP server lists, in its order.
const mcpTools = "eva_search, eva_show, eva_status"

func init() {
	registerRows(700, "mcp manifests", func(r *Report, in *inspection) { inspectMCPManifests(r, in.d) })
	registerRows(900, "mcp server", func(r *Report, in *inspection) {
		if in.hosts {
			inspectMCPServer(in.ctx, r, in.d)
		}
	})
}

// inspectMCPManifests adds a row for each plugin host: whether the plugin
// folder's .mcp.json declares the eva-md server at the installed binary. It
// compares a file and starts nothing, so eva-md status has the rows too.
func inspectMCPManifests(r *Report, d paths.Dirs) {
	binary := filepath.Join(d.Share, "bin", "eva-md")
	for _, host := range []string{"claude", "codex"} {
		name := host + " mcp manifest"
		if !Installed(d, host) {
			if app, absent := Absent(host); absent {
				r.add(name, "UNKNOWN", app+" isn't installed on this Mac", "")
			} else {
				r.add(name, "UNKNOWN", "eva-md is not installed for "+host, "run eva-md install")
			}
			continue
		}
		data, err := readPrivate(filepath.Join(pluginFolder(d, host), ".mcp.json"), 64<<10)
		if err != nil || !bytes.Equal(data, install.MCPManifest(host, binary)) {
			r.add(name, "FAIL", "the plugin folder predates the MCP server, or differs", "run eva-md install")
			continue
		}
		r.add(name, "PASS", "declares the eva-md server at the installed binary", "")
	}
}

// pluginFolder is a plugin host's installed plugin folder.
func pluginFolder(d paths.Dirs, host string) string {
	return filepath.Join(d.Share, "marketplace", cli(host), "plugins", "eva-md")
}

// inspectMCPServer adds the doctor's mcp server row: the installed binary,
// started as the Claude Code plugin starts it, from / with only HOME and
// PATH, answers initialize and lists the tools. That proves the binary
// answers, not that a host connected it.
func inspectMCPServer(ctx context.Context, r *Report, d paths.Dirs) {
	binary := filepath.Join(d.Share, "bin", "eva-md")
	if info, err := os.Lstat(binary); err != nil || !info.Mode().IsRegular() || info.Mode()&0o111 == 0 {
		r.add("mcp server", "UNKNOWN", "stable hook binary is not installed", "run eva-md install")
		return
	}
	tools, took, err := probeMCP(ctx, binary, mcpProbeTimeout)
	switch {
	case err != nil:
		r.add("mcp server", "FAIL", "the installed binary did not answer as an MCP server", "run eva-md install")
	case len(tools) == 0:
		r.add("mcp server", "UNKNOWN", "mcp = false in the config; the tools are off", "")
	case strings.Join(tools, ", ") != mcpTools:
		r.add("mcp server", "FAIL", "the installed binary lists other tools: "+list(tools), "run eva-md install")
	default:
		r.add("mcp server", "PASS", fmt.Sprintf("answers initialize and lists %s in %d ms", mcpTools, took.Milliseconds()), "")
	}
}

// mcpProbeTimeout bounds the doctor's probe of the MCP server. Tests raise it
// before any test runs, so a slow machine can't fail a test of what the
// probe finds.
var mcpProbeTimeout = 3 * time.Second

// DoctorClient is the client name the doctor's probe sends, which has the
// server answer without waiting for a chat's SessionStart hook: the probe
// starts no chat, so none comes.
const DoctorClient = "eva-md-doctor"

// probeMCP starts binary as an MCP server for at most limit, mcpProbeTimeout
// or a short one in the test of a server that hangs, sends initialize,
// notifications/initialized and tools/list, and returns the names of the
// tools it lists and how long the answers took. It names itself
// DoctorClient. It reads at most 64 KB, then closes the server's stdin,
// which ends it.
func probeMCP(parent context.Context, binary string, limit time.Duration) (tools []string, took time.Duration, err error) {
	ctx, cancel := context.WithTimeout(parent, limit)
	defer cancel()
	cmd := exec.CommandContext(ctx, binary, "mcp", "--host", "claude-code")
	cmd.Dir = "/"
	home, _ := os.UserHomeDir()
	cmd.Env = []string{"HOME=" + home, "PATH=" + os.Getenv("PATH")}
	cmd.Stderr = io.Discard
	cmd.WaitDelay = time.Second
	in, err := cmd.StdinPipe()
	if err != nil {
		return nil, 0, err
	}
	out, err := cmd.StdoutPipe()
	if err != nil {
		return nil, 0, err
	}
	start := time.Now()
	if err = cmd.Start(); err != nil {
		return nil, 0, err
	}
	defer func() { in.Close(); _ = cmd.Wait() }()
	requests := `{"jsonrpc":"2.0","id":1,"method":"initialize","params":{"protocolVersion":"2025-06-18","capabilities":{},"clientInfo":{"name":"` + DoctorClient + `","version":"1"}}}` + "\n" +
		`{"jsonrpc":"2.0","method":"notifications/initialized"}` + "\n" +
		`{"jsonrpc":"2.0","id":2,"method":"tools/list"}` + "\n"
	if _, err = io.WriteString(in, requests); err != nil {
		return nil, 0, err
	}
	s := bufio.NewScanner(io.LimitReader(out, 64<<10))
	s.Buffer(make([]byte, 4096), 64<<10)
	initialized, listed := false, false
	for (!initialized || !listed) && s.Scan() {
		var reply struct {
			ID     json.RawMessage `json:"id"`
			Result json.RawMessage `json:"result"`
			Error  json.RawMessage `json:"error"`
		}
		if json.Unmarshal(s.Bytes(), &reply) != nil || len(reply.Error) > 0 && string(reply.Error) != "null" {
			return nil, 0, errors.New("not an MCP answer")
		}
		switch string(reply.ID) {
		case "1":
			var r struct {
				ServerInfo struct{ Name string } `json:"serverInfo"`
			}
			if json.Unmarshal(reply.Result, &r) != nil || r.ServerInfo.Name != "eva-md" {
				return nil, 0, errors.New("not eva-md's server")
			}
			initialized = true
		case "2":
			var r struct {
				Tools []struct{ Name string } `json:"tools"`
			}
			if json.Unmarshal(reply.Result, &r) != nil || r.Tools == nil {
				return nil, 0, errors.New("no tool list")
			}
			tools = []string{}
			for _, t := range r.Tools {
				tools = append(tools, t.Name)
			}
			listed = true
		}
	}
	if !initialized || !listed {
		return nil, 0, errors.New("no answer")
	}
	return tools, time.Since(start), nil
}

// NewestBeat returns the time of host's newest valid heartbeat for event,
// or for any of the host's events when event is "".
func NewestBeat(d paths.Dirs, host, event string, now time.Time) (time.Time, bool) {
	events := hostEvents(host)
	if event != "" {
		events = []string{event}
	}
	var newest time.Time
	for _, e := range events {
		if b, valid := readBeat(d, host, e, now); valid && b.At.After(newest) {
			newest = b.At
		}
	}
	return newest, !newest.IsZero()
}

// Backlog is what waits for an update in one project's inbox folder: of
// its 64 newest session files, those longer than their watermark, and the
// bytes past it, as eva-md status counts them, with one stat and one small
// read a file. A watermark that can't be read or is past the end of its
// file is left out; the doctor names it.
func Backlog(ctx context.Context, d paths.Dirs, project identity.Project) (sessions int, bytes int64, err error) {
	name, err := paths.SafeName(project.Key)
	if err != nil {
		return 0, 0, err
	}
	left := maxEntries
	files, _ := newestSessions(filepath.Join(d.Inbox(), name), &left)
	store := inbox.Store{Root: d.Inbox()}
	for _, f := range files {
		if err := ctx.Err(); err != nil {
			return sessions, bytes, err
		}
		m, err := store.Mark(f.path)
		if err != nil || m.Offset > f.size {
			continue
		}
		if n := m.Pending(f.size); n > 0 {
			sessions++
			bytes += n
		}
	}
	return sessions, bytes, nil
}

// Summary is eva_status's two lines on the hooks and the background work:
// when each host's hooks last ran and how many of their recent runs failed,
// and whether Codex's await approval; then each host's last update, the
// daily commit and the push. It reads the newest records, as the doctor
// does, and asks no host.
func Summary(ctx context.Context, d paths.Dirs, c config.Config, now time.Time) []string {
	var hooks []string
	for _, host := range updateHosts {
		at, ran := NewestBeat(d, host, "", now)
		switch {
		case ran:
			part := host + " ran " + howLongAgo(now.Sub(at))
			if failed, runs := hookRuns(d, host, now); runs > 0 {
				part += fmt.Sprintf(" (%d of the last %d runs failed)", failed, runs)
			}
			hooks = append(hooks, part)
		case Installed(d, host):
			hooks = append(hooks, host+" never ran")
		default:
			hooks = append(hooks, host+" not installed")
		}
	}
	line := "hooks: " + strings.Join(hooks, ", ")
	if TrustAwaited(d, now) {
		line += "; Codex hooks await approval in /hooks"
	}
	if ctx.Err() != nil {
		return []string{line, "updates: unknown"}
	}
	return []string{line, "updates: " + updatesSummary(d, c, now)}
}

// updatesSummary words each host's updates and the daily commit and push in
// one line.
func updatesSummary(d paths.Dirs, c config.Config, now time.Time) string {
	if c.WikiPath == "" {
		return "no wiki is set up"
	}
	var parts []string
	if !c.Updates {
		parts = append(parts, "off (updates = false)")
	} else {
		records, err := update.Recent(d.State, now, doctorLog)
		for _, host := range updateHosts {
			switch {
			case !Installed(d, host):
				parts = append(parts, host+" not installed")
			case err != nil:
				parts = append(parts, host+" unknown, as the run log is unreadable")
			default:
				parts = append(parts, host+" "+updateState(update.Host(records, host, now), host))
			}
		}
	}
	return strings.Join(parts, ", ") + "; " + commitState(d, c, now)
}

// updateState words a host's run-log state in a few words.
func updateState(s update.HostState, host string) string {
	var state string
	switch {
	case s.Held != "":
		state = "paused today (" + holdWords(s.Held, host) + ")"
	case len(s.Failures) > 0:
		state = fmt.Sprintf("last %s %s, %d failed in a row", when(s.Last.At), runOutcome(s.Last), len(s.Failures))
	case s.Last.At != "":
		state = "last " + when(s.Last.At) + " " + runOutcome(s.Last)
	default:
		state = "none today or yesterday"
	}
	if n := len(s.Quarantined); n > 0 {
		state += fmt.Sprintf(", %d %s quarantined", n, plural(n, "session", "sessions"))
	}
	return state
}

// commitState words the daily commit and the push in a few words.
func commitState(d paths.Dirs, c config.Config, now time.Time) string {
	if !c.Commits {
		return "daily commit off (commits = false)"
	}
	records, err := commitRecords(d.State, now, doctorCommit)
	if err != nil {
		return "daily commit unknown, as its records are unreadable"
	}
	s := readCommits(records)
	state := "no daily commit yet"
	if s.last.At != "" {
		state = "daily commit " + when(s.last.At) + " " + s.last.Outcome
		if s.last.Reason != "" {
			state += " (" + s.last.Reason + ")"
		}
	}
	if s.push != "" {
		state += ", push " + s.push
		if !s.pushBad.IsZero() {
			state += " since " + s.pushBad.Format("2006-01-02 15:04")
		}
	}
	return state
}

// TrustAwaited reports the needs-trust flag install leaves for Codex when
// no Codex SessionStart hook has run since, as QuickNotice reads it.
func TrustAwaited(d paths.Dirs, now time.Time) bool {
	file := filepath.Join(d.State, "needs-trust.json")
	info, err := os.Lstat(file)
	if err != nil {
		return false
	}
	data, err := readPrivate(file, 16384)
	var flag struct {
		Host       string `json:"host"`
		NeedsTrust bool   `json:"needs_trust"`
	}
	if err != nil || json.Unmarshal(data, &flag) != nil || flag.Host != "codex" || !flag.NeedsTrust {
		return false
	}
	b, valid := readBeat(d, "codex", "SessionStart", now)
	return !valid || !b.At.After(info.ModTime())
}

// howLongAgo words how long ago something happened.
func howLongAgo(d time.Duration) string {
	if d < time.Minute {
		return "under a minute ago"
	}
	return age(d) + " ago"
}
