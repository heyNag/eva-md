package hostfake

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

const contentsChanged = "plugin contents changed; hook approval required"

// A first install leaves Codex's hooks untrusted and unrun until Approve;
// then they run from the copy.
func TestCodexHooksWaitForApproval(t *testing.T) {
	h := newHost(t, Codex)
	wantTrust(t, h.f, Untrusted)
	for _, r := range h.fire(t, "SessionStart") {
		wantNotRun(t, r, Skipped, "untrusted")
	}
	h.approve(t)
	wantTrust(t, h.f, Trusted)
	fired := h.fire(t, "SessionStart")
	if len(fired) != 1 {
		t.Fatalf("SessionStart fired %d hooks, want 1", len(fired))
	}
	wantRan(t, fired[startKey], "v1 start")
	if got := string(fired[startKey].Stdout); !strings.Contains(got, `"hook_event_name":"SessionStart"`) {
		t.Errorf("the hook was not given the payload on stdin: %q", got)
	}
	if fired[startKey].Timeout != 10 {
		t.Errorf("the declared timeout was recorded as %d, want 10", fired[startKey].Timeout)
	}
}

// A stray .DS_Store in the approved copy gives modified, and Fire fails
// every hook with Codex's words.
func TestStrayFileInTheCopyUnapproves(t *testing.T) {
	h := newHost(t, Codex)
	h.approve(t)
	write(t, filepath.Join(h.cache(), ".DS_Store"), "made-up\n")
	wantTrust(t, h.f, Modified)
	for _, event := range []string{"SessionStart", "Stop"} {
		for _, r := range h.fire(t, event) {
			wantNotRun(t, r, Failed, contentsChanged)
		}
	}
}

// Installing the same bytes again restores trust, with no review.
func TestTheSameBytesAgainRestoreTrust(t *testing.T) {
	h := newHost(t, Codex)
	h.approve(t)
	write(t, filepath.Join(h.cache(), ".DS_Store"), "made-up\n")
	wantTrust(t, h.f, Modified)
	h.install(t)
	wantTrust(t, h.f, Trusted)
	wantRan(t, h.fire(t, "Stop")[stopKey], "v1 stop")
}

// Files the hash passes over keep trust.
func TestSkippedNamesKeepTrust(t *testing.T) {
	h := newHost(t, Codex)
	h.approve(t)
	for _, name := range []string{"hooks/__pycache__/hook.cpython-312.pyc", "skills/recall/helper.pyc", ".venv/bin/python", "made_up.egg-info/PKG-INFO"} {
		write(t, filepath.Join(h.cache(), filepath.FromSlash(name)), "made-up\n")
	}
	wantTrust(t, h.f, Trusted)
	wantRan(t, h.fire(t, "SessionStart")[startKey], "v1 start")
}

// A symlink in the copy fails the hash: every hook stops, and Approve
// can't approve the copy.
func TestSymlinkStopsEveryHook(t *testing.T) {
	h := newHost(t, Codex)
	h.approve(t)
	if err := os.Symlink("SKILL.md", filepath.Join(h.cache(), "skills", "recall", "link.md")); err != nil {
		t.Fatal(err)
	}
	wantTrust(t, h.f, Modified)
	for _, event := range []string{"SessionStart", "Stop"} {
		fired := h.fire(t, event)
		if len(fired) != 1 {
			t.Fatalf("%s fired %d hooks, want 1", event, len(fired))
		}
		for _, r := range fired {
			wantNotRun(t, r, Failed, "unsupported plugin entry type: skills/recall/link.md")
		}
	}
	if err := h.f.Approve(Codex); err == nil {
		t.Error("Approve approved a copy whose contents can't be hashed")
	}
	if _, err := CodexContentHash(h.cache()); err == nil {
		t.Error("CodexContentHash hashed a folder with a symlink")
	}
}

