package health

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"

	"github.com/heyNag/eva-md/internal/config"
	"github.com/heyNag/eva-md/internal/paths"
)

// The one line a chat start may show, with the switch health on, is
// prepared outside the hook: eva-md doctor --notice runs Prepare, which
// evaluates the conditions (see Conditions) and writes each host's line to
// <state>/health/notice.json; the hook reads its own host's line there and
// computes nothing, and once it has shown one it records it in
// <state>/health/shown.json, so a line is shown once a day at most.
const (
	noticeFolder = "health"
	noticeFile   = "notice.json"
	shownFile    = "shown.json"
	noticeLock   = "health-notice.lock"
)

const (
	// NoticeEvery is the age past which a chat start asks for the notice
	// to be prepared again.
	NoticeEvery = 30 * time.Minute
	// noticeFresh is how long a prepared line may be shown. The notice is
	// prepared when a chat start or a prompt asks for it, so the line a
	// start reads is the one prepared at the start or prompt before it,
	// which may be the evening before: within a shorter time, a person
	// whose starts are hours apart would never see a line. A condition
	// that closed since is shown once at most, as the same start asks for
	// the notice again.
	noticeFresh = 24 * time.Hour
	// shownQuiet is how long a line, once shown, is not shown again.
	shownQuiet = 24 * time.Hour
	// maxShown bounds the lines shown.json keeps.
	maxShown = 32
)

// notice is notice.json: when it was prepared, and each host's line, by the
// host's id; a host with nothing to say has none.
type notice struct {
	V        int                   `json:"v"`
	Prepared string                `json:"prepared"`
	Lines    map[string]NoticeLine `json:"lines"`
}

// A NoticeLine is a condition's id and its line.
type NoticeLine struct {
	ID   string `json:"id"`
	Text string `json:"text"`
}

// shown is shown.json: the line shown last, by its id, and when, as
// {"id","at"}, and the others shown in the 24 hours before it, so two
// hosts whose lines differ do not each show theirs at every other start.
type shown struct {
	ID      string      `json:"id"`
	At      string      `json:"at"`
	Earlier []shownLine `json:"earlier,omitempty"`
}

type shownLine struct {
	ID string `json:"id"`
	At string `json:"at"`
}

// Prepare writes the notice: with the switch health on, it evaluates the
// conditions at now and writes, for each host's chat start, the most severe
// relevant one, leaving out that host's own capture.silent and
// trust.review, to <state>/health/notice.json, atomically (folder 0700,
// file 0600). A lock keeps two preparers apart: while another holds it,
// this one does nothing. It then removes what a healthy state should not
// hold: the needs-trust flag once the hooks it waits for have run, and the
// push check's old timer, commit/push-retry, which the hook names
// commit/push-checked with the switch on. It reads local files and runs
// git's local reads in the wiki; it starts no host command and makes no
// network or model call. With the switch off, or no config to read, it
// does nothing.
func Prepare(ctx context.Context, d paths.Dirs, now time.Time) error {
	c, err := config.LoadContext(ctx, d.ConfigFile())
	if err != nil {
		return err
	}
	if !Switch.On(c) {
		return nil
	}
	unlock, busy, err := paths.TryLock(d.Locks(), noticeLock)
	if err != nil || busy {
		return err
	}
	defer unlock()
	conds := evaluate(ctx, d, c, now)
	// A check cut short may have missed a condition: the last notice stays,
	// and is marked as checked, so chat starts wait the usual time before
	// asking for another check, which would likely be cut short too.
	if err := ctx.Err(); err != nil {
		return errors.Join(err, markChecked(d, now))
	}
	n := notice{V: 1, Prepared: now.Format(time.RFC3339), Lines: map[string]NoticeLine{}}
	for _, host := range updateHosts {
		if cond, ok := lineFor(conds, host); ok {
			n.Lines[host] = NoticeLine{cond.ID, cond.Text}
		}
	}
	data, err := json.Marshal(n)
	if err != nil {
		return err
	}
	root, err := paths.OpenRoot(filepath.Join(d.State, noticeFolder), true)
	if err != nil {
		return err
	}
	err = paths.WriteFileAtomicAt(ctx, root, noticeFile, append(data, '\n'), 0o600)
	root.Close()
	return errors.Join(err, tidy(d, now))
}

// evaluate is Conditions; a test replaces it to cut a check short while it
// runs.
var evaluate = Conditions

// markChecked marks the notice as checked at now without changing what it
// says: it sets notice.json's modification time, which NoticeDue reads, or,
// with no notice yet, writes one with no time and no line, which no chat
// start shows. It has a moment of its own, since the check's time is out.
func markChecked(d paths.Dirs, now time.Time) error {
	root, err := paths.OpenRoot(filepath.Join(d.State, noticeFolder), true)
	if err != nil {
		return err
	}
	defer root.Close()
	if _, err := root.Lstat(noticeFile); err != nil {
		ctx, cancel := context.WithTimeout(context.Background(), time.Second)
		defer cancel()
		if err := paths.WriteFileAtomicAt(ctx, root, noticeFile, []byte(`{"v":1,"prepared":"","lines":{}}`+"\n"), 0o600); err != nil {
			return err
		}
	}
	return root.Chtimes(noticeFile, now, now)
}

