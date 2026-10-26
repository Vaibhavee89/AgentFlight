package mcp

import (
	"context"
	"encoding/json"
	"errors"
	stdruntime "runtime"
	"testing"
	"time"

	"github.com/Vaibhavee89/agentflight/internal/policy"
	"github.com/Vaibhavee89/agentflight/internal/provider"
	"github.com/Vaibhavee89/agentflight/internal/runtime"
	"github.com/Vaibhavee89/agentflight/internal/tools"
)

func TestRuntime_AgentLoopCallsMCPToolThroughPolicy(t *testing.T) {
	fs := newFakeServer(t)
	reg := registryWithFake(t, fs)

	scripted := provider.NewMockProvider([]provider.ModelResponse{
		{ToolCalls: []tools.ToolCall{{Name: "echo", Input: json.RawMessage(`{"text":"hi there"}`)}}},
		{Content: "the server said: hi there"},
	}, nil)

	pol := policy.NewDefaultPolicy([]string{"echo"}, policy.WithMaxAttempts(2))
	rt := runtime.NewRuntime(scripted, reg, nil, runtime.WithPolicy(pol), runtime.WithMaxSteps(4))

	res, err := rt.Run(context.Background(), "mcp-loop", "say hi")
	if err != nil {
		t.Fatalf("Run() error: %v", err)
	}
	if res.Content != "the server said: hi there" || res.Steps != 2 {
		t.Fatalf("Run() = %+v, want content from the MCP tool and 2 steps", res)
	}
}

func TestRuntime_MCPTimeoutsAreRetriedByPolicy(t *testing.T) {
	fs := newFakeServer(t)
	reg := registryWithFake(t, fs)

	scripted := provider.NewMockProvider([]provider.ModelResponse{
		{ToolCalls: []tools.ToolCall{{Name: "hang", Input: json.RawMessage(`{}`)}}},
		{Content: "gave up on hang"},
	}, nil)

	pol := policy.NewDefaultPolicy([]string{"hang"},
		policy.WithMaxAttempts(3),
		policy.WithTimeout(20*time.Millisecond),
		policy.WithBackoff(time.Millisecond, 2*time.Millisecond),
	)
	rt := runtime.NewRuntime(scripted, reg, nil, runtime.WithPolicy(pol), runtime.WithMaxSteps(3))

	res, err := rt.Run(context.Background(), "mcp-timeout", "hang please")
	if err != nil {
		t.Fatalf("Run() error: %v", err)
	}
	if res.Content != "gave up on hang" {
		t.Fatalf("Content = %q", res.Content)
	}
	if got := pol.Retries(); got != 2 {
		t.Fatalf("policy retries = %d, want 2 (three attempts, each timing out)", got)
	}
}

// TestRuntime_CancelDuringMCPCallStopsAndLeaksNothing cancels a run while its
// MCP tool call is in flight. The run must return ErrCancelled promptly, and
// the goroutines it started must all exit. Run with -race.
func TestRuntime_CancelDuringMCPCallStopsAndLeaksNothing(t *testing.T) {
	fs := newFakeServer(t)
	reg := registryWithFake(t, fs)

	scripted := provider.NewMockProvider([]provider.ModelResponse{
		{ToolCalls: []tools.ToolCall{{Name: "hang", Input: json.RawMessage(`{}`)}}},
		{Content: "unreachable"},
	}, nil)
	rt := runtime.NewRuntime(scripted, reg, nil, runtime.WithMaxSteps(3))

	before := stdruntime.NumGoroutine()

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	time.AfterFunc(30*time.Millisecond, cancel)

	start := time.Now()
	_, err := rt.Run(ctx, "mcp-cancel", "hang please")
	elapsed := time.Since(start)

	if !errors.Is(err, runtime.ErrCancelled) {
		t.Fatalf("Run() error = %v, want wrapping %v", err, runtime.ErrCancelled)
	}
	if elapsed > 2*time.Second {
		t.Fatalf("Run() took %v to stop after cancellation", elapsed)
	}

	deadline := time.Now().Add(3 * time.Second)
	for stdruntime.NumGoroutine() > before && time.Now().Before(deadline) {
		time.Sleep(10 * time.Millisecond)
	}
	if after := stdruntime.NumGoroutine(); after > before {
		t.Fatalf("goroutines: %d before run, %d after cancellation", before, after)
	}
}
