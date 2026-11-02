package resume

import (
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"slices"
	"strings"

	"github.com/heyNag/eva-md/internal/config"
	"github.com/heyNag/eva-md/internal/systempage"
)

const (
	// maxLinking bounds the system pages LinkedFrom reads.
	maxLinking = 2000
	// linkersShown is how many linking pages its line names.
	linkersShown = 12
)

// LinkedFrom says which system pages link to the system page ref names, or
// to a section of it, as one line for eva-md show and eva_show to print
// after the page: "eva.md: systems/orders: linked from systems/billing,
// systems/payments". It reads the other system pages through the wiki's
// pinned root, never the map, each up to systempage.MaxSaved, and finds
// their links as markdown reads them (see systempage.Links). It returns ""
// for a ref of another kind, a page no page links to and a wiki it can't
// read; a page it can't read or past maxLinking is passed over, and running
// out of time ends the reading.
func LinkedFrom(ctx context.Context, cfg config.Config, ref string) string {
	page, _, _ := strings.Cut(ref, "#")
	slug, ok := strings.CutPrefix(strings.TrimSuffix(page, ".md"), "systems/")
	if !ok || systempage.CheckSlug(slug) != nil {
		return ""
	}
	root, err := openWiki(cfg)
	if err != nil {
		return ""
	}
	defer root.Close()
	d, err := root.Open("systems")
	if err != nil {
		return ""
	}
	entries, _ := d.ReadDir(-1)
	d.Close()
	var linking []string
	read := 0
	for _, e := range entries {
		other, ok := strings.CutSuffix(e.Name(), ".md")
		if !ok || other == slug || !e.Type().IsRegular() || systempage.CheckSlug(other) != nil {
			continue
		}
		if read++; read > maxLinking || ctx.Err() != nil {
			break
		}
		if slices.Contains(systempage.Links(pageBody(root, filepath.Join("systems", e.Name()))), slug) {
			linking = append(linking, "systems/"+other)
		}
	}
	if len(linking) == 0 {
		return ""
	}
	slices.Sort(linking)
	line := "eva.md: systems/" + slug + ": linked from " + strings.Join(linking[:min(len(linking), linkersShown)], ", ")
	if n := len(linking) - linkersShown; n > 0 {
		line += fmt.Sprintf(" and %d more", n)
	}
	return line
}

// pageBody returns the lines of a page after its frontmatter, or none for a
// page that can't be read, is a link or is over systempage.MaxSaved.
func pageBody(root *os.Root, name string) []string {
	f, err := regular(root, name)
	if err != nil {
		return nil
	}
	defer f.Close()
	data, err := io.ReadAll(io.LimitReader(f, systempage.MaxSaved+1))
	if err != nil || len(data) > systempage.MaxSaved {
		return nil
	}
	lines := strings.Split(string(data), "\n")
	if strings.TrimRight(lines[0], "\r") != "---" {
		return lines
	}
	for i := 1; i < len(lines); i++ {
		if strings.TrimRight(lines[i], "\r") == "---" {
			return lines[i+1:]
		}
	}
	return nil
}
