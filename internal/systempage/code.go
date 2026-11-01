package systempage

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"time"

	"github.com/heyNag/eva-md/internal/coderef"
	"github.com/heyNag/eva-md/internal/config"
	"github.com/heyNag/eva-md/internal/okfadapter"
	"github.com/heyNag/eva-md/internal/paths"
)

// maxMap bounds the system map ReadCode reads, as the brief bounds it.
const maxMap = 256 << 10

// MaxSaved bounds a system page as system write saves it, in bytes: 1 KB
// over the 24 KB cap of the page as written, for the lines the save adds.
const MaxSaved = maxPage + 1<<10

// Code is what the system pages cite of code, for a pass that measures it
// (see coderef.Status).
type Code struct {
	Pages      []coderef.Page
	Unreadable []string // the pages okf can't read, by slug
	MapWithout int      // the pages whose line on the map lacks the code they cite
}

// ReadCode reads what the system pages cite of code: each page's slug,
// as_of, review day and code sources, from its frontmatter as okf reads it,
// at most MaxSaved a page, and the system map, at most 256 KB, to count the
// pages whose line lacks their code, as a page written before the map
// carried code does until it is written again. With slugs, it reads those
// pages only, and a slug with no page is an error. It writes nothing.
func ReadCode(c config.Config, dirs paths.Dirs, slugs ...string) (Code, error) {
	w, err := openWiki(c, dirs)
	if err != nil {
		return Code{}, err
	}
	for _, slug := range slugs {
		if !isPage(w, slug) {
			return Code{}, fmt.Errorf("systempage: there is no system page systems/%s.md", slug)
		}
	}
	if len(slugs) == 0 {
		if slugs, err = pageSlugs(w); err != nil {
			return Code{}, err
		}
	}
	listed := mapCode(w)
	var out Code
	for _, slug := range slugs {
		pg, err := w.ReadMax("systems/"+slug, MaxSaved)
		if err != nil {
			out.Unreadable = append(out.Unreadable, slug)
			continue
		}
		p := codePage(slug, pg.Concept)
		if len(p.Refs) > 0 && !listed[slug] {
			out.MapWithout++
		}
		out.Pages = append(out.Pages, p)
	}
	return out, nil
}

// pageSlugs lists the system pages by slug, sorted; a wiki without the
// systems folder has none.
func pageSlugs(w *okfadapter.Wiki) ([]string, error) {
	files, err := os.ReadDir(filepath.Join(w.Root(), "systems"))
	if errors.Is(err, fs.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("systempage: list the system pages: %w", err)
	}
	var out []string
	for _, f := range files {
		if slug, ok := strings.CutSuffix(f.Name(), ".md"); ok && f.Type().IsRegular() && CheckSlug(slug) == nil {
			out = append(out, slug)
		}
	}
	slices.Sort(out)
	return out, nil
}

// mapCode reports, by slug, the pages whose line on the system map lists
// code. A map that is missing, linked, not a regular file or over 256 KB
// lists none.
func mapCode(w *okfadapter.Wiki) map[string]bool {
	out := map[string]bool{}
	info, err := os.Lstat(filepath.Join(w.Root(), "systems", "map.md"))
	if err != nil || !info.Mode().IsRegular() || info.Size() > maxMap {
		return out
	}
	data, err := readWikiFile(w, "systems/map.md")
	if err != nil || len(data) > maxMap {
		return out
	}
	for _, l := range strings.Split(data, "\n") {
		if e, ok := ParseMapLine(strings.TrimRight(l, "\r")); ok && len(e.Code) > 0 {
			out[e.Slug] = true
		}
	}
	return out
}

// codePage is what a pass measures of a page okf read: its as_of and review
// day, when each is a day, and its code sources.
func codePage(slug string, c *okfadapter.Concept) coderef.Page {
	p := coderef.Page{Slug: slug}
	var sources []string
	for _, s := range c.Sources {
		sources = append(sources, s.Resource)
	}
	p.Refs = codeRefs(sources)
	if as, _ := c.Extra["as_of"].(string); validDay(as) {
		p.AsOf = as
	}
	if validDay(c.StaleAfter) {
		p.ReviewBy = c.StaleAfter
	}
	return p
}

func validDay(s string) bool {
	_, err := time.Parse(time.DateOnly, s)
	return err == nil
}
