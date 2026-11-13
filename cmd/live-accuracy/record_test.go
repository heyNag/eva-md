package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/heyNag/eva-md/internal/update"
	"github.com/heyNag/eva-md/internal/update/accuracy"
)

// added is an answer that adds a next step citing the batch's first entry,
// with a summary whose text JSON would escape as HTML; filed is a run of
// whitespace the encoder drops.
const added = `{ "summary": "Work goes on <here> & there", "last_chat": "", "done": [],
	"items": [{"id": null, "kind": "next", "action": "add", "text": "Check the first entry again", "tag": "stated", "refs": [1], "reason": null, "replacement": null}],
	"timeline": [], "unknowns": [], "rules": [], "knowledge": [] }`

// mixed answers request n with one of the ways a pass ends: an answer that
// adds an item, one that changes nothing, one the validator refuses, each
// at some effort or none, and the two refusals the run goes on after.
func mixed(n int) (update.Result, error) {
	switch {
	case n%7 == 0:
		return update.Result{Reason: update.ReasonToolUsed, Effort: "max"}, nil
	case n%5 == 0:
		return update.Result{Reason: update.ReasonInvalidOutput}, nil
	case n == 11:
		return update.Result{Output: []byte(`{"summary":1}`), Effort: "high"}, nil
	case n%3 == 0:
		return update.Result{Output: []byte(empty), Effort: "high"}, nil
	case n%4 == 0:
		return update.Result{Output: []byte(added), Degraded: "an optional MCP server failed"}, nil
	}
	return update.Result{Output: []byte(added), Effort: "max"}, nil
}

// readLines decodes a recording's lines.
func readLines(t *testing.T, path string) []passRecord {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var lines []passRecord
	for _, l := range strings.Split(strings.TrimSuffix(string(data), "\n"), "\n") {
		r, err := decodeRecord([]byte(l))
		if err != nil {
			t.Fatalf("%q: %v", l, err)
		}
		lines = append(lines, r)
	}
	return lines
}

// pagesIn reads every file of a folder, by name.
func pagesIn(t *testing.T, dir string) map[string]string {
	t.Helper()
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	pages := map[string]string{}
	for _, e := range entries {
		data, err := os.ReadFile(filepath.Join(dir, e.Name()))
		if err != nil {
			t.Fatal(err)
		}
		pages[e.Name()] = string(data)
	}
	return pages
}

