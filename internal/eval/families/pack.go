package families

import (
	_ "embed"
	"encoding/json"
	"fmt"
	"slices"

	"github.com/heyNag/eva-md/internal/eval/scenario"
)

// tuningPack is packs/tuning.json: for each tuning family, its seeds and
// the hash of each instance.
//
//go:embed packs/tuning.json
var tuningPack []byte

// PackFile is the format of a pack.
type PackFile struct {
	Families []PackFamily `json:"families"`
}

// PackFamily is one family's instances in a pack.
type PackFamily struct {
	Family    string      `json:"family"`
	Instances []PackEntry `json:"instances"`
}

// PackEntry is one instance: its seed and the sha256 of its canonical
// JSON, as Hash gives it.
type PackEntry struct {
	Seed   uint64 `json:"seed"`
	SHA256 string `json:"sha256"`
}

// Tuning is the tuning pack's instances, in its order, each regenerated
// from its seed and checked against its hash. A generator change that
// alters an instance is an error until packs/tuning.json is updated.
func Tuning() ([]scenario.Scenario, error) { return readPack(tuningPack) }

// readPack regenerates and checks every instance of a pack. A pack lists
// every tuning family once, at least one instance of each, and no
// held-out family.
func readPack(data []byte) ([]scenario.Scenario, error) {
	var p PackFile
	if err := json.Unmarshal(data, &p); err != nil {
		return nil, fmt.Errorf("families: the pack: %w", err)
	}
	var out []scenario.Scenario
	var listed []string
	for _, pf := range p.Families {
		f, ok := ByName(pf.Family)
		switch {
		case !ok:
			return nil, fmt.Errorf("families: the pack lists no family %q", pf.Family)
		case f.Split != scenario.SplitTuning:
			return nil, fmt.Errorf("families: the pack lists %s, which is held out", f.Name)
		case slices.Contains(listed, f.Name):
			return nil, fmt.Errorf("families: the pack lists %s twice", f.Name)
		case len(pf.Instances) == 0:
			return nil, fmt.Errorf("families: the pack lists no instance of %s", f.Name)
		}
		listed = append(listed, f.Name)
		for _, e := range pf.Instances {
			sc := f.Generate(e.Seed)
			if slices.ContainsFunc(out, func(o scenario.Scenario) bool { return o.ID == sc.ID }) {
				return nil, fmt.Errorf("families: the pack lists %s twice", sc.ID)
			}
			if err := sc.Validate(); err != nil {
				return nil, fmt.Errorf("families: %w", err)
			}
			h, err := Hash(sc)
			if err != nil {
				return nil, fmt.Errorf("families: %s: %w", sc.ID, err)
			}
			if h != e.SHA256 {
				return nil, fmt.Errorf("families: %s is no longer the instance the pack pins: update packs/tuning.json", sc.ID)
			}
			out = append(out, sc)
		}
	}
	for _, f := range All() {
		if f.Split == scenario.SplitTuning && !slices.Contains(listed, f.Name) {
			return nil, fmt.Errorf("families: the pack lists no instance of %s", f.Name)
		}
	}
	return out, nil
}
