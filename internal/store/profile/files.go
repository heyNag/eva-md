package profile

import (
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/heyNag/eva-md/internal/ledger/event"
	"github.com/heyNag/eva-md/internal/store/codec"
)

// handEdits reads every hand-edit record, so the checks of each file know
// whether it holds a person's lines.
func (c *checker) handEdits() {
	exists := map[string]bool{}
	for _, l := range c.locs {
		exists[l.path] = true
	}
	// A line may name a path that is gone, as a union cannot drop it; it
	// must still be a path a hand edit can be recorded for.
	recordable := func(p string) bool {
		l := classify(p)
		return l.bad == "" && (documents[l.class] || l.class == "readme" || l.class == "index")
	}
	newest := map[string][2]string{} // path: at, state
	for _, l := range c.locs {
		if l.class != "hand-edits" || l.bad != "" {
			continue
		}
		lines, ok := c.textFile(l)
		if !ok {
			continue
		}
		fault := func(detail string) { c.fault(l.path, "", "hand-edits", detail) }
		if !c.writers[l.writer] {
			fault("writer " + l.writer + " has no record")
		}
		if len(lines) == 0 || lines[0] != "path\tat\tstate" {
			fault("the header is not path, at, state")
			continue
		}
		for i, line := range lines[1:] {
			cols := strings.Split(line, "\t")
			if len(cols) != 3 {
				fault("line " + itoa(i+2) + " does not have three columns")
				continue
			}
			t, timeOK := parseTime(cols[1])
			switch {
			case !recordable(cols[0]):
				fault("line " + itoa(i+2) + " names no path of a document, README.md or index.md")
			case !timeOK:
				fault("line " + itoa(i+2) + " has no datetime")
			case cols[2] != "unacknowledged" && cols[2] != "acknowledged":
				fault("line " + itoa(i+2) + " has a state that is not unacknowledged or acknowledged")
			default:
				c.hand[cols[0]] = true
				if prev, ok := newest[cols[0]]; ok {
					// The newest line holds; on a tie, unacknowledged
					// holds, whichever writer's record is read first.
					pt, _ := parseTime(prev[0])
					if t.Before(pt) || t.Equal(pt) && prev[1] == "unacknowledged" {
						continue
					}
				}
				newest[cols[0]] = [2]string{cols[1], cols[2]}
			}
		}
	}
	for p, v := range newest {
		if v[1] == "unacknowledged" && exists[p] {
			c.rep.Unacknowledged = append(c.rep.Unacknowledged, p)
		}
	}
	slices.Sort(c.rep.Unacknowledged)
}

// textFile reads a static or record file, which must be readable text with
// LF line ends and a final newline, and returns its lines.
func (c *checker) textFile(l loc) ([]string, bool) {
	b, err := c.read(l.path)
	if err != nil {
		c.fault(l.path, "", "unreadable", "cannot be read")
		return nil, false
	}
	if why := codec.UnreadableText(b); why != "" {
		c.fault(l.path, "", "unreadable", "a record or static file holds "+why)
		return nil, false
	}
	s := string(b)
	if strings.Contains(s, "\r") || !strings.HasSuffix(s, "\n") {
		c.fault(l.path, "", "encoding", "has a carriage return or no final newline")
		return nil, false
	}
	return strings.Split(strings.TrimSuffix(s, "\n"), "\n"), true
}

