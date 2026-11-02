package update

import (
	"slices"
	"strings"
	"time"
	"unicode"

	"github.com/heyNag/eva-md/internal/identity"
	"github.com/heyNag/eva-md/internal/knowledgepage"
	"github.com/heyNag/eva-md/internal/threadpage"
	"github.com/heyNag/eva-md/internal/wikigit"
)

// keepKnowledge is how many knowledge changes a pass keeps; the rest are
// counted over the cap.
const keepKnowledge = 5

// maxPathBytes bounds a code path a knowledge entry names.
const maxPathBytes = 200

// KnowledgeSet is the knowledge on file a pass sees: the active pages as the
// recall index lists them, and the project and commit a new page is learned
// at. The duplicate checks compare a new statement with every row that
// applies to the project (see applies). Build sets Shown to the ids the
// input lists, the only ones a change may name.
type KnowledgeSet struct {
	Rows    []knowledgepage.Row
	Project string   // the project key new pages are learned in
	Head    string   // the branch's newest commit, which code sources cite; empty when git didn't tell
	Shown   []string // the ids the input lists, k<N>
}

// project is the project key new pages are learned in, the general area's
// when the set names none, as knowledgepage.Save keys them.
func (k KnowledgeSet) project() string {
	if k.Project == "" {
		return identity.GeneralKey
	}
	return k.Project
}

// applies reports a row that holds in the set's project: one for all
// projects, or the project's own. Another project's own row is shown
// read-only and never changed, nor compared with as a duplicate.
func (k KnowledgeSet) applies(r knowledgepage.Row) bool {
	return r.Scope == knowledgepage.ScopeAll || r.Project == k.project()
}

// KnowledgeCounts counts what Validate did with the output's knowledge
// changes, and what the save left out of their code sources.
type KnowledgeCounts struct {
	Added          int `json:"added"`
	Superseded     int `json:"superseded"`
	Dropped        int `json:"dropped"`
	Refused        int `json:"refused"`         // no text or reason, a tag the kind doesn't take, a restatement, a token nothing grounds, or a line the secret scan flags
	Stale          int `json:"stale"`           // supersedes and drops citing no user prompt as new as the entry
	Duplicates     int `json:"duplicates"`      // statements knowledge on file, or this output, holds already
	NearDuplicates int `json:"near_duplicates"` // adds that knowledge on file nearly holds
	Ignored        int `json:"ignored"`         // ids the input didn't show, second changes to an id, and every change while the out-of-order guard holds
	OverCap        int `json:"over_cap"`        // changes past the 5 a pass keeps
	ReadOnly       int `json:"read_only"`       // changes to a page a person locked or verified
	PathsDropped   int `json:"paths_dropped"`   // paths no cited entry writes, or past 3, or that the save couldn't cite
}

// promotable reports whether a statement of kind may carry tag: a decision
// or a constraint only the user's own words or agreement, a fact or a
// procedure those or a tool result. A suggestion or an inference is never
// promoted.
func promotable(kind, tag string) bool {
	switch kind {
	case knowledgepage.Decision, knowledgepage.Constraint:
		return userTag(tag)
	case knowledgepage.Fact, knowledgepage.Procedure:
		return userTag(tag) || tag == "observed"
	}
	return false
}

// knowledge checks the output's knowledge changes: supersedes and drops
// first, the first one for an id the input showed only, then the adds, at
// most 5 kept in all. A statement must rest on what the batch's entries
// settle: its refs cite them, its tag is one they support and its kind
// takes, every code-like token and number in it is grounded, and no
// statement on file that applies to the project, nor one this output adds,
// holds it or nearly holds it. A supersede or a drop needs the user's own
// words at or after the entry's time, and leaves a Timeline line on the
// thread page. While the out-of-order guard keeps the Now, nothing changes.
func (v *validator) knowledge(ops []knowledgeOp) {
	if v.u.KeepNow {
		v.report.Knowledge.Ignored += len(ops)
		return
	}
	acted := map[string]bool{}
	for _, op := range ops {
		if op.Action != knowledgepage.Add {
			v.changeKnowledge(op, acted)
		}
	}
	for _, op := range ops {
		if op.Action == knowledgepage.Add {
			v.addKnowledge(op)
		}
	}
}

