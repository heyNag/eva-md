package catchup

import (
	"context"
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/heyNag/eva-md/internal/paths"
	"github.com/heyNag/eva-md/internal/session"
)

// The daily maintain keeps each host's recovery records within these: a
// record not saved for RecordsMaxAge is removed, and so is one past the
// newest RecordsKept of its host. That keeps a host's folder well within the
// 4,096 records the chat-start catch-up lists (see RecoverProject), so the
// 256 newest it keeps of them are the host's newest.
const (
	RecordsMaxAge = 30 * 24 * time.Hour
	RecordsKept   = 2048
)

// Prune removes, from each host's folder of recovery records,
// <state>/catchup/<host>/, the records not saved in the RecordsMaxAge before
// now and those past the newest RecordsKept, newest by when each was last
// saved, and returns how many it removed. It removes nothing else: the
// drift records beside those folders stay, and so does anything in them
// that isn't a record. A chat whose record is gone starts again at its
// transcript's end at its next event, as a new chat does, so nothing is read
// twice; whether it is off the record comes from the session's marker, so a
// record that keeps the chat off the record stays while that marker isn't
// set. The hooks save a record only under its session's lock, so Prune
// removes one only while it holds that lock, which it takes without
// waiting, and only if the record is still the one it listed: a record a
// hook holds, or saved again after the listing, stays (see pruneRecord).
// A record that can't be removed stays for the next run, and the first
// such error is returned once the rest are done.
func Prune(ctx context.Context, dirs paths.Dirs, now time.Time) (int, error) {
	return prune(ctx, dirs, now, RecordsMaxAge, RecordsKept)
}

// prune is Prune with the oldest a record may be and how many a host keeps.
func prune(ctx context.Context, dirs paths.Dirs, now time.Time, maxAge time.Duration, keep int) (int, error) {
	root, err := paths.OpenRoot(filepath.Join(dirs.State, "catchup"), false)
	if errors.Is(err, fs.ErrNotExist) {
		return 0, nil
	}
	if err != nil {
		return 0, err
	}
	defer root.Close()
	removed := 0
	var first error
	for _, host := range []string{"claude-code", "codex"} {
		n, err := pruneHost(ctx, root, dirs, host, now, maxAge, keep)
		removed += n
		if first == nil {
			first = err
		}
	}
	return removed, first
}

// pruneHost is prune for one host's folder.
func pruneHost(ctx context.Context, root *os.Root, dirs paths.Dirs, host string, now time.Time, maxAge time.Duration, keep int) (int, error) {
	d, err := root.Open(host)
	if errors.Is(err, fs.ErrNotExist) {
		return 0, nil
	}
	if err != nil {
		return 0, err
	}
	items, err := d.ReadDir(-1)
	d.Close()
	if err != nil {
		return 0, err
	}
	type record struct {
		name string
		mod  time.Time
	}
	var records []record
	for _, item := range items {
		if !strings.HasSuffix(item.Name(), ".json") || !item.Type().IsRegular() {
			continue
		}
		if info, err := item.Info(); err == nil {
			records = append(records, record{item.Name(), info.ModTime()})
		}
	}
	sort.Slice(records, func(i, j int) bool {
		if !records[i].mod.Equal(records[j].mod) {
			return records[i].mod.After(records[j].mod)
		}
		return records[i].name < records[j].name
	})
	removed := 0
	var first error
	for i, r := range records {
		if i < keep && now.Sub(r.mod) <= maxAge {
			continue
		}
		if err := ctx.Err(); err != nil {
			return removed, err
		}
		switch gone, err := pruneRecord(ctx, root, dirs, filepath.Join(host, r.name), r.mod); {
		case gone:
			removed++
		case err != nil && first == nil:
			first = err
		}
	}
	return removed, first
}

// beforePruneLock runs between pruneRecord's read of a record and its try
// of the session's lock; a test saves the record again there, as a hook
// would.
var beforePruneLock = func() {}

// pruneRecord removes the record name, as listed when it was last saved at
// mod, and reports whether it did. The hooks save a record only under the
// lock of the session it names, which a file's name can't always give back,
// so it reads the session from the record and takes that lock without
// waiting: while a hook holds it, the record stays for the next run. Under
// the lock it checks the record is still the one listed, since a hook that
// saved it after the listing replaced it, and only then removes it. A
// blocked record of a chat off the record stays while the session's
// off-the-record marker isn't set, as when it couldn't be written: the
// record is then all that keeps the chat off the record, until recovery
// writes the marker at the chat's next pass. A record that can't be read,
// damaged or of another version, is removed as listed without the lock: no
// hook saves one again, since each reads the record first and stops at one
// it can't.
func pruneRecord(ctx context.Context, root *os.Root, dirs paths.Dirs, name string, mod time.Time) (bool, error) {
	s, found, err := loadFile(ctx, root, name)
	switch {
	case ctx.Err() != nil:
		return false, ctx.Err()
	case err == nil && !found:
		return false, nil
	case err == nil:
		beforePruneLock()
		release, busy, err := session.TryLock(dirs.Locks(), s.Host, s.Session)
		if err != nil || busy {
			return false, err
		}
		defer release()
	}
	if info, err := root.Lstat(name); err != nil || !info.Mode().IsRegular() || !info.ModTime().Equal(mod) {
		return false, nil
	}
	if found && s.Blocked && s.Reason == "off_record" {
		if _, set, err := (session.Markers{Root: dirs.Sessions()}).GetContext(ctx, s.Host, s.Session, session.OffRecord); err != nil || !set {
			return false, err
		}
	}
	switch err := root.Remove(name); {
	case err == nil:
		return true, nil
	case errors.Is(err, fs.ErrNotExist):
		return false, nil
	default:
		return false, err
	}
}
