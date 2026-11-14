package drive

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"os"
	"path/filepath"
	"reflect"
	"slices"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/heyNag/eva-md/internal/eval/continuation/drive/fakehost"
	"github.com/heyNag/eva-md/internal/eval/continuation/task"
	"github.com/heyNag/eva-md/internal/host/adapter"
)

// webOff are the flags that turn each host's web tools off when the CLI
// lists every web feature, written out here apart from the host table, so
// that a table that drops one fails.
var webOff = map[adapter.ID][]string{
	adapter.ClaudeCode: {"--disallowedTools", "WebSearch", "WebFetch"},
	adapter.Codex: {"-c", "web_search=disabled", "--disable", "standalone_web_search", "--disable", "browser_use",
		"--disable", "browser_use_external", "--disable", "browser_use_full_cdp_access", "--disable", "computer_use"},
}

// keptTools are the tools a chat needs to edit files and run tests, which
// no flag may deny: Claude Code's by their names, Codex's by its features.
var keptTools = map[adapter.ID][]string{
	adapter.ClaudeCode: {"Bash", "Edit", "Write", "MultiEdit", "Read", "Glob", "Grep", "NotebookEdit"},
	adapter.Codex:      {"shell_tool", "unified_exec", "apply_patch_freeform"},
}

// checkWebOff fails t unless a chat's arguments hold the host's web-off
// flags, after every other flag of the chat (the condition's among them)
// and right before the prompt, or on Codex before the session it
// continues, and deny none of keptTools.
func checkWebOff(t *testing.T, id adapter.ID, args []string) {
	t.Helper()
	end := slices.Index(args, "--")
	if end < 0 {
		t.Fatalf("no prompt in %q", args)
	}
	flags := args[:end]
	if id == adapter.Codex {
		if i := slices.Index(flags, "resume"); i >= 0 {
			flags = flags[:i]
		}
	}
	want := webOff[id]
	if len(flags) < len(want) || !slices.Equal(flags[len(flags)-len(want):], want) {
		t.Fatalf("%s: the chat's flags %q do not end with %q", id, flags, want)
	}
	for _, kept := range keptTools[id] {
		switch id {
		case adapter.ClaudeCode:
			for i, a := range args[:end] {
				if (a == "--disallowedTools" || a == "--disallowed-tools") && slices.ContainsFunc(args[i+1:end], func(v string) bool {
					return v == kept || strings.HasPrefix(v, kept+"(") || strings.Contains(v, ","+kept) || strings.Contains(v, kept+",")
				}) {
					t.Fatalf("%s: %s is denied: %q", id, kept, args)
				}
				if a == "--tools" || a == "--allowedTools" {
					t.Fatalf("%s: the chat's tools are narrowed: %q", id, args)
				}
			}
		case adapter.Codex:
			if holdsRun(args[:end], []string{"--disable", kept}) || slices.Contains(args[:end], "features."+kept+"=false") {
				t.Fatalf("%s: %s is turned off: %q", id, kept, args)
			}
		}
	}
}

// checkChatsWebOff checks each chat call of calls, a start or a
// follow-up, with checkWebOff, and gives how many there were.
func checkChatsWebOff(t *testing.T, id adapter.ID, calls []fakehost.Call) int {
	t.Helper()
	n := 0
	for _, c := range calls {
		if c.Mode == "start" || c.Mode == "continue" {
			checkWebOff(t, id, c.Args)
			n++
		}
	}
	return n
}

// The host table's arguments turn the web tools off, for a first call and
// a follow-up, after the flags of each condition: memory off, the plugin's,
// and a condition's own.
func TestWebToolsOffInTheTable(t *testing.T) {
	for _, h := range adapter.All() {
		e := hosts[h.ID]
		var listed int
		web, err := e.WebOff(func(args ...string) ([]byte, error) {
			listed++
			if !slices.Equal(args, []string{"features", "list"}) {
				t.Fatalf("%s: listed %q", h.ID, args)
			}
			var b strings.Builder
			for _, name := range fakehost.DefaultFeatures {
				b.WriteString(name + "  stable  true\n")
			}
			return []byte(b.String()), nil
		})
		if err != nil || !slices.Equal(web, webOff[h.ID]) {
			t.Fatalf("%s: %q %v", h.ID, web, err)
		}
		if want := map[adapter.ID]int{adapter.Codex: 1}[h.ID]; listed != want {
			t.Fatalf("%s: listed %d times, want %d", h.ID, listed, want)
		}
		plugin := map[adapter.ID][]string{
			adapter.ClaudeCode: {"--settings", `{"autoMemoryEnabled":false}`, "--plugin-dir", "/made-up/plugin"},
			adapter.Codex:      {"--disable", "plugins"},
		}[h.ID]
		for _, extra := range [][]string{e.Off, plugin, append(slices.Clone(e.Off), "--made-up-flag")} {
			for _, resume := range []string{"", "s-1"} {
				args := e.Argv(chat{Prompt: "a made-up prompt", Turns: 5, Model: "made-up-model", Resume: resume, Extra: extra, Web: web})
				checkWebOff(t, h.ID, args)
				if !holdsRun(args, extra) {
					t.Fatalf("%s: the condition's flags are not in %q", h.ID, args)
				}
			}
		}
	}
}

