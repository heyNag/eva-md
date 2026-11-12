package main

import (
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"testing"

	"github.com/heyNag/eva-md/internal/hostfacts"
)

var (
	runBookSpan  = regexp.MustCompile("`(hostrec[^`]*)`")
	runBookBlock = regexp.MustCompile("(?s)\n```\n(.*?)\n```")
)

// runBookLines gives every hostrec command line of the run-book's Part 1,
// in order: each code span and each line of a code block that starts with
// hostrec.
func runBookLines(t *testing.T) []string {
	t.Helper()
	book := readDoc(t, "docs/v2/host-sitting-runbook.md")
	partOne, _, ok := strings.Cut(book, "\n## Part 2")
	if !ok {
		t.Fatal("the run-book has no Part 2")
	}
	type at struct {
		pos  int
		line string
	}
	var found []at
	for _, m := range runBookBlock.FindAllStringSubmatchIndex(partOne, -1) {
		for _, l := range strings.Split(partOne[m[2]:m[3]], "\n") {
			if l = strings.TrimSpace(l); strings.HasPrefix(l, "hostrec ") {
				found = append(found, at{m[2], l})
			}
		}
	}
	blocks := runBookBlock.ReplaceAllString(partOne, "\n")
	for _, m := range runBookSpan.FindAllStringSubmatchIndex(blocks, -1) {
		found = append(found, at{m[0], blocks[m[2]:m[3]]})
	}
	var out []string
	for _, f := range found {
		out = append(out, f.line)
	}
	return out
}

// words splits a command line as a shell would: words between spaces,
// double quotes kept together, ~/ and /Users/Shared made the test's.
func words(line string, h *testHome) []string {
	var out []string
	var cur strings.Builder
	quoted, any := false, false
	for _, r := range line {
		switch {
		case r == '"':
			quoted, any = !quoted, true
		case r == ' ' && !quoted:
			if any {
				out = append(out, cur.String())
			}
			cur.Reset()
			any = false
		default:
			cur.WriteRune(r)
			any = true
		}
	}
	if any {
		out = append(out, cur.String())
	}
	for i, w := range out {
		w = strings.Replace(w, "~/", h.home+"/", 1)
		out[i] = strings.Replace(w, "/Users/Shared", h.shared, 1)
	}
	return out[1:]
}

// notCommands are the run-book's code spans that name hostrec or a command
// in prose, with no arguments to run, and the templates whose every use
// the run-book also writes out: the lines below run each.
var notCommands = map[string]string{
	"hostrec":             "the recorder's name",
	"hostrec mark":        "the command's name in prose",
	"hostrec setup":       "the command's name in prose; S0-05 writes its line",
	"hostrec note":        "the command's name in prose",
	"hostrec register":    "the command's name in prose",
	"hostrec mark <step>": "the template of every step's mark",
	"hostrec arm <name>":  "the template of arm start, Part 1's one arm",
}

