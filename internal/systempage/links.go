package systempage

import (
	"path"
	"slices"
	"strings"
)

// Links returns the slugs of the system pages a saved page's body links to,
// each once, in the order first linked: a relative link to <slug>.md,
// perhaps with a #section, outside code spans, as markdown reads it, and as
// system write lets a page link. A reserved name, such as the map's link
// that ends every page, names no page.
func Links(body []string) []string {
	var out []string
	for _, l := range body {
		text := codeSpans(l)
		var targets []string
		for _, m := range inlineLink.FindAllStringSubmatch(text, -1) {
			targets = append(targets, m[1])
		}
		if m := refDef.FindStringSubmatch(text); m != nil {
			targets = append(targets, m[1])
		}
		for _, t := range targets {
			file, _, _ := strings.Cut(t, "#")
			if file == "" || scheme.MatchString(file) {
				continue
			}
			slug, ok := strings.CutSuffix(path.Clean(file), ".md")
			if ok && CheckSlug(slug) == nil && !slices.Contains(out, slug) {
				out = append(out, slug)
			}
		}
	}
	return out
}