// Codex turns off only the web features its list names, since it refuses
// a name it does not know, and its web search by its setting always; a
// list it cannot give, or one that names nothing, stops the run.
func TestCodexWebOffFollowsItsList(t *testing.T) {
	list := func(out string, err error) func(...string) ([]byte, error) {
		return func(...string) ([]byte, error) { return []byte(out), err }
	}
	got, err := codexWebOff(list("shell_tool  stable  true\nstandalone_web_search  experimental  false\nnot a feature line\n", nil))
	if want := []string{"-c", "web_search=disabled", "--disable", "standalone_web_search"}; err != nil || !slices.Equal(got, want) {
		t.Fatalf("%q %v, want %q", got, err, want)
	}
	got, err = codexWebOff(list("shell_tool  stable  true\n", nil))
	if want := []string{"-c", "web_search=disabled"}; err != nil || !slices.Equal(got, want) {
		t.Fatalf("%q %v, want %q", got, err, want)
	}
	for name, l := range map[string]func(...string) ([]byte, error){
		"an error":       list("", errors.New("made-up failure")),
		"an empty list":  list("", nil),
		"no feature row": list("a made-up notice\n", nil),
	} {
		if got, err := codexWebOff(l); err == nil {
			t.Errorf("%s: %q", name, got)
		}
	}
}

// On the fake, every chat of a run, first call and follow-up, under memory
// off, the ideal record, a condition's own flags and eva-md's plugin, has
// the host's web tools off; on Codex the run asks the CLI for its features
// once a trial, in the trial's environment, and turns off only those it
// lists.
func TestEveryChatHasItsWebToolsOff(t *testing.T) {
	for _, h := range adapter.All() {
		for _, c := range []string{"off", "oracle", "launch", "plugin"} {
			t.Run(string(h.ID)+"/"+c, func(t *testing.T) {
				r := newRig(t, 1)
				r.opts.Host, r.opts.Trials = string(h.ID), 2
				switch c {
				case "oracle":
					r.opts.Condition = task.Oracle
				case "launch", "plugin":
					r.opts.Condition = task.Eva
					useArm(t, Arm{Prepare: func(_ context.Context, tr Trial) (Launch, error) {
						if c == "launch" {
							return Launch{Args: []string{"--made-up-flag"}}, nil
						}
						write(t, filepath.Join(pluginDir(tr.Dirs.Home, h), "hooks", "hooks.json"),
							`{"hooks":{"Stop":[{"hooks":[{"type":"command","command":"'eva-md' hook --host x","timeout":5}]}]}}`)
						return Launch{Plugin: true}, nil
					}})
				}
				s, out, err := r.run()
				if err != nil || s.Judged != 2 {
					t.Fatalf("run: %+v %v\n%s", s, err, out)
				}
				calls := r.calls()
				chats := checkChatsWebOff(t, h.ID, calls)
				want := 4 // a start and a follow-up a trial
				if c == "oracle" {
					want = 2
				}
				if chats != want || countMode(calls, "continue") != want-2 {
					t.Fatalf("%d chats, want %d: %+v", chats, want, calls)
				}
				lists := countMode(calls, "tools")
				if h.ID == adapter.Codex && lists != 2 || h.ID != adapter.Codex && lists != 0 {
					t.Fatalf("%d lists of features", lists)
				}
				for _, call := range calls {
					if call.Mode == "tools" && !strings.HasSuffix(call.Fixed["HOME"], filepath.Join("t1", "home")) && !strings.HasSuffix(call.Fixed["HOME"], filepath.Join("t2", "home")) {
						t.Fatalf("the list ran outside a trial's home: %v", call.Fixed)
					}
				}
			})
		}
	}
	// A Codex that lists no web feature is given its setting alone.
	t.Run("codex/no web feature", func(t *testing.T) {
		r := newRig(t, 1)
		r.opts.Host = string(adapter.Codex)
		r.script.Features = []string{"shell_tool", "hooks", "plugins"}
		if s, out, err := r.run(); err != nil || s.Judged != 1 {
			t.Fatalf("run: %+v %v\n%s", s, err, out)
		}
		for _, call := range r.calls() {
			if call.Mode == "start" && (slices.Contains(call.Args, "standalone_web_search") || !holdsRun(call.Args, []string{"-c", "web_search=disabled"})) {
				t.Fatalf("args: %q", call.Args)
			}
		}
	})
}

