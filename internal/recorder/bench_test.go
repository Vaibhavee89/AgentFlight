package recorder_test

import (
	"context"
	"encoding/json"
	"path/filepath"
	"sync"
	"testing"

	"github.com/Vaibhavee89/agentflight/internal/recorder"
	"github.com/Vaibhavee89/agentflight/internal/storage"
)

type nullStore struct {
	mu sync.Mutex
	n  int
}

func (s *nullStore) Append(ctx context.Context, e recorder.Event) error {
	s.mu.Lock()
	s.n++
	s.mu.Unlock()
	return nil
}

var benchPayload = map[string]any{"step": 1, "index": 0, "call": map[string]any{"Name": "echo", "Input": map[string]string{"text": "hello"}}}

func BenchmarkRecorderAppend(b *testing.B) {
	b.Run("memory", func(b *testing.B) {
		rec := recorder.NewRecorder(&nullStore{})
		ctx := context.Background()
		b.ReportAllocs()
		b.ResetTimer()
		for i := 0; i < b.N; i++ {
			if err := rec.Record(ctx, "bench-run", recorder.EventToolCall, benchPayload); err != nil {
				b.Fatalf("Record: %v", err)
			}
		}
	})

	b.Run("sqlite", func(b *testing.B) {
		store, err := storage.Open(context.Background(), filepath.Join(b.TempDir(), "bench.db"))
		if err != nil {
			b.Fatalf("storage.Open: %v", err)
		}
		b.Cleanup(func() { store.Close() })

		rec := recorder.NewRecorder(store)
		ctx := context.Background()
		b.ReportAllocs()
		b.ResetTimer()
		for i := 0; i < b.N; i++ {
			if err := rec.Record(ctx, "bench-run", recorder.EventToolCall, benchPayload); err != nil {
				b.Fatalf("Record: %v", err)
			}
		}
	})
}

// BenchmarkEncodeEvent isolates the JSON encoding cost that every Record
// call pays before touching the store.
func BenchmarkEncodeEvent(b *testing.B) {
	data, err := json.Marshal(benchPayload)
	if err != nil {
		b.Fatalf("marshal: %v", err)
	}
	e := recorder.Event{RunID: "bench-run", Seq: 1, Version: recorder.CurrentVersion, Type: recorder.EventToolCall, Payload: data}
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if _, err := e.Encode(); err != nil {
			b.Fatalf("Encode: %v", err)
		}
	}
}
