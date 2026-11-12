package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/heyNag/eva-md/internal/hostfacts"
)

func TestParseRemote(t *testing.T) {
	for in, want := range map[string]hostfacts.Remote{
		"git@git.madeup-forge.test:pears/hf-scratch-plum.git":          {Host: "git.madeup-forge.test", Namespace: "pears", Path: "pears/hf-scratch-plum"},
		"ssh://git@git.madeup-forge.test:2222/a/b/hf-scratch-plum.git": {Host: "git.madeup-forge.test", Port: "2222", Namespace: "a/b", Path: "a/b/hf-scratch-plum"},
		"ssh://git.madeup-forge.test/hf-scratch-plum":                  {Host: "git.madeup-forge.test", Path: "hf-scratch-plum"},
		"madeup-forge:group/hf-scratch-plum.git":                       {Host: "madeup-forge", Namespace: "group", Path: "group/hf-scratch-plum"},
	} {
		got, err := parseRemote(in)
		want.Address = in
		if err != nil || got != want || repoName(got) != scratchRepo {
			t.Errorf("%s: %+v %v", in, got, err)
		}
	}
	for _, bad := range []string{"https://git.madeup-forge.test/pears/hf-scratch-plum.git", "/srv/pears/hf-scratch-plum.git", "ssh://git.madeup-forge.test", ""} {
		if _, err := parseRemote(bad); err == nil {
			t.Errorf("%q was taken as an SSH address", bad)
		}
	}
}

func TestKeyAndRemoteSet(t *testing.T) {
	h := newHome(t)
	h.setup(t)
	h.must(t, "", "mark", "G-01")
	h.must(t, "", "key")
	key := filepath.Join(h.home, ".ssh", "hf_scratch_deploy")
	pub := filepath.Join(h.shared, publicKeyOut)
	for _, p := range []string{key, key + ".pub", pub} {
		if _, err := os.Stat(p); err != nil {
			t.Fatal(err)
		}
	}
	if data, _ := os.ReadFile(pub); !strings.Contains(string(data), "hf-eval-made-up") || strings.Contains(string(data), "@") {
		t.Errorf("the public half's comment: %s", data)
	}
	if code, _, _ := h.run(t, "", "key"); code == 0 {
		t.Error("a second key was made over the first")
	}
	address := filepath.Join(h.shared, addressFile)
	names := filepath.Join(h.home, "hf-private", namesFile)
	for _, c := range []struct{ address, answer, want string }{
		{"git@git.madeup-forge.test:pears/my-real-wiki.git", "y\n", "not hf-scratch-plum"},
		{"https://git.madeup-forge.test/pears/hf-scratch-plum.git", "y\n", "not an SSH address"},
		{madeUpAddress, "n\n", "not confirmed"},
		{madeUpAddress, "", "not confirmed"},
	} {
		if err := os.WriteFile(address, []byte(c.address+"\n"), 0o644); err != nil {
			t.Fatal(err)
		}
		code, _, errOut := h.run(t, c.answer, "remote", "set")
		if code == 0 || !strings.Contains(errOut, c.want) {
			t.Errorf("%s answered %q: %d %q", c.address, c.answer, code, errOut)
		}
		if data, _ := os.ReadFile(names); strings.Contains(string(data), "remote") {
			t.Errorf("%s was stored", c.address)
		}
		if _, err := os.Stat(pub); err != nil {
			t.Errorf("a refused remote set deleted the public half")
		}
	}
	out := h.must(t, "y\n", "remote", "set")
	if !strings.Contains(out, scratchRepo) {
		t.Errorf("remote set does not show the name: %s", out)
	}
	if _, err := os.Stat(pub); !os.IsNotExist(err) {
		t.Error("the public half's file is still there")
	}
	if data, err := os.ReadFile(address); err != nil || strings.TrimSpace(string(data)) != madeUpAddress {
		t.Errorf("the address file was touched: %v", err)
	}
	var n hostfacts.Names
	data, _ := os.ReadFile(names)
	if json.Unmarshal(data, &n) != nil || n.Remote == nil || n.Remote.Host != "git.madeup-forge.test" {
		t.Errorf("the stored remote: %s", data)
	}
	if strings.Contains(strings.Join(slices.Collect(mapValues(h.recFiles(t))), ""), "madeup-forge") {
		t.Error("the recording holds the remote's host")
	}
	if _, err := os.Stat(filepath.Join(h.home, "hf-private", keysFile)); err != nil {
		t.Error("the key hostrec made is not kept for scan")
	}
}

