package systempage

import (
	"encoding/json"
	"fmt"
	"regexp"
	"slices"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/heyNag/eva-md/internal/freetext"
	"github.com/heyNag/eva-md/internal/okfadapter"
)

// required are the frontmatter keys every page sets; repos and aliases may
// be left out.
var required = []string{"type", "title", "description", "status", "generated", "sources", "as_of", "state"}

// fixed are the keys whose value is always the same.
var fixed = map[string]string{"type": "system", "status": "stable", "state": "active"}

// keyLine is a frontmatter line that starts a key.
var keyLine = regexp.MustCompile(`^([a-z_]+):(?:[ ]+(.*))?$`)

// field is one key of the frontmatter: the value on its line and the lines
// of its block below.
type field struct {
	line  int
	value string
	block []item
}

// item is a line of a block or of a list, trimmed, with its number.
type item struct {
	line int
	text string
}

// checkFront reads the frontmatter's lines, from line first, into pg, and
// returns the line of each key. It reads the part of YAML a page needs: one
// key per line, a value on the key's line ("double quoted", 'single quoted'
// or plain, without the spaces around it, as okf saves it), lists as [a, b]
// or "- a" lines, and generated as { by: <host>/<model> } or a "by:" line.
// The page okf saves is built from these values, never from the text.
func checkFront(pg *page, lines []string, first int, now time.Time, p *problems) map[string]int {
	fields := parseFront(lines, first, p)
	for _, key := range required {
		if _, ok := fields[key]; !ok {
			p.add(0, "the frontmatter has no "+key)
		}
	}
	keys := map[string]int{}
	for key, f := range fields {
		keys[key] = f.line
		switch key {
		case "type", "status", "state":
			if v, ok := scalar(f, key, p); ok && v != fixed[key] {
				p.add(f.line, key+" must be "+fixed[key])
			}
		case "title":
			pg.title = text(f, key, maxTitle, p)
		case "description":
			pg.description = text(f, key, maxDescription, p)
		case "generated":
			pg.actor = generated(f, p)
		case "as_of":
			pg.asOf = date(f, now, p)
		case "sources":
			pg.sources = sources(f, p)
		case "repos":
			pg.repos = repos(f, p)
		case "aliases":
			pg.aliases = aliases(f, p)
		}
	}
	if pg.repos == nil {
		pg.repos = []string{}
	}
	if pg.aliases == nil {
		pg.aliases = []string{}
	}
	return keys
}

// parseFront splits the frontmatter into its keys. Unknown and repeated
// keys are refused, and their blocks are skipped.
func parseFront(lines []string, first int, p *problems) map[string]field {
	const skip = "-" // the key being skipped, which no key can be
	fields := map[string]field{}
	known := append(slices.Clone(required), "repos", "aliases")
	cur := ""
	for i, l := range lines {
		n := first + i
		switch {
		case strings.TrimSpace(l) == "":
		case l[0] == ' ' || l[0] == '-':
			if f, ok := fields[cur]; ok {
				f.block = append(f.block, item{n, strings.TrimSpace(l)})
				fields[cur] = f
			} else if cur == "" {
				p.add(n, "a frontmatter line under no key")
			}
		default:
			m := keyLine.FindStringSubmatch(l)
			if m == nil {
				p.add(n, "not a frontmatter line; a key starts its line, as in title: <title>")
				cur = ""
				continue
			}
			_, repeated := fields[m[1]]
			switch {
			case !slices.Contains(known, m[1]):
				p.add(n, "a key a system page doesn't have")
				cur = skip
			case repeated:
				p.add(n, m[1]+" is set twice")
				cur = skip
			default:
				cur, fields[m[1]] = m[1], field{line: n, value: strings.TrimSpace(m[2])}
			}
		}
	}
	return fields
}