// file checks one file.
func (c *checker) file(l loc) {
	if namesLibrary(l.path) {
		c.fault(l.path, "", "name", "the path names the library")
	}
	if l.class == "" {
		c.fault(l.path, "", "path", "is in no class of the layout")
		return
	}
	if l.bad != "" {
		c.fault(l.path, "", l.class, l.bad)
		return
	}
	switch l.class {
	case "notes", "legacy", "hand-edits":
		return
	case "index":
		if c.hand[l.path] {
			// A person's index is their text; it need only be readable.
			if b, err := c.read(l.path); err != nil || codec.UnreadableText(b) != "" {
				c.fault(l.path, "", "unreadable", "a static file cannot be read")
			}
			return
		}
		fallthrough
	case "gitattributes", "gitignore", "manifest", "schema-marker", "writer-record", "coverage-v1":
		lines, ok := c.textFile(l)
		if !ok {
			return
		}
		if l.class != "coverage-v1" && namesLibrary(strings.Join(lines, "\n")) {
			c.fault(l.path, "", "name", "the file names the library")
		}
		c.static(l, lines)
		return
	}
	b, err := c.read(l.path)
	if err != nil {
		b = []byte{0}
	}
	if max := caps[l.class]; max > 0 && len(b) > max {
		c.fault(l.path, "", "size", "is "+itoa(len(b))+" bytes, over "+itoa(max))
	}
	d := load(l.path, b)
	d.loc, d.hand = l, c.hand[l.path]
	if d.unreadable != "" {
		if l.class == "readme" {
			c.fault(l.path, "", "unreadable", "a static file holds "+d.unreadable)
		} else {
			c.rep.Protected[l.path] = Protection{Unreadable: true}
		}
		return
	}
	if !d.hand && (strings.Contains(string(b), "\r") || !strings.HasSuffix(string(b), "\n")) {
		c.fault(l.path, "", "encoding", "has a carriage return or no final newline")
	}
	if l.class == "readme" && !d.hand && namesLibrary(string(b)) {
		c.fault(l.path, "", "name", "the file names the library")
	}
	if d.prot != (Protection{}) {
		c.rep.Protected[l.path] = d.prot
	}
	c.frontmatter(d)
	c.files[l.path] = d
	f := fc{c, d}
	if want := types[l.class]; want != "" {
		if s, ok := f.str("type", true); ok && s != want {
			f.fault("type", "type", "is "+strconv.Quote(s)+", not "+want)
		}
	}
	switch l.class {
	case "readme":
		f.text("title", false, 200)
		f.text("description", false, 300)
	case "item-file":
		c.item(f)
	case "workstream-file":
		c.workstream(f)
	case "session-file":
		c.session(f)
	case "disagreement-file":
		c.disagreement(f)
	case "project-record":
		c.project(f)
	case "system-page":
		c.system(f)
	}
}

// frontmatter checks what holds for every document's block.
func (c *checker) frontmatter(d *doc) {
	seen := map[string]bool{}
	mayProtect := slices.Contains([]string{"item-file", "workstream-file", "system-page"}, d.loc.class)
	// Which marks an entry's fault has named: an unknown key or a merge is
	// faulted once, under locked, and names both.
	var carriedLocked, carriedVerified bool
	for _, e := range d.entries {
		if namesLibrary(e.Key) {
			c.fault(d.path, e.Key, "name", "a key names the library")
		}
		if e.Key == "locked" || e.Key == "verified" || e.Key == unknownKey || e.Key == mergeKey {
			if !mayProtect {
				rule := e.Key
				if rule != "verified" {
					rule = "locked"
				}
				c.fault(d.path, e.Key, rule, "only items, workstreams and system pages may carry it")
				carriedLocked = carriedLocked || e.Key != "verified"
				carriedVerified = carriedVerified || e.Key != "locked"
			}
			if e.Key == "verified" && e.Written {
				checkVerified(c, d, e)
			}
			continue
		}
		if d.hand || d.loc.class == "system-page" {
			continue
		}
		if seen[e.Key] {
			c.fault(d.path, e.Key, "frontmatter", "the key is repeated")
		}
		seen[e.Key] = true
		if !slices.Contains(allowed[d.loc.class], e.Key) {
			c.fault(d.path, e.Key, "frontmatter", "the key is not one of the class's")
		}
	}
	// A mark on no entry the codec reads as the key, such as "locked:false"
	// or a line with a tab before its key, still protects, and is still
	// carried: each mark no entry's fault named has a fault of its own.
	if !mayProtect && d.prot.Locked && !carriedLocked {
		c.fault(d.path, "locked", "locked", "only items, workstreams and system pages may carry it")
	}
	if !mayProtect && d.prot.Verified && !carriedVerified {
		c.fault(d.path, "verified", "verified", "only items, workstreams and system pages may carry it")
	}
	if d.hand || d.loc.class == "system-page" {
		return
	}
	for _, n := range d.opaque {
		c.fault(d.path, "", "written-form", "line "+itoa(n)+" is in no written form")
	}
	for _, e := range d.entries {
		if !e.Written {
			c.fault(d.path, e.Key, "written-form", "line "+itoa(e.Line)+" is in no written form")
		}
	}
}

