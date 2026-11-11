package main

import (
	"bufio"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"os/exec"
	"os/user"
	"path/filepath"
	"runtime"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/heyNag/eva-md/internal/hostfacts"
)

// The scratch repositories' one commit is made by this made-up identity,
// set in each repository's own config.
const (
	scratchUser   = "HF Scratch"
	scratchEmail  = "scratch@example.invalid"
	projectWordMD = "Made-up project notes. The project word is WILLOW-CANDLE.\n"
)

// scratchFiles are the scratch folders' files, under HOME, exactly as the
// card gives them; the folders are made even when empty.
var scratchFiles = map[string]string{
	"scratch/alpha-orchard/CLAUDE.md":         projectWordMD,
	"scratch/alpha-orchard/orchard/plums.txt": "made-up plum list\ncount: 12\ncolour: purple\n",
	"scratch/alpha-orchard/orchard/pears.txt": "made-up pear list\ncount: 40\ncrate: wooden\nnote: none of this is real\n",
	"scratch/beta-lantern/lantern/wicks.txt":  "made-up wick list\nwick one: amber\nwick two: grey\n",
	"scratch/beta-lantern/lantern/oil.txt":    "made-up oil: amber, thin\n",
	"scratch/long-notes/AGENTS.md":            longNotes(),
}

// scratchDirs are the scratch folders made empty.
var scratchDirs = []string{"scratch/outside"}

// scratchRepos are the scratch folders that are git repositories.
var scratchRepos = []string{"scratch/alpha-orchard", "scratch/beta-lantern"}

// longNotes is about 70 KB of made-up instructions (F-134).
func longNotes() string {
	var b strings.Builder
	b.WriteString("# Made-up notes\n\nNothing here is real. These lines only make the file long.\n\n")
	for i := 1; b.Len() < 70_000; i++ {
		fmt.Fprintf(&b, "- Made-up note %04d: the orchard keeps plums, pears and quinces in wooden crates, and the lantern burns amber oil.\n", i)
	}
	return b.String()
}

// listAccounts gives the accounts of the machine: the evaluation account,
// whose home is home, and every other person's. Tests replace it.
var listAccounts = systemAccounts

// computerNames gives the machine's names. Tests replace it.
var computerNames = systemComputerNames

func cmdSetup(c *cli, args []string) int {
	flags, words, err := parseArgs(args, []string{"rec"}, []string{"more"})
	if err != nil || len(words) != 0 {
		return c.fail("usage: hostrec setup --rec <dir>")
	}
	if flags["more"] != "" {
		return c.fail("setup --more belongs to Part 2 of the recorder, which this build does not have yet")
	}
	home, err := homeDir()
	if err != nil {
		return c.fail("%v", err)
	}
	rec := flags["rec"]
	if !filepath.IsAbs(rec) {
		return c.fail("--rec must be an absolute path, such as ~/hf-rec")
	}
	rec = filepath.Clean(rec)
	private := filepath.Join(home, privateName)
	if within(private, rec) || within(rec, private) || rec == home {
		return c.fail("the recording folder must be apart from %s and from the home itself", private)
	}
	if _, err := os.Lstat(filepath.Join(home, markerName)); err == nil {
		return c.fail("this home is set up already; setup runs once")
	}
	info, err := os.Stat(home)
	if err != nil {
		return c.fail("%v", err)
	}
	made := born(info)
	if err := homeChecks(home, made); err != nil {
		return c.fail("%v", err)
	}
	if entries, err := os.ReadDir(rec); err == nil && len(entries) > 0 {
		return c.fail("the recording folder %s is not empty", rec)
	}
	eval, others, err := listAccounts(home)
	if err != nil {
		return c.fail("reading the machine's accounts: %v", err)
	}
	names := hostfacts.Names{Eval: eval, Others: others, Computer: computerNames()}
	bin, err := os.Executable()
	if err != nil {
		return c.fail("%v", err)
	}
	tools := filepath.Join(home, toolsName)
	market := filepath.Join(home, marketName)
	key := make([]byte, 32)
	if _, err := rand.Read(key); err != nil {
		return c.fail("%v", err)
	}
	steps := []struct {
		what string
		do   func() error
	}{
		{"the private folder", func() error {
			if err := os.MkdirAll(private, 0o700); err != nil {
				return err
			}
			if err := os.Chmod(private, 0o700); err != nil {
				return err
			}
			if err := os.WriteFile(filepath.Join(private, keyFile), []byte(hex.EncodeToString(key)+"\n"), 0o600); err != nil {
				return err
			}
			return writeJSON(filepath.Join(private, namesFile), names, 0o600)
		}},
		{"the scratch marketplaces", func() error {
			for _, f := range pluginFiles(market, tools) {
				if err := os.MkdirAll(filepath.Dir(f.path), 0o755); err != nil {
					return err
				}
				if err := os.WriteFile(f.path, f.data, f.mode); err != nil {
					return err
				}
			}
			return nil
		}},
		{"the scratch folders", func() error { return writeScratch(home) }},
		{"the tools", func() error {
			if err := os.MkdirAll(tools, 0o755); err != nil {
				return err
			}
			if err := copyFile(bin, filepath.Join(tools, "hf-missing-check"), 0o755); err != nil {
				return err
			}
			link := filepath.Join(tools, "hf-linked-name")
			if _, err := os.Lstat(link); err == nil {
				return nil
			}
			return os.Symlink(filepath.Join(tools, "hostrec"), link)
		}},
		{"the recording folder", func() error { return os.MkdirAll(rec, 0o700) }},
		{"the setup marker", func() error {
			return writeJSON(filepath.Join(home, markerName), marker{V: 1, Rec: rec, Made: made.UTC().Format(time.RFC3339Nano)}, 0o600)
		}},
	}
	c.say("hostrec setup: found no eva-md install and no earlier session in this account.")
	for _, s := range steps {
		if err := s.do(); err != nil {
			return c.fail("writing %s: %v", s.what, err)
		}
	}
	c.say("Made the recording folder %s, the private folder %s (never handed over),", rec, private)
	c.say("the scratch folders under %s, and the scratch marketplaces under %s.", filepath.Join(home, scratchName), market)
	c.say("Kept %s to replace in everything written to the recording.", plural(1+len(others)+len(names.Computer), "name of this machine", "names of this machine's accounts and computer"))
	c.say("The hosts' commands that add the scratch plugins, which hostrec register runs:")
	for _, host := range []string{"claude-code", "codex"} {
		for _, argv := range registerCommands(host, market) {
			c.say("  %s </dev/null", printable(argv))
		}
	}
	return 0
}