// A live run with --record writes a line for each of the suite's 21
// passes, in order, holding the host, the model asked for, the effort the
// answer came at, the answer or null and the runner's reason, and nothing
// of the machine; with --pages it writes the 12 pages. A replay of that
// recording calls nothing, prints the same report, exits as the run did
// and writes the same pages byte for byte.
func TestRecordAndReplay(t *testing.T) {
	tmp := isolate(t)
	dir := t.TempDir()
	recording := filepath.Join(dir, "answers.jsonl")
	livePages, replayPages := filepath.Join(dir, "live"), filepath.Join(dir, "replay")
	if err := os.Mkdir(replayPages, 0o700); err != nil {
		t.Fatal(err)
	}
	runner := &fakeRunner{t: t, tmp: tmp, answer: mixed}
	var out, errOut bytes.Buffer
	args := []string{"--host", "codex", "--effort", "high", "--model", "gpt-test", "--spend-model-quota", "--record", recording, "--pages", livePages}
	code := run(context.Background(), args, &out, &errOut, func(string) update.Runner { return runner })
	if code != 1 || len(runner.requests) != 21 {
		t.Fatalf("exit %d after %d calls\n%s\n%s", code, len(runner.requests), out.String(), errOut.String())
	}
	report := out.String()
	for _, want := range []string{"content, pass 2: the answer was refused: tool_used\n", "content, pass 1: the answer was refused: invalid_output\n",
		"content, pass 2: the answer was refused: invalid_output: update: output rejected: ", "- Check the first entry again [inferred]", "    Work goes on <here> & there\n",
		"  answers refused: 9 of 21\n  answers at effort high: 6 of 21\n  answers at effort max: 5 of 21\n  answers with no effort flag: 3 of 21\nThe run misses the target.\n"} {
		if !strings.Contains(report, want) {
			t.Errorf("the report lacks %q:\n%s", want, report)
		}
	}

	data, err := os.ReadFile(recording)
	if err != nil {
		t.Fatal(err)
	}
	for _, leak := range []string{tmp, os.Getenv("HOME"), dir, "/", "MCP", "tokens", `"time"`} {
		if bytes.Contains(data, []byte(leak)) {
			t.Errorf("the recording holds %q", leak)
		}
	}
	if !bytes.Contains(data, []byte(`"summary":"Work goes on <here> & there"`)) {
		t.Errorf("the answer is not kept as given, compacted:\n%s", data)
	}
	lines := readLines(t, recording)
	if len(lines) != 21 {
		t.Fatalf("%d lines", len(lines))
	}
	n := 0
	for i, s := range accuracy.Scripts() {
		for p := range s.Passes {
			l := lines[n]
			n++
			want, _ := mixed(n)
			output := string(l.Output)
			if want.Output == nil && output != "null" || want.Output != nil && output == "null" {
				t.Errorf("line %d: output %s", n, output)
			}
			if l.V != 1 || l.Host != "codex" || l.Model != "gpt-test" || l.Script != i+1 || l.Pass != p+1 || l.Effort != want.Effort || l.Reason != want.Reason {
				t.Errorf("line %d: %+v", n, l)
			}
		}
	}
	pages := pagesIn(t, livePages)
	if len(pages) != 12 {
		t.Fatalf("pages %v", pages)
	}
	// Each page is the one the run ended the script with, as the report
	// prints it for each script that missed something.
	printed := reportPages(t, report)
	if len(printed) < 6 {
		t.Fatalf("the report prints %d pages:\n%s", len(printed), report)
	}
	added := 0
	for n, want := range printed {
		got := pages[pageName(n)]
		var lines []string
		for _, l := range strings.Split(strings.TrimSpace(got), "\n") {
			lines = append(lines, strings.TrimRight(l, " "))
		}
		if strings.Join(lines, "\n") != want {
			t.Errorf("%s holds\n%s\nthe report's page is\n%s", pageName(n), got, want)
		}
		if strings.Contains(want, "- Check the first entry again [inferred]") {
			added++
		}
	}
	if added == 0 {
		t.Errorf("no page the report prints holds the item the answers added:\n%s", report)
	}

	var again, againErr bytes.Buffer
	args = []string{"--host", "codex", "--effort", "high", "--model", "gpt-test", "--replay", recording, "--pages", replayPages}
	if code := run(context.Background(), args, &again, &againErr, noRunner(t)); code != 1 || again.String() != report {
		t.Fatalf("replay exit %d\n%s\nwant\n%s\n%s", code, again.String(), report, againErr.String())
	}
	if replayed := pagesIn(t, replayPages); !mapsEqual(replayed, pages) {
		t.Errorf("the replay's pages differ")
	}
	// Without the run's --effort, the replay's report says the effort is
	// the recording's, and is otherwise the same.
	again.Reset()
	args = []string{"--host", "codex", "--model", "gpt-test", "--replay", recording}
	header := "The accuracy suite's 12 fact scripts on codex, gpt-test, effort "
	if code := run(context.Background(), args, &again, &againErr, noRunner(t)); code != 1 ||
		again.String() != strings.Replace(report, header+"high:\n", header+"as recorded:\n", 1) || !strings.HasPrefix(report, header+"high:\n") {
		t.Fatalf("replay with no --effort: exit %d\n%s", code, again.String())
	}
	if left := leftIn(t, tmp); len(left) != 0 {
		t.Errorf("the runs left %q", left)
	}
	if left := leftIn(t, os.Getenv("HOME")); len(left) != 0 {
		t.Errorf("the runs wrote %q in the home folder", left)
	}
}

