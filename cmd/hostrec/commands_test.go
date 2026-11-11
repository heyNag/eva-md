package main

import (
	"archive/tar"
	"compress/gzip"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"testing"

	"github.com/heyNag/eva-md/internal/hostfacts"
)

var runBookCommand = regexp.MustCompile("`hostrec ([a-z][a-z-]*)")

// Every hostrec command the run-book names exists; those of its Part 1
// are built, and the others are Part 2's, which this build names.
func TestEveryRunBookCommandExists(t *testing.T) {
	book := readDoc(t, "docs/v2/host-sitting-runbook.md")
	partOne, _, ok := strings.Cut(book, "\n## Part 2")
	if !ok {
		t.Fatal("the run-book has no Part 2")
	}
	seen := map[string]bool{}
	for _, m := range runBookCommand.FindAllStringSubmatch(partOne, -1) {
		seen[m[1]] = true
		if _, ok := commands[m[1]]; !ok {
			t.Errorf("Part 1 of the run-book names hostrec %s, which is not built", m[1])
		}
	}
	if len(seen) < 15 {
		t.Errorf("Part 1 names only %d commands: %v", len(seen), seen)
	}
	for _, m := range runBookCommand.FindAllStringSubmatch(book, -1) {
		if _, ok := commands[m[1]]; !ok && !slices.Contains(part2, m[1]) {
			t.Errorf("the run-book names hostrec %s, which is neither built nor Part 2's", m[1])
		}
	}
	h := newHome(t)
	for _, name := range part2 {
		if code, _, errOut := h.run(t, "", name); code == 0 || !strings.Contains(errOut, "Part 2") {
			t.Errorf("hostrec %s: %d %q", name, code, errOut)
		}
	}
	if code, out, _ := h.run(t, ""); code != 0 || !strings.Contains(out, "hostrec, the recorder") {
		t.Errorf("hostrec with no command: %d %s", code, out)
	}
}

