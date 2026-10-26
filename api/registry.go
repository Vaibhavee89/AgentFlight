package api

import (
	"context"
	"errors"
	"sync"

	"github.com/Vaibhavee89/agentflight/internal/runtime"
)

// Run statuses reported by the API.
const (
	statusRunning     = "running"
	statusCompleted   = "completed"
	statusCancelled   = "cancelled"
	statusFailed      = "failed"
	statusInterrupted = "interrupted"
)

// run is the in-process record of one run started by this server. cancel and
// done are set only while a goroutine is executing the run.
type run struct {
	status string
	result runtime.RunResult
	err    string
	cancel context.CancelFunc
	done   chan struct{}
}

// registry tracks runs started by this process, guarded by a mutex. A run
// absent from the registry is either unknown or was left behind by a previous
// process; the server falls back to the event log for those.
type registry struct {
	mu   sync.Mutex
	runs map[string]*run
}

func newRegistry() *registry {
	return &registry{runs: make(map[string]*run)}
}

// begin marks id active with its cancel func and done channel. It returns
// false, without changing anything, if id is already active.
func (g *registry) begin(id string, cancel context.CancelFunc, done chan struct{}) bool {
	g.mu.Lock()
	defer g.mu.Unlock()
	r, ok := g.runs[id]
	if ok && r.cancel != nil {
		return false
	}
	if !ok {
		r = &run{}
		g.runs[id] = r
	}
	r.status = statusRunning
	r.result = runtime.RunResult{}
	r.err = ""
	r.cancel = cancel
	r.done = done
	return true
}

// finish records the outcome of an active run and marks it inactive. A run
// stopped by cancellation is recorded as cancelled, not failed.
func (g *registry) finish(id string, res runtime.RunResult, err error) {
	g.mu.Lock()
	defer g.mu.Unlock()
	r, ok := g.runs[id]
	if !ok {
		r = &run{}
		g.runs[id] = r
	}
	r.cancel = nil
	r.done = nil
	switch {
	case err == nil:
		r.status = statusCompleted
		r.result = res
	case errors.Is(err, runtime.ErrCancelled):
		r.status = statusCancelled
	default:
		r.status = statusFailed
		r.err = err.Error()
	}
}

// cancel requests cancellation of an active run. It reports whether the run
// was active.
func (g *registry) cancel(id string) bool {
	g.mu.Lock()
	defer g.mu.Unlock()
	r, ok := g.runs[id]
	if !ok || r.cancel == nil {
		return false
	}
	r.cancel()
	return true
}

// snapshot returns a copy of id's record, if this process has one.
func (g *registry) snapshot(id string) (run, bool) {
	g.mu.Lock()
	defer g.mu.Unlock()
	r, ok := g.runs[id]
	if !ok {
		return run{}, false
	}
	return *r, true
}

// cancelAll cancels every active run and returns the done channels of the
// runs that were active.
func (g *registry) cancelAll() []chan struct{} {
	g.mu.Lock()
	defer g.mu.Unlock()
	var dones []chan struct{}
	for _, r := range g.runs {
		if r.cancel != nil {
			r.cancel()
			dones = append(dones, r.done)
		}
	}
	return dones
}

func (g *registry) activeCount() int {
	g.mu.Lock()
	defer g.mu.Unlock()
	n := 0
	for _, r := range g.runs {
		if r.cancel != nil {
			n++
		}
	}
	return n
}
