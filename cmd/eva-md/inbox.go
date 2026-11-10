package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"os/signal"
	"slices"
	"sort"
	"strings"
	"syscall"
	"time"
	"unicode"

	"github.com/heyNag/eva-md/internal/catchup"
	"github.com/heyNag/eva-md/internal/config"
	"github.com/heyNag/eva-md/internal/inbox"
	"github.com/heyNag/eva-md/internal/lint"
	"github.com/heyNag/eva-md/internal/paths"
	"github.com/heyNag/eva-md/internal/searchindex"
	"github.com/heyNag/eva-md/internal/session"
)

// How long eva-md inbox clear waits for a project's search index that
// another writer holds, as a search's top-up holds one for well under a
// second, and for a session a hook is writing, which a hook holds for about
// 1.5 s at most.
const (
	inboxIndexWait = 5 * time.Second
	inboxLockWait  = 10 * time.Second
)

// inboxListed is how many captures the list names before it counts the
// rest.
const inboxListed = 20

// errInboxBusy is eva-md inbox clear's error while the background update
// runs, or another writer holds the search index of one of the projects
// past inboxIndexWait: it removed nothing.
var errInboxBusy = errors.New("a background update is running, or the search index of these chats' projects is being built, so nothing was removed; try again shortly")

func init() {
	register(command{name: "inbox", file: "inbox.go", place: 130,
		synopsis: []string{"eva-md inbox clear [--session ID ...] [--project KEY] [--before YYYY-MM-DD] [--apply]"},
		run: func(ctx context.Context, args []string, _ io.Reader, out, errOut io.Writer) int {
			return runInbox(ctx, args, out, errOut)
		}})
}

// runInbox runs eva-md inbox clear: it removes captured sessions from the
// inbox, chosen by session, by project or by the day they were last
// written, after a preview, as decision 4 asks. Without --apply it lists
// what it would remove, and how many refs in the wiki's thread pages name
// those chats, and changes nothing. With --apply it takes each session in
// turn under its lock and, unless the chat is off the record, removes its
// transcript recovery record, then records it as removed, for the daily
// lint, and removes its session files and watermarks; last it drops the
// search index of their projects, which eva-md index or the daily
// maintenance builds again. It never touches the wiki or its history. A
// filter given an empty value is a usage error.
func runInbox(ctx context.Context, args []string, out, errOut io.Writer) int {
	return inboxClear(ctx, args, out, errOut, time.Local)
}

