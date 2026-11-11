// Package shapes is eva-md audit shapes: the shapes of the hosts' own
// records of the chats eva-md captured, and of eva-md's records of them,
// as figures for the facts of the host fact sheet marked "by the field
// report" (docs/v2/host-facts.md). Which row types the hosts write, which
// fields a row has, in what order and with what gaps rows come, how
// sessions are named: in counts, buckets, days, versions of three numbers
// and names from the fixed lists in lists.go only. A name on no list is
// counted under other, with how many distinct ones there were and in how
// many chats. No value, text, id, path, folder or name found in a record is
// printed; ids and turn ids are kept as hashes while the report is made, to
// count a chat once and to find the inbox's turn ids on the host's rows.
//
// It reads eva-md's inbox through the ledger's v1 reader, its session
// markers and recovery records, and the hosts' session files the capture
// audit lists (audit.ListStore), of sessions the inbox holds only, each
// admitted by recovery's path rules (catchup.Admits) and opened by
// recovery's opener (catchup.OpenUnregistered); on Claude Code also a
// session's subagents' files, in the folder beside its file, reached
// through no link. A session said off the record at any time, by eva-md's
// records or the host's, or in a folder the hooks leave out, is not opened
// when eva-md's records show it, and when only the host's record does it is
// read and nothing of it is kept: its chat is counted, and nothing else;
// its subagents' files are not listed, and no id its rows name joins it to
// another chat.
// It writes nothing, runs no command and makes no network or model call.
package shapes

import (
	"context"
	"errors"
	"io/fs"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/heyNag/eva-md/internal/catchup"
	"github.com/heyNag/eva-md/internal/config"
	"github.com/heyNag/eva-md/internal/host/adapter"
	"github.com/heyNag/eva-md/internal/ledger/audit"
	"github.com/heyNag/eva-md/internal/ledger/event"
	"github.com/heyNag/eva-md/internal/ledger/inboxv1"
	"github.com/heyNag/eva-md/internal/paths"
	"github.com/heyNag/eva-md/internal/session"
)

// Options says where to read.
type Options struct {
	Dirs    paths.Dirs
	Home    string        // the home whose hosts' folders hold their records
	TempDir string        // the temporary folder chats there are left out in
	Config  config.Config // for the folders left out by design
	Version string        // eva-md's version
	Now     time.Time

	rowLimit int // the longest host row decoded, audit.RowLimit when 0; tests lower it
}

// open opens a host's file under recovery's rules. Tests replace it to see
// what is opened.
var open = catchup.OpenUnregistered

// known is a session the inbox holds, with what eva-md's records show of
// it: said off the record, in a folder left out, or not read whole, or
// time ran out before its records were read; its folders, its inbox's
// figures and its turn ids.
type known struct {
	host, id                string
	off, away, unknown, cut bool
	cwds                    []string
	t                       *tally
	tids                    map[uint64]bool
}

// A file's status, as the report counts files.
const (
	statusRead     = "read"
	statusOff      = "off_the_record"
	statusLeftOut  = "left_out"
	statusNotWhole = "not_read_whole"
	statusNoFolder = "no_folder"
	statusRefused  = "refused"
	statusGone     = "gone"
	statusUnread   = "unreadable"
	statusNotYet   = "not_reached"
	// A Codex file recovery's opener refused because its first 64 KiB
	// hold no first record that names the session and its folder.
	statusNotIdentified = "not_identified"
)

// identityRefusal is the error recovery's opener gives for a Codex file
// whose first record, in the file's first 64 KiB, does not name the
// session and its folder. catchup exports no sentinel for it; a test
// plants such a file, so a change of the message is noticed.
const identityRefusal = "catchup: transcript identity mismatch"

// file is one host file, read or not: its host, the node its name gives,
// whether it is a subagent's file, why it was not read, and what reading
// it found; while the report is made, its path and its session.
type file struct {
	host, name string
	subagent   bool
	status     string
	f          *fileRead
	path       string
	k          *known
}

