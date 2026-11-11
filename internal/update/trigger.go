package update

import (
	"crypto/sha256"
	"encoding/hex"
	"slices"
	"time"
)

// The triggers that start an update, as the run log names them: the hooks
// pass the name with TriggerFlag on the job they start, and a person's
// eva-md update --foreground is manual. A job started with no name, by an
// older hook or a person without --foreground, records none. The sessions
// the day's sweep picks are recorded as sweep, whatever started the job
// that took it.
const (
	TriggerEnd     = "session_end" // SessionEnd
	TriggerCompact = "compact"     // PreCompact
	TriggerStop    = "stop_size"   // a Stop with 60 KB or more to process
	TriggerGap     = "prompt_gap"  // a prompt 30 minutes or more after the chat's file last changed
	TriggerStart   = "chat_start"  // a chat start's catch-up of idle sessions
	TriggerSweep   = "sweep"       // the day's sweep of idle chats
	TriggerManual  = "manual"      // eva-md update --foreground
	TriggerOther   = "other"       // a name this release doesn't know, as a later hook may pass
)

// TriggerFlag is the flag of eva-md update that names its trigger. Releases
// up to 2026.11.7 refuse it, so the hooks pass it only to the build they
// run (see hook.detach).
const TriggerFlag = "--trigger"

// Triggers lists the names a record can carry, in the order a report shows
// them.
var Triggers = []string{TriggerEnd, TriggerCompact, TriggerStop, TriggerGap, TriggerStart, TriggerSweep, TriggerManual, TriggerOther}

// TriggerName is the name the run log records for a trigger as the flag
// gave it: a known name as it is, none as none, and any other as other, so
// no text a command line carries reaches the run log, and a name a later
// hook passes still starts its update.
func TriggerName(s string) string {
	if s == "" || slices.Contains(Triggers, s) {
		return s
	}
	return TriggerOther
}

// OutcomeBusy is the outcome of a job that left because another held the
// update lock. Its record names no host and goes to a file of its own
// beside the run log (see busyFile), which only the report of eva-md
// status --updates reads, so the budget, holds, backoffs, quarantines and
// every reader of the run log are what they were.
const OutcomeBusy = "busy"

// sessionHash is the busy record's name for a session: the first 16 hex
// digits of a SHA-256 of its host and id, from which the id can't be read
// back, and which is the same for every busy exit of the session, so the
// report can count sessions.
func sessionHash(host, id string) string {
	sum := sha256.Sum256([]byte("eva-md busy session\x00" + host + "\x00" + id))
	return hex.EncodeToString(sum[:8])
}

// busy records a job that left because another job held the update lock:
// its trigger, the sweep's when it was started for the sweep alone, and
// the sessions it was started for, by sessionHash. A record that can't be
// written is logged; the job still leaves as it always did.
func (j Job) busy(refs []Ref, sweep bool) {
	rec := Record{At: j.now().Format(time.RFC3339), Outcome: OutcomeBusy, Trigger: j.Trigger, Manual: j.Manual}
	if len(refs) == 0 && sweep {
		rec.Trigger = TriggerSweep
	}
	for _, ref := range refs {
		rec.SessionHashes = append(rec.SessionHashes, sessionHash(ref.Host, ref.ID))
	}
	if err := appendTo(j.Dirs.State, busyFile(day(rec.At)), rec); err != nil {
		j.logf("the busy exit was not recorded: %v", err)
	}
}