// reportPages gives the page the report prints after each script's last
// pass, by the script's place in the suite, for the scripts that missed
// something, with the report's indent taken off.
func reportPages(t *testing.T, report string) map[int]string {
	t.Helper()
	pages := map[int]string{}
	script, in := 0, false
	var lines []string
	end := func() {
		if in {
			pages[script] = strings.TrimRight(strings.Join(lines, "\n"), "\n")
		}
		in, lines = false, nil
	}
	for _, l := range strings.Split(report, "\n") {
		switch {
		case strings.HasPrefix(l, "- "):
			end()
			script++
		case strings.HasPrefix(l, "On "):
			end()
		case l == "  The page after its last pass:":
			in = true
		case in:
			if l != "" && !strings.HasPrefix(l, "    ") {
				t.Fatalf("a page's line %q", l)
			}
			lines = append(lines, strings.TrimPrefix(l, "    "))
		}
	}
	end()
	return pages
}

func mapsEqual(a, b map[string]string) bool {
	if len(a) != len(b) {
		return false
	}
	for k, v := range a {
		if w, ok := b[k]; !ok || w != v {
			return false
		}
	}
	return true
}

// A replay counts the efforts of its answers, and without --effort says
// the effort is the recording's; it asks no runner, needs no
// --spend-model-quota, and writes nothing but its pages.
func TestReplayMeetsTheTarget(t *testing.T) {
	tmp := isolate(t)
	dir := t.TempDir()
	recording := filepath.Join(dir, "answers.jsonl")
	writeRecording(t, recording, func(r *passRecord) { r.Effort = "max" })
	var out, errOut bytes.Buffer
	code := run(context.Background(), []string{"--host", "claude-code", "--replay", recording}, &out, &errOut, noRunner(t))
	// Answers that change nothing miss every fact, so the run misses the
	// target, as TestRun's does.
	if code != 1 || !strings.HasPrefix(out.String(), "The accuracy suite's 12 fact scripts on claude-code, the host's default model, effort as recorded:\n") ||
		!strings.HasSuffix(out.String(), "  answers refused: 0 of 21\n  answers at effort max: 21 of 21\nThe run misses the target.\n") {
		t.Fatalf("exit %d\n%s\n%s", code, out.String(), errOut.String())
	}
	if left := leftIn(t, tmp); len(left) != 0 {
		t.Errorf("the replay left %q", left)
	}
	if left := leftIn(t, dir); !slices.Equal(left, []string{"answers.jsonl"}) {
		t.Errorf("the replay wrote %q", left)
	}
}

// writeRecording writes a recording of every pass of the suite on
// claude-code, the host's default model, each answering with nothing to
// change, after edit has changed its line.
func writeRecording(t *testing.T, path string, edit func(*passRecord)) {
	t.Helper()
	var buf bytes.Buffer
	for i, s := range accuracy.Scripts() {
		for p := range s.Passes {
			r := passRecord{V: 1, Host: "claude-code", Effort: "max", Script: i + 1, Pass: p + 1, Output: json.RawMessage(empty)}
			if edit != nil {
				edit(&r)
			}
			line, err := json.Marshal(r)
			if err != nil {
				t.Fatal(err)
			}
			buf.Write(append(line, '\n'))
		}
	}
	if err := os.WriteFile(path, buf.Bytes(), 0o600); err != nil {
		t.Fatal(err)
	}
}

// noNewline marks a recording's last line written with no newline after
// it.
const noNewline = "<no newline>"

