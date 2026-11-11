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
// through no symlink, and on Codex a first record that names the session
// and its folder. The caller closes the file.
func OpenTranscript(s Source) (*os.File, error) {
	f, err := openTranscript(s)
	if err != nil {
		return nil, err
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
