package guard

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io/fs"
	"maps"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
)

// Ceiling is the size budget: the lines of this repository's Go built into
// v1's macOS binary, at V1, which v2 may not exceed.
const Ceiling = 53694

// V1 is v1's last commit before the v2 plan. Only files it had can be
// scheduled for deletion.
const V1 = "51b241c"

// CutOver is the card whose merge lands the deletions: once the index
// lists it as merged, nothing is subtracted and the ceiling is plain.
const CutOver = "CUT-4"

// Arches are the architectures a release builds eva-md for, on macOS.
var Arches = []string{"arm64", "amd64"}

// listFormat prints, for each package of the main module, its module's
// folder and the path of each Go file it builds, tests excluded.
const listFormat = `{{with .Module}}{{if .Main}}{{range $.GoFiles}}{{$.Module.Dir}}{{"\t"}}{{$.Dir}}/{{.}}{{"\n"}}{{end}}{{end}}{{end}}`

// Built returns the lines, counted as newlines, of each Go file of the main
// module that eva-md is built from, by its path from the module's root:
// the non-test files go list -deps ./cmd/eva-md gives for macOS, without
// cgo or build tags, on each of Arches together. repo is the module's
// root.
func Built(ctx context.Context, repo string) (map[string]int, error) {
	built := map[string]int{}
	for _, arch := range Arches {
		cmd := exec.CommandContext(ctx, "go", "list", "-deps", "-f", listFormat, "./cmd/eva-md")
		cmd.Dir = repo
		// GOENV=off keeps the flags, tags and settings of a go env file
		// out, as the release stand-in does: an empty GOFLAGS would not
		// override one written there.
		cmd.Env = append(os.Environ(), "GOOS=darwin", "GOARCH="+arch, "CGO_ENABLED=0", "GOFLAGS=", "GOWORK=off", "GOENV=off")
		var out, errOut bytes.Buffer
		cmd.Stdout, cmd.Stderr = &out, &errOut
		if err := cmd.Run(); err != nil {
			return nil, fmt.Errorf("guard: go list for darwin/%s: %w: %s", arch, err, strings.TrimSpace(errOut.String()))
		}
		for _, line := range strings.Split(strings.TrimSpace(out.String()), "\n") {
			if line == "" {
				continue
			}
			root, file, ok := strings.Cut(line, "\t")
			rel, err := filepath.Rel(root, file)
			if !ok || err != nil || !filepath.IsLocal(rel) {
				return nil, fmt.Errorf("guard: go list printed %q", line)
			}
			rel = filepath.ToSlash(rel)
			if _, seen := built[rel]; seen {
				continue
			}
			data, err := os.ReadFile(file)
			if err != nil {
				return nil, fmt.Errorf("guard: %w", err)
			}
			built[rel] = bytes.Count(data, []byte("\n"))
		}
	}
	if len(built) == 0 {
		return nil, errors.New("guard: go list gave no file of the main module")
	}
	return built, nil
}

// V1Files lists the files the repository had at V1.
func V1Files(ctx context.Context, repo string) (map[string]bool, error) {
	out, err := git(ctx, repo, "ls-tree", "-r", "-z", "--name-only", V1+"^{commit}")
	if err != nil {
		return nil, fmt.Errorf("guard: cannot list v1's files at %s, which needs the whole history: %w", V1, err)
	}
	files := map[string]bool{}
	for _, f := range strings.Split(out, "\x00") {
		if f != "" {
			files[f] = true
		}
	}
	return files, nil
}

// A Size is the size check's count.
type Size struct {
	Built     int      // the lines built into eva-md
	Scheduled int      // the lines of them that merged cards schedule for deletion
	Files     int      // the files those lines are in
	Ceiling   int      // the budget
	Plain     bool     // the cut-over card has merged, so nothing is subtracted
	Merged    []string // the merged cards, in the index's order
	Faults    []string // a deletes block's faults, one line each
}

// Net is the built lines less the scheduled ones.
func (s Size) Net() int { return s.Built - s.Scheduled }

// Headroom is the lines left under the ceiling, negative when over it.
func (s Size) Headroom() int { return s.Ceiling - s.Net() }