// A changed timeout, or a group inserted above eva-md's, needs review. At
// 0.163.0 the contents are not bound, so only the declaration decides: the
// Stop hook, unchanged, still runs.
func TestChangedDeclarationNeedsReview(t *testing.T) {
	inserted := func(h *host) string {
		data, err := os.ReadFile(filepath.Join(h.plugin, "hooks", "hooks.json"))
		if err != nil {
			t.Fatal(err)
		}
		other := `"SessionStart": [{"hooks": [{"type": "command", "command": "echo another tool", "timeout": 10}]}, `
		return strings.Replace(string(data), `"SessionStart": [`, other, 1)
	}
	for name, edit := range map[string]func(h *host) string{
		"timeout": func(h *host) string { return hooksJSON(Codex, h.hookBin, 30) },
		"group":   inserted,
	} {
		t.Run(name, func(t *testing.T) {
			h := newHost(t, Codex)
			if err := h.f.SetVersion(Codex, "0.163.0"); err != nil {
				t.Fatal(err)
			}
			h.approve(t)
			write(t, filepath.Join(h.plugin, "hooks", "hooks.json"), edit(h))
			h.install(t)
			got := hooksList(t, h.f)
			if got[stopKey] != Trusted {
				t.Errorf("the unchanged Stop hook is %s", got[stopKey])
			}
			wantRan(t, h.fire(t, "Stop")[stopKey], "v1 stop")
			starts := h.fire(t, "SessionStart")
			if len(starts) != len(got)-1 {
				t.Fatalf("SessionStart fired %d hooks, hooks/list has %d", len(starts), len(got)-1)
			}
			for key, r := range starts {
				if got[key] == Trusted {
					t.Errorf("%s is trusted after its declaration moved or changed", key)
				}
				wantNotRun(t, r, Skipped, "needs review")
			}
			if name == "group" && got["eva-md@eva-md:hooks/hooks.json:SessionStart:1:0"] != Untrusted {
				t.Errorf("eva-md's hook, moved to group 1, is %q, want untrusted: hooks/list %v", got["eva-md@eva-md:hooks/hooks.json:SessionStart:1:0"], got)
			}
		})
	}
}

// A new binary behind an unchanged command keeps trust, and the new binary
// runs.
func TestNewBinaryBehindTheCommandKeepsTrust(t *testing.T) {
	h := newHost(t, Codex)
	h.approve(t)
	h.writeHookBin(t, "v2")
	wantTrust(t, h.f, Trusted)
	wantRan(t, h.fire(t, "SessionStart")[startKey], "v2 start")
}

// A plugin folder edited but not added again still runs the old copy, with
// its trust kept.
func TestAnEditedFolderNotAddedRunsTheOldCopy(t *testing.T) {
	h := newHost(t, Codex)
	h.approve(t)
	write(t, filepath.Join(h.plugin, "hooks", "hooks.json"), strings.ReplaceAll(hooksJSON(Codex, h.hookBin, 10), " start", " edited"))
	write(t, filepath.Join(h.plugin, "skills", "recall", "SKILL.md"), "an edited made-up skill\n")
	wantTrust(t, h.f, Trusted)
	wantRan(t, h.fire(t, "SessionStart")[startKey], "v1 start")
}

// At 0.163.0 a change to the copy keeps trust; at 0.164.0 it doesn't.
func TestContentsAreBoundFrom0_164_0(t *testing.T) {
	for version, bound := range map[string]bool{"0.163.0": false, "0.164.0": true} {
		t.Run(version, func(t *testing.T) {
			h := newHost(t, Codex)
			if err := h.f.SetVersion(Codex, version); err != nil {
				t.Fatal(err)
			}
			h.approve(t)
			write(t, filepath.Join(h.cache(), "skills", "recall", "SKILL.md"), "a changed made-up skill\n")
			if !bound {
				wantTrust(t, h.f, Trusted)
				wantRan(t, h.fire(t, "SessionStart")[startKey], "v1 start")
				return
			}
			wantTrust(t, h.f, Modified)
			wantNotRun(t, h.fire(t, "SessionStart")[startKey], Failed, contentsChanged)
		})
	}
}

