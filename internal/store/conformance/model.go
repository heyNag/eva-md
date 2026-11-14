// Package conformance holds the store's properties, a reference model that
// keeps them, and the harness that runs a subject through seeded
// schedules. A property that cannot fail proves nothing, so each is shown
// failing on a broken variant of the model. PROPERTIES.md beside it says
// what each property is and where it comes from.
//
// Nothing in eva-md imports this package: tests do, and the store's fake
// is built on its model.
package conformance

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"maps"
	"regexp"
	"slices"
	"strings"
	"time"

	"github.com/heyNag/eva-md/internal/store"
	"github.com/heyNag/eva-md/internal/store/codec"
)

// The model's errors.
var (
	ErrBusy    = errors.New("conformance: the store is open")
	ErrLost    = errors.New("conformance: the store folder is gone")
	ErrClosed  = errors.New("conformance: the store was closed")
	ErrStopped = errors.New("conformance: a fault stopped this process's store")
	ErrTxn     = errors.New("conformance: the pass is not well formed")
	ErrNoFile  = errors.New("conformance: no such file")
	ErrRestore = errors.New("conformance: nothing to restore, or the remote is another store's")
)

// The paths the model keeps.
const (
	manifestPath = ".eva-md/store.toml"
	recordPath   = ".eva-md/hand-edits.tsv"
)

var (
	reItemID = regexp.MustCompile(`^i[0-9a-hjkmnp-tv-z]{13}$`)
	reKey    = regexp.MustCompile(`^[A-Za-z0-9_-]{1,80}$`)
)

// Model is the reference model of the store: an in-memory state machine
// for one writer on one machine. Only its durable part survives Close and
// a reopen: the store folder (its files, journal and commits, with the
// hand-edit record and coverage in the tree), the store id and job list
// kept outside it, and the remote. Its bytes need not follow the format:
// a property compares a subject only with itself.
type Model struct {
	storeID string                   // kept outside the store folder, as the state is
	jobs    map[string][]store.Range // the local job list: ranges this machine ran
	remote  *pushed                  // the last push
	folder  *folder                  // nil before the first open and after Lose
	open    bool                     // the store's lock

	// Broken variants, each set only by a test, so a property is seen to
	// fail.
	filesBeforeJournal bool // write the files, then the journal
	skipPassLookup     bool // never look a pass up among the commits
	bareLock           bool // read a lock only as the line locked: true
	refusalDropsPass   bool // one refusal refuses the whole pass
	coverageOutside    bool // keep coverage in the folder, outside the tree
	queueFromJobs      bool // read coverage from the local job list too
	sessionUnguarded   bool // write notes and coverage into a protected session file
	requiresIgnored    bool // treat every requirement as met
	faultIgnored       bool // never call the fault
	faultEarlyOnly     bool // call the fault at recover and plan alone
	sessionRewritten   bool // write a session file anew on each pass, with only that pass's notes
	handTextDropped    bool // keep only the notes the store wrote of a session file's log
}

// folder is the store folder.
type folder struct {
	files   map[string][]byte // the working tree
	journal *journal          // the pass being written, until it finishes
	commits []commit
	outside map[string][]store.Range // coverage kept outside the tree, by the broken variant
}

type journal struct {
	pass  string
	paths []string
}

type commit struct {
	Commit
	tree     map[string][]byte
	outcomes []store.Outcome
}

type pushed struct {
	storeID string
	commits []commit
}

// NewModel returns a model with no store yet: the first Open makes one.
func NewModel() *Model {
	return &Model{jobs: map[string][]store.Range{}}
}

