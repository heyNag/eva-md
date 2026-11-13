// Package families generates the fidelity instrument's new scenario
// families: chats that plant the failures the field report and the owner's
// note found and v1's 12 scripts do not. Three are for tuning, and their
// instances are pinned in packs/tuning.json; three are held out, and their
// instances are drawn at gate time from a seed taken from the commit under
// test, so none exists while code is written. Every text comes from the
// templates and banks committed here, and every value in them is made up.
// No release contains it, and it imports no code that organizes memory.
package families

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"hash/fnv"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/heyNag/eva-md/internal/eval/scenario"
	"github.com/heyNag/eva-md/internal/host/adapter"
	"github.com/heyNag/eva-md/internal/ledger/event"
)

// Family is one family of generated scenarios.
type Family struct {
	Name  string
	Split scenario.Split
	// Generate makes the family's instance for seed, whose id is
	// <name>.<seed in hex>. The same seed always gives the same instance.
	Generate func(seed uint64) scenario.Scenario
}

// All lists the families, the tuning ones first.
func All() []Family {
	return []Family{
		{Name: "moment", Split: scenario.SplitTuning, Generate: moment},
		{Name: "correction", Split: scenario.SplitTuning, Generate: correction},
		{Name: "other-author", Split: scenario.SplitTuning, Generate: otherAuthor},
		{Name: "limit", Split: scenario.SplitHeldout, Generate: limit},
		{Name: "unagreed", Split: scenario.SplitHeldout, Generate: unagreed},
		{Name: "accepted", Split: scenario.SplitHeldout, Generate: accepted},
	}
}

// ByName is the family called name.
func ByName(name string) (Family, bool) {
	for _, f := range All() {
		if f.Name == name {
			return f, true
		}
	}
	return Family{}, false
}

// Draw makes n instances of the family called name from seed, each from a
// seed of its own that seed's stream gives. A held-out family is drawn so
// at gate time, with SeedOf the candidate's commit. The stream starts from
// the seed mixed, not from the seed itself: splitmix64 steps by a constant,
// so two raw starts that differ by a few steps would share all but a few
// instances. Mixed, two draws from different seeds share an instance only
// by a collision of 64-bit numbers.
func Draw(name string, seed uint64, n int) ([]scenario.Scenario, error) {
	f, ok := ByName(name)
	if !ok {
		return nil, fmt.Errorf("families: no family %q", name)
	}
	if n < 1 {
		return nil, errors.New("families: a draw makes at least one instance")
	}
	g := &gen{state: mix(seed)}
	var out []scenario.Scenario
	seen := map[uint64]bool{}
	for len(out) < n {
		s := g.next()
		if seen[s] {
			continue
		}
		seen[s] = true
		out = append(out, f.Generate(s))
	}
	return out, nil
}

// SeedOf is the seed of a draw at gate time: the first 16 hex digits of
// the candidate's commit, read as a number. The commit is given in
// lower-case hex, 16 digits or more.
func SeedOf(commit string) (uint64, error) {
	if len(commit) < 16 || strings.Trim(commit, "0123456789abcdef") != "" {
		return 0, errors.New("families: a commit is 16 or more lower-case hex digits")
	}
	return strconv.ParseUint(commit[:16], 16, 64)
}

// Hash is the sha256, in hex, of an instance's canonical JSON, as a pack
// pins it.
func Hash(sc scenario.Scenario) (string, error) {
	data, err := json.Marshal(sc)
	if err != nil {
		return "", err
	}
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:]), nil
}

// gen is the families' random stream, splitmix64, written out here so an
// instance depends on no library's choice of algorithm.
type gen struct{ state uint64 }

// golden is splitmix64's step.
const golden = 0x9e3779b97f4a7c15

func (g *gen) next() uint64 {
	g.state += golden
	return mix(g.state)
}

// mix is splitmix64's finalizer, a bijection of 64-bit numbers that
// scatters numbers close together far apart.
func mix(z uint64) uint64 {
	z = (z ^ z>>30) * 0xbf58476d1ce4e5b9
	z = (z ^ z>>27) * 0x94d049bb133111eb
	return z ^ z>>31
}

// intn is a number from 0 to n-1.
func (g *gen) intn(n int) int { return int(g.next() % uint64(n)) }

func (g *gen) pick(list []string) string { return list[g.intn(len(list))] }

// apart draws k entries of list, no entry twice.
func (g *gen) apart(list []string, k int) []string {
	list = slices.Clone(list)
	for i := range k {
		j := i + g.intn(len(list)-i)
		list[i], list[j] = list[j], list[i]
	}
	return list[:k]
}

// names draws k names of the bank, no two alike.
func (g *gen) names(k int) []string { return g.apart(names, k) }

// names is the bank every planted name is drawn from: made-up names of
// services, steps, stores and checks, each with a letter from g to z, so a
// results row can be checked for it.
var names = []string{
	"amber-gull", "basalt-wren", "cobalt-moth", "dune-lark", "ember-vole", "fjord-newt", "garnet-owl", "hollow-pike",
	"indigo-mink", "juniper-toad", "kelp-heron", "lumen-stoat", "marrow-finch", "nimbus-crow", "onyx-lynx", "pewter-shrew",
	"quartz-loon", "rust-marten", "slate-ibis", "tundra-mole", "umber-skink", "velvet-tern", "willow-gecko", "yarrow-hare",
}

// texts holds every template, by family and the role of the turn it
// writes; the turns that plant nothing are the fillers'. A planting turn's
// role has at least three. {v}, {w} and {n} are the values a turn holds.
// The pack pins what they render, so a change to one word of a tuning
// family's template, or of a filler the tuning families use, fails the
// pack until it is updated.
var texts = map[string]map[string][]string{
	"filler": fillerTexts, "heldout-filler": heldoutFillerTexts,
	"moment": momentTexts, "correction": correctionTexts, "other-author": otherAuthorTexts,
	"limit": limitTexts, "unagreed": unagreedTexts, "accepted": acceptedTexts,
}