// Claude Code asks for no approval and loads its plugin in place: an edited
// hook runs at the next Fire, unapproved.
func TestClaudeCodeRunsAnEditedHookWithoutApproval(t *testing.T) {
	h := newHost(t, Claude)
	wantRan(t, h.fire(t, "SessionStart")[startKey], "v1 start")
	edited := strings.ReplaceAll(hooksJSON(Claude, h.hookBin, 30), `"start"`, `"edited"`)
	write(t, filepath.Join(h.plugin, "hooks", "hooks.json"), edited)
	write(t, filepath.Join(h.plugin, ".DS_Store"), "made-up\n")
	fired := h.fire(t, "SessionStart")
	wantRan(t, fired[startKey], "v1 edited")
	if fired[startKey].Timeout != 30 {
		t.Errorf("the edited timeout was recorded as %d", fired[startKey].Timeout)
	}
	if err := h.f.Approve(Claude); err != nil {
		t.Errorf("Approve on Claude Code: %v", err)
	}
}

// No command grants trust, and --dangerously-bypass-hook-trust is refused
// and recorded.
func TestNoCommandGrantsTrust(t *testing.T) {
	h := newHost(t, Codex)
	for _, args := range [][]string{
		{"plugin", "add", "eva-md@eva-md", "--json"},
		{"plugin", "list", "--json", "--marketplace=eva-md"},
		{"plugin", "marketplace", "list", "--json"},
		{"features", "list"},
		{"--version"},
	} {
		h.run(t, args...)
	}
	_, err := h.f.Run(t.Context(), Codex, "--dangerously-bypass-hook-trust", "exec", "made-up prompt")
	if e, ok := err.(*ExitError); !ok || e.Code != 2 || !strings.Contains(e.Stderr, "refused") {
		t.Errorf("the bypass flag gave %v", err)
	}
	wantTrust(t, h.f, Untrusted)
	calls, err := h.f.Calls()
	if err != nil {
		t.Fatal(err)
	}
	refused := 0
	for _, c := range calls {
		bypass := len(c.Args) > 0 && c.Args[0] == "--dangerously-bypass-hook-trust"
		if c.Refused != bypass {
			t.Errorf("%s was recorded with refused %t", c, c.Refused)
		}
		if c.Refused {
			refused++
		}
	}
	if refused != 1 {
		t.Errorf("%d calls were recorded as refused, want 1", refused)
	}
}

// CodexContentHash covers each file's path, size and bytes, and nothing of
// a folder without files.
func TestCodexContentHash(t *testing.T) {
	dir := realTemp(t)
	write(t, filepath.Join(dir, "a", "b.txt"), "made-up\n")
	write(t, filepath.Join(dir, "a.txt"), "x")
	base, err := CodexContentHash(dir)
	if err != nil || !strings.HasPrefix(base, "sha256:") {
		t.Fatalf("hash %q, %v", base, err)
	}
	if again, _ := CodexContentHash(dir); again != base {
		t.Error("the hash is not the same twice")
	}
	if err := os.Mkdir(filepath.Join(dir, "empty"), 0o700); err != nil {
		t.Fatal(err)
	}
	if same, _ := CodexContentHash(dir); same != base {
		t.Error("an empty folder changed the hash")
	}
	changes := map[string]func(){
		"bytes": func() { write(t, filepath.Join(dir, "a.txt"), "y") },
		"size":  func() { write(t, filepath.Join(dir, "a.txt"), "xx") },
		"path":  func() { os.Rename(filepath.Join(dir, "a.txt"), filepath.Join(dir, "c.txt")) },
	}
	for name, change := range changes {
		change()
		if got, _ := CodexContentHash(dir); got == base {
			t.Errorf("a change of %s left the hash as it was", name)
		}
		os.Remove(filepath.Join(dir, "c.txt"))
		write(t, filepath.Join(dir, "a.txt"), "x")
	}
	if got, _ := CodexContentHash(dir); got != base {
		t.Fatal("the folder was not restored")
	}
}