// Open takes the store's lock and recovers: a journal's paths go back to
// the last commit, and any other change in the folder is adopted as a
// hand edit, a commit of kind human. The first open makes the store.
func (m *Model) Open(clock Clock, fault store.Fault) (store.Store, error) {
	if m.open {
		return nil, ErrBusy
	}
	if m.folder == nil {
		if m.storeID != "" {
			return nil, ErrLost
		}
		pass := passAt(clock(), make([]byte, 10))
		id, err := store.NewID('s', pass, 0)
		if err != nil {
			return nil, err
		}
		m.storeID = id
		m.folder = newFolder(id)
	}
	m.recover(m.folder, clock)
	m.open = true
	return &handle{m: m, f: m.folder, clock: clock, fault: fault}, nil
}

// newFolder is a new store: its manifest, in one commit of kind init.
func newFolder(storeID string) *folder {
	files := map[string][]byte{manifestPath: []byte("schema = 2\nmin_reader = 2\nstore_id = \"" + storeID + "\"\nepoch = 1\n")}
	f := &folder{files: files, outside: map[string][]store.Range{}}
	f.commit(Commit{Kind: store.KindInit}, maps.Clone(files), nil)
	return f
}

// head is the tree of the last commit.
func (f *folder) head() map[string][]byte {
	return f.commits[len(f.commits)-1].tree
}

// commit adds a commit whose id is a hash of its parent, its fields and
// its tree, so one history gives one id.
func (f *folder) commit(c Commit, tree map[string][]byte, outcomes []store.Outcome) string {
	h := sha256.New()
	if len(f.commits) > 0 {
		h.Write([]byte(f.commits[len(f.commits)-1].ID))
	}
	meta, _ := json.Marshal(c)
	h.Write(meta)
	for _, p := range slices.Sorted(maps.Keys(tree)) {
		sum := sha256.Sum256(tree[p])
		h.Write([]byte("\x00" + p + "\x00"))
		h.Write(sum[:])
	}
	c.ID = hex.EncodeToString(h.Sum(nil))[:40]
	f.commits = append(f.commits, commit{Commit: c, tree: tree, outcomes: outcomes})
	return c.ID
}

// find is the commit of a pass, or nil.
func (f *folder) find(pass string) *commit {
	for i := range f.commits {
		if f.commits[i].Pass == pass {
			return &f.commits[i]
		}
	}
	return nil
}

// recover undoes an unfinished pass and adopts what anything else changed.
func (m *Model) recover(f *folder, clock Clock) {
	head := f.head()
	if f.journal != nil {
		for _, p := range f.journal.paths {
			if b, ok := head[p]; ok {
				f.files[p] = b
			} else {
				delete(f.files, p)
			}
		}
		f.journal = nil
	}
	var dirty []string
	for p, b := range f.files {
		if h, ok := head[p]; !ok || !bytes.Equal(h, b) {
			dirty = append(dirty, p)
		}
	}
	for p := range head {
		if _, ok := f.files[p]; !ok {
			dirty = append(dirty, p)
		}
	}
	if len(dirty) == 0 {
		return
	}
	slices.Sort(dirty)
	record := f.files[recordPath]
	if record == nil {
		record = []byte("path\tat\tstate\n")
	}
	at := clock().UTC().Format(time.RFC3339)
	for _, p := range dirty {
		if strings.HasSuffix(p, ".md") {
			record = append(slices.Clip(record), p+"\t"+at+"\tunacknowledged\n"...)
		}
	}
	f.files[recordPath] = record
	f.commit(Commit{Kind: store.KindHuman, Revised: len(dirty)}, maps.Clone(f.files), nil)
}

// unacknowledged reports a path whose newest line in the hand-edit record
// is unacknowledged.
func unacknowledged(record []byte, path string) bool {
	state := ""
	for line := range strings.SplitSeq(string(record), "\n") {
		if f := strings.Split(line, "\t"); len(f) == 3 && f[0] == path {
			state = f[2]
		}
	}
	return state == "unacknowledged"
}

// handle is the store as one process holds it.
type handle struct {
	m       *Model
	f       *folder
	clock   Clock
	fault   store.Fault
	closed  bool
	stopped bool
}

