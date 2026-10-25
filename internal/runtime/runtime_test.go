package runtime

import (
	"context"
	"encoding/json"
	"errors"
	"sync"
	"testing"

	"github.com/Vaibhavee89/agentflight/internal/provider"
	"github.com/Vaibhavee89/agentflight/internal/recorder"
	"github.com/Vaibhavee89/agentflight/internal/tools"
	"github.com/Vaibhavee89/agentflight/internal/tools/builtin"
)

type recordedEvent struct {
	runID     string
	eventType recorder.EventType
	payload   any
}

type stubRecorder struct {
	mu     sync.Mutex
	events []recordedEvent
}

func (s *stubRecorder) Record(ctx context.Context, runID string, eventType recorder.EventType, payload any) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.events = append(s.events, recordedEvent{runID: runID, eventType: eventType, payload: payload})
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
	want := RunResult{RunID: "run-1", Content: "the answer", Steps: 1, Status: "completed"}
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
	want := RunResult{RunID: "run-2", Content: "done", Steps: 2, Status: "completed"}
	if got != want {
		t.Fatalf("Run() = %+v, want %+v", got, want)
	}

	// RunStarted, ModelRequest, and ModelResponse come first in order, and
	// ModelRequest, ModelResponse, RunFinished come last; the two
	// ToolCall/ToolResult pairs in between can interleave in either order,
	// since tool calls within a step run concurrently.
	if len(rec.events) != 10 {
		t.Fatalf("recorded %d events, want 10: %+v", len(rec.events), rec.events)
	}
	for _, e := range rec.events {
		if e.runID != "run-2" {
			t.Fatalf("event.runID = %q, want %q", e.runID, "run-2")
		}
	}

	wantPrefix := []recorder.EventType{recorder.EventRunStarted, recorder.EventModelRequest, recorder.EventModelResponse}
	for i, want := range wantPrefix {
		if rec.events[i].eventType != want {
			t.Fatalf("event[%d].eventType = %q, want %q", i, rec.events[i].eventType, want)
		}
	}
	wantSuffix := []recorder.EventType{recorder.EventModelRequest, recorder.EventModelResponse, recorder.EventRunFinished}
	for i, want := range wantSuffix {
		idx := len(rec.events) - len(wantSuffix) + i
		if rec.events[idx].eventType != want {
			t.Fatalf("event[%d].eventType = %q, want %q", idx, rec.events[idx].eventType, want)
		}
	}

	var calls, results int
	var echoFound, calcFound bool
	for _, e := range rec.events[3:7] {
		switch e.eventType {
		case recorder.EventToolCall:
			calls++
		case recorder.EventToolResult:
			results++
			payload, ok := e.payload.(toolResultPayload)
			if !ok {
				t.Fatalf("payload = %#v, want toolResultPayload", e.payload)
			}
			switch payload.Name {
			case "echo":
				if payload.Output.Result != "hi" {
					t.Fatalf("echo tool result = %q, want %q", payload.Output.Result, "hi")
				}
				echoFound = true
			case "calculator":
				if payload.Output.Result != "3" {
					t.Fatalf("calculator tool result = %q, want %q", payload.Output.Result, "3")
				}
				calcFound = true
			default:
				t.Fatalf("unexpected tool result for %q", payload.Name)
			}
		default:
			t.Fatalf("unexpected event type in tool-call phase: %q", e.eventType)
		}
	}
	if calls != 2 || results != 2 {
		t.Fatalf("got %d tool_call and %d tool_result events, want 2 and 2", calls, results)
	}
	if !echoFound || !calcFound {
		t.Fatalf("missing expected tool results: echo=%v calculator=%v", echoFound, calcFound)
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
