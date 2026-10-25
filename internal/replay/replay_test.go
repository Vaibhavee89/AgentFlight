package replay

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"sync"
	"testing"

	"github.com/Vaibhavee89/agentflight/internal/provider"
	"github.com/Vaibhavee89/agentflight/internal/recorder"
	"github.com/Vaibhavee89/agentflight/internal/runtime"
	"github.com/Vaibhavee89/agentflight/internal/tools"
	"github.com/Vaibhavee89/agentflight/internal/tools/builtin"
)

type memoryStore struct {
	mu     sync.Mutex
	events []recorder.Event
}

func (m *memoryStore) Append(ctx context.Context, event recorder.Event) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.events = append(m.events, event)
	return nil
}

func (m *memoryStore) Events(ctx context.Context, runID string) ([]recorder.Event, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	var out []recorder.Event
	for _, e := range m.events {
		if e.RunID == runID {
			out = append(out, e)
		}
	}
	return out, nil
}

// tamperPayload rewrites the payload of the occurrence-th event of type
// eventType for runID, by decoding it to a generic map, applying mutate,
// and re-encoding it.
func tamperPayload(t *testing.T, store *memoryStore, runID string, eventType recorder.EventType, occurrence int, mutate func(map[string]any)) {
	t.Helper()
	store.mu.Lock()
	defer store.mu.Unlock()

	count := 0
	for i, e := range store.events {
		if e.RunID != runID || e.Type != eventType {
			continue
		}
		if count != occurrence {
			count++
			continue
		}
		var v map[string]any
		if err := json.Unmarshal(e.Payload, &v); err != nil {
			t.Fatalf("tamperPayload: unmarshal: %v", err)
		}
		mutate(v)
		data, err := json.Marshal(v)
		if err != nil {
			t.Fatalf("tamperPayload: marshal: %v", err)
		}
		store.events[i].Payload = data
		return
	}
	t.Fatalf("tamperPayload: no %s event #%d found for run %s", eventType, occurrence, runID)
}

func newEchoRegistry(t *testing.T) *tools.Registry {
	t.Helper()
	reg := tools.NewRegistry()
	if err := reg.Register(builtin.NewEcho()); err != nil {
		t.Fatalf("register echo: %v", err)
	}
	return reg
}

func TestReplay_MatchesOriginalRun(t *testing.T) {
	store := &memoryStore{}
	rec := recorder.NewRecorder(store)

	responses := []provider.ModelResponse{
		{ToolCalls: []tools.ToolCall{{Name: "echo", Input: json.RawMessage(`{"text":"hi"}`)}}},
		{Content: "done"},
	}
	p := provider.NewMockProvider(responses, nil)

	rt := runtime.NewRuntime(p, newEchoRegistry(t), rec, runtime.WithMaxSteps(5))
	original, err := rt.Run(context.Background(), "run-1", "prompt")
	if err != nil {
		t.Fatalf("Run() error: %v", err)
	}

	got, err := Replay(context.Background(), store, "run-1")
	if err != nil {
		t.Fatalf("Replay() error: %v", err)
	}
	if got != original {
		t.Fatalf("Replay() = %+v, want %+v", got, original)
	}
}

func TestReplay_PromptChangeCausesMismatch(t *testing.T) {
	store := &memoryStore{}
	rec := recorder.NewRecorder(store)

	p := provider.NewMockProvider([]provider.ModelResponse{{Content: "done"}}, nil)
	rt := runtime.NewRuntime(p, tools.NewRegistry(), rec)

	if _, err := rt.Run(context.Background(), "run-2", "original prompt"); err != nil {
		t.Fatalf("Run() error: %v", err)
	}

	tamperPayload(t, store, "run-2", recorder.EventRunStarted, 0, func(v map[string]any) {
		v["prompt"] = "a different prompt"
	})

	_, err := Replay(context.Background(), store, "run-2")
	if !errors.Is(err, runtime.ErrReplayMismatch) {
		t.Fatalf("Replay() error = %v, want wrapping %v", err, runtime.ErrReplayMismatch)
	}
}

func TestReplay_ToolOutputChangeCausesMismatch(t *testing.T) {
	store := &memoryStore{}
	rec := recorder.NewRecorder(store)

	responses := []provider.ModelResponse{
		{ToolCalls: []tools.ToolCall{{Name: "echo", Input: json.RawMessage(`{"text":"hi"}`)}}},
		{Content: "done"},
	}
	p := provider.NewMockProvider(responses, nil)

	rt := runtime.NewRuntime(p, newEchoRegistry(t), rec, runtime.WithMaxSteps(5))
	if _, err := rt.Run(context.Background(), "run-3", "prompt"); err != nil {
		t.Fatalf("Run() error: %v", err)
	}

	tamperPayload(t, store, "run-3", recorder.EventToolResult, 0, func(v map[string]any) {
		if out, ok := v["output"].(map[string]any); ok {
			out["Result"] = "tampered"
		}
	})

	_, err := Replay(context.Background(), store, "run-3")
	if !errors.Is(err, runtime.ErrReplayMismatch) {
		t.Fatalf("Replay() error = %v, want wrapping %v", err, runtime.ErrReplayMismatch)
	}
}

func TestReplayProvider_Complete_MismatchIncludesDiff(t *testing.T) {
	events := []recorder.Event{
		{Type: recorder.EventModelRequest, Payload: json.RawMessage(`{"Prompt":"hello","ToolResults":null}`)},
		{Type: recorder.EventModelResponse, Payload: json.RawMessage(`{"Content":"hi","ToolCalls":null,"Usage":{"InputTokens":0,"OutputTokens":0}}`)},
	}
	rp, err := NewReplayProvider(events)
	if err != nil {
		t.Fatalf("NewReplayProvider() error: %v", err)
	}

	_, err = rp.Complete(context.Background(), provider.Request{Prompt: "goodbye"})
	if !errors.Is(err, runtime.ErrReplayMismatch) {
		t.Fatalf("Complete() error = %v, want wrapping %v", err, runtime.ErrReplayMismatch)
	}
	if !strings.Contains(err.Error(), "hello") || !strings.Contains(err.Error(), "goodbye") {
		t.Fatalf("Complete() error = %v, want diff summary mentioning both prompts", err)
	}
}

func TestReplayTool_Execute_NameMismatch(t *testing.T) {
	events := []recorder.Event{
		{Type: recorder.EventToolResult, Payload: json.RawMessage(`{"name":"echo","output":{"Result":"hi"}}`)},
	}
	rt, err := NewReplayTool(events)
	if err != nil {
		t.Fatalf("NewReplayTool() error: %v", err)
	}

	_, err = rt.Execute(context.Background(), tools.ToolCall{Name: "calculator"})
	if !errors.Is(err, runtime.ErrReplayMismatch) {
		t.Fatalf("Execute() error = %v, want wrapping %v", err, runtime.ErrReplayMismatch)
	}
}

func TestReplayTool_Execute_Exhausted(t *testing.T) {
	rt, err := NewReplayTool(nil)
	if err != nil {
		t.Fatalf("NewReplayTool() error: %v", err)
	}

	_, err = rt.Execute(context.Background(), tools.ToolCall{Name: "echo"})
	if !errors.Is(err, ErrReplayExhausted) {
		t.Fatalf("Execute() error = %v, want wrapping %v", err, ErrReplayExhausted)
	}
}
