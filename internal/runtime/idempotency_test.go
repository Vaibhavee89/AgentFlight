package runtime

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/Vaibhavee89/agentflight/internal/policy"
	"github.com/Vaibhavee89/agentflight/internal/provider"
	"github.com/Vaibhavee89/agentflight/internal/recorder"
	"github.com/Vaibhavee89/agentflight/internal/tools"
	"github.com/Vaibhavee89/agentflight/internal/tools/builtin"
)

func TestIdempotencyKey(t *testing.T) {
	base := tools.ToolCall{Name: "send_message", Input: json.RawMessage(`{"to":"a","body":"hi"}`)}
	baseKey := idempotencyKey("run-1", 2, 0, base)

	tests := []struct {
		name string
		key  string
		same bool
	}{
		{"identical inputs", idempotencyKey("run-1", 2, 0, base), true},
		{"reordered keys and extra whitespace", idempotencyKey("run-1", 2, 0, tools.ToolCall{Name: "send_message", Input: json.RawMessage(`{ "body": "hi",  "to": "a" }`)}), true},
		{"idempotency key already set is ignored", idempotencyKey("run-1", 2, 0, tools.ToolCall{Name: base.Name, Input: base.Input, IdempotencyKey: "old"}), true},
		{"different run", idempotencyKey("run-2", 2, 0, base), false},
		{"different step", idempotencyKey("run-1", 3, 0, base), false},
		{"different index", idempotencyKey("run-1", 2, 1, base), false},
		{"different tool", idempotencyKey("run-1", 2, 0, tools.ToolCall{Name: "echo", Input: base.Input}), false},
		{"different arguments", idempotencyKey("run-1", 2, 0, tools.ToolCall{Name: base.Name, Input: json.RawMessage(`{"to":"a","body":"bye"}`)}), false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := tt.key == baseKey; got != tt.same {
				t.Fatalf("key %q equal to base %q = %v, want %v", tt.key, baseKey, got, tt.same)
			}
		})
	}

	t.Run("invalid JSON input still gives a stable key", func(t *testing.T) {
		call := tools.ToolCall{Name: "x", Input: json.RawMessage(`not json`)}
		if idempotencyKey("r", 0, 0, call) != idempotencyKey("r", 0, 0, call) {
			t.Fatal("key changed between calls with the same invalid input")
		}
	})
}

// keyRecordingExecutor records the idempotency key of every call it sees. It
// fails the first failFirst calls with a retryable timeout.
type keyRecordingExecutor struct {
	mu        sync.Mutex
	keys      []string
	failFirst int
}

func (e *keyRecordingExecutor) Execute(ctx context.Context, call tools.ToolCall) (tools.ToolOutput, error) {
	e.mu.Lock()
	defer e.mu.Unlock()
	e.keys = append(e.keys, call.IdempotencyKey)
	if len(e.keys) <= e.failFirst {
		return tools.ToolOutput{}, policy.ErrToolTimeout
	}
	return tools.ToolOutput{Result: "ok"}, nil
}

func TestRuntime_ToolCallsCarryAnIdempotencyKey(t *testing.T) {
	tests := []struct {
		name      string
		failFirst int
		wantCalls int
	}{
		{name: "single attempt", failFirst: 0, wantCalls: 1},
		{name: "policy retries reuse the same key", failFirst: 2, wantCalls: 3},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			backend := &memoryBackend{}
			exec := &keyRecordingExecutor{failFirst: tt.failFirst}
			p := provider.NewMockProvider([]provider.ModelResponse{
				{ToolCalls: []tools.ToolCall{{Name: "echo", Input: json.RawMessage(`{"text":"x"}`)}}},
				{Content: "done"},
			}, nil)
			pol := policy.NewDefaultPolicy([]string{"echo"},
				policy.WithMaxAttempts(5), policy.WithBackoff(time.Millisecond, 2*time.Millisecond))
			rt := NewRuntime(p, exec, recorder.NewRecorder(backend), WithPolicy(pol))

			if _, err := rt.Run(context.Background(), "run-k", "prompt"); err != nil {
				t.Fatalf("Run() error: %v", err)
			}

			if len(exec.keys) != tt.wantCalls {
				t.Fatalf("executor saw %d calls, want %d", len(exec.keys), tt.wantCalls)
			}
			want := idempotencyKey("run-k", 0, 0, tools.ToolCall{Name: "echo", Input: json.RawMessage(`{"text":"x"}`)})
			for i, k := range exec.keys {
				if k != want {
					t.Fatalf("attempt %d key = %q, want %q", i, k, want)
				}
			}

			// The key is visible in the event log, on the tool_call event.
			events, _ := backend.Events(context.Background(), "run-k")
			var recorded string
			for _, e := range events {
				if e.Type != recorder.EventToolCall {
					continue
				}
				var payload toolCallPayload
				if err := json.Unmarshal(e.Payload, &payload); err != nil {
					t.Fatalf("decode tool_call: %v", err)
				}
				recorded = payload.IdempotencyKey
			}
			if recorded != want {
				t.Fatalf("tool_call event key = %q, want %q", recorded, want)
			}
		})
	}
}

