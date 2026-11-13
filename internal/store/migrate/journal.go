package migrate

import (
	"encoding/json"
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"

	"github.com/heyNag/eva-md/internal/paths"
)

// journalFile is the record of a step under way, in the state folder: it
// exists from before the first change to the store until the step's commit
// is made, so a run that finds it knows the store holds a step's unfinished
// work, or its commit not yet acknowledged.
const journalFile = "migrate-journal.json"

// A journal records the run's pass, its step, the commit before, and the
// store id the run gives a store without one, so a recovery writes the
// same bytes whatever id the machine offers then.
type journal struct {
	Pass    string `json:"pass"`
	From    int    `json:"from"`
	To      int    `json:"to"`
	Before  string `json:"before"`
	StoreID string `json:"store_id,omitempty"`
}

var reCommit = regexp.MustCompile(`^([0-9a-f]{40}|[0-9a-f]{64})$`)

var errJournal = errors.New("migrate: the journal is unreadable")

// readJournal returns the journal in stateDir, or nil when there is none.
func readJournal(stateDir string) (*journal, error) {
	b, err := readSmall(filepath.Join(stateDir, journalFile), 4<<10)
	if errors.Is(err, fs.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, errJournal
	}
	var j journal
	if json.Unmarshal(b, &j) != nil || !rePass.MatchString(j.Pass) || !reCommit.MatchString(j.Before) || j.From < 1 || j.To <= j.From ||
		j.StoreID != "" && !reStoreID.MatchString(j.StoreID) {
		return nil, errJournal
	}
	return &j, nil
}

func writeJournal(stateDir string, j journal) error {
	b, err := json.Marshal(j)
	if err != nil {
		return err
	}
	if err := paths.EnsureDir(stateDir); err != nil {
		return err
	}
	err = paths.WriteFileAtomic(filepath.Join(stateDir, journalFile), b, 0o600)
	hook("journal write")
	return err
}

func removeJournal(stateDir string) error {
	err := os.Remove(filepath.Join(stateDir, journalFile))
	if err == nil {
		err = paths.SyncDir(stateDir)
	}
	hook("journal remove")
	return err
}