var fillerTexts = map[string][]string{
	"ask": {
		"Can you show me what the scheduler logged overnight?",
		"Let's look at the retry loop next.",
		"What did the last test run say?",
		"Walk me through the queue code once more.",
	},
	"answer": {
		"The scheduler logged nothing unusual overnight.",
		"The retry loop reads cleanly; nothing to change there.",
		"The last run passed and nothing new came up.",
		"The queue code is as we left it on Monday.",
	},
	"tool": {
		"Bash go test ./internal/queue/... (exit 0)",
		"Bash go vet ./... (exit 0)",
		"Read internal/queue/queue.go (212 lines)",
	},
}

// builder writes one instance: one session, its turns a few minutes apart.
type builder struct {
	g  *gen
	sc scenario.Scenario
	at time.Time
}

func newBuilder(name string, split scenario.Split, seed uint64, what string) *builder {
	// The family's name is mixed into the stream, so that one seed gives
	// each family other sessions and values.
	h := fnv.New64a()
	h.Write([]byte(name))
	g := &gen{state: seed ^ h.Sum64()}
	host := string(adapter.ClaudeCode)
	if g.intn(2) == 1 {
		host = string(adapter.Codex)
	}
	id := fmt.Sprintf("%08x-%04x-4%03x-8%03x-%012x", g.next()>>32, g.intn(1<<16), g.intn(1<<12), g.intn(1<<12), g.next()>>16)
	b := &builder{g: g, at: time.Date(2026, 2, 2+g.intn(20), 9, g.intn(30), 0, 0, time.UTC)}
	b.sc = scenario.Scenario{ID: fmt.Sprintf("%s.%x", name, seed), Family: name, Name: what, Split: split, Seed: seed,
		Sessions: []scenario.Session{{Host: host, ID: id, Branch: g.pick([]string{"main", "feature/queue", "fix/retry"})}},
		Passes:   []scenario.Pass{{}}}
	return b
}

// text renders a template of family's role, filling {v}, {w} and {n}.
func (b *builder) text(family, role string, v, w, n string) string {
	return fillIn(b.g.pick(texts[family][role]), v, w, n)
}

// fillIn fills a template's {v}, {w} and {n}.
func fillIn(tmpl, v, w, n string) string {
	return strings.NewReplacer("{v}", v, "{w}", w, "{n}", n).Replace(tmpl)
}

// say adds a turn that plants facts, each given its id here, and returns
// where it stands. A fact whose kinds hold a dated line stands under the
// turn's day.
func (b *builder) say(author event.Author, kind event.Kind, agent, text string, facts ...*scenario.Fact) scenario.Ref {
	b.at = b.at.Add(time.Duration(1+b.g.intn(4)) * time.Minute)
	t := scenario.Turn{At: b.at, Author: author, Kind: kind, Agent: agent, Text: text}
	for _, f := range facts {
		f.ID = fmt.Sprintf("f%d", len(b.sc.Facts)+1)
		if slices.Contains(f.Kinds, scenario.KindLog) {
			f.LogDate = b.at.Format(time.DateOnly)
		}
		b.sc.Facts = append(b.sc.Facts, *f)
		t.Plants = append(t.Plants, f.ID)
	}
	s := &b.sc.Sessions[0]
	s.Turns = append(s.Turns, t)
	return scenario.Ref{Session: s.ID, Seq: int64(len(s.Turns))}
}

// fill adds from least to most exchanges that plant nothing: the user asks,
// a tool now and then runs, and the agent gives the answer that goes with
// the question. A held-out family's exchanges come from templates of their
// own, so that nothing of it is tuned on through the tuning families. It
// returns where the first turn stands.
func (b *builder) fill(least, most int) scenario.Ref {
	from := "filler"
	if b.sc.Split == scenario.SplitHeldout {
		from = "heldout-filler"
	}
	first := scenario.Ref{Session: b.sc.Sessions[0].ID, Seq: int64(len(b.sc.Sessions[0].Turns) + 1)}
	for range least + b.g.intn(most-least+1) {
		i := b.g.intn(len(texts[from]["ask"]))
		b.say(event.AuthorTyped, event.KindPrompt, "", texts[from]["ask"][i])
		if b.g.intn(4) == 0 {
			b.say(event.AuthorTool, event.KindTool, "", b.text(from, "tool", "", "", ""))
		}
		b.say(event.AuthorAgent, event.KindReply, "", texts[from]["answer"][i])
	}
	return first
}

// lastID is the id of the fact planted last.
func (b *builder) lastID() string { return b.sc.Facts[len(b.sc.Facts)-1].ID }

// end sets the turn at which the fact with id ends.
func (b *builder) end(id string, at scenario.Ref) {
	for i := range b.sc.Facts {
		if b.sc.Facts[i].ID == id {
			b.sc.Facts[i].Until = &at
		}
	}
}

// fact is a fact to plant, with its kinds, authority, Must and slots.
func fact(must scenario.Must, authority scenario.Authority, kinds []scenario.Kind, slots ...scenario.Slot) *scenario.Fact {
	return &scenario.Fact{Kinds: kinds, Authority: authority, Must: must, Slots: slots}
}

func kinds(k ...scenario.Kind) []scenario.Kind { return k }

func slot(class scenario.SlotClass, value string, forms ...string) scenario.Slot {
	return scenario.Slot{Class: class, Value: value, Forms: forms}
}

// done ends the instance, with an exchange that plants nothing after it.
func (b *builder) done() scenario.Scenario {
	b.fill(0, 1)
	return b.sc
}
