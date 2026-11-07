package catchup

import (
	"context"
	"errors"
	"io/fs"
	"path/filepath"
	"slices"

	"github.com/heyNag/eva-md/internal/paths"
)

// Forget removes a session's recovery record when the session's capture in
// one of the inbox project folders named is being removed (eva-md inbox
// clear) and the record would append to it, or when the record can't be
// read, so recovery never brings back rows of a removed capture, as a
// cursor that lags behind the hooks could. Without a record, start-of-chat
// catch-up passes the session by, and a hook of the chat, if it is still
// open, registers it again at the transcript's end. A record that appends
// to another project folder, as after the chat moved there, stays, and so
// does a blocked one, as a chat off the record has: it may be all that
// keeps the chat off the record, when its markers couldn't be written, and
// it brings nothing back, since a blocked record recovers no row and
// leaving the block starts again at the transcript's end. The caller holds
// the session's lock (session.Lock).
func Forget(ctx context.Context, dirs paths.Dirs, host, id string, folders []string) error {
	name, err := stateFile(host, id)
	if err != nil {
		return err
	}
	s, found, err := load(ctx, dirs, host, id)
	switch {
	case ctx.Err() != nil:
		return ctx.Err()
	case err == nil && !found:
		return nil
	case err == nil && s.Blocked:
		return nil
	case err == nil:
		folder, e := paths.SafeName(s.Entry.Project)
		if e == nil && !slices.Contains(folders, folder) {
			return nil
		}
	}
	root, err := paths.OpenRoot(filepath.Join(dirs.State, "catchup"), false)
	if errors.Is(err, fs.ErrNotExist) {
		return nil
	}
	if err != nil {
		return err
	}
	defer root.Close()
	if err := root.Remove(name); err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return nil
		}
		return err
	}
	return paths.SyncDirAt(root, host)
}