// Holds reports whether the size is within its budget and the deletes
// blocks have no fault.
func (s Size) Holds() bool { return s.Headroom() >= 0 && len(s.Faults) == 0 }

// Count counts the size of the repository at repo against ceiling, from
// the lines built into eva-md, as Built gives them. The scheduled lines
// are the built lines of the files that a deletes block of a merged card
// matches, each counted once, and only files that v1 had, as v1 lists
// them; v1 is called only when a merged card has a deletes block. An
// exact entry naming a file v1 did not have is a fault. With no card index
// nothing is scheduled. Once the cut-over card has merged nothing is
// subtracted, and each scheduled file still in the repository is a fault.
// The error is for a card or an index that cannot be read.
func Count(repo string, built map[string]int, ceiling int, v1 func() (map[string]bool, error)) (Size, error) {
	s := Size{Ceiling: ceiling}
	for _, n := range built {
		s.Built += n
	}
	index, err := os.ReadFile(filepath.Join(repo, filepath.FromSlash(CardsDir), "README.md"))
	if errors.Is(err, fs.ErrNotExist) {
		return s, nil
	} else if err != nil {
		return s, fmt.Errorf("guard: %w", err)
	}
	s.Merged = Merged(string(index))
	s.Plain = slices.Contains(s.Merged, CutOver)
	var entries []Pattern
	var owners []string
	for _, card := range s.Merged {
		file := CardFile(card)
		if file == "" {
			s.Faults = append(s.Faults, fmt.Sprintf("the index lists %q as merged, which is not a card's id", card))
			continue
		}
		text, err := os.ReadFile(filepath.Join(repo, filepath.FromSlash(file)))
		if err != nil {
			s.Faults = append(s.Faults, fmt.Sprintf("%s: the index lists it as merged and %s cannot be read", card, file))
			continue
		}
		ps, _, err := Patterns(string(text), "deletes")
		if err != nil {
			s.Faults = append(s.Faults, card+": "+err.Error())
			continue
		}
		for _, p := range ps {
			entries = append(entries, p)
			owners = append(owners, card)
		}
	}
	if len(entries) == 0 {
		return s, nil
	}
	had, err := v1()
	if err != nil {
		return s, err
	}
	scheduled := map[string]bool{}
	for i, p := range entries {
		if _, folder := p.folder(); !folder && !had[string(p)] {
			s.Faults = append(s.Faults, fmt.Sprintf("%s: its deletes block names %s, which v1 did not have", owners[i], p))
		}
		for f := range had {
			if p.Match(f) {
				scheduled[f] = true
			}
		}
	}
	for _, f := range slices.Sorted(maps.Keys(scheduled)) {
		if s.Plain {
			if _, err := os.Lstat(filepath.Join(repo, filepath.FromSlash(f))); err == nil {
				s.Faults = append(s.Faults, fmt.Sprintf("%s is scheduled for deletion and still present, after %s merged", f, CutOver))
			}
			continue
		}
		if n, ok := built[f]; ok {
			s.Scheduled += n
			s.Files++
		}
	}
	return s, nil
}

// Report writes the count, one figure a line.
func (s Size) Report() string {
	var b strings.Builder
	fmt.Fprintf(&b, "Built lines: %d\n", s.Built)
	switch {
	case s.Plain:
		fmt.Fprintf(&b, "Scheduled lines: 0 (%s has merged: the ceiling is plain)\n", CutOver)
	default:
		fmt.Fprintf(&b, "Scheduled lines: %d, in %d files of the merged cards' deletes blocks\n", s.Scheduled, s.Files)
	}
	fmt.Fprintf(&b, "Net lines: %d\n", s.Net())
	fmt.Fprintf(&b, "Ceiling: %d\n", s.Ceiling)
	fmt.Fprintf(&b, "Headroom: %d\n", s.Headroom())
	for _, f := range s.Faults {
		fmt.Fprintf(&b, "Fault: %s\n", f)
	}
	if s.Headroom() < 0 {
		fmt.Fprintf(&b, "The net lines exceed the ceiling by %d.\n", -s.Headroom())
	}
	return b.String()
}
