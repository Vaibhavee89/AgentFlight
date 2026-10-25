package recovery

import (
	"context"
	"testing"

	"github.com/Vaibhavee89/agentflight/internal/provider"
)

// memoryStore is a minimal in-memory Store, used here only to exercise the
// Store contract against table-driven cases.
type memoryStore struct {
	checkpoints map[string]Checkpoint
}

func (m *memoryStore) SaveCheckpoint(ctx context.Context, cp Checkpoint) error {
	if m.checkpoints == nil {
		m.checkpoints = make(map[string]Checkpoint)
	}
	m.checkpoints[cp.RunID] = cp
	return nil
}

func (m *memoryStore) LatestCheckpoint(ctx context.Context, runID string) (Checkpoint, bool, error) {
	cp, ok := m.checkpoints[runID]
	return cp, ok, nil
}

func TestStore_SaveAndLatestCheckpoint(t *testing.T) {
	tests := []struct {
		name      string
		save      []Checkpoint
		lookupRun string
		wantFound bool
		wantStep  int
	}{
		{
			name:      "no checkpoint saved for run",
			lookupRun: "run-missing",
			wantFound: false,
		},
		{
			name: "returns the checkpoint saved for that run",
			save: []Checkpoint{
				{RunID: "run-1", Step: 3, Request: provider.Request{Prompt: "hi"}},
			},
			lookupRun: "run-1",
			wantFound: true,
			wantStep:  3,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			store := &memoryStore{}
			ctx := context.Background()
			for _, cp := range tt.save {
				if err := store.SaveCheckpoint(ctx, cp); err != nil {
					t.Fatalf("SaveCheckpoint() error: %v", err)
				}
			}

			got, found, err := store.LatestCheckpoint(ctx, tt.lookupRun)
			if err != nil {
				t.Fatalf("LatestCheckpoint() error: %v", err)
			}
			if found != tt.wantFound {
				t.Fatalf("LatestCheckpoint() found = %v, want %v", found, tt.wantFound)
			}
			if tt.wantFound && got.Step != tt.wantStep {
				t.Fatalf("LatestCheckpoint().Step = %d, want %d", got.Step, tt.wantStep)
			}
		})
	}
}
