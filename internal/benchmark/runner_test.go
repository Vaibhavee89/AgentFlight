package benchmark

import (
	"context"
	"errors"
	"fmt"
	"sync/atomic"
	"testing"
	"time"

	"github.com/Vaibhavee89/agentflight/internal/provider"
	"github.com/Vaibhavee89/agentflight/internal/runtime"
	"github.com/Vaibhavee89/agentflight/internal/tools"
)

// trackingProvider counts how many Complete calls are in flight at once,
// recording the observed maximum, and holds each call open for delay.
type trackingProvider struct {
	inFlight *int32
	max      *int32
	delay    time.Duration
}

func (p trackingProvider) Complete(ctx context.Context, req provider.Request) (provider.ModelResponse, error) {
	n := atomic.AddInt32(p.inFlight, 1)
	defer atomic.AddInt32(p.inFlight, -1)

	for {
		cur := atomic.LoadInt32(p.max)
		if n <= cur {
			break
		}
		if atomic.CompareAndSwapInt32(p.max, cur, n) {
			break
		}
	}

	select {
	case <-time.After(p.delay):
	case <-ctx.Done():
		return provider.ModelResponse{}, ctx.Err()
	}
	return provider.ModelResponse{Content: "ok"}, nil
}

func TestRunner_Run_NeverExceedsConcurrency(t *testing.T) {
	const concurrency = 4
	const numTasks = 20

	var inFlight, maxInFlight int32

	tasks := make([]Task, numTasks)
	for i := range tasks {
		tasks[i] = Task{ID: fmt.Sprintf("task-%d", i), Prompt: "hi"}
	}

	newRuntime := func() *runtime.Runtime {
		p := trackingProvider{inFlight: &inFlight, max: &maxInFlight, delay: 5 * time.Millisecond}
		return runtime.NewRuntime(p, tools.NewRegistry(), nil)
	}

	runner := NewRunner(Config{Concurrency: concurrency, PerTaskTimeout: time.Second, Repeats: 1})
	results, err := runner.Run(context.Background(), tasks, newRuntime)
	if err != nil {
		t.Fatalf("Run() error: %v", err)
	}
	if len(results) != numTasks {
		t.Fatalf("got %d results, want %d", len(results), numTasks)
	}
	for i, res := range results {
		if res.Err != nil {
			t.Fatalf("results[%d] unexpected error: %v", i, res.Err)
		}
	}

	if got := atomic.LoadInt32(&maxInFlight); got > concurrency {
		t.Fatalf("max in-flight tasks = %d, want <= %d", got, concurrency)
	}
}

// reverseLatencyProvider makes earlier tasks (by index parsed from the
// prompt "task-N") take longer than later ones, so completion order is the
// reverse of input order.
type reverseLatencyProvider struct {
	base  time.Duration
	total int
}

func (p reverseLatencyProvider) Complete(ctx context.Context, req provider.Request) (provider.ModelResponse, error) {
	var idx int
	fmt.Sscanf(req.Prompt, "task-%d", &idx)
	delay := time.Duration(p.total-idx) * p.base
	select {
	case <-time.After(delay):
	case <-ctx.Done():
		return provider.ModelResponse{}, ctx.Err()
	}
	return provider.ModelResponse{Content: req.Prompt}, nil
}

func TestRunner_Run_PreservesTaskOrderRegardlessOfCompletionOrder(t *testing.T) {
	const numTasks = 8

	tasks := make([]Task, numTasks)
	for i := range tasks {
		tasks[i] = Task{ID: fmt.Sprintf("task-%d", i), Prompt: fmt.Sprintf("task-%d", i)}
	}

	newRuntime := func() *runtime.Runtime {
		p := reverseLatencyProvider{base: 5 * time.Millisecond, total: numTasks}
		return runtime.NewRuntime(p, tools.NewRegistry(), nil)
	}

	runner := NewRunner(Config{Concurrency: numTasks, PerTaskTimeout: time.Second, Repeats: 1})
	results, err := runner.Run(context.Background(), tasks, newRuntime)
	if err != nil {
		t.Fatalf("Run() error: %v", err)
	}
	if len(results) != numTasks {
		t.Fatalf("got %d results, want %d", len(results), numTasks)
	}
	for i, res := range results {
		wantID := fmt.Sprintf("task-%d", i)
		if res.TaskID != wantID {
			t.Fatalf("results[%d].TaskID = %q, want %q (output must keep input order)", i, res.TaskID, wantID)
		}
	}
}

// failOnPromptProvider fails Complete for one specific prompt and succeeds
// for every other.
type failOnPromptProvider struct {
	failPrompt string
}

