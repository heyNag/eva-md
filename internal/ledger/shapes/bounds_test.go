package shapes

import (
	"context"
	"maps"
	"slices"
	"strings"
	"sync"
	"testing"

	"github.com/heyNag/eva-md/internal/inbox"
)

// countdown is a context whose time runs out at its nth look: Err then
// reports it, and Done is closed, at that exact point and not by a clock.
type countdown struct {
	context.Context
	mu   sync.Mutex
	n    int
	done chan struct{}
}

func newCountdown(n int) *countdown {
	return &countdown{Context: context.Background(), n: n, done: make(chan struct{})}
}

func (c *countdown) Err() error {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.n > 0 {
		c.n--
		if c.n == 0 {
			close(c.done)
		}
	}
	if c.n == 0 {
		return context.Canceled
	}
	return nil
}

func (c *countdown) Done() <-chan struct{} { return c.done }

// ended is whether time ran out.
func (c *countdown) ended() bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.n == 0
}

// When time runs out anywhere, also while the inbox is read, every part
// says it is incomplete, and each host file not read by then is counted as
// not reached, never as not read whole, so the parts' states and the
// files' counts do not take a cut for a file that could not be read.
func TestAReportCutInTheInboxSaysSo(t *testing.T) {
	cuts := 0
	for n := 1; ; n++ {
		f := newFixture(t, noLink)
		oneChatEach(f)
		ctx := newCountdown(n)
		r, _ := f.runWith(ctx)
		if !ctx.ended() {
			break
		}
		cuts++
		for p, state := range r.Parts {
			if state != PartIncomplete {
				t.Errorf("cut at look %d: part %s is %s", n, p, state)
			}
		}
		for _, h := range []string{claude, codex} {
			listed, read, notReached := get(r.Hosts, h+"/files/listed"), get(r.Hosts, h+"/files/read"), get(r.Hosts, h+"/files/not_reached")
			if read+notReached != listed || get(r.Hosts, h+"/files/not_read_whole") != 0 {
				t.Errorf("cut at look %d: %s: %v", n, h, leaves(r.Hosts))
			}
		}
	}
	if cuts < 3 {
		t.Errorf("time ran out at only %d looks", cuts)
	}
}

// A Codex file whose first record is longer than the 64 KiB recovery's
// opener reads for it is refused unread and counted as not identified,
// its chat as not read whole, and adds nothing to F-134, which says it is
// settled in part; a first record of under 64 KiB is read and counted.
func TestALongFirstCodexRowIsNotIdentified(t *testing.T) {
	f := newFixture(t, noLink)
	oneChatEach(f)
	ids := codexIDs(2)
	for i, size := range []int{70 << 10, 60 << 10} {
		path := f.codexFile(5, ids[i], xMeta(f, ids[i], map[string]any{"source": "cli", "instructions": strings.Repeat("i", size)}),
			xRow(at(1, 0), "event_msg", map[string]any{"type": "user_message", "message": "hi"}))
		f.session(codex, ids[i], path)
	}
	r, _ := f.run()
	for path, want := range map[string]int{
		"codex/files/listed": 3, "codex/files/read": 2, "codex/files/not_identified": 1, "codex/files/refused": 0,
		"codex/chats/read": 2, "codex/chats/not_read_whole": 1,
	} {
		if got := get(r.Hosts, path); got != want {
			t.Errorf("%s is %d, want %d", path, got, want)
		}
	}
	for path, want := range map[string]int{
		"F-134/codex/first_row_bytes/under 1 KiB": 1, "F-134/codex/first_row_bytes/16 to 64 KiB": 1, "F-134/codex/first_row_bytes/64 KiB to 1 MiB": 0,
	} {
		if got := get(r.Facts, path); got != want {
			t.Errorf("%s is %d, want %d", path, got, want)
		}
	}
	if slices.Contains(r.Settled["in_full"], "F-134") || !slices.Contains(r.Settled["in_part"], "F-134") {
		t.Errorf("settled %v", r.Settled)
	}
}

// A row dated before 2000, as one in year 0000 that is a day earlier in
// UTC, or after the year after the report's, is counted as out of range,
// and its day is never printed: the report still prints.
func TestATimeOutOfRangeIsNotADay(t *testing.T) {
	f := newFixture(t, noLink)
	oneChatEach(f)
	id := marker + "-odd-times"
	early, late := cUser(f, id, "p1", "", "early"), cUser(f, id, "p2", "", "late")
	early["timestamp"], late["timestamp"] = "0000-01-01T00:30:00+01:00", "2028-01-01T00:00:00Z"
	path := f.claudeFile("", id, early, cUser(f, id, "p3", at(2, 0), "in range"), late)
	f.session(claude, id, path, inbox.Entry{TS: at(2, 0), Turn: "p3", Role: "user", Kind: "prompt", Text: marker})
	r, _ := f.run()
	if got := get(r.Hosts, "claude-code/rows/out_of_range"); got != 2 {
		t.Errorf("out of range %d, hosts %v", got, leaves(r.Hosts))
	}
	if want := map[string][2]string{claude: {"2026-11-02", "2026-11-02"}, codex: {"2026-11-02", "2026-11-02"}}; !maps.Equal(r.Days, want) {
		t.Errorf("days %v", r.Days)
	}
}
