package runtime

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"time"

	"golang.org/x/sync/errgroup"

	"github.com/Vaibhavee89/agentflight/internal/policy"
	"github.com/Vaibhavee89/agentflight/internal/provider"
	"github.com/Vaibhavee89/agentflight/internal/recorder"
	"github.com/Vaibhavee89/agentflight/internal/recovery"
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

// EventReader reads back a run's recorded events in seq order, as both
// recorder.Store and *storage.SQLiteStore do. Resume uses it to find tool
// results already recorded for the step it is about to re-attempt.
type EventReader interface {
	Events(ctx context.Context, runID string) ([]recorder.Event, error)
}

// policyExecutor is implemented by policies that can run a tool call under
// their own gating, retry, and per-tool timeout rules (for example,
// *policy.DefaultPolicy). Policies that don't implement it still get
// Before-gating from Runtime, just without retries.
type policyExecutor interface {
	Execute(ctx context.Context, exec policy.ToolExecutor, call tools.ToolCall) (tools.ToolOutput, error)
}

// retryCounter is implemented by policies that track how many retries they
// have performed (such as *policy.DefaultPolicy), used to populate
// RunResult.Retries with the count observed during one Run/Resume call.
type retryCounter interface {
	Retries() int64
}

// RunResult is the terminal output of a run: Status is "completed" for a
// normal finish or "cancelled" if ctx was cancelled mid-run. Usage and
// Retries cover only the steps executed during this Run/Resume call.
type RunResult struct {
	RunID   string
	Content string
	Steps   int
	Status  string
	Usage   provider.Usage
	Retries int
}

type runStartedPayload struct {
	Prompt string `json:"prompt"`
}

type runErrorPayload struct {
	Step    int    `json:"step"`
	Message string `json:"message"`
}

type toolCallPayload struct {
	Step  int            `json:"step"`
	Index int            `json:"index"`
	Call  tools.ToolCall `json:"call"`
}

type toolResultPayload struct {
	Step   int              `json:"step"`
	Index  int              `json:"index"`
	Name   string           `json:"name"`
	Output tools.ToolOutput `json:"output"`
	Error  string           `json:"error,omitempty"`
}

type policyDecisionPayload struct {
	Tool      string `json:"tool"`
	Allowed   bool   `json:"allowed"`
	Reason    string `json:"reason,omitempty"`
	Retry     bool   `json:"retry,omitempty"`
	Attempt   int    `json:"attempt,omitempty"`
	BackoffMS *int64 `json:"backoff_ms,omitempty"`
}

type checkpointPayload struct {
	Step int `json:"step"`
}

// stepIndex identifies one tool call within a run by its step and its
// position among that step's tool calls.
type stepIndex struct {
	step  int
	index int
}

