package hook

import (
	"context"
	"time"

	"github.com/heyNag/eva-md/internal/health"
)

// The health line a chat start may show is prepared outside the hook, by
// eva-md doctor --notice (see health.Prepare): with the switch health on,
// a brief asks for that job when the prepared notice is missing or older
// than 30 minutes, at the cost of one stat, and once the output has
// carried a prepared line the launch records it as shown. Off, the
// default, neither does anything, and a chat start says what v1's
// QuickNotice finds.
func init() {
	registerHandler(310, "health notice", atBriefed, askNotice)
	registerHandler(410, "health notice shown", atLaunch, noticeShown)
}

// noticeJob prepares the notice. It runs git's local reads in the wiki
// only, so it keeps none of git's variables.
var noticeJob = job{args: []string{"doctor", "--notice"}, reason: reasonHealth}

// askNotice asks for noticeJob when the notice is due (see
// health.NoticeDue).
func askNotice(t *turn) {
	if health.Switch.On(t.env.Config) && health.NoticeDue(t.env.Dirs, t.now) {
		t.ask(noticeJob)
	}
}

// noticeShown records the line the run's output carried as shown, after
// the output is written, within a moment of its own: the capture's context
// is done by then.
func noticeShown(t *turn) {
	if t.shown == "" || !health.Switch.On(t.env.Config) {
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond+t.env.Slack)
	defer cancel()
	_ = health.MarkShown(ctx, t.env.Dirs, t.shown, t.now)
}
