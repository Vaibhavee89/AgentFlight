package api

import (
	"context"
	"errors"
	"fmt"
	"testing"

	"github.com/Vaibhavee89/agentflight/internal/runtime"
)

func TestRegistry_Transitions(t *testing.T) {
	tests := []struct {
		name       string
		finishWith error
		wantStatus string
	}{
		{name: "clean finish completes", finishWith: nil, wantStatus: statusCompleted},
		{name: "cancellation is cancelled, not failed", finishWith: fmt.Errorf("x: %w", runtime.ErrCancelled), wantStatus: statusCancelled},
		{name: "other error fails", finishWith: errors.New("boom"), wantStatus: statusFailed},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			g := newRegistry()
			_, cancel := context.WithCancel(context.Background())
			defer cancel()
			if !g.begin("r", cancel, make(chan struct{})) {
				t.Fatalf("begin() = false on a fresh run")
			}
			g.finish("r", runtime.RunResult{}, tt.finishWith)
			r, ok := g.snapshot("r")
			if !ok || r.status != tt.wantStatus {
				t.Fatalf("status = %q (found=%v), want %q", r.status, ok, tt.wantStatus)
			}
		})
	}
}

func TestRegistry_BeginRejectsActiveRun(t *testing.T) {
	g := newRegistry()
	_, cancel := context.WithCancel(context.Background())
	defer cancel()
	if !g.begin("r", cancel, make(chan struct{})) {
		t.Fatalf("first begin() = false")
	}
	if g.begin("r", cancel, make(chan struct{})) {
		t.Fatalf("second begin() = true while run is active")
	}
}

func TestRegistry_CancelOnlyActiveRuns(t *testing.T) {
	g := newRegistry()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	g.begin("active", cancel, make(chan struct{}))
	g.finish("done", runtime.RunResult{}, nil)

	if !g.cancel("active") {
		t.Fatalf("cancel(active) = false")
	}
	if ctx.Err() == nil {
		t.Fatalf("cancel func was not invoked")
	}
	if g.cancel("done") {
		t.Fatalf("cancel(done) = true for a finished run")
	}
	if g.cancel("unknown") {
		t.Fatalf("cancel(unknown) = true")
	}
}
