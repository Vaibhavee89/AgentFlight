package runtime_test

import (
	"context"
	"encoding/json"
	"sync"
	"testing"

	"github.com/Vaibhavee89/agentflight/internal/provider"
	"github.com/Vaibhavee89/agentflight/internal/recorder"
	"github.com/Vaibhavee89/agentflight/internal/runtime"
	"github.com/Vaibhavee89/agentflight/internal/tools"
	"github.com/Vaibhavee89/agentflight/internal/tools/builtin"
)

type discardStore struct {
	mu sync.Mutex
	n  int
}

func (s *discardStore) Append(ctx context.Context, e recorder.Event) error {
	s.mu.Lock()
	s.n++
	s.mu.Unlock()
	return nil
}

// stepScript is a three-step agent: each of the first two steps makes one
// tool call, and the third finishes with content.
func stepScript() []provider.ModelResponse {
	call := tools.ToolCall{Name: "echo", Input: json.RawMessage(`{"text":"hi"}`)}
	return []provider.ModelResponse{
		{ToolCalls: []tools.ToolCall{call}},
		{ToolCalls: []tools.ToolCall{call}},
		{Content: "done"},
	}
}

// BenchmarkRuntimeStep measures one full three-step run with recording off
// (no Recorder) versus on (a Recorder writing to an in-memory store). The
// difference is the per-run recording overhead; ReportMetric also divides
// it by steps so the cost reads per step.
func BenchmarkRuntimeStep(b *testing.B) {
	const steps = 3

	run := func(b *testing.B, withRecording bool) {
		reg := tools.NewRegistry()
		if err := reg.Register(builtin.NewEcho()); err != nil {
			b.Fatalf("register echo: %v", err)
		}
		ctx := context.Background()
		b.ReportAllocs()
		b.ResetTimer()
		for i := 0; i < b.N; i++ {
			var rec runtime.Recorder
			if withRecording {
				rec = recorder.NewRecorder(&discardStore{})
			}
			p := provider.NewMockProvider(stepScript(), nil)
			opts := []runtime.Option{runtime.WithMaxSteps(steps + 1)}
			rt := runtime.NewRuntime(p, reg, rec, opts...)
			if _, err := rt.Run(ctx, "bench-run", "prompt"); err != nil {
				b.Fatalf("Run: %v", err)
			}
		}
		b.StopTimer()
		perRun := float64(b.Elapsed().Nanoseconds()) / float64(b.N)
		b.ReportMetric(perRun/steps, "ns/step")
	}

	b.Run("recording=off", func(b *testing.B) { run(b, false) })
	b.Run("recording=on", func(b *testing.B) { run(b, true) })
}