func (h *handle) usable() error {
	switch {
	case h.closed:
		return ErrClosed
	case h.stopped:
		return ErrStopped
	case h.m.folder != h.f:
		return ErrLost
	}
	return nil
}

// step stops Apply where the fault says, as a process's death would.
func (h *handle) step(s store.Step, i int) error {
	early := s == store.StepRecover || s == store.StepPlan
	if h.fault == nil || h.m.faultIgnored || h.m.faultEarlyOnly && !early {
		return nil
	}
	if err := h.fault(s, i); err != nil {
		h.stopped = true
		return err
	}
	return nil
}

func (h *handle) Apply(txn store.Txn) (store.Result, error) {
	if err := h.usable(); err != nil {
		return store.Result{}, err
	}
	if err := checkTxn(txn); err != nil {
		return store.Result{}, err
	}
	m, f := h.m, h.f
	if err := h.step(store.StepRecover, 0); err != nil {
		return store.Result{}, err
	}
	m.recover(f, h.clock)
	if !m.skipPassLookup {
		if c := f.find(txn.Pass); c != nil {
			return store.Result{Outcomes: slices.Clone(c.outcomes), Commit: c.ID}, nil
		}
	}
	if err := h.step(store.StepPlan, 0); err != nil {
		return store.Result{}, err
	}
	p, err := m.plan(f, txn)
	if err != nil {
		return store.Result{}, err
	}
	if len(p.writes) == 0 && len(p.covers) == 0 {
		return store.Result{Outcomes: p.outcomes}, nil
	}
	paths := slices.Sorted(maps.Keys(p.writes))
	write := func() error {
		for i, path := range paths {
			if err := h.step(store.StepWrite, i); err != nil {
				return err
			}
			f.files[path] = p.writes[path]
		}
		if m.coverageOutside && len(p.covers) > 0 {
			f.outside[txn.Session] = merge(f.outside[txn.Session], p.covers)
		}
		return nil
	}
	if m.filesBeforeJournal {
		if err := write(); err != nil {
			return store.Result{}, err
		}
	}
	if err := h.step(store.StepJournal, 0); err != nil {
		return store.Result{}, err
	}
	f.journal = &journal{pass: txn.Pass, paths: paths}
	if !m.filesBeforeJournal {
		if err := write(); err != nil {
			return store.Result{}, err
		}
	}
	if err := h.step(store.StepStage, 0); err != nil {
		return store.Result{}, err
	}
	tree := maps.Clone(f.head())
	for _, path := range paths {
		tree[path] = f.files[path]
	}
	if err := h.step(store.StepCommit, 0); err != nil {
		return store.Result{}, err
	}
	c := Commit{Pass: txn.Pass, Kind: txn.Kind, Session: txn.Session, Covers: p.covers, Created: p.created, Revised: p.revised, Refused: p.refused}
	id := f.commit(c, tree, p.outcomes)
	if err := h.step(store.StepFinish, 0); err != nil {
		return store.Result{}, err
	}
	f.journal = nil
	if len(p.covers) > 0 {
		m.jobs[txn.Session] = merge(m.jobs[txn.Session], p.covers)
	}
	return store.Result{Outcomes: slices.Clone(p.outcomes), Commit: id}, nil
}

// Plan recovers and plans on a copy of the folder, so it writes nothing.
func (h *handle) Plan(txn store.Txn) ([]store.Outcome, error) {
	if err := h.usable(); err != nil {
		return nil, err
	}
	if err := checkTxn(txn); err != nil {
		return nil, err
	}
	f := &folder{files: maps.Clone(h.f.files), commits: slices.Clip(h.f.commits), outside: h.f.outside}
	if h.f.journal != nil {
		j := *h.f.journal
		f.journal = &j
	}
	h.m.recover(f, h.clock)
	if !h.m.skipPassLookup {
		if c := f.find(txn.Pass); c != nil {
			return slices.Clone(c.outcomes), nil
		}
	}
	p, err := h.m.plan(f, txn)
	if err != nil {
		return nil, err
	}
	return p.outcomes, nil
}