func checkVerified(c *checker, d *doc, e codec.Entry) {
	list, isList := e.Value.([]any)
	if !isList {
		list = []any{e.Value}
	}
	for _, v := range list {
		m, _ := v.(map[string]any)
		by, _ := m["by"].(string)
		at, _ := m["at"].(string)
		if _, ok := parseTime(at); !ok || !reActor.MatchString(by) {
			c.fault(d.path, "verified", "verified", `an entry is not {"by": <actor>, "at": <datetime>}`)
			return
		}
	}
}

// static checks the static and record files, whose bytes the format fixes.
func (c *checker) static(l loc, lines []string) {
	fault := func(field, rule, detail string) { c.fault(l.path, field, rule, detail) }
	switch l.class {
	case "index":
		for i, line := range lines {
			if line != "" && !reIndexLine.MatchString(line) {
				fault("", "index", "line "+itoa(i+1)+" is neither a heading nor an entry")
			}
		}
	case "gitattributes", "gitignore":
		if strings.Join(lines, "\n")+"\n" != statics[l.class] {
			fault("", l.class, "is not "+strconv.Quote(statics[l.class]))
		}
	case "manifest":
		keys := []string{"schema", "min_reader", "store_id", "epoch"}
		if len(lines) != 4 {
			fault("", "manifest", "is not four lines")
			return
		}
		vals := map[string]int{}
		for i, k := range keys {
			if k == "store_id" {
				// The id is a string; a number of any form is no s id.
				if m := reTOMLStr.FindStringSubmatch(lines[i]); m != nil && m[1] == k {
					if !validID(m[2], 's') {
						fault(k, k, "is not an s id")
					}
				} else if strings.HasPrefix(lines[i], k+" = ") {
					fault(k, k, "is not a string holding an s id")
				} else {
					fault("", "manifest", "line "+itoa(i+1)+" is not "+k)
					return
				}
			} else if m := reTOMLInt.FindStringSubmatch(lines[i]); m != nil && m[1] == k {
				vals[k], _ = strconv.Atoi(m[2])
			} else {
				fault("", "manifest", "line "+itoa(i+1)+" is not "+k)
				return
			}
		}
		if vals["schema"] != 2 {
			fault("schema", "schema", "is not 2")
		}
		if vals["min_reader"] < 2 || vals["min_reader"] > vals["schema"] {
			fault("min_reader", "min_reader", "is not from 2 to schema")
		}
		if vals["epoch"] < 1 {
			fault("epoch", "epoch", "is not 1 or more")
		}
	case "schema-marker":
		m := reTOMLInt.FindStringSubmatch(strings.Join(lines, "\n"))
		if m == nil || m[1] != "schema_version" {
			fault("", "schema-marker", "is not one line schema_version = <n>")
		} else if m[2] != "2" {
			fault("schema_version", "schema_version", "is not 2")
		}
	case "writer-record":
		if len(lines) != 3 {
			fault("", "writer-record", "is not three lines")
			return
		}
		label := reTOMLStr.FindStringSubmatch(lines[0])
		key := reTOMLStr.FindStringSubmatch(lines[1])
		reads := reReads.FindStringSubmatch(lines[2])
		if label == nil || label[1] != "label" || key == nil || key[1] != "signing_key" || reads == nil {
			fault("", "writer-record", "the lines are not label, signing_key and reads")
			return
		}
		if !reLabel.MatchString(label[2]) {
			fault("label", "label", "is not 1 to 32 of a-z0-9-")
		}
		if key[2] != "" && !reSignKey.MatchString(key[2]) {
			fault("signing_key", "signing_key", "is neither empty nor an ed25519 public key")
		}
		lo, _ := strconv.Atoi(reads[1])
		hi, _ := strconv.Atoi(reads[2])
		if lo > hi || lo < 1 {
			fault("reads", "reads", "is not two generations, lowest first")
		}
	case "coverage-v1":
		if lines[0] != "session\tthrough" {
			fault("", "coverage-v1", "the header is not session, through")
			return
		}
		seen := map[string]bool{}
		for i, line := range lines[1:] {
			s, n, ok := strings.Cut(line, "\t")
			id, err := event.ParseID(s + "#1")
			if !ok || err != nil || id.String() != s+"#1" || seen[s] || !positive(n) {
				fault("", "coverage-v1", "line "+itoa(i+2)+" is not a new session and a line number")
			}
			seen[s] = true
		}
	}
}