// A recording that is not one of a whole run of the suite, on the host and
// model the flags name, in this format, is refused before anything runs:
// exit 2, nothing printed on stdout, no runner made, no pages folder made.
func TestReplayRefusals(t *testing.T) {
	type edit = func(lines []string) []string
	line := func(i int, f func(map[string]any)) edit {
		return func(lines []string) []string {
			var m map[string]any
			if err := json.Unmarshal([]byte(lines[i]), &m); err != nil {
				panic(err)
			}
			f(m)
			data, err := json.Marshal(m)
			if err != nil {
				panic(err)
			}
			lines[i] = string(data)
			return lines
		}
	}
	for _, tc := range []struct {
		name  string
		edit  edit
		flags []string
		want  string
	}{
		{"another host", nil, []string{"--host", "codex"}, "line 1 was recorded on another host than codex"},
		{"one line of another host", line(4, func(m map[string]any) { m["host"] = "codex" }), nil, "line 5 was recorded on another host than claude-code"},
		{"another model", nil, []string{"--model", "claude-test"}, "line 1 was answered by another model than the one --model names"},
		{"another version", line(0, func(m map[string]any) { m["v"] = 2 }), nil, "line 1 is of another version of the format than 1"},
		{"a pass lacking", func(l []string) []string { return slices.Delete(l, 7, 8) }, nil, "the recording lacks 1 pass of the suite, script 6, pass 1 first: the run that made it stopped part way"},
		{"a pass twice", func(l []string) []string { return append(l, l[3]) }, nil, "line 22 repeats script 3, pass 2"},
		{"a pass twice in place of another", func(l []string) []string { l[4] = l[3]; return l }, nil, "line 5 repeats script 3, pass 2"},
		{"a reason the update does not know", line(9, func(m map[string]any) { m["reason"] = "overloaded" }), nil, "line 10 has a reason the update does not know"},
		{"a script the suite lacks", line(20, func(m map[string]any) { m["script"] = 13 }), nil, "line 21 is of a pass the suite does not have"},
		{"a pass the script lacks", line(0, func(m map[string]any) { m["pass"] = 2 }), nil, "line 1 is of a pass the suite does not have"},
		{"pass 0", line(1, func(m map[string]any) { m["pass"] = 0 }), nil, "line 2 is of a pass the suite does not have"},
		{"an effort that is not one", line(2, func(m map[string]any) { m["effort"] = "max; rm -rf" }), nil, "line 3 has an effort that is not one"},
		{"a key added", line(0, func(m map[string]any) { m["tokens_in"] = 120 }), nil, "line 1 is not a pass of a recording"},
		{"a key missing", line(0, func(m map[string]any) { delete(m, "reason") }), nil, "line 1 is not a pass of a recording"},
		{"a key of another type", line(0, func(m map[string]any) { m["script"] = "1" }), nil, "line 1 is not a pass of a recording"},
		{"a null reason", line(5, func(m map[string]any) { m["reason"] = nil }), nil, "line 6 is not a pass of a recording"},
		{"a null effort", line(5, func(m map[string]any) { m["effort"] = nil }), nil, "line 6 is not a pass of a recording"},
		{"a null model", line(0, func(m map[string]any) { m["model"] = nil }), nil, "line 1 is not a pass of a recording"},
		{"a null host", line(0, func(m map[string]any) { m["host"] = nil }), nil, "line 1 is not a pass of a recording"},
		{"a null version", line(0, func(m map[string]any) { m["v"] = nil }), nil, "line 1 is not a pass of a recording"},
		{"a null script", line(0, func(m map[string]any) { m["script"] = nil }), nil, "line 1 is not a pass of a recording"},
		{"a null pass", line(0, func(m map[string]any) { m["pass"] = nil }), nil, "line 1 is not a pass of a recording"},
		{"a number for a string", line(0, func(m map[string]any) { m["effort"] = 3 }), nil, "line 1 is not a pass of a recording"},
		{"a key given twice", func(l []string) []string {
			l[3] = strings.Replace(l[3], `"reason":""`, `"reason":"auth","reason":""`, 1)
			return l
		}, nil, "line 4 is not a pass of a recording"},
		{"a key given twice with one value", func(l []string) []string {
			l[3] = strings.Replace(l[3], `"pass":2`, `"pass":2,"pass":2`, 1)
			return l
		}, nil, "line 4 is not a pass of a recording"},
		{"a key in another case", func(l []string) []string {
			l[3] = strings.Replace(l[3], `"reason":`, `"Reason":`, 1)
			return l
		}, nil, "line 4 is not a pass of a recording"},
		{"two objects on a line", func(l []string) []string { l[3] += "{}"; return l }, nil, "line 4 is not a pass of a recording"},
		{"a line that is not JSON", func(l []string) []string { l[6] = "not json"; return l }, nil, "line 7 is not a pass of a recording"},
		{"a blank line", func(l []string) []string { return slices.Insert(l, 2, "") }, nil, "line 3 is not a pass of a recording"},
		{"a cut last line", func(l []string) []string { l[20] = l[20][:30]; return l }, nil, "line 21 is not a pass of a recording"},
		{"no whole last line", func(l []string) []string { l[20] += noNewline; return l }, nil, "the recording does not end with a whole line"},
		{"no line", func([]string) []string { return nil }, nil, "the recording lacks 21 passes of the suite, script 1, pass 1 first"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			tmp := isolate(t)
			dir := t.TempDir()
			recording := filepath.Join(dir, "answers.jsonl")
			writeRecording(t, recording, nil)
			if tc.edit != nil {
				data, err := os.ReadFile(recording)
				if err != nil {
					t.Fatal(err)
				}
				lines := tc.edit(strings.Split(strings.TrimSuffix(string(data), "\n"), "\n"))
				text := strings.Join(lines, "\n") + "\n"
				if len(lines) == 0 {
					text = ""
				}
				text = strings.TrimSuffix(text, noNewline+"\n")
				if err := os.WriteFile(recording, []byte(text), 0o600); err != nil {
					t.Fatal(err)
				}
			}
			pages := filepath.Join(dir, "pages")
			args := append([]string{"--host", "claude-code", "--replay", recording, "--pages", pages}, tc.flags...)
			var out, errOut bytes.Buffer
			code := run(context.Background(), args, &out, &errOut, noRunner(t))
			want := "live-accuracy: the recording " + recording + " is refused: " + tc.want
			if code != 2 || out.Len() != 0 || !strings.HasPrefix(errOut.String(), want) || !strings.HasSuffix(errOut.String(), ". Nothing was called.\n") {
				t.Fatalf("exit %d, out %q, err %q, want %q", code, out.String(), errOut.String(), want)
			}
			if left := leftIn(t, dir); !slices.Equal(left, []string{"answers.jsonl"}) {
				t.Errorf("the refused replay left %q", left)
			}
			if left := leftIn(t, tmp); len(left) != 0 {
				t.Errorf("the refused replay left %q", left)
			}
		})
	}
}

