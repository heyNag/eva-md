package update

import (
	"testing"

	"github.com/heyNag/eva-md/internal/threadpage"
)

// Exports for the test of the accuracy suite's live run, in package
// update_test, which can import internal/update/accuracy/live: the canned
// outputs are here, in the suite's offline tests.

// Isolate gives a test its own home, git config and temp folder, and a PATH
// of only the fake CLIs and the system folders.
var Isolate = isolate

// CannedAnswer is the canned output of pass n of the fact script named
// history, as a model would write it for batch b and page: the correct one,
// or with content, when the pass has one, the first adversarial output
// whose content failure only the metrics catch.
func CannedAnswer(t *testing.T, history string, n int, b Batch, page threadpage.Page, content bool) []byte {
	t.Helper()
	for _, s := range factScripts() {
		if s.name != history {
			continue
		}
		ps := s.passes[n-1]
		answer := ps.correct
		for _, bd := range ps.bad {
			if content && bd.content {
				answer = bd.output
				break
			}
		}
		return encode(t, answer(view{t: t, b: b, p: page}))
	}
	t.Fatalf("no fact script %q", history)
	return nil
}