func positive(s string) bool {
	n, err := strconv.ParseInt(s, 10, 64)
	return err == nil && n > 0 && strconv.FormatInt(n, 10) == s
}

// areaProject checks project on a file in an area: the folder's in a
// project, an existing project or none in global/.
func (c *checker) areaProject(f fc) {
	l := f.d.loc
	if l.area != "global" {
		if s, ok := f.str("project", true); ok && s != l.pid {
			f.fault("project", "project", "is not the folder's project "+l.pid)
		}
		return
	}
	if s, ok := f.str("project", false); ok {
		c.later = append(c.later, func() {
			if !c.projectExists(s) {
				f.fault("project", "project", "names no project of the store")
			}
		})
	}
}

func (c *checker) projectExists(pid string) bool {
	return strings.HasSuffix(c.ids[pid], "/project.md")
}

func (c *checker) item(f fc) {
	l := f.d.loc
	kind, kindOK := f.oneOf("type", true, kinds)
	if id, ok := f.str("id", true); ok {
		if id != l.id {
			f.fault("id", "id", "is not the file name's id")
		}
	}
	c.month(f, idTime(l.id))
	f.text("title", true, 200)
	f.text("description", false, 300)
	state, stateOK := f.oneOf("state", true, itemStates)
	c.status(f, state, stateOK)
	learned, learnedOK := f.datetime("learned", true)
	if t, ok := f.datetime("state_at", true); ok && learnedOK && t.Before(learned) {
		f.fault("state_at", "state_at", "is before learned")
	}
	if t, ok := f.datetime("asserted_at", true); ok && learnedOK && t.Before(learned) {
		f.fault("asserted_at", "asserted_at", "is before learned")
	}
	if t, ok := f.datetime("confirmed", false); ok && learnedOK && t.Before(learned) {
		f.fault("confirmed", "confirmed", "is before learned")
	}
	if stateOK {
		reason, has := f.str("state_reason", false)
		want := map[string][]string{"retired": {"user", "deleted_by_hand"}, "retracted": {"forgotten"}}[state]
		if has != (state != "active") || (has && !slices.Contains(want, reason)) {
			f.fault("state_reason", "state_reason", "does not fit state "+state)
		}
	}
	scope, scopeOK := f.str("scope", true)
	if scopeOK && (l.area == "global") != (scope == "all") || scopeOK && !slices.Contains([]string{"here", "project", "all"}, scope) {
		f.fault("scope", "scope", "is not here or project in a project, or all in global/")
	}
	c.areaProject(f)
	if ws, ok := f.str("workstream", false); ok {
		if l.area == "global" {
			f.fault("workstream", "workstream", "an item for every project has no workstream")
		} else {
			c.later = append(c.later, func() {
				if p := c.ids[ws]; !strings.HasPrefix(p, l.area+"/workstreams/") {
					f.fault("workstream", "workstream", "names no workstream of the item's project")
				}
			})
		}
	} else if scope == "here" {
		f.fault("workstream", "workstream", "is required for scope here")
	}
	from, migrated := f.str("migrated_from", false)
	if migrated && !reV1Path.MatchString(from) {
		f.fault("migrated_from", "migrated_from", "is not a v1 page's path")
	}
	min := 1
	if migrated {
		min = 0
	}
	srcs, srcOK := f.sources(min, 3, migrated)
	tag, tagOK := f.str("tag", true)
	if tagOK && kindOK && (srcOK || migrated) {
		if why := tagFault(kind, tag, srcs, migrated); why != "" {
			f.fault("tag", "tag", why)
		}
	}
	words, hasWords := f.str("words", false)
	switch {
	case kind != "rule" && kind != "constraint" && hasWords:
		f.fault("words", "words", "only a rule or a constraint carries the user's words")
	case kindOK && (kind == "rule" || kind == "constraint") && !hasWords && !migrated:
		f.fault("words", "words", "a rule or a constraint holds the user's words")
	case hasWords && (utf8Len(words) == 0 || utf8Len(words) > 240 || strings.ContainsAny(words, "\n\r")):
		f.fault("words", "words", "is not one line of 1 to 240 characters")
	}
	f.generated()
	if ids, ok := f.strings("supersedes", false); ok {
		seen := map[string]bool{}
		for _, id := range ids {
			if id == l.id || seen[id] || !validID(id, 'i') {
				f.fault("supersedes", "supersedes", strconv.Quote(id)+" is its own id, repeated or not an item id")
				continue
			}
			seen[id] = true
			c.later = append(c.later, func() {
				if p := c.ids[id]; !strings.Contains(p, "/items/") {
					f.fault("supersedes", "supersedes", id+" names no item of the store")
				}
			})
		}
	}
	if b, ok := f.str("branch", false); ok && !validBranch(b) {
		f.fault("branch", "branch", "is not a git branch name")
	}
	if a, ok := f.strings("anchors", false); ok {
		if len(a) > 16 {
			f.fault("anchors", "anchors", "holds more than 16")
		}
		for _, s := range a {
			if !reAnchor.MatchString(s) {
				f.fault("anchors", "anchors", strconv.Quote(s)+" is not <repo>@<commit>:<path>")
			}
		}
	}
	if cues, ok := f.strings("cues", false); ok {
		if len(cues) > 8 {
			f.fault("cues", "cues", "holds more than 8")
		}
		for _, s := range cues {
			if n := utf8Len(s); n == 0 || n > 60 || strings.ContainsAny(s, "\n\r") {
				f.fault("cues", "cues", "a cue is not 1 to 60 characters")
			}
		}
	}
	f.statement()
	if srcOK {
		f.citations(srcs, true)
	}
}