// Run makes the report. It returns an error only when the inbox cannot be
// listed; a part cut short by ctx says so.
func Run(ctx context.Context, o Options) (Report, error) {
	r := Report{Format: Format, Version: o.Version, Day: o.Now.Format(time.DateOnly), Parts: map[string]string{},
		Days: map[string][2]string{}, Settled: map[string][]string{"in_full": settledInFull, "in_part": settledInPart, "not_by_shapes": notByShapes}}
	away := audit.Outside(audit.Options{Dirs: o.Dirs, Home: o.Home, TempDir: o.TempDir, Config: o.Config})
	sessions, part, err := readInbox(ctx, o, away)
	if err != nil && ctx.Err() == nil {
		return Report{}, err
	}
	if err != nil {
		// Time ran out before the inbox was listed: nothing is known to
		// eva-md, so no host file is read.
		merge(&r, map[string]int{}, newGroups(), nil, nil)
		for p := range r.Parts {
			r.Parts[p] = PartIncomplete
		}
		r.Parts["inbox"] = PartIncomplete
		return r, nil
	}
	r.Parts["inbox"] = part
	counts := map[string]int{}
	g := newGroups()
	for id, k := range sessions {
		g.find(id)
		switch {
		case k.cut, k.unknown:
			g.mark(id, notWhole)
		case k.off:
			g.mark(id, offRecord)
		case k.away:
			g.mark(id, leftOut)
		}
	}
	var files []file
	listing := audit.ListStore(o.Home)
	for _, h := range adapter.All() {
		host := string(h.ID)
		for _, f := range listing[host] {
			if !f.Regular {
				counts[host+"/files/"+statusRefused]++
				continue
			}
			counts[host+"/files/listed"]++
			k := sessions[node(host, f.ID)]
			if k == nil {
				counts[facts024+"/"+host+"/no_eva_md_file"]++
				counts[host+"/files/not_known"]++
				continue
			}
			files = append(files, readFile(ctx, o, away, k, f.ID, f.Path, false))
		}
	}
	// Each file's own rows put its chat out before anything its rows name
	// joins it to another: here chats are joined by their files' names
	// only, as a session's copies share its id.
	for _, f := range files {
		g.flagFile(f)
	}
	// On Claude Code, the subagents' files of a session that nothing has
	// put out, each joined to the session by name. Of a session put out,
	// its subagents' folder is not listed.
	for _, f := range slices.Clone(files) {
		if f.host != string(adapter.ClaudeCode) || f.status != statusRead || g.flag[g.find(f.name)] != 0 {
			continue
		}
		for _, sub := range subagentFiles(f.path) {
			s := readFile(ctx, o, away, f.k, strings.TrimSuffix(filepath.Base(sub), ".jsonl"), sub, true)
			g.union(s.name, f.name)
			g.flagFile(s)
			files = append(files, s)
		}
	}
	g.joinNamed(files)
	merge(&r, counts, g, sessions, files)
	if ctx.Err() != nil {
		// Time ran out on the way: what was counted is a floor.
		for p := range r.Parts {
			r.Parts[p] = PartIncomplete
		}
	}
	return r, nil
}

// readInbox reads the inbox's sessions, by node, and what eva-md's records
// show of each. part is the inbox's part's state.
func readInbox(ctx context.Context, o Options, away func(string) bool) (map[string]*known, string, error) {
	reader := inboxv1.Backend{Root: o.Dirs.Inbox()}
	keys, err := reader.Sessions(ctx)
	if err != nil {
		return nil, "", err
	}
	part := PartNone
	sessions := map[string]*known{}
	for _, key := range keys {
		if _, ok := adapter.ByID(key.Host); !ok {
			continue
		}
		id := node(key.Host, key.Session)
		k := sessions[id]
		if k == nil {
			k = &known{host: key.Host, id: key.Session, t: newTally(), tids: map[uint64]bool{}}
			sessions[id] = k
			k.records(ctx, o)
		}
		s, events, err := reader.Read(ctx, key)
		if ctx.Err() != nil {
			// Time ran out: the session's files are not reached.
			k.cut = true
			part = PartIncomplete
			continue
		}
		if err != nil {
			k.unknown = true
			part = PartIncomplete
			continue
		}
		if part == PartNone {
			part = PartCounted
		}
		k.inbox(o, s, events)
	}
	for _, k := range sessions {
		for _, d := range k.cwds {
			if away(d) {
				k.away = true
			}
		}
	}
	return sessions, part, nil
}

