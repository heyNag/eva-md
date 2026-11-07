package mcp

import (
	"fmt"
	"path/filepath"
	"strings"
	"testing"

	"github.com/heyNag/eva-md/internal/systempage"
)

// The system map and the system index past their 25 KB, each one list with
// no section to ask for, are cut with a note that names no section: it
// gives the command that prints the list whole and eva_search, and for the
// map the index, which lists the systems in shorter lines. A page with no
// "## " heading at all is cut with the command alone.
func TestShowCutsALongSystemList(t *testing.T) {
	f := newFixture(t)
	f.wiki(t)
	bin := filepath.Join(f.s.Dirs.Share, "bin", "eva-md")
	var m, index strings.Builder
	m.WriteString("---\ntype: map\ntitle: System map\n---\n\n# System map\n\n")
	index.WriteString("# Systems\n\n")
	for i := 0; m.Len() < 40<<10 || index.Len() < 40<<10; i++ {
		slug := fmt.Sprintf("ledger-%04d", i)
		fmt.Fprintf(&m, "- [Ledger %04d](%s.md): Keeps the books of region %04d · repos: %s · code: %s@4e2bf4c:internal/ledger · as of 2026-10-28\n", i, slug, i, f.project.Key, f.project.Key)
		fmt.Fprintf(&index, "* [Ledger %04d](%s.md) - Keeps the books of region %04d, closing each day's entries before the nightly export\n", i, slug, i)
	}
	writeFile(t, filepath.Join(f.c.WikiPath, "systems", "map.md"), m.String())
	writeFile(t, filepath.Join(f.c.WikiPath, "systems", "index.md"), index.String())
	for ref, hint := range map[string]string{
		"systems/map":      "the rest lists more systems; systems/index lists them all in shorter lines, " + bin + " show 'systems/map' prints the map whole, and eva_search finds a system page by its words]",
		"systems/map.md":   "the rest lists more systems; systems/index lists them all in shorter lines, " + bin + " show 'systems/map.md' prints the map whole, and eva_search finds a system page by its words]",
		"systems/index":    "the rest lists more systems; " + bin + " show 'systems/index' prints it whole, and eva_search finds a system page by its words]",
		"systems/index.md": "the rest lists more systems; " + bin + " show 'systems/index.md' prints it whole, and eva_search finds a system page by its words]",
	} {
		got := f.answer(t, "eva_show", map[string]any{"ref": ref})
		if len(got) > systempage.MaxSaved+len(showLabel)+1 || !strings.Contains(got, "(ledger-0000.md)") || !strings.HasSuffix(got, " KB left; "+hint) {
			t.Fatalf("%s: %d bytes:\n%s", ref, len(got), got[max(len(got)-400, 0):])
		}
	}
	var notes strings.Builder
	notes.WriteString("---\ntype: notes\ntitle: Notes\n---\n\n# Notes\n\n")
	for i := 0; notes.Len() < 40<<10; i++ {
		fmt.Fprintf(&notes, "The nightly export closed batch %d without a retry.\n", i)
	}
	writeFile(t, filepath.Join(f.c.WikiPath, "projects", "orders", "notes.md"), notes.String())
	got := f.answer(t, "eva_show", map[string]any{"ref": "projects/orders/notes"})
	if len(got) > showCap+len(showLabel)+1 || !strings.HasSuffix(got, " KB left; the text goes on; "+bin+" show 'projects/orders/notes' prints it whole]") {
		t.Fatalf("a page with no section: %d bytes:\n%s", len(got), got[max(len(got)-400, 0):])
	}
}
