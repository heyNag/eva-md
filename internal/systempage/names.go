package systempage

import (
	"cmp"
	"context"
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"unicode"

	"github.com/heyNag/eva-md/internal/okfadapter"
)

// NamesFile is the names index, which the prompt hook reads to point a
// prompt that names a system at its page. It lives in the wiki's hidden
// folder, which okf ignores and the daily commit stages. Each line is a
// name, the page's id and its title, separated by tabs, sorted by name: the
// names are each system page's title, slug and aliases, as normalName makes
// them. Every write builds it again from the pages, and so does eva-md
// doctor when it is missing, unreadable or out of date.
const NamesFile = ".eva-md/names.tsv"

// Bounds of the names index. Past them, the aliases of the pages with the
// oldest as_of are left out first, then, if need be, those pages' other
// names.
const (
	MaxNames     = 2000
	MaxNamesSize = 128 << 10
)

// Name is a line of the names index: a name of a system page, the page's id,
// systems/<slug>, and its title.
type Name struct{ Name, ID, Title string }

func (n Name) line() string { return n.Name + "\t" + n.ID + "\t" + n.Title }

// NamesReport is what the names index lists.
type NamesReport struct {
	Pages, Names int // the system pages and the names the index lists
	// NoAliases and LeftOut count the oldest pages whose aliases, and whose
	// every name, the bounds left out.
	NoAliases, LeftOut int
	// Rebuilt reports an index that was missing, unreadable or out of date,
	// and was written again, and Stale one that was left so.
	Rebuilt, Stale bool
}

// names builds the names index's text from the system pages, within its
// bounds.
func names(entries []system) (string, NamesReport) {
	r := NamesReport{Pages: len(entries)}
	type page struct {
		asOf, slug   string
		own, aliases []Name // the title's and the slug's names, and the aliases'
	}
	pages := make([]page, len(entries))
	count, size := 0, 0
	for i, e := range entries {
		pg := page{asOf: e.AsOf, slug: e.Slug}
		seen := map[string]bool{}
		add := func(list *[]Name, s string) {
			if n := normalName(s); n != "" && !seen[n] {
				seen[n] = true
				name := Name{n, "systems/" + e.Slug, e.Title}
				*list = append(*list, name)
				count, size = count+1, size+len(name.line())+1
			}
		}
		add(&pg.own, e.Title)
		add(&pg.own, e.Slug)
		for _, a := range e.aliases {
			add(&pg.aliases, a)
		}
		pages[i] = pg
	}
	oldest := make([]*page, len(pages))
	for i := range pages {
		oldest[i] = &pages[i]
	}
	slices.SortFunc(oldest, func(a, b *page) int { return cmp.Or(strings.Compare(a.asOf, b.asOf), strings.Compare(a.slug, b.slug)) })
	over := func() bool { return count > MaxNames || size > MaxNamesSize }
	drop := func(list *[]Name) {
		for _, n := range *list {
			count, size = count-1, size-len(n.line())-1
		}
		*list = nil
	}
	for _, pg := range oldest {
		if !over() {
			break
		}
		if len(pg.aliases) > 0 {
			drop(&pg.aliases)
			r.NoAliases++
		}
	}
	for _, pg := range oldest {
		if !over() {
			break
		}
		drop(&pg.own)
		r.LeftOut++
	}
	var all []Name
	for _, pg := range pages {
		all = append(append(all, pg.own...), pg.aliases...)
	}
	slices.SortFunc(all, func(a, b Name) int { return cmp.Or(strings.Compare(a.Name, b.Name), strings.Compare(a.ID, b.ID)) })
	var b strings.Builder
	for _, n := range all {
		b.WriteString(n.line() + "\n")
	}
	r.Names = len(all)
	return b.String(), r
}

// normalName is a name as the index lists it: on one line, with runs of
// space made one and anything other than a letter or a digit taken off its
// ends. Its case is kept, since a prompt matches some names only in it.
func normalName(s string) string {
	s = strings.Join(strings.Fields(strings.Map(func(r rune) rune {
		if hidden(r) {
			return ' '
		}
		return r
	}, s)), " ")
	return strings.TrimFunc(s, func(r rune) bool { return !unicode.IsLetter(r) && !unicode.IsDigit(r) })
}

// ParseNames reads the names index, skipping a line that is not one of its
// lines: a name, the id of a system page and a title, separated by tabs.
func ParseNames(data []byte) []Name {
	var out []Name
	for l := range strings.SplitSeq(string(data), "\n") {
		name, rest, ok := strings.Cut(l, "\t")
		id, title, more := strings.Cut(rest, "\t")
		slug, page := strings.CutPrefix(id, "systems/")
		if !ok || !more || name == "" || strings.Contains(title, "\t") || !page || CheckSlug(slug) != nil {
			continue
		}
		out = append(out, Name{name, id, title})
	}
	return out
}

// RefreshNames writes the names index again from the system pages when it
// is missing, unreadable or out of date, as eva-md doctor does, and reports
// what it lists. It reads the pages without the wiki lock, and takes the
// lock, reading them again under it, only to write; it waits for the lock
// as any write of the wiki does, up to a minute, whatever ctx says. Without
// write, as eva-md status, it writes nothing and reports such an index as
// Stale.
func RefreshNames(ctx context.Context, o Options, write bool) (NamesReport, error) {
	w, err := openWiki(o.Config, o.Dirs)
	if err != nil {
		return NamesReport{}, err
	}
	text, r := names(systems(w))
	if r.Stale = !current(w, text); !r.Stale || !write {
		return r, nil
	}
	if err := ctx.Err(); err != nil {
		return NamesReport{}, err
	}
	err = w.Locked(func(lw *okfadapter.Wiki) error {
		if text, r = names(systems(lw)); current(lw, text) {
			return nil
		}
		r.Rebuilt = true
		return lw.WriteFile(NamesFile, []byte(text))
	})
	if err != nil {
		return NamesReport{}, err
	}
	return r, nil
}

// current reports whether the names index on disk is text: no file when
// text is empty, and otherwise a regular file of text's size that holds it,
// so a file far too large is never read.
func current(w *okfadapter.Wiki, text string) bool {
	info, err := os.Lstat(filepath.Join(w.Root(), filepath.FromSlash(NamesFile)))
	if err != nil {
		return errors.Is(err, fs.ErrNotExist) && text == ""
	}
	if !info.Mode().IsRegular() || info.Size() != int64(len(text)) {
		return false
	}
	old, err := readWikiFile(w, NamesFile)
	return err == nil && old == text
}