// The recording written above, unedited, replays: the refusals refuse
// what their edits break, not the recording itself.
func TestTheRefusalsRecordingReplays(t *testing.T) {
	isolate(t)
	recording := filepath.Join(t.TempDir(), "answers.jsonl")
	writeRecording(t, recording, nil)
	var out, errOut bytes.Buffer
	if code := run(context.Background(), []string{"--host", "claude-code", "--replay", recording}, &out, &errOut, noRunner(t)); code != 1 ||
		!strings.Contains(out.String(), "  answers at effort max: 21 of 21\n") {
		t.Fatalf("exit %d\n%s\n%s", code, out.String(), errOut.String())
	}
}

// A run that stops part way, on a host failure that says nothing of the
// answers, keeps the passes it recorded, the one that stopped it among
// them; one that stops on a request the runner can't run keeps those
// before it. A replay of either is refused before anything runs.
func TestAStoppedRunsRecordingIsRefused(t *testing.T) {
	for _, tc := range []struct {
		name   string
		answer func(n int) (update.Result, error)
		lines  int
		lacks  string
	}{
		{"auth", func(n int) (update.Result, error) {
			if n == 3 {
				return update.Result{Reason: update.ReasonAuth}, nil
			}
			return update.Result{Output: []byte(empty), Effort: "max"}, nil
		}, 3, "lacks 18 passes of the suite, script 3, pass 2 first"},
		{"a request the runner can't run", func(n int) (update.Result, error) {
			if n == 3 {
				return update.Result{}, errors.New("update: the batch is empty")
			}
			return update.Result{Output: []byte(empty), Effort: "max"}, nil
		}, 2, "lacks 19 passes of the suite, script 3, pass 1 first"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			tmp := isolate(t)
			dir := t.TempDir()
			recording, pages := filepath.Join(dir, "answers.jsonl"), filepath.Join(dir, "pages")
			runner := &fakeRunner{t: t, tmp: tmp, answer: tc.answer}
			var out, errOut bytes.Buffer
			code := run(context.Background(), []string{"--host", "claude-code", "--spend-model-quota", "--record", recording, "--pages", pages}, &out, &errOut,
				func(string) update.Runner { return runner })
			if code != 1 || len(runner.requests) != 3 {
				t.Fatalf("exit %d after %d calls\n%s\n%s", code, len(runner.requests), out.String(), errOut.String())
			}
			lines := readLines(t, recording)
			if len(lines) != tc.lines || lines[len(lines)-1].Reason != map[int]string{3: update.ReasonAuth, 2: ""}[tc.lines] {
				t.Fatalf("lines %+v", lines)
			}
			// The scripts that ended before the stop have their pages.
			if got := pagesIn(t, pages); len(got) != 2 || got["v1.01.md"] == "" || got["v1.02.md"] == "" {
				t.Fatalf("pages %v", got)
			}
			out.Reset()
			errOut.Reset()
			code = run(context.Background(), []string{"--host", "claude-code", "--replay", recording}, &out, &errOut, noRunner(t))
			if code != 2 || out.Len() != 0 || !strings.Contains(errOut.String(), " is refused: the recording "+tc.lacks+": the run that made it stopped part way. Nothing was called.\n") {
				t.Fatalf("replay exit %d, out %q, err %q", code, out.String(), errOut.String())
			}
		})
	}
}

