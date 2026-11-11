package main

import (
	"bufio"
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/heyNag/eva-md/internal/hostfacts"
	"github.com/heyNag/eva-md/internal/testgitenv"
)

// The made-up machine the tests run the recorder on: every name is made up.
var (
	fakeEval     = hostfacts.Account{Short: "hfevaltest", Full: "Hazel Evaltest"}
	fakeOther    = hostfacts.Account{Short: "ownerquill", Full: "Quill Ownerby", Home: "/Users/ownerquill"}
	fakeComputer = []string{"Quills-Orchard-Mac", "quills-orchard-mac.local"}
)

// TestMain raises the recorder's time limits, so no test depends on the
// machine's speed, and makes the test binary stand in for the hosts' CLIs
// and ssh when a test starts it under one of their names.
func TestMain(m *testing.M) {
	if fake := os.Getenv("HOSTREC_FAKE"); fake != "" {
		os.Exit(fakeHost(fake, os.Args[1:]))
	}
	testgitenv.Clear()
	hostTimeout = 10 * time.Minute
	trustTimeout = 10 * time.Minute
	lockWait = 10 * time.Minute
	startPause = 0
	os.Exit(m.Run())
}

// testHome is a made-up evaluation home, with the fake hosts on PATH.
type testHome struct {
	home, rec, bin, shared, log string
}