// addKnowledge checks a new statement.
func (v *validator) addKnowledge(op knowledgeOp) {
	n := &v.report.Knowledge
	if len(v.kOps) == keepKnowledge {
		n.OverCap++
		return
	}
	title, text := scrub(deref(op.Title), knowledgepage.MaxTitle), scrub(deref(op.Text), knowledgepage.MaxStatement)
	c := v.cite(op.Refs)
	switch {
	case title == "" || text == "":
		n.Refused++
		return
	case !v.dated(title+"\n"+text, c.nums):
		v.report.Dates++
		return
	}
	tag := v.tag(op.Tag, c, nil, "add", text)
	switch {
	case !promotable(op.Kind, tag) || userTag(tag) && !usersWords(text, c):
		n.Refused++
	case v.kKeys[normalize(text)]:
		n.Duplicates++
	case v.nearKnowledge(text):
		n.NearDuplicates++
	case !v.groundsAll(title, text):
		n.Refused++
	default:
		scope, _ := v.scope(op.Scope, "", text, c)
		k := knowledgepage.Op{Action: knowledgepage.Add, Kind: op.Kind, Title: title, Text: text, Tag: tag,
			Scope: scope, Refs: c.refs, At: c.newest, Paths: v.knownPaths(op.Paths, c)}
		if v.flagged(k, knowledgepage.Entry{}, "") {
			n.Refused++
			return
		}
		v.kKeys[normalize(text)] = true
		v.kOn[k.Text] = true
		v.kOps = append(v.kOps, k)
		n.Added++
	}
}

// changeKnowledge checks a supersede or a drop of an entry the input showed.
// Only the user's own words at or after the entry's time change it: a drop
// needs such a cited prompt that names the entry, as a rule's drop does,
// and a supersede's words test reads only such prompts, so an older chat's
// word never changes what a newer one settled, and a newer prompt about
// something else lends it no time.
func (v *validator) changeKnowledge(op knowledgeOp, acted map[string]bool) {
	n := &v.report.Knowledge
	id := strings.TrimSpace(deref(op.ID))
	row, ok := v.kShown[id]
	if !ok || acted[id] {
		n.Ignored++
		return
	}
	acted[id] = true
	num, _ := knowledgepage.ParseID(id)
	switch {
	case len(v.kOps) == keepKnowledge:
		n.OverCap++
		return
	case row.ReadOnly || !v.b.Knowledge.applies(row):
		n.ReadOnly++
		return
	}
	c := v.cite(op.Refs)
	fresh := v.since(c, row.At)
	if op.Action == knowledgepage.Drop {
		reason := scrub(deref(op.Reason), knowledgepage.MaxReason)
		names := func(own string) bool { return mentions(own, row.Statement) }
		switch {
		case reason == "":
			n.Refused++
		case !v.dated(reason, c.nums):
			v.report.Dates++
		case !said(c, names, nil):
			n.Refused++
		case !said(fresh, names, nil):
			n.Stale++
		default:
			k := knowledgepage.Op{Action: knowledgepage.Drop, ID: num, Kind: row.Kind, Reason: reason, Tag: "stated", Refs: c.refs, At: c.newest}
			trail := v.trailText(id, row, "(dropped: "+cutRunes(reason, trailShare)+")")
			if v.flagged(k, rowEntry(row), trailLine(trail, c)) {
				n.Refused++
				return
			}
			delete(v.kKeys, normalize(row.Statement))
			delete(v.kOn, row.Statement)
			v.kOps = append(v.kOps, k)
			n.Dropped++
			v.knownTrail(trail, c)
		}
		return
	}
	title, text := scrub(deref(op.Title), knowledgepage.MaxTitle), scrub(deref(op.Text), knowledgepage.MaxStatement)
	switch {
	case text == "" || normalize(text) == normalize(row.Statement):
		n.Refused++
		return
	case !v.dated(title+"\n"+text, c.nums):
		v.report.Dates++
		return
	case len(fresh.users) == 0:
		n.Stale++
		return
	}
	tag := v.tag(op.Tag, c, nil, "add", text)
	switch {
	case !promotable(op.Kind, tag) || userTag(tag) && !usersWords(text, fresh):
		n.Refused++
	case v.kKeys[normalize(text)]:
		n.Duplicates++
	case !v.groundsAll(title, text):
		n.Refused++
	default:
		scope, narrowed := v.scope(op.Scope, row.Scope, text, fresh)
		k := knowledgepage.Op{Action: knowledgepage.Supersede, ID: num, Kind: op.Kind, Title: title, Text: text, Tag: tag,
			Scope: scope, Refs: c.refs, At: c.newest, Paths: v.knownPaths(op.Paths, c)}
		if narrowed {
			k.Project = v.b.Knowledge.project()
		}
		trail := v.trailText(id, row, "→ "+cutRunes(text, trailShare))
		if v.flagged(k, rowEntry(row), trailLine(trail, c)) {
			n.Refused++
			return
		}
		delete(v.kKeys, normalize(row.Statement))
		delete(v.kOn, row.Statement)
		v.kKeys[normalize(text)] = true
		v.kOn[text] = true
		v.kOps = append(v.kOps, k)
		n.Superseded++
		v.knownTrail(trail, c)
	}
}

