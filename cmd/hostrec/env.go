package main

import (
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/heyNag/eva-md/internal/hostfacts"
	"github.com/heyNag/eva-md/internal/paths"
)

// The evaluation home's fixed places, under HOME.
const (
	markerName   = ".hf-setup.json" // the setup marker
	privateName  = "hf-private"     // never handed over
	toolsName    = "hf-tools"
	scratchName  = "scratch"
	marketName   = "hf/marketplace"
	keyFile      = "key"        // in the private folder: the recording's hash key
	namesFile    = "names.json" // in the private folder: what is replaced
	keysFile     = "keys.json"  // in the private folder: the keys hostrec made
	packedFile   = "packed.json"
	knownHosts   = "known_hosts"
	deployKey    = ".ssh/hf_scratch_deploy"
	addressFile  = "hf-scratch-address.txt"
	publicKeyOut = "hf-scratch-deploy-key.pub"
	scratchRepo  = "hf-scratch-plum"
)

// installRecord is eva-md's install record, in its data folder:
// install.RecordFile, which a test holds equal, named here so the recorder
// imports nothing of the install.
const installRecord = "install-record.json"

// sharedDir is where the owner's own account and the evaluation account
// pass files. Tests point it at a folder of their own.
var sharedDir = "/Users/Shared"

// marker is the setup marker's content.
type marker struct {
	V   int    `json:"v"`
	Rec string `json:"rec"`
	// Made is when setup ran, in a home with no host's session: a session
	// from before it means a home that is not an evaluation home.
	Made string `json:"made"`
}

// env is an evaluation home that passed the checks.
type env struct {
	home, rec, private string
	made               time.Time
	key                []byte
	names              hostfacts.Names
	repl               *hostfacts.Replacer
}

func homeDir() (string, error) {
	home := os.Getenv("HOME")
	if home == "" || !filepath.IsAbs(home) {
		return "", errors.New("HOME is not an absolute path")
	}
	return filepath.Clean(home), nil
}

// loadEnv finds the setup marker and runs the checks every command but
// setup, scan, scrub, fixtures and check needs.
func loadEnv() (*env, error) {
	home, err := homeDir()
	if err != nil {
		return nil, err
	}
	data, err := os.ReadFile(filepath.Join(home, markerName))
	if err != nil {
		return nil, errors.New("this home was not set up by hostrec setup; nothing was done")
	}
	var m marker
	if err := json.Unmarshal(data, &m); err != nil || m.V != 1 || !filepath.IsAbs(m.Rec) {
		return nil, errors.New("the setup marker does not read; nothing was done")
	}
	made, err := time.Parse(time.RFC3339Nano, m.Made)
	if err != nil {
		return nil, errors.New("the setup marker does not read; nothing was done")
	}
	if err := homeChecks(home, made); err != nil {
		return nil, err
	}
	e := &env{home: home, rec: m.Rec, private: filepath.Join(home, privateName), made: made}
	key, err := os.ReadFile(filepath.Join(e.private, keyFile))
	if err != nil {
		return nil, errors.New("the private folder has no key; nothing was done")
	}
	if e.key, err = hex.DecodeString(strings.TrimSpace(string(key))); err != nil || len(e.key) < 16 {
		return nil, errors.New("the private folder's key does not read; nothing was done")
	}
	if data, err = os.ReadFile(filepath.Join(e.private, namesFile)); err != nil {
		return nil, errors.New("the private folder has no names; nothing was done")
	}
	if err := json.Unmarshal(data, &e.names); err != nil {
		return nil, errors.New("the private folder's names do not read; nothing was done")
	}
	e.repl = hostfacts.NewReplacer(e.names)
	return e, nil
}

// homeChecks refuses a home that holds an eva-md install record or config,
// or a host's session made before setup ran in it.
func homeChecks(home string, made time.Time) error {
	if err := evaCheck(home); err != nil {
		return err
	}
	old, err := earlierSession(home, made)
	if err != nil {
		return err
	}
	if old {
		return errors.New("this home holds a host's session from before hostrec setup ran in it, so it is not an evaluation home; nothing was done")
	}
	return nil
}

// evaCheck refuses a home that holds an eva-md install record or config.
func evaCheck(home string) error {
	dirs := paths.Dirs{
		Config: filepath.Join(home, ".config", "eva-md"),
		Share:  filepath.Join(home, ".local", "share", "eva-md"),
	}
	for _, p := range []string{filepath.Join(dirs.Share, installRecord), dirs.ConfigFile(), dirs.AnswersFile()} {
		if _, err := os.Lstat(p); err == nil || !errors.Is(err, fs.ErrNotExist) {
			return errors.New("this home holds an eva-md install or config, so it is not an evaluation home; nothing was done")
		}
	}
	return nil
}

