package main

import (
	"archive/tar"
	"bufio"
	"compress/gzip"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"strings"

	"github.com/heyNag/eva-md/internal/hostfacts"
)

// privateNames reads the names and the keys hostrec made from the private
// folder, when this home has one: scan and scrub run without the setup
// marker, as the lead may run them anywhere.
func privateNames() (hostfacts.Names, []string) {
	var n hostfacts.Names
	var k madeKeys
	home, err := homeDir()
	if err != nil {
		return n, nil
	}
	if data, err := os.ReadFile(filepath.Join(home, privateName, namesFile)); err == nil {
		_ = json.Unmarshal(data, &n)
	}
	if data, err := os.ReadFile(filepath.Join(home, privateName, keysFile)); err == nil {
		_ = json.Unmarshal(data, &k)
	}
	return n, k.Keys
}

func cmdScan(c *cli, args []string) int {
	if len(args) != 1 {
		return c.fail("usage: hostrec scan <dir>")
	}
	dir := args[0]
	if info, err := os.Stat(dir); err != nil || !info.IsDir() {
		return c.fail("%s is not a folder", dir)
	}
	names, keys := privateNames()
	in := bufio.NewReader(c.in)
	ask := func(q string) string {
		fmt.Fprint(c.out, q)
		line, _ := in.ReadString('\n')
		return strings.TrimSpace(line)
	}
	c.say("hostrec scan asks a few questions once; the answers are kept nowhere.")
	var words []hostfacts.ScanWord
	add := func(text, kind string) {
		if strings.TrimSpace(text) != "" {
			words = append(words, hostfacts.ScanWord{Text: text, Kind: kind})
		}
	}
	add(ask("The email address you sign in with: "), "what you typed")
	full := ""
	for full == "" {
		if full = ask("Your full name (required): "); full == "" {
			if _, err := in.Peek(1); err != nil {
				return c.fail("your full name is required; nothing was scanned")
			}
		}
	}
	add(full, "what you typed")
	guess := ""
	if len(names.Others) == 1 {
		guess = names.Others[0].Short
	}
	short := ask(fmt.Sprintf("Your own account's short name [%s]: ", guess))
	if short == "" {
		short = guess
	}
	add(short, "what you typed")
	add(ask("Your git server's host name: "), "what you typed")
	add(ask("Your user or group name on the git server: "), "what you typed")
	for _, w := range strings.Split(ask("Any other words, separated by commas: "), ",") {
		add(strings.TrimSpace(w), "what you typed")
	}
	add(names.Eval.Short, "an account's name")
	add(names.Eval.Full, "an account's name")
	add(names.Eval.Home, "a home path")
	for _, a := range names.Others {
		add(a.Short, "an account's name")
		add(a.Full, "an account's name")
		add(a.Home, "a home path")
	}
	for _, n := range names.Computer {
		add(n, "the computer's name")
	}
	if r := names.Remote; r != nil {
		add(r.Address, "the scratch remote")
		add(r.Host, "the scratch remote")
		add(r.Namespace, "the scratch remote")
	}
	res, err := hostfacts.Scan(dir, words, hostfacts.Known{Keys: keys})
	if err != nil {
		return c.fail("%v", err)
	}
	c.say("Counted, not shown: values the hosts and the recorder make.")
	for _, k := range res.KindsCounted() {
		c.say("  %s: %d", k, res.Counts[k])
	}
	if len(res.Hits) == 0 {
		c.say("hits to judge: none")
		return 0
	}
	c.say("hits to judge: %d", len(res.Hits))
	for _, h := range res.Hits {
		c.say("  %s:%d  %s: %s", h.File, h.Line, h.Kind, h.Text)
	}
	c.say("Scrub each with hostrec scrub %s \"<the text>\", then scan again.", dir)
	return 1
}

func cmdScrub(c *cli, args []string) int {
	if len(args) != 2 || strings.TrimSpace(args[1]) == "" {
		return c.fail("usage: hostrec scrub <dir> \"<the text>\"")
	}
	if info, err := os.Stat(args[0]); err != nil || !info.IsDir() {
		return c.fail("%s is not a folder", args[0])
	}
	n, err := hostfacts.Scrub(args[0], args[1])
	if err != nil {
		return c.fail("%v", err)
	}
	c.say("hostrec scrub: replaced %s with a made-up value of the same shape.", plural(n, "place", "places"))
	return 0
}

// packed are the files pack wrote, which pack --clean and teardown delete.
type packed struct {
	Files []string `json:"files"`
}

// packedNames are the names pack writes in the shared folder.
func packedNames() []string {
	names := []string{"hf-rec.tar.gz", "hf-rec.tar.gz.sha256"}
	home, err := homeDir()
	if err != nil {
		return names
	}
	var p packed
	if data, err := os.ReadFile(filepath.Join(home, privateName, packedFile)); err == nil && json.Unmarshal(data, &p) == nil {
		for _, f := range p.Files {
			if filepath.Dir(f) == filepath.Clean(sharedDir) {
				names = append(names, filepath.Base(f))
			}
		}
	}
	return names
}

