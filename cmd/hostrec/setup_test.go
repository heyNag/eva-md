package main

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/heyNag/eva-md/internal/hostfacts"
)

func readDoc(t *testing.T, rel string) string {
	t.Helper()
	data, err := os.ReadFile(filepath.Join("..", "..", filepath.FromSlash(rel)))
	if err != nil {
		t.Fatal(err)
	}
	return string(data)
}

// factEvents gives the events a fact of the sheet lists in its sentence,
// "... can declare A, B, ... and Z ...".
func factEvents(t *testing.T, id string) []string {
	t.Helper()
	sheet := readDoc(t, "docs/v2/host-facts.md")
	_, rest, ok := strings.Cut(sheet, "\n#### "+id+". ")
	if !ok {
		t.Fatalf("the sheet has no %s", id)
	}
	_, fact, _ := strings.Cut(rest, "- **Fact.** ")
	fact, _, _ = strings.Cut(fact, "\n")
	var out []string
	for _, m := range regexp.MustCompile(`\b(Session|User|Pre|Post|Subagent|Stop|Interrupt)[A-Za-z]*\b`).FindAllString(fact, -1) {
		if !slices.Contains(out, m) {
			out = append(out, m)
		}
	}
	return out
}

type hooksFile struct {
	Hooks map[string][]struct {
		Matcher string           `json:"matcher"`
		Hooks   []map[string]any `json:"hooks"`
	} `json:"hooks"`
}

func readHooks(t *testing.T, path string) hooksFile {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var h hooksFile
	if err := json.Unmarshal(data, &h); err != nil {
		t.Fatal(err)
	}
	return h
}

// commandPath gives the program a hook's command starts: its first word,
// unquoted.
func commandPath(cmd string) string {
	if strings.HasPrefix(cmd, "'") {
		end := strings.Index(cmd[1:], "'")
		return cmd[1 : end+1]
	}
	word, _, _ := strings.Cut(cmd, " ")
	return word
}

