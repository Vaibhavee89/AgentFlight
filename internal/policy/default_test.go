package policy

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/Vaibhavee89/agentflight/internal/tools"
)

type execResult struct {
	output tools.ToolOutput
	err    error
}

type stubExecutor struct {
	mu      sync.Mutex
	calls   int
	results []execResult
}

func (s *stubExecutor) Execute(ctx context.Context, call tools.ToolCall) (tools.ToolOutput, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	idx := s.calls
	s.calls++
	if idx < len(s.results) {
		return s.results[idx].output, s.results[idx].err
	}
	last := s.results[len(s.results)-1]
	return last.output, last.err
}

func (s *stubExecutor) callCount() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.calls
}

type blockingExecutor struct{}

func (blockingExecutor) Execute(ctx context.Context, call tools.ToolCall) (tools.ToolOutput, error) {
	<-ctx.Done()
	return tools.ToolOutput{}, ctx.Err()
}

func instantSleeper(ctx context.Context, d time.Duration) error { return nil }

func TestDefaultPolicy_Before(t *testing.T) {
	tests := []struct {
		name    string
		call    tools.ToolCall
		wantErr bool
	}{
		{name: "allowed tool passes", call: tools.ToolCall{Name: "echo"}},
		{name: "unlisted tool is denied", call: tools.ToolCall{Name: "calculator"}, wantErr: true},
	}

	p := NewDefaultPolicy([]string{"echo"})
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := p.Before(context.Background(), tt.call)
			if tt.wantErr {
				if !errors.Is(err, ErrPolicyDenied) {
					t.Fatalf("Before() error = %v, want wrapping %v", err, ErrPolicyDenied)
				}
				return
			}
			if err != nil {
				t.Fatalf("Before() unexpected error: %v", err)
			}
		})
	}
}

func TestDefaultPolicy_Execute_RetriesTransientThenSucceeds(t *testing.T) {
	exec := &stubExecutor{results: []execResult{
		{err: &StatusError{StatusCode: 500}},
		{err: &StatusError{StatusCode: 503}},
		{output: tools.ToolOutput{Result: "ok"}},
	}}
	p := NewDefaultPolicy([]string{"demo"},
		WithMaxAttempts(3),
		WithSleeper(instantSleeper),
	)

	out, err := p.Execute(context.Background(), exec, tools.ToolCall{Name: "demo"})
	if err != nil {
		t.Fatalf("Execute() unexpected error: %v", err)
	}
	if out.Result != "ok" {
		t.Fatalf("Execute() = %+v, want Result %q", out, "ok")
	}
	if got := exec.callCount(); got != 3 {
		t.Fatalf("exec called %d times, want 3", got)
	}
}

func TestDefaultPolicy_Execute_TerminalFailsAfterOneAttempt(t *testing.T) {
	exec := &stubExecutor{results: []execResult{
		{err: &StatusError{StatusCode: 404}},
		{output: tools.ToolOutput{Result: "unreachable"}},
	}}
	p := NewDefaultPolicy([]string{"demo"}, WithMaxAttempts(5), WithSleeper(instantSleeper))

	_, err := p.Execute(context.Background(), exec, tools.ToolCall{Name: "demo"})
	if err == nil {
		t.Fatalf("Execute() expected error, got nil")
	}
	if got := exec.callCount(); got != 1 {
		t.Fatalf("exec called %d times, want 1 (terminal failure should not retry)", got)
	}
}

func TestDefaultPolicy_Execute_DeniedCallAttemptedOnce(t *testing.T) {
	exec := &stubExecutor{results: []execResult{{output: tools.ToolOutput{Result: "should not run"}}}}
	p := NewDefaultPolicy([]string{"allowed-only"}, WithMaxAttempts(5), WithSleeper(instantSleeper))

	_, err := p.Execute(context.Background(), exec, tools.ToolCall{Name: "forbidden"})
	if !errors.Is(err, ErrPolicyDenied) {
		t.Fatalf("Execute() error = %v, want wrapping %v", err, ErrPolicyDenied)
	}
	if got := exec.callCount(); got != 0 {
		t.Fatalf("exec called %d times, want 0", got)
	}
}

func TestDefaultPolicy_Execute_PerToolTimeout(t *testing.T) {
	p := NewDefaultPolicy([]string{"slow"}, WithTimeout(10*time.Millisecond), WithMaxAttempts(1))

	_, err := p.Execute(context.Background(), blockingExecutor{}, tools.ToolCall{Name: "slow"})
	if !errors.Is(err, ErrToolTimeout) {
		t.Fatalf("Execute() error = %v, want wrapping %v", err, ErrToolTimeout)
	}
}

func TestDefaultPolicy_Execute_CancelDuringBackoffReturnsImmediately(t *testing.T) {
	exec := &stubExecutor{results: []execResult{{err: &StatusError{StatusCode: 503}}}}
	p := NewDefaultPolicy([]string{"demo"}, WithMaxAttempts(5), WithBackoff(time.Second, time.Second))

	ctx, cancel := context.WithCancel(context.Background())
	go func() {
		time.Sleep(10 * time.Millisecond)
		cancel()
	}()

	start := time.Now()
	_, err := p.Execute(ctx, exec, tools.ToolCall{Name: "demo"})
	elapsed := time.Since(start)

	if !errors.Is(err, context.Canceled) {
		t.Fatalf("Execute() error = %v, want wrapping %v", err, context.Canceled)
	}
	if elapsed > 500*time.Millisecond {
		t.Fatalf("Execute() took %v, want well under the 1s backoff", elapsed)
	}
}

func TestClassify(t *testing.T) {
	tests := []struct {
		name string
		err  error
		want Disposition
	}{
		{name: "policy denied is terminal", err: fmt.Errorf("wrap: %w", ErrPolicyDenied), want: Terminal},
		{name: "tool not found is terminal", err: &tools.ErrToolNotFound{Name: "x"}, want: Terminal},
		{name: "context canceled is terminal", err: context.Canceled, want: Terminal},
		{name: "context deadline exceeded is terminal", err: context.DeadlineExceeded, want: Terminal},
		{name: "tool timeout is retryable", err: fmt.Errorf("wrap: %w", ErrToolTimeout), want: Retryable},
		{name: "transient transport failure is retryable", err: fmt.Errorf("wrap: %w", ErrTransient), want: Retryable},
		{name: "net timeout error is retryable", err: fakeNetError{timeout: true}, want: Retryable},
		{name: "net non-timeout error is terminal", err: fakeNetError{timeout: false}, want: Terminal},
		{name: "status 429 is retryable", err: &StatusError{StatusCode: 429}, want: Retryable},
		{name: "status 500 is retryable", err: &StatusError{StatusCode: 500}, want: Retryable},
		{name: "status 404 is terminal", err: &StatusError{StatusCode: 404}, want: Terminal},
		{name: "unknown error is terminal", err: errors.New("boom"), want: Terminal},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := Classify(tt.err); got != tt.want {
				t.Fatalf("Classify(%v) = %v, want %v", tt.err, got, tt.want)
			}
		})
	}
}

type fakeNetError struct{ timeout bool }

func (e fakeNetError) Error() string   { return "fake net error" }
func (e fakeNetError) Timeout() bool   { return e.timeout }
func (e fakeNetError) Temporary() bool { return e.timeout }
