package recovery

import (
	"context"
	"time"

	"github.com/Vaibhavee89/agentflight/internal/provider"
)

// Checkpoint captures enough state to resume a run: the step about to be
// attempted next, and the request (messages so far) to send for it.
type Checkpoint struct {
	RunID   string
	Step    int
	Request provider.Request
	SavedAt time.Time
}

// Store persists and retrieves the latest checkpoint for a run.
type Store interface {
	SaveCheckpoint(ctx context.Context, cp Checkpoint) error
	LatestCheckpoint(ctx context.Context, runID string) (Checkpoint, bool, error)
}