// Coverage reads the session file in the last commit's tree.
func (h *handle) Coverage(session string) ([]store.Range, error) {
	if err := h.usable(); err != nil {
		return nil, err
	}
	var out []store.Range
	if h.m.coverageOutside {
		out = slices.Clone(h.f.outside[session])
	} else {
		out, _, _ = parseSession(h.f.head()[sessionPath(session)])
	}
	if h.m.queueFromJobs {
		out = merge(out, h.m.jobs[session])
	}
	return out, nil
}

func (h *handle) Close() error {
	if h.closed {
		return nil
	}
	h.closed = true
	h.m.open = false
	return nil
}

// checkTxn refuses a pass that is not well formed as a whole.
func checkTxn(txn store.Txn) error {
	if _, ok := store.PassTime(txn.Pass); !ok || !slices.Contains(store.Kinds, txn.Kind) {
		return ErrTxn
	}
	if len(txn.Covers) > 0 && txn.Session == "" {
		return ErrTxn
	}
	last := 0
	for _, r := range txn.Covers {
		done := r.Outcome == "done"
		if r.From <= last || r.To < r.From || done != (r.Reason == "") || !slices.Contains([]string{"done", "skipped", "refused"}, r.Outcome) {
			return ErrTxn
		}
		last = r.To
	}
	return nil
}

// planned is what a pass would do.
type planned struct {
	outcomes                  []store.Outcome
	writes                    map[string][]byte
	covers                    []store.Range // ranges not yet covered, with the pass set
	created, revised, refused int
}

// plan works out each operation's outcome and the bytes it would write.
func (m *Model) plan(f *folder, txn store.Txn) (*planned, error) {
	p := &planned{writes: map[string][]byte{}}
	read := func(path string) ([]byte, bool) {
		if b, ok := p.writes[path]; ok {
			return b, true
		}
		b, ok := f.files[path]
		return b, ok
	}
	for i := range txn.Ops {
		o := m.planOp(txn, i, p, read)
		switch {
		case o.Result == store.ResultRefused:
			p.refused++
		case o.Result == store.ResultApplied && txn.Ops[i].Type == store.OpItemCreate:
			p.created++
		case o.Result == store.ResultApplied:
			p.revised++
		}
		p.outcomes = append(p.outcomes, o)
	}
	if m.refusalDropsPass && p.refused > 0 {
		for i, o := range p.outcomes {
			if o.Result != store.ResultRefused {
				p.outcomes[i] = store.Outcome{Result: store.ResultRefused, Reason: store.ReasonRequires, ID: o.ID}
			}
		}
		return &planned{outcomes: p.outcomes, writes: map[string][]byte{}, refused: len(p.outcomes)}, nil
	}
	if len(txn.Covers) == 0 {
		return p, nil
	}
	path := sessionPath(txn.Session)
	b, ok := read(path)
	if ok && m.sessionShut(b) != "" {
		// The store writes nothing into a session file that is locked,
		// verified or unreadable, so its session takes no coverage, and
		// the ranges stay in the queue until a person repairs the file.
		return p, nil
	}
	_, mine := p.writes[path]
	have, keep, log := m.session(b, mine)
	if m.coverageOutside {
		have = f.outside[txn.Session]
	}
	for _, r := range txn.Covers {
		r.Pass = txn.Pass
		i := slices.IndexFunc(have, func(h store.Range) bool { return h.From <= r.To && r.From <= h.To })
		switch {
		case i < 0:
			p.covers = append(p.covers, r)
		case have[i].From != r.From || have[i].To != r.To:
			return nil, ErrTxn
		}
	}
	if len(p.covers) > 0 && !m.coverageOutside {
		p.writes[path] = renderSession(txn.Session, keep, merge(have, p.covers), log)
	}
	return p, nil
}

