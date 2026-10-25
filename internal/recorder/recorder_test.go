package recorder

import (
	"context"
	"sort"
	"sync"
	"testing"
)

type memoryStore struct {
	mu     sync.Mutex
	events []Event
}

func (m *memoryStore) Append(ctx context.Context, event Event) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.events = append(m.events, event)
	return nil
}

func (m *memoryStore) snapshot() []Event {
	m.mu.Lock()
	defer m.mu.Unlock()
	out := make([]Event, len(m.events))
	copy(out, m.events)
	return out
}

func TestRecorder_Record_AssignsSequentialSeq(t *testing.T) {
	store := &memoryStore{}
	rec := NewRecorder(store)

	for i := 0; i < 5; i++ {
		if err := rec.Record(context.Background(), "run-1", EventModelRequest, map[string]int{"i": i}); err != nil {
			t.Fatalf("Record() unexpected error: %v", err)
		}
	}

	events := store.snapshot()
	if len(events) != 5 {
		t.Fatalf("got %d events, want 5", len(events))
	}
	for i, e := range events {
		if e.Seq != int64(i+1) {
			t.Fatalf("event[%d].Seq = %d, want %d", i, e.Seq, i+1)
		}
		if e.Version != CurrentVersion {
			t.Fatalf("event[%d].Version = %d, want %d", i, e.Version, CurrentVersion)
		}
		if e.Time.IsZero() {
			t.Fatalf("event[%d].Time is zero", i)
		}
	}
}

func TestRecorder_Record_SeparateRunsHaveIndependentSeq(t *testing.T) {
	store := &memoryStore{}
	rec := NewRecorder(store)

	if err := rec.Record(context.Background(), "run-a", EventRunStarted, nil); err != nil {
		t.Fatalf("Record() run-a: %v", err)
	}
	if err := rec.Record(context.Background(), "run-b", EventRunStarted, nil); err != nil {
		t.Fatalf("Record() run-b: %v", err)
	}
	if err := rec.Record(context.Background(), "run-a", EventRunFinished, nil); err != nil {
		t.Fatalf("Record() run-a: %v", err)
	}

	seqByRun := map[string][]int64{}
	for _, e := range store.snapshot() {
		seqByRun[e.RunID] = append(seqByRun[e.RunID], e.Seq)
	}
	if got := seqByRun["run-a"]; len(got) != 2 || got[0] != 1 || got[1] != 2 {
		t.Fatalf("run-a seqs = %v, want [1 2]", got)
	}
	if got := seqByRun["run-b"]; len(got) != 1 || got[0] != 1 {
		t.Fatalf("run-b seqs = %v, want [1]", got)
	}
}

func TestRecorder_Record_ConcurrentAppendsHaveNoGapsOrDuplicates(t *testing.T) {
	store := &memoryStore{}
	rec := NewRecorder(store)

	const n = 50
	var wg sync.WaitGroup
	for i := 0; i < n; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			if err := rec.Record(context.Background(), "run-stress", EventToolCall, map[string]int{"i": i}); err != nil {
				t.Errorf("Record() goroutine %d: %v", i, err)
			}
		}(i)
	}
	wg.Wait()

	events := store.snapshot()
	if len(events) != n {
		t.Fatalf("got %d events, want %d", len(events), n)
	}

	seqs := make([]int64, len(events))
	for i, e := range events {
		seqs[i] = e.Seq
	}
	sort.Slice(seqs, func(i, j int) bool { return seqs[i] < seqs[j] })

	seen := make(map[int64]bool, n)
	for _, s := range seqs {
		if seen[s] {
			t.Fatalf("duplicate seq %d", s)
		}
		seen[s] = true
	}
	for i := 0; i < n; i++ {
		want := int64(i + 1)
		if !seen[want] {
			t.Fatalf("missing seq %d (gap detected)", want)
		}
	}
}