// A --record that names a file that exists, or a --pages that names a
// folder that holds something or a file, is refused before anything is
// made or called, and leaves what it names as it was; so is --record with
// --replay, and --replay with a flag for the CLI. A run refused for want
// of --spend-model-quota makes no recording and no pages folder.
func TestRecordAndPagesRefusals(t *testing.T) {
	for _, tc := range []struct {
		name string
		args func(dir string) []string
		want string
	}{
		{"a recording that exists", func(dir string) []string {
			return []string{"--spend-model-quota", "--record", filepath.Join(dir, "file")}
		}, "live-accuracy: --record must name a file that does not exist yet; usage: "},
		{"a recording that is a link to nothing", func(dir string) []string {
			return []string{"--spend-model-quota", "--record", filepath.Join(dir, "dangling")}
		}, "live-accuracy: --record must name a file that does not exist yet; usage: "},
		{"pages in a folder that holds a file", func(dir string) []string {
			return []string{"--spend-model-quota", "--pages", filepath.Join(dir, "full")}
		}, "live-accuracy: --pages must name a folder that is new or empty; usage: "},
		{"pages in a file", func(dir string) []string {
			return []string{"--spend-model-quota", "--pages", filepath.Join(dir, "file")}
		}, "live-accuracy: --pages must name a folder that is new or empty; usage: "},
		{"pages in a link to nothing", func(dir string) []string {
			return []string{"--spend-model-quota", "--pages", filepath.Join(dir, "dangling")}
		}, "live-accuracy: --pages names a link to nothing; usage: "},
		{"record and replay", func(dir string) []string {
			return []string{"--record", filepath.Join(dir, "new"), "--replay", filepath.Join(dir, "file")}
		}, "live-accuracy: --record and --replay can't be used together; usage: "},
		{"replay with a CLI", func(dir string) []string {
			return []string{"--replay", filepath.Join(dir, "file"), "--cli", "/opt/bin/claude"}
		}, "live-accuracy: --replay runs no CLI, so it takes no --cli or --codex-ignore-user-config; usage: "},
		{"no quota", func(dir string) []string {
			return []string{"--record", filepath.Join(dir, "new"), "--pages", filepath.Join(dir, "pages")}
		}, "live-accuracy: the run makes 21 model calls on claude-code's subscription"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			tmp := isolate(t)
			dir := t.TempDir()
			for path, data := range map[string]string{"file": "kept\n", "full/page.md": "kept\n"} {
				if err := os.MkdirAll(filepath.Dir(filepath.Join(dir, path)), 0o700); err != nil {
					t.Fatal(err)
				}
				if err := os.WriteFile(filepath.Join(dir, path), []byte(data), 0o600); err != nil {
					t.Fatal(err)
				}
			}
			if err := os.Symlink(filepath.Join(dir, "nowhere"), filepath.Join(dir, "dangling")); err != nil {
				t.Fatal(err)
			}
			var out, errOut bytes.Buffer
			code := run(context.Background(), append([]string{"--host", "claude-code"}, tc.args(dir)...), &out, &errOut, noRunner(t))
			if code != 2 || out.Len() != 0 || !strings.HasPrefix(errOut.String(), tc.want) {
				t.Fatalf("exit %d, out %q, err %q", code, out.String(), errOut.String())
			}
			if left := leftIn(t, dir); !slices.Equal(left, []string{"dangling", "file", "full", filepath.Join("full", "page.md")}) {
				t.Errorf("the refused run left %q", left)
			}
			for _, path := range []string{"file", "full/page.md"} {
				if data, err := os.ReadFile(filepath.Join(dir, path)); err != nil || string(data) != "kept\n" {
					t.Errorf("%s: %q, %v", path, data, err)
				}
			}
			if left := leftIn(t, tmp); len(left) != 0 {
				t.Errorf("the refused run left %q", left)
			}
		})
	}
}

