package harrowgate

import "testing"

func TestRunNeverTwiceAtOnce(t *testing.T) {
	var s Scheduler
	inner := true
	ran := s.Run("sweep", func() { inner = s.Run("sweep", func() {}) })
	if !ran || inner {
		t.Errorf("Run = %v, the nested Run = %v; want true and false", ran, inner)
	}
	if !s.Run("sweep", func() {}) {
		t.Error("the job did not run again once it had ended")
	}
}