// A chat that the host table gives no web-off flags is a failure the tests
// see: the fake answers nothing, and the trial is host_failed, not judged.
func TestAChatWithItsWebToolsOnFails(t *testing.T) {
	for _, h := range adapter.All() {
		t.Run(string(h.ID), func(t *testing.T) {
			was := hosts[h.ID]
			t.Cleanup(func() { hosts[h.ID] = was })
			e := was
			e.WebOff = func(func(...string) ([]byte, error)) ([]string, error) { return []string{"--made-up-flag"}, nil }
			hosts[h.ID] = e
			r := newRig(t, 1)
			r.opts.Host = string(h.ID)
			s, out, err := r.run()
			if err != nil || s.Judged != 0 || s.Unjudged[CodeHostFailed] != 1 {
				t.Fatalf("run: %+v %v\n%s", s, err, out)
			}
			ends, err := fakehost.ReadEnds(r.script.Dir)
			if err != nil || !slices.ContainsFunc(ends, func(e fakehost.End) bool { return e.Exit == 3 && strings.Contains(e.Fault, "web tools") }) {
				t.Fatalf("ends %+v: %v", ends, err)
			}
		})
	}
	// And the table may give none at all.
	was := hosts[adapter.ClaudeCode]
	t.Cleanup(func() { hosts[adapter.ClaudeCode] = was })
	e := was
	e.WebOff = func(func(...string) ([]byte, error)) ([]string, error) { return nil, nil }
	hosts[adapter.ClaudeCode] = e
	r := newRig(t, 1)
	if _, _, err := r.run(); err == nil || countMode(r.calls(), "start") != 0 {
		t.Fatalf("a run with no web tool off started a chat: %v", err)
	}
}

// A Codex whose features cannot be listed starts no chat: the run stops on
// an error, its record ended error.
func TestUnlistedFeaturesStopTheRun(t *testing.T) {
	r := newRig(t, 1)
	r.opts.Host, r.opts.Trials = string(adapter.Codex), 2
	r.script.NoFeatures = true
	s, out, err := r.run()
	if err == nil || !strings.Contains(err.Error(), "web tools cannot be turned off") || s.Judged != 0 {
		t.Fatalf("run: %+v %v\n%s", s, err, out)
	}
	if calls := r.calls(); countMode(calls, "start") != 0 || countMode(calls, "tools") != 1 {
		t.Fatalf("calls: %+v", calls)
	}
	records, err := LoadRuns(r.opts.Out)
	if err != nil || len(records) != 1 || records[0].Ended != EndedError {
		t.Fatalf("records %+v: %v", records, err)
	}
	emptyWork(t, r)
}