// recoveredResult is a tool result already recorded for a stepIndex, found
// by scanning the event log before a resumed run reaches that call.
type recoveredResult struct {
	output tools.ToolOutput
	err    error
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

// WithCheckpoints sets the store Runtime saves a Checkpoint to after each
// completed step, and that Resume loads the latest checkpoint from.
// Without one, Runtime does not checkpoint and Resume always fails.
func WithCheckpoints(store recovery.Store) Option {
	return func(r *Runtime) { r.checkpoints = store }
}

// WithEventReader sets the reader Resume uses to find tool results already
// recorded for the step it is about to re-attempt, so it does not execute
// them again.
func WithEventReader(reader EventReader) Option {
	return func(r *Runtime) { r.events = reader }
}

// Runtime executes agent runs: it calls a ModelProvider, executes any
// requested tool calls concurrently via a ToolExecutor, records every
// request, response, tool call/result, policy decision, error, checkpoint,
// and run start/finish through a Recorder, and repeats until the provider
// stops requesting tools or a limit is reached.
type Runtime struct {
	provider    ModelProvider
	tools       ToolExecutor
	recorder    Recorder
	policy      policy.Policy
	checkpoints recovery.Store
	events      EventReader
	maxSteps    int
	logger      *slog.Logger
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
	return r.runLoop(ctx, runID, 0, provider.Request{Prompt: prompt}, nil)
}

// Resume continues runID from its latest checkpoint: it loads the saved
// request and step, finds any tool results already recorded for the step it
// is about to re-attempt (so it never executes a recorded call again), and
// continues the same loop Run uses.
func (r *Runtime) Resume(ctx context.Context, runID string) (RunResult, error) {
	if r.checkpoints == nil {
		return RunResult{}, fmt.Errorf("run %s: %w", runID, ErrNoCheckpoint)
	}

	cp, found, err := r.checkpoints.LatestCheckpoint(ctx, runID)
	if err != nil {
		return RunResult{}, fmt.Errorf("run %s: load checkpoint: %w", runID, err)
	}
	if !found {
		return RunResult{}, fmt.Errorf("run %s: %w", runID, ErrNoCheckpoint)
	}

	recovered, err := r.recoveredToolResults(ctx, runID)
	if err != nil {
		return RunResult{}, err
	}

	return r.runLoop(ctx, runID, cp.Step, cp.Request, recovered)
}

// runLoop is the shared step loop used by Run (starting fresh at step 0)
// and Resume (starting from a saved checkpoint, with already-recorded tool
// results available in recovered).
func (r *Runtime) runLoop(ctx context.Context, runID string, startStep int, req provider.Request, recovered map[stepIndex]recoveredResult) (RunResult, error) {
	startRetries := r.retriesSoFar()
	var totalUsage provider.Usage

	for step := startStep; ; step++ {
		if step >= r.maxSteps {
			err := fmt.Errorf("run %s: %w", runID, ErrMaxSteps)
			r.recordError(ctx, runID, step, err)
			return RunResult{}, err
		}
		if err := ctx.Err(); err != nil {
			return r.finishCancelled(ctx, runID, step, req, err)
		}

		if err := r.record(ctx, runID, recorder.EventModelRequest, req); err != nil {
			return RunResult{}, err
		}

		resp, err := r.provider.Complete(ctx, req)
		if err != nil {
			if ctxErr := ctx.Err(); ctxErr != nil {
				return r.finishCancelled(ctx, runID, step, req, ctxErr)
			}
			wrapped := fmt.Errorf("run %s: step %d: provider: %w", runID, step, err)
			r.recordError(ctx, runID, step, wrapped)
			return RunResult{}, wrapped
		}

		if err := r.record(ctx, runID, recorder.EventModelResponse, resp); err != nil {
			return RunResult{}, err
		}
		totalUsage.InputTokens += resp.Usage.InputTokens
		totalUsage.OutputTokens += resp.Usage.OutputTokens

		toolResults, cancelled, cancelErr := r.runToolCalls(ctx, runID, step, resp.ToolCalls, recovered)
		if cancelled {
			nextReq := provider.Request{Prompt: req.Prompt, ToolResults: toolResults}
			return r.finishCancelled(ctx, runID, step+1, nextReq, cancelErr)
		}

		r.logger.DebugContext(ctx, "runtime step", "run_id", runID, "step", step, "tool_calls", len(resp.ToolCalls))

		if len(resp.ToolCalls) == 0 {
			result := RunResult{
				RunID:   runID,
				Content: resp.Content,
				Steps:   step + 1,
				Status:  "completed",
				Usage:   totalUsage,
				Retries: int(r.retriesSoFar() - startRetries),
			}
			if err := r.record(ctx, runID, recorder.EventRunFinished, result); err != nil {
				return RunResult{}, err
			}
			return result, nil
		}

		nextReq := provider.Request{Prompt: req.Prompt, ToolResults: toolResults}
		if err := r.saveCheckpoint(ctx, runID, step+1, nextReq); err != nil {
			return RunResult{}, err
		}
		req = nextReq
	}
}

// runToolCalls executes calls concurrently, skipping any whose result is
// already in recovered, and waits for every launched goroutine to exit
// (via errgroup) before returning, even if ctx is cancelled partway
// through. The second return value is true if ctx was cancelled by the time
// every goroutine had exited.
func (r *Runtime) runToolCalls(ctx context.Context, runID string, step int, calls []tools.ToolCall, recovered map[stepIndex]recoveredResult) ([]provider.ToolResult, bool, error) {
	toolResults := make([]provider.ToolResult, len(calls))

	var g errgroup.Group
	for i, call := range calls {
		i, call := i, call

		if rr, ok := recovered[stepIndex{step: step, index: i}]; ok {
			toolResults[i] = provider.ToolResult{Call: call, Output: rr.output, Err: rr.err}
			continue
		}

		g.Go(func() error {
			r.runSingleToolCall(ctx, runID, step, i, call, toolResults)
			return nil
		})
	}
	_ = g.Wait()

	if err := ctx.Err(); err != nil {
		return toolResults, true, err
	}
	return toolResults, false, nil
}

// runSingleToolCall records the call, executes it (through policy if one is
// configured), records the policy decision and result, and stores the
// outcome at results[index]. It is safe to run concurrently with other
// calls writing to disjoint indices of the same slice.
func (r *Runtime) runSingleToolCall(ctx context.Context, runID string, step, index int, call tools.ToolCall, results []provider.ToolResult) {
	if err := r.record(ctx, runID, recorder.EventToolCall, toolCallPayload{Step: step, Index: index, Call: call}); err != nil {
		r.logger.ErrorContext(ctx, "failed to record tool_call", "run_id", runID, "step", step, "index", index, "error", err)
	}

	callCtx := policy.WithRetryObserver(ctx, func(n policy.RetryNotice) {
		backoff := n.Backoff.Milliseconds()
		payload := policyDecisionPayload{
			Tool:      n.Tool,
			Allowed:   true,
			Retry:     true,
			Attempt:   n.Attempt,
			BackoffMS: &backoff,
		}
		if n.Err != nil {
			payload.Reason = n.Err.Error()
		}
		if err := r.record(ctx, runID, recorder.EventPolicyDecision, payload); err != nil {
			r.logger.ErrorContext(ctx, "failed to record retry", "run_id", runID, "step", step, "index", index, "error", err)
		}
	})
	output, execErr := r.executeTool(callCtx, call)

	if err := r.recordPolicyDecision(ctx, runID, call, execErr); err != nil {
		r.logger.ErrorContext(ctx, "failed to record policy_decision", "run_id", runID, "step", step, "index", index, "error", err)
	}

	resultPayload := toolResultPayload{Step: step, Index: index, Name: call.Name, Output: output}
	if execErr != nil {
		resultPayload.Error = execErr.Error()
	}
	if err := r.record(ctx, runID, recorder.EventToolResult, resultPayload); err != nil {
		r.logger.ErrorContext(ctx, "failed to record tool_result", "run_id", runID, "step", step, "index", index, "error", err)
	}

	results[index] = provider.ToolResult{Call: call, Output: output, Err: execErr}
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

// retriesSoFar returns the configured policy's cumulative retry count, or 0
// if there is no policy or it doesn't track retries.
func (r *Runtime) retriesSoFar() int64 {
	if rc, ok := r.policy.(retryCounter); ok {
		return rc.Retries()
	}
	return 0
}

// finishCancelled finalizes a run that was stopped by ctx cancellation: it
// saves a last checkpoint and records a run_finished event with
// status=cancelled, using a context detached from ctx's cancellation so
// these writes are not aborted too, then returns the error Run/Resume report
// to their caller.
func (r *Runtime) finishCancelled(ctx context.Context, runID string, step int, nextReq provider.Request, cause error) (RunResult, error) {
	wrapped := fmt.Errorf("run %s: step %d: %w: %w", runID, step, ErrCancelled, cause)

	finalizeCtx := context.WithoutCancel(ctx)

	if err := r.saveCheckpoint(finalizeCtx, runID, step, nextReq); err != nil {
		r.logger.Error("failed to save final checkpoint on cancellation", "run_id", runID, "step", step, "error", err)
	}

	if r.recorder != nil {
		result := RunResult{RunID: runID, Steps: step, Status: "cancelled"}
		if err := r.recorder.Record(finalizeCtx, runID, recorder.EventRunFinished, result); err != nil {
			r.logger.Error("failed to record run_finished on cancellation", "run_id", runID, "step", step, "error", err)
		}
	}

	return RunResult{}, wrapped
}

// saveCheckpoint is a no-op when no checkpoint store is configured;
// otherwise it saves cp and records a checkpoint event.
func (r *Runtime) saveCheckpoint(ctx context.Context, runID string, step int, req provider.Request) error {
	if r.checkpoints == nil {
		return nil
	}
	cp := recovery.Checkpoint{RunID: runID, Step: step, Request: req, SavedAt: time.Now()}
	if err := r.checkpoints.SaveCheckpoint(ctx, cp); err != nil {
		return fmt.Errorf("run %s: save checkpoint step %d: %w", runID, step, err)
	}
	return r.record(ctx, runID, recorder.EventCheckpoint, checkpointPayload{Step: step})
}

// recoveredToolResults scans runID's recorded events for tool_result
// entries, so Resume can skip re-executing any call whose result is already
// recorded for its step. It returns an empty map if no EventReader is
// configured.
func (r *Runtime) recoveredToolResults(ctx context.Context, runID string) (map[stepIndex]recoveredResult, error) {
	out := make(map[stepIndex]recoveredResult)
	if r.events == nil {
		return out, nil
	}

	events, err := r.events.Events(ctx, runID)
	if err != nil {
		return nil, fmt.Errorf("run %s: load events for resume: %w", runID, err)
	}

	for _, e := range events {
		if e.Type != recorder.EventToolResult {
			continue
		}
		var payload toolResultPayload
		if err := json.Unmarshal(e.Payload, &payload); err != nil {
			return nil, fmt.Errorf("run %s: decode tool_result seq %d: %w", runID, e.Seq, err)
		}
		rr := recoveredResult{output: payload.Output}
		if payload.Error != "" {
			rr.err = errors.New(payload.Error)
		}
		out[stepIndex{step: payload.Step, index: payload.Index}] = rr
	}
	return out, nil
}

// record is a no-op when no Recorder is configured; otherwise it records
// eventType/payload for runID and wraps any failure as a fatal run error. The
// write ignores ctx's cancellation: cancellation is observed at step
// boundaries, and an event that was cut short would leave the log without
// the final state the caller is waiting for.
func (r *Runtime) record(ctx context.Context, runID string, eventType recorder.EventType, payload any) error {
	if r.recorder == nil {
		return nil
	}
	if err := r.recorder.Record(context.WithoutCancel(ctx), runID, eventType, payload); err != nil {
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
	if err := r.recorder.Record(context.WithoutCancel(ctx), runID, recorder.EventError, payload); err != nil {
		r.logger.ErrorContext(ctx, "failed to record error event", "run_id", runID, "step", step, "error", err)
	}
}