// tidy removes the needs-trust flag once the hooks it waits for have run,
// unless install wrote it again meanwhile, and the push check's old timer.
// A flag that does not read as install writes it stays.
func tidy(d paths.Dirs, now time.Time) error {
	root, err := paths.OpenRoot(d.State, false)
	if err != nil {
		return err
	}
	defer root.Close()
	var errs []error
	if reviewer, flag, clean, ok := trustFlag(d); ok && clean && trustMet(d, reviewer, flag, now) {
		errs = append(errs, removeFlag(root, flag))
	}
	if err := root.Remove(filepath.Join("commit", "push-retry")); err != nil && !errors.Is(err, os.ErrNotExist) {
		errs = append(errs, err)
	}
	return errors.Join(errs...)
}

// removeFlag removes the needs-trust flag in root, the state folder, when
// it is still the file flag describes: the same file, with the same
// modification time and size. One install wrote again after flag was read
// stays.
func removeFlag(root *os.Root, flag os.FileInfo) error {
	info, err := root.Lstat("needs-trust.json")
	if err != nil || !os.SameFile(info, flag) || !info.ModTime().Equal(flag.ModTime()) || info.Size() != flag.Size() {
		return nil
	}
	return root.Remove("needs-trust.json")
}

// NoticeDue reports whether a chat start should ask for the notice to be
// prepared: notice.json is missing, or was last written more than
// NoticeEvery before now, or after it. It costs one stat.
func NoticeDue(d paths.Dirs, now time.Time) bool {
	info, err := os.Lstat(filepath.Join(d.State, noticeFolder, noticeFile))
	if err != nil {
		return true
	}
	age := now.Sub(info.ModTime())
	return age > NoticeEvery || age < -time.Minute
}

// PreparedLine is the line a chat start of host shows at now, by its id,
// or "" for none: host's line in notice.json, prepared in the last 24 hours,
// that is not one shown.json records as shown in the last 24. It reads the
// two files and computes nothing; a line that is not one this build could
// prepare is never shown.
func PreparedLine(d paths.Dirs, host string, now time.Time) (id, text string) {
	data, err := readPrivate(filepath.Join(d.State, noticeFolder, noticeFile), 64<<10)
	var n notice
	if err != nil || json.Unmarshal(data, &n) != nil || n.V != 1 {
		return "", ""
	}
	prepared, err := time.Parse(time.RFC3339, n.Prepared)
	if err != nil || now.Sub(prepared) > noticeFresh || prepared.After(now.Add(time.Minute)) {
		return "", ""
	}
	line, ok := n.Lines[host]
	if !ok || !knownID(line.ID) || !validLine(line.Text) {
		return "", ""
	}
	if _, quiet := shownSince(readShown(d), now)[line.ID]; quiet {
		return "", ""
	}
	return line.ID, line.Text
}

// validLine reports a line a chat start may show: valid UTF-8, starting
// "eva.md: ", at most maxLine characters, with no control character.
func validLine(text string) bool {
	if !utf8.ValidString(text) || !strings.HasPrefix(text, "eva.md: ") || utf8.RuneCountInString(text) > maxLine {
		return false
	}
	return strings.IndexFunc(text, unicode.IsControl) < 0
}

// readShown reads shown.json, or nothing when it can't be read.
func readShown(d paths.Dirs) shown {
	var s shown
	data, err := readPrivate(filepath.Join(d.State, noticeFolder, shownFile), 16<<10)
	if err != nil || json.Unmarshal(data, &s) != nil {
		return shown{}
	}
	return s
}

// shownSince maps each line s records as shown in the 24 hours before now
// to when it was shown last; a time that can't be read, or that is more
// than a minute after now, counts as none.
func shownSince(s shown, now time.Time) map[string]time.Time {
	out := map[string]time.Time{}
	for _, l := range append([]shownLine{{s.ID, s.At}}, s.Earlier...) {
		at, err := time.Parse(time.RFC3339, l.At)
		if l.ID == "" || err != nil || at.After(now.Add(time.Minute)) || now.Sub(at) >= shownQuiet {
			continue
		}
		if last, ok := out[l.ID]; !ok || at.After(last) {
			out[l.ID] = at
		}
	}
	return out
}

// MarkShown records in shown.json that the line id was shown at now, with
// the others shown in the 24 hours before, atomically, within ctx. Two
// chat starts that record at once may each keep only its own line, which
// costs at most one line shown once more.
func MarkShown(ctx context.Context, d paths.Dirs, id string, now time.Time) error {
	if !knownID(id) {
		return errors.New("health: not a condition")
	}
	earlier := shownSince(readShown(d), now)
	delete(earlier, id)
	s := shown{ID: id, At: now.Format(time.RFC3339)}
	for _, other := range conditionOrder(earlier) {
		if len(s.Earlier) == maxShown {
			break
		}
		s.Earlier = append(s.Earlier, shownLine{other, earlier[other].Format(time.RFC3339)})
	}
	data, err := json.Marshal(s)
	if err != nil {
		return err
	}
	root, err := paths.OpenRoot(filepath.Join(d.State, noticeFolder), true)
	if err != nil {
		return err
	}
	defer root.Close()
	return paths.WriteFileAtomicAt(ctx, root, shownFile, append(data, '\n'), 0o600)
}

// conditionOrder lists the ids of shown, newest first, the same order for
// the same map.
func conditionOrder(shown map[string]time.Time) []string {
	var ids []string
	for id := range shown {
		ids = append(ids, id)
	}
	slices.SortFunc(ids, func(a, b string) int {
		if c := shown[b].Compare(shown[a]); c != 0 {
			return c
		}
		return strings.Compare(a, b)
	})
	return ids
}
