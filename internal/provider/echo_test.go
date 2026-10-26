package provider

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/Vaibhavee89/agentflight/internal/tools"
)

func TestEchoProvider_Complete(t *testing.T) {
	tests := []struct {
		name        string
		req         Request
		wantTool    bool
		wantContent string
	}{
		{name: "first step asks for an echo call", req: Request{Prompt: "hi"}, wantTool: true},
		{
			name:        "after tool result answers with it",
			req:         Request{Prompt: "hi", ToolResults: []ToolResult{{Call: tools.ToolCall{Name: "echo"}, Output: tools.ToolOutput{Result: "hi"}}}},
			wantContent: "echo: hi",
		},
	}

	p := EchoProvider{}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			resp, err := p.Complete(context.Background(), tt.req)
			if err != nil {
				t.Fatalf("Complete() error: %v", err)
			}
			if tt.wantTool != (len(resp.ToolCalls) == 1 && resp.ToolCalls[0].Name == "echo") {
				t.Fatalf("ToolCalls = %+v, want echo call=%v", resp.ToolCalls, tt.wantTool)
			}
			if resp.Content != tt.wantContent {
				t.Fatalf("Content = %q, want %q", resp.Content, tt.wantContent)
			}
		})
	}
}

func TestEchoProvider_LatencyRespectsCancellation(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	start := time.Now()
	_, err := EchoProvider{Latency: time.Hour}.Complete(ctx, Request{Prompt: "hi"})
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("Complete() error = %v, want wrapping %v", err, context.Canceled)
	}
	if time.Since(start) > time.Second {
		t.Fatalf("Complete() blocked %v after cancellation", time.Since(start))
	}
}