func refused(reason, id string) store.Outcome {
	return store.Outcome{Result: store.ResultRefused, Reason: reason, ID: id}
}

// planOp is one operation's outcome; what it would write goes in p.writes.
func (m *Model) planOp(txn store.Txn, i int, p *planned, read func(string) ([]byte, bool)) store.Outcome {
	op := txn.Ops[i]
	for _, r := range op.Requires {
		if r < 0 || r >= i {
			return refused(store.ReasonInvalid, op.ID)
		}
	}
	for _, r := range op.Requires {
		if res := p.outcomes[r].Result; res != store.ResultApplied && res != store.ResultNoop && !m.requiresIgnored {
			return refused(store.ReasonRequires, op.ID)
		}
	}
	if op.Authority != store.AuthorityModel && op.Authority != store.AuthorityUser {
		return refused(store.ReasonInvalid, op.ID)
	}
	switch op.Type {
	case store.OpItemCreate:
		return m.create(txn, i, p, read)
	case store.OpItemRevise, store.OpItemState, store.OpDocAcknowledge:
		return m.change(op, p, read)
	case store.OpSessionAppend:
		text, ok := line(op.Fields["text"], 500)
		if txn.Session == "" || !ok {
			return refused(store.ReasonInvalid, op.ID)
		}
		path := sessionPath(txn.Session)
		b, ok := read(path)
		if reason := m.sessionShut(b); ok && reason != "" {
			return refused(reason, op.ID)
		}
		_, mine := p.writes[path]
		have, keep, log := m.session(b, mine)
		p.writes[path] = renderSession(txn.Session, keep, have, log+"- did: "+text+"\n")
		return store.Outcome{Result: store.ResultApplied, ID: op.ID}
	}
	return refused(store.ReasonInvalid, op.ID)
}

// kinds are the five kinds of item.
var kinds = []string{"fact", "decision", "constraint", "procedure", "rule"}

func (m *Model) create(txn store.Txn, i int, p *planned, read func(string) ([]byte, bool)) store.Outcome {
	op := txn.Ops[i]
	kind, _ := op.Fields["kind"].(string)
	title, okTitle := line(op.Fields["title"], 200)
	statement, okStatement := line(op.Fields["statement"], 500)
	if op.ID != "" || op.Base != "" || !slices.Contains(kinds, kind) || !okTitle || !okStatement {
		return refused(store.ReasonInvalid, op.ID)
	}
	id, err := store.NewID('i', txn.Pass, i)
	if err != nil {
		return refused(store.ReasonInvalid, "")
	}
	path := itemPath(id)
	if _, ok := read(path); ok {
		return refused(store.ReasonConflict, id)
	}
	p.writes[path] = renderItem(id, kind, title, "active", statement)
	return store.Outcome{Result: store.ResultApplied, ID: id}
}

