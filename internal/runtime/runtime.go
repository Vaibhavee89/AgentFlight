package runtime

import (
	"context"
	"errors"
	"fmt"
	"log/slog"

	"github.com/Vaibhavee89/agentflight/internal/policy"
	"github.com/Vaibhavee89/agentflight/internal/provider"
	"github.com/Vaibhavee89/agentflight/internal/recorder"
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

// Recorder persists one typed event for a run.
type Recorder interface {
	Record(ctx context.Context, runID string, eventType recorder.EventType, payload any) error
}

// policyExecutor is implemented by policies that can run a tool call under
// their own gating, retry, and per-tool timeout rules (for example,
// *policy.DefaultPolicy). Policies that don't implement it still get
// Before-gating from Runtime, just without retries.
type policyExecutor interface {
	Execute(ctx context.Context, exec policy.ToolExecutor, call tools.ToolCall) (tools.ToolOutput, error)
}

// RunResult is the terminal output of a completed run.
type RunResult struct {
	RunID   string
	Content string
	Steps   int
}

type runStartedPayload struct {
	Prompt string `json:"prompt"`
}

type runErrorPayload struct {
	Step    int    `json:"step"`
	Message string `json:"message"`
}

type toolResultPayload struct {
	Name   string           `json:"name"`
	Output tools.ToolOutput `json:"output"`
	Error  string           `json:"error,omitempty"`
}

type policyDecisionPayload struct {
	Tool    string `json:"tool"`
	Allowed bool   `json:"allowed"`
	Reason  string `json:"reason,omitempty"`
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
// requested tool calls via a ToolExecutor, records every request, response,
// tool call/result, policy decision, error, and run start/finish through a
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
	if err := r.record(ctx, runID, recorder.EventRunStarted, runStartedPayload{Prompt: prompt}); err != nil {
		return RunResult{}, err
	}

	req := provider.Request{Prompt: prompt}

	for step := 0; ; step++ {
		if step >= r.maxSteps {
			err := fmt.Errorf("run %s: %w", runID, ErrMaxSteps)
			r.recordError(ctx, runID, step, err)
			return RunResult{}, err
		}
		if err := ctx.Err(); err != nil {
			wrapped := fmt.Errorf("run %s: step %d: %w: %w", runID, step, ErrCancelled, err)
			r.recordError(ctx, runID, step, wrapped)
			return RunResult{}, wrapped
		}

		if err := r.record(ctx, runID, recorder.EventModelRequest, req); err != nil {
			return RunResult{}, err
		}

		resp, err := r.provider.Complete(ctx, req)
		if err != nil {
			wrapped := fmt.Errorf("run %s: step %d: provider: %w", runID, step, err)
			r.recordError(ctx, runID, step, wrapped)
			return RunResult{}, wrapped
		}

		if err := r.record(ctx, runID, recorder.EventModelResponse, resp); err != nil {
			return RunResult{}, err
		}

		toolResults := make([]provider.ToolResult, 0, len(resp.ToolCalls))
		for _, call := range resp.ToolCalls {
			if err := ctx.Err(); err != nil {
				wrapped := fmt.Errorf("run %s: step %d: %w: %w", runID, step, ErrCancelled, err)
				r.recordError(ctx, runID, step, wrapped)
				return RunResult{}, wrapped
			}

			if err := r.record(ctx, runID, recorder.EventToolCall, call); err != nil {
				return RunResult{}, err
			}

			output, execErr := r.executeTool(ctx, call)

			if err := r.recordPolicyDecision(ctx, runID, call, execErr); err != nil {
				return RunResult{}, err
			}

			resultPayload := toolResultPayload{Name: call.Name, Output: output}
			if execErr != nil {
				resultPayload.Error = execErr.Error()
			}
			if err := r.record(ctx, runID, recorder.EventToolResult, resultPayload); err != nil {
				return RunResult{}, err
			}

			toolResults = append(toolResults, provider.ToolResult{Call: call, Output: output, Err: execErr})
		}

		r.logger.DebugContext(ctx, "runtime step", "run_id", runID, "step", step, "tool_calls", len(resp.ToolCalls))

		if len(resp.ToolCalls) == 0 {
			result := RunResult{RunID: runID, Content: resp.Content, Steps: step + 1}
			if err := r.record(ctx, runID, recorder.EventRunFinished, result); err != nil {
				return RunResult{}, err
			}
			return result, nil
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

// record is a no-op when no Recorder is configured; otherwise it records
// eventType/payload for runID and wraps any failure as a fatal run error.
func (r *Runtime) record(ctx context.Context, runID string, eventType recorder.EventType, payload any) error {
	if r.recorder == nil {
		return nil
	}
	if err := r.recorder.Record(ctx, runID, eventType, payload); err != nil {
		return fmt.Errorf("run %s: record %s: %w", runID, eventType, err)
	}
	return nil
}

// recordPolicyDecision records whether the configured policy allowed call,
// based on whether execErr wraps policy.ErrPolicyDenied. It is a no-op when
// no policy is configured.
func (r *Runtime) recordPolicyDecision(ctx context.Context, runID string, call tools.ToolCall, execErr error) error {
	if r.policy == nil {
		return nil
	}
	denied := errors.Is(execErr, policy.ErrPolicyDenied)
	payload := policyDecisionPayload{Tool: call.Name, Allowed: !denied}
	if denied {
		payload.Reason = execErr.Error()
	}
	return r.record(ctx, runID, recorder.EventPolicyDecision, payload)
}

// recordError best-effort records a terminal run error. Failures to record
// are logged rather than returned, so they never mask the original error.
func (r *Runtime) recordError(ctx context.Context, runID string, step int, cause error) {
	if r.recorder == nil {
		return
	}
	payload := runErrorPayload{Step: step, Message: cause.Error()}
	if err := r.recorder.Record(ctx, runID, recorder.EventError, payload); err != nil {
		r.logger.ErrorContext(ctx, "failed to record error event", "run_id", runID, "step", step, "error", err)
	}
}
