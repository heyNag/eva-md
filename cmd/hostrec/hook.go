package main

import (
	"bufio"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"syscall"
	"time"

	"github.com/heyNag/eva-md/internal/hostfacts"
)

// The hook's time: its own work is bounded by hookBudget, past which main
// answers {} and exits; the start arm waits startPause before it answers,
// as step C-01 expects; a hook waits lockWait at most for another's lock.
// Tests set startPause to nothing.
var (
	hookBudget = 1500 * time.Millisecond
	startPause = time.Second
	lockWait   = time.Second
)

const (
	maxPayload      = 16 << 20
	snapshotBytes   = 8 << 20
	snapshotFields  = 5000
	maxFilesWalked  = 20000
	progressFile    = "progress.json"
	hashesFile      = "payload-hashes.txt"
	lockFile        = ".lock"
	startWord       = "MARIGOLD-TUESDAY"
	startContext    = "This is a made-up test. The codeword for this chat is " + startWord + "."
	startSystemLine = "hf test: the scratch start hook ran. It is made up; nothing to do."
)

// envMarks are the run-book's made-up environment marks: whether each is
// set is kept, never a value.
var envMarks = []string{"HF_PROGRAM_MARK", "HF_PLAIN_MARK", "HF_FAKE_API_TOKEN"}

// The arms of Part 1: start, and disarm. Part 2 adds the rest.
const armStart = "start"

type armFile struct {
	V    int      `json:"v"`
	Name string   `json:"name,omitempty"`
	Args []string `json:"args,omitempty"`
}

// progress is what the hooks carry from one to the next: where each record
// file's next snapshot starts, and each session's last prompt, both by
// keyed hash.
type progress struct {
	Files   map[string]fileProgress `json:"files"`
	Prompts map[string]string       `json:"prompts"`
}

type fileProgress struct {
	From  int64 `json:"from"`
	Index int   `json:"index"`
}

func cmdHook(c *cli, args []string) int {
	answer, pause := hookRun(c, args)
	if pause > 0 {
		time.Sleep(pause)
	}
	fmt.Fprintln(c.out, answer)
	return 0
}

// hookRun keeps what the host sent and gives the answer. It fails closed:
// on any error it answers {}, and once the home has passed its checks it
// keeps one line in errors.jsonl.
func hookRun(c *cli, args []string) (answer string, pause time.Duration) {
	answer = "{}"
	var e *env
	defer func() {
		if r := recover(); r != nil {
			answer, pause = "{}", 0
			if e != nil {
				e.oops("hook", fmt.Errorf("the hook failed: %v", r))
			}
		}
	}()
	flags, words, argErr := parseArgs(args, []string{"host", "event", "via"}, nil)
	raw, readErr := io.ReadAll(io.LimitReader(c.in, maxPayload+1))
	var err error
	if e, err = loadEnv(); err != nil {
		return "{}", 0
	}
	st, err := e.state()
	if err != nil {
		e.oops("hook", errors.New("the sitting's state does not read"))
		return "{}", 0
	}
	if !st.Open {
		return "{}", 0
	}
	host, event, via := flags["host"], flags["event"], flags["via"]
	switch {
	case argErr != nil || len(words) != 0 || event == "" || !slices.Contains([]string{"claude-code", "codex"}, host):
		e.oops("hook", errors.New("the hook's arguments do not read"))
		return "{}", 0
	case readErr != nil:
		e.oops("hook", errors.New("the payload could not be read"))
		return "{}", 0
	case len(raw) > maxPayload:
		e.oops("hook", errors.New("the payload is larger than the hook reads"))
		return "{}", 0
	}
	payload, err := hostfacts.Decode(raw)
	if _, ok := payload.(map[string]any); err != nil || !ok {
		e.oops("hook", errors.New("the payload is not one JSON object"))
		return "{}", 0
	}
	wall, mono := time.Now(), nanotime()
	unlock, err := e.lock()
	if err != nil {
		e.oops("hook", err)
		return "{}", 0
	}
	defer unlock()
	var a armFile
	if err := e.get(hostfacts.ArmFile, &a); err != nil {
		e.oops("hook", errors.New("the arm does not read"))
	}
	ev := hostfacts.Event{
		V: 1, Mark: st.Mark, Wall: wall.UTC().Format(time.RFC3339Nano), MonoNS: mono,
		Host: host, Event: event, Via: via, Payload: payload,
		PayloadHashes: hostfacts.PayloadHashes(e.key, payload),
	}
	e.describe(&ev, payload, st)
	out, label, wait := armAnswer(a, event, via, payload)
	ev.Arm, ev.Answer = a.Name, label
	if p, err := parent(os.Getppid()); err == nil {
		proc := &hostfacts.Proc{Parent: p.name, Login: &p.login}
		if !p.born.IsZero() {
			ms := float64(time.Since(p.born).Microseconds()) / 1000
			proc.StartCostMS = &ms
		}
		ev.Proc = proc
	}
	if err := e.line(hostfacts.EventsFile, ev); err != nil {
		e.oops("hook", errors.New("the event could not be written"))
		return "{}", 0
	}
	return out, wait
}