// records reads the session's markers and recovery record: a marker that
// says it is or was off the record, or a record blocked for that, puts it
// off the record, and one that cannot be read leaves it not read whole.
func (k *known) records(ctx context.Context, o Options) {
	t, host := k.t, k.host
	t.add(facts013, host, "inbox_ids", idForm(k.id))
	if strings.Contains(k.id, ".") {
		t.add(facts013, host, "inbox_ids", "with_dot")
	}
	markers := session.Markers{Root: o.Dirs.Sessions()}
	for _, name := range []string{session.OffRecord, session.WasOff} {
		_, set, err := markers.GetContext(ctx, host, k.id, name)
		k.unknown = k.unknown || err != nil
		k.off = k.off || set
	}
	s, found, err := catchup.Record(ctx, o.Dirs, host, k.id)
	switch {
	case err != nil:
		k.unknown = true
	case !found:
		t.add(facts005, host, "no_record")
	default:
		t.add(facts005, host, "records")
		k.off = k.off || s.Blocked && s.Reason == "off_record"
		if filepath.IsAbs(s.Cwd) {
			k.cwds = append(k.cwds, s.Cwd)
		}
		switch {
		case !s.Missing:
			t.add(facts005, host, "path_named")
		case s.MissingWhy == catchup.MissingNoneNamed:
			t.add(facts005, host, "missing", "none_named")
		case s.MissingWhy == catchup.MissingRefused:
			t.add(facts005, host, "missing", "refused")
		case s.MissingWhy == catchup.MissingNotWritten:
			t.add(facts005, host, "missing", "not_written")
		default:
			t.add(facts005, host, "missing", "why_not_kept")
		}
	}
}

// inbox counts one inbox file of the session: a range said off the record
// puts it off the record; its folders, turn ids, replies, gaps, voice
// hand-overs and class are kept.
func (k *known) inbox(o Options, s event.Session, events []event.Event) {
	t, host := k.t, k.host
	if s.Class == event.ClassUnrun {
		t.chat(facts138, host, "end_with_no_start")
	}
	turns := map[int]map[string]bool{}
	for _, e := range events {
		if e.Kind == event.KindControl || e.Has(event.MarkWithheld) && e.Why == event.WhyOffRecord {
			k.off = true
		}
		if e.Ctx != nil && e.Ctx.Cwd != "" {
			cwd := e.Ctx.Cwd
			if !filepath.IsAbs(cwd) {
				cwd = filepath.Join(o.Home, cwd)
			}
			k.cwds = append(k.cwds, cwd)
		}
		if e.Has(event.MarkWithheld) {
			continue
		}
		if e.Turn > 0 && e.Agent == "" {
			if turns[e.Turn] == nil {
				turns[e.Turn] = map[string]bool{}
			}
			if e.TID != "" {
				turns[e.Turn][e.TID] = true
			}
		}
		if e.TID != "" && e.Agent == "" && len(k.tids) < maxNames {
			k.tids[hash(e.TID)] = true
		}
		switch {
		case e.Kind == event.KindReply && e.Has(event.MarkRecovered):
			t.add(facts004, host, "replies_recovered")
		case e.Kind == event.KindReply && e.Src.Hook:
			t.add(facts004, host, "replies_by_hook")
		case e.Kind == event.KindGap && e.Text == "gap: missing final reply":
			t.add(facts004, host, "missing_reply_gaps")
		case e.Kind == event.KindPrompt && e.Has(event.MarkWrapper):
			t.add(facts141, host, "inbox", "wrapper_prompts")
		}
	}
	for _, ids := range turns {
		switch len(ids) {
		case 0:
			t.add(facts001, host, "inbox_turns", "no_id")
		case 1:
			t.add(facts001, host, "inbox_turns", "one_id")
		default:
			t.add(facts001, host, "inbox_turns", "more_ids")
		}
	}
}

// readFile reads one file of the session k, the file at path named for id,
// under recovery's rules, unless eva-md's records leave the session out or
// time ran out before it.
func readFile(ctx context.Context, o Options, away func(string) bool, k *known, id, path string, subagent bool) file {
	f := file{host: k.host, name: node(k.host, id), subagent: subagent, path: path, k: k}
	switch {
	case k.cut:
		f.status = statusNotYet
	case k.unknown:
		f.status = statusNotWhole
	case k.off:
		f.status = statusOff
	case k.away:
		f.status = statusLeftOut
	case ctx.Err() != nil:
		f.status = statusNotYet
	}
	if f.status != "" {
		return f
	}
	cwd := ""
	if len(k.cwds) > 0 {
		cwd = k.cwds[0]
	}
	src := catchup.Source{Host: k.host, Home: o.Home, Session: id, Path: path, Cwd: cwd}
	if cwd == "" {
		f.status = statusNoFolder
		return f
	}
	if !catchup.Admits(src) {
		f.status = statusRefused
		return f
	}
	h, err := open(src)
	switch {
	case errors.Is(err, fs.ErrNotExist):
		f.status = statusGone
		return f
	case err != nil && k.host == string(adapter.Codex) && err.Error() == identityRefusal:
		// The opener found no first record in the file's first 64 KiB that
		// names the session and its folder: one longer than that, or one of
		// another session or folder.
		f.status = statusNotIdentified
		return f
	case err != nil:
		f.status = statusRefused
		return f
	}
	defer h.Close()
	var written time.Time
	if info, err := h.Stat(); err == nil {
		written = info.ModTime()
	}
	limit := o.rowLimit
	if limit <= 0 {
		limit = audit.RowLimit
	}
	got, err := readRows(ctx, src, away, h, limit, written, o.Now)
	switch {
	case ctx.Err() != nil:
		f.status = statusNotYet
	case err != nil:
		f.status = statusUnread
	default:
		f.status, f.f = statusRead, &got
		// The ids its rows name are kept as nodes, hashed, from here on.
		for _, named := range got.ids {
			got.nodes = append(got.nodes, node(k.host, named))
		}
		if subagent {
			got.t.add(facts027, k.host, "subagent_files", "read")
			if slices.Contains(got.ids, k.id) {
				got.t.add(facts027, k.host, "subagent_files", "of_another_chat")
			}
			got.ids = nil
		} else {
			got.t.add(facts013, k.host, "file_ids", idForm(id))
			if strings.Contains(id, ".") {
				got.t.add(facts013, k.host, "file_ids", "with_dot")
			}
			got.ids = nil
		}
	}
	return f
}