// scalar reads a value written on its key's line.
func scalar(f field, key string, p *problems) (string, bool) {
	if len(f.block) > 0 || strings.HasPrefix(f.value, "|") || strings.HasPrefix(f.value, ">") {
		p.add(f.line, key+" takes one value, on its key's line")
		return "", false
	}
	v, ok := unquote(f.value)
	switch {
	case !ok:
		p.add(f.line, "a quoted value that doesn't close, or a bad escape in one")
	case strings.TrimSpace(v) == "":
		p.add(f.line, key+" is empty")
	case checkText(f.line, v, p):
		// okf saves a value without the spaces around it.
		return strings.TrimSpace(v), true
	}
	return "", false
}

// text reads a title or a description: plain text the map can show as it
// is, at most limit characters.
func text(f field, key string, limit int, p *problems) string {
	v, ok := scalar(f, key, p)
	if !ok || !plain(f.line, v, key, p) {
		return ""
	}
	if strings.Contains(v, "---") {
		p.add(f.line, key+" holds ---, which okf refuses there")
		return ""
	}
	if utf8.RuneCountInString(v) > limit {
		p.add(f.line, fmt.Sprintf("%s is over %d characters", key, limit))
		return ""
	}
	return v
}

// plain refuses what the map can't show as it is: a link, a speaker label
// such as [user], or the separator the map writes between a line's fields.
func plain(n int, v, key string, p *problems) bool {
	switch {
	case strings.Contains(v, "]("):
		p.add(n, key+" holds a link; links go in the body")
	case freetext.SpeakerShaped(v):
		p.add(n, key+" reads as a speaker label, such as [user]")
	case separates(v):
		p.add(n, key+" holds ' · ', which the map writes between its fields; use another separator")
	default:
		return true
	}
	return false
}

// generated reads the page's author, generated: { by: <host>/<model> },
// with the host claude-code or codex and the model unknown when the chat
// doesn't know it. An at: is left to okf, which sets the save time.
func generated(f field, p *problems) string {
	const rule = "generated is { by: claude-code/<model> } or { by: codex/<model> }, with the model unknown when not known"
	entries := f.value
	switch {
	case entries == "":
		var parts []string
		for _, it := range f.block {
			parts = append(parts, it.text)
		}
		entries = strings.Join(parts, ",")
	case strings.HasPrefix(entries, "{") && strings.HasSuffix(entries, "}") && len(f.block) == 0:
		entries = entries[1 : len(entries)-1]
	default:
		p.add(f.line, rule)
		return ""
	}
	by := ""
	for _, e := range splitFlow(entries) {
		if strings.TrimSpace(e) == "" {
			continue
		}
		k, v, ok := strings.Cut(e, ":")
		v, quoted := unquote(strings.TrimSpace(v))
		switch k = strings.TrimSpace(k); {
		case !ok || !quoted || k != "by" && k != "at":
			p.add(f.line, rule)
			return ""
		case k == "by":
			by = v
		}
	}
	host, model, _ := strings.Cut(by, "/")
	if host != "claude-code" && host != "codex" || !checkText(f.line, by, p) {
		p.add(f.line, rule)
		return ""
	}
	return okfadapter.Actor(host, model)
}

// date reads as_of, the day the content reflects, which is no later than
// tomorrow.
func date(f field, now time.Time, p *problems) string {
	v, ok := scalar(f, "as_of", p)
	if !ok {
		return ""
	}
	t, err := time.Parse(time.DateOnly, v)
	switch {
	case err != nil:
		p.add(f.line, "as_of is a day, YYYY-MM-DD")
	case t.After(now.AddDate(0, 0, 1)):
		p.add(f.line, "as_of is in the future")
	default:
		return v
	}
	return ""
}

// sources reads the sources, each a "- resource: <source>" line.
func sources(f field, p *problems) []string {
	items, ok := list(f)
	if !ok {
		p.add(f.line, "sources is a list of - resource: <source> lines")
		return nil
	}
	if len(items) == 0 {
		p.add(f.line, "sources is empty; cite at least one")
	}
	var out []string
	for _, it := range items {
		rest, isResource := strings.CutPrefix(it.text, "resource:")
		v, quoted := unquote(strings.TrimSpace(rest))
		if !isResource || !quoted || !checkText(it.line, v, p) || !source(v) {
			p.add(it.line, sourceRule)
			continue
		}
		out = append(out, v)
	}
	return out
}

