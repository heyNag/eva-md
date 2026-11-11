package shapes

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/heyNag/eva-md/internal/catchup"
	"github.com/heyNag/eva-md/internal/host/adapter"
	"github.com/heyNag/eva-md/internal/inbox"
	"github.com/heyNag/eva-md/internal/paths"
	"github.com/heyNag/eva-md/internal/session"
)

// The hosts, as the adapter names them.
var (
	claude = string(adapter.ClaudeCode)
	codex  = string(adapter.Codex)
)

// marker begins every made-up value, key and name the fixtures plant, so
// a report that prints one is caught.
const marker = "zqmk"

// fixture is a made-up home with an inbox, recovery records, markers and
// the hosts' folders of chats. home is the home as the report and the
// hosts name it, which may be a link to real, where the files are and
// eva-md's own folders, which refuse a home that is a link.
type fixture struct {
	t          *testing.T
	home, real string
	dirs       paths.Dirs
	cwd        string // the chats' folder, as the hosts write it
	rowLimit   int
}

// Where a fixture's home holds a link, as macOS reaches its temporary
// folders through one and a person may link their hosts' folders.
const (
	noLink      = "no link"
	homeLink    = "the home"     // the home is named through a link
	dotfiles    = "dotfiles"     // ~/.claude and ~/.codex are links
	chatFolders = "chat folders" // ~/.claude/projects and ~/.codex/sessions are links
)

// links are the ways a home may hold a link, none first.
var links = []string{noLink, homeLink, dotfiles, chatFolders}

// newFixture makes a home that holds a link where link says.
func newFixture(t *testing.T, link string) *fixture {
	t.Helper()
	real, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	home := real
	symlink := func(target, name string) {
		t.Helper()
		if err := os.MkdirAll(target, 0o700); err != nil {
			t.Fatal(err)
		}
		if err := os.MkdirAll(filepath.Dir(name), 0o700); err != nil {
			t.Fatal(err)
		}
		if err := os.Symlink(target, name); err != nil {
			t.Fatal(err)
		}
	}
	elsewhere := filepath.Join(real, marker+"-dotfiles")
	switch link {
	case homeLink:
		// In a temporary folder of its own, which on macOS is itself
		// reached through /var.
		home = filepath.Join(t.TempDir(), "home")
		symlink(real, home)
	case dotfiles:
		symlink(filepath.Join(elsewhere, ".claude"), filepath.Join(real, ".claude"))
		symlink(filepath.Join(elsewhere, ".codex"), filepath.Join(real, ".codex"))
	case chatFolders:
		symlink(filepath.Join(elsewhere, "projects"), filepath.Join(real, ".claude", "projects"))
		symlink(filepath.Join(elsewhere, "sessions"), filepath.Join(real, ".codex", "sessions"))
	}
	t.Setenv("HOME", real)
	t.Setenv("GIT_CONFIG_GLOBAL", filepath.Join(real, "gitconfig"))
	t.Setenv("GIT_CONFIG_NOSYSTEM", "1")
	f := &fixture{t: t, home: home, real: real, cwd: filepath.Join(home, marker+"-cwd"),
		dirs: paths.Dirs{Config: filepath.Join(real, ".config", "eva-md"), State: filepath.Join(real, ".local", "state", "eva-md"), Share: filepath.Join(real, ".local", "share", "eva-md")}}
	if err := os.MkdirAll(filepath.Join(real, marker+"-cwd"), 0o700); err != nil {
		t.Fatal(err)
	}
	return f
}

// at is a time of the fixtures' day, minutes and seconds past 10:00 UTC.
func at(min, sec int) string {
	return time.Date(2026, 11, 2, 10, min, sec, 0, time.UTC).Format(time.RFC3339)
}