// change is an operation aimed at an item that exists: a revision, a
// change of state, or an acknowledgement of its hand edit.
func (m *Model) change(op store.Op, p *planned, read func(string) ([]byte, bool)) store.Outcome {
	path := itemPath(op.ID)
	b, ok := read(path)
	if !reItemID.MatchString(op.ID) || !ok {
		return refused(store.ReasonUnknownID, op.ID)
	}
	prot := m.protection(b)
	record, _ := read(recordPath)
	unacked := unacknowledged(record, path)
	switch reason := m.shut(b); {
	case reason != "":
		return refused(reason, op.ID)
	case op.Authority != store.AuthorityUser && prot.Verified:
		return refused(store.ReasonVerified, op.ID)
	case op.Authority != store.AuthorityUser && unacked:
		return refused(store.ReasonHandEdit, op.ID)
	case op.Base != hash(b):
		return refused(store.ReasonConflict, op.ID)
	}
	var nb []byte
	switch op.Type {
	case store.OpItemRevise:
		nb = b
		title, hasTitle := op.Fields["title"]
		statement, hasStatement := op.Fields["statement"]
		if !hasTitle && !hasStatement {
			return refused(store.ReasonInvalid, op.ID)
		}
		if hasTitle {
			t, ok := line(title, 200)
			if !ok {
				return refused(store.ReasonInvalid, op.ID)
			}
			q, _ := json.Marshal(t)
			nb = setLine(nb, "title", string(q))
		}
		if hasStatement {
			s, ok := line(statement, 500)
			if !ok {
				return refused(store.ReasonInvalid, op.ID)
			}
			nb = setBody(nb, s+"\n")
		}
	case store.OpItemState:
		state, _ := op.Fields["state"].(string)
		cur := readItem(b).State
		if !slices.Contains([]string{"active", "retired", "retracted"}, state) || cur == "retracted" && state != cur {
			return refused(store.ReasonInvalid, op.ID)
		}
		nb = setLine(b, "state", state)
	case store.OpDocAcknowledge:
		if !unacked {
			return store.Outcome{Result: store.ResultNoop, ID: op.ID}
		}
		p.writes[recordPath] = append(slices.Clip(record), path+"\t-\tacknowledged\n"...)
		return store.Outcome{Result: store.ResultApplied, ID: op.ID}
	}
	if bytes.Equal(nb, b) {
		return store.Outcome{Result: store.ResultNoop, ID: op.ID}
	}
	p.writes[path] = nb
	return store.Outcome{Result: store.ResultApplied, ID: op.ID}
}

// protection reads a document's protection through the codec, fail-closed.
// The broken variant reads a lock only as the bare line locked: true.
func (m *Model) protection(b []byte) codec.Protection {
	p := codec.Read(b).Protection()
	if m.bareLock && !p.Unreadable {
		p.Locked = bytes.HasPrefix(b, []byte("locked: true\n")) || bytes.Contains(b, []byte("\nlocked: true\n"))
	}
	return p
}

// shut is the reason a file refuses every operation, whatever its
// authority: unreadable, then locked; "" when it refuses none.
func (m *Model) shut(b []byte) string {
	switch p := m.protection(b); {
	case p.Unreadable:
		return store.ReasonUnreadable
	case p.Locked:
		return store.ReasonLocked
	}
	return ""
}

// sessionShut is the reason a session file refuses a note and takes no
// coverage, whatever the authority: unreadable, locked, then verified. The
// format lets no one lock or verify a session file, and no operation can
// clear either mark there, so one that is there shuts the file until a
// person repairs it. An unacknowledged hand edit shuts nothing, as no
// operation can acknowledge a session file.
func (m *Model) sessionShut(b []byte) string {
	if m.sessionUnguarded {
		return ""
	}
	if reason := m.shut(b); reason != "" {
		return reason
	}
	if m.protection(b).Verified {
		return store.ReasonVerified
	}
	return ""
}

// line is a value that is a string of one line, 1 to max bytes.
func line(v any, max int) (string, bool) {
	s, ok := v.(string)
	return s, ok && s != "" && len(s) <= max && !strings.ContainsAny(s, "\r\n")
}

func hash(b []byte) string {
	sum := sha256.Sum256(b)
	return hex.EncodeToString(sum[:])
}

func itemPath(id string) string { return "items/" + id + ".md" }

// sessionPath is the session's file, <host>.<key>.md as the format names
// it, when the session is <host>:<key> and both are 1 to 80 of
// A-Za-z0-9_-; else x and a hash of the whole session. A safe name holds
// one dot and a hashed one none, so two sessions never share a file.
func sessionPath(session string) string {
	host, key, ok := strings.Cut(session, ":")
	if !ok || !reKey.MatchString(host) || !reKey.MatchString(key) {
		sum := sha256.Sum256([]byte(session))
		return "sessions/x" + hex.EncodeToString(sum[:8]) + ".md"
	}
	return "sessions/" + host + "." + key + ".md"
}