// A call whose output names a web tool's call, in any shape the reader
// knows, leaves its trial unjudged, web_used, writes no row, and stops the
// run as hidden_found does: the rest not run, the summary printed, the
// record ended web_used, and Run errs.
func TestWebUseStopsTheRun(t *testing.T) {
	for _, h := range adapter.All() {
		for name, c := range map[string]struct {
			set    func(r *rig)
			judged int
			starts int
			line   string
		}{
			"a search":    {func(r *rig) { r.script.Web = "tool" }, 0, 1, "trial hidden-banner.t1: unjudged, web_used\n"},
			"a fetch":     {func(r *rig) { r.script.Web = "fetch" }, 0, 1, "trial hidden-banner.t1: unjudged, web_used\n"},
			"the API's":   {func(r *rig) { r.script.Web = "server" }, 0, 1, "trial hidden-banner.t1: unjudged, web_used\n"},
			"a count":     {func(r *rig) { r.script.Web = "count" }, 0, 1, "trial hidden-banner.t1: unjudged, web_used\n"},
			"a model's":   {func(r *rig) { r.script.Web = "model_count" }, 0, 1, "trial hidden-banner.t1: unjudged, web_used\n"},
			"no usage":    {func(r *rig) { r.script.Web, r.script.NoUsage = "tool", true }, 0, 1, "trial hidden-banner.t1: unjudged, web_used\n"},
			"in round 2":  {func(r *rig) { r.script.Web, r.script.From = "tool", 2 }, 0, 1, "trial hidden-banner.t1: unjudged, web_used\n"},
			"a later one": {func(r *rig) { r.script.Web, r.script.Session = "tool", 2 }, 1, 2, "trial hidden-banner.t2: unjudged, web_used\n"},
		} {
			t.Run(string(h.ID)+"/"+name, func(t *testing.T) {
				r := newRig(t, 1)
				r.opts.Host, r.opts.Trials = string(h.ID), 3
				c.set(r)
				s, out, err := r.run()
				var stopped *Stopped
				if !errors.As(err, &stopped) || stopped.Code != CodeWebUsed {
					t.Fatalf("run: %v\n%s", err, out)
				}
				if s.Judged != c.judged || s.Unjudged[CodeWebUsed] != 1 || len(s.Unjudged) != 1 || s.NotRun != 2-c.judged {
					t.Fatalf("summary %+v\n%s", s, out)
				}
				if !strings.Contains(out, c.line) || !strings.Contains(out, ", web_used 1") {
					t.Fatalf("output:\n%s", out)
				}
				if got := len(r.rows()); got != 9*c.judged {
					t.Fatalf("%d rows", got)
				}
				if starts := countMode(r.calls(), "start"); starts != c.starts {
					t.Fatalf("%d sessions started", starts)
				}
				records, err := LoadRuns(r.opts.Out)
				if err != nil || len(records) != 1 || records[0].Ended != CodeWebUsed || records[0].Unjudged[CodeWebUsed] != 1 ||
					records[0].Judged != c.judged || records[0].NotRun != 2-c.judged {
					t.Fatalf("records %+v: %v", records, err)
				}
				emptyWork(t, r)
			})
		}
	}
}

// A call that named a web tool's call before the session's limit ended it
// is web_used, not timeout: the run stops. The limit ends at an exact
// point, once the fake's child that holds the output runs.
func TestWebUseThenTheLimitStopsTheRun(t *testing.T) {
	for _, h := range adapter.All() {
		t.Run(string(h.ID), func(t *testing.T) {
			r := newRig(t, 1)
			r.opts.Host, r.opts.Trials = string(h.ID), 2
			ready := filepath.Join(r.base, "ready")
			if err := syscall.Mkfifo(ready, 0o600); err != nil {
				t.Fatal(err)
			}
			r.script.StallAfter, r.script.Ready, r.script.Web = true, ready, "tool"
			r.opts.sessionLimit = func(ctx context.Context, d time.Duration) (context.Context, context.CancelFunc) {
				c, cancel := context.WithCancel(ctx)
				go func() {
					// Opening blocks until the child opens the pipe to write.
					if f, err := os.Open(ready); err == nil {
						_, _ = io.ReadAll(f)
						_ = f.Close()
					}
					cancel()
				}()
				return c, cancel
			}
			res := guarded(t, r)
			var stopped *Stopped
			if !errors.As(res.err, &stopped) || stopped.Code != CodeWebUsed || res.s.Unjudged[CodeWebUsed] != 1 || res.s.Unjudged[CodeTimeout] != 0 ||
				res.s.NotRun != 1 || res.s.Tokens != 1050 {
				t.Fatalf("run: %+v %v\n%s", res.s, res.err, res.out)
			}
		})
	}
}

// Calls of other tools go on: a chat that edits and runs commands is
// judged.
func TestOtherToolCallsGoOn(t *testing.T) {
	for _, h := range adapter.All() {
		t.Run(string(h.ID), func(t *testing.T) {
			r := newRig(t, 1)
			r.opts.Host, r.opts.Trials = string(h.ID), 2
			r.script.Events = 5
			if s, out, err := r.run(); err != nil || s.Judged != 2 || s.Unjudged[CodeWebUsed] != 0 {
				t.Fatalf("run: %+v %v\n%s", s, err, out)
			}
		})
	}
}