// tagFault says how an item's tag breaks section 5's rules, or "".
func tagFault(kind, tag string, srcs []source, migrated bool) string {
	if migrated {
		if !slices.Contains(migratedTags, tag) {
			return strconv.Quote(tag) + " is not a tag a migrated item may keep"
		}
		return ""
	}
	best := ""
	typedStated := false
	for _, s := range srcs {
		if best == "" || rank[s.supports] > rank[best] {
			best = s.supports
		}
		typedStated = typedStated || s.pv == "typed" && s.supports == "stated"
	}
	switch {
	case best != tag:
		return strconv.Quote(tag) + " is not the highest support of its sources, " + strconv.Quote(best)
	case !slices.Contains(kindTags[kind], tag):
		return "a " + kind + " cannot be " + strconv.Quote(tag)
	case tag == "accepted" && len(srcs) < 2:
		return "an accepted item keeps both spans"
	case (kind == "rule" || kind == "constraint") && !typedStated:
		return "a " + kind + " needs a typed span that states it"
	}
	return ""
}

func utf8Len(s string) int { return len([]rune(s)) }

func validBranch(b string) bool {
	return b != "" && len(b) <= 200 && !strings.Contains(b, "..") && !strings.ContainsAny(b, " ~^:?*[\\\t\n") &&
		!strings.HasPrefix(b, "-") && !strings.HasPrefix(b, "/") && !strings.HasSuffix(b, "/") && !strings.HasSuffix(b, ".lock")
}

func (c *checker) month(f fc, t time.Time) {
	if got := t.UTC().Format("2006/01"); got != f.d.loc.ym {
		f.fault("", "month", "the folder is "+f.d.loc.ym+", the month is "+got)
	}
}

func (c *checker) status(f fc, state string, stateOK bool) {
	s, ok := f.str("status", true)
	want := "deprecated"
	if state == "active" {
		want = "stable"
	}
	if ok && stateOK && s != want {
		f.fault("status", "status", "is not "+want+" for state "+state)
	}
}