func cmdPack(c *cli, args []string) int {
	flags, words, err := parseArgs(args, []string{"to"}, []string{"clean"})
	if err != nil {
		return c.fail("usage: hostrec pack <dir> [--to <dir>] | hostrec pack --clean")
	}
	e, code := c.needEnv()
	if e == nil {
		return code
	}
	record := filepath.Join(e.private, packedFile)
	if flags["clean"] != "" {
		if len(words) != 0 || flags["to"] != "" {
			return c.fail("usage: hostrec pack --clean")
		}
		var p packed
		if data, err := os.ReadFile(record); err == nil {
			_ = json.Unmarshal(data, &p)
		}
		n := 0
		for _, f := range p.Files {
			if err := os.Remove(f); err == nil {
				n++
				c.say("deleted %s", f)
			} else if !errors.Is(err, fs.ErrNotExist) {
				c.say("could not delete %s: %v", f, err)
			}
		}
		_ = os.Remove(record)
		c.say("hostrec pack --clean: %s.", plural(n, "file deleted", "files deleted"))
		return 0
	}
	if len(words) != 1 {
		return c.fail("usage: hostrec pack <dir> [--to <dir>]")
	}
	dir, err := filepath.Abs(words[0])
	if err != nil {
		return c.fail("%v", err)
	}
	if within(dir, e.private) || within(e.private, dir) {
		return c.fail("the folder to pack holds the private folder, or is in it; nothing was packed")
	}
	to := flags["to"]
	if to == "" {
		to = sharedDir
	}
	if to, err = filepath.Abs(to); err != nil {
		return c.fail("%v", err)
	}
	if within(dir, to) {
		return c.fail("the archive would be written inside the folder it packs; give --to another folder")
	}
	base := filepath.Base(dir)
	archive := filepath.Join(to, base+".tar.gz")
	sum := archive + ".sha256"
	size, digest, err := writeArchive(dir, base, archive)
	if err != nil {
		_ = os.Remove(archive)
		return c.fail("packing: %v", err)
	}
	if err := os.WriteFile(sum, []byte(digest+"  "+filepath.Base(archive)+"\n"), 0o644); err != nil {
		return c.fail("writing the checksum: %v", err)
	}
	_ = os.Chmod(archive, 0o644)
	_ = os.Chmod(sum, 0o644)
	if err := writeJSON(record, packed{Files: []string{archive, sum}}, 0o600); err != nil {
		return c.fail("%v", err)
	}
	_ = e.command("pack", map[string]any{"bytes": size})
	c.say("hostrec pack: wrote %s (%d bytes) and %s, without the private folder.", archive, size, sum)
	return 0
}

// writeArchive writes the regular files under dir to a gzip-compressed tar
// archive, under the folder name base, and gives its size and checksum.
// Links and anything else that is not a regular file are left out.
func writeArchive(dir, base, archive string) (int64, string, error) {
	f, err := os.OpenFile(archive, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, 0o644)
	if err != nil {
		return 0, "", err
	}
	defer f.Close()
	h := sha256.New()
	gz := gzip.NewWriter(io.MultiWriter(f, h))
	tw := tar.NewWriter(gz)
	err = filepath.WalkDir(dir, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if !d.Type().IsRegular() || d.Name() == lockFile || strings.HasPrefix(d.Name(), ".tmp-") {
			return nil
		}
		rel, err := filepath.Rel(dir, p)
		if err != nil {
			return err
		}
		data, err := os.ReadFile(p)
		if err != nil {
			return err
		}
		if err := tw.WriteHeader(&tar.Header{Name: filepath.ToSlash(filepath.Join(base, rel)), Mode: 0o644, Size: int64(len(data)), Typeflag: tar.TypeReg, Format: tar.FormatPAX}); err != nil {
			return err
		}
		_, err = tw.Write(data)
		return err
	})
	if err == nil {
		err = tw.Close()
	}
	if err == nil {
		err = gz.Close()
	}
	if err == nil {
		err = f.Sync()
	}
	if err != nil {
		return 0, "", err
	}
	info, err := f.Stat()
	if err != nil {
		return 0, "", err
	}
	return info.Size(), hex.EncodeToString(h.Sum(nil)), nil
}

func cmdFixtures(c *cli, args []string) int {
	flags, words, err := parseArgs(args, []string{"rec", "out", "host"}, nil)
	if err != nil || len(words) != 0 || flags["rec"] == "" || flags["out"] == "" {
		return c.fail("usage: hostrec fixtures --rec <dir> --out <dir> [--host claude-code|codex]")
	}
	if inside, err := hostfacts.InsideCheckout(flags["rec"]); err != nil || inside {
		return c.fail("the recording %s is inside a git checkout; keep it outside any checkout", flags["rec"])
	}
	if h := flags["host"]; h != "" && h != "claude-code" && h != "codex" {
		return c.fail("--host must be claude-code or codex")
	}
	meta, err := hostfacts.WriteFixtures(hostfacts.FixtureOptions{Rec: flags["rec"], Out: flags["out"], Host: flags["host"]})
	if err != nil {
		return c.fail("%v", err)
	}
	c.say("hostrec fixtures: wrote %s and fixture.json to %s.", strings.Join(meta.Files, ", "), flags["out"])
	return 0
}

func cmdCheck(c *cli, args []string) int {
	flags, words, err := parseArgs(args, []string{"rec", "fixtures"}, nil)
	if err != nil || len(words) != 0 || flags["rec"] == "" || flags["fixtures"] == "" {
		return c.fail("usage: hostrec check --rec <dir> --fixtures <dir>")
	}
	survived, err := hostfacts.CheckSurvival(flags["rec"], flags["fixtures"])
	if err != nil {
		return c.fail("%v", err)
	}
	made, err := hostfacts.CheckFixtureDir(flags["fixtures"])
	if err != nil {
		return c.fail("%v", err)
	}
	for _, p := range append(survived, made...) {
		c.say("%s", p)
	}
	if n := len(survived) + len(made); n > 0 {
		return c.fail("the fixtures fail the check: %s", plural(n, "problem", "problems"))
	}
	c.say("hostrec check: no value of the recording is in the fixtures, and every value is one the generator makes.")
	return 0
}