// The readers find a web tool's call in each shape they know, and no other
// tool, nor a count of 0, is one.
func TestReadersFindWebCalls(t *testing.T) {
	for name, c := range map[string]struct {
		out string
		web bool
	}{
		"WebSearch":           {`{"type":"assistant","message":{"content":[{"type":"tool_use","name":"WebSearch"}]}}`, true},
		"WebFetch":            {`{"type":"assistant","message":{"content":[{"type":"text"},{"type":"tool_use","name":"WebFetch"}]}}`, true},
		"server web_search":   {`{"type":"assistant","message":{"content":[{"type":"server_tool_use","name":"web_search"}]}}`, true},
		"server web_fetch":    {`{"type":"assistant","message":{"content":[{"type":"server_tool_use","name":"web_fetch"}]}}`, true},
		"usage's count":       {`{"type":"result","usage":{"input_tokens":1,"server_tool_use":{"web_search_requests":0,"web_fetch_requests":2}}}`, true},
		"modelUsage's count":  {`{"type":"result","modelUsage":{"m":{"inputTokens":1,"webSearchRequests":1}}}`, true},
		"other tools":         {`{"type":"assistant","message":{"content":[{"type":"tool_use","name":"Bash"},{"type":"tool_use","name":"Edit"}]}}`, false},
		"counts of 0":         {`{"type":"result","usage":{"input_tokens":1,"server_tool_use":{"web_search_requests":0,"web_fetch_requests":0}},"modelUsage":{"m":{"webSearchRequests":0,"webFetchRequests":0}}}`, false},
		"the opening's tools": {`{"type":"system","subtype":"init","tools":["Bash","WebSearch"]}`, false},
	} {
		if r := readStreamJSON([]byte(c.out + "\n")); r.Web != c.web {
			t.Errorf("Claude Code, %s: %+v", name, r)
		}
	}
	for name, c := range map[string]struct {
		out string
		web bool
	}{
		"completed":   {`{"type":"item.completed","item":{"type":"web_search","query":"made up"}}`, true},
		"started":     {`{"type":"item.started","item":{"type":"web_search"}}`, true},
		"other items": {`{"type":"item.completed","item":{"type":"command_execution"}}` + "\n" + `{"type":"item.completed","item":{"type":"file_change"}}`, false},
	} {
		r := readExecJSON([]byte(c.out + "\n"))
		if r.Web != c.web {
			t.Errorf("Codex, %s: %+v", name, r)
		}
	}
	// A started item is no tool call of its own: its completion is.
	if r := readExecJSON([]byte(`{"type":"item.started","item":{"type":"command_execution"}}` + "\n" + `{"type":"item.completed","item":{"type":"command_execution"}}` + "\n")); r.AgentEvents != 1 {
		t.Fatalf("agent events: %+v", r)
	}
}

// LoadRuns takes a run that web_used stopped, and reads a record written
// before the code was on the list as counting it 0; the writer still
// counts every code.
func TestLoadRunsTakesWebUsed(t *testing.T) {
	good := Record{V: 1, Run: "20261113t090001z", Instrument: "continuation", Host: "codex", HostVersions: []string{"1.2.3"}, Condition: "off",
		Commit: strings.Repeat("ab", 20), Tasks: []string{"made-up-task"}, Trials: 1, Planned: 1, Unjudged: zeros(), Ended: CodeWebUsed}
	good.Unjudged[CodeWebUsed] = 1
	data, err := json.Marshal(good)
	if err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	write(t, filepath.Join(dir, RunsFile), string(data)+"\n")
	if got, err := LoadRuns(dir); err != nil || len(got) != 1 || !reflect.DeepEqual(got[0], good) {
		t.Fatalf("a run web_used stopped: %+v %v", got, err)
	}
	older := good
	older.Ended, older.Judged = EndedDone, 1
	older.Unjudged = zeros()
	delete(older.Unjudged, CodeWebUsed)
	if data, err = json.Marshal(older); err != nil {
		t.Fatal(err)
	}
	write(t, filepath.Join(dir, RunsFile), string(data)+"\n")
	got, err := LoadRuns(dir)
	if err != nil || len(got) != 1 || !reflect.DeepEqual(got[0].Unjudged, zeros()) {
		t.Fatalf("an older record: %+v %v", got, err)
	}
	if err := appendRecord(t.TempDir(), older, nil); err == nil {
		t.Fatal("a record that does not count web_used was appended")
	}
	if !slices.Contains(Codes, CodeWebUsed) || !slices.Contains(stopCodes, CodeWebUsed) || !slices.Contains(Ends, CodeWebUsed) {
		t.Fatal("web_used is not on the lists")
	}
}
