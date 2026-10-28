package runtime

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/Vaibhavee89/agentflight/internal/policy"
	"github.com/Vaibhavee89/agentflight/internal/provider"
	"github.com/Vaibhavee89/agentflight/internal/recorder"
	"github.com/Vaibhavee89/agentflight/internal/tools"
)

type flakyTool struct{ failures int }

func (f *flakyTool) Execute(ctx context.Context, call tools.ToolCall) (tools.ToolOutput, error) {
	if f.failures > 0 {
		f.failures--
		return tools.ToolOutput{}, policy.ErrToolTimeout
	}
	return tools.ToolOutput{Result: "done"}, nil
}

// Each retry must appear in the event log with its attempt number and backoff,
// before the tool_result it leads to, so a timeline can show it.
func TestRetriesAreRecordedWithAttemptAndBackoff(t *testing.T) {
	backend := &memoryBackend{}
	rec := recorder.NewRecorder(backend)
	p := provider.NewMockProvider([]provider.ModelResponse{
		{ToolCalls: []tools.ToolCall{{Name: "work", Input: json.RawMessage(`{}`)}}},
		{Content: "finished"},
	}, nil)
	pol := policy.NewDefaultPolicy([]string{"work"},
		policy.WithMaxAttempts(3),
		policy.WithBackoff(3*time.Millisecond, 5*time.Millisecond),
	)
	rt := NewRuntime(p, &flakyTool{failures: 2}, rec, WithPolicy(pol), WithMaxSteps(3))

	if _, err := rt.Run(context.Background(), "retry-events", "go"); err != nil {
		t.Fatalf("Run() error: %v", err)
	}

	events, err := backend.Events(context.Background(), "retry-events")
	if err != nil {
		t.Fatalf("Events: %v", err)
	}
	var retries []map[string]any
	resultSeq, lastRetrySeq := int64(-1), int64(-1)
	for _, e := range events {
		switch e.Type {
		case recorder.EventPolicyDecision:
			var payload map[string]any
			if err := json.Unmarshal(e.Payload, &payload); err != nil {
				t.Fatalf("decode policy_decision: %v", err)
			}
			if payload["retry"] == true {
				retries = append(retries, payload)
				lastRetrySeq = e.Seq
			}
		case recorder.EventToolResult:
			resultSeq = e.Seq
		}
	}
	if len(retries) != 2 {
		t.Fatalf("got %d retry events, want 2: %+v", len(retries), retries)
	}
	for i, r := range retries {
		if int(r["attempt"].(float64)) != i+1 {
			t.Fatalf("retry %d attempt = %v, want %d", i, r["attempt"], i+1)
		}
		if _, ok := r["backoff_ms"]; !ok {
			t.Fatalf("retry %d has no backoff_ms: %+v", i, r)
		}
	}
	if lastRetrySeq >= resultSeq {
		t.Fatalf("retry seq %d is not before its tool_result seq %d", lastRetrySeq, resultSeq)
	}
}
