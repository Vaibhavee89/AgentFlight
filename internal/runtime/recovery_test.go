package runtime

import (
	"context"
	"encoding/json"
	"errors"
	stdruntime "runtime"
	"sync"
	"testing"
	"time"

	"github.com/Vaibhavee89/agentflight/internal/provider"
	"github.com/Vaibhavee89/agentflight/internal/recorder"
	"github.com/Vaibhavee89/agentflight/internal/recovery"
	"github.com/Vaibhavee89/agentflight/internal/tools"
	"github.com/Vaibhavee89/agentflight/internal/tools/builtin"
)

// memoryBackend backs a Recorder (via Append), Runtime's EventReader (via
// Events), and Runtime's checkpoint Store (via SaveCheckpoint/
// LatestCheckpoint) with an in-memory, concurrency-safe implementation.
type memoryBackend struct {
	mu          sync.Mutex
	events      []recorder.Event
	checkpoints map[string][]recovery.Checkpoint
}

func (b *memoryBackend) Append(ctx context.Context, event recorder.Event) error {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.events = append(b.events, event)
	return nil
}

func (b *memoryBackend) Events(ctx context.Context, runID string) ([]recorder.Event, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	var out []recorder.Event
	for _, e := range b.events {
		if e.RunID == runID {
			out = append(out, e)
		}
	}
	return out, nil
}

func (b *memoryBackend) SaveCheckpoint(ctx context.Context, cp recovery.Checkpoint) error {
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.checkpoints == nil {
		b.checkpoints = make(map[string][]recovery.Checkpoint)
	}
	b.checkpoints[cp.RunID] = append(b.checkpoints[cp.RunID], cp)
	return nil
}

func (b *memoryBackend) LatestCheckpoint(ctx context.Context, runID string) (recovery.Checkpoint, bool, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	cps := b.checkpoints[runID]
	if len(cps) == 0 {
		return recovery.Checkpoint{}, false, nil
	}
	return cps[len(cps)-1], true, nil
}

// blockingToolExecutor blocks until ctx is done, then returns ctx.Err(),
// simulating a slow tool call that properly respects cancellation. It calls
// onStart once, the first time Execute is entered, so a test can wait for
// the call to be in flight before cancelling.
type blockingToolExecutor struct {
	mu      sync.Mutex
	started bool
	onStart func()
}

func (b *blockingToolExecutor) Execute(ctx context.Context, call tools.ToolCall) (tools.ToolOutput, error) {
	b.mu.Lock()
	first := !b.started
	b.started = true
	b.mu.Unlock()
	if first && b.onStart != nil {
		b.onStart()
	}
	<-ctx.Done()
	return tools.ToolOutput{}, ctx.Err()
}

// toolExecFunc adapts a function to the ToolExecutor interface.
type toolExecFunc func(ctx context.Context, call tools.ToolCall) (tools.ToolOutput, error)

func (f toolExecFunc) Execute(ctx context.Context, call tools.ToolCall) (tools.ToolOutput, error) {
	return f(ctx, call)
}

func TestRuntime_Run_CancelMidRun_NoGoroutineLeaks(t *testing.T) {
	backend := &memoryBackend{}
	rec := recorder.NewRecorder(backend)

	ctx, cancel := context.WithCancel(context.Background())

	started := make(chan struct{})
	exec := &blockingToolExecutor{onStart: func() { close(started) }}

	responses := []provider.ModelResponse{
		{ToolCalls: []tools.ToolCall{{Name: "slow-a"}, {Name: "slow-b"}}},
	}
	p := provider.NewMockProvider(responses, nil)

	rt := NewRuntime(p, exec, rec, WithCheckpoints(backend), WithEventReader(backend))

	before := stdruntime.NumGoroutine()

	done := make(chan struct{})
	var runErr error
	go func() {
		_, runErr = rt.Run(ctx, "run-leak-check", "prompt")
		close(done)
	}()

	<-started
	cancel()
	<-done

	if !errors.Is(runErr, ErrCancelled) {
		t.Fatalf("Run() error = %v, want wrapping %v", runErr, ErrCancelled)
	}

	deadline := time.Now().Add(2 * time.Second)
	for stdruntime.NumGoroutine() > before && time.Now().Before(deadline) {
		time.Sleep(10 * time.Millisecond)
	}
	if after := stdruntime.NumGoroutine(); after > before {
		t.Fatalf("goroutine count = %d after cancel, want <= %d (no leaks)", after, before)
	}
}