func TestSetupWritesThePluginsAndFolders(t *testing.T) {
	h := newHome(t)
	out := h.must(t, "", "setup", "--rec", h.rec)
	for _, want := range []string{"found no eva-md install and no earlier session", "claude plugin marketplace add " + filepath.Join(h.home, "hf/marketplace/claude-code") + " --json </dev/null",
		"codex plugin add hf-hooks@hf-scratch --json </dev/null"} {
		if !strings.Contains(out, want) {
			t.Errorf("setup does not say %q:\n%s", want, out)
		}
	}
	market := filepath.Join(h.home, "hf", "marketplace")
	tools := filepath.Join(h.home, "hf-tools")
	roots := pluginRoots(market)

	claude := readHooks(t, filepath.Join(roots[0], "hooks", "hooks.json"))
	codex := readHooks(t, filepath.Join(roots[1], "hooks", "hooks.json"))
	for _, c := range []struct {
		fact  string
		hooks hooksFile
		root  string
		host  string
	}{{"F-075", claude, roots[0], "claude-code"}, {"F-120", codex, roots[1], "codex"}} {
		want := factEvents(t, c.fact)
		if len(want) < 10 {
			t.Fatalf("%s lists %v", c.fact, want)
		}
		var got []string
		for ev, groups := range c.hooks.Hooks {
			got = append(got, ev)
			plain := false
			for _, g := range groups {
				for _, hk := range g.Hooks {
					cmd, _ := hk["command"].(string)
					if g.Matcher == "" && cmd == hookCommand(filepath.Join(tools, "hostrec"), c.host, ev, "") {
						plain = true
					}
					if cmd == "${CLAUDE_PLUGIN_ROOT}/scripts/hf-exec-check" {
						if c.host != "claude-code" || ev != "UserPromptSubmit" {
							t.Errorf("an exec-form hook on %s %s", c.host, ev)
						}
						continue
					}
					p := commandPath(cmd)
					if !filepath.IsAbs(p) || filepath.Dir(p) != tools {
						t.Errorf("%s %s: the command %q is not an absolute path in %s", c.host, ev, cmd, tools)
					}
					for _, r := range roots {
						if within(r, p) {
							t.Errorf("%s %s: the command %q is inside a plugin root", c.host, ev, cmd)
						}
					}
				}
			}
			if !plain {
				t.Errorf("%s %s has no recorder hook with no matcher", c.host, ev)
			}
		}
		slices.Sort(got)
		slices.Sort(want)
		if !slices.Equal(got, want) {
			t.Errorf("%s's plugin declares %v; %s lists %v", c.host, got, c.fact, want)
		}
	}
	// The hooks beside the recorder's: the exec form, the missing binary and
	// the two-second limit on Claude Code; the matchers, the start handler
	// with no limit and the missing binary on Codex.
	vias := func(groups []struct {
		Matcher string           `json:"matcher"`
		Hooks   []map[string]any `json:"hooks"`
	}) map[string]map[string]any {
		out := map[string]map[string]any{}
		for _, g := range groups {
			for _, hk := range g.Hooks {
				cmd := hk["command"].(string)
				_, via, _ := strings.Cut(cmd, "--via ")
				if strings.Contains(cmd, "hf-exec-check") {
					via = "exec-check"
				}
				hk["matcher"] = g.Matcher
				out[via] = hk
			}
		}
		return out
	}
	cu := vias(claude.Hooks["UserPromptSubmit"])
	if cu["timeout-check"]["timeout"] != 2.0 || cu["exec-check"]["args"] == nil || !strings.Contains(cu["missing-check"]["command"].(string), filepath.Join(tools, "hf-missing-check")) {
		t.Errorf("Claude Code's prompt hooks: %v", cu)
	}
	cs := vias(codex.Hooks["SessionStart"])
	if cs[""]["additionalContextLimit"] != 3000.0 || cs["plain-start"] == nil || cs["plain-start"]["additionalContextLimit"] != nil {
		t.Errorf("Codex's start handlers: %v", cs)
	}
	cp := vias(codex.Hooks["PreToolUse"])
	if cp["shell-matcher"]["matcher"] != "^Bash$" || cp["edit-matcher"]["matcher"] != "Edit|Write" || cp[""]["matcher"] != "" {
		t.Errorf("Codex's pre-tool hooks: %v", cp)
	}
	if !strings.Contains(vias(codex.Hooks["UserPromptSubmit"])["missing-check"]["command"].(string), "hf-missing-check") {
		t.Error("Codex has no hook naming the missing binary")
	}
	for _, rel := range []string{
		"claude-code/.claude-plugin/marketplace.json", "claude-code/plugins/hf-recorder/.claude-plugin/plugin.json",
		"claude-code/plugins/hf-recorder/commands/hf-pause-check.md", "claude-code/plugins/hf-recorder/agents/hf-gardener.md",
		"claude-code/plugins/hf-recorder/skills/hf-orchard-skill/SKILL.md", "codex/.agents/plugins/marketplace.json",
		"codex/plugins/hf-hooks/.codex-plugin/plugin.json", "codex/plugins/hf-hooks/skills/hf-inside-skill/SKILL.md",
	} {
		data, err := os.ReadFile(filepath.Join(market, filepath.FromSlash(rel)))
		if err != nil {
			t.Error(err)
			continue
		}
		if strings.HasSuffix(rel, "marketplace.json") && !strings.Contains(string(data), `"name": "hf-scratch"`) {
			t.Errorf("%s is not the hf-scratch marketplace", rel)
		}
	}
	if info, err := os.Stat(filepath.Join(roots[0], "scripts", "hf-exec-check")); err != nil || info.Mode()&0o111 == 0 {
		t.Errorf("the exec-form script is not executable: %v", err)
	}
	// The scratch folders, exactly.
	for rel, want := range scratchFiles {
		data, err := os.ReadFile(filepath.Join(h.home, filepath.FromSlash(rel)))
		if err != nil || string(data) != want {
			t.Errorf("%s: %v %q", rel, err, data)
		}
	}
	if n := len(scratchFiles["scratch/long-notes/AGENTS.md"]); n < 68_000 || n > 72_000 {
		t.Errorf("the long notes are %d bytes", n)
	}
	if entries, err := os.ReadDir(filepath.Join(h.home, "scratch", "outside")); err != nil || len(entries) != 0 {
		t.Errorf("the outside folder: %v %v", entries, err)
	}
	for _, repo := range scratchRepos {
		dir := filepath.Join(h.home, filepath.FromSlash(repo))
		out, err := gitIn(dir, "log", "--format=%an <%ae>|%cn <%ce>")
		if err != nil || strings.TrimSpace(string(out)) != "HF Scratch <scratch@example.invalid>|HF Scratch <scratch@example.invalid>" {
			t.Errorf("%s's history: %q %v", repo, out, err)
		}
		for k, want := range map[string]string{"user.name": "HF Scratch", "user.email": "scratch@example.invalid"} {
			if out, err := gitIn(dir, "config", "--local", k); err != nil || strings.TrimSpace(string(out)) != want {
				t.Errorf("%s's %s: %q", repo, k, out)
			}
		}
		if out, _ := gitIn(dir, "status", "--porcelain"); len(out) != 0 {
			t.Errorf("%s has changes left: %s", repo, out)
		}
	}
	if info, err := os.Stat(filepath.Join(tools, "hf-missing-check")); err != nil || info.Mode()&0o111 == 0 {
		t.Errorf("hf-missing-check: %v", err)
	}
	if target, err := os.Readlink(filepath.Join(tools, "hf-linked-name")); err != nil || target != filepath.Join(tools, "hostrec") {
		t.Errorf("hf-linked-name: %q %v", target, err)
	}
	private := filepath.Join(h.home, "hf-private")
	if info, err := os.Stat(private); err != nil || info.Mode().Perm() != 0o700 {
		t.Errorf("the private folder: %v", err)
	}
	var names hostfacts.Names
	data, err := os.ReadFile(filepath.Join(private, namesFile))
	if err != nil || json.Unmarshal(data, &names) != nil || names.Eval.Short != fakeEval.Short || names.Eval.Home != h.home || len(names.Others) != 1 || len(names.Computer) != 2 {
		t.Errorf("the names: %+v %v", names, err)
	}
	if _, err := os.Stat(filepath.Join(h.home, markerName)); err != nil {
		t.Error("no setup marker")
	}
	if code, _, errOut := h.run(t, "", "setup", "--rec", h.rec); code == 0 || !strings.Contains(errOut, "set up already") {
		t.Errorf("a second setup: %d %s", code, errOut)
	}
}

