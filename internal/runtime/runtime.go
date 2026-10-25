package runtime

import (
	"context"
	"fmt"
	"log/slog"

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

// Recorder records a single step event for a run.
type Recorder interface {
	Record(ctx context.Context, runID string, event Event) error
}

// Event is a single recorded step of a run.
type Event struct {
	Step        int
	Request     provider.Request
	Response    provider.ModelResponse
	ToolResults []provider.ToolResult
}

// RunResult is the terminal output of a completed run.
type RunResult struct {
	RunID   string
	Content string
	Steps   int
}

// Option configures a Runtime.
type Option func(*Runtime)

// WithMaxSteps sets the maximum number of provider steps a run may take
// before it returns ErrMaxSteps. The default is 10.
func WithMaxSteps(n int) Option {
	return func(r *Runtime) { r.maxSteps = n }
}

// WithLogger sets the logger a Runtime uses for step-level diagnostics.
func WithLogger(l *slog.Logger) Option {
	return func(r *Runtime) { r.logger = l }
}

// Runtime executes agent runs: it calls a ModelProvider, executes any
// requested tool calls via a ToolExecutor, records each step through a
// Recorder, and repeats until the provider stops requesting tools or a limit
// is reached.
type Runtime struct {
	provider ModelProvider
	tools    ToolExecutor
	recorder Recorder
	maxSteps int
	logger   *slog.Logger
}

// NewRuntime builds a Runtime from its required collaborators and options.
func NewRuntime(p ModelProvider, t ToolExecutor, rec Recorder, opts ...Option) *Runtime {
	r := &Runtime{
		provider: p,
		tools:    t,
		recorder: rec,
		maxSteps: 10,
		logger:   slog.Default(),
	}
	for _, opt := range opts {
		opt(r)
	}
	return r
}

// Run drives a single agent run from prompt to a finished response: it
// alternates between asking the provider for the next step and executing any
// tool calls the provider requests, until the provider returns a step with
// no tool calls (success), the step limit is reached (ErrMaxSteps), or ctx
// is cancelled (ErrCancelled).
func (r *Runtime) Run(ctx context.Context, runID string, prompt string) (RunResult, error) {
	req := provider.Request{Prompt: prompt}

	for step := 0; ; step++ {
		if step >= r.maxSteps {
			return RunResult{}, fmt.Errorf("run %s: %w", runID, ErrMaxSteps)
		}
		if err := ctx.Err(); err != nil {
			return RunResult{}, fmt.Errorf("run %s: step %d: %w: %w", runID, step, ErrCancelled, err)
		}

		resp, err := r.provider.Complete(ctx, req)
		if err != nil {
			return RunResult{}, fmt.Errorf("run %s: step %d: provider: %w", runID, step, err)
		}

		toolResults := make([]provider.ToolResult, 0, len(resp.ToolCalls))
		for _, call := range resp.ToolCalls {
			if err := ctx.Err(); err != nil {
				return RunResult{}, fmt.Errorf("run %s: step %d: %w: %w", runID, step, ErrCancelled, err)
			}
			output, execErr := r.tools.Execute(ctx, call)
			toolResults = append(toolResults, provider.ToolResult{Call: call, Output: output, Err: execErr})
		}

		if r.recorder != nil {
			event := Event{Step: step, Request: req, Response: resp, ToolResults: toolResults}
			if err := r.recorder.Record(ctx, runID, event); err != nil {
				return RunResult{}, fmt.Errorf("run %s: step %d: record: %w", runID, step, err)
			}
		}

		r.logger.DebugContext(ctx, "runtime step", "run_id", runID, "step", step, "tool_calls", len(resp.ToolCalls))

		if len(resp.ToolCalls) == 0 {
			return RunResult{RunID: runID, Content: resp.Content, Steps: step + 1}, nil
		}

		req = provider.Request{Prompt: prompt, ToolResults: toolResults}
	}
}
