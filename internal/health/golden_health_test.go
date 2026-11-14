package health

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/heyNag/eva-md/internal/paths"
)

// The rows eva-md status and doctor print with the switch health on, on
// the fixture homes of TestGoldenRows, are pinned byte for byte: each
// stage's lag, the counted losses, one row for each host nobody uses and
// the verdict. Their heartbeats are from long ago and their chat folders
// are missing, so neither host is used.
func TestGoldenRowsWithHealthOn(t *testing.T) {
	for _, tc := range []struct {
		name string
		home int
		run  func(context.Context, paths.Dirs) Report
	}{
		{"status-health", setUpHome, Status},
		{"doctor-health", setUpHome, doctor},
		{"doctor-codex-health", codexHome, doctor},
		{"status-nowiki-health", noWikiHome, Status},
	} {
		t.Run(tc.name, func(t *testing.T) {
			d, home := goldenHome(t, tc.home)
			f, err := os.OpenFile(d.ConfigFile(), os.O_APPEND|os.O_WRONLY, 0)
			if err != nil {
				t.Fatal(err)
			}
			_, err = f.WriteString("[switches]\nhealth = true\n")
			f.Close()
			if err != nil {
				t.Fatal(err)
			}
			got := render(t, tc.run(t.Context(), d), home)
			file := filepath.Join("testdata", tc.name+".golden")
			if *updateGolden {
				if err := os.WriteFile(file, got, 0o644); err != nil {
					t.Fatal(err)
				}
			}
			want, err := os.ReadFile(file)
			if err != nil {
				t.Fatal(err)
			}
			if !bytes.Equal(got, want) {
				t.Fatalf("%s rows differ from %s:\n--- got\n%s--- want\n%s", tc.name, file, got, want)
			}
		})
	}
}

// With the switch set false, the rows are v1's golden rows, byte for byte.
func TestGoldenRowsWithHealthOff(t *testing.T) {
	for _, tc := range []struct {
		name string
		home int
		run  func(context.Context, paths.Dirs) Report
	}{
		{"status", setUpHome, Status},
		{"doctor", setUpHome, doctor},
		{"doctor-codex", codexHome, doctor},
		{"status-nowiki", noWikiHome, Status},
	} {
		t.Run(tc.name, func(t *testing.T) {
			d, home := goldenHome(t, tc.home)
			f, err := os.OpenFile(d.ConfigFile(), os.O_APPEND|os.O_WRONLY, 0)
			if err != nil {
				t.Fatal(err)
			}
			_, err = f.WriteString("[switches]\nhealth = false\n")
			f.Close()
			if err != nil {
				t.Fatal(err)
			}
			got := render(t, tc.run(t.Context(), d), home)
			want, err := os.ReadFile(filepath.Join("testdata", tc.name+".golden"))
			if err != nil {
				t.Fatal(err)
			}
			if !bytes.Equal(got, want) {
				t.Fatalf("%s rows with the switch off differ from v1's:\n--- got\n%s--- want\n%s", tc.name, got, want)
			}
		})
	}
}
