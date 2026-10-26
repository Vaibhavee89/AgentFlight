package benchmark

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/Vaibhavee89/agentflight/internal/provider"
	"github.com/Vaibhavee89/agentflight/internal/runtime"
	"github.com/Vaibhavee89/agentflight/internal/tools"
)

// latencyProvider simulates a real model call's network latency.
type latencyProvider struct {
	delay time.Duration
}

func (p latencyProvider) Complete(ctx context.Context, req provider.Request) (provider.ModelResponse, error) {
	select {
	case <-time.After(p.delay):
		return provider.ModelResponse{Content: "ok"}, nil
	case <-ctx.Done():
		return provider.ModelResponse{}, ctx.Err()
	}
}

// BenchmarkRunner_Concurrency compares wall-clock throughput for a fixed
// batch of tasks, each paying a fixed 50ms simulated model latency, at
// Concurrency 1 (fully serial) versus 8 (bounded parallel). Concurrency 8
// should be close to 8x faster for a batch this size, since the latency
// dominates and nothing else contends.
func BenchmarkRunner_Concurrency(b *testing.B) {
	const numTasks = 16
	const latency = 50 * time.Millisecond

	tasks := make([]Task, numTasks)
	for i := range tasks {
		tasks[i] = Task{ID: fmt.Sprintf("task-%d", i), Prompt: "hi"}
	}

	newRuntime := func() *runtime.Runtime {
		return runtime.NewRuntime(latencyProvider{delay: latency}, tools.NewRegistry(), nil)
	}

	for _, concurrency := range []int{1, 8} {
		b.Run(fmt.Sprintf("concurrency=%d", concurrency), func(b *testing.B) {
			runner := NewRunner(Config{Concurrency: concurrency, PerTaskTimeout: 5 * time.Second, Repeats: 1})

			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				results, err := runner.Run(context.Background(), tasks, newRuntime)
				if err != nil {
					b.Fatalf("Run() error: %v", err)
				}
				if len(results) != numTasks {
					b.Fatalf("got %d results, want %d", len(results), numTasks)
				}
			}
		})
	}
}