// repos reads the repos and turns each into its project key, once.
func repos(f field, p *problems) []string {
	items, ok := list(f)
	if !ok {
		p.add(f.line, "repos is a list, [<remote or key>, ...] or - lines")
		return nil
	}
	out := []string{}
	for _, it := range items {
		v, quoted := unquote(it.text)
		key, isRepo := projectKey(v)
		if !quoted || !isRepo {
			p.add(it.line, "a repo is a git remote or a project key")
			continue
		}
		if !slices.Contains(out, key) {
			out = append(out, key)
		}
	}
	return out
}

// aliases reads the other names of the system.
func aliases(f field, p *problems) []string {
	items, ok := list(f)
	if !ok {
		p.add(f.line, "aliases is a list, [<name>, ...] or - lines")
		return nil
	}
	if len(items) > maxAliases {
		p.add(f.line, fmt.Sprintf("aliases holds more than %d names", maxAliases))
	}
	out := []string{}
	for _, it := range items {
		raw, quoted := unquote(it.text)
		v := strings.TrimSpace(raw)
		switch n := utf8.RuneCountInString(v); {
		case !quoted:
			p.add(it.line, "a quoted value that doesn't close, or a bad escape in one")
		case n < minAlias || n > maxAlias:
			p.add(it.line, fmt.Sprintf("an alias is %d to %d characters", minAlias, maxAlias))
		case checkText(it.line, raw, p) && plain(it.line, v, "an alias", p):
			out = append(out, v)
		}
	}
	return out
}

// list reads a list: [a, "b, c"] on the key's line, or "- a" lines below
// it. Each item keeps its line.
func list(f field) ([]item, bool) {
	if f.value != "" {
		if len(f.block) > 0 || !strings.HasPrefix(f.value, "[") || !strings.HasSuffix(f.value, "]") {
			return nil, false
		}
		var out []item
		for _, part := range splitFlow(f.value[1 : len(f.value)-1]) {
			if part = strings.TrimSpace(part); part != "" {
				out = append(out, item{f.line, part})
			}
		}
		return out, true
	}
	var out []item
	for _, it := range f.block {
		rest, ok := strings.CutPrefix(it.text, "-")
		if !ok || rest != "" && rest[0] != ' ' {
			return nil, false
		}
		out = append(out, item{it.line, strings.TrimSpace(rest)})
	}
	return out, true
}

// splitFlow splits a flow list or mapping at the commas outside quotes.
func splitFlow(s string) []string {
	var parts []string
	var quote byte
	start := 0
	for i := 0; i < len(s); i++ {
		switch c := s[i]; {
		case quote == '"' && c == '\\':
			i++
		case quote != 0:
			if c == quote {
				quote = 0
			}
		case c == '"' || c == '\'':
			quote = c
		case c == ',':
			parts = append(parts, s[start:i])
			start = i + 1
		}
	}
	return append(parts, s[start:])
}

// unquote reads a scalar: "double quoted" as JSON reads it, 'single quoted'
// with a quote inside written twice, or plain. It reports false for a quote
// that doesn't close.
func unquote(s string) (string, bool) {
	switch {
	case strings.HasPrefix(s, `"`):
		var v string
		if len(s) < 2 || !strings.HasSuffix(s, `"`) || json.Unmarshal([]byte(s), &v) != nil {
			return "", false
		}
		return v, true
	case strings.HasPrefix(s, "'"):
		if len(s) < 2 || !strings.HasSuffix(s, "'") {
			return "", false
		}
		return strings.ReplaceAll(s[1:len(s)-1], "''", "'"), true
	}
	return s, true
}