// writeScratch writes the scratch folders, and gives each repository its
// one commit by the made-up identity, set in its own config.
func writeScratch(home string) error {
	for _, rel := range scratchDirs {
		if err := os.MkdirAll(filepath.Join(home, filepath.FromSlash(rel)), 0o755); err != nil {
			return err
		}
	}
	for rel, text := range scratchFiles {
		p := filepath.Join(home, filepath.FromSlash(rel))
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			return err
		}
		if err := os.WriteFile(p, []byte(text), 0o644); err != nil {
			return err
		}
	}
	for _, rel := range scratchRepos {
		dir := filepath.Join(home, filepath.FromSlash(rel))
		for _, argv := range [][]string{
			{"init", "-q", "-b", "main"},
			{"config", "user.name", scratchUser},
			{"config", "user.email", scratchEmail},
			{"add", "-A"},
			{"commit", "-q", "-m", "Made-up scratch files"},
		} {
			if out, err := gitIn(dir, argv...); err != nil {
				return fmt.Errorf("git %s: %v: %s", argv[0], err, strings.TrimSpace(string(out)))
			}
		}
	}
	return nil
}

// gitEnv is the environment of every git hostrec starts: no configuration
// or identity the environment carries, no user or system config file, and
// no prompt.
func gitEnv() []string {
	var env []string
	for _, kv := range os.Environ() {
		name, _, _ := strings.Cut(kv, "=")
		if strings.HasPrefix(name, "GIT_CONFIG") || strings.HasPrefix(name, "GIT_AUTHOR_") || strings.HasPrefix(name, "GIT_COMMITTER_") ||
			name == "GIT_SSH" || name == "GIT_SSH_COMMAND" || name == "GIT_ASKPASS" || name == "SSH_ASKPASS" {
			continue
		}
		env = append(env, kv)
	}
	return append(env, "GIT_CONFIG_GLOBAL=/dev/null", "GIT_CONFIG_NOSYSTEM=1", "GIT_TERMINAL_PROMPT=0")
}

func gitIn(dir string, args ...string) ([]byte, error) {
	cmd := exec.Command("git", append([]string{"-C", dir}, args...)...)
	cmd.Env = gitEnv()
	cmd.Stdin = nil
	return cmd.CombinedOutput()
}

func copyFile(from, to string, mode os.FileMode) error {
	in, err := os.Open(from)
	if err != nil {
		return err
	}
	defer in.Close()
	tmp := to + ".tmp"
	out, err := os.OpenFile(tmp, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, mode)
	if err != nil {
		return err
	}
	if _, err := io.Copy(out, in); err != nil {
		out.Close()
		return err
	}
	if err := out.Close(); err != nil {
		return err
	}
	return os.Rename(tmp, to)
}

