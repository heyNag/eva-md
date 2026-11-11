package main

import (
	"context"
	"errors"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"time"
)

// The classes a push's outcome is kept as: never the server's words.
const (
	pushAccepted   = "accepted"
	pushCommitter  = "refused for the committer"
	pushSignature  = "refused for the signature"
	pushProtected  = "not settled: a protected branch"
	pushBranchRule = "not settled: a branch-name rule"
	pushKey        = "not settled: the key"
	pushNoAnswer   = "not settled: no answer from the server"
	pushOther      = "not settled: another refusal"
)

// The branches the two commits are pushed to, each by itself.
const (
	branchUnsigned = "hf-check-unsigned"
	branchSigned   = "hf-check-signed"
)

var (
	keyRefusal       = regexp.MustCompile(`(?i)permission denied|publickey|host key verification failed|could not read from remote|access denied|not authorized|unauthorized|deploy key`)
	noAnswer         = regexp.MustCompile(`(?i)could not resolve|connection (timed out|refused|reset|closed)|no route to host|network is unreachable|operation timed out|kex_exchange|broken pipe`)
	signatureRefusal = regexp.MustCompile(`(?i)signature|signed|gpg`)
	committerRefusal = regexp.MustCompile(`(?i)committer|author|e-?mail|identity`)
	protectedRefusal = regexp.MustCompile(`(?i)protected`)
	branchRefusal    = regexp.MustCompile(`(?i)branch name|ref ?name|naming|not allowed to (create|push) (a )?branch|refs/heads.*(not allowed|forbidden)`)
)

// pushClass sorts a push's outcome by the server's answer, which is read
// here and kept nowhere.
func pushClass(exit int, output string) string {
	switch {
	case exit == 0:
		return pushAccepted
	case noAnswer.MatchString(output):
		return pushNoAnswer
	case keyRefusal.MatchString(output) && !strings.Contains(output, "remote rejected") && !strings.Contains(output, "pre-receive"):
		return pushKey
	case protectedRefusal.MatchString(output):
		return pushProtected
	case committerRefusal.MatchString(output):
		return pushCommitter
	case signatureRefusal.MatchString(output):
		return pushSignature
	case branchRefusal.MatchString(output):
		return pushBranchRule
	case strings.Contains(output, "rejected") || strings.Contains(output, "denied") || strings.Contains(output, "declined"):
		return pushOther
	}
	return pushNoAnswer
}

