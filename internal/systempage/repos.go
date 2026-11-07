package systempage

import (
	"cmp"
	"slices"
	"strings"
)

// ReposFile is the repos index, which the prompt hook reads to learn which
// projects share a system page with the chat's, so that recall may show
// their knowledge. It lives in the wiki's hidden folder, which okf ignores
// and the daily commit stages. After its header, each line is the id of a
// system page that lists two or more repos and the project keys of those
// repos (see MapEntry), separated by tabs, sorted by id; a page that lists
// fewer makes no project another's sibling and has no line. Every write
// builds it again from the pages, and so does eva-md doctor, with the names
// index, when it is missing, unreadable or out of date. With no such page
// it is no file.
const ReposFile = ".eva-md/repos.tsv"

// MaxReposSize bounds the repos index. Past it, the pages with the oldest
// as_of are left out, a page without one counting as oldest.
const MaxReposSize = 64 << 10

// reposHeader is the repos index's first line, naming its format.
const reposHeader = "# eva-md repos index 1"

// reposIndex builds the repos index's text from the system pages, within
// MaxReposSize, and returns how many pages it lists: "" and 0 when no page
// lists two repos.
func reposIndex(entries []system) (string, int) {
	type line struct{ id, text, asOf string }
	var lines []line
	size := len(reposHeader) + 1
	for _, e := range entries {
		var keys []string
		for _, k := range e.Repos {
			if k = strings.Join(strings.Fields(k), " "); k != "" && !slices.Contains(keys, k) {
				keys = append(keys, k)
			}
		}
		if len(keys) < 2 {
			continue
		}
		l := line{id: "systems/" + e.Slug, asOf: e.AsOf}
		l.text = l.id + "\t" + strings.Join(keys, "\t") + "\n"
		lines, size = append(lines, l), size+len(l.text)
	}
	if size > MaxReposSize {
		oldest := slices.Clone(lines)
		slices.SortFunc(oldest, func(a, b line) int { return cmp.Or(strings.Compare(a.asOf, b.asOf), strings.Compare(a.id, b.id)) })
		for _, l := range oldest {
			if size <= MaxReposSize {
				break
			}
			lines = slices.DeleteFunc(lines, func(k line) bool { return k.id == l.id })
			size -= len(l.text)
		}
	}
	if len(lines) == 0 {
		return "", 0
	}
	var b strings.Builder
	b.WriteString(reposHeader + "\n")
	for _, l := range lines {
		b.WriteString(l.text)
	}
	return b.String(), len(lines)
}

// ParseRepos reads the repos index: the project keys of each system page it
// lists. Text that does not start with the index's header gives none, and a
// line that is not one of its lines, a page's id and two or more keys, is
// skipped.
func ParseRepos(data []byte) [][]string {
	first, rest, _ := strings.Cut(string(data), "\n")
	if first != reposHeader {
		return nil
	}
	var out [][]string
	for l := range strings.SplitSeq(rest, "\n") {
		f := strings.Split(l, "\t")
		slug, page := strings.CutPrefix(f[0], "systems/")
		if !page || CheckSlug(slug) != nil || len(f) < 3 || slices.Contains(f[1:], "") {
			continue
		}
		out = append(out, f[1:])
	}
	return out
}

// Siblings returns the projects that share a system page with project, by
// the repos index's pages: every other key of a page that lists it.
func Siblings(pages [][]string, project string) map[string]bool {
	out := map[string]bool{}
	for _, keys := range pages {
		if slices.Contains(keys, project) {
			for _, k := range keys {
				if k != project {
					out[k] = true
				}
			}
		}
	}
	return out
}
