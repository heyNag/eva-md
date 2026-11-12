package main

import (
	"bytes"
	"encoding/hex"
	"encoding/json"
	"maps"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/heyNag/eva-md/internal/hostfacts"
)

// A payload's string under a model, plan or tier key, in any letter case,
// at the top, inside an object or inside a list, is written as its keyed
// hash, and so is a key inside such a value, as a model's name a map is
// keyed by: no marker is left in the recording, payload_hashes still joins
// the value to the row that holds it and names a hidden key as the kept
// payload writes it, and a version is kept as written.
func TestAPayloadNamesNoModel(t *testing.T) {
	h := newHome(t)
	h.setup(t)
	h.must(t, "", "mark", "C-04")
	markers := map[string]string{
		"model": "QM-TOP-MODEL", "Model": "QM-CAP-MODEL", "model_id": "QM-ID-MODEL", "plan": "QM-PLAN", "tier": "QM-TIER",
		"obj.model": "QM-OBJ-MODEL", "obj.Plan_type": "QM-OBJ-PLAN", "obj.deep.subscriptionTier": "QM-DEEP-TIER",
		"list.0.model": "QM-LIST-MODEL", "models.0": "QM-LIST-ITEM", "modelInfo.id": "QM-NESTED-ID",
	}
	keyMarkers := []string{"QM-KEY-MODEL", "qm-key-model-2"}
	tr := newTranscript(t, filepath.Join(h.home, ".claude", "projects", "-made-up", sessionID+".jsonl"))
	tr.add(map[string]any{"type": "assistant", "message": map[string]any{"role": "assistant", "model": markers["model"], "content": "made up"}})
	payload := map[string]any{
		"session_id": sessionID, "transcript_path": tr.path, "hook_event_name": "Stop", "version": "9.9.9-made-up",
		"model": markers["model"], "Model": markers["Model"], "model_id": markers["model_id"], "plan": markers["plan"], "tier": markers["tier"],
		"obj":             map[string]any{"model": markers["obj.model"], "Plan_type": markers["obj.Plan_type"], "deep": map[string]any{"subscriptionTier": markers["obj.deep.subscriptionTier"]}, "kept": "made-up kept"},
		"list":            []any{map[string]any{"model": markers["list.0.model"]}, "made-up kept"},
		"models":          []any{markers["models.0"]},
		"modelInfo":       map[string]any{"id": markers["modelInfo.id"]},
		"modelUsage":      map[string]any{keyMarkers[0]: map[string]any{"inputTokens": 5, "name": "made-up usage"}},
		"availableModels": map[string]any{keyMarkers[1]: "made-up enabled"},
	}
	h.hook(t, "claude-code", "Stop", payload)
	for name, text := range h.recFiles(t) {
		for _, m := range append(slices.Collect(maps.Values(markers)), keyMarkers...) {
			if strings.Contains(text, m) {
				t.Errorf("%s holds %s", name, m)
			}
		}
	}
	ev := h.events(t)[0]
	p := ev["payload"].(map[string]any)
	hashes := ev["payload_hashes"].(map[string]any)
	key, err := os.ReadFile(filepath.Join(h.home, privateName, keyFile))
	if err != nil {
		t.Fatal(err)
	}
	keyBytes, err := hex.DecodeString(strings.TrimSpace(string(key)))
	if err != nil {
		t.Fatal(err)
	}
	hidden := func(k string) string { return "#" + hostfacts.Hash(keyBytes, k) }
	markers["modelInfo."+hidden("id")] = markers["modelInfo.id"]
	delete(markers, "modelInfo.id")
	markers["modelUsage."+hidden(keyMarkers[0])+"."+hidden("name")] = "made-up usage"
	markers["availableModels."+hidden(keyMarkers[1])] = "made-up enabled"
	for path, m := range markers {
		want := hostfacts.Hash(keyBytes, m)
		if hashes[path] != want {
			t.Errorf("payload_hashes[%s] is %v, want the value's hash", path, hashes[path])
		}
	}
	usage, _ := p["modelUsage"].(map[string]any)[hidden(keyMarkers[0])].(map[string]any)
	if usage[hidden("inputTokens")] == nil || usage[hidden("name")] != hostfacts.Hash(keyBytes, "made-up usage") {
		t.Errorf("the payload kept a hidden map as %v", p["modelUsage"])
	}
	if p["model"] != hashes["model"] || p["version"] != "9.9.9-made-up" || p["obj"].(map[string]any)["kept"] != "made-up kept" || p["list"].([]any)[1] != "made-up kept" {
		t.Errorf("the payload kept: %v", p)
	}
	if got := fields(snapshotRows(ev)[0])["message.model"]["hash"]; got == nil || got != hashes["model"] {
		t.Errorf("the row's model hash %v does not join the payload's %v", got, hashes["model"])
	}
}

