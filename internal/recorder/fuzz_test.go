package recorder_test

import (
	"context"
	"encoding/json"
	"errors"
	"sync"
	"testing"

	"github.com/Vaibhavee89/agentflight/internal/provider"
	"github.com/Vaibhavee89/agentflight/internal/recorder"
	"github.com/Vaibhavee89/agentflight/internal/runtime"
	"github.com/Vaibhavee89/agentflight/internal/tools"
	"github.com/Vaibhavee89/agentflight/internal/tools/builtin"
)

type seedStore struct {
	mu     sync.Mutex
	events []recorder.Event
}

func (s *seedStore) Append(ctx context.Context, e recorder.Event) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.events = append(s.events, e)
	return nil
}

// realRecordedEvents runs a scripted two-step agent through the runtime and
// returns the events it actually recorded, so fuzz seeds are genuine.
func realRecordedEvents(f *testing.F) []recorder.Event {
	f.Helper()
	store := &seedStore{}
	reg := tools.NewRegistry()
	if err := reg.Register(builtin.NewEcho()); err != nil {
		f.Fatalf("register echo: %v", err)
	}
	p := provider.NewMockProvider([]provider.ModelResponse{
		{ToolCalls: []tools.ToolCall{{Name: "echo", Input: json.RawMessage(`{"text":"hi"}`)}}},
		{Content: "done"},
	}, nil)
	rt := runtime.NewRuntime(p, reg, recorder.NewRecorder(store))
	if _, err := rt.Run(context.Background(), "seed-run", "prompt"); err != nil {
		f.Fatalf("seed run: %v", err)
	}
	return store.events
}

func FuzzDecodeEvent(f *testing.F) {
	for _, e := range realRecordedEvents(f) {
		data, err := e.Encode()
		if err != nil {
			f.Fatalf("seed Encode(): %v", err)
		}
		f.Add(data)
	}
	f.Add([]byte(`not json`))
	f.Add([]byte(`{"version":2,"type":"run_started"}`))
	f.Add([]byte(`{"version":1,"type":"bogus"}`))
	f.Add([]byte(``))
	f.Add([]byte(`null`))

	f.Fuzz(func(t *testing.T, data []byte) {
		e, err := recorder.DecodeEvent(data)
		if err != nil {
			if !errors.Is(err, recorder.ErrMalformedEvent) && !errors.Is(err, recorder.ErrUnsupportedVersion) && !errors.Is(err, recorder.ErrUnknownEventType) {
				t.Fatalf("DecodeEvent returned an untyped error: %v", err)
			}
			return
		}

		if e.Version != recorder.CurrentVersion {
			t.Fatalf("accepted event with version %d, want %d", e.Version, recorder.CurrentVersion)
		}

		encoded, err := e.Encode()
		if err != nil {
			t.Fatalf("re-encode of accepted event failed: %v", err)
		}
		again, err := recorder.DecodeEvent(encoded)
		if err != nil {
			t.Fatalf("round-trip decode failed: %v", err)
		}
		if again.Type != e.Type || again.Seq != e.Seq || again.RunID != e.RunID || again.Version != e.Version {
			t.Fatalf("round-trip changed event: %+v -> %+v", e, again)
		}
	})
}