func (c *checker) workstream(f fc) {
	l := f.d.loc
	if id, ok := f.str("id", true); ok {
		if id != l.id {
			f.fault("id", "id", "is not the file name's id")
		}
		if b, has := f.str("branch", false); has && id != workstreamID(l.pid, b) {
			f.fault("id", "id", "is not the id derived from the project and the branch")
		}
	}
	if b, ok := f.str("branch", false); ok && !validBranch(b) {
		f.fault("branch", "branch", "is not a git branch name")
	}
	f.text("title", true, 200)
	f.text("description", false, 300)
	state, stateOK := f.oneOf("state", true, wsStates)
	c.status(f, state, stateOK)
	f.datetime("state_at", true)
	c.areaProject(f)
	f.generated()
	srcs, srcOK := f.sources(1, 3, false)
	if line := f.statement(); line != "" && !reRef.MatchString(line) {
		f.fault("", "statement", "the status line cites no source")
	}
	if srcOK {
		f.citations(srcs, false)
	}
	seen := map[string]bool{}
	for _, sec := range []struct{ name, letter string }{{"Next steps", "n"}, {"Open questions", "q"}} {
		lines, _ := f.d.under(sec.name)
		for _, line := range lines {
			m := reWorkLine.FindStringSubmatch(line)
			if m == nil || !validID(m[2], sec.letter[0]) || seen[m[2]] {
				f.fault("", "working-items", "a line under "+sec.name+" is not - <text> <!-- id:"+sec.letter+"… --> with a new id")
				continue
			}
			seen[m[2]] = true
		}
	}
	lines, _ := f.d.under("Decisions in force")
	for _, line := range lines {
		m := reInForce.FindStringSubmatch(line)
		if m == nil {
			f.fault("", "decisions-in-force", "a line is not - [<title>](/<path>)")
			continue
		}
		target := m[2]
		c.later = append(c.later, func() {
			t := c.files[target]
			if c.rep.Protected[target].Unreadable && strings.HasPrefix(target, l.area+"/items/") {
				return // what it holds cannot be read, so it cannot be held against the link
			}
			if t == nil || t.loc.class != "item-file" || t.loc.area != l.area || t.get("type") == nil || t.get("type").Scalar != "decision" || t.get("state") == nil || t.get("state").Scalar != "active" {
				f.fault("", "decisions-in-force", "/"+target+" is not an active decision of the project")
			} else if by := c.supersededBy(t.loc.id); by != "" {
				f.fault("", "decisions-in-force", "/"+target+" is superseded by "+by)
			}
		})
	}
}

// supersededBy returns the id of an active item that lists id in its
// supersedes, or "": superseded is computed from successors.
func (c *checker) supersededBy(id string) string {
	paths := make([]string, 0, len(c.files))
	for p := range c.files {
		paths = append(paths, p)
	}
	slices.Sort(paths)
	for _, p := range paths {
		d := c.files[p]
		state, sup := d.get("state"), d.get("supersedes")
		if d.loc.class != "item-file" || state == nil || state.Scalar != "active" || sup == nil {
			continue
		}
		if !sup.Written {
			// A person's line, read for the id in any form.
			if strings.Contains(sup.Text, id) {
				return d.loc.id
			}
			continue
		}
		list, _ := sup.Value.([]any)
		for _, v := range list {
			if s, _ := v.(string); s == id {
				return d.loc.id
			}
		}
	}
	return ""
}

func (c *checker) session(f fc) {
	l := f.d.loc
	if host, sid, ok := f.sessionField(true); ok && (host != l.host || sessionKey(sid) != l.skey) {
		f.fault("", "session-file", "the name does not match the session "+host+":"+sid)
	}
	if w, ok := f.str("writer", true); ok && (w != l.writer || !c.writers[w]) {
		f.fault("writer", "writer", "is not the file name's writer with a record")
	}
	if t, ok := f.datetime("started", true); ok {
		c.month(f, t)
	}
	c.areaProject(f)
	if ws, ok := f.strings("workstreams", false); ok {
		for _, id := range ws {
			c.later = append(c.later, func() {
				if p := c.ids[id]; !strings.HasPrefix(p, l.area+"/workstreams/") {
					f.fault("workstreams", "workstreams", strconv.Quote(id)+" names no workstream of the project")
				}
			})
		}
	}
	f.generated()
	c.covers(f)
	srcs, srcOK := f.sources(0, 0, false)
	lines, found := f.d.under("Log")
	if !found {
		f.fault("", "log", "there is no ## Log")
	}
	for _, line := range lines {
		if !reLogLine.MatchString(line) || !reRef.MatchString(line) {
			f.fault("", "log", "a line is not - <did|found|failed|left>: <text> with a citation")
		}
	}
	if srcOK {
		f.citations(srcs, false)
	}
}

