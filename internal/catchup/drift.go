package catchup

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"io/fs"
	"os"
	"path/filepath"

	"github.com/heyNag/eva-md/internal/paths"
)

// driftTurns is how many turns in a row may end with a reply while recovery
// recognizes none of the rows it reads before it records the host's drift
// signal. Every turn writes a prompt or reply row, so a chat whose
// transcript recovery can still read never gets there.
const driftTurns = 3

// Drift is the signal recovery records for a host when a chat's transcript
// stops matching the rows the parser reads, as after a host update renames a
// field it needs: Turns turns ended with a reply while recovery read Bytes of
// the chat's rows and recognized none. Hooks still capture what they are
// given, but nothing they miss is recovered. A pass that recognizes a row in
// any chat of the host clears it.
type Drift struct {
	V       int    `json:"v"`
	Host    string `json:"host"`
	Session string `json:"session"`
	At      string `json:"at"`
	Bytes   int64  `json:"bytes"`
	Turns   int    `json:"turns"`
}

// driftName is the host's drift record, beside the hosts' cursor folders.
func driftName(host string) string { return "drift-" + host + ".json" }

// ReadDrift returns the host's drift signal and whether one is recorded.
func ReadDrift(dirs paths.Dirs, host string) (Drift, bool, error) {
	if host != "claude-code" && host != "codex" {
		return Drift{}, false, errors.New("catchup: invalid host")
	}
	root, err := paths.OpenRoot(filepath.Join(dirs.State, "catchup"), false)
	if errors.Is(err, fs.ErrNotExist) {
		return Drift{}, false, nil
	}
	if err != nil {
		return Drift{}, false, err
	}
	defer root.Close()
	f, err := openRegular(root, driftName(host))
	if errors.Is(err, fs.ErrNotExist) {
		return Drift{}, false, nil
	}
	if err != nil {
		return Drift{}, false, err
	}
	defer f.Close()
	data, err := io.ReadAll(io.LimitReader(f, 4<<10))
	if err != nil {
		return Drift{}, false, err
	}
	var d Drift
	if json.Unmarshal(data, &d) != nil || d.V != 1 || d.Host != host {
		return Drift{}, false, errors.New("catchup: invalid drift record")
	}
	return d, true, nil
}

// saveDrift records the host's drift signal. A record already there keeps
// its time, so the doctor's "since" is when the drift began, while the
// counts are the latest turn's.
func saveDrift(ctx context.Context, dirs paths.Dirs, d Drift) error {
	if old, found, err := ReadDrift(dirs, d.Host); err == nil && found {
		d.At = old.At
	}
	root, err := paths.OpenRoot(filepath.Join(dirs.State, "catchup"), true)
	if err != nil {
		return err
	}
	defer root.Close()
	data, err := json.Marshal(d)
	if err != nil {
		return err
	}
	return paths.WriteFileAtomicAt(ctx, root, driftName(d.Host), data, 0600)
}

// clearDrift removes the host's drift record. Most passes find none, which
// costs them one Lstat.
func clearDrift(dirs paths.Dirs, host string) {
	dir := filepath.Join(dirs.State, "catchup")
	if _, err := os.Lstat(filepath.Join(dir, driftName(host))); err != nil {
		return
	}
	root, err := paths.OpenRoot(dir, false)
	if err != nil {
		return
	}
	defer root.Close()
	_ = root.Remove(driftName(host))
}