func renderItem(id, kind, title, state, statement string) []byte {
	q, _ := json.Marshal(title)
	return []byte("---\ntype: " + kind + "\nid: " + id + "\ntitle: " + string(q) + "\nstate: " + state + "\n---\n" + statement + "\n")
}

// split is a document's frontmatter lines, between its first two ---
// lines, and its body.
func split(b []byte) (front []string, body string) {
	lines := strings.SplitAfter(string(b), "\n")
	if len(lines) == 0 || lines[0] != "---\n" {
		return nil, string(b)
	}
	for i := 1; i < len(lines); i++ {
		if lines[i] == "---\n" {
			return lines[1:i], strings.Join(lines[i+1:], "")
		}
	}
	return lines[1:], ""
}

// setLine sets the first frontmatter line of the key.
func setLine(b []byte, key, value string) []byte {
	front, body := split(b)
	for i, l := range front {
		if strings.HasPrefix(l, key+": ") {
			front[i] = key + ": " + value + "\n"
			return []byte("---\n" + strings.Join(front, "") + "---\n" + body)
		}
	}
	return b
}

func setBody(b []byte, body string) []byte {
	front, _ := split(b)
	return []byte("---\n" + strings.Join(front, "") + "---\n" + body)
}

// readItem reads an item's fields from the first line of each key, which
// it can do even where the codec finds the document unreadable.
func readItem(b []byte) Item {
	front, body := split(b)
	var it Item
	for _, l := range front {
		k, v, _ := strings.Cut(strings.TrimSuffix(l, "\n"), ": ")
		switch {
		case k == "type" && it.Kind == "":
			it.Kind = v
		case k == "state" && it.State == "":
			it.State = v
		case k == "title" && it.Title == "":
			if json.Unmarshal([]byte(v), &it.Title) != nil {
				it.Title = v
			}
		}
	}
	it.Statement = strings.TrimSuffix(body, "\n")
	return it
}

// parseSession reads a session file's ranges, the frontmatter lines the
// store does not own, which a pass keeps as they are, and its log.
func parseSession(b []byte) ([]store.Range, []string, string) {
	front, body := split(b)
	var out []store.Range
	var keep []string
	for _, l := range front {
		if s, ok := strings.CutPrefix(l, "  - "); ok {
			var r store.Range
			if json.Unmarshal([]byte(s), &r) == nil {
				out = append(out, r)
				continue
			}
		}
		if !strings.HasPrefix(l, "session: ") && l != "covers:\n" {
			keep = append(keep, l)
		}
	}
	log, _ := strings.CutPrefix(body, "## Log\n")
	return out, keep, log
}

// session is parseSession as a pass that rewrites the file reads it; mine
// says whether the pass wrote the file already. The broken variants lose
// part of the log: sessionRewritten all of it the pass did not write, and
// handTextDropped every line but the notes the store wrote.
func (m *Model) session(b []byte, mine bool) ([]store.Range, []string, string) {
	have, keep, log := parseSession(b)
	switch {
	case m.sessionRewritten && !mine:
		log = ""
	case m.handTextDropped:
		var notes strings.Builder
		for l := range strings.SplitAfterSeq(log, "\n") {
			if strings.HasPrefix(l, "- did: ") {
				notes.WriteString(l)
			}
		}
		log = notes.String()
	}
	return have, keep, log
}

// renderSession writes a session file: its session, the lines it keeps,
// its ranges and its log.
func renderSession(session string, keep []string, covers []store.Range, log string) []byte {
	q, _ := json.Marshal(session)
	var b strings.Builder
	b.WriteString("---\nsession: " + string(q) + "\n")
	b.WriteString(strings.Join(keep, ""))
	if len(covers) > 0 {
		b.WriteString("covers:\n")
		for _, r := range covers {
			j, _ := json.Marshal(r)
			b.WriteString("  - " + string(j) + "\n")
		}
	}
	b.WriteString("---\n## Log\n" + log)
	return []byte(b.String())
}