func mapValues(m map[string]string) func(func(string) bool) {
	return func(yield func(string) bool) {
		for _, v := range m {
			if !yield(v) {
				return
			}
		}
	}
}

// A made-up git server: a bare repository with a README on main, whose
// pre-receive hook refuses a signed commit on hf-check-signed for its
// signature, and anything pushed to main.
func madeUpServer(t *testing.T, h *testHome) (server, bare, mainSHA string) {
	t.Helper()
	server = filepath.Join(filepath.Dir(h.home), "server")
	bare = filepath.Join(server, "pears", "hf-scratch-plum.git")
	seed := filepath.Join(filepath.Dir(h.home), "seed")
	for _, argv := range [][]string{{"init", "-q", "--bare", "-b", "main", bare}, {"init", "-q", "-b", "main", seed}} {
		if out, err := gitIn(filepath.Dir(h.home), argv...); err != nil {
			t.Fatalf("%v: %s", err, out)
		}
	}
	if err := os.WriteFile(filepath.Join(seed, "README.md"), []byte("Made-up scratch repository.\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	for _, argv := range [][]string{{"add", "README.md"}, {"-c", "user.name=Made Up", "-c", "user.email=made-up@example.invalid", "commit", "-q", "-m", "README"}, {"push", "-q", bare, "main"}} {
		if out, err := gitIn(seed, argv...); err != nil {
			t.Fatalf("%v: %s", err, out)
		}
	}
	hook := `#!/bin/sh
while read old new ref; do
  case "$ref" in
  refs/heads/main) echo "made-up rule: main is protected"; exit 1 ;;
  refs/heads/hf-check-signed)
    if git cat-file commit "$new" | grep -q '^gpgsig'; then echo "GL-HOOK-ERR: commit signature could not be verified"; else echo "made-up rule: refused"; fi
    exit 1 ;;
  esac
done
exit 0
`
	if err := os.WriteFile(filepath.Join(bare, "hooks", "pre-receive"), []byte(hook), 0o755); err != nil {
		t.Fatal(err)
	}
	out, err := gitIn(bare, "rev-parse", "main")
	if err != nil {
		t.Fatal(err)
	}
	return server, bare, strings.TrimSpace(string(out))
}

func TestPushSendsEachCommitByItselfWithTheDeployKey(t *testing.T) {
	h := newHome(t)
	h.setup(t)
	server, bare, mainSHA := madeUpServer(t, h)
	self, _ := os.Executable()
	if err := os.WriteFile(filepath.Join(h.bin, "ssh"), []byte("#!/bin/sh\nHOSTREC_FAKE=ssh exec "+shellQuote(self)+" \"$@\"\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("HOSTREC_FAKE_SERVER", server)
	// The account's own git config names a signer that must not be called.
	called := filepath.Join(filepath.Dir(h.home), "stand-in-called")
	standIn := filepath.Join(h.bin, "stand-in-signer")
	if err := os.WriteFile(standIn, []byte("#!/bin/sh\ntouch "+shellQuote(called)+"\nexit 1\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	accountConfig := "[gpg]\n\tformat = ssh\n[gpg \"ssh\"]\n\tprogram = " + standIn + "\n[user]\n\tsigningkey = /made/up/key.pub\n\tname = Made Up Account\n\temail = account@example.invalid\n[commit]\n\tgpgsign = true\n"
	for _, p := range []string{os.Getenv("GIT_CONFIG_GLOBAL"), filepath.Join(h.home, ".gitconfig")} {
		if err := os.WriteFile(p, []byte(accountConfig), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	h.must(t, "", "mark", "G-01")
	h.must(t, "", "key")
	if err := os.WriteFile(filepath.Join(h.shared, addressFile), []byte(madeUpAddress+"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	h.must(t, "y\n", "remote", "set")
	h.must(t, "", "mark", "G-04")
	out := h.must(t, "", "push")
	if !strings.Contains(out, "unsigned commit, pushed by itself to hf-check-unsigned: accepted") || !strings.Contains(out, "signed commit, pushed by itself to hf-check-signed: refused for the signature") {
		t.Errorf("push says:\n%s", out)
	}
	key := filepath.Join(h.home, ".ssh", "hf_scratch_deploy")
	known := filepath.Join(h.home, "hf-private", knownHosts)
	calls := 0
	for _, argv := range h.fakeLog(t) {
		if argv[0] != "ssh" {
			continue
		}
		calls++
		joined := strings.Join(argv, " ")
		for _, want := range []string{"-i " + key, "-o IdentitiesOnly=yes", "-o IdentityAgent=none", "-o UserKnownHostsFile=" + known, "-F /dev/null"} {
			if !strings.Contains(joined, want) {
				t.Errorf("ssh was started without %q: %v", want, argv)
			}
		}
	}
	if calls < 3 {
		t.Errorf("ssh was started %d times", calls)
	}
	if _, err := os.Stat(filepath.Join(h.home, ".ssh", "known_hosts")); !os.IsNotExist(err) {
		t.Error("the account's known_hosts was written")
	}
	if _, err := os.Stat(called); !os.IsNotExist(err) {
		t.Error("the account's signer was called")
	}
	refs, err := gitIn(bare, "for-each-ref", "--format=%(refname)")
	if err != nil {
		t.Fatal(err)
	}
	if got := strings.Fields(string(refs)); !slices.Equal(got, []string{"refs/heads/hf-check-unsigned", "refs/heads/main"}) {
		t.Errorf("the server's branches: %v", got)
	}
	if now, _ := gitIn(bare, "rev-parse", "main"); strings.TrimSpace(string(now)) != mainSHA {
		t.Error("the default branch moved")
	}
	log, err := gitIn(bare, "log", "--format=%P|%an <%ae>|%cn <%ce>", "hf-check-unsigned", "-1")
	if err != nil {
		t.Fatal(err)
	}
	if want := mainSHA + "|" + pushName + " <" + pushEmail + ">|" + pushName + " <" + pushEmail + ">"; strings.TrimSpace(string(log)) != want {
		t.Errorf("the unsigned commit: %s, want %s", log, want)
	}
	var result map[string]any
	for _, l := range strings.Split(strings.TrimSpace(h.recFiles(t)[hostfacts.OutputsFile]), "\n") {
		var o hostfacts.OutputLine
		if json.Unmarshal([]byte(l), &o) == nil && o.Command == "push" {
			result = o.Result
		}
	}
	if result["unsigned"] != pushAccepted || result["signed"] != pushSignature || result["first_contact"] != true {
		t.Errorf("the recorded outcome: %v", result)
	}
	if entries, _ := filepath.Glob(filepath.Join(h.home, "hf-private", "push-*")); len(entries) != 0 {
		t.Errorf("push left its work folder: %v", entries)
	}
}

func TestPushClasses(t *testing.T) {
	for out, want := range map[string]string{
		"": pushNoAnswer,
		"git@x: Permission denied (publickey).\nfatal: Could not read from remote repository.":                                                        pushKey,
		"ssh: Could not resolve hostname git.madeup-forge.test":                                                                                       pushNoAnswer,
		"remote: GitLab: You are not allowed to push code to protected branches on this project.\n ! [remote rejected] x (pre-receive hook declined)": pushProtected,
		"remote: GL-HOOK-ERR: Commit must be signed with a GPG key\n ! [remote rejected] x (pre-receive hook declined)":                               pushSignature,
		"remote: Committer email 'hf-test@machines.eva-md.invalid' is not verified.\n ! [remote rejected] x (pre-receive hook declined)":              pushCommitter,
		"remote: Branch name does not follow the pattern\n ! [remote rejected] x (pre-receive hook declined)":                                         pushBranchRule,
		"remote: made-up rule: refused\n ! [remote rejected] x (pre-receive hook declined)":                                                           pushOther,
	} {
		code := 1
		if want == pushAccepted {
			code = 0
		}
		if got := pushClass(code, out); got != want {
			t.Errorf("%q: %s, want %s", out, got, want)
		}
	}
	if pushClass(0, "anything") != pushAccepted {
		t.Error("a push that exits 0 is not accepted")
	}
}