// systemAccounts reads the machine's accounts: on a Mac from the directory
// service, elsewhere from /etc/passwd. Only people's accounts are kept:
// those with a user id from 500 (a Mac's first is 501) and a name that does
// not begin with "_".
func systemAccounts(home string) (hostfacts.Account, []hostfacts.Account, error) {
	var all []hostfacts.Account
	var uids []int
	if runtime.GOOS == "darwin" {
		out, err := exec.Command("dscacheutil", "-q", "user").Output()
		if err != nil {
			return hostfacts.Account{}, nil, err
		}
		var a hostfacts.Account
		uid := -1
		flush := func() {
			if a.Short != "" {
				all, uids = append(all, a), append(uids, uid)
			}
			a, uid = hostfacts.Account{}, -1
		}
		for _, line := range strings.Split(string(out), "\n") {
			k, v, ok := strings.Cut(line, ":")
			if !ok {
				flush()
				continue
			}
			v = strings.TrimSpace(v)
			switch strings.TrimSpace(k) {
			case "name":
				a.Short = v
			case "uid":
				uid, _ = strconv.Atoi(v)
			case "dir":
				a.Home = v
			case "gecos":
				a.Full = v
			}
		}
		flush()
	} else {
		data, err := os.ReadFile("/etc/passwd")
		if err != nil {
			return hostfacts.Account{}, nil, err
		}
		s := bufio.NewScanner(strings.NewReader(string(data)))
		for s.Scan() {
			f := strings.Split(s.Text(), ":")
			if len(f) < 6 {
				continue
			}
			uid, _ := strconv.Atoi(f[2])
			full, _, _ := strings.Cut(f[4], ",")
			all, uids = append(all, hostfacts.Account{Short: f[0], Full: full, Home: f[5]}), append(uids, uid)
		}
	}
	var eval hostfacts.Account
	var others []hostfacts.Account
	me, _ := user.Current()
	for i, a := range all {
		switch {
		case a.Home == home || me != nil && a.Short == me.Username:
			eval = a
			eval.Home = home
		case uids[i] >= 500 && uids[i] < 65534 && !strings.HasPrefix(a.Short, "_"):
			others = append(others, a)
		}
	}
	if eval.Short == "" {
		if me == nil {
			return eval, nil, errors.New("this account is not in the machine's list")
		}
		eval = hostfacts.Account{Short: me.Username, Full: me.Name, Home: home}
	}
	return eval, others, nil
}

// systemComputerNames reads the machine's names: on a Mac its
// ComputerName, LocalHostName and HostName, and the output of hostname,
// whole and up to its first dot.
func systemComputerNames() []string {
	var out []string
	add := func(s string) {
		s = strings.TrimSpace(s)
		if s != "" && s != "localhost" && !slices.Contains(out, s) {
			out = append(out, s)
		}
	}
	if runtime.GOOS == "darwin" {
		for _, k := range []string{"ComputerName", "LocalHostName", "HostName"} {
			if b, err := exec.Command("scutil", "--get", k).Output(); err == nil {
				add(string(b))
			}
		}
	}
	if b, err := exec.Command("hostname").Output(); err == nil {
		add(string(b))
	}
	if h, err := os.Hostname(); err == nil {
		add(h)
	}
	for _, n := range slices.Clone(out) {
		if short, _, ok := strings.Cut(n, "."); ok {
			add(short)
		}
	}
	return out
}

func cmdTeardown(c *cli, args []string) int {
	if len(args) != 0 {
		return c.fail("usage: hostrec teardown")
	}
	e, code := c.needEnv()
	if e == nil {
		return code
	}
	h := e.home
	var removed []string
	remove := func(p string) {
		if _, err := os.Lstat(p); errors.Is(err, fs.ErrNotExist) {
			return
		}
		if err := os.RemoveAll(p); err != nil {
			c.say("could not remove %s: %v", p, err)
			return
		}
		removed = append(removed, p)
	}
	for _, rel := range []string{"home-b", "hf-alt-claude", "hf-alt-codex", ".local/share/hf-test", deployKey, deployKey + ".pub"} {
		remove(filepath.Join(h, filepath.FromSlash(rel)))
	}
	for _, name := range append([]string{publicKeyOut}, packedNames()...) {
		remove(filepath.Join(sharedDir, name))
	}
	remove(e.private)
	remove(e.rec)
	remove(filepath.Join(h, scratchName))
	remove(filepath.Join(h, "hf"))
	remove(filepath.Join(h, markerName))
	for _, p := range removed {
		c.say("removed %s", p)
	}
	c.say("%s.", plural(len(removed), "thing removed", "things removed"))
	return 0
}