// merge is the union of two lists of ranges, by their start, a range the
// first holds kept as it is there.
func merge(a, b []store.Range) []store.Range {
	out := slices.Clone(a)
	for _, r := range b {
		if !slices.ContainsFunc(out, func(h store.Range) bool { return h.From == r.From && h.To == r.To }) {
			out = append(out, r)
		}
	}
	slices.SortFunc(out, func(x, y store.Range) int { return x.From - y.From })
	return out
}

// Files is the store folder's working tree, path to bytes.
func (m *Model) Files() map[string][]byte {
	if m.folder == nil {
		return map[string][]byte{}
	}
	out := make(map[string][]byte, len(m.folder.files))
	for p, b := range m.folder.files {
		out[p] = bytes.Clone(b)
	}
	return out
}

// History is the store's commits, oldest first.
func (m *Model) History() []Commit {
	if m.folder == nil {
		return nil
	}
	out := make([]Commit, len(m.folder.commits))
	for i, c := range m.folder.commits {
		out[i] = c.Commit
		out[i].Covers = slices.Clone(c.Covers)
	}
	return out
}

// SessionFile is the path of the session's file, and whether it is in the
// working tree.
func (m *Model) SessionFile(session string) (string, bool) {
	if m.folder == nil {
		return "", false
	}
	path := sessionPath(session)
	_, ok := m.folder.files[path]
	return path, ok
}

// Items is every item file in the working tree, by path.
func (m *Model) Items() []Item {
	if m.folder == nil {
		return nil
	}
	var out []Item
	for _, p := range slices.Sorted(maps.Keys(m.folder.files)) {
		id, ok := strings.CutPrefix(p, "items/")
		if !ok || !strings.HasSuffix(id, ".md") {
			continue
		}
		b := m.folder.files[p]
		it := readItem(b)
		it.ID, it.Path, it.Base = strings.TrimSuffix(id, ".md"), p, hash(b)
		out = append(out, it)
	}
	return out
}

// HandEdit changes a file as a person or another tool would, outside the
// store's lock. The store adopts it when it next recovers.
func (m *Model) HandEdit(path string, e Edit) error {
	if m.folder == nil {
		return ErrLost
	}
	b, ok := m.folder.files[path]
	if !ok {
		return ErrNoFile
	}
	insert := func(l string) []byte {
		first, rest, _ := bytes.Cut(b, []byte("\n"))
		return []byte(string(first) + "\n" + l + "\n" + string(rest))
	}
	switch e.Kind {
	case EditChange:
		b = setBody(b, e.Text+"\n")
	case EditLock:
		b = insert("locked: true  # note")
	case EditVerify:
		b = insert(`verified: [{"by": "human:person", "at": "2026-01-01T00:00:00Z"}]`)
	case EditGarble:
		b = insert("<<<<<<< ours")
	default:
		return ErrNoFile
	}
	m.folder.files[path] = b
	return nil
}

// Push copies the store's commits to the remote.
func (m *Model) Push() error {
	if m.folder == nil {
		return ErrLost
	}
	m.remote = &pushed{storeID: m.storeID, commits: slices.Clip(slices.Clone(m.folder.commits))}
	return nil
}

// Lose deletes the store folder, and nothing outside it.
func (m *Model) Lose() error {
	if m.open {
		return ErrBusy
	}
	m.folder = nil
	return nil
}

// Restore brings a lost store folder back: from the remote's last push,
// or as a new store with the same store id when there was none.
func (m *Model) Restore() error {
	if m.open || m.folder != nil || m.storeID == "" {
		return ErrRestore
	}
	if m.remote == nil {
		m.folder = newFolder(m.storeID)
		return nil
	}
	if m.remote.storeID != m.storeID {
		return ErrRestore
	}
	f := &folder{commits: slices.Clip(slices.Clone(m.remote.commits)), outside: map[string][]store.Range{}}
	f.files = maps.Clone(f.head())
	m.folder = f
	return nil
}
