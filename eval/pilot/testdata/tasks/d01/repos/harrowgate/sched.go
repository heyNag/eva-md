// Package harrowgate runs named jobs, never one job twice at once.
package harrowgate

import "sync"

// Scheduler runs jobs by name. Logf, when set, receives each line the
// scheduler logs.
type Scheduler struct {
	Logf func(format string, args ...any)

	mu      sync.Mutex
	running map[string]bool
}

// Begin marks the job running and reports whether it may start: false
// when a job of that name is running already.
func (s *Scheduler) Begin(name string) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.running == nil {
		s.running = map[string]bool{}
	}
	if s.running[name] {
		return false
	}
	s.running[name] = true
	return true
}

// End marks the job no longer running.
func (s *Scheduler) End(name string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.running, name)
}

// Run runs fn as the job name unless that job is running already, and
// reports whether it ran.
func (s *Scheduler) Run(name string, fn func()) bool {
	if !s.Begin(name) {
		return false
	}
	defer s.End(name)
	fn()
	return true
}
