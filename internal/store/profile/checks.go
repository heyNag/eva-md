package profile

import (
	"regexp"
	"slices"
	"strconv"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"

	"github.com/heyNag/eva-md/internal/ledger/event"
)

var (
	reDatetime  = regexp.MustCompile(`^[0-9]{4}-[0-9]{2}-[0-9]{2}T[0-9]{2}:[0-9]{2}:[0-9]{2}(Z|[+-][0-9]{2}:[0-9]{2})$`)
	reActor     = regexp.MustCompile(`^((human|process):[a-z0-9][a-z0-9._-]*|[a-z0-9][a-z0-9._-]*/[A-Za-z0-9][A-Za-z0-9._@-]*)$`)
	reSourceID  = regexp.MustCompile(`^e[1-9][0-9]*$`)
	reHash      = regexp.MustCompile(`^sha256:[0-9a-f]{64}$`)
	reAnchor    = regexp.MustCompile(`^[a-z0-9.-]+(/[A-Za-z0-9._-]+)+@[0-9a-f]{7,40}:[^\s:]*$`)
	reRemote    = regexp.MustCompile(`^[a-z0-9.-]+(:[0-9]+)?(/[A-Za-z0-9._~-]+)+$`)
	reCommit    = regexp.MustCompile(`^[0-9a-f]{40}$`)
	reLegacyKey = regexp.MustCompile(`^[A-Za-z0-9._-]{1,128}$`)
	reV1Path    = regexp.MustCompile(`^[a-z0-9][a-z0-9/_.-]*\.md$`)
	reRef       = regexp.MustCompile(`\[\^([A-Za-z0-9_-]+)\]`)
	reDef       = regexp.MustCompile(`^\[\^([A-Za-z0-9_-]+)\]: (.*)$`)
	reWorkLine  = regexp.MustCompile(`^- (.+) <!-- id:([a-z0-9]+) -->$`)
	reInForce   = regexp.MustCompile(`^- \[(.+)\]\(/([^)]+)\)$`)
	reLogLine   = regexp.MustCompile(`^- (did|found|failed|left): \S`)
	reIndexLine = regexp.MustCompile(`^(# \S.*|\* \[[^\]]+\]\([^)]+\) - \S.*)$`)
	reTOMLStr   = regexp.MustCompile(`^([a-z_]+) = "([^"\\]*)"$`)
	reTOMLInt   = regexp.MustCompile(`^([a-z_]+) = (0|[1-9][0-9]{0,8})$`)
	reReads     = regexp.MustCompile(`^reads = \[(0|[1-9][0-9]{0,8}), (0|[1-9][0-9]{0,8})\]$`)
	reLabel     = regexp.MustCompile(`^[a-z0-9-]{1,32}$`)
	reSignKey   = regexp.MustCompile(`^ssh-ed25519 [A-Za-z0-9+/]{68}$`)
)

var (
	kinds      = []string{"fact", "decision", "constraint", "procedure", "rule"}
	itemStates = []string{"active", "retired", "retracted"}
	wsStates   = []string{"active", "merged", "archived"}
	notes      = []string{"did", "found", "failed", "left"}
	skipped    = []string{"no_user_turn", "program", "subagent", "empty"}
	refused    = []string{"timeout", "answer_invalid", "tool_used"}
	reasons    = []string{"locked", "verified", "hand_edit", "authority", "project"}
	// rank orders the tags; echo adds no support.
	rank = map[string]int{"stated": 5, "accepted": 4, "observed": 3, "suggested": 2, "external": 1, "echo": 0}
	// pvSupports is the spec's table of the tags a span may support, by the
	// record's author of the event it sits in.
	pvSupports = map[event.Author][]string{
		event.AuthorTyped:    {"stated", "accepted", "external"},
		event.AuthorChosen:   {"accepted"},
		event.AuthorSpoken:   {"stated", "accepted"},
		event.AuthorTool:     {"observed", "external"},
		event.AuthorAgent:    {"suggested", "external", "echo"},
		event.AuthorSubagent: {"suggested", "external", "echo"},
		event.AuthorFront:    {"external"},
		event.AuthorPasted:   {"external"},
		event.AuthorHost:     {"external"},
		event.AuthorProgram:  {"external"},
		event.AuthorUnknown:  {"external"},
		event.AuthorEva:      {"echo"},
		event.AuthorV1User:   {"stated", "accepted", "external"},
	}
	kindTags = map[string][]string{
		"rule": {"stated"}, "constraint": {"stated"}, "decision": {"stated", "accepted"},
		"procedure": {"stated", "accepted", "observed"}, "fact": {"stated", "observed"},
	}
	migratedTags = []string{"stated", "accepted", "observed", "reading", "inferred"}
	allowed      = map[string][]string{
		"item-file": {"type", "id", "title", "description", "status", "state", "state_at", "state_reason", "scope", "project", "workstream",
			"tag", "words", "generated", "learned", "asserted_at", "confirmed", "supersedes", "branch", "anchors", "cues", "sources",
			"migrated_from", "locked", "verified"},
		"workstream-file":   {"type", "id", "title", "description", "status", "state", "state_at", "project", "branch", "generated", "sources", "locked", "verified"},
		"session-file":      {"type", "session", "project", "writer", "started", "workstreams", "generated", "covers", "sources"},
		"project-record":    {"type", "id", "title", "description", "remotes", "root_commits", "legacy_keys", "merged_into", "related", "shares_with"},
		"disagreement-file": {"type", "id", "item", "op", "reason", "sources", "at", "session", "project", "generated"},
		"readme":            {"type", "title", "description"},
	}
	types = map[string]string{"workstream-file": "workstream", "session-file": "session", "project-record": "project",
		"disagreement-file": "disagreement", "readme": "readme", "system-page": "system"}
	statics = map[string]string{"gitattributes": "* -text -filter -ident\n", "gitignore": ".tmp-*\n"}
)