func TestRuntime_Run_CancelMidRun_WritesFinalEventAndCheckpoint(t *testing.T) {
	backend := &memoryBackend{}
	rec := recorder.NewRecorder(backend)

	ctx, cancel := context.WithCancel(context.Background())

	started := make(chan struct{})
	exec := &blockingToolExecutor{onStart: func() { close(started) }}

	responses := []provider.ModelResponse{
		{ToolCalls: []tools.ToolCall{{Name: "slow"}}},
	}
	p := provider.NewMockProvider(responses, nil)

	rt := NewRuntime(p, exec, rec, WithCheckpoints(backend), WithEventReader(backend))

	done := make(chan struct{})
	var runErr error
	go func() {
		_, runErr = rt.Run(ctx, "run-final-check", "prompt")
		close(done)
	}()

	<-started
	cancel()
	<-done

	if !errors.Is(runErr, ErrCancelled) {
		t.Fatalf("Run() error = %v, want wrapping %v", runErr, ErrCancelled)
	}

	events, err := backend.Events(context.Background(), "run-final-check")
	if err != nil {
		t.Fatalf("Events() error: %v", err)
	}
	var sawCancelledFinish bool
	for _, e := range events {
		if e.Type != recorder.EventRunFinished {
			continue
		}
		var payload struct {
			Status string `json:"Status"`
		}
		if err := json.Unmarshal(e.Payload, &payload); err != nil {
			t.Fatalf("decode run_finished payload: %v", err)
		}
		if payload.Status == "cancelled" {
			sawCancelledFinish = true
		}
	}
	if !sawCancelledFinish {
		t.Fatalf("no run_finished event with status=cancelled recorded; events: %+v", events)
	}

	cp, found, err := backend.LatestCheckpoint(context.Background(), "run-final-check")
	if err != nil {
		t.Fatalf("LatestCheckpoint() error: %v", err)
	}
	if !found {
		t.Fatalf("LatestCheckpoint() found = false, want a last checkpoint saved on cancellation")
	}
	if cp.Step != 1 {
		t.Fatalf("LatestCheckpoint().Step = %d, want 1", cp.Step)
	}
}

func TestRuntime_Resume_CompletesWithoutDuplicateToolExecutions(t *testing.T) {
	backend := &memoryBackend{}
	rec := recorder.NewRecorder(backend)

	ctx, cancel := context.WithCancel(context.Background())

	var mu sync.Mutex
	var executed []string
	var cancelOnce sync.Once

	tracking := &trackingTool{
		inner: builtin.NewEcho(),
		onExec: func(call tools.ToolCall) {
			mu.Lock()
			executed = append(executed, string(call.Input))
			mu.Unlock()
			cancelOnce.Do(cancel)
		},
	}
	reg := tools.NewRegistry()
	if err := reg.Register(tracking); err != nil {
		t.Fatalf("register tracking tool: %v", err)
	}

	responses := []provider.ModelResponse{
		{ToolCalls: []tools.ToolCall{{Name: "echo", Input: json.RawMessage(`{"text":"a"}`)}}},
		{ToolCalls: []tools.ToolCall{{Name: "echo", Input: json.RawMessage(`{"text":"b"}`)}}},
		{Content: "done"},
	}
	p := provider.NewMockProvider(responses, nil)

	rt := NewRuntime(p, reg, rec, WithMaxSteps(5), WithCheckpoints(backend), WithEventReader(backend))

	_, err := rt.Run(ctx, "run-resume", "prompt")
	if !errors.Is(err, ErrCancelled) {
		t.Fatalf("Run() error = %v, want wrapping %v", err, ErrCancelled)
	}

	// The resumed run should not trigger another cancellation.
	tracking.mu.Lock()
	tracking.onExec = func(call tools.ToolCall) {
		mu.Lock()
		executed = append(executed, string(call.Input))
		mu.Unlock()
	}
	tracking.mu.Unlock()

	result, err := rt.Resume(context.Background(), "run-resume")
	if err != nil {
		t.Fatalf("Resume() error: %v", err)
	}
	if result.Content != "done" || result.Status != "completed" {
		t.Fatalf("Resume() = %+v, want Content done Status completed", result)
	}

	mu.Lock()
	defer mu.Unlock()
	if len(executed) != 2 {
		t.Fatalf("executed %v, want exactly 2 unique tool executions", executed)
	}
	seen := make(map[string]bool, len(executed))
	for _, e := range executed {
		if seen[e] {
			t.Fatalf("duplicate tool execution for input %s: executed=%v", e, executed)
		}
		seen[e] = true
	}
}

