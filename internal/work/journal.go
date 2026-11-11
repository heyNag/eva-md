package work

import (
	"bufio"
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path"
	"regexp"
	"slices"
	"strings"
	"syscall"
	"time"
)

// The journal's events.
const (
	evStart       = "start"
	evFinish      = "finish"
	evInterrupted = "interrupted"
)

// keepMonths is how long a journal file is kept: one whose month ended
// more than this many months ago is removed by the worker.
const keepMonths = 13

// An entry is one line of the journal: counts, ids and words, never a
// session id, a path, an error's text or captured text.
type entry struct {
	T       string   `json:"t"`
	Run     string   `json:"run"`
	Ev      string   `json:"ev"`
	Kind    string   `json:"kind"`
	Prio    int      `json:"prio"`
	Marks   int      `json:"marks"`
	Reasons []string `json:"reasons"`
	Hosts   []string `json:"hosts"`
	Outcome Outcome  `json:"outcome,omitempty"`
	Ms      *int64   `json:"ms,omitempty"`
}

// stamp is how the journal writes a time: UTC, to the millisecond.
func stamp(t time.Time) string { return t.UTC().Format("2006-01-02T15:04:05.000Z") }

// startEntry is the start line of a run of j given marks.
func startEntry(t time.Time, id string, j Job, marks []listed) entry {
	var reasons, hosts []string
	for _, m := range marks {
		if m.Reason != "" {
			reasons = append(reasons, m.Reason)
		}
		if m.Host != "" {
			hosts = append(hosts, m.Host)
		}
	}
	slices.Sort(reasons)
	slices.Sort(hosts)
	return entry{
		T: stamp(t), Run: id, Ev: evStart, Kind: j.Kind, Prio: j.Priority, Marks: len(marks),
		Reasons: append([]string{}, slices.Compact(reasons)...),
		Hosts:   append([]string{}, slices.Compact(hosts)...),
	}
}

var journalName = regexp.MustCompile(`^(\d{4}-\d{2})\.jsonl$`)

// appendJournal writes e as one line of its month's file and syncs it. A
// line a killed worker left unfinished is ended first, so it can't swallow
// this one.
func appendJournal(root *os.Root, e entry) error {
	line, err := json.Marshal(e)
	if err != nil {
		return fmt.Errorf("work: journal: %w", err)
	}
	name := path.Join(journalDir, e.T[:len("2006-01")]+".jsonl")
	f, err := root.OpenFile(name, os.O_RDWR|os.O_APPEND|os.O_CREATE|syscall.O_NOFOLLOW, 0o600)
	if err != nil {
		return fmt.Errorf("work: journal: %w", err)
	}
	defer f.Close()
	info, err := f.Stat()
	if err != nil || !info.Mode().IsRegular() {
		return errors.New("work: the journal is not a regular file")
	}
	if size := info.Size(); size > 0 {
		last := make([]byte, 1)
		if _, err := f.ReadAt(last, size-1); err != nil {
			return fmt.Errorf("work: journal: %w", err)
		}
		if last[0] != '\n' {
			line = append([]byte{'\n'}, line...)
		}
	}
	if _, err := f.Write(append(line, '\n')); err != nil {
		return fmt.Errorf("work: journal: %w", err)
	}
	if err := f.Sync(); err != nil {
		return fmt.Errorf("work: journal: %w", err)
	}
	if info.Size() == 0 {
		f, err := root.Open(journalDir)
		if err != nil {
			return fmt.Errorf("work: journal: %w", err)
		}
		defer f.Close()
		if err := f.Sync(); err != nil {
			return fmt.Errorf("work: journal: %w", err)
		}
	}
	return nil
}

// journalFiles lists the journal's files, oldest month first.
func journalFiles(root *os.Root) ([]string, error) {
	f, err := root.Open(journalDir)
	if err != nil {
		return nil, fmt.Errorf("work: journal: %w", err)
	}
	names, err := f.Readdirnames(-1)
	f.Close()
	if err != nil {
		return nil, fmt.Errorf("work: journal: %w", err)
	}
	names = slices.DeleteFunc(names, func(n string) bool { return !journalName.MatchString(n) })
	slices.Sort(names)
	return names, nil
}

// runLines returns the journal's lines for run id, oldest first. A line
// that is not an entry is passed over.
func runLines(root *os.Root, id string) ([]entry, error) {
	names, err := journalFiles(root)
	if err != nil {
		return nil, err
	}
	needle := []byte(`"run":"` + id + `"`)
	var out []entry
	for _, name := range names {
		f, err := root.Open(path.Join(journalDir, name))
		if err != nil {
			if errors.Is(err, fs.ErrNotExist) {
				continue
			}
			return nil, fmt.Errorf("work: journal: %w", err)
		}
		r := bufio.NewReader(f)
		for {
			line, err := r.ReadBytes('\n')
			if bytes.Contains(line, needle) {
				var e entry
				if json.Unmarshal(line, &e) == nil && e.Run == id {
					out = append(out, e)
				}
			}
			if err == io.EOF {
				break
			}
			if err != nil {
				f.Close()
				return nil, fmt.Errorf("work: journal: %w", err)
			}
		}
		f.Close()
	}
	return out, nil
}

// prune removes the journal's files whose month ended more than
// keepMonths months before now.
func prune(root *os.Root, now time.Time) error {
	names, err := journalFiles(root)
	if err != nil {
		return err
	}
	cutoff := now.UTC().AddDate(0, -keepMonths, 0)
	for _, name := range names {
		month, err := time.Parse("2006-01", strings.TrimSuffix(name, ".jsonl"))
		if err != nil {
			continue
		}
		if month.AddDate(0, 1, 0).Before(cutoff) {
			if err := root.Remove(path.Join(journalDir, name)); err != nil && !errors.Is(err, fs.ErrNotExist) {
				return fmt.Errorf("work: journal: %w", err)
			}
		}
	}
	return nil
}
