package main

import (
	"bufio"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/heyNag/eva-md/internal/hostfacts"
)

// The recorder's commits are made by this identity, which no one has.
const (
	pushName  = "eva.md (hf-test)"
	pushEmail = "hf-test@machines.eva-md.invalid"
)

// madeKeys are the keys hostrec made, in the private folder: their public
// halves and fingerprints, which scan counts and does not show.
type madeKeys struct {
	Keys []string `json:"keys"`
}

func (e *env) addKey(values ...string) error {
	path := filepath.Join(e.private, keysFile)
	var k madeKeys
	if data, err := os.ReadFile(path); err == nil {
		_ = json.Unmarshal(data, &k)
	}
	for _, v := range values {
		if v = strings.TrimSpace(v); v != "" {
			k.Keys = append(k.Keys, v)
		}
	}
	return writeJSON(path, k, 0o600)
}

// keyParts gives a public key's line and its fingerprint, as the keys
// hostrec made are counted by scan.
func keyParts(pub string) []string {
	data, err := os.ReadFile(pub)
	if err != nil {
		return nil
	}
	fields := strings.Fields(string(data))
	var out []string
	if len(fields) >= 2 {
		out = append(out, fields[1])
	}
	if fp, err := exec.Command("ssh-keygen", "-l", "-E", "sha256", "-f", pub).Output(); err == nil {
		if f := strings.Fields(string(fp)); len(f) >= 2 {
			out = append(out, f[1])
		}
	}
	return out
}

func cmdKey(c *cli, args []string) int {
	if len(args) != 0 {
		return c.fail("usage: hostrec key")
	}
	e, code := c.needEnv()
	if e == nil {
		return code
	}
	key := filepath.Join(e.home, filepath.FromSlash(deployKey))
	if _, err := os.Lstat(key); err == nil {
		return c.fail("the deploy key %s is made already; hostrec key makes it once", key)
	}
	if err := os.MkdirAll(filepath.Dir(key), 0o700); err != nil {
		return c.fail("%v", err)
	}
	if out, err := exec.Command("ssh-keygen", "-q", "-t", "ed25519", "-N", "", "-C", "hf-eval-made-up", "-f", key).CombinedOutput(); err != nil {
		return c.fail("ssh-keygen: %v: %s", err, strings.TrimSpace(string(out)))
	}
	pub, err := os.ReadFile(key + ".pub")
	if err != nil {
		return c.fail("%v", err)
	}
	if err := e.addKey(keyParts(key + ".pub")...); err != nil {
		return c.fail("keeping the key's name: %v", err)
	}
	out := filepath.Join(sharedDir, publicKeyOut)
	if err := os.WriteFile(out, pub, 0o644); err != nil {
		return c.fail("writing the public half: %v", err)
	}
	_ = os.Chmod(out, 0o644)
	_ = e.command("key", map[string]any{"made": true, "public_half_shared": true})
	c.say("hostrec: made the deploy key in this account, and wrote its public half to %s.", out)
	return 0
}

// scpAddress is git's scp-like SSH address, user@host:path.
var scpAddress = regexp.MustCompile(`^(?:[A-Za-z0-9._-]+@)?([A-Za-z0-9.-]+):([^/][^:]*)$`)

// parseRemote reads an SSH address, as ssh://[user@]host[:port]/path or
// [user@]host:path, into its parts.
func parseRemote(addr string) (hostfacts.Remote, error) {
	r := hostfacts.Remote{Address: addr}
	var path string
	if rest, ok := strings.CutPrefix(addr, "ssh://"); ok {
		hostPart, p, ok := strings.Cut(rest, "/")
		if !ok {
			return r, errors.New("the address names no repository")
		}
		if i := strings.LastIndexByte(hostPart, '@'); i >= 0 {
			hostPart = hostPart[i+1:]
		}
		host, port, _ := strings.Cut(hostPart, ":")
		r.Host, r.Port, path = host, port, p
	} else if m := scpAddress.FindStringSubmatch(addr); m != nil {
		r.Host, path = m[1], m[2]
	} else {
		return r, errors.New("the address is not an SSH address")
	}
	path = strings.Trim(path, "/")
	if r.Host == "" || path == "" {
		return r, errors.New("the address is not an SSH address")
	}
	r.Path = strings.TrimSuffix(path, ".git")
	if i := strings.LastIndexByte(r.Path, '/'); i >= 0 {
		r.Namespace = r.Path[:i]
	}
	return r, nil
}

// repoName is the repository's own name, the last part of its path.
func repoName(r hostfacts.Remote) string { return r.Path[strings.LastIndexByte(r.Path, '/')+1:] }

func cmdRemote(c *cli, args []string) int {
	if len(args) == 0 || args[0] != "set" {
		if len(args) > 0 && args[0] == "read" {
			return c.fail("remote read belongs to Part 2 of the recorder, which this build does not have yet")
		}
		return c.fail("usage: hostrec remote set")
	}
	if len(args) != 1 {
		return c.fail("usage: hostrec remote set")
	}
	e, code := c.needEnv()
	if e == nil {
		return code
	}
	data, err := os.ReadFile(filepath.Join(sharedDir, addressFile))
	if err != nil {
		return c.fail("could not read %s: save the scratch repository's address there first (G-02)", filepath.Join(sharedDir, addressFile))
	}
	addr := strings.TrimSpace(string(data))
	if strings.ContainsAny(addr, "\n\r\t ") {
		return c.fail("the address file holds more than one address")
	}
	r, err := parseRemote(addr)
	if err != nil {
		return c.fail("%v", err)
	}
	if name := repoName(r); name != scratchRepo {
		return c.fail("the address names the repository %q, not %s; it must be the scratch repository's, never your wiki's", name, scratchRepo)
	}
	c.say("The address names the repository %s, at %s.", scratchRepo, r.Path)
	fmt.Fprint(c.out, "Is that the scratch repository you made? Type y to store it: ")
	answer, _ := bufio.NewReader(c.in).ReadString('\n')
	if strings.TrimSpace(answer) != "y" {
		return c.fail("not confirmed; nothing was stored")
	}
	e.names.Remote = &r
	if err := writeJSON(filepath.Join(e.private, namesFile), e.names, 0o600); err != nil {
		return c.fail("storing the address: %v", err)
	}
	e.repl = hostfacts.NewReplacer(e.names)
	deleted := false
	pub := filepath.Join(sharedDir, publicKeyOut)
	if err := os.Remove(pub); err == nil {
		deleted = true
	} else if !errors.Is(err, fs.ErrNotExist) {
		c.say("could not delete %s: %v", pub, err)
	}
	_ = e.command("remote set", map[string]any{"stored": true, "deleted_public_half": deleted})
	c.say("hostrec: stored the address in the private folder.")
	if deleted {
		c.say("Deleted %s. The address file stays; your own account deletes it in G-05.", pub)
	}
	return 0
}