// since is c with only the cited user prompts at or after at, compared as
// instants, all of them when at is zero.
func (v *validator) since(c cites, at time.Time) cites {
	f := c
	f.users, f.owns, f.own, f.newestUser = nil, nil, "", time.Time{}
	for i, n := range c.users {
		if e := v.b.Entries[n-1]; at.IsZero() || !e.At.Before(at) {
			f.users, f.owns = append(f.users, n), append(f.owns, c.owns[i])
			f.own += c.owns[i] + "\n"
			f.newestUser = later(f.newestUser, e.At)
		}
	}
	return f
}

// usersWords reports whether a statement under the user's tag, stated or
// accepted, rests on the user's words: the cited prompts' own words, never
// text pasted into them nor the proposal accepted, hold at least half of
// its distinctive words. A statement outlives the chat, so citing the
// user's question, or a closing "thanks", can't make the assistant's answer
// the user's.
func usersWords(text string, c cites) bool { return mentions(c.own, text) }

// nearKnowledge reports a statement that knowledge on file that applies to
// the project, or an add of this output, nearly holds: it holds 0.6 or more
// of the statement's content tokens, or the statement holds every code-like
// token of one that has some, each whole, and shares a word with it beside
// them.
func (v *validator) nearKnowledge(text string) bool {
	mine := map[string]bool{}
	for _, t := range codeTokens(text) {
		mine[strings.ToLower(t.text)] = true
	}
	for s := range v.kOn {
		if overlap(text, s) >= overlapShare {
			return true
		}
		codes := codeTokens(s)
		if len(codes) == 0 || !sharesWord(text, s) {
			continue
		}
		if !slices.ContainsFunc(codes, func(t codeToken) bool { return !mine[strings.ToLower(t.text)] }) {
			return true
		}
	}
	return false
}

// sharesWord reports whether a and b share a content word outside their
// code-like tokens.
func sharesWord(a, b string) bool { return shareToken(beside(a), beside(b)) }

// beside is s without its code-like tokens.
func beside(s string) string {
	for _, t := range codeTokens(s) {
		s = strings.ReplaceAll(s, t.text, " ")
	}
	return s
}

// groundsAll counts the grounding misses of texts, as ground does, and
// reports whether there were none: knowledge outlives the chat, so a
// statement the batch doesn't ground is not promoted.
func (v *validator) groundsAll(texts ...string) bool {
	before := v.report.Misses
	for _, s := range texts {
		v.ground(s)
	}
	return v.report.Misses == before
}