func newHome(t *testing.T) *testHome {
	t.Helper()
	root, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	h := &testHome{home: filepath.Join(root, "home"), bin: filepath.Join(root, "bin"), shared: filepath.Join(root, "shared"), log: filepath.Join(root, "fake.log")}
	h.rec = filepath.Join(h.home, "hf-rec")
	for _, d := range []string{h.home, h.bin, h.shared} {
		if err := os.MkdirAll(d, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	self, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"claude", "codex"} {
		script := "#!/bin/sh\nHOSTREC_FAKE=" + name + " exec " + shellQuote(self) + " \"$@\"\n"
		if err := os.WriteFile(filepath.Join(h.bin, name), []byte(script), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	gitconfig := filepath.Join(root, "gitconfig")
	if err := os.WriteFile(gitconfig, nil, 0o644); err != nil {
		t.Fatal(err)
	}
	t.Setenv("HOME", h.home)
	t.Setenv("GIT_CONFIG_GLOBAL", gitconfig)
	t.Setenv("GIT_CONFIG_NOSYSTEM", "1")
	t.Setenv("PATH", h.bin+string(os.PathListSeparator)+os.Getenv("PATH"))
	t.Setenv("HOSTREC_FAKE_LOG", h.log)
	t.Setenv("HOSTREC_FAKE_STATE", filepath.Join(root, "fake-state"))
	for _, v := range []string{"CLAUDE_CONFIG_DIR", "CODEX_HOME", "HOSTREC_FAKE_FAIL_JSON", "HOSTREC_FAKE_ASK", "HOSTREC_FAKE_TRUST"} {
		t.Setenv(v, "")
		os.Unsetenv(v)
	}
	oldShared, oldAccounts, oldNames := sharedDir, listAccounts, computerNames
	t.Cleanup(func() { sharedDir, listAccounts, computerNames = oldShared, oldAccounts, oldNames })
	sharedDir = h.shared
	listAccounts = func(home string) (hostfacts.Account, []hostfacts.Account, error) {
		e := fakeEval
		e.Home = home
		return e, []hostfacts.Account{fakeOther}, nil
	}
	computerNames = func() []string { return slices.Clone(fakeComputer) }
	return h
}

// run runs one hostrec command in this process.
func (h *testHome) run(t *testing.T, stdin string, args ...string) (int, string, string) {
	t.Helper()
	var out, errOut bytes.Buffer
	code := run(args, strings.NewReader(stdin), &out, &errOut)
	return code, out.String(), errOut.String()
}

// must runs a command that must succeed.
func (h *testHome) must(t *testing.T, stdin string, args ...string) string {
	t.Helper()
	code, out, errOut := h.run(t, stdin, args...)
	if code != 0 {
		t.Fatalf("hostrec %s: exit %d\n%s%s", strings.Join(args, " "), code, out, errOut)
	}
	return out
}

func (h *testHome) setup(t *testing.T) {
	t.Helper()
	h.must(t, "", "setup", "--rec", h.rec)
}

// hook runs the hook with a payload and gives its answer.
func (h *testHome) hook(t *testing.T, host, event string, payload any, via ...string) string {
	t.Helper()
	data, err := json.Marshal(payload)
	if err != nil {
		t.Fatal(err)
	}
	args := []string{"hook", "--host", host, "--event", event}
	if len(via) > 0 {
		args = append(args, "--via", via[0])
	}
	code, out, errOut := h.run(t, string(data), args...)
	if code != 0 || errOut != "" {
		t.Fatalf("the hook exited %d: %s", code, errOut)
	}
	return strings.TrimSpace(out)
}

// recFiles gives every file under the recording folder, by its name.
func (h *testHome) recFiles(t *testing.T) map[string]string {
	t.Helper()
	files := map[string]string{}
	err := filepath.WalkDir(h.rec, func(p string, d os.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return err
		}
		data, err := os.ReadFile(p)
		if err != nil {
			return err
		}
		rel, _ := filepath.Rel(h.rec, p)
		files[rel] = string(data)
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	return files
}

func (h *testHome) events(t *testing.T) []map[string]any {
	t.Helper()
	var out []map[string]any
	s := bufio.NewScanner(strings.NewReader(h.recFiles(t)[hostfacts.EventsFile]))
	s.Buffer(make([]byte, 64<<10), 64<<20)
	for s.Scan() {
		var v map[string]any
		if err := json.Unmarshal(s.Bytes(), &v); err != nil {
			t.Fatal(err)
		}
		out = append(out, v)
	}
	return out
}

// fakeLog gives the argument lists the fake hosts were started with.
func (h *testHome) fakeLog(t *testing.T) [][]string {
	t.Helper()
	data, err := os.ReadFile(h.log)
	if os.IsNotExist(err) {
		return nil
	}
	if err != nil {
		t.Fatal(err)
	}
	var out [][]string
	for _, l := range strings.Split(strings.TrimSpace(string(data)), "\n") {
		var argv []string
		if err := json.Unmarshal([]byte(l), &argv); err != nil {
			t.Fatal(err)
		}
		out = append(out, argv)
	}
	return out
}

// fakeHost stands in for claude, codex or ssh. It logs its arguments, and
// keeps the plugins it was asked to add in a state file.
func fakeHost(name string, args []string) int {
	argv := append([]string{name}, args...)
	if p := os.Getenv("HOSTREC_FAKE_LOG"); p != "" {
		line, _ := json.Marshal(argv)
		if f, err := os.OpenFile(p, os.O_WRONLY|os.O_APPEND|os.O_CREATE, 0o600); err == nil {
			f.Write(append(line, '\n'))
			f.Close()
		}
	}
	if name == "ssh" {
		return fakeSSH(args)
	}
	state := os.Getenv("HOSTREC_FAKE_STATE") + "-" + name
	plugins := func() []string {
		data, _ := os.ReadFile(state)
		return strings.Fields(string(data))
	}
	save := func(p []string) { os.WriteFile(state, []byte(strings.Join(p, "\n")), 0o600) }
	words := slices.DeleteFunc(slices.Clone(args), func(a string) bool { return strings.HasPrefix(a, "--") })
	switch {
	case slices.Equal(args, []string{"--version"}):
		fmt.Println("9.9.9 (made-up " + name + ")")
		return 0
	case len(args) >= 2 && args[0] == "--no-daemon" && args[1] == "app-server":
		return fakeAppServer()
	}
	if os.Getenv("HOSTREC_FAKE_FAIL_JSON") != "" && slices.Contains(args, "--json") && (slices.Contains(words, "install") || slices.Contains(words, "add")) {
		fmt.Println("error: unknown option '--json'")
		return 2
	}
	if os.Getenv("HOSTREC_FAKE_ASK") != "" && (slices.Contains(words, "install") || slices.Contains(words, "add")) {
		fmt.Println("Do you trust this marketplace? (y/N)")
		return 1
	}
	switch {
	case len(words) >= 2 && words[0] == "plugin" && words[1] == "list":
		out, _ := json.Marshal(map[string]any{"plugins": plugins()})
		fmt.Println(string(out))
	case len(words) >= 3 && words[0] == "plugin" && words[1] == "marketplace":
	case len(words) >= 3 && words[0] == "plugin" && (words[1] == "install" || words[1] == "add"):
		save(append(plugins(), words[2]))
	case len(words) >= 3 && words[0] == "plugin" && (words[1] == "uninstall" || words[1] == "remove"):
		save(slices.DeleteFunc(plugins(), func(p string) bool { return p == words[2] }))
	default:
		fmt.Println("the fake host does not know", strings.Join(args, " "))
		return 2
	}
	return 0
}

// fakeAppServer answers initialize and hooks/list as Codex's app server
// does, with the scratch hooks and one of another plugin.
func fakeAppServer() int {
	trust := os.Getenv("HOSTREC_FAKE_TRUST")
	if trust == "" {
		trust = "trusted"
	}
	s := bufio.NewScanner(os.Stdin)
	for s.Scan() {
		var req struct {
			ID     int    `json:"id"`
			Method string `json:"method"`
		}
		if json.Unmarshal(s.Bytes(), &req) != nil || req.ID == 0 {
			continue
		}
		var result any = map[string]any{}
		if req.Method == "hooks/list" {
			var hooks []map[string]any
			for _, ev := range []string{"sessionStart", "sessionStart", "userPromptSubmit", "stop"} {
				hooks = append(hooks, map[string]any{"eventName": ev, "pluginId": "hf-hooks@hf-scratch", "enabled": true, "trustStatus": trust})
			}
			hooks = append(hooks, map[string]any{"eventName": "stop", "pluginId": "eva-md@eva-md", "enabled": true, "trustStatus": "untrusted"})
			result = map[string]any{"data": []any{map[string]any{"hooks": hooks}}}
		}
		out, _ := json.Marshal(map[string]any{"id": req.ID, "result": result})
		fmt.Println(string(out))
		if req.Method == "hooks/list" {
			return 0
		}
	}
	return 0
}

// fakeSSH stands for the git server: it runs the git command it is given
// in the folder HOSTREC_FAKE_SERVER, where the bare repository is.
func fakeSSH(args []string) int {
	if len(args) == 0 {
		return 255
	}
	cmd := args[len(args)-1]
	sh := execCommand("sh", "-c", cmd)
	sh.Dir = os.Getenv("HOSTREC_FAKE_SERVER")
	sh.Stdin, sh.Stdout, sh.Stderr = os.Stdin, os.Stdout, os.Stderr
	if err := sh.Run(); err != nil {
		if code := exitCode(err); code > 0 {
			return code
		}
		return 255
	}
	return 0
}

func copyTree(t *testing.T, from, to string) {
	t.Helper()
	err := filepath.WalkDir(from, func(p string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel, _ := filepath.Rel(from, p)
		dst := filepath.Join(to, rel)
		if d.IsDir() {
			return os.MkdirAll(dst, 0o755)
		}
		in, err := os.Open(p)
		if err != nil {
			return err
		}
		defer in.Close()
		out, err := os.Create(dst)
		if err != nil {
			return err
		}
		defer out.Close()
		_, err = io.Copy(out, in)
		return err
	})
	if err != nil {
		t.Fatal(err)
	}
}