// A recording that cannot be made, in a folder that does not exist, stops
// the run before any runner is made, any home or pages folder is made or
// anything is called.
func TestARecordingThatCannotBeMade(t *testing.T) {
	tmp := isolate(t)
	dir := t.TempDir()
	var out, errOut bytes.Buffer
	args := []string{"--host", "claude-code", "--spend-model-quota", "--record", filepath.Join(dir, "none", "answers.jsonl"), "--pages", filepath.Join(dir, "pages")}
	if code := run(context.Background(), args, &out, &errOut, noRunner(t)); code != 1 || out.Len() != 0 || !strings.HasPrefix(errOut.String(), "live-accuracy: could not make the recording: ") {
		t.Fatalf("exit %d, out %q, err %q", code, out.String(), errOut.String())
	}
	if left := leftIn(t, dir); len(left) != 0 {
		t.Errorf("the run left %q", left)
	}
	if left := leftIn(t, tmp); len(left) != 0 {
		t.Errorf("the run left %q", left)
	}
}

// A recording's line holds the format's keys in its order, and nothing
// else: an answer of another form than an object is kept as given, an
// answer that is not JSON as null.
func TestRecordLine(t *testing.T) {
	path := filepath.Join(t.TempDir(), "answers.jsonl")
	rec, err := createRecording(path, "codex", "")
	if err != nil {
		t.Fatal(err)
	}
	for _, res := range []update.Result{
		{Output: []byte("{\n  \"a\": [1, 2]\n}"), Effort: "low", TokensIn: 9, Attempts: 2, Model: "x", Degraded: "d"},
		{Output: []byte("not json"), Reason: update.ReasonInvalidOutput},
		{Reason: update.ReasonTimeout},
	} {
		if err := rec.write(3, 2, res); err != nil {
			t.Fatal(err)
		}
	}
	if err := rec.close(); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	want := `{"v":1,"host":"codex","effort":"low","model":"","script":3,"pass":2,"output":{"a":[1,2]},"reason":""}` + "\n" +
		`{"v":1,"host":"codex","effort":"","model":"","script":3,"pass":2,"output":null,"reason":"invalid_output"}` + "\n" +
		`{"v":1,"host":"codex","effort":"","model":"","script":3,"pass":2,"output":null,"reason":"timeout"}` + "\n"
	if string(data) != want {
		t.Fatalf("%s\nwant\n%s", data, want)
	}
	if _, err := createRecording(path, "codex", ""); err == nil {
		t.Fatal("a second recording was made over the first")
	}
	for i, l := range readLines(t, path) {
		res := l.result()
		if i == 0 && string(res.Output) != `{"a":[1,2]}` || i > 0 && res.Output != nil {
			t.Errorf("line %d gives %q", i+1, res.Output)
		}
	}
}