// describe fills in what the hook reads beside the payload: the working
// folder's git shape, the environment's names, the files added under the
// host's folder since the mark, and the record's snapshot.
func (e *env) describe(ev *hostfacts.Event, payload any, st state) {
	if cwd := str(payload, "cwd"); filepath.IsAbs(cwd) {
		ev.Git = gitShape(cwd)
	}
	for _, kv := range os.Environ() {
		name, _, _ := strings.Cut(kv, "=")
		ev.EnvNames = append(ev.EnvNames, name)
		if slices.Contains(envMarks, name) {
			ev.EnvMarks = append(ev.EnvMarks, name)
		}
	}
	slices.Sort(ev.EnvNames)
	ev.EnvNames = slices.Compact(ev.EnvNames)
	if since, err := time.Parse(time.RFC3339Nano, st.MarkWall); err == nil {
		ev.FilesAdded, ev.FilesCut = filesAdded(hostFolder(e.home, ev.Host), since)
	}
	var pr progress
	if err := e.get(progressFile, &pr); err != nil {
		e.oops("hook", errors.New("the hooks' progress does not read"))
	}
	if pr.Files == nil {
		pr.Files = map[string]fileProgress{}
	}
	if pr.Prompts == nil {
		pr.Prompts = map[string]string{}
	}
	session := hostfacts.Hash(e.key, str(payload, "session_id"))
	if p := str(payload, "prompt"); p != "" && ev.Event == "UserPromptSubmit" {
		pr.Prompts[session] = hostfacts.Hash(e.key, p)
	}
	seen := e.payloadHashes(ev.PayloadHashes)
	if path := str(payload, "transcript_path"); filepath.IsAbs(path) {
		in := hostfacts.SnapshotInput{
			Host: ev.Host, Path: path, Key: e.key, PayloadHashes: seen,
			Prompt: pr.Prompts[session], MaxBytes: snapshotBytes, MaxFields: snapshotFields,
		}
		if r := str(payload, "last_assistant_message"); r != "" {
			in.Reply = hostfacts.Hash(e.key, r)
		}
		if t := str(payload, "turn_id"); t != "" {
			in.Turn = hostfacts.Hash(e.key, t)
		}
		fileKey := hostfacts.Hash(e.key, path)
		in.From, in.Index = pr.Files[fileKey].From, pr.Files[fileKey].Index
		snap, next, index, err := hostfacts.TakeSnapshot(in)
		if err != nil {
			e.oops("hook", errors.New("the record file could not be read"))
		} else {
			ev.Record = &snap
			pr.Files[fileKey] = fileProgress{next, index}
		}
	}
	if err := e.put(progressFile, pr); err != nil {
		e.oops("hook", errors.New("the hooks' progress could not be written"))
	}
}

// payloadHashes adds this payload's hashes to the recording's list, and
// gives every hash any payload of the recording carried.
func (e *env) payloadHashes(these map[string]string) map[string]bool {
	seen := map[string]bool{}
	if data, err := os.ReadFile(filepath.Join(e.rec, hashesFile)); err == nil {
		for _, h := range strings.Fields(string(data)) {
			seen[h] = true
		}
	}
	var b strings.Builder
	for _, h := range these {
		if !seen[h] {
			seen[h] = true
			b.WriteString(h + "\n")
		}
	}
	if b.Len() > 0 {
		f, err := os.OpenFile(filepath.Join(e.rec, hashesFile), os.O_WRONLY|os.O_APPEND|os.O_CREATE, 0o600)
		if err == nil {
			_, err = f.WriteString(e.repl.String(b.String()))
			f.Close()
		}
		if err != nil {
			e.oops("hook", errors.New("the payload hashes could not be written"))
		}
	}
	return seen
}

// lock takes the recording's lock, waiting lockWait at most.
func (e *env) lock() (func(), error) {
	f, err := os.OpenFile(filepath.Join(e.rec, lockFile), os.O_RDWR|os.O_CREATE, 0o600)
	if err != nil {
		return nil, errors.New("the recording's lock could not be opened")
	}
	deadline := time.Now().Add(lockWait)
	for {
		err := syscall.Flock(int(f.Fd()), syscall.LOCK_EX|syscall.LOCK_NB)
		if err == nil {
			return func() { _ = syscall.Flock(int(f.Fd()), syscall.LOCK_UN); f.Close() }, nil
		}
		if !errors.Is(err, syscall.EWOULDBLOCK) || time.Now().After(deadline) {
			f.Close()
			return nil, errors.New("the recording's lock is held")
		}
		time.Sleep(5 * time.Millisecond)
	}
}

