package baseline

import (
	"context"
	"fmt"

	"github.com/Vaibhavee89/agentflight/internal/provider"
	"github.com/Vaibhavee89/agentflight/internal/tools"
)

// ModelProvider completes one step of a conversation given the running request state.
type ModelProvider interface {
	Complete(ctx context.Context, req provider.Request) (provider.ModelResponse, error)
}

// ToolExecutor executes a single tool call.
type ToolExecutor interface {
	Execute(ctx context.Context, call tools.ToolCall) (tools.ToolOutput, error)
}

// Result is the terminal output of a run.
type Result struct {
	RunID   string
	Content string
	Steps   int
}

// Run is a deliberately naive agent loop: no recording, no retries, no
// checkpoints, no typed errors, and it aborts on the first error. It takes
// the same inputs and produces the same shape of output as runtime.Runtime,
// and serves as the control group for later experiments.
func Run(ctx context.Context, p ModelProvider, t ToolExecutor, runID string, prompt string) (Result, error) {
	req := provider.Request{Prompt: prompt}

	for step := 0; ; step++ {
		resp, err := p.Complete(ctx, req)
		if err != nil {
			return Result{}, fmt.Errorf("baseline run %s step %d: %v", runID, step, err)
		}

		if len(resp.ToolCalls) == 0 {
			return Result{RunID: runID, Content: resp.Content, Steps: step + 1}, nil
		}

		results := make([]provider.ToolResult, 0, len(resp.ToolCalls))
		for _, call := range resp.ToolCalls {
			out, err := t.Execute(ctx, call)
			if err != nil {
				return Result{}, fmt.Errorf("baseline run %s step %d tool %s: %v", runID, step, call.Name, err)
			}
			results = append(results, provider.ToolResult{Call: call, Output: out})
		}

		req = provider.Request{Prompt: prompt, ToolResults: results}
	}
}