// scope is the scope a statement gets, and whether it narrows an entry for
// all projects: all only when one cited prompt's own words both hold the
// statement (see mentions) and name all projects (see everywhere), or the
// chat is outside a project, and otherwise the project's. A supersede of an
// entry for all projects keeps all, unless a cited prompt's own words narrow
// it (see narrows) in a chat in a project.
func (v *validator) scope(want, was, text string, c cites) (string, bool) {
	switch {
	case was == knowledgepage.ScopeAll && !v.b.Rules.Outside && said(c, func(own string) bool { return narrows(own, text) }, nil):
		return knowledgepage.ScopeProject, true
	case was == knowledgepage.ScopeAll:
		return knowledgepage.ScopeAll, false
	case want != knowledgepage.ScopeAll:
		return knowledgepage.ScopeProject, false
	case v.b.Rules.Outside || said(c, func(own string) bool { return mentions(own, text) && everywhere(own, text) }, nil):
		return knowledgepage.ScopeAll, false
	}
	return knowledgepage.ScopeProject, false
}

// thisProjectOnly are the words, read in the cited prompt's own words, that
// narrow an entry for all projects to the chat's project.
var thisProjectOnly = []string{"only this project", "only in this project", "only for this project", "just this project", "just in this project",
	"just for this project", "this project only", "only this repo", "only in this repo", "only for this repo", "just this repo", "just in this repo",
	"just for this repo", "this repo only", "only this repository", "only in this repository", "only for this repository", "just this repository",
	"this repository only"}

// narrows reports whether own, one prompt's own words, narrows a statement
// for all projects to the project: it names all projects under a negation
// of scope (see negatedBefore), as "not in all my projects", or says the
// statement holds only here, as "just this repo".
func narrows(own, text string) bool {
	words := contentTokens(text)
	if namesAllProjects(own, func(before []string) bool { return negatedBefore(before, words) }) {
		return true
	}
	said := " " + normalize(strings.ReplaceAll(own, "’", "'")) + " "
	return slices.ContainsFunc(thisProjectOnly, func(p string) bool { return strings.Contains(said, " "+p+" ") })
}

// knownPaths keeps the code paths a statement is about that a cited entry
// writes, at most 3: each trimmed, without a leading ./ or a trailing /,
// relative, with no .. part, space or control character, and at most 200
// bytes, and equal to a path or identifier a cited entry's text holds,
// trimmed the same way, or to a whole folder of one, ending at a /. The rest
// are left out and counted.
func (v *validator) knownPaths(paths []string, c cites) []string {
	var written []string
	for _, n := range c.nums {
		for _, t := range codeTokens(v.b.Entries[n-1].Text) {
			if t.class == classPath || t.class == classIdent {
				written = append(written, trimPath(t.text))
			}
		}
	}
	var out []string
	for _, p := range paths {
		p = trimPath(p)
		if len(out) == maxPaths || !relativePath(p) || slices.Contains(out, p) || !slices.ContainsFunc(written, func(w string) bool {
			return w == p || strings.HasPrefix(w, p+"/")
		}) {
			v.report.Knowledge.PathsDropped++
			continue
		}
		out = append(out, p)
	}
	return out
}

// trimPath is a path as a knowledge page cites it: trimmed, without a
// leading ./ or a trailing /.
func trimPath(p string) string {
	return strings.TrimSuffix(strings.TrimPrefix(strings.TrimSpace(p), "./"), "/")
}

// relativePath reports a path a code source may cite.
func relativePath(p string) bool {
	return p != "" && len(p) <= maxPathBytes && !strings.HasPrefix(p, "/") && !slices.Contains(strings.Split(p, "/"), "..") &&
		!strings.ContainsFunc(p, func(r rune) bool { return unicode.IsSpace(r) || unicode.IsControl(r) })
}

