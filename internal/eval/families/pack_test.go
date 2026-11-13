package families

import (
	"encoding/json"
	"flag"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/heyNag/eva-md/internal/eval/scenario"
)

// update rewrites packs/tuning.json and the golden renderings from the
// generators as they stand: go test ./internal/eval/families -update. The
// diff it leaves is the change to review.
var update = flag.Bool("update", false, "rewrite the tuning pack and the golden renderings")

// packSeeds are the seeds a new tuning family starts with in the pack.
func packSeeds() []uint64 {
	var seeds []uint64
	for s := range uint64(20) {
		seeds = append(seeds, s+1)
	}
	return seeds
}

// writePack writes the tuning pack as the generators make it, keeping each
// family's seeds.
func writePack(t *testing.T) {
	t.Helper()
	var old PackFile
	if err := json.Unmarshal(tuningPack, &old); err != nil {
		t.Fatal(err)
	}
	var p PackFile
	for _, f := range All() {
		if f.Split != scenario.SplitTuning {
			continue
		}
		seeds := packSeeds()
		for _, pf := range old.Families {
			if pf.Family == f.Name && len(pf.Instances) > 0 {
				seeds = nil
				for _, e := range pf.Instances {
					seeds = append(seeds, e.Seed)
				}
			}
		}
		pf := PackFamily{Family: f.Name}
		for _, s := range seeds {
			h, err := Hash(f.Generate(s))
			if err != nil {
				t.Fatal(err)
			}
			pf.Instances = append(pf.Instances, PackEntry{Seed: s, SHA256: h})
		}
		p.Families = append(p.Families, pf)
	}
	data, err := json.MarshalIndent(p, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join("packs", "tuning.json"), append(data, '\n'), 0o644); err != nil {
		t.Fatal(err)
	}
	tuningPack = append(data, '\n')
}

// TestPackHoldsTheGenerators: the committed pack lists every tuning
// family, at least 20 instances of each, and each instance is the one its
// seed makes now.
func TestPackHoldsTheGenerators(t *testing.T) {
	if *update {
		writePack(t)
	}
	scs, err := Tuning()
	if err != nil {
		t.Fatal(err)
	}
	count := map[string]int{}
	for _, sc := range scs {
		count[sc.Family]++
	}
	for _, f := range All() {
		switch {
		case f.Split == scenario.SplitTuning && count[f.Name] < 20:
			t.Errorf("the pack holds %d instances of %s, want at least 20", count[f.Name], f.Name)
		case f.Split == scenario.SplitHeldout && count[f.Name] != 0:
			t.Errorf("the pack holds %d instances of %s, which is held out", count[f.Name], f.Name)
		}
	}
}

// reword changes one word of a template: its first word that is no
// placeholder.
func reword(tmpl string) string {
	words := strings.Split(tmpl, " ")
	for i, w := range words {
		if !strings.Contains(w, "{") {
			words[i] = "Quokka"
			return strings.Join(words, " ")
		}
	}
	panic("a template with no word: " + tmpl)
}

// TestPackCatchesEveryTemplate: changing one word of any template a tuning
// family renders, its fillers included, fails the pack; changing one of a
// held-out family's does not, since none of its words is in the pack.
func TestPackCatchesEveryTemplate(t *testing.T) {
	tuned := map[string]bool{"filler": true}
	for _, f := range All() {
		if f.Split == scenario.SplitTuning {
			tuned[f.Name] = true
		}
	}
	if _, err := readPack(tuningPack); err != nil {
		t.Fatal(err)
	}
	checked := 0
	for family, roles := range texts {
		for role, list := range roles {
			for i, tmpl := range list {
				list[i] = reword(tmpl)
				_, err := readPack(tuningPack)
				list[i] = tmpl
				switch {
				case tuned[family] && (err == nil || !strings.Contains(err.Error(), "update packs/tuning.json")):
					t.Errorf("%s %s %d: reworded, the pack says %v", family, role, i, err)
				case !tuned[family] && err != nil:
					t.Errorf("%s %s %d: a held-out template changes the pack: %v", family, role, i, err)
				}
				checked++
			}
		}
	}
	if _, err := readPack(tuningPack); err != nil {
		t.Fatalf("the templates were not put back: %v", err)
	}
	if checked < 60 {
		t.Errorf("%d templates checked; the walk missed some", checked)
	}
}

// TestPackRefusesWhatItDoesNotPin: a pack that lists a held-out family, a
// family that does not exist, one twice, one with no instance, an instance
// twice, an instance whose hash differs, or that leaves a tuning family
// out, is an error.
func TestPackRefusesWhatItDoesNotPin(t *testing.T) {
	var good PackFile
	if err := json.Unmarshal(tuningPack, &good); err != nil {
		t.Fatal(err)
	}
	clone := func() PackFile {
		var p PackFile
		for _, pf := range good.Families {
			p.Families = append(p.Families, PackFamily{Family: pf.Family, Instances: slices.Clone(pf.Instances)})
		}
		return p
	}
	for _, c := range []struct {
		name, want string
		mutate     func(*PackFile)
	}{
		{"a held-out family", "which is held out", func(p *PackFile) {
			p.Families = append(p.Families, PackFamily{Family: "limit", Instances: []PackEntry{{Seed: 1}}})
		}},
		{"an unknown family", "lists no family", func(p *PackFile) {
			p.Families = append(p.Families, PackFamily{Family: "wish", Instances: []PackEntry{{Seed: 1}}})
		}},
		{"a family twice", "twice", func(p *PackFile) { p.Families = append(p.Families, p.Families[0]) }},
		{"a family with no instance", "no instance of", func(p *PackFile) { p.Families[1].Instances = nil }},
		{"an instance twice", "twice", func(p *PackFile) {
			p.Families[0].Instances = append(p.Families[0].Instances, p.Families[0].Instances[0])
		}},
		{"a hash that differs", "update packs/tuning.json", func(p *PackFile) {
			p.Families[2].Instances[3].SHA256 = strings.Repeat("0", 64)
		}},
		{"a seed that differs", "update packs/tuning.json", func(p *PackFile) { p.Families[0].Instances[0].Seed += 1000 }},
		{"a tuning family left out", "no instance of", func(p *PackFile) { p.Families = p.Families[1:] }},
	} {
		p := clone()
		c.mutate(&p)
		data, err := json.Marshal(p)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := readPack(data); err == nil || !strings.Contains(err.Error(), c.want) {
			t.Errorf("%s: %v, want an error that says %q", c.name, err, c.want)
		}
	}
	if _, err := readPack([]byte("{")); err == nil {
		t.Error("a pack that is not JSON is read")
	}
}