func TestRuntime_Resume_SkipsAlreadyRecordedToolCall(t *testing.T) {
	backend := &memoryBackend{}
	rec := recorder.NewRecorder(backend)
	ctx := context.Background()
	const runID = "run-idempotent"

	if err := backend.SaveCheckpoint(ctx, recovery.Checkpoint{RunID: runID, Step: 0, Request: provider.Request{Prompt: "prompt"}}); err != nil {
		t.Fatalf("seed checkpoint: %v", err)
	}
	if err := rec.Record(ctx, runID, recorder.EventToolResult, map[string]any{
		"step": 0, "index": 0, "name": "echo", "output": map[string]string{"Result": "already-done"},
	}); err != nil {
		t.Fatalf("seed tool_result: %v", err)
	}

	var executed bool
	exec := toolExecFunc(func(ctx context.Context, call tools.ToolCall) (tools.ToolOutput, error) {
		executed = true
		return tools.ToolOutput{Result: "fresh"}, nil
	})

	responses := []provider.ModelResponse{
		{ToolCalls: []tools.ToolCall{{Name: "echo"}}},
		{Content: "done"},
	}
	p := provider.NewMockProvider(responses, nil)

	rt := NewRuntime(p, exec, rec, WithMaxSteps(5), WithCheckpoints(backend), WithEventReader(backend))

	result, err := rt.Resume(ctx, runID)
	if err != nil {
		t.Fatalf("Resume() error: %v", err)
	}
	if executed {
		t.Fatalf("tool was executed again despite an already-recorded result")
	}
	if result.Content != "done" {
		t.Fatalf("Resume() = %+v, want Content done", result)
	}
}

func TestRuntime_Resume_NoCheckpoint(t *testing.T) {
	backend := &memoryBackend{}
	rec := recorder.NewRecorder(backend)
	p := provider.NewMockProvider(nil, nil)
	rt := NewRuntime(p, tools.NewRegistry(), rec, WithCheckpoints(backend), WithEventReader(backend))

	_, err := rt.Resume(context.Background(), "no-such-run")
	if !errors.Is(err, ErrNoCheckpoint) {
		t.Fatalf("Resume() error = %v, want wrapping %v", err, ErrNoCheckpoint)
	}
}

// trackingTool wraps a tools.Tool and records every call it actually
// executes (not ones skipped by idempotence), optionally invoking a hook
// after each execution.
type trackingTool struct {
	mu     sync.Mutex
	inner  tools.Tool
	onExec func(call tools.ToolCall)
}

func (t *trackingTool) Spec() tools.ToolSpec { return t.inner.Spec() }

func (t *trackingTool) Execute(ctx context.Context, call tools.ToolCall) (tools.ToolOutput, error) {
	out, err := t.inner.Execute(ctx, call)
	t.mu.Lock()
	onExec := t.onExec
	t.mu.Unlock()
	if onExec != nil {
		onExec(call)
	}
	return out, err
}