// A page that cannot be written stops the run before its next pass, with
// no call made and no line recorded after it, and exits 1; the last
// script's page, which no pass comes after, still turns the exit to 1
// after the report.
func TestAPageThatCannotBeWritten(t *testing.T) {
	for _, tc := range []struct {
		name  string
		at    int // the request in whose script the page is made unwritable
		page  string
		calls int
		want  string
	}{
		{"in the middle", 2, "v1.02.md", 2, "live-accuracy: the run stopped: could not write the page v1.02.md: "},
		{"the last", 21, "v1.12.md", 21, "live-accuracy: could not write the page v1.12.md: "},
	} {
		t.Run(tc.name, func(t *testing.T) {
			tmp := isolate(t)
			dir := t.TempDir()
			recording, pages := filepath.Join(dir, "answers.jsonl"), filepath.Join(dir, "pages")
			// A folder where the page goes stops its write, as root too.
			runner := &fakeRunner{t: t, tmp: tmp, answer: func(n int) (update.Result, error) {
				if n == tc.at {
					if err := os.Mkdir(filepath.Join(pages, tc.page), 0o700); err != nil {
						t.Error(err)
					}
				}
				return update.Result{Output: []byte(empty), Effort: "max"}, nil
			}}
			var out, errOut bytes.Buffer
			args := []string{"--host", "claude-code", "--spend-model-quota", "--record", recording, "--pages", pages}
			code := run(context.Background(), args, &out, &errOut, func(string) update.Runner { return runner })
			if code != 1 || len(runner.requests) != tc.calls || !strings.Contains(errOut.String(), tc.want) {
				t.Fatalf("exit %d after %d calls\n%s\n%s", code, len(runner.requests), out.String(), errOut.String())
			}
			if lines := readLines(t, recording); len(lines) != tc.calls {
				t.Errorf("%d lines recorded", len(lines))
			}
			if summed := strings.Contains(out.String(), "\nOn claude-code, against the target"); summed != (tc.calls == 21) {
				t.Errorf("the report:\n%s", out.String())
			}
			if left := leftIn(t, tmp); len(left) != 0 {
				t.Errorf("the run left %q", left)
			}
		})
	}
}

// A replay whose context has ended answers nothing: it stops at the first
// pass, as a live run does, and prints no history.
func TestAReplayStopsWithItsContext(t *testing.T) {
	isolate(t)
	recording := filepath.Join(t.TempDir(), "answers.jsonl")
	writeRecording(t, recording, nil)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	var out, errOut bytes.Buffer
	code := run(ctx, []string{"--host", "claude-code", "--replay", recording}, &out, &errOut, noRunner(t))
	if code != 1 || out.String() != "The accuracy suite's 12 fact scripts on claude-code, the host's default model, effort as recorded:\n" ||
		!strings.HasSuffix(errOut.String(), "live-accuracy: the run stopped: context canceled\n") {
		t.Fatalf("exit %d\n%s\n%s", code, out.String(), errOut.String())
	}
}