// armAnswer is what the hook answers under an arm, the label the recording
// keeps for it, and how long it waits first. Part 1's one arm, start,
// answers a new, resumed or cleared chat's start with the start word: the
// recorder's own SessionStart hook does, and none of the hooks declared
// beside it.
func armAnswer(a armFile, event, via string, payload any) (answer, label string, wait time.Duration) {
	if a.Name != armStart || event != "SessionStart" || via != "" {
		return "{}", "", 0
	}
	switch str(payload, "source") {
	case "startup", "resume", "clear":
	default:
		return "{}", "", 0
	}
	data, _ := json.Marshal(map[string]any{
		"hookSpecificOutput": map[string]string{"hookEventName": "SessionStart", "additionalContext": startContext},
		"systemMessage":      startSystemLine,
	})
	return string(data), "start-word", startPause
}

func str(v any, key string) string {
	m, _ := v.(map[string]any)
	s, _ := m[key].(string)
	return s
}

// gitShape reads a folder's git state from its files, starting nothing:
// whether it is in a repository, whether HEAD is detached, and whether its
// git folder is its own or a worktree's of a shared one.
func gitShape(dir string) *hostfacts.GitShape {
	for d := dir; ; d = filepath.Dir(d) {
		dotgit := filepath.Join(d, ".git")
		info, err := os.Lstat(dotgit)
		if err == nil {
			g := &hostfacts.GitShape{Repo: true, Common: "own"}
			gitDir := dotgit
			if info.Mode().IsRegular() {
				data, err := os.ReadFile(dotgit)
				target, ok := strings.CutPrefix(strings.TrimSpace(string(data)), "gitdir: ")
				if err != nil || !ok {
					return g
				}
				if !filepath.IsAbs(target) {
					target = filepath.Join(d, target)
				}
				gitDir = target
				if _, err := os.Stat(filepath.Join(gitDir, "commondir")); err == nil {
					g.Common = "shared"
				}
			}
			head, err := os.ReadFile(filepath.Join(gitDir, "HEAD"))
			g.Detached = err == nil && !strings.HasPrefix(string(head), "ref: ")
			return g
		}
		if filepath.Dir(d) == d {
			return &hostfacts.GitShape{}
		}
	}
}

// hostFolder is the host's own folder: the one its environment names, or
// the default.
func hostFolder(home, host string) string {
	if host == "codex" {
		if d := os.Getenv("CODEX_HOME"); filepath.IsAbs(d) {
			return d
		}
		return filepath.Join(home, ".codex")
	}
	if d := os.Getenv("CLAUDE_CONFIG_DIR"); filepath.IsAbs(d) {
		return d
	}
	return filepath.Join(home, ".claude")
}

var plainWord = regexp.MustCompile(`^[a-z][a-z_-]{0,31}$`)
var plainExt = regexp.MustCompile(`^\.[a-z0-9]{1,8}$`)

// namePattern is a file's name pattern under the host's folder: its first
// folder when that is a plain word, every other folder and the name as *,
// and the extension kept, as projects/*/*.jsonl.
func namePattern(rel string) string {
	segs := strings.Split(filepath.ToSlash(rel), "/")
	for i, s := range segs {
		switch {
		case i == 0 && len(segs) > 1 && plainWord.MatchString(s):
		case i == len(segs)-1:
			ext := strings.ToLower(filepath.Ext(s))
			if !plainExt.MatchString(ext) {
				ext = ""
			}
			segs[i] = "*" + ext
		default:
			segs[i] = "*"
		}
	}
	return strings.Join(segs, "/")
}

// filesAdded counts the files made under root since a time, by name
// pattern, and reports a walk cut short.
func filesAdded(root string, since time.Time) (map[string]int, bool) {
	counts := map[string]int{}
	walked, cut := 0, false
	_ = filepath.WalkDir(root, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return nil
		}
		if walked++; walked > maxFilesWalked {
			cut = true
			return filepath.SkipAll
		}
		if !d.Type().IsRegular() {
			return nil
		}
		info, err := d.Info()
		if err != nil || born(info).Before(since) {
			return nil
		}
		if rel, err := filepath.Rel(root, p); err == nil {
			counts[namePattern(rel)]++
		}
		return nil
	})
	if len(counts) == 0 {
		return nil, cut
	}
	return counts, cut
}

// readLines reads a JSON-lines file of the recording into values of T, one
// per line; a missing file is no lines.
func readLines[T any](path string) ([]T, error) {
	f, err := os.Open(path)
	if errors.Is(err, fs.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	defer f.Close()
	var out []T
	s := bufio.NewScanner(f)
	s.Buffer(make([]byte, 64<<10), 256<<20)
	for s.Scan() {
		if len(strings.TrimSpace(s.Text())) == 0 {
			continue
		}
		var v T
		if err := json.Unmarshal(s.Bytes(), &v); err != nil {
			return nil, fmt.Errorf("%s does not read: %w", filepath.Base(path), err)
		}
		out = append(out, v)
	}
	return out, s.Err()
}