// fc checks the fields of one document.
type fc struct {
	c *checker
	d *doc
}

func (f fc) fault(field, rule, detail string) { f.c.fault(f.d.path, field, rule, detail) }

// str is key's value as a string. A line a person wrote is read leniently.
func (f fc) str(key string, required bool) (string, bool) {
	e := f.d.get(key)
	if e == nil {
		if required {
			f.fault(key, key, "is required")
		}
		return "", false
	}
	if !e.written {
		return e.raw, true
	}
	s, ok := e.val.(string)
	if !ok {
		f.fault(key, key, "is not a string")
	}
	return s, ok
}

// value is key's decoded value. A line in no written form gives nothing:
// the written-form rule has said so, or the file is hand-edited and its
// lines are the person's.
func (f fc) value(key string, required bool) (any, bool) {
	e := f.d.get(key)
	if e == nil {
		if required {
			f.fault(key, key, "is required")
		}
		return nil, false
	}
	return e.val, e.written
}

func (f fc) list(key string, required bool) ([]any, bool) {
	v, ok := f.value(key, required)
	if !ok {
		return nil, false
	}
	l, isList := v.([]any)
	if !isList {
		f.fault(key, key, "is not a list")
	}
	return l, isList
}

// strings is key's value as a list of strings.
func (f fc) strings(key string, required bool) ([]string, bool) {
	l, ok := f.list(key, required)
	if !ok {
		return nil, false
	}
	var out []string
	for _, v := range l {
		s, isStr := v.(string)
		if !isStr {
			f.fault(key, key, "holds a value that is not a string")
			return nil, false
		}
		out = append(out, s)
	}
	return out, true
}

// text checks a single-line text of 1 to max characters.
func (f fc) text(key string, required bool, max int) (string, bool) {
	s, ok := f.str(key, required)
	if !ok {
		return "", false
	}
	if n := utf8.RuneCountInString(s); n == 0 || n > max || strings.IndexFunc(s, unicode.IsControl) >= 0 {
		f.fault(key, key, "is not one line of 1 to "+itoa(max)+" characters")
		return s, false
	}
	return s, true
}

// datetime checks an RFC 3339 time with seconds and an explicit offset.
func (f fc) datetime(key string, required bool) (time.Time, bool) {
	s, ok := f.str(key, required)
	if !ok {
		return time.Time{}, false
	}
	t, ok := parseTime(s)
	if !ok {
		f.fault(key, key, "is not a datetime with seconds and an offset")
	}
	return t, ok
}

func parseTime(s string) (time.Time, bool) {
	if !reDatetime.MatchString(s) {
		return time.Time{}, false
	}
	t, err := time.Parse(time.RFC3339, s)
	return t, err == nil
}

func (f fc) oneOf(key string, required bool, set []string) (string, bool) {
	s, ok := f.str(key, required)
	if ok && !slices.Contains(set, s) {
		f.fault(key, key, strconv.Quote(s)+" is not one of "+strings.Join(set, ", "))
		return s, false
	}
	return s, ok
}

// generated checks {"by": <actor>, "at": <datetime>}.
func (f fc) generated() {
	v, ok := f.value("generated", true)
	if !ok {
		return
	}
	m, isObj := v.(map[string]any)
	by, _ := m["by"].(string)
	at, _ := m["at"].(string)
	_, timeOK := parseTime(at)
	if !isObj || len(m) != 2 || !reActor.MatchString(by) || !timeOK {
		f.fault("generated", "generated", `is not {"by": <actor>, "at": <datetime>}`)
	}
}

// source is one entry of sources.
type source struct {
	id, resource, supports string
	pv                     event.Author
}