// session stores the session's inbox lines with v1's own writer, in the
// chats' folder, and makes its recovery record as a hook does, naming the
// host's file at path ("" for none).
func (f *fixture) session(host, id, path string, lines ...inbox.Entry) {
	f.t.Helper()
	if len(lines) == 0 {
		lines = []inbox.Entry{{TS: at(0, 0), Turn: "t1", Role: "user", Kind: "prompt", Text: marker + " prompt"}}
	}
	for _, e := range lines {
		e.V, e.Host, e.Session = 1, host, id
		e.Project, e.Branch, e.Model = marker+"-project", marker+"-branch", marker+"-model"
		if e.Cwd == "" {
			e.Cwd = marker + "-cwd"
		}
		if e.Origin == "" {
			e.Origin = "hook"
		}
		if err := (inbox.Store{Root: f.dirs.Inbox()}).Append(e); err != nil {
			f.t.Fatal(err)
		}
	}
	e := inbox.Entry{V: 1, Host: host, Session: id, Project: marker + "-project", Branch: marker + "-branch", Cwd: marker + "-cwd"}
	if err := catchup.Register(context.Background(), f.dirs, f.home, path, f.cwd, e, false, false, time.Date(2026, 11, 2, 9, 0, 0, 0, time.UTC)); err != nil {
		f.t.Fatal(err)
	}
}

// appendInbox adds lines to the session's inbox file as they are, with no
// check of the writer's: a line may name another project than the file's
// folder.
func (f *fixture) appendInbox(host, id string, lines ...inbox.Entry) {
	f.t.Helper()
	path := inbox.Store{Root: f.dirs.Inbox()}.Path(marker+"-project", host, id)
	file, err := os.OpenFile(path, os.O_WRONLY|os.O_APPEND, 0)
	if err != nil {
		f.t.Fatal(err)
	}
	defer file.Close()
	for _, e := range lines {
		e.V, e.Host, e.Session, e.Origin = 1, host, id, "hook"
		if e.Project == "" {
			e.Project = marker + "-project"
		}
		data, err := json.Marshal(e)
		if err != nil {
			f.t.Fatal(err)
		}
		if _, err := file.Write(append(data, '\n')); err != nil {
			f.t.Fatal(err)
		}
	}
}

// block marks the session's recovery record blocked as said off the
// record, as recovery does when it cannot write the session's marker.
func (f *fixture) block(host, id string) {
	f.t.Helper()
	path := filepath.Join(f.dirs.State, "catchup", host, id+".json")
	data, err := os.ReadFile(path)
	if err != nil {
		f.t.Fatal(err)
	}
	var record map[string]any
	if err := json.Unmarshal(data, &record); err != nil {
		f.t.Fatal(err)
	}
	record["blocked"], record["reason"] = true, "off_record"
	if data, err = json.Marshal(record); err != nil {
		f.t.Fatal(err)
	}
	if err := os.WriteFile(path, data, 0o600); err != nil {
		f.t.Fatal(err)
	}
}

// mark sets a session marker, as the hooks do.
func (f *fixture) mark(host, id, name string) {
	f.t.Helper()
	if err := (session.Markers{Root: f.dirs.Sessions()}).Set(host, id, name, "1"); err != nil {
		f.t.Fatal(err)
	}
}

// claudeFile writes a Claude Code session file in the project folder
// folder ("" for the chats' folder's) and returns its path. A row that is
// a string is written as it is.
func (f *fixture) claudeFile(folder, id string, rows ...any) string {
	if folder == "" {
		folder = "-" + marker + "-cwd"
	}
	return f.write(filepath.Join(f.real, ".claude", "projects", folder, id+".jsonl"), rows...)
}

// subagentFile writes the file of a subagent of the Claude Code session
// id, in the chats' folder.
func (f *fixture) subagentFile(id, agent string, rows ...any) string {
	return f.write(filepath.Join(f.real, ".claude", "projects", "-"+marker+"-cwd", id, "subagents", agent+".jsonl"), rows...)
}