// memoryLedger is an in-memory EffectLedger that counts every attempt.
type memoryLedger struct {
	mu       sync.Mutex
	effects  map[string]string
	attempts int
}

func (l *memoryLedger) ApplyEffect(ctx context.Context, key, tool, payload string) (bool, error) {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.attempts++
	if l.effects == nil {
		l.effects = make(map[string]string)
	}
	if _, seen := l.effects[key]; seen {
		return false, nil
	}
	l.effects[key] = payload
	return true, nil
}

func (l *memoryLedger) counts() (effects, attempts int) {
	l.mu.Lock()
	defer l.mu.Unlock()
	return len(l.effects), l.attempts
}

var errParkedTool = errors.New("parked tool released")

// parkAfterEffect runs the first call it receives to completion, so its side
// effect happens, then holds it open until release is closed and fails it. That
// is a process dying in the window after a tool acted and before the runtime
// could record the result. Calls after the first pass straight through.
type parkAfterEffect struct {
	inner   runtimeToolExecutor
	applied chan struct{}
	release chan struct{}
	once    sync.Once
}

type runtimeToolExecutor interface {
	Execute(ctx context.Context, call tools.ToolCall) (tools.ToolOutput, error)
}

func (p *parkAfterEffect) Execute(ctx context.Context, call tools.ToolCall) (tools.ToolOutput, error) {
	first := false
	p.once.Do(func() { first = true })
	if !first {
		return p.inner.Execute(ctx, call)
	}
	if _, err := p.inner.Execute(ctx, call); err != nil {
		return tools.ToolOutput{}, err
	}
	close(p.applied)
	<-p.release
	return tools.ToolOutput{}, errParkedTool
}

// TestRuntime_ResumeAfterCrashInEffectWindow_SendsOnce is the regression test
// for duplicate side effects. A tool performs its effect and the process dies
// before the result is recorded, so resume has no result and runs the call
// again. With idempotency keys the repeat is recognised and does nothing.
func TestRuntime_ResumeAfterCrashInEffectWindow_SendsOnce(t *testing.T) {
	send := func(body string) tools.ToolCall {
		return tools.ToolCall{Name: "send_message", Input: json.RawMessage(`{"to":"alice","body":"` + body + `"}`)}
	}

	tests := []struct {
		name        string
		resumedBody string
		wantEffects int
	}{
		{name: "resume asks for the same message again", resumedBody: "hello", wantEffects: 1},
		// A call the model changed its mind about is a new operation, not a repeat.
		{name: "resume asks for a different message", resumedBody: "goodbye", wantEffects: 2},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			backend := &memoryBackend{}
			rec := recorder.NewRecorder(backend)
			ledger := &memoryLedger{}
			reg := tools.NewRegistry()
			if err := reg.Register(builtin.NewSendMessage(ledger)); err != nil {
				t.Fatalf("register: %v", err)
			}
			const runID = "run-effect-window"

			// First process: sends the message, then "crashes" before recording it.
			parked := &parkAfterEffect{inner: reg, applied: make(chan struct{}), release: make(chan struct{})}
			first := NewRuntime(
				provider.NewMockProvider([]provider.ModelResponse{{ToolCalls: []tools.ToolCall{send("hello")}}}, nil),
				parked, rec, WithCheckpoints(backend), WithEventReader(backend))
			done := make(chan struct{})
			go func() {
				defer close(done)
				_, _ = first.Run(context.Background(), runID, "tell alice hello")
			}()
			<-parked.applied

			// Second process: resumes from the checkpoint and asks for the send again.
			second := NewRuntime(
				provider.NewMockProvider([]provider.ModelResponse{
					{ToolCalls: []tools.ToolCall{send(tt.resumedBody)}},
					{Content: "done"},
				}, nil),
				reg, rec, WithCheckpoints(backend), WithEventReader(backend))
			res, err := second.Resume(context.Background(), runID)
			if err != nil {
				t.Fatalf("Resume() error: %v", err)
			}
			if res.Status != "completed" {
				t.Fatalf("Resume() status = %q, want completed", res.Status)
			}

			close(parked.release)
			<-done

			effects, attempts := ledger.counts()
			if effects != tt.wantEffects {
				t.Fatalf("messages sent = %d, want %d (ledger attempts: %d)", effects, tt.wantEffects, attempts)
			}
			if attempts != 2 {
				t.Fatalf("ledger attempts = %d, want 2 (one before the crash, one on resume)", attempts)
			}
			for key := range ledger.effects {
				if !strings.HasPrefix(key, runID+":0:0:") {
					t.Fatalf("effect key %q does not start with run id, step and index", key)
				}
			}
		})
	}
}