// Every hostrec command line of the run-book's Part 1 is run, in the
// order of the sittings, in a made-up home with fake host CLIs, a fake ssh
// and a local bare repository standing for the git server, with the hosts'
// hooks fed made-up payloads where a step makes a chat; each does what its
// step's Expect says. The run's script holds every line of the run-book,
// and only those.
func TestTheRunBooksCommandsRun(t *testing.T) {
	h := newHome(t)
	// The run-book's own evaluation account.
	listAccounts = func(home string) (hostfacts.Account, []hostfacts.Account, error) {
		return hostfacts.Account{Short: "hfeval", Full: "HF Eval Home", Home: home}, []hostfacts.Account{fakeOther}, nil
	}
	server, bare, _ := madeUpServer(t, h)
	self, _ := os.Executable()
	sshPath := filepath.Join(h.bin, "ssh")
	fakeSSHScript := "#!/bin/sh\nHOSTREC_FAKE=ssh exec " + shellQuote(self) + " \"$@\"\n"
	noNetwork := "#!/bin/sh\necho 'ssh: Could not resolve hostname git.madeup-forge.test: nodename nor servname provided' >&2\nexit 255\n"
	t.Setenv("HOSTREC_FAKE_SERVER", server)
	writeExe := func(p, text string) {
		t.Helper()
		if err := os.WriteFile(p, []byte(text), 0o755); err != nil {
			t.Fatal(err)
		}
	}

	cwd := filepath.Join(h.home, "scratch", "alpha-orchard")
	claudeSession, forkSession := sessionID, "2b3c4d5e-6f70-4182-93a4-b5c6d7e8f901"
	claudeRecord := func(id string) *transcript {
		return newTranscript(t, filepath.Join(h.home, ".claude", "projects", strings.ReplaceAll(cwd, "/", "-"), id+".jsonl"))
	}
	var cc, fork *transcript
	claude := func(tr *transcript, event string, extra map[string]any) {
		p := map[string]any{"session_id": filepath.Base(strings.TrimSuffix(tr.path, ".jsonl")), "transcript_path": tr.path, "cwd": cwd, "hook_event_name": event}
		for k, v := range extra {
			p[k] = v
		}
		h.hook(t, "claude-code", event, p)
	}
	turn := func(tr *transcript, prompt string) {
		claude(tr, "UserPromptSubmit", map[string]any{"prompt": prompt})
		tr.add(map[string]any{"type": "user", "message": map[string]any{"role": "user", "content": prompt}})
		tr.add(map[string]any{"type": "assistant", "message": map[string]any{"role": "assistant", "content": []any{map[string]any{"type": "text", "text": "Made-up reply."}}}})
		claude(tr, "Stop", map[string]any{"last_assistant_message": "Made-up reply.", "stop_hook_active": false})
	}
	rollout := newTranscript(t, filepath.Join(h.home, ".codex", "sessions", "2026", "05", "02", "rollout-2026-05-02T10-00-00-"+sessionID+".jsonl"))
	codex := func(event string, extra map[string]any) {
		p := map[string]any{"session_id": sessionID, "turn_id": turnID, "transcript_path": rollout.path, "cwd": cwd, "hook_event_name": event}
		for k, v := range extra {
			p[k] = v
		}
		h.hook(t, "codex", event, p)
	}
	codexTurn := func(prompt string) {
		codex("UserPromptSubmit", map[string]any{"prompt": prompt})
		rollout.add(voiceRows(prompt)...)
		codex("Stop", map[string]any{"last_assistant_message": "Made-up reply."})
	}

	type step struct {
		line   string
		before func()
		after  func(out string)
		stdin  string
		code   int
		want   []string
	}
	var fingerprint string
	steps := []step{
		// Sitting 0.
		{line: "hostrec setup --rec ~/hf-rec", want: []string{"found no eva-md install and no earlier session in this account", "Made the recording folder " + h.rec}},
		// "How to follow a step" and "Where a sitting can stop".
		{line: "hostrec mark C-00", want: []string{"the recording is open"}},
		{line: "hostrec tail", want: []string{"0 events since the mark of C-00"}},
		{line: `hostrec note "off-script at <step>"`, want: []string{"noted at C-00"}},
		{line: `hostrec note "what you saw"`, want: []string{"noted at C-00"}},
		{line: "hostrec disarm", want: []string{"disarmed: the scratch hooks record only"}},
		// While the recording is closed, the scratch hooks record nothing.
		{line: "hostrec close", want: []string{"the recording is closed"}, after: func(string) {
			p := map[string]any{"session_id": sessionID, "transcript_path": filepath.Join(h.home, "none.jsonl"), "cwd": cwd, "hook_event_name": "UserPromptSubmit", "prompt": "Made-up."}
			h.hook(t, "claude-code", "UserPromptSubmit", p)
			if strings.TrimSpace(h.recFiles(t)[hostfacts.EventsFile]) != "" {
				t.Error("a hook recorded an event while the recording was closed")
			}
		}},
		// Sitting 1.
		{line: "hostrec mark C-00", want: []string{"the recording is open"}},
		{line: "hostrec mark G-01"},
		{line: "hostrec key", want: []string{"wrote its public half to " + filepath.Join(h.shared, publicKeyOut)}, after: func(string) {
			if _, err := os.Stat(filepath.Join(h.home, ".ssh", "hf_scratch_deploy")); err != nil {
				t.Errorf("key made no key in the account: %v", err)
			}
		}},
		{line: `hostrec note "G-02: not beside the wiki"`, before: func() {
			// The owner saves the address in G-02, from the owner's own
			// account.
			if err := os.WriteFile(filepath.Join(h.shared, addressFile), []byte(madeUpAddress+"\n"), 0o644); err != nil {
				t.Fatal(err)
			}
		}, want: []string{"noted at G-01"}},
		{line: `hostrec note "G-02: deploy keys switched off"`, want: []string{"noted at G-01"}},
		{line: "hostrec mark G-03"},
		{line: "hostrec remote set", stdin: "y\n", want: []string{"hf-scratch-plum", "stored the address", "Deleted " + filepath.Join(h.shared, publicKeyOut)}},
		{line: "hostrec mark G-04"},
		// The pushes fail before the server answers: G-04's one more try
		// comes at the start of sitting 2.
		{line: "hostrec push", before: func() { writeExe(sshPath, noNetwork) }, code: 1, want: []string{pushNoAnswer}},
		// C-0a and C-0b.
		{line: "hostrec mark C-0a"},
		{line: "hostrec mark C-0b"},
		{line: "hostrec register claude-code", want: []string{"exit status 0", "claude-code lists " + claudePlugin}},
		{line: `hostrec note "C-0b: <the command's first four words> asked: <the question, in a few words>"`, want: []string{"noted at C-0b"}},
		{line: "hostrec register --check", want: []string{"claude-code lists " + claudePlugin}},
		{line: "hostrec mark C-01"},
		{line: "hostrec arm start"},
		{line: `hostrec note "start line: ..."`, before: func() {
			cc = claudeRecord(claudeSession)
			claude(cc, "SessionStart", map[string]any{"source": "startup"})
		}, want: []string{"noted at C-01"}},
		{line: "hostrec mark C-02"},
		{line: "hostrec tail", before: func() {
			turn(cc, "This is a test with made-up content. Answer on two lines.")
		}, want: []string{"claude-code  UserPromptSubmit", "claude-code  Stop", "2 events since the mark of C-02"}},
		{line: "hostrec mark C-04"},
		{line: "hostrec mark C-15", before: func() {
			claude(cc, "SessionEnd", map[string]any{"reason": "prompt_input_exit"})
			claude(cc, "SessionStart", map[string]any{"source": "startup"})
			claude(cc, "PreToolUse", map[string]any{"tool_name": "Read", "tool_use_id": toolUseID, "tool_input": map[string]any{"file_path": cwd + "/orchard/plums.txt"}})
			turn(cc, "Made-up test. First write one sentence saying what you will do.")
		}},
		{line: "hostrec mark C-17", before: func() {
			claude(cc, "UserPromptSubmit", map[string]any{"prompt": "Made-up test. Run the shell command `sleep 60`, then say done."})
			cc.add(map[string]any{"type": "user", "message": map[string]any{"role": "user", "content": "[Request interrupted by user for tool use]"}})
		}},
		{line: "hostrec mark C-19", before: func() {
			claude(cc, "UserPromptSubmit", map[string]any{"prompt": "<command-name>/hf-recorder:hf-pause-check</command-name>"})
		}},
		{line: `hostrec note "no schedule offered"`, want: []string{"noted at C-19"}},
		{line: "hostrec mark C-27", before: func() {
			turn(cc, "Made-up test. Start a background shell command.")
		}},
		{line: "hostrec mark C-42", before: func() {
			claude(cc, "SessionStart", map[string]any{"source": "resume"})
			turn(cc, "Made-up test: say hello again.")
			fork = claudeRecord(forkSession)
			claude(fork, "SessionStart", map[string]any{"source": "startup"})
			turn(fork, "Made-up test: say hello from the fork.")
		}},
		{line: "hostrec disarm"},
		{line: "hostrec summary sitting-1", want: []string{"Events that fired:", "claude-code SessionStart: 4",
			"F-016 (core) A prompt the host writes is marked as the host's: recorded at", "F-033 (core) A fork or resume names its parent: recorded at C-27",
			"F-175 (core) The server takes a push of machine commits: recorded at G-04", "Each host CLI's version at each mark:", "C-42: claude-code 9.9.9 (made-up claude)"}},
		{line: "hostrec close", want: []string{"the recording is closed"}},
		// Sitting 2, with G-04's one more try and G-05 right after X-00.
		{line: "hostrec mark X-00", want: []string{"the recording is open"}},
		{line: "hostrec mark G-04"},
		// The first time it reaches the server, it shows the server's host
		// key, which it keeps in the recorder's own known-hosts file only.
		{line: "hostrec push", before: func() {
			writeExe(sshPath, fakeSSHScript)
			hostKey := filepath.Join(t.TempDir(), "made-up-host-key")
			if out, err := execCommand("ssh-keygen", "-q", "-t", "ed25519", "-N", "", "-C", "", "-f", hostKey).CombinedOutput(); err != nil {
				t.Fatalf("ssh-keygen: %v %s", err, out)
			}
			t.Setenv("HOSTREC_FAKE_HOSTKEY", hostKey+".pub")
		}, after: func(out string) {
			if _, rest, ok := strings.Cut(out, " SHA256:"); ok {
				fingerprint, _, _ = strings.Cut(rest, " ")
			}
			if _, err := os.Stat(filepath.Join(h.home, ".ssh", "known_hosts")); !os.IsNotExist(err) {
				t.Errorf("the server's host key went into the account's own known-hosts file: %v", err)
			}
			if _, err := os.Stat(filepath.Join(h.home, privateName, knownHosts)); err != nil {
				t.Errorf("the recorder's own known-hosts file: %v", err)
			}
		}, want: []string{"The server's host key, now in the recorder's own known-hosts file only:\n256 SHA256:",
			"The unsigned commit, pushed by itself to hf-check-unsigned: accepted.", "The signed commit, pushed by itself to hf-check-signed: refused for the signature.", "Read back: hf-check-unsigned."}},
		{line: "hostrec mark G-05"},
		{line: `hostrec note "G-05: signed commit shown as <verified, unverified or no label>"`, before: func() {
			// The owner deletes the repository and the address file.
			if err := os.RemoveAll(bare); err != nil {
				t.Fatal(err)
			}
			if err := os.Remove(filepath.Join(h.shared, addressFile)); err != nil {
				t.Fatal(err)
			}
		}, want: []string{"noted at G-05"}},
		{line: `hostrec note "G-05: no signed branch"`, want: []string{"noted at G-05"}},
		{line: "hostrec mark X-0a"},
		{line: "hostrec mark X-0b"},
		{line: "hostrec register codex", want: []string{"exit status 0", "codex lists " + codexPlugin}},
		{line: "hostrec mark X-0c"},
		{line: "hostrec trust", want: []string{"4 of them trusted and enabled"}},
		{line: "hostrec mark X-02"},
		{line: "hostrec arm start"},
		{line: "hostrec tail", before: func() {
			rollout.add(map[string]any{"type": "session_meta", "payload": map[string]any{"id": sessionID, "originator": "codex_cli_rs"}})
			if answer := h.hook(t, "codex", "SessionStart", map[string]any{"session_id": sessionID, "transcript_path": rollout.path, "cwd": cwd, "source": "startup"}); !strings.Contains(answer, startWord) {
				t.Errorf("X-02's start: %s", answer)
			}
			codexTurn("This is a test with made-up content. Answer on two lines.")
		}, want: []string{"codex  SessionStart", "codex  UserPromptSubmit"}},
		{line: "hostrec mark X-04"},
		{line: "hostrec mark X-26", before: func() { codexTurn("Made-up test. First write one sentence saying what you will do.") }},
		{line: "hostrec mark X-39", before: func() { codexTurn("Made-up test: reply with the single word PEBBLE.") }},
		{line: `hostrec note "codex app version: <the version shown>"`, want: []string{"noted at X-39"}},
		{line: `hostrec note "voice setting: <what it shows>"`, want: []string{"noted at X-39"}},
		{line: "hostrec mark X-40"},
		{line: "hostrec arm start"},
		{line: "hostrec tail", want: []string{"0 events since the mark of X-40"}},
		{line: "hostrec mark X-43", before: func() {
			codexTurn("Made-up test: say hello from the app, and tell me any codeword you were given at the start.")
		}},
		{line: `hostrec note "X-43: earliest schedule <minutes> min"`, want: []string{"noted at X-43"}},
		{line: "hostrec mark X-44", before: func() { codexTurn("Made-up test: say tick.") }},
		{line: "hostrec mark X-46", before: func() {
			s := hostfacts.VoiceScript
			codexTurn(handOver("", "Run the shell command sleep 30, then list the files in the orchard folder.", "user: "+s[0].Text+"\nuser: Run the shell command sleep 30, then list the files in the orchard folder."))
			codexTurn(handOver("", s[1].Text, "user: "+s[1].Text))
		}},
		{line: "hostrec voice-check", want: []string{"The voice call (X-44): 4 events from Codex, 2 hand-overs.", "Hand-over 1: closest to the script's line 1", "Hand-over 2: closest to the script's line 2"}},
		{line: "hostrec mark X-50"},
		{line: "hostrec disarm"},
		{line: "hostrec summary sitting-2", want: []string{"Events that fired:", "F-121 (core)", "F-142 (core) The request is the front model's wording: recorded at",
			"F-143 (core) Each spoken line keeps its speaker: recorded at X-44", "F-144 (core) A task voice starts by itself begins inside the wrapper: recorded at X-44"}},
		{line: "hostrec close", want: []string{"the recording is closed"}},
		// The hand-over.
		{line: "hostrec scan ~/hf-rec", stdin: "made.up.owner@madeup-mail.net\nQuinn Realname\n\ngit.madeup-forge.test\npears\norchardco\n", want: []string{"hits to judge: none"}},
		{line: `hostrec scrub ~/hf-rec "<the text>"`, want: []string{"hostrec scrub: replaced 0 places"}},
		{line: "hostrec scan ~/hf-rec", stdin: "made.up.owner@madeup-mail.net\nQuinn Realname\n\ngit.madeup-forge.test\npears\norchardco\n", want: []string{"hits to judge: none"}},
		{line: "hostrec pack ~/hf-rec --to /Users/Shared", want: []string{"wrote " + filepath.Join(h.shared, "hf-rec.tar.gz") + " (", "bytes) and " + filepath.Join(h.shared, "hf-rec.tar.gz.sha256")}, after: func(string) {
			// Both readable by the owner's own account.
			for _, p := range []string{filepath.Join(h.shared, "hf-rec.tar.gz"), filepath.Join(h.shared, "hf-rec.tar.gz.sha256")} {
				if info, err := os.Stat(p); err != nil || info.Mode().Perm()&0o004 == 0 {
					t.Errorf("%s is not readable by another account: %v", p, err)
				}
			}
		}},
		{line: "hostrec pack --clean", want: []string{"deleted " + filepath.Join(h.shared, "hf-rec.tar.gz"), "2 files deleted"}},
	}

	// The script holds every line of the run-book's Part 1, and only those.
	var script []string
	for _, s := range steps {
		script = append(script, s.line)
	}
	for _, l := range runBookLines(t) {
		if _, ok := notCommands[l]; !ok && !slices.Contains(script, l) {
			t.Errorf("the run-book's line %q is not run here", l)
		}
	}
	book := runBookLines(t)
	for _, l := range script {
		if !slices.Contains(book, l) {
			t.Errorf("the line %q is not the run-book's", l)
		}
	}
	for l := range notCommands {
		if !slices.Contains(book, l) {
			t.Errorf("%q is no longer in the run-book", l)
		}
	}

	for i, s := range steps {
		if s.before != nil {
			s.before()
		}
		code, out, errOut := h.run(t, s.stdin, words(s.line, h)...)
		if s.after != nil {
			s.after(out)
		}
		if code != s.code {
			t.Errorf("step %d, %s: exit %d, want %d\n%s%s", i, s.line, code, s.code, out, errOut)
		}
		for _, w := range s.want {
			if !strings.Contains(out+errOut, w) {
				t.Errorf("step %d, %s: does not say %q:\n%s%s", i, s.line, w, out, errOut)
			}
		}
	}
	// What setup made, the hand-over left behind, and the recording.
	for _, p := range []string{filepath.Join(h.home, "hf-private"), filepath.Join(h.home, "scratch", "alpha-orchard"), filepath.Join(h.home, "scratch", "beta-lantern"),
		filepath.Join(h.home, "scratch", "outside"), filepath.Join(h.home, "scratch", "long-notes", "AGENTS.md"), filepath.Join(h.home, "hf", "marketplace")} {
		if _, err := os.Stat(p); err != nil {
			t.Errorf("setup did not make %s", p)
		}
	}
	for _, p := range []string{filepath.Join(h.shared, "hf-rec.tar.gz"), filepath.Join(h.shared, "hf-rec.tar.gz.sha256"), filepath.Join(h.shared, publicKeyOut)} {
		if _, err := os.Stat(p); !os.IsNotExist(err) {
			t.Errorf("%s is left in the shared folder", p)
		}
	}
	all := strings.ToLower(strings.Join(slices.Collect(mapValues(h.recFiles(t))), "\n"))
	if fingerprint == "" {
		t.Error("push showed no fingerprint")
	}
	// The fingerprint was shown on screen only, and scan's answers are
	// kept nowhere.
	for _, bad := range []string{"hfeval", "hf eval home", strings.ToLower(h.home), "git.madeup-forge.test", fakeOther.Short, strings.ToLower(fingerprint),
		"made.up.owner@madeup-mail.net", "quinn realname", "orchardco"} {
		if strings.Contains(all, bad) {
			t.Errorf("the recording holds %q", bad)
		}
	}
}
