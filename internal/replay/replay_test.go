package replay

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/Vaibhavee89/agentflight/internal/provider"
	"github.com/Vaibhavee89/agentflight/internal/recorder"
	"github.com/Vaibhavee89/agentflight/internal/recovery"
	"github.com/Vaibhavee89/agentflight/internal/runtime"
	"github.com/Vaibhavee89/agentflight/internal/tools"
	"github.com/Vaibhavee89/agentflight/internal/tools/builtin"
)

type memoryStore struct {
	mu          sync.Mutex
	events      []recorder.Event
	checkpoints map[string]recovery.Checkpoint
}

func (m *memoryStore) SaveCheckpoint(ctx context.Context, cp recovery.Checkpoint) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.checkpoints == nil {
		m.checkpoints = make(map[string]recovery.Checkpoint)
	}
	m.checkpoints[cp.RunID] = cp
	return nil
}

func (m *memoryStore) LatestCheckpoint(ctx context.Context, runID string) (recovery.Checkpoint, bool, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	cp, ok := m.checkpoints[runID]
	return cp, ok, nil
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

func newEchoRegistry(t testing.TB) *tools.Registry {
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

func TestReplay_ToolNameChangeIsReportedWithSeq(t *testing.T) {
	store := &memoryStore{}
	rec := recorder.NewRecorder(store)

	responses := []provider.ModelResponse{
		{ToolCalls: []tools.ToolCall{{Name: "echo", Input: json.RawMessage(`{"text":"hi"}`)}}},
		{Content: "done"},
	}
	rt := runtime.NewRuntime(provider.NewMockProvider(responses, nil), newEchoRegistry(t), rec, runtime.WithMaxSteps(5))
	if _, err := rt.Run(context.Background(), "run-tool-name", "prompt"); err != nil {
		t.Fatalf("Run() error: %v", err)
	}

	var resultSeq int64
	for _, e := range store.events {
		if e.Type == recorder.EventToolResult {
			resultSeq = e.Seq
		}
	}
	tamperPayload(t, store, "run-tool-name", recorder.EventToolResult, 0, func(v map[string]any) {
		v["name"] = "calculator"
	})

	_, err := Replay(context.Background(), store, "run-tool-name")
	var mm *MismatchError
	if !errors.As(err, &mm) {
		t.Fatalf("Replay() error = %v, want *MismatchError", err)
	}
	if mm.Seq != resultSeq {
		t.Fatalf("MismatchError.Seq = %d, want the tool_result seq %d", mm.Seq, resultSeq)
	}
	if !errors.Is(err, runtime.ErrReplayMismatch) {
		t.Fatalf("Replay() error = %v, want wrapping %v", err, runtime.ErrReplayMismatch)
	}
}

// stallingProvider answers its first call with a tool request, then blocks on
// the second call until ctx is cancelled, like a run interrupted mid-way.
type stallingProvider struct {
	mu    sync.Mutex
	calls int
	first provider.ModelResponse
}

func (p *stallingProvider) Complete(ctx context.Context, req provider.Request) (provider.ModelResponse, error) {
	p.mu.Lock()
	p.calls++
	n := p.calls
	p.mu.Unlock()
	if n == 1 {
		return p.first, nil
	}
	<-ctx.Done()
	return provider.ModelResponse{}, ctx.Err()
}

// TestReplay_ResumedRunVerifies guards against replay reporting a mismatch for
// a run that was interrupted and resumed. The recorded token usage of such a
// run covers only the steps after the resume, so it never equals what a
// replay of the whole log adds up to; replay must not treat that as a
// divergence, while still catching real ones.
func TestReplay_ResumedRunVerifies(t *testing.T) {
	tests := []struct {
		name         string
		tamper       func(t *testing.T, store *memoryStore, runID string)
		wantMismatch bool
	}{
		{name: "untouched resumed run verifies"},
		{
			// Occurrence 1 is the resumed run's finish; 0 is the cancelled one.
			name: "recorded retries are not compared",
			tamper: func(t *testing.T, store *memoryStore, runID string) {
				tamperPayload(t, store, runID, recorder.EventRunFinished, 1, func(m map[string]any) { m["Retries"] = 3 })
			},
		},
		{
			name: "changed final content is still caught",
			tamper: func(t *testing.T, store *memoryStore, runID string) {
				tamperPayload(t, store, runID, recorder.EventRunFinished, 1, func(m map[string]any) { m["Content"] = "something else" })
			},
			wantMismatch: true,
		},
		{
			name: "changed tool output in a resumed run is still caught",
			tamper: func(t *testing.T, store *memoryStore, runID string) {
				tamperPayload(t, store, runID, recorder.EventToolResult, 0, func(m map[string]any) {
					m["output"] = map[string]any{"Result": "tampered"}
				})
			},
			wantMismatch: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			store := &memoryStore{}
			rec := recorder.NewRecorder(store)
			const runID = "run-resumed"

			first := provider.ModelResponse{
				ToolCalls: []tools.ToolCall{{Name: "echo", Input: json.RawMessage(`{"text":"hi"}`)}},
				Usage:     provider.Usage{InputTokens: 100, OutputTokens: 10},
			}
			ctx, cancel := context.WithCancel(context.Background())
			rt := runtime.NewRuntime(&stallingProvider{first: first}, newEchoRegistry(t), rec,
				runtime.WithCheckpoints(store), runtime.WithEventReader(store))
			done := make(chan error, 1)
			go func() {
				_, err := rt.Run(ctx, runID, "prompt")
				done <- err
			}()
			// Wait until the run has checkpointed after step 0, then interrupt it.
			for {
				if cp, ok, _ := store.LatestCheckpoint(context.Background(), runID); ok && cp.Step >= 1 {
					break
				}
				time.Sleep(time.Millisecond)
			}
			cancel()
			if err := <-done; !errors.Is(err, runtime.ErrCancelled) {
				t.Fatalf("Run() error = %v, want ErrCancelled", err)
			}

			resumed := runtime.NewRuntime(
				provider.NewMockProvider([]provider.ModelResponse{{Content: "done", Usage: provider.Usage{InputTokens: 150, OutputTokens: 5}}}, nil),
				newEchoRegistry(t), rec, runtime.WithCheckpoints(store), runtime.WithEventReader(store))
			if _, err := resumed.Resume(context.Background(), runID); err != nil {
				t.Fatalf("Resume() error: %v", err)
			}

			if tt.tamper != nil {
				tt.tamper(t, store, runID)
			}

			_, err := Replay(context.Background(), store, runID)
			var mismatch *MismatchError
			if gotMismatch := errors.As(err, &mismatch); gotMismatch != tt.wantMismatch {
				t.Fatalf("Replay() error = %v, want mismatch = %v", err, tt.wantMismatch)
			}
			if !tt.wantMismatch && err != nil {
				t.Fatalf("Replay() error: %v", err)
			}
		})
	}
}