// inboxClear is runInbox with the zone the day --before names is read in,
// the local one but in a test.
func inboxClear(ctx context.Context, args []string, out, errOut io.Writer, zone *time.Location) int {
	if len(args) < 2 || args[1] != "clear" {
		return usage(errOut, "inbox takes clear: eva-md inbox clear --session ID | --project KEY | --before YYYY-MM-DD [--apply]")
	}
	var sel inboxSelection
	f := flag.NewFlagSet("inbox clear", flag.ContinueOnError)
	f.SetOutput(io.Discard)
	f.Func("session", "", func(v string) error {
		id, err := inboxSessionArg(v)
		if err == nil {
			sel.sessions = append(sel.sessions, id)
		}
		return err
	})
	project := f.String("project", "", "")
	before := f.String("before", "", "")
	apply := f.Bool("apply", false, "")
	if err := f.Parse(args[2:]); err != nil || f.NArg() != 0 {
		return usage(errOut, "invalid arguments for inbox clear; --session takes a session id or a ref such as s:3f2a91c0")
	}
	// A filter given an empty value, as a script's unset variable gives it,
	// is refused, never taken for one not given, which would widen what is
	// removed; and hasBefore, not the day's time, says a day was given,
	// since 0001-01-01 in UTC is the zero time.
	given := map[string]bool{}
	f.Visit(func(fl *flag.Flag) { given[fl.Name] = true })
	if given["project"] {
		if *project == "" || strings.ContainsAny(*project, `/\`) || strings.HasPrefix(*project, ".") || len(*project) > 512 {
			return usage(errOut, "--project takes a project key, the name of its folder in the inbox")
		}
		sel.project = *project
	}
	if given["before"] {
		day, err := time.ParseInLocation(time.DateOnly, *before, zone)
		if err != nil {
			return usage(errOut, "--before must be a date, YYYY-MM-DD")
		}
		sel.before, sel.hasBefore = day, true
	}
	if len(sel.sessions) == 0 && sel.project == "" && !sel.hasBefore {
		return usage(errOut, "inbox clear needs --session, --project or --before to choose what to remove")
	}
	dirs, err := paths.Default()
	if err != nil {
		fmt.Fprintln(errOut, "eva-md: home folder is unavailable")
		return 1
	}
	ctx, stop := signal.NotifyContext(ctx, os.Interrupt, syscall.SIGTERM)
	defer stop()
	store := inbox.Store{Root: dirs.Inbox()}
	all, unread, err := store.Captures()
	if err != nil {
		fmt.Fprintln(errOut, "eva-md:", err)
		return 1
	}
	chosen, err := sel.pick(all)
	if err != nil {
		fmt.Fprintln(errOut, "eva-md:", err)
		return 1
	}
	if n := len(unread); n > 0 {
		fmt.Fprintf(out, "%d %s of the inbox couldn't be read in full, so some of %s chats may be left out: %s\n", n, plural(n, "project folder", "project folders"), plural(n, "its", "their"), strings.Join(unread, ", "))
	}
	if len(chosen) == 0 {
		fmt.Fprintln(out, "Nothing to remove: no captured session matches.")
		return 0
	}
	if !*apply {
		fmt.Fprintln(out, "Preview; nothing was changed.")
		inboxReport(ctx, out, dirs, chosen, false)
		fmt.Fprintln(out, "A chat still open is captured again from its next turn.")
		fmt.Fprintln(out, "Add --apply to remove "+plural(len(chosen), "it.", "them."))
		return 0
	}
	done, err := inboxRemove(ctx, dirs, chosen, time.Now(), inboxIndexWait, inboxLockWait)
	if len(done) > 0 {
		inboxReport(ctx, out, dirs, done, true)
	}
	if err != nil {
		fmt.Fprintln(errOut, "eva-md:", err)
		return 1
	}
	return 0
}

// inboxReport says what eva-md inbox clear removed, or would remove: the
// captures, what goes with them, the refs in the wiki that name them, and
// what it keeps.
func inboxReport(ctx context.Context, out io.Writer, dirs paths.Dirs, captures []inbox.Capture, done bool) {
	one := len(captures) == 1
	pick := func(single, many string) string {
		if one {
			return single
		}
		return many
	}
	verb, goes, does := "Would remove", pick("goes", "go"), "does"
	if done {
		verb, goes, does = "Removed", "went", "did"
	}
	inboxList(out, verb, captures)
	projects := map[string]bool{}
	for _, c := range captures {
		projects[c.Project] = true
	}
	of := pick("its project", "their project")
	if len(projects) > 1 {
		of = "their projects"
	}
	// A chat off the record keeps its transcript recovery record, which may
	// be all that keeps it off the record (see catchup.Forget).
	fmt.Fprintf(out, "%s %s too, %s, and so %s the search index of %s, until eva-md index or the daily maintenance builds it again.\n",
		pick("Its watermark", "Their watermarks"), goes, pick("and its transcript recovery record unless the chat is off the record", "and their transcript recovery records except those of chats off the record"), does, of)
	if line := inboxCited(ctx, dirs, captures, pick("this chat", "these chats")); line != "" {
		fmt.Fprintln(out, line)
	}
	fmt.Fprintf(out, "The wiki keeps what updates wrote from %s, and so does its history; eva-md never rewrites either.\n", pick("this chat", "these chats"))
}

// inboxSessionArg reads a --session value: a session id, or a ref to one,
// s:<id> with or without #<line>, whose id may be the first 8 characters a
// short ref keeps.
func inboxSessionArg(v string) (string, error) {
	id, _, _ := strings.Cut(strings.TrimPrefix(v, "s:"), "#")
	if id == "" || len(id) > 256 || strings.ContainsAny(id, `/\`) || strings.ContainsFunc(id, func(r rune) bool { return unicode.IsControl(r) || unicode.IsSpace(r) || unicode.In(r, unicode.Cf) }) {
		return "", errors.New("invalid session")
	}
	return id, nil
}

// inboxSelection is what eva-md inbox clear removes: the captures of the
// sessions named, those of one project folder, those last written before a
// day, or those that match all of what was given.
type inboxSelection struct {
	sessions  []string  // ids, or the first 8 characters a short ref keeps
	project   string    // a project key, the name of its inbox folder
	before    time.Time // the start of a local day, when hasBefore
	hasBefore bool      // false for any day
}

// pick returns the captures the selection names, oldest first. A session is
// named by its full id, as its file's name holds it or as the hooks were
// given it, or by the first 8 characters of its id; 8 characters that two
// sessions share, and no session's whole id, name neither, and pick says
// which they are.
func (s inboxSelection) pick(all []inbox.Capture) ([]inbox.Capture, error) {
	var in []inbox.Capture
	for _, c := range all {
		if s.project != "" && c.Project != s.project && c.Project != inboxSafeName(s.project) {
			continue
		}
		if s.hasBefore && !c.Mod.Before(s.before) {
			continue
		}
		in = append(in, c)
	}
	named := map[string]bool{}
	for _, v := range s.sessions {
		exact, prefixed := map[string]bool{}, map[string]bool{}
		for _, c := range in {
			switch {
			case c.ID == v || c.ID == inboxSafeName(v):
				exact[c.ID] = true
			case len([]rune(v)) <= 8 && inboxShortID(c.ID) == v:
				prefixed[c.ID] = true
			}
		}
		ids := exact
		if len(exact) == 0 {
			ids = prefixed
		}
		if len(ids) > 1 {
			list := make([]string, 0, len(ids))
			for id := range ids {
				list = append(list, id)
			}
			sort.Strings(list)
			return nil, fmt.Errorf("s:%s names %d sessions, so nothing was chosen; give the full id of the one to remove: %s", v, len(ids), strings.Join(list, ", "))
		}
		for id := range ids {
			named[id] = true
		}
	}
	var out []inbox.Capture
	for _, c := range in {
		if len(s.sessions) == 0 || named[c.ID] {
			out = append(out, c)
		}
	}
	sort.SliceStable(out, func(i, j int) bool { return out[i].Mod.Before(out[j].Mod) })
	return out, nil
}

// inboxSafeName is a session id or project key as the inbox names its file or
// folder.
func inboxSafeName(v string) string {
	if safe, err := paths.SafeName(v); err == nil {
		return safe
	}
	return v
}

// inboxShortID is the first 8 characters of a session id, which a short ref
// keeps.
func inboxShortID(id string) string {
	if r := []rune(id); len(r) > 8 {
		return string(r[:8])
	}
	return id
}

// inboxRemove removes the captures chosen. It takes the update's lock, so no
// update writes a watermark again for a file removed, and the search index
// of the captures' projects, so none is indexed meanwhile, without waiting
// for the first and waiting up to indexWait for the second; while either is
// busy it removes nothing (errInboxBusy). It then takes each session in turn
// under its lock, waiting up to lockWait for a hook writing it: its
// transcript recovery record goes, when it would append to a project folder
// whose capture goes and doesn't keep the chat off the record (see
// catchup.Forget), then the session is recorded as removed, then its
// session files go, each before its watermark. So a removal cut short, at a
// session a hook still holds, by ctx or by a failure, records no session it
// keeps, unless a file it recorded can't be removed or it is killed in
// between. Last it drops the index of each project a capture was removed
// from, and removes a project folder left empty. It returns the captures it
// removed, and stops at a session a hook still holds, or the first failure.
func inboxRemove(ctx context.Context, dirs paths.Dirs, chosen []inbox.Capture, now time.Time, indexWait, lockWait time.Duration) ([]inbox.Capture, error) {
	// The update job's lock (see internal/update's lockUpdates).
	unlock, busy, err := paths.TryLock(dirs.Locks(), "update.lock")
	if err != nil {
		return nil, err
	}
	if busy {
		return nil, errInboxBusy
	}
	defer unlock()
	var projects []string
	for _, c := range chosen {
		if !slices.Contains(projects, c.Project) {
			projects = append(projects, c.Project)
		}
	}
	hold, busy, err := searchindex.HoldFolders(dirs, projects, indexWait)
	if err != nil {
		return nil, err
	}
	if busy {
		return nil, errInboxBusy
	}
	defer hold.Release()
	store := inbox.Store{Root: dirs.Inbox()}
	type chat struct{ host, id string }
	var order []chat
	captures := map[chat][]inbox.Capture{}
	for _, c := range chosen {
		k := chat{c.Host, store.SessionID(c)}
		if _, ok := captures[k]; !ok {
			order = append(order, k)
		}
		captures[k] = append(captures[k], c)
	}
	var done []inbox.Capture
	var failure error
	for _, k := range order {
		lockCtx, cancel := context.WithTimeout(ctx, lockWait)
		release, err := session.Lock(lockCtx, dirs.Locks(), k.host, k.id)
		cancel()
		if err != nil {
			if ctx.Err() != nil {
				failure = ctx.Err()
			} else {
				failure = fmt.Errorf("a hook was still writing s:%s, so it and the chats after it were left; run this again", inboxShortID(k.id))
			}
			break
		}
		var folders []string
		for _, c := range captures[k] {
			folders = append(folders, c.Project)
		}
		// The session is recorded as removed just before its files go, so
		// the lint never takes a ref to it for one lost, and only once its
		// lock is held and Forget, which stops when ctx ends, is done: the
		// files' removal doesn't stop for ctx, so no session a removal cut
		// short keeps is recorded.
		err = catchup.Forget(ctx, dirs, k.host, k.id, folders)
		if err == nil {
			err = store.RecordRemoved(ctx, now, captures[k])
		}
		for _, c := range captures[k] {
			if err != nil {
				break
			}
			if err = store.Remove(c); err == nil {
				done = append(done, c)
			}
		}
		release()
		if err != nil {
			failure = err
			break
		}
	}
	for _, p := range projects {
		if slices.ContainsFunc(done, func(c inbox.Capture) bool { return c.Project == p }) {
			if err := hold.Drop(p); err != nil && failure == nil {
				failure = err
			}
			store.Tidy(p)
		}
	}
	return done, failure
}

// inboxList prints what eva-md inbox clear removes or would remove, the
// first inboxListed captures by name and the rest as a count.
func inboxList(out io.Writer, verb string, captures []inbox.Capture) {
	var total int64
	for _, c := range captures {
		total += c.Size
	}
	n := len(captures)
	fmt.Fprintf(out, "%s %d captured %s, %s:\n", verb, n, plural(n, "session", "sessions"), inboxSize(total))
	for i, c := range captures {
		if i == inboxListed {
			fmt.Fprintf(out, "  and %d more\n", n-i)
			break
		}
		if !c.File {
			fmt.Fprintf(out, "  %s %s in %s: its file is already gone, and only its watermark is left\n", c.Host, c.ID, c.Project)
			continue
		}
		fmt.Fprintf(out, "  %s %s in %s, last written %s, %s\n", c.Host, c.ID, c.Project, c.Mod.Local().Format("2006-01-02 15:04"), inboxSize(c.Size))
	}
}

// inboxCited says how many refs in the wiki's thread pages name the
// captures' sessions, chats, as the daily lint reads refs, or nothing
// without a wiki.
func inboxCited(ctx context.Context, dirs paths.Dirs, captures []inbox.Capture, chats string) string {
	c, err := config.Load(dirs.ConfigFile())
	if err != nil || c.WikiPath == "" {
		return ""
	}
	var ids []string
	for _, cp := range captures {
		ids = append(ids, cp.ID)
	}
	refs, pages, err := lint.Citing(ctx, c.WikiPath, ids)
	switch {
	case err != nil:
		return "The refs in the wiki to " + chats + " couldn't be counted: " + err.Error()
	case refs == 0:
		return "No ref in the wiki's thread pages names " + chats + "."
	}
	return fmt.Sprintf("%d %s on %d thread %s %s %s; the doctor counts %s as removed on purpose, not lost.", refs, plural(refs, "ref", "refs"), len(pages), plural(len(pages), "page", "pages"), plural(refs, "names", "name"), chats, plural(refs, "it", "them"))
}

// inboxSize words a number of bytes.
func inboxSize(n int64) string {
	switch {
	case n < 1024:
		return fmt.Sprintf("%d bytes", n)
	case n < 1<<20:
		return fmt.Sprintf("%.1f KB", float64(n)/(1<<10))
	case n < 1<<30:
		return fmt.Sprintf("%.1f MB", float64(n)/(1<<20))
	}
	return fmt.Sprintf("%.1f GB", float64(n)/(1<<30))
}