func (c *checker) covers(f fc) {
	l, ok := f.list("covers", true)
	if !ok {
		return
	}
	last := int64(0)
	for _, v := range l {
		m, _ := v.(map[string]any)
		from, to := num(m["from"]), num(m["to"])
		pass, _ := m["pass"].(string)
		outcome, _ := m["outcome"].(string)
		reason, hasReason := m["reason"].(string)
		want := 5
		if hasReason {
			want = 6
		}
		ok := len(m) == want && from > last && to >= from && rePass.MatchString(pass) && num(m["contract"]) >= 1
		switch outcome {
		case "done":
			ok = ok && !hasReason
		case "skipped":
			ok = ok && slices.Contains(skipped, reason)
		case "refused":
			ok = ok && slices.Contains(refused, reason)
		default:
			ok = false
		}
		if !ok {
			f.fault("covers", "covers", "a range is not a coverage object after the one before it")
			return
		}
		last = to
	}
}

// num reads a JSON whole number of at least 1, or 0.
func num(v any) int64 {
	n, ok := v.(interface{ String() string })
	if !ok {
		return 0
	}
	i, err := strconv.ParseInt(n.String(), 10, 64)
	if err != nil || i < 1 {
		return 0
	}
	return i
}

func (c *checker) disagreement(f fc) {
	l := f.d.loc
	if id, ok := f.str("id", true); ok {
		if id != l.id {
			f.fault("id", "id", "is not the file name's id")
		}
	}
	c.month(f, idTime(l.id))
	if it, ok := f.str("item", true); ok {
		c.later = append(c.later, func() {
			if !strings.HasPrefix(c.ids[it], l.area+"/items/") {
				f.fault("item", "item", "names no item in the same area")
			}
		})
	}
	f.oneOf("op", true, []string{"supersede", "retire"})
	f.oneOf("reason", true, reasons)
	f.datetime("at", true)
	f.sessionField(true)
	c.areaProject(f)
	f.generated()
	srcs, srcOK := f.sources(1, 3, false)
	f.statement()
	if srcOK {
		f.citations(srcs, false)
	}
}

func (c *checker) project(f fc) {
	l := f.d.loc
	if id, ok := f.str("id", true); ok {
		if id != l.pid {
			f.fault("id", "id", "is not the folder's project id")
		}
	}
	f.text("title", true, 200)
	f.text("description", false, 300)
	lists := []struct {
		key  string
		test func(string) bool
	}{
		{"remotes", reRemote.MatchString}, {"root_commits", reCommit.MatchString}, {"legacy_keys", reLegacyKey.MatchString},
	}
	for _, x := range lists {
		if vals, ok := f.strings(x.key, false); ok {
			for _, s := range vals {
				if !x.test(s) {
					f.fault(x.key, x.key, strconv.Quote(s)+" is not of the field's form")
				}
			}
		}
	}
	var refs []struct{ key, pid string }
	if s, ok := f.str("merged_into", false); ok {
		refs = append(refs, struct{ key, pid string }{"merged_into", s})
	}
	for _, key := range []string{"related", "shares_with"} {
		vals, _ := f.strings(key, false)
		for _, s := range vals {
			refs = append(refs, struct{ key, pid string }{key, s})
		}
	}
	for _, r := range refs {
		c.later = append(c.later, func() {
			if r.pid == l.pid || !c.projectExists(r.pid) {
				f.fault(r.key, r.key, strconv.Quote(r.pid)+" is not another project of the store")
			}
		})
	}
}

func (c *checker) system(f fc) {
	if s, ok := f.str("stale_after", false); ok {
		if _, timeOK := parseTime(s); !timeOK {
			f.fault("stale_after", "stale_after", "is not a datetime with seconds and an offset")
		}
	}
	for _, line := range f.d.body {
		if strings.Contains(line, "](map.md)") {
			f.fault("", "system-page", "the map link line is still there")
		}
	}
}

// crossFile runs the checks that need every file read.
func (c *checker) crossFile() {
	for _, fn := range c.later {
		fn()
	}
}