// setupChecks are setup's checks: a new evaluation home holds no eva-md
// install or config, and no host's session at all, since setup runs before
// either host is signed in (step S0-05). A real home, whose sessions are
// all newer than the home itself, is refused by its first session.
func setupChecks(home string) error {
	if err := evaCheck(home); err != nil {
		return err
	}
	found, err := earlierSession(home, time.Date(9999, 1, 1, 0, 0, 0, 0, time.UTC))
	if err != nil {
		return err
	}
	if found {
		return errors.New("this home holds a host's session already, so it is not a new evaluation home: setup runs before either host is signed in; nothing was done")
	}
	return nil
}

// sessionRoots are where the hosts keep their sessions in a home.
var sessionRoots = []string{".claude/projects", ".codex/sessions", ".codex/archived_sessions"}

// maxSessionEntries bounds the walk of the hosts' session folders; past it
// the home is taken as not an evaluation home, whose sessions are few.
const maxSessionEntries = 20000

// earlierSession reports a session file made before the account was.
func earlierSession(home string, made time.Time) (bool, error) {
	seen := 0
	found := false
	for _, root := range sessionRoots {
		err := filepath.WalkDir(filepath.Join(home, root), func(p string, d fs.DirEntry, err error) error {
			if err != nil {
				if errors.Is(err, fs.ErrNotExist) {
					return nil
				}
				return err
			}
			if seen++; seen > maxSessionEntries {
				return errors.New("the hosts' session folders hold more files than an evaluation home's; nothing was done")
			}
			if !d.Type().IsRegular() || !strings.HasSuffix(d.Name(), ".jsonl") {
				return nil
			}
			info, err := d.Info()
			if err != nil {
				return nil
			}
			if born(info).Before(made) {
				found = true
				return filepath.SkipAll
			}
			return nil
		})
		if err != nil {
			return false, err
		}
		if found {
			return true, nil
		}
	}
	return false, nil
}

// line writes v as one line of name under the recording folder, every name
// replaced first.
func (e *env) line(name string, v any) error {
	data, err := e.repl.Line(v)
	if err != nil {
		return err
	}
	f, err := os.OpenFile(filepath.Join(e.rec, name), os.O_WRONLY|os.O_APPEND|os.O_CREATE, 0o600)
	if err != nil {
		return err
	}
	_, err = f.Write(data)
	if cerr := f.Close(); err == nil {
		err = cerr
	}
	return err
}

// put replaces the file name under the recording folder with v, every name
// replaced first.
func (e *env) put(name string, v any) error {
	data, err := e.repl.Line(v)
	if err != nil {
		return err
	}
	return paths.WriteFileAtomic(filepath.Join(e.rec, name), data, 0o600)
}

// get reads the file name under the recording folder into v; a missing
// file leaves v as it is.
func (e *env) get(name string, v any) error {
	data, err := os.ReadFile(filepath.Join(e.rec, name))
	if errors.Is(err, fs.ErrNotExist) {
		return nil
	}
	if err != nil {
		return err
	}
	return json.Unmarshal(data, v)
}

// state is the sitting's state, in the recording folder.
type state struct {
	Open     bool   `json:"open"`
	Mark     string `json:"mark,omitempty"`
	MarkWall string `json:"mark_wall,omitempty"`
}

func (e *env) state() (state, error) {
	var s state
	err := e.get(hostfacts.StateFile, &s)
	return s, err
}

// command keeps what a command found, in counts, states and field names.
func (e *env) command(name string, result map[string]any) error {
	s, _ := e.state()
	return e.line(hostfacts.OutputsFile, hostfacts.OutputLine{V: 1, Mark: s.Mark, Wall: wallNow(), Command: name, Result: result})
}

// oops keeps one line in errors.jsonl; it fails quietly.
func (e *env) oops(where string, err error) {
	_ = e.line(hostfacts.ErrorsFile, hostfacts.ErrorLine{V: 1, Wall: wallNow(), Where: where, Error: err.Error()})
}

func wallNow() string { return time.Now().UTC().Format(time.RFC3339Nano) }

// needEnv loads the evaluation home for a command that needs it.
func (c *cli) needEnv() (*env, int) {
	e, err := loadEnv()
	if err != nil {
		return nil, c.fail("%v", err)
	}
	return e, 0
}

// within reports whether p is dir or under it.
func within(dir, p string) bool {
	rel, err := filepath.Rel(dir, p)
	return err == nil && rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator))
}

func writeJSON(path string, v any, perm os.FileMode) error {
	data, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		return err
	}
	return paths.WriteFileAtomic(path, append(data, '\n'), perm)
}

func plural(n int, one, many string) string {
	if n == 1 {
		return fmt.Sprintf("%d %s", n, one)
	}
	return fmt.Sprintf("%d %s", n, many)
}
