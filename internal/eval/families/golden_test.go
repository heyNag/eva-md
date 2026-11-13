package families

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"slices"
	"testing"

	"github.com/heyNag/eva-md/internal/eval/scenario"
	"github.com/heyNag/eva-md/internal/inbox"
)

// TestGoldenV1Renderings: each tuning family's first instance in the pack
// renders, through V1Entries for its session's host, to the entries
// committed in testdata/golden; a held-out family has no golden file.
func TestGoldenV1Renderings(t *testing.T) {
	var p PackFile
	if err := json.Unmarshal(tuningPack, &p); err != nil {
		t.Fatal(err)
	}
	for _, f := range All() {
		path := filepath.Join("testdata", "golden", f.Name+".json")
		if f.Split == scenario.SplitHeldout {
			if _, err := os.Stat(path); !os.IsNotExist(err) {
				t.Errorf("%s is held out, yet %s exists (%v)", f.Name, path, err)
			}
			continue
		}
		i := slices.IndexFunc(p.Families, func(pf PackFamily) bool { return pf.Family == f.Name && len(pf.Instances) > 0 })
		if i < 0 {
			t.Fatalf("the pack holds no instance of %s", f.Name)
		}
		sc := f.Generate(p.Families[i].Instances[0].Seed)
		var sessions [][]inbox.Entry
		for _, s := range sc.Sessions {
			entries, err := scenario.V1Entries(s, s.Host)
			if err != nil {
				t.Fatalf("%s: %v", sc.ID, err)
			}
			sessions = append(sessions, entries)
		}
		got, err := json.MarshalIndent(sessions, "", "  ")
		if err != nil {
			t.Fatal(err)
		}
		got = append(got, '\n')
		if *update {
			if err := os.WriteFile(path, got, 0o644); err != nil {
				t.Fatal(err)
			}
		}
		want, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		if !bytes.Equal(got, want) {
			t.Errorf("%s renders other entries than %s holds", sc.ID, path)
		}
	}
}
