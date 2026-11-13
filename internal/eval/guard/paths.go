package guard

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os/exec"
	"regexp"
	"slices"
	"strings"
)

// A Judgement is what the ownership check found of one commit.
type Judgement struct {
	Commit string   // the commit's id
	Cards  []string // the cards its Card: lines name; none for the lead's
	Paths  int      // the paths its diff touches, a rename counted as two
	Faults []string // one line for each fault, naming its card and path
}

// Lead reports whether the commit is the lead's: one with no Card:
// line, which is listed and not judged.
func (j Judgement) Lead() bool { return len(j.Cards) == 0 }

// Ownership judges each first-parent commit in from..to, oldest first, on
// its diff against its first parent: every path it adds, changes or
// deletes, a rename as the two paths, must be inside the union of the
// paths blocks of the cards its Card: lines name, each card read from
// the commit's own tree. A card that does not exist, a card with no paths
// block, and a commit that widens the paths block its card had in the
// first parent are faults too. The commits a merge brings in by its second
// parent are judged only as the merge's own diff. The error is for a
// range or a repository git cannot read.
func Ownership(ctx context.Context, repo, from, to string) ([]Judgement, error) {
	for _, rev := range []string{from, to} {
		if _, err := git(ctx, repo, "rev-parse", "--verify", "--quiet", rev+"^{commit}"); err != nil {
			return nil, fmt.Errorf("guard: %s is not a commit of %s", rev, repo)
		}
	}
	log, err := git(ctx, repo, "log", "--no-show-signature", "--first-parent", "--reverse",
		"--format=%H%x1f%P%x1f%B%x00", from+".."+to)
	if err != nil {
		return nil, fmt.Errorf("guard: cannot read the commits: %w", err)
	}
	var out []Judgement
	for _, record := range strings.Split(log, "\x00") {
		record = strings.TrimLeft(record, "\n")
		if record == "" {
			continue
		}
		fields := strings.SplitN(record, "\x1f", 3)
		if len(fields) != 3 {
			return nil, errors.New("guard: git printed a commit in a form it does not know")
		}
		parent, _, _ := strings.Cut(fields[1], " ")
		j, err := judge(ctx, repo, fields[0], parent, cardsOf(fields[2]))
		if err != nil {
			return nil, err
		}
		out = append(out, j)
	}
	return out, nil
}

// cardLine is a line of a commit's message that names a card: "Card:" and
// one word, alone on its line.
var cardLine = regexp.MustCompile(`^Card:[ \t]*(\S+)[ \t]*$`)

// cardsOf returns the cards a commit's message names, in its order and
// without repeats: the word of each Card: line, wherever the line stands.
// A commit that lands a package carries its line straight after the
// subject, where git does not read it as a trailer; a line in the last
// paragraph, as a trailer, is read too.
func cardsOf(message string) []string {
	var cards []string
	for _, line := range strings.Split(message, "\n") {
		m := cardLine.FindStringSubmatch(strings.TrimRight(line, "\r"))
		if m != nil && !slices.Contains(cards, m[1]) {
			cards = append(cards, m[1])
		}
	}
	return cards
}

func judge(ctx context.Context, repo, commit, parent string, cards []string) (Judgement, error) {
	j := Judgement{Commit: commit, Cards: cards}
	args := []string{"diff-tree", "-r", "-z", "--no-renames", "--name-only", "--no-commit-id"}
	if parent == "" {
		args = append(args, "--root", commit)
	} else {
		args = append(args, parent, commit)
	}
	diff, err := git(ctx, repo, args...)
	if err != nil {
		return j, fmt.Errorf("guard: cannot read the diff of %s: %w", commit, err)
	}
	var paths []string
	for _, p := range strings.Split(diff, "\x00") {
		if p != "" {
			paths = append(paths, p)
		}
	}
	j.Paths = len(paths)
	if j.Lead() {
		return j, nil
	}
	var owned []Pattern
	for _, card := range cards {
		ps, fault := cardPaths(ctx, repo, commit, card)
		if fault != "" {
			j.Faults = append(j.Faults, card+": "+fault)
			continue
		}
		owned = append(owned, ps...)
		if parent == "" {
			continue
		}
		if was, ok := previousPaths(ctx, repo, parent, card); ok {
			for _, p := range ps {
				if !slices.ContainsFunc(was, func(w Pattern) bool { return w.covers(p) }) {
					j.Faults = append(j.Faults, fmt.Sprintf("%s: the commit widens its card's paths block with %s", card, p))
				}
			}
		}
	}
	if len(j.Faults) > 0 {
		// A path is judged only against cards that could all be read: a
		// missing card would make every path it owns look like a fault.
		return j, nil
	}
	names := strings.Join(cards, ", ")
	for _, p := range paths {
		if !anyMatch(owned, p) {
			j.Faults = append(j.Faults, fmt.Sprintf("%s: %s is outside the card's paths", names, p))
		}
	}
	return j, nil
}

// cardPaths reads the card's paths block from the commit's tree, or says
// why it cannot.
func cardPaths(ctx context.Context, repo, commit, card string) ([]Pattern, string) {
	file := CardFile(card)
	if file == "" {
		return nil, "the Card: line names no card: a card's id is like EV-11"
	}
	text, err := git(ctx, repo, "cat-file", "blob", commit+":"+file)
	if err != nil {
		return nil, "the card " + file + " does not exist in the commit"
	}
	ps, found, err := Patterns(text, "paths")
	switch {
	case err != nil:
		return nil, "the card " + file + " cannot be read: " + err.Error()
	case !found:
		return nil, "the card " + file + " has no paths block"
	}
	return ps, ""
}

// previousPaths reads the card's paths block in the commit's first
// parent; ok is false when the card did not exist there, so the commit
// adds it and can widen nothing. A card that had no block had no paths.
func previousPaths(ctx context.Context, repo, parent, card string) ([]Pattern, bool) {
	text, err := git(ctx, repo, "cat-file", "blob", parent+":"+CardFile(card))
	if err != nil {
		return nil, false
	}
	ps, _, err := Patterns(text, "paths")
	if err != nil {
		return nil, true
	}
	return ps, true
}

// git runs git in repo and returns its output. Its error says how git
// ended and never what it printed.
func git(ctx context.Context, repo string, args ...string) (string, error) {
	cmd := exec.CommandContext(ctx, "git", append([]string{"-C", repo}, args...)...)
	var out bytes.Buffer
	cmd.Stdout = &out
	if err := cmd.Run(); err != nil {
		return "", fmt.Errorf("git %s: %w", args[0], err)
	}
	return out.String(), nil
}