// subagentFiles are the session files of the subagents of the Claude Code
// session whose file is at path: <id>/subagents/<name>.jsonl beside it,
// regular files reached through no link. Nothing is opened but the folder.
func subagentFiles(path string) []string {
	dir := filepath.Join(strings.TrimSuffix(path, ".jsonl"), "subagents")
	root, err := paths.OpenRoot(dir, false)
	if err != nil {
		return nil
	}
	defer root.Close()
	d, err := root.Open(".")
	if err != nil {
		return nil
	}
	defer d.Close()
	entries, err := d.ReadDir(-1)
	if err != nil {
		return nil
	}
	var out []string
	for _, e := range entries {
		name := e.Name()
		if e.Type().IsRegular() && strings.HasSuffix(name, ".jsonl") && len(name) > len(".jsonl") && !strings.HasPrefix(name, ".") {
			out = append(out, filepath.Join(dir, name))
		}
	}
	slices.Sort(out)
	return out
}

// node names a session, or a file's session, by host and id.
func node(host, id string) string { return host + "\x00" + strconv.FormatUint(hash(id), 16) }

// idForm is the form of a session id: a UUID's, or another.
func idForm(id string) string {
	if len(id) != 36 {
		return "other_form"
	}
	for i, c := range id {
		dash := i == 8 || i == 13 || i == 18 || i == 23
		if dash != (c == '-') || !dash && !strings.ContainsRune("0123456789abcdef", c) {
			return "other_form"
		}
	}
	return "uuid_form"
}

// The reasons a chat is left out, strongest first.
const (
	offRecord = 1 << iota
	leftOut
	notWhole
)

// groups joins the nodes of one chat: a session's files, its copies, its
// forks and its subagents' files, by the ids their names and rows give.
type groups struct {
	parent map[string]string
	flag   map[string]int
}

func newGroups() *groups { return &groups{parent: map[string]string{}, flag: map[string]int{}} }

func (g *groups) find(x string) string {
	if _, ok := g.parent[x]; !ok {
		g.parent[x] = x
	}
	for g.parent[x] != x {
		g.parent[x] = g.parent[g.parent[x]]
		x = g.parent[x]
	}
	return x
}

func (g *groups) union(a, b string) {
	ra, rb := g.find(a), g.find(b)
	if ra != rb {
		g.parent[ra] = rb
		g.flag[rb] |= g.flag[ra]
	}
}

func (g *groups) mark(x string, flag int) {
	r := g.find(x)
	g.flag[r] |= flag
}

// reason is the strongest reason a flag gives to leave a chat out, as a
// file's status, or "" for none.
func reason(flag int) string {
	switch {
	case flag&offRecord != 0:
		return statusOff
	case flag&leftOut != 0:
		return statusLeftOut
	case flag&notWhole != 0:
		return statusNotWhole
	}
	return ""
}

// flagFile puts the chat of a file out when the file was not read, or its
// own rows say it was off the record, ran in a folder left out, or could
// not be told.
func (g *groups) flagFile(f file) {
	g.find(f.name)
	switch {
	case f.f == nil:
		if f.status != statusOff && f.status != statusLeftOut {
			g.mark(f.name, notWhole)
		}
	case f.f.off:
		g.mark(f.name, offRecord)
	case f.f.away:
		g.mark(f.name, leftOut)
	case f.f.unknown:
		g.mark(f.name, notWhole)
	}
}

