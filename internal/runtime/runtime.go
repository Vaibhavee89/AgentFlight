package runtime

import (
	"context"
	"fmt"
	"log/slog"

	"github.com/Vaibhavee89/agentflight/internal/policy"
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

// policyExecutor is implemented by policies that can run a tool call under
// their own gating, retry, and per-tool timeout rules (for example,
// *policy.DefaultPolicy). Policies that don't implement it still get
// Before-gating from Runtime, just without retries.
type policyExecutor interface {
	Execute(ctx context.Context, exec policy.ToolExecutor, call tools.ToolCall) (tools.ToolOutput, error)
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

// WithPolicy sets the policy used to gate every tool call. If p also
// implements the retry/timeout capability provided by *policy.DefaultPolicy,
// Runtime uses it for retries too; otherwise tool calls are only gated.
func WithPolicy(p policy.Policy) Option {
	return func(r *Runtime) { r.policy = p }
}

// Runtime executes agent runs: it calls a ModelProvider, executes any
// requested tool calls via a ToolExecutor, records each step through a
// Recorder, and repeats until the provider stops requesting tools or a limit
// is reached.
type Runtime struct {
	provider ModelProvider
	tools    ToolExecutor
	recorder Recorder
	policy   policy.Policy
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
			output, execErr := r.executeTool(ctx, call)
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

// executeTool runs call, applying the configured policy if any. A policy
// that implements policyExecutor (such as *policy.DefaultPolicy) handles its
// own gating, retries, and per-tool timeout; any other Policy only gates the
// call via Before before Runtime executes it once.
func (r *Runtime) executeTool(ctx context.Context, call tools.ToolCall) (tools.ToolOutput, error) {
	if r.policy == nil {
		return r.tools.Execute(ctx, call)
	}
	if retryer, ok := r.policy.(policyExecutor); ok {
		return retryer.Execute(ctx, r.tools, call)
	}
	if err := r.policy.Before(ctx, call); err != nil {
		return tools.ToolOutput{}, err
	}
	return r.tools.Execute(ctx, call)
}
