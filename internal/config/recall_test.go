package config

import (
	"os"
	"path/filepath"
	"reflect"
	"testing"
)

// recall = false stops the knowledge a prompt gets: absent it is true, an
// explicit false stays false through a save, and a misspelled key is
// reported as unknown rather than read as the switch.
func TestRecallSwitch(t *testing.T) {
	isolate(t)
	if !Default().Recall {
		t.Fatal("recall is off by default")
	}
	file := filepath.Join(t.TempDir(), "config.toml")
	for _, tc := range []struct {
		body    string
		recall  bool
		unknown []string
	}{
		{"notice = \"start\"\n", true, nil},
		{"recall = false\n", false, nil},
		{"recall = true\nmcp = false\n", true, nil},
		{"recalls = false\n", true, []string{"recalls"}},
	} {
		if err := os.WriteFile(file, []byte(tc.body), 0o600); err != nil {
			t.Fatal(err)
		}
		c, err := Load(file)
		if err != nil || c.Recall != tc.recall || !reflect.DeepEqual(c.Unknown, tc.unknown) {
			t.Fatalf("%q: recall %v, unknown %q, %v", tc.body, c.Recall, c.Unknown, err)
		}
		if err := Save(file, c); err != nil {
			t.Fatal(err)
		}
		if again, err := Load(file); err != nil || again.Recall != tc.recall {
			t.Fatalf("%q saved and read back: %v, %v", tc.body, again.Recall, err)
		}
	}
	if err := os.WriteFile(file, []byte("recall = \"no\"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := Load(file); err == nil {
		t.Error("a string for recall was accepted")
	}
}