// sources checks the sources list and returns its entries.
func (f fc) sources(min, max int, migrated bool) ([]source, bool) {
	if f.d.get("sources") == nil && min == 0 {
		return nil, true
	}
	l, ok := f.list("sources", min > 0)
	if !ok {
		return nil, false
	}
	if len(l) < min || (max > 0 && len(l) > max) {
		f.fault("sources", "sources", "holds "+itoa(len(l))+" entries")
	}
	var out []source
	seen := map[string]bool{}
	for _, v := range l {
		m, isObj := v.(map[string]any)
		s := source{}
		s.id, _ = m["id"].(string)
		s.resource, _ = m["resource"].(string)
		s.supports, _ = m["supports"].(string)
		pv, _ := m["pv"].(string)
		hash, _ := m["hash"].(string)
		s.pv = event.Author(pv)
		bad := ""
		switch {
		case !isObj || len(m) != 5:
			bad = "an entry is not {id, resource, pv, supports, hash}"
		case !reSourceID.MatchString(s.id) || seen[s.id]:
			bad = "an id is not e<n> or is repeated: " + strconv.Quote(s.id)
		case !validResource(s.resource, migrated):
			bad = "a resource is not an event id: " + strconv.Quote(s.resource)
		case !s.pv.Valid():
			bad = "a pv is not one of the record's authors: " + strconv.Quote(pv)
		case !reHash.MatchString(hash):
			bad = "a hash is not sha256:<64 hex>"
		}
		if bad != "" {
			f.fault("sources", "sources", bad)
			return nil, false
		}
		seen[s.id] = true
		if !slices.Contains(pvSupports[s.pv], s.supports) || (s.pv == event.AuthorV1User && !migrated) {
			f.fault("sources", "supports", "a span by "+pv+" cannot support "+strconv.Quote(s.supports))
			return nil, false
		}
		out = append(out, s)
	}
	return out, true
}

func validResource(r string, migrated bool) bool {
	if migrated && strings.HasPrefix(r, "s:") {
		ref, err := event.ParseRef(r)
		return err == nil && ref.Legacy
	}
	_, err := event.ParseID(r)
	return err == nil
}

// sessionField checks <host>:<session id> and returns its parts.
func (f fc) sessionField(required bool) (host, sid string, ok bool) {
	s, ok := f.str("session", required)
	if !ok {
		return "", "", false
	}
	id, err := event.ParseID(s + "#1")
	if err != nil || id.String() != s+"#1" {
		f.fault("session", "session", "is not <host>:<session id>")
		return "", "", false
	}
	return id.Host, id.Session, true
}

// citations checks that every footnote names a source and every source is
// cited, and, with defs, that each source is defined as its resource.
func (f fc) citations(srcs []source, defs bool) {
	byID := map[string]string{}
	for _, s := range srcs {
		byID[s.id] = s.resource
	}
	cited, defined := map[string]bool{}, map[string]bool{}
	for _, line := range f.d.body {
		if m := reDef.FindStringSubmatch(line); m != nil {
			res, ok := byID[m[1]]
			if !ok || m[2] != res {
				f.fault("", "citations", "the definition of [^"+m[1]+"] is not its source's resource")
			}
			defined[m[1]] = true
			continue
		}
		for _, m := range reRef.FindAllStringSubmatch(line, -1) {
			if _, ok := byID[m[1]]; !ok {
				f.fault("", "citations", "[^"+m[1]+"] names no source")
			}
			cited[m[1]] = true
		}
	}
	for _, s := range srcs {
		if !cited[s.id] {
			f.fault("", "citations", "source "+s.id+" is not cited")
		}
		if defs && !defined[s.id] {
			f.fault("", "citations", "source "+s.id+" has no definition")
		}
	}
}

// statement checks the body's first line and returns it.
func (f fc) statement() string {
	for _, line := range f.d.body {
		if strings.TrimSpace(line) == "" {
			continue
		}
		bare := strings.TrimSpace(reRef.ReplaceAllString(line, ""))
		if strings.HasPrefix(line, "#") || bare == "" || utf8.RuneCountInString(line) > 500 {
			f.fault("", "statement", "the body's first line is not a statement of at most 500 characters")
		}
		return line
	}
	f.fault("", "statement", "the body has no statement")
	return ""
}

// section returns the lines under a "## <name>" heading, up to the next
// heading of that level or higher, and whether the heading is there.
func (f fc) section(name string) ([]string, bool) {
	var out []string
	in, found := false, false
	for _, line := range f.d.body {
		if strings.HasPrefix(line, "#") && (strings.HasPrefix(line, "## ") || strings.HasPrefix(line, "# ")) {
			in = line == "## "+name
			found = found || in
			continue
		}
		if in && strings.TrimSpace(line) != "" {
			out = append(out, line)
		}
	}
	return out, found
}
