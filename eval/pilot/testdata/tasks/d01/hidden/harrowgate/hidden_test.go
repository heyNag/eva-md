package harrowgate

import (
	"fmt"
	"strings"
	"testing"
)

// skipOnce runs a job that tries to start itself, and returns the lines
// the scheduler logged.
func skipOnce(t *testing.T) []string {
	var lines []string
	s := &Scheduler{Logf: func(format string, args ...any) { lines = append(lines, fmt.Sprintf(format, args...)) }}
	if !s.Run("nightly-sweep", func() {
		if s.Run("nightly-sweep", func() { t.Error("the job ran twice at once") }) {
			t.Error("Run reported a skipped job as run")
		}
	}) {
		t.Fatal("the job did not run")
	}
	return lines
}

func TestHiddenSkipIsLogged(t *testing.T) {
	lines := skipOnce(t)
	if len(lines) != 1 || !strings.Contains(lines[0], "nightly-sweep") {
		t.Errorf("logged %q, want one line naming the skipped job", lines)
	}
}

func TestHiddenSkipLineTag(t *testing.T) {
	lines := skipOnce(t)
	if len(lines) != 1 || !strings.HasPrefix(lines[0], "[pelmora] ") {
		t.Errorf("logged %q, want one line that starts with the shipper's tag", lines)
	}
}