// joinNamed joins each file to the chats its rows name, once every file
// has put its chat out or not. A file whose chat is out by then, by any of
// its files or by eva-md's records, joins nothing by what its rows name, so
// that nothing inside a session left out reaches another chat's figures.
func (g *groups) joinNamed(files []file) {
	out := make([]bool, len(files))
	for i, f := range files {
		out[i] = g.flag[g.find(f.name)] != 0
	}
	for i, f := range files {
		if out[i] || f.f == nil {
			continue
		}
		for _, n := range f.f.nodes {
			g.union(f.name, n)
		}
	}
}

// merge adds the figures of the chats left in to counts, then fills the
// report. Each file is already joined to its chat.
func merge(r *Report, counts map[string]int, g *groups, sessions map[string]*known, files []file) {
	type chat struct {
		host     string
		files    []*fileRead
		sessions []*known
	}
	chats := map[string]*chat{}
	member := func(name, host string) *chat {
		root := g.find(name)
		c := chats[root]
		if c == nil {
			c = &chat{host: host}
			chats[root] = c
		}
		return c
	}
	for _, f := range files {
		c := member(f.name, f.host)
		status := f.status
		if why := reason(g.flag[g.find(f.name)]); why != "" && status == statusRead {
			status = why
		}
		if f.subagent {
			counts[f.host+"/files/subagent_files"]++
		}
		counts[f.host+"/files/"+status]++
		if status == statusRead {
			c.files = append(c.files, f.f)
		}
	}
	for id, k := range sessions {
		c := member(id, k.host)
		c.sessions = append(c.sessions, k)
	}
	others := map[string]map[uint64]bool{}
	first, last := map[string]time.Time{}, map[string]time.Time{}
	for root, c := range chats {
		if why := reason(g.flag[root]); why != "" {
			counts[c.host+"/chats/"+why]++
			continue
		}
		if len(c.files) == 0 && len(c.sessions) == 0 {
			continue
		}
		present := map[string]bool{}
		turns := map[uint64]bool{}
		var all []*tally
		for _, f := range c.files {
			all = append(all, f.t)
			for h := range f.t.turns {
				turns[h] = true
			}
			if !f.first.IsZero() && (first[c.host].IsZero() || f.first.Before(first[c.host])) {
				first[c.host] = f.first
			}
			if f.last.After(last[c.host]) {
				last[c.host] = f.last
			}
		}
		for _, k := range c.sessions {
			all = append(all, k.t)
		}
		for _, t := range all {
			for p, n := range t.n {
				counts[p] += n
			}
			for p := range t.present {
				present[p] = true
			}
			for p, set := range t.others {
				if others[p] == nil {
					others[p] = map[uint64]bool{}
				}
				for h := range set {
					others[p][h] = true
				}
			}
		}
		for p := range present {
			counts[p]++
		}
		if present[facts138+"/"+c.host+"/end_with_no_start"] && present[facts138+"/"+c.host+"/with_a_turn"] {
			counts[facts138+"/"+c.host+"/end_with_no_start_and_a_turn"]++
		}
		if len(c.files) > 0 {
			counts[c.host+"/chats/read"]++
			for _, b := range fileBuckets {
				if b.upTo < 0 || len(c.files) <= b.upTo {
					counts[c.host+"/chats/files_per_chat/"+b.name]++
					break
				}
			}
			for _, k := range c.sessions {
				for h := range k.tids {
					if turns[h] {
						counts[facts001+"/"+c.host+"/inbox_ids/on_rows"]++
					} else {
						counts[facts001+"/"+c.host+"/inbox_ids/not_on_rows"]++
					}
				}
			}
		} else {
			counts[c.host+"/chats/inbox_only"]++
		}
	}
	for p, set := range others {
		counts[p+"/other_names"] = len(set)
	}
	hosts, facts := map[string]int{}, map[string]int{}
	for p, n := range counts {
		if strings.HasPrefix(p, "F-") {
			facts[p] = n
		} else {
			hosts[p] = n
		}
	}
	r.Hosts, r.Facts = tree(hosts), tree(facts)
	for _, f := range append(append([]string{}, settledInFull...), settledInPart...) {
		if _, ok := r.Facts[f]; !ok {
			r.Facts[f] = map[string]any{}
		}
	}
	for _, h := range adapter.All() {
		host := string(h.ID)
		if !first[host].IsZero() {
			r.Days[host] = [2]string{first[host].UTC().Format(time.DateOnly), last[host].UTC().Format(time.DateOnly)}
		}
		switch {
		case counts[host+"/files/"+statusNotYet] > 0 || counts[host+"/files/"+statusUnread] > 0:
			r.Parts[host] = PartIncomplete
		case counts[host+"/files/listed"] > 0:
			r.Parts[host] = PartCounted
		default:
			r.Parts[host] = PartNone
		}
	}
}