// codexFile writes a Codex session file in a day's folder and returns its
// path.
func (f *fixture) codexFile(day int, id string, rows ...any) string {
	return f.write(filepath.Join(f.real, ".codex", "sessions", "2026", "11", fmt.Sprintf("%02d", day),
		fmt.Sprintf("rollout-2026-11-%02dT10-00-00-%s.jsonl", day, id)), rows...)
}

// write writes rows to path, a row a line.
func (f *fixture) write(path string, rows ...any) string {
	f.t.Helper()
	var b bytes.Buffer
	for _, r := range rows {
		if s, ok := r.(string); ok {
			b.WriteString(s + "\n")
			continue
		}
		data, err := json.Marshal(r)
		if err != nil {
			f.t.Fatal(err)
		}
		b.Write(data)
		b.WriteByte('\n')
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		f.t.Fatal(err)
	}
	if err := os.WriteFile(path, b.Bytes(), 0o600); err != nil {
		f.t.Fatal(err)
	}
	return path
}

// options are the report's options for the home.
func (f *fixture) options() Options {
	return Options{Dirs: f.dirs, Home: f.home, TempDir: filepath.Join(f.home, "tmp"), Version: "2026.11.9", rowLimit: f.rowLimit,
		Now: time.Date(2026, 11, 11, 12, 0, 0, 0, time.UTC)}
}

// run makes the report and prints it.
func (f *fixture) run() (Report, string) {
	f.t.Helper()
	return f.runWith(context.Background())
}

func (f *fixture) runWith(ctx context.Context) (Report, string) {
	f.t.Helper()
	r, err := Run(ctx, f.options())
	if err != nil {
		f.t.Fatal(err)
	}
	var out bytes.Buffer
	if err := r.Write(&out); err != nil {
		f.t.Fatalf("%v: %+v", err, r)
	}
	return r, out.String()
}

// get is the count at a path of a tree of the report, 0 when none.
func get(tree map[string]any, path string) int {
	var node any = tree
	for _, p := range strings.Split(path, "/") {
		m, ok := node.(map[string]any)
		if !ok {
			return 0
		}
		node = m[p]
	}
	c, _ := node.(Count)
	return int(c)
}

// leaves are a tree's counts by path.
func leaves(tree map[string]any) map[string]int {
	out := map[string]int{}
	var walk func(prefix string, m map[string]any)
	walk = func(prefix string, m map[string]any) {
		for k, v := range m {
			switch v := v.(type) {
			case map[string]any:
				walk(prefix+k+"/", v)
			case Count:
				out[prefix+k] = int(v)
			}
		}
	}
	walk("", tree)
	return out
}

// Claude Code's rows: a user row with text, an assistant row ending its
// turn, and a row of any type, each of the session id with the chats'
// folder.
func cUser(f *fixture, id, turn, ts, text string) map[string]any {
	return map[string]any{"type": "user", "sessionId": id, "promptId": turn, "timestamp": ts, "cwd": f.cwd,
		"message": map[string]any{"role": "user", "content": text}}
}

func cReply(f *fixture, id, turn, ts string) map[string]any {
	return map[string]any{"type": "assistant", "sessionId": id, "promptId": turn, "timestamp": ts, "cwd": f.cwd,
		"message": map[string]any{"role": "assistant", "stop_reason": "end_turn", "content": []any{map[string]any{"type": "text", "text": marker + " reply"}}}}
}

// Codex's rows: the session's first record, and a row of any type.
func xMeta(f *fixture, id string, extra map[string]any) map[string]any {
	p := map[string]any{"id": id, "cwd": f.cwd}
	for k, v := range extra {
		p[k] = v
	}
	return map[string]any{"timestamp": at(0, 0), "type": "session_meta", "payload": p}
}

func xRow(ts, typ string, payload map[string]any) map[string]any {
	return map[string]any{"timestamp": ts, "type": typ, "payload": payload}
}
