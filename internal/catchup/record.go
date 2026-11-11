package catchup

import (
	"context"
	"errors"
	"os"

	"github.com/heyNag/eva-md/internal/paths"
)

// Record reads the session's recovery record, as recovery reads it, and
// changes nothing. found is false when there is none.
func Record(ctx context.Context, dirs paths.Dirs, host, id string) (s Source, found bool, err error) {
	return load(ctx, dirs, host, id)
}

// Admits reports whether recovery's rules admit the transcript s names,
// judged by its path alone, before anything is opened: an absolute path of
// the host's file for the session, in the host's own folder of chats.
func Admits(s Source) bool {
	_, err := transcriptPath(s)
	return err == nil
}

// OpenTranscript opens, read-only, the transcript s names under recovery's
// rules: those Admits judges, then a regular file with one link reached
// through no symlink, the file registered (its device and inode) and no
// shorter than recovery's cursor, and on Codex a first record that names
// the session and its folder. The caller closes the file.
func OpenTranscript(s Source) (*os.File, error) {
	f, err := openTranscript(s)
	if err != nil {
		return nil, err
	}
	info, err := f.Stat()
	if err != nil {
		f.Close()
		return nil, err
	}
	// As Recover judges it, a scan of a long row that no longer fits the
	// cursor set aside first.
	skip := s.Skip != nil && s.Skip.Start == s.Offset && s.Skip.Pos >= s.Skip.Start
	if dev, ino := fileID(info); dev != s.Device || ino != s.Inode || info.Size() < s.Offset || skip && info.Size() < s.Skip.Pos {
		f.Close()
		return nil, errors.New("catchup: transcript replaced or truncated")
	}
	if s.Host == "codex" {
		header := make([]byte, 64<<10)
		n, _ := f.ReadAt(header, 0)
		if !codexIdentity(header[:n], s) {
			f.Close()
			return nil, errors.New("catchup: transcript identity mismatch")
		}
	}
	return f, nil
}

// LongRow follows a row too long to decode whole, as recovery's scan of
// one does, for the record control it holds. It keeps a few short fields
// and none of the row's text. The zero LongRow starts a row.
type LongRow struct{ z rowScan }

// Feed scans the next part of the row; data holds no newline.
func (l *LongRow) Feed(data []byte) { l.z.feed(data) }

// Control returns the record control the whole row gives, by recovery's
// rules for a user's row of the session s names in any folder, and the
// row's time ("" when it has no valid one). ok is false when the row is
// not JSON the scan follows, so whether it holds a control is not known.
func (l *LongRow) Control(s Source) (control, ts string, ok bool) {
	if l.z.Bad || l.z.State != scanEnd {
		return "", "", false
	}
	if cwd := l.z.field(fieldCwd); cwd != "" {
		s.Cwd = cwd
	}
	control, ts = l.z.verdict(s)
	return control, ts, true
}
