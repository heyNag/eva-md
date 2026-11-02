package systempage

import (
	"slices"
	"strings"

	"github.com/heyNag/eva-md/internal/coderef"
)

// maxCited bounds the code sources ReadFront keeps from a page edited by
// hand; system write saves at most coderef.MaxRefs.
const maxCited = 64

// Front is what search reads from a saved page's frontmatter: the page's
// title, description and as_of, its other names and the code it cites.
type Front struct {
	Title, Description, AsOf string
	Aliases                  []string
	Code                     []Cited
	Line                     map[string]int // each key's line, counting from 1 at the first line given
}

// Cited is one code source as the page writes it, and the ref it reads as.
type Cited struct {
	Source string
	Ref    coderef.Ref
}

// ReadFront reads the lines between a saved page's --- lines, as okf saved
// them or a person edited them. It never refuses: a value that fails a check
// is left out, and so is a key a system page doesn't have. It keeps at most
// maxAliases aliases and maxCited code sources, each code source once.
func ReadFront(lines []string) Front {
	var p problems
	f := Front{Line: map[string]int{}}
	for key, fd := range parseFront(lines, 1, &p) {
		f.Line[key] = fd.line
		switch key {
		case "title":
			f.Title = savedValue(fd)
		case "description":
			f.Description = savedValue(fd)
		case "as_of":
			f.AsOf = savedValue(fd)
		case "aliases":
			items, _ := list(fd)
			for _, it := range items {
				v, ok := unquote(it.text)
				if v = strings.TrimSpace(v); ok && v != "" && len(f.Aliases) < maxAliases {
					f.Aliases = append(f.Aliases, v)
				}
			}
		case "sources":
			f.Code = savedCode(fd)
		}
	}
	return f
}

// savedValue reads a value written on its key's line, or "".
func savedValue(f field) string {
	if len(f.block) > 0 {
		return ""
	}
	v, ok := unquote(f.value)
	if !ok {
		return ""
	}
	return strings.TrimSpace(v)
}

// savedCode reads the code sources among a sources block's "- resource:"
// lines, passing over the URLs and the keys okf may save under a source.
func savedCode(f field) []Cited {
	var out []Cited
	for _, it := range f.block {
		rest, ok := strings.CutPrefix(it.text, "-")
		if !ok {
			continue
		}
		rest, ok = strings.CutPrefix(strings.TrimSpace(rest), "resource:")
		if !ok {
			continue
		}
		v, quoted := unquote(strings.TrimSpace(rest))
		r, isCode := coderef.Parse(v)
		if !quoted || !isCode || slices.ContainsFunc(out, func(c Cited) bool { return c.Ref == r }) {
			continue
		}
		if out = append(out, Cited{Source: v, Ref: r}); len(out) == maxCited {
			break
		}
	}
	return out
}