// treeHashes hashes every file and link under the folders.
func treeHashes(t *testing.T, dirs ...string) map[string]string {
	t.Helper()
	out := map[string]string{}
	for _, dir := range dirs {
		err := filepath.WalkDir(dir, func(p string, d os.DirEntry, err error) error {
			if err != nil || d.IsDir() {
				return err
			}
			var data []byte
			if d.Type()&os.ModeSymlink != 0 {
				target, err := os.Readlink(p)
				if err != nil {
					return err
				}
				data = []byte("link:" + target)
			} else if data, err = os.ReadFile(p); err != nil {
				return err
			}
			info, err := d.Info()
			if err != nil {
				return err
			}
			sum := sha256.Sum256(data)
			out[p] = hex.EncodeToString(sum[:]) + info.Mode().String()
			return nil
		})
		if err != nil {
			t.Fatal(err)
		}
	}
	return out
}

// No arm changes a plugin's bytes, under its root or in a host's copy;
// every arm but disarm and the start-text probes answers a new chat's
// start with the start word.
func TestArmsLeavePluginBytesAndGiveTheStartWord(t *testing.T) {
	h := newHome(t)
	h.setup(t)
	market := filepath.Join(h.home, "hf")
	// The hosts' copies of the plugins, as they keep them.
	copyTree(t, filepath.Join(market, "marketplace", "claude-code", "plugins", claudePlugin), filepath.Join(h.home, ".claude", "plugins", "cache", marketplace, claudePlugin, "1"))
	copyTree(t, filepath.Join(market, "marketplace", "codex", "plugins", codexPlugin), filepath.Join(h.home, ".codex", "plugins", "cache", marketplace, codexPlugin, "1.0.0"))
	watched := []string{market, filepath.Join(h.home, ".claude", "plugins"), filepath.Join(h.home, ".codex", "plugins")}
	before := treeHashes(t, watched...)
	h.must(t, "", "mark", "C-01")
	start := map[string]any{"session_id": "made-up-session", "source": "startup", "cwd": filepath.Join(h.home, "scratch", "alpha-orchard"), "hook_event_name": "SessionStart"}
	// Part 2's arms are refused, and leave the arm in force as it was.
	armed := false
	for _, a := range [][]string{{"arm", "start"}, {"disarm"}, {"arm", "start"}, {"arm", "pretool-word"}, {"disarm"}, {"arm", "start-9900"}, {"arm", "plugin-edit"}} {
		code, _, _ := h.run(t, "", a...)
		if after := treeHashes(t, watched...); !mapsEqual(before, after) {
			t.Fatalf("%v changed a plugin's bytes", a)
		}
		if (code == 0) != (a[0] == "disarm" || a[1] == "start") {
			t.Errorf("%v exited %d", a, code)
		}
		if code == 0 {
			armed = a[0] == "arm"
		}
		for _, host := range []string{"claude-code", "codex"} {
			answer := h.hook(t, host, "SessionStart", start)
			if armed != strings.Contains(answer, startWord) {
				t.Errorf("%v: %s's start answered %s", a, host, answer)
			}
			if armed {
				var v map[string]any
				if json.Unmarshal([]byte(answer), &v) != nil || !strings.HasPrefix(v["systemMessage"].(string), "hf test") ||
					v["hookSpecificOutput"].(map[string]any)["hookEventName"] != "SessionStart" {
					t.Errorf("the start answer: %s", answer)
				}
			}
		}
		if code != 0 && a[0] == "arm" && a[1] == "start" {
			t.Fatalf("arm start failed")
		}
	}
	h.must(t, "", "arm", "start")
	for _, c := range []struct {
		source, via string
	}{{"resume", ""}, {"clear", ""}, {"compact", ""}, {"startup", "plain-start"}, {"startup", "exec-check"}} {
		p := map[string]any{"session_id": "made-up-session", "source": c.source}
		var answer string
		if c.via != "" {
			answer = h.hook(t, "codex", "SessionStart", p, c.via)
		} else {
			answer = h.hook(t, "claude-code", "SessionStart", p)
		}
		if want := c.via == "" && c.source != "compact"; want != strings.Contains(answer, startWord) {
			t.Errorf("source %s via %q: %s", c.source, c.via, answer)
		}
	}
	if answer := h.hook(t, "claude-code", "UserPromptSubmit", map[string]any{"prompt": "made up"}); answer != "{}" {
		t.Errorf("a prompt hook answered %s", answer)
	}
}