// flagged reports a change that the daily commit's secret scan would flag
// in a line the save writes for it (see knowledgepage.Written: the page with
// its History line, its lines in the folder's index, on the map, in the
// recall index and in log.md) or in its Timeline line on the thread page,
// trail, so a kept change never keeps a file out of a commit. on is the
// entry a supersede or a drop changes, as the recall index lists it.
func (v *validator) flagged(k knowledgepage.Op, on knowledgepage.Entry, trail string) bool {
	lines := knowledgepage.Written(k, on, knowledgepage.SaveMeta{Project: v.b.Knowledge.Project, Commit: v.b.Knowledge.Head,
		Now: func() time.Time { return v.newest }})
	lines = append(lines, trail)
	return wikigit.SecretLines(strings.Join(lines, "\n")) != nil
}

// rowEntry is the entry a row of the recall index lists, as far as the row
// tells it.
func rowEntry(r knowledgepage.Row) knowledgepage.Entry {
	id, _ := knowledgepage.ParseID(r.ID)
	return knowledgepage.Entry{ID: id, Kind: r.Kind, Title: r.Title, Statement: r.Statement, Tag: r.Tag, Scope: r.Scope, Project: r.Project,
		State: knowledgepage.Active, AsOf: r.AsOf, At: r.At, Paths: r.Paths}
}

// trailText is the text of the Timeline line a kept supersede or drop
// leaves on the thread page, "knowledge <id>: ~~<old>~~ → <new>" or
// "knowledge <id>: ~~<old>~~ (dropped: <reason>)", so no knowledge changes
// off the record.
func (v *validator) trailText(id string, row knowledgepage.Row, change string) string {
	return "knowledge " + id + ": ~~" + cutRunes(row.Statement, trailShare) + "~~ " + change
}

// trailLine is the Timeline line as the thread page writes it.
func trailLine(text string, c cites) string {
	return strings.Join(append([]string{"- change [stated]: " + threadpage.CleanLine(text, threadpage.TextCap)}, c.refs...), " ")
}

// knownTrail adds a kept change's Timeline line.
func (v *validator) knownTrail(text string, c cites) {
	v.u.Events = append(v.u.Events, threadpage.Event{Kind: "change", Text: text, Tag: "stated", Refs: c.refs})
	v.at, v.linesAt = later(v.at, c.newest), later(v.linesAt, c.newest)
}

// settle moves the changes the save passed over from what Validate kept to
// what became of them: one on a page a person locked or verified is
// read-only, one the pages hold already a duplicate, and any other refused.
// It adds the paths the save couldn't cite to those left out.
func (k *KnowledgeCounts) settle(ops []knowledgepage.Op, r knowledgepage.Result) {
	k.PathsDropped += r.PathsDropped
	for _, s := range r.Skipped {
		if s.Op < 0 || s.Op >= len(ops) {
			continue
		}
		switch ops[s.Op].Action {
		case knowledgepage.Add:
			k.Added--
		case knowledgepage.Supersede:
			k.Superseded--
		case knowledgepage.Drop:
			k.Dropped--
		}
		switch s.Reason {
		case knowledgepage.ReasonLocked, knowledgepage.ReasonVerified:
			k.ReadOnly++
		case knowledgepage.ReasonDuplicate, knowledgepage.ReasonSame, knowledgepage.ReasonRetired:
			k.Duplicates++
		default:
			k.Refused++
		}
	}
}

// withoutTrails removes the Timeline lines of the supersedes and drops the
// save passed over, so the thread page records no change that wasn't made.
func withoutTrails(events []threadpage.Event, skipped []knowledgepage.Skip) []threadpage.Event {
	return slices.DeleteFunc(events, func(e threadpage.Event) bool {
		return e.Kind == "change" && slices.ContainsFunc(skipped, func(s knowledgepage.Skip) bool {
			return s.ID != 0 && strings.HasPrefix(e.Text, "knowledge "+knowledgepage.Name(s.ID)+": ~~")
		})
	})
}
