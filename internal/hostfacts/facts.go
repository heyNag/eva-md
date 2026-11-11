package hostfacts

import (
	"slices"
	"strings"
)

// Fact is one row of the run-book's table "Facts to steps": a fact the sheet
// settles in a sitting, its mark, and the steps that give evidence for it.
// Part1 and Part2 are the run-book's cells as written: "none", or step ids
// separated by ", ", a Part 1 step followed by " (core)" when the fact is
// core there and a Part 2 step by its group's letter, as "C-05 (C)".
type Fact struct {
	ID, Title, Sitting, Mark, Part1, Part2 string
}

// Step is one row of the run-book's table "Steps to facts".
type Step struct {
	ID, Title, Where, Minutes, Facts string
}

// Facts gives the run-book's table "Facts to steps", in its order.
func Facts() []Fact { return slices.Clone(facts) }

// Steps gives the run-book's table "Steps to facts", in its order.
func Steps() []Step { return slices.Clone(steps) }

// Core reports whether the fact's mark makes any half of it core.
func (f Fact) Core() bool { return strings.HasPrefix(f.Mark, "core") }

// StepRef is one step a fact names, with its group's letter for a Part 2
// step and whether the fact is core there for a Part 1 step.
type StepRef struct {
	ID    string
	Group string
	Core  bool
}

// Refs gives the steps the fact names, Part 1's first.
func (f Fact) Refs() []StepRef {
	var refs []StepRef
	for _, cell := range []string{f.Part1, f.Part2} {
		if cell == "none" {
			continue
		}
		for _, item := range strings.Split(cell, ", ") {
			id, rest, _ := strings.Cut(item, " ")
			ref := StepRef{ID: id}
			switch {
			case rest == "(core)":
				ref.Core = true
			case len(rest) == 3 && rest[0] == '(' && rest[2] == ')':
				ref.Group = rest[1:2]
			}
			refs = append(refs, ref)
		}
	}
	return refs
}

// Scope names the steps of one summary: "sitting-1", "sitting-2", or a Part
// 2 group's letter. It gives the step ids in the run-book's order, and false
// for a scope the run-book does not have.
func Scope(name string) ([]string, bool) {
	where := ""
	switch {
	case name == "sitting-1":
		where = "Part 1, sitting 1"
	case name == "sitting-2":
		where = "Part 1, sitting 2"
	case len(name) == 1 && name[0] >= 'A' && name[0] <= 'Z':
		where = "Part 2, group " + name
	default:
		return nil, false
	}
	var ids []string
	for _, s := range steps {
		if s.Where == where {
			ids = append(ids, s.ID)
		}
	}
	return ids, len(ids) > 0
}

// FactsIn gives the facts that name at least one of the steps, in the
// table's order.
func FactsIn(stepIDs []string) []Fact {
	var out []Fact
	for _, f := range facts {
		for _, r := range f.Refs() {
			if slices.Contains(stepIDs, r.ID) {
				out = append(out, f)
				break
			}
		}
	}
	return out
}

// IsStep reports whether id is a step of the run-book.
func IsStep(id string) bool {
	return slices.ContainsFunc(steps, func(s Step) bool { return s.ID == id })
}

// VoiceLine is one line the owner says in voice call one (X-44), with its
// label: the part's number, and a letter for a second line in one part.
type VoiceLine struct {
	Label, Text string
}

// VoiceScript is what the owner says in X-44, in order, as the run-book's
// "Say" lines write it. TestVoiceScriptEqualsTheRunBook holds it equal.
var VoiceScript = []VoiceLine{
	{"1", "Hello. This is a test with made-up content. Please ask the agent to run the shell command sleep thirty, and then list the files in the orchard folder."},
	{"2", "Also ask the agent to create a file named fig dot t x t in the orchard folder, containing the single word ripe."},
	{"3", "Don't send this one to the agent. Just tell me, in one sentence: what colour is a ripe plum?"},
	{"4", "Suggest one short name for a new file about quinces. Don't create it yet."},
	{"4b", "Yes, use that name. Ask the agent to create that file with the word golden inside."},
	{"5", "Ask the agent to write the number four seven two one three into a file named port dot t x t."},
	{"5b", "Sorry, I said that wrong. The right number is four seven two three one. Ask the agent to fix port dot t x t."},
	{"6", "Off the record."},
	{"6b", "Ask the agent to create a file named pebble dot t x t containing the word pebble."},
	{"6c", "Back on the record."},
	{"6d", "Ask the agent to list the files in the orchard folder."},
	{"7", "This last line is made-up and is not for the agent: the lantern is blue."},
}

// Words splits a text into its lower-case words, for comparing what was said
// with what a hand-over asked.
func Words(text string) []string {
	return strings.FieldsFunc(strings.ToLower(text), func(r rune) bool {
		return !(r >= 'a' && r <= 'z' || r >= '0' && r <= '9' || r == '\'' || r == '-')
	})
}

// ClosestLine gives the script line whose words a request keeps most of,
// and how many of that line's words it keeps. Nothing of the request's text
// is returned.
func ClosestLine(request string) (label string, kept, of int) {
	have := map[string]bool{}
	for _, w := range Words(request) {
		have[w] = true
	}
	best := -1.0
	for _, l := range VoiceScript {
		words := Words(l.Text)
		n := 0
		for _, w := range words {
			if have[w] {
				n++
			}
		}
		if share := float64(n) / float64(len(words)); share > best {
			best, label, kept, of = share, l.Label, n, len(words)
		}
	}
	return label, kept, of
}
