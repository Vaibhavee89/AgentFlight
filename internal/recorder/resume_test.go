package recorder_test

import (
	"context"
	"path/filepath"
	"testing"

	"github.com/Vaibhavee89/agentflight/internal/recorder"
	"github.com/Vaibhavee89/agentflight/internal/storage"
)

// A recorder created after a restart must continue a run's sequence from
// what the store already holds, not restart at 1 and collide with it.
func TestRecorder_NewInstanceContinuesExistingRunSequence(t *testing.T) {
	ctx := context.Background()
	store, err := storage.Open(ctx, filepath.Join(t.TempDir(), "seq.db"))
	if err != nil {
		t.Fatalf("storage.Open: %v", err)
	}
	t.Cleanup(func() { store.Close() })

	first := recorder.NewRecorder(store)
	for i := 0; i < 3; i++ {
		if err := first.Record(ctx, "run-1", recorder.EventRunStarted, map[string]int{"i": i}); err != nil {
			t.Fatalf("first Record %d: %v", i, err)
		}
	}

	restarted := recorder.NewRecorder(store)
	if err := restarted.Record(ctx, "run-1", recorder.EventRunFinished, nil); err != nil {
		t.Fatalf("Record after restart: %v", err)
	}

	events, err := store.Events(ctx, "run-1")
	if err != nil {
		t.Fatalf("Events: %v", err)
	}
	if len(events) != 4 {
		t.Fatalf("got %d events, want 4", len(events))
	}
	for i, e := range events {
		if e.Seq != int64(i+1) {
			t.Fatalf("events[%d].Seq = %d, want %d", i, e.Seq, i+1)
		}
	}
}