func cmdPush(c *cli, args []string) int {
	if len(args) != 0 {
		return c.fail("usage: hostrec push")
	}
	e, code := c.needEnv()
	if e == nil {
		return code
	}
	if e.names.Remote == nil {
		return c.fail("no address is stored; run hostrec remote set first (G-03)")
	}
	addr := e.names.Remote.Address
	key := filepath.Join(e.home, filepath.FromSlash(deployKey))
	if _, err := os.Stat(key); err != nil {
		return c.fail("no deploy key; run hostrec key first (G-01)")
	}
	known := filepath.Join(e.private, knownHosts)
	firstContact := true
	if info, err := os.Stat(known); err == nil && info.Size() > 0 {
		firstContact = false
	}
	work, err := os.MkdirTemp(e.private, "push-")
	if err != nil {
		return c.fail("%v", err)
	}
	defer os.RemoveAll(work)
	// The deploy key is given per call, with no agent and no config file;
	// the server's host key goes to the recorder's own file only.
	sshCommand := "ssh -F /dev/null -i " + shellQuote(key) + " -o IdentitiesOnly=yes -o IdentityAgent=none" +
		" -o UserKnownHostsFile=" + shellQuote(known) + " -o StrictHostKeyChecking=accept-new -o BatchMode=yes"
	env := append(gitEnv(), "GIT_SSH_COMMAND="+sshCommand)
	git := func(args ...string) (string, int) {
		ctx, cancel := context.WithTimeout(context.Background(), hostTimeout)
		defer cancel()
		cmd := exec.CommandContext(ctx, "git", append([]string{"-C", work}, args...)...)
		cmd.Env = env
		cmd.WaitDelay = time.Second
		out, err := cmd.CombinedOutput()
		var exitErr *exec.ExitError
		switch {
		case err == nil:
			return string(out), 0
		case errors.As(err, &exitErr) && exitErr.ExitCode() > 0:
			return string(out), exitErr.ExitCode()
		}
		return string(out), 255
	}
	result := map[string]any{"first_contact": firstContact}
	defer func() { _ = e.command("push", result) }()
	if _, code := git("init", "-q"); code != 0 {
		return c.fail("git init failed")
	}
	out, code := git("fetch", "-q", "--depth=1", addr, "HEAD")
	if fp := fingerprints(known); firstContact && fp != "" {
		c.say("The server's host key, now in the recorder's own known-hosts file only:\n%s", fp)
	}
	if code != 0 {
		result["fetch"] = pushClass(code, out)
		return c.fail("fetching the default branch failed: %s", result["fetch"])
	}
	result["fetch"] = "fetched"
	sign := filepath.Join(work, "signing")
	if out, err := exec.Command("ssh-keygen", "-q", "-t", "ed25519", "-N", "", "-C", "hf-test-signing", "-f", sign).CombinedOutput(); err != nil {
		return c.fail("making the signing key: %v: %s", err, strings.TrimSpace(string(out)))
	}
	_ = e.addKey(keyParts(sign + ".pub")...)
	ident := []string{"-c", "user.name=" + pushName, "-c", "user.email=" + pushEmail}
	commit := func(signed bool, file, text string) (string, error) {
		if _, code := git("checkout", "-q", "--detach", "FETCH_HEAD"); code != 0 {
			return "", errors.New("checking out the default branch failed")
		}
		if err := os.WriteFile(filepath.Join(work, file), []byte(text), 0o644); err != nil {
			return "", err
		}
		if _, code := git("add", file); code != 0 {
			return "", errors.New("git add failed")
		}
		args := append([]string{}, ident...)
		if signed {
			// The signing key is passed here, never read from the
			// account's git config, which gitEnv shuts out too.
			args = append(args, "-c", "gpg.format=ssh", "-c", "gpg.ssh.program=ssh-keygen", "-c", "user.signingkey="+sign+".pub", "-c", "commit.gpgsign=true")
		} else {
			args = append(args, "-c", "commit.gpgsign=false")
		}
		args = append(args, "commit", "-q", "-m", "Made-up check commit from the host sittings")
		if signed {
			args = append(args, "-S")
		}
		if out, code := git(args...); code != 0 {
			return "", errors.New("the commit failed: " + firstLine(out))
		}
		sha, code := git("rev-parse", "HEAD")
		if code != 0 {
			return "", errors.New("git rev-parse failed")
		}
		return strings.TrimSpace(sha), nil
	}
	unsigned, err := commit(false, "hf-check-unsigned.txt", "Made-up check file. Nothing here is real.\n")
	if err != nil {
		return c.fail("%v", err)
	}
	signed, err := commit(true, "hf-check-signed.txt", "Made-up check file, signed. Nothing here is real.\n")
	if err != nil {
		return c.fail("%v", err)
	}
	for _, p := range []struct{ name, sha, branch string }{{"unsigned", unsigned, branchUnsigned}, {"signed", signed, branchSigned}} {
		out, code := git("push", "-q", addr, p.sha+":refs/heads/"+p.branch)
		result[p.name] = pushClass(code, out)
		c.say("The %s commit, pushed by itself to %s: %s.", p.name, p.branch, result[p.name])
	}
	out, code = git("ls-remote", addr, "refs/heads/"+branchUnsigned, "refs/heads/"+branchSigned)
	if code == 0 {
		var seen []string
		for _, b := range []string{branchUnsigned, branchSigned} {
			if strings.Contains(out, "refs/heads/"+b) {
				seen = append(seen, b)
			}
		}
		result["branches_read_back"] = seen
		c.say("Read back: %s.", orNoneList(seen))
	} else {
		result["branches_read_back"] = "no answer"
	}
	return 0
}

func orNoneList(s []string) string {
	if len(s) == 0 {
		return "no branch"
	}
	return strings.Join(s, " and ")
}

func firstLine(s string) string {
	line, _, _ := strings.Cut(strings.TrimSpace(s), "\n")
	return line
}

// fingerprints gives the fingerprints of the host keys in a known-hosts
// file, for the screen only.
func fingerprints(known string) string {
	if _, err := os.Stat(known); errors.Is(err, fs.ErrNotExist) {
		return ""
	}
	out, err := exec.Command("ssh-keygen", "-l", "-E", "sha256", "-f", known).Output()
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(out))
}
