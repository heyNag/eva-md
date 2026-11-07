package hook

import (
	"context"
	"fmt"
	"path/filepath"
	"sync"
	"testing"

	"github.com/heyNag/eva-md/internal/session"
)

// Saves that start the same chat's new prompts marker at once each save,
// never fail: on macOS, a file created through os.Root without O_EXCL can
// fail with ENOENT for the caller that loses the race. The session lock
// keeps a chat's saves apart, but the marker never depends on it to be
// created.
func TestNewPromptsMarkerTakesConcurrentCreators(t *testing.T) {
	markers := session.Markers{Root: filepath.Join(t.TempDir(), "sessions")}
	for i := 0; i < 50; i++ {
		id := fmt.Sprint("chat-", i)
		start := make(chan struct{})
		var wg sync.WaitGroup
		for worker := 0; worker < 8; worker++ {
			wg.Go(func() {
				<-start
				if err := saveState(context.Background(), markers, "codex", id, promptState{Prompts: worker + 1}); err != nil {
					t.Errorf("%s: %v", id, err)
				}
			})
		}
		close(start)
		wg.Wait()
		if t.Failed() {
			return
		}
		if s, err := readState(context.Background(), markers, "codex", id); err != nil || s.Prompts < 1 {
			t.Fatalf("%s: state %+v, %v", id, s, err)
		}
	}
}