// mark, note, tail and close keep the sitting's state; each mark keeps the
// hosts' versions, and summary warns when one changed.
func TestASittingAndItsSummary(t *testing.T) {
	h := newHome(t)
	h.setup(t)
	if code, _, errOut := h.run(t, "", "mark", "C-99"); code == 0 || !strings.Contains(errOut, "not a step") {
		t.Errorf("a step the run-book does not have: %d %s", code, errOut)
	}
	if out := h.must(t, "", "mark", "C-00"); !strings.Contains(out, "the recording is open") {
		t.Errorf("the first mark: %s", out)
	}
	h.must(t, "", "mark", "C-04")
	p := map[string]any{"session_id": sessionID, "cwd": filepath.Join(h.home, "scratch", "alpha-orchard")}
	h.hook(t, "claude-code", "UserPromptSubmit", p)
	h.hook(t, "claude-code", "Stop", p)
	h.hook(t, "codex", "Stop", p)
	out := h.must(t, "", "tail")
	if strings.Count(out, "claude-code  UserPromptSubmit") != 1 || strings.Count(out, "  Stop") != 2 || !strings.Contains(out, "3 events since the mark of C-04") {
		t.Errorf("tail:\n%s", out)
	}
	if strings.Contains(out, sessionID) {
		t.Error("tail shows a payload")
	}
	h.must(t, "", "note", "made-up", "note", "at C-04")
	notes := h.recFiles(t)[hostfacts.NotesFile]
	if !strings.Contains(notes, `"mark":"C-04"`) || !strings.Contains(notes, "made-up note at C-04") {
		t.Errorf("the note: %s", notes)
	}
	// The host's version changes before C-15.
	t.Setenv("HOSTREC_FAKE_VERSION", "")
	if err := os.WriteFile(filepath.Join(h.bin, "claude"), []byte("#!/bin/sh\necho '9.9.10 (made-up claude)'\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	h.must(t, "", "mark", "C-15")
	code, out, _ := h.run(t, "", "summary", "sitting-1")
	if code == 0 {
		t.Error("summary passes with core facts missing")
	}
	for _, want := range []string{
		"claude-code Stop: 1", "claude-code UserPromptSubmit: 1",
		"C-04: claude-code 9.9.9 (made-up claude); codex 9.9.9 (made-up codex)",
		"WARNING: claude-code changed version before C-15",
		"F-016 (core) A prompt the host writes is marked as the host's: recorded at C-04: 2 events, 0 results, 1 note",
		"F-033 (core) A fork or resume names its parent: MISSING; settled by C-27",
		"F-175 (core) The server takes a push of machine commits: MISSING; settled by G-04, G-05",
		"2 core facts missing.",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("summary does not say %q:\n%s", want, out)
		}
	}
	if strings.Contains(out, "codex Stop") {
		t.Error("sitting 1's summary lists Codex's events")
	}
	if code, _, _ := h.run(t, "", "summary", "sitting-9"); code == 0 {
		t.Error("a scope the run-book does not have")
	}
	if out := h.must(t, "", "close"); !strings.Contains(out, "closed") {
		t.Errorf("close: %s", out)
	}
	if out := h.must(t, "", "close"); !strings.Contains(out, "closed already") {
		t.Errorf("a second close: %s", out)
	}
	marks := h.recFiles(t)[hostfacts.MarksFile]
	if strings.Count(marks, `"kind":"mark"`) != 3 || strings.Count(marks, `"kind":"close"`) != 1 {
		t.Errorf("the marks: %s", marks)
	}
}

// voice-check counts the call's rows by type and matches each hand-over to
// the script's line, printing no text of either.
func TestVoiceCheck(t *testing.T) {
	h := newHome(t)
	h.setup(t)
	h.must(t, "", "mark", "X-44")
	rollout := newTranscript(t, filepath.Join(h.home, ".codex", "sessions", "2026", "05", "01", "rollout-made-up.jsonl"))
	rollout.add(map[string]any{"type": "session_meta", "payload": map[string]any{"id": sessionID, "realtime_version": "made-up-v9"}},
		map[string]any{"type": "realtime_item", "payload": map[string]any{"type": "message", "role": "user", "text": "spoken made-up words"}},
		map[string]any{"type": "realtime_item", "payload": map[string]any{"type": "message", "role": "assistant", "text": "more spoken made-up words"}})
	for _, request := range []string{
		"Run the shell command sleep 30, then list the files in the orchard folder.",
		"Create a file named fig.txt in the orchard folder containing the single word ripe.",
	} {
		h.hook(t, "codex", "UserPromptSubmit", map[string]any{"session_id": sessionID, "transcript_path": rollout.path,
			"prompt": "<realtime_delegation>\n  <input>" + request + "</input>\n</realtime_delegation>", "voice": map[string]any{"realtime_version": "made-up-v9"}})
	}
	h.hook(t, "codex", "Stop", map[string]any{"session_id": sessionID, "transcript_path": rollout.path})
	out := h.must(t, "", "voice-check")
	for _, want := range []string{"3 events from Codex, 2 hand-overs", "realtime_item: 2", "session_meta: 1",
		"Hand-over 1: closest to the script's line 1", "Hand-over 2: closest to the script's line 2",
		"voice.realtime_version names the voice feature's version: made-up-v9"} {
		if !strings.Contains(out, want) {
			t.Errorf("voice-check does not say %q:\n%s", want, out)
		}
	}
	for _, bad := range []string{"fig.txt", "spoken", "sleep 30"} {
		if strings.Contains(out, bad) {
			t.Errorf("voice-check prints %q", bad)
		}
	}
}

func TestRegisterAndUnregister(t *testing.T) {
	h := newHome(t)
	h.setup(t)
	h.must(t, "", "mark", "C-0b")
	out := h.must(t, "", "register", "claude-code")
	if !strings.Contains(out, "claude-code lists hf-recorder.") {
		t.Errorf("register:\n%s", out)
	}
	var adds [][]string
	for _, argv := range h.fakeLog(t) {
		if slices.Contains(argv, "add") || slices.Contains(argv, "install") {
			adds = append(adds, argv)
		}
	}
	market := filepath.Join(h.home, "hf", "marketplace")
	if want := registerCommands("claude-code", market); len(adds) != 2 || !slices.Equal(adds[0], want[0]) || !slices.Equal(adds[1], want[1]) {
		t.Errorf("the commands run: %v", adds)
	}
	// A host lists the plugin already: nothing is added again.
	os.Remove(h.log)
	if out := h.must(t, "", "register", "claude-code"); !strings.Contains(out, "lists hf-recorder already") {
		t.Errorf("a second register: %s", out)
	}
	for _, argv := range h.fakeLog(t) {
		if slices.Contains(argv, "install") {
			t.Errorf("a plugin the host lists was added again: %v", argv)
		}
	}
	// A command that fails with --json is tried once more without it.
	os.Remove(h.log)
	t.Setenv("HOSTREC_FAKE_FAIL_JSON", "1")
	out = h.must(t, "", "register", "codex")
	if !strings.Contains(out, "codex lists hf-hooks.") || !strings.Contains(out, "codex plugin add hf-hooks@hf-scratch </dev/null: exit status 0") {
		t.Errorf("register codex:\n%s", out)
	}
	tries := 0
	for _, argv := range h.fakeLog(t) {
		if slices.Contains(argv, "add") && slices.Contains(argv, "hf-hooks@hf-scratch") {
			tries++
		}
	}
	if tries != 2 {
		t.Errorf("the plugin was added in %d forms", tries)
	}
	// A command that asks a question fails, and says so.
	t.Setenv("HOSTREC_FAKE_FAIL_JSON", "")
	t.Setenv("HOSTREC_FAKE_ASK", "1")
	h.must(t, "", "unregister", "codex")
	code, out, _ := h.run(t, "", "register", "codex")
	if code == 0 || !strings.Contains(out, "and it asked a question") || !strings.Contains(out, "Type it yourself") {
		t.Errorf("a register that asks: %d\n%s", code, out)
	}
	t.Setenv("HOSTREC_FAKE_ASK", "")
	recorded := h.recFiles(t)[hostfacts.OutputsFile]
	if !strings.Contains(recorded, `"asked":true`) || !strings.Contains(recorded, `"json":false`) || strings.Contains(recorded, "trust this marketplace") {
		t.Errorf("the recorded outcomes: %s", recorded)
	}
	out = h.must(t, "", "register", "--check")
	if !strings.Contains(out, "claude-code lists hf-recorder.") || !strings.Contains(out, "codex lists no scratch plugin.") {
		t.Errorf("register --check:\n%s", out)
	}
	h.must(t, "", "unregister", "all")
	if out := h.must(t, "", "register", "--check"); !strings.Contains(out, "claude-code lists no scratch plugin.") {
		t.Errorf("after unregister all:\n%s", out)
	}
	for _, bad := range [][]string{{"register", "claude-code", "hf-hooks"}, {"register", "all"}, {"register", "made-up-host"}} {
		if code, _, _ := h.run(t, "", bad...); code == 0 {
			t.Errorf("%v was taken", bad)
		}
	}
}

func TestTrust(t *testing.T) {
	h := newHome(t)
	h.setup(t)
	h.must(t, "", "mark", "X-0c")
	out := h.must(t, "", "trust")
	if !strings.Contains(out, "Codex loads 4 hooks from the scratch plugins; 4 of them trusted and enabled.") || !strings.Contains(out, "hf-hooks sessionStart: 2, trusted") {
		t.Errorf("trust:\n%s", out)
	}
	t.Setenv("HOSTREC_FAKE_TRUST", "untrusted")
	if code, out, _ := h.run(t, "", "trust"); code == 0 || !strings.Contains(out, "0 of them trusted") {
		t.Errorf("untrusted hooks: %d\n%s", code, out)
	}
	if !strings.Contains(h.recFiles(t)[hostfacts.OutputsFile], `"command":"trust"`) {
		t.Error("trust's outcome is not recorded")
	}
}

// pack writes the archive and its checksum without the private folder, and
// --clean deletes them; fixtures refuse a recording inside a checkout, and
// fixtures made from a hook's recording pass check; scan finds what was
// typed; teardown removes what the sittings made.
func TestTheHandOver(t *testing.T) {
	h := newHome(t)
	h.setup(t)
	h.must(t, "", "mark", "C-04")
	h.hook(t, "claude-code", "UserPromptSubmit", map[string]any{"session_id": sessionID, "prompt": "Made-up prompt naming quinnreal", "cwd": filepath.Join(h.home, "scratch")})
	h.must(t, "", "close")
	out := h.must(t, "", "pack", h.rec, "--to", h.shared)
	archive := filepath.Join(h.shared, "hf-rec.tar.gz")
	if !strings.Contains(out, archive) {
		t.Errorf("pack: %s", out)
	}
	data, err := os.ReadFile(archive)
	if err != nil {
		t.Fatal(err)
	}
	sum := sha256.Sum256(data)
	if line, _ := os.ReadFile(archive + ".sha256"); string(line) != hex.EncodeToString(sum[:])+"  hf-rec.tar.gz\n" {
		t.Errorf("the checksum file: %q", line)
	}
	gz, err := gzip.NewReader(strings.NewReader(string(data)))
	if err != nil {
		t.Fatal(err)
	}
	tr := tar.NewReader(gz)
	var names []string
	for {
		hd, err := tr.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			t.Fatal(err)
		}
		names = append(names, hd.Name)
	}
	if !slices.Contains(names, "hf-rec/events.jsonl") || slices.ContainsFunc(names, func(n string) bool { return strings.Contains(n, "private") || strings.Contains(n, ".lock") }) {
		t.Errorf("the archive holds %v", names)
	}
	if code, _, _ := h.run(t, "", "pack", filepath.Join(h.home, "hf-private")); code == 0 {
		t.Error("pack took the private folder")
	}
	if code, _, _ := h.run(t, "", "pack", h.home); code == 0 {
		t.Error("pack took a folder holding the private folder")
	}
	h.must(t, "", "pack", "--clean")
	for _, p := range []string{archive, archive + ".sha256"} {
		if _, err := os.Stat(p); !os.IsNotExist(err) {
			t.Errorf("--clean left %s", p)
		}
	}
	// scan, with the owner's answers on stdin.
	answers := "made.up.owner@madeup-mail.net\nQuinn Realname\n\ngit.madeup-forge.test\npears\nquinnreal, orchardco\n"
	code, out, _ := h.run(t, answers, "scan", h.rec)
	if code == 0 || !strings.Contains(out, "events.jsonl:1  what you typed: quinnreal") || !strings.Contains(out, "uuid: ") {
		t.Errorf("scan:\n%s", out)
	}
	if !strings.Contains(out, "Your own account's short name ["+fakeOther.Short+"]") {
		t.Errorf("scan does not offer its guess:\n%s", out)
	}
	if code, _, _ := h.run(t, "\n\n", "scan", h.rec); code == 0 {
		t.Error("scan ran with no full name")
	}
	h.must(t, "", "scrub", h.rec, "quinnreal")
	if code, out, _ := h.run(t, answers, "scan", h.rec); code != 0 || !strings.Contains(out, "hits to judge: none") {
		t.Errorf("scan after scrub: %d\n%s", code, out)
	}
	// fixtures and check, from the recording.
	fx := filepath.Join(filepath.Dir(h.home), "fixtures", "claude-code", "9.9.9")
	h.must(t, "", "fixtures", "--rec", h.rec, "--out", fx)
	if out := h.must(t, "", "check", "--rec", h.rec, "--fixtures", fx); !strings.Contains(out, "no value of the recording is in the fixtures") {
		t.Errorf("check: %s", out)
	}
	ev, _ := os.ReadFile(filepath.Join(fx, hostfacts.EventsFile))
	var fixture map[string]any
	if err := json.Unmarshal([]byte(strings.SplitN(string(ev), "\n", 2)[0]), &fixture); err != nil {
		t.Fatal(err)
	}
	made := fixture["payload"].(map[string]any)["session_id"].(string)
	if err := os.WriteFile(filepath.Join(fx, hostfacts.EventsFile), []byte(strings.ReplaceAll(string(ev), made, sessionID)), 0o644); err != nil {
		t.Fatal(err)
	}
	if code, _, _ := h.run(t, "", "check", "--rec", h.rec, "--fixtures", fx); code == 0 {
		t.Error("check passes a recorded session id put back")
	}
	checkout := filepath.Join(filepath.Dir(h.home), "checkout")
	if out, err := gitIn(filepath.Dir(h.home), "init", "-q", checkout); err != nil {
		t.Fatalf("%v %s", err, out)
	}
	copyTree(t, h.rec, filepath.Join(checkout, "rec"))
	if code, _, errOut := h.run(t, "", "fixtures", "--rec", filepath.Join(checkout, "rec"), "--out", t.TempDir()); code == 0 || !strings.Contains(errOut, "inside a git checkout") {
		t.Errorf("fixtures took a recording inside a checkout: %d %s", code, errOut)
	}
	// teardown.
	h.must(t, "", "key")
	out = h.must(t, "", "teardown")
	for _, p := range []string{h.rec, filepath.Join(h.home, "hf-private"), filepath.Join(h.home, "scratch"), filepath.Join(h.home, "hf"), filepath.Join(h.home, markerName),
		filepath.Join(h.home, ".ssh", "hf_scratch_deploy"), filepath.Join(h.shared, publicKeyOut)} {
		if _, err := os.Lstat(p); !os.IsNotExist(err) {
			t.Errorf("teardown left %s", p)
		}
		if !strings.Contains(out, "removed "+p) {
			t.Errorf("teardown does not list %s", p)
		}
	}
	if _, err := os.Stat(filepath.Join(h.home, "hf-tools", "hf-missing-check")); err != nil {
		t.Error("teardown removed the tools")
	}
}

// Nothing the recorder prints names a library: only eva's name shows.
func TestOnlyEvasNameShows(t *testing.T) {
	var all strings.Builder
	for _, c := range commands {
		all.WriteString(c.help)
	}
	h := newHome(t)
	_, out, errOut := h.run(t, "", "made-up-command")
	all.WriteString(out + errOut)
	h.setup(t)
	all.WriteString(h.must(t, "", "mark", "C-00"))
	for _, bad := range []string{"okf", "okf-agent-memory"} {
		if strings.Contains(strings.ToLower(all.String()), bad) {
			t.Errorf("the recorder shows %q", bad)
		}
	}
}
