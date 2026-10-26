package recorder

import (
	"context"
	"encoding/json"
	"fmt"
	"sync"
	"time"
)

// Store persists a single event, durably and in order.
type Store interface {
	Append(ctx context.Context, event Event) error
}

// SeqStore is optionally implemented by a Store that can report the highest
// seq already persisted for a run. A Recorder consults it the first time it
// sees a run, so a recorder created after a restart continues the run's
// sequence instead of colliding with events already stored.
type SeqStore interface {
	LastSeq(ctx context.Context, runID string) (int64, error)
}

// Recorder assigns each run a monotonically increasing sequence number,
// stamps every event with CurrentVersion and a timestamp, and writes
// through to a Store. It is safe for concurrent use, including concurrent
// Record calls for the same run from multiple goroutines.
type Recorder struct {
	store Store
	clock func() time.Time

	mu   sync.Mutex
	next map[string]int64
}

// Option configures a Recorder.
type Option func(*Recorder)

// WithClock overrides the clock used to stamp events, for deterministic tests.
func WithClock(clock func() time.Time) Option {
	return func(r *Recorder) { r.clock = clock }
}

// NewRecorder builds a Recorder that writes through to store.
func NewRecorder(store Store, opts ...Option) *Recorder {
	r := &Recorder{
		store: store,
		clock: time.Now,
		next:  make(map[string]int64),
	}
	for _, opt := range opts {
		opt(r)
	}
	return r
}

// Record encodes payload as JSON and appends a new event of type eventType
// for runID, stamped with CurrentVersion, the current time, and a seq one
// greater than the last seq recorded for that run.
func (r *Recorder) Record(ctx context.Context, runID string, eventType EventType, payload any) error {
	data, err := json.Marshal(payload)
	if err != nil {
		return fmt.Errorf("recorder: encode payload for %s: %w", eventType, err)
	}

	seq, err := r.nextSeq(ctx, runID)
	if err != nil {
		return fmt.Errorf("recorder: seq for run %s: %w", runID, err)
	}

	event := Event{
		RunID:   runID,
		Seq:     seq,
		Version: CurrentVersion,
		Type:    eventType,
		Time:    r.clock(),
		Payload: data,
	}

	if err := r.store.Append(ctx, event); err != nil {
		return fmt.Errorf("recorder: append run %s seq %d: %w", runID, event.Seq, err)
	}
	return nil
}

// nextSeq returns the next sequence number for runID, incrementing its
// counter under the Recorder's mutex so concurrent callers never see gaps
// or duplicates. The first call for a run seeds the counter from the store
// when the store implements SeqStore.
func (r *Recorder) nextSeq(ctx context.Context, runID string) (int64, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	last, seen := r.next[runID]
	if !seen {
		if src, ok := r.store.(SeqStore); ok {
			persisted, err := src.LastSeq(ctx, runID)
			if err != nil {
				return 0, err
			}
			last = persisted
		}
	}
	seq := last + 1
	r.next[runID] = seq
	return seq, nil
}