// A hook that could not take the recording's lock keeps no event, and its
// line in errors.jsonl names its host, its event and its via, so the
// sibling that lost can be told from the recording.
func TestALostLockNamesTheHook(t *testing.T) {
	h := newHome(t)
	h.setup(t)
	h.must(t, "", "mark", "C-02")
	f, err := os.OpenFile(filepath.Join(h.rec, lockFile), os.O_RDWR|os.O_CREATE, 0o600)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	if err := syscall.Flock(int(f.Fd()), syscall.LOCK_EX|syscall.LOCK_NB); err != nil {
		t.Fatal(err)
	}
	var out, errOut bytes.Buffer
	c := &cli{in: strings.NewReader(`{"session_id":"made-up","hook_event_name":"UserPromptSubmit"}`), out: &out, err: &errOut, lockWait: time.Nanosecond}
	if code := cmdHook(c, []string{"--host", "claude-code", "--event", "UserPromptSubmit", "--via", viaTimeoutCheck}); code != 0 || strings.TrimSpace(out.String()) != "{}" || errOut.Len() != 0 {
		t.Fatalf("the hook: %d %q %q", code, out.String(), errOut.String())
	}
	_ = syscall.Flock(int(f.Fd()), syscall.LOCK_UN)
	files := h.recFiles(t)
	if files[hostfacts.EventsFile] != "" {
		t.Error("a hook that lost the lock kept an event")
	}
	var line hostfacts.ErrorLine
	if err := json.Unmarshal([]byte(strings.TrimSpace(files[hostfacts.ErrorsFile])), &line); err != nil {
		t.Fatalf("errors.jsonl: %q %v", files[hostfacts.ErrorsFile], err)
	}
	if line.Where != "hook" || line.Host != "claude-code" || line.Event != "UserPromptSubmit" || line.Via != viaTimeoutCheck || !strings.Contains(line.Error, "lock") {
		t.Errorf("the lock's line: %+v", line)
	}
}

// setup and remote set warn when a name of this machine is also a word the
// recording keeps, and name the word; a machine whose names clash with none
// gets no warning.
func TestSetupAndRemoteSetWarnOfAClash(t *testing.T) {
	h := newHome(t)
	listAccounts = func(home string) (hostfacts.Account, []hostfacts.Account, error) {
		e := fakeEval
		e.Home = home
		return e, []hostfacts.Account{fakeOther, {Short: "user", Full: "Made Up User"}}, nil
	}
	// clashes gives the words a warning lists.
	clashes := func(out string) []string {
		_, rest, ok := strings.Cut(out, "WARNING: ")
		if !ok {
			return nil
		}
		lines := strings.Split(rest, "\n")
		if len(lines) < 2 {
			return nil
		}
		return strings.Split(strings.TrimPrefix(lines[1], "  "), ", ")
	}
	out := h.must(t, "", "setup", "--rec", h.rec)
	got := clashes(out)
	for _, w := range []string{"user", "user: ", "USER"} {
		if !slices.Contains(got, w) {
			t.Errorf("setup does not warn that the account named user clashes with %q:\n%s", w, out)
		}
	}
	if slices.Contains(got, "agent") {
		t.Errorf("setup warns of a group it was not given:\n%s", out)
	}
	if err := os.WriteFile(filepath.Join(h.shared, addressFile), []byte("git@git.madeup-forge.test:agent/hf-scratch-plum.git\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	out = h.must(t, "y\n", "remote", "set")
	if got := clashes(out); !slices.Contains(got, "user") || !slices.ContainsFunc(got, func(w string) bool { return strings.EqualFold(w, "agent") }) {
		t.Errorf("remote set does not warn of the group named agent:\n%s", out)
	}
	if !strings.Contains(h.recFiles(t)[hostfacts.OutputsFile], `"clashes":`) {
		t.Error("remote set keeps no count of the clashes")
	}

	plain := newHome(t)
	if out := plain.must(t, "", "setup", "--rec", plain.rec); strings.Contains(out, "WARNING") {
		t.Errorf("setup warns on a machine whose names clash with none:\n%s", out)
	}
	if err := os.WriteFile(filepath.Join(plain.shared, addressFile), []byte(madeUpAddress+"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if out := plain.must(t, "y\n", "remote", "set"); strings.Contains(out, "WARNING") {
		t.Errorf("remote set warns of a group that clashes with nothing:\n%s", out)
	}
}
