package policy

import (
	"context"
	"fmt"
	"math/rand"
	"sync"
	"sync/atomic"
	"time"

	"github.com/Vaibhavee89/agentflight/internal/tools"
)

// Sleeper waits for d, or returns early with ctx's error if ctx is done
// first. Tests inject a fast Sleeper to keep backoff waits instant.
type Sleeper func(ctx context.Context, d time.Duration) error

func defaultSleep(ctx context.Context, d time.Duration) error {
	if d <= 0 {
		return nil
	}
	timer := time.NewTimer(d)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-timer.C:
		return nil
	}
}

// DefaultPolicy enforces a tool allowlist, a per-tool timeout, and
// exponential backoff with jitter for retryable tool failures.
type DefaultPolicy struct {
	allowed     map[string]struct{}
	timeout     time.Duration
	maxAttempts int
	baseDelay   time.Duration
	maxDelay    time.Duration
	sleep       Sleeper

	mu  sync.Mutex
	rng *rand.Rand

	retries int64
}

// DefaultOption configures a DefaultPolicy.
type DefaultOption func(*DefaultPolicy)

// WithTimeout sets the per-tool timeout applied to each attempt. Zero (the
// default) disables the timeout.
func WithTimeout(d time.Duration) DefaultOption {
	return func(p *DefaultPolicy) { p.timeout = d }
}

// WithMaxAttempts sets the maximum number of attempts (including the first)
// made for a tool call. The default is 1 (no retries).
func WithMaxAttempts(n int) DefaultOption {
	return func(p *DefaultPolicy) { p.maxAttempts = n }
}

// WithBackoff sets the exponential backoff base and cap used between
// retries; the actual delay is a random jitter in [0, min(base*2^(n-1), max)).
func WithBackoff(base, max time.Duration) DefaultOption {
	return func(p *DefaultPolicy) { p.baseDelay = base; p.maxDelay = max }
}

// WithSleeper overrides the backoff wait function, letting tests make
// backoff instant without changing retry behavior.
func WithSleeper(s Sleeper) DefaultOption {
	return func(p *DefaultPolicy) { p.sleep = s }
}

// NewDefaultPolicy builds a DefaultPolicy that only allows the named tools.
func NewDefaultPolicy(allowed []string, opts ...DefaultOption) *DefaultPolicy {
	allowedSet := make(map[string]struct{}, len(allowed))
	for _, name := range allowed {
		allowedSet[name] = struct{}{}
	}

	p := &DefaultPolicy{
		allowed:     allowedSet,
		maxAttempts: 1,
		baseDelay:   100 * time.Millisecond,
		maxDelay:    2 * time.Second,
		sleep:       defaultSleep,
		rng:         rand.New(rand.NewSource(time.Now().UnixNano())),
	}
	for _, opt := range opts {
		opt(p)
	}
	return p
}

// Before rejects call with an error wrapping ErrPolicyDenied unless its name
// is in the allowlist.
func (p *DefaultPolicy) Before(ctx context.Context, call tools.ToolCall) error {
	if _, ok := p.allowed[call.Name]; !ok {
		return fmt.Errorf("policy: tool %q: %w", call.Name, ErrPolicyDenied)
	}
	return nil
}

// Classify reports whether err is worth retrying.
func (p *DefaultPolicy) Classify(err error) Disposition {
	return Classify(err)
}

// Execute runs call against exec under this policy: it gates the call via
// Before, enforces the per-tool timeout on each attempt, and retries
// Retryable failures with exponential backoff and jitter up to MaxAttempts.
// A denial from Before is never retried. The backoff wait respects ctx
// cancellation and returns immediately if ctx is done.
func (p *DefaultPolicy) Execute(ctx context.Context, exec ToolExecutor, call tools.ToolCall) (tools.ToolOutput, error) {
	if err := p.Before(ctx, call); err != nil {
		return tools.ToolOutput{}, err
	}

	var lastErr error
	for attempt := 1; attempt <= p.maxAttempts; attempt++ {
		if err := ctx.Err(); err != nil {
			return tools.ToolOutput{}, fmt.Errorf("policy: %w", err)
		}

		output, err := p.attempt(ctx, exec, call)
		if err == nil {
			return output, nil
		}
		lastErr = err

		if Classify(err) != Retryable || attempt == p.maxAttempts {
			return tools.ToolOutput{}, lastErr
		}

		atomic.AddInt64(&p.retries, 1)

		if err := p.sleep(ctx, p.backoff(attempt)); err != nil {
			return tools.ToolOutput{}, fmt.Errorf("policy: %w", err)
		}
	}
	return tools.ToolOutput{}, lastErr
}

func (p *DefaultPolicy) attempt(ctx context.Context, exec ToolExecutor, call tools.ToolCall) (tools.ToolOutput, error) {
	callCtx := ctx
	var cancel context.CancelFunc
	if p.timeout > 0 {
		callCtx, cancel = context.WithTimeout(ctx, p.timeout)
		defer cancel()
	}

	output, err := exec.Execute(callCtx, call)
	if err != nil && callCtx.Err() != nil && ctx.Err() == nil {
		// callCtx hit its own deadline while the parent ctx is still live:
		// this is our per-tool timeout, not an outer cancellation.
		return tools.ToolOutput{}, fmt.Errorf("policy: tool %q: %w", call.Name, ErrToolTimeout)
	}
	return output, err
}

// Retries reports the total number of retry attempts this policy has made
// across every Execute call since it was constructed. Callers that want a
// per-run count should snapshot it before and after a run and report the
// difference.
func (p *DefaultPolicy) Retries() int64 {
	return atomic.LoadInt64(&p.retries)
}

// backoff returns a jittered delay for the given attempt (1-based), the
// number of the attempt that just failed.
func (p *DefaultPolicy) backoff(attempt int) time.Duration {
	d := p.baseDelay << uint(attempt-1)
	if p.maxDelay > 0 && d > p.maxDelay {
		d = p.maxDelay
	}
	if d <= 0 {
		return 0
	}

	p.mu.Lock()
	jitter := time.Duration(p.rng.Int63n(int64(d) + 1))
	p.mu.Unlock()
	return jitter
}
