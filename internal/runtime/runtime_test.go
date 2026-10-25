package runtime

import (
	"context"
	"encoding/json"
	"errors"
	"sync"
	"testing"

	"github.com/Vaibhavee89/agentflight/internal/provider"
	"github.com/Vaibhavee89/agentflight/internal/tools"
	"github.com/Vaibhavee89/agentflight/internal/tools/builtin"
)

type stubRecorder struct {
	mu     sync.Mutex
	events []Event
}

func (s *stubRecorder) Record(ctx context.Context, runID string, event Event) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.events = append(s.events, event)
	return nil
}

type cancelingExecutor struct {
	cancel context.CancelFunc
	out    tools.ToolOutput
}

func (c cancelingExecutor) Execute(ctx context.Context, call tools.ToolCall) (tools.ToolOutput, error) {
	c.cancel()
	return c.out, nil
}

func newRegistry(t *testing.T) *tools.Registry {
	t.Helper()
	reg := tools.NewRegistry()
	if err := reg.Register(builtin.NewEcho()); err != nil {
		t.Fatalf("register echo: %v", err)
	}
	if err := reg.Register(builtin.NewCalculator()); err != nil {
		t.Fatalf("register calculator: %v", err)
	}
	return reg
}

func TestRuntime_Run_SingleTurn(t *testing.T) {
	p := provider.NewMockProvider([]provider.ModelResponse{{Content: "the answer"}}, nil)
	rt := NewRuntime(p, tools.NewRegistry(), nil)

	got, err := rt.Run(context.Background(), "run-1", "what is it?")
	if err != nil {
		t.Fatalf("Run() unexpected error: %v", err)
	}
	want := RunResult{RunID: "run-1", Content: "the answer", Steps: 1}
	if got != want {
		t.Fatalf("Run() = %+v, want %+v", got, want)
	}
}

func TestRuntime_Run_MultiToolCallTurn(t *testing.T) {
	responses := []provider.ModelResponse{
		{
			ToolCalls: []tools.ToolCall{
				{Name: "echo", Input: json.RawMessage(`{"text":"hi"}`)},
				{Name: "calculator", Input: json.RawMessage(`{"operation":"add","a":1,"b":2}`)},
			},
		},
		{Content: "done"},
	}
	p := provider.NewMockProvider(responses, nil)
	rec := &stubRecorder{}
	rt := NewRuntime(p, newRegistry(t), rec, WithMaxSteps(5))

	got, err := rt.Run(context.Background(), "run-2", "prompt")
	if err != nil {
		t.Fatalf("Run() unexpected error: %v", err)
	}
	want := RunResult{RunID: "run-2", Content: "done", Steps: 2}
	if got != want {
		t.Fatalf("Run() = %+v, want %+v", got, want)
	}

	if len(rec.events) != 2 {
		t.Fatalf("recorded %d events, want 2", len(rec.events))
	}
	first := rec.events[0]
	if len(first.ToolResults) != 2 {
		t.Fatalf("step 0 recorded %d tool results, want 2", len(first.ToolResults))
	}
	if first.ToolResults[0].Output.Result != "hi" {
		t.Fatalf("echo result = %q, want %q", first.ToolResults[0].Output.Result, "hi")
	}
	if first.ToolResults[1].Output.Result != "3" {
		t.Fatalf("calculator result = %q, want %q", first.ToolResults[1].Output.Result, "3")
	}
}

func TestRuntime_Run_MaxStepsHit(t *testing.T) {
	toolCall := tools.ToolCall{Name: "echo", Input: json.RawMessage(`{"text":"x"}`)}
	responses := []provider.ModelResponse{
		{ToolCalls: []tools.ToolCall{toolCall}},
		{ToolCalls: []tools.ToolCall{toolCall}},
	}
	p := provider.NewMockProvider(responses, nil)
	rt := NewRuntime(p, newRegistry(t), nil, WithMaxSteps(2))

	_, err := rt.Run(context.Background(), "run-3", "prompt")
	if !errors.Is(err, ErrMaxSteps) {
		t.Fatalf("Run() error = %v, want wrapping %v", err, ErrMaxSteps)
	}
}

func TestRuntime_Run_ContextCancelledMidRun(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	responses := []provider.ModelResponse{
		{ToolCalls: []tools.ToolCall{{Name: "noop"}}},
		{Content: "unreachable"},
	}
	p := provider.NewMockProvider(responses, nil)
	exec := cancelingExecutor{cancel: cancel}
	rt := NewRuntime(p, exec, nil)

	_, err := rt.Run(ctx, "run-4", "prompt")
	if !errors.Is(err, ErrCancelled) {
		t.Fatalf("Run() error = %v, want wrapping %v", err, ErrCancelled)
	}
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("Run() error = %v, want wrapping %v", err, context.Canceled)
	}
}
