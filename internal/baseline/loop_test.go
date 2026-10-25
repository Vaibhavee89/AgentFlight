package baseline

import (
	"context"
	"encoding/json"
	"errors"
	"testing"

	"github.com/Vaibhavee89/agentflight/internal/provider"
	"github.com/Vaibhavee89/agentflight/internal/tools"
	"github.com/Vaibhavee89/agentflight/internal/tools/builtin"
)

func newRegistry(t *testing.T) *tools.Registry {
	t.Helper()
	reg := tools.NewRegistry()
	if err := reg.Register(builtin.NewEcho()); err != nil {
		t.Fatalf("register echo: %v", err)
	}
	return reg
}

func TestRun(t *testing.T) {
	tests := []struct {
		name      string
		responses []provider.ModelResponse
		errs      []error
		want      Result
		wantErr   bool
	}{
		{
			name:      "single turn answer",
			responses: []provider.ModelResponse{{Content: "the answer"}},
			want:      Result{RunID: "run-1", Content: "the answer", Steps: 1},
		},
		{
			name: "multi tool call turn",
			responses: []provider.ModelResponse{
				{ToolCalls: []tools.ToolCall{{Name: "echo", Input: json.RawMessage(`{"text":"hi"}`)}}},
				{Content: "done"},
			},
			want: Result{RunID: "run-1", Content: "done", Steps: 2},
		},
		{
			name:      "aborts on first provider error",
			responses: []provider.ModelResponse{{Content: "unreachable"}},
			errs:      []error{errors.New("boom")},
			wantErr:   true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			p := provider.NewMockProvider(tt.responses, tt.errs)
			got, err := Run(context.Background(), p, newRegistry(t), "run-1", "prompt")
			if tt.wantErr {
				if err == nil {
					t.Fatalf("Run() expected error, got nil")
				}
				return
			}
			if err != nil {
				t.Fatalf("Run() unexpected error: %v", err)
			}
			if got != tt.want {
				t.Fatalf("Run() = %+v, want %+v", got, tt.want)
			}
		})
	}
}