func (p failOnPromptProvider) Complete(ctx context.Context, req provider.Request) (provider.ModelResponse, error) {
	if req.Prompt == p.failPrompt {
		return provider.ModelResponse{}, errors.New("boom: simulated provider failure")
	}
	return provider.ModelResponse{Content: "ok: " + req.Prompt}, nil
}

func TestRunner_Run_TaskFailureDoesNotStopSuite(t *testing.T) {
	tasks := []Task{
		{ID: "ok-1", Prompt: "fine"},
		{ID: "fails", Prompt: "boom"},
		{ID: "ok-2", Prompt: "fine"},
	}

	newRuntime := func() *runtime.Runtime {
		p := failOnPromptProvider{failPrompt: "boom"}
		return runtime.NewRuntime(p, tools.NewRegistry(), nil)
	}

	runner := NewRunner(Config{Concurrency: 1, PerTaskTimeout: time.Second, Repeats: 1})
	results, err := runner.Run(context.Background(), tasks, newRuntime)
	if err != nil {
		t.Fatalf("Run() unexpected top-level error: %v", err)
	}
	if len(results) != 3 {
		t.Fatalf("got %d results, want 3", len(results))
	}
	if results[0].Err != nil || results[0].Result.Content == "" {
		t.Fatalf("results[0] = %+v, want a successful result", results[0])
	}
	if results[1].Err == nil {
		t.Fatalf("results[1].Err = nil, want an error for the failing task")
	}
	if results[2].Err != nil || results[2].Result.Content == "" {
		t.Fatalf("results[2] = %+v, want a successful result", results[2])
	}
}

// slowFixedProvider waits delay before answering, respecting cancellation.
type slowFixedProvider struct {
	delay time.Duration
}

func (p slowFixedProvider) Complete(ctx context.Context, req provider.Request) (provider.ModelResponse, error) {
	select {
	case <-time.After(p.delay):
		return provider.ModelResponse{Content: "ok"}, nil
	case <-ctx.Done():
		return provider.ModelResponse{}, ctx.Err()
	}
}

func TestRunner_Run_ParentCancellationReturnsPartialResultsPromptly(t *testing.T) {
	const numTasks = 20
	tasks := make([]Task, numTasks)
	for i := range tasks {
		tasks[i] = Task{ID: fmt.Sprintf("task-%d", i), Prompt: "hi"}
	}

	ctx, cancel := context.WithCancel(context.Background())
	go func() {
		time.Sleep(20 * time.Millisecond)
		cancel()
	}()

	newRuntime := func() *runtime.Runtime {
		p := slowFixedProvider{delay: 200 * time.Millisecond}
		return runtime.NewRuntime(p, tools.NewRegistry(), nil)
	}

	runner := NewRunner(Config{Concurrency: 2, PerTaskTimeout: time.Second, Repeats: 1})

	start := time.Now()
	results, err := runner.Run(ctx, tasks, newRuntime)
	elapsed := time.Since(start)

	if err == nil {
		t.Fatalf("Run() expected a cancellation error, got nil")
	}
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("Run() error = %v, want wrapping %v", err, context.Canceled)
	}
	if len(results) != numTasks {
		t.Fatalf("got %d results, want %d (partial results keep the full slice length)", len(results), numTasks)
	}

	var completed int
	for _, r := range results {
		if r.TaskID != "" {
			completed++
		}
	}
	if completed >= numTasks {
		t.Fatalf("all %d tasks completed despite cancellation; test is not exercising early stop", completed)
	}
	if elapsed > 2*time.Second {
		t.Fatalf("Run() took %v after cancellation, want a prompt return", elapsed)
	}
}

func TestRunner_Run_RepeatsEachTask(t *testing.T) {
	tasks := []Task{{ID: "task-1", Prompt: "hi"}}
	newRuntime := func() *runtime.Runtime {
		p := failOnPromptProvider{failPrompt: "never"}
		return runtime.NewRuntime(p, tools.NewRegistry(), nil)
	}

	runner := NewRunner(Config{Concurrency: 2, PerTaskTimeout: time.Second, Repeats: 3})
	results, err := runner.Run(context.Background(), tasks, newRuntime)
	if err != nil {
		t.Fatalf("Run() error: %v", err)
	}
	if len(results) != 3 {
		t.Fatalf("got %d results, want 3 (one task repeated 3 times)", len(results))
	}
	seenRepeats := map[int]bool{}
	for _, res := range results {
		if res.TaskID != "task-1" {
			t.Fatalf("result.TaskID = %q, want %q", res.TaskID, "task-1")
		}
		seenRepeats[res.Repeat] = true
	}
	for rep := 0; rep < 3; rep++ {
		if !seenRepeats[rep] {
			t.Fatalf("missing repeat index %d among results: %+v", rep, results)
		}
	}
}