func mapsEqual(a, b map[string]string) bool {
	if len(a) != len(b) {
		return false
	}
	for k, v := range a {
		if b[k] != v {
			return false
		}
	}
	return true
}

var (
	scratchMention = regexp.MustCompile(`\b(?:orchard|lantern)/[a-z<>-]+\.txt\b|~/scratch/[a-z-]+(?:/[a-z/]+\.(?:txt|md))?|~/hf-tools/hf-[a-z-]+`)
	// What a step asks an agent to make, or names to be missing: these
	// files are not setup's.
	madeByAStep = regexp.MustCompile(`(?i)(creat|write the|writes|delet|no-such|plan how)`)
)

// Every scratch file the run-book names is there as setup writes it, but
// those a step asks an agent to make; and each line a step asks an agent to
// find or change is there when the step runs, whichever Part 2 groups ran
// before it.
func TestRunBookScratchFilesAreThere(t *testing.T) {
	h := newHome(t)
	h.setup(t)
	book := readDoc(t, "docs/v2/host-sitting-runbook.md")
	// The files a step asks an agent to make, or names as missing; and
	// orchard/quince.txt, which C-10 makes from the option the owner picks.
	made := map[string]bool{"orchard/quince.txt": true}
	for _, line := range strings.Split(book, "\n") {
		if madeByAStep.MatchString(line) {
			for _, m := range scratchMention.FindAllString(line, -1) {
				if strings.HasSuffix(m, ".txt") && !strings.HasPrefix(m, "~/") {
					made[m] = true
				}
			}
		}
	}
	for _, keep := range []string{"orchard/plums.txt", "orchard/pears.txt", "lantern/wicks.txt", "lantern/oil.txt"} {
		delete(made, keep)
	}
	checked := 0
	for _, line := range strings.Split(book, "\n") {
		for _, loc := range scratchMention.FindAllStringIndex(line, -1) {
			m := line[loc[0]:loc[1]]
			if strings.Contains(m, "<") {
				continue
			}
			if made[m] {
				continue
			}
			p := strings.TrimPrefix(m, "~/")
			if strings.HasPrefix(m, "orchard/") {
				p = "scratch/alpha-orchard/" + m
			}
			if strings.HasPrefix(m, "lantern/") {
				p = "scratch/beta-lantern/" + m
			}
			if _, err := os.Lstat(filepath.Join(h.home, filepath.FromSlash(p))); err != nil {
				t.Errorf("the run-book names %s, which setup did not make: %q", m, strings.TrimSpace(line))
			}
			checked++
		}
	}
	if checked < 20 {
		t.Fatalf("only %d mentions checked", checked)
	}
	// What steps ask an agent to find, and what Part 2's steps change.
	needs := []struct{ file, line string }{
		{"scratch/alpha-orchard/orchard/plums.txt", "count: "},
		{"scratch/alpha-orchard/orchard/pears.txt", "made-up pear list"},
		{"scratch/beta-lantern/lantern/wicks.txt", "amber"},
		{"scratch/alpha-orchard/CLAUDE.md", "WILLOW-CANDLE"},
	}
	edits := []struct{ file, from, to string }{
		{"scratch/alpha-orchard/orchard/plums.txt", "count: 12", "count: 14"},
		{"scratch/alpha-orchard/orchard/plums.txt", "count: 1", "count: 13"},
		{"scratch/alpha-orchard/orchard/pears.txt", "note: none of this is real\n", "note: none of this is real\nchecked: yes\n"},
		{"scratch/alpha-orchard/orchard/pears.txt", "note: none of this is real\n", "note: none of this is real\nchecked: again\n"},
	}
	for _, e := range edits {
		if !strings.Contains(book, strings.TrimPrefix(strings.TrimSpace(e.to[strings.LastIndex(strings.TrimSpace(e.to), "\n")+1:]), "note: none of this is real")) {
			t.Errorf("the run-book no longer asks for %q", e.to)
		}
	}
	check := func(when string) {
		for _, n := range needs {
			data, err := os.ReadFile(filepath.Join(h.home, filepath.FromSlash(n.file)))
			if err != nil || !strings.Contains(string(data), n.line) {
				t.Errorf("%s: %s lacks %q", when, n.file, n.line)
			}
		}
	}
	check("after setup")
	for i, e := range edits {
		p := filepath.Join(h.home, filepath.FromSlash(e.file))
		data, err := os.ReadFile(p)
		if err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte(strings.Replace(string(data), e.from, e.to, 1)), 0o644); err != nil {
			t.Fatal(err)
		}
		check("after edit " + string(rune('1'+i)))
	}
}

// setup refuses a home with eva-md in it, or a host's session from before
// the account was made; every other command but scan, scrub, fixtures and
// check refuses a home with no setup marker, and the hook answers {} and
// writes nothing there.
func TestTheRecorderRunsOnlyInAnEvaluationHome(t *testing.T) {
	for _, c := range []struct {
		name  string
		plant func(h *testHome) error
		want  string
	}{
		{"an install record", func(h *testHome) error { return writeFile(h.home, ".local/share/eva-md/install-record.json", "{}") }, "eva-md install or config"},
		{"a config", func(h *testHome) error { return writeFile(h.home, ".config/eva-md/config.toml", "") }, "eva-md install or config"},
		{"an older Claude Code session", func(h *testHome) error {
			p := filepath.Join(h.home, ".claude", "projects", "-made-up", "0f1e2d3c-4b5a-4968-8776-655443322110.jsonl")
			if err := writeFile(h.home, ".claude/projects/-made-up/0f1e2d3c-4b5a-4968-8776-655443322110.jsonl", "{}\n"); err != nil {
				return err
			}
			old := time.Date(2001, 2, 3, 4, 5, 6, 0, time.UTC)
			return os.Chtimes(p, old, old)
		}, "session from before"},
		{"an older Codex session", func(h *testHome) error {
			p := filepath.Join(h.home, ".codex", "sessions", "2001", "02", "03", "rollout-made-up.jsonl")
			if err := writeFile(h.home, ".codex/sessions/2001/02/03/rollout-made-up.jsonl", "{}\n"); err != nil {
				return err
			}
			old := time.Date(2001, 2, 3, 4, 5, 6, 0, time.UTC)
			return os.Chtimes(p, old, old)
		}, "session from before"},
	} {
		t.Run(c.name, func(t *testing.T) {
			h := newHome(t)
			if err := c.plant(h); err != nil {
				t.Fatal(err)
			}
			code, _, errOut := h.run(t, "", "setup", "--rec", h.rec)
			if code == 0 || !strings.Contains(errOut, c.want) {
				t.Errorf("setup: %d %q", code, errOut)
			}
			for _, p := range []string{h.rec, filepath.Join(h.home, "hf-private"), filepath.Join(h.home, markerName), filepath.Join(h.home, "scratch")} {
				if _, err := os.Lstat(p); !errors.Is(err, os.ErrNotExist) {
					t.Errorf("setup refused and still made %s", p)
				}
			}
		})
	}
	h := newHome(t)
	for _, args := range [][]string{{"mark", "C-00"}, {"close"}, {"note", "made up"}, {"tail"}, {"arm", "start"}, {"disarm"}, {"trust"}, {"register"},
		{"unregister", "all"}, {"key"}, {"remote", "set"}, {"push"}, {"summary", "sitting-1"}, {"voice-check"}, {"pack", h.rec}, {"teardown"}} {
		if code, _, errOut := h.run(t, "", args...); code == 0 || !strings.Contains(errOut, "not set up by hostrec setup") {
			t.Errorf("%v with no marker: %d %q", args, code, errOut)
		}
	}
	if answer := h.hook(t, "claude-code", "SessionStart", map[string]any{"source": "startup"}); answer != "{}" {
		t.Errorf("the hook with no marker: %s", answer)
	}
	if entries, _ := os.ReadDir(h.home); len(entries) != 0 {
		t.Errorf("the hook wrote in a home with no marker: %v", entries)
	}
	// A home set up, in which eva-md is installed later, is refused too.
	h.setup(t)
	if err := writeFile(h.home, ".config/eva-md/config.env", ""); err != nil {
		t.Fatal(err)
	}
	if code, _, errOut := h.run(t, "", "mark", "C-00"); code == 0 || !strings.Contains(errOut, "eva-md install or config") {
		t.Errorf("mark with eva-md installed: %d %q", code, errOut)
	}
}

func writeFile(home, rel, text string) error {
	p := filepath.Join(home, filepath.FromSlash(rel))
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		return err
	}
	return os.WriteFile(p, []byte(text), 0o644)
}

func execCommand(name string, args ...string) *exec.Cmd { return exec.Command(name, args...) }

func exitCode(err error) int {
	var e *exec.ExitError
	if errors.As(err, &e) {
		return e.ExitCode()
	}
	return -1
}
