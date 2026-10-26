package fault

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/Vaibhavee89/agentflight/internal/policy"
	"github.com/Vaibhavee89/agentflight/internal/provider"
	"github.com/Vaibhavee89/agentflight/internal/tools"
)

type stubProvider struct {
	resp provider.ModelResponse
	err  error
}

func (s stubProvider) Complete(ctx context.Context, req provider.Request) (provider.ModelResponse, error) {
	return s.resp, s.err
}

type stubTool struct {
	out tools.ToolOutput
	err error
}

func (s stubTool) Execute(ctx context.Context, call tools.ToolCall) (tools.ToolOutput, error) {
	return s.out, s.err
}

func TestFaultProvider_RateZero_NeverInjects(t *testing.T) {
	inner := stubProvider{resp: provider.ModelResponse{Content: "ok"}}
	p := NewFaultProvider(inner, Config{}, 1)

	for i := 0; i < 50; i++ {
		resp, err := p.Complete(context.Background(), provider.Request{})
		if err != nil {
			t.Fatalf("Complete() unexpected error: %v", err)
		}
		if resp.Content != "ok" {
			t.Fatalf("Complete() = %+v, want passthrough", resp)
		}
	}
}

func TestFaultProvider_RateOne_AlwaysInjectsRetryableError(t *testing.T) {
	inner := stubProvider{resp: provider.ModelResponse{Content: "ok"}}
	p := NewFaultProvider(inner, Config{ProviderErrorRate: 1}, 1)

	for i := 0; i < 20; i++ {
		_, err := p.Complete(context.Background(), provider.Request{})
		if err == nil {
			t.Fatalf("Complete() call %d: expected injected error, got nil", i)
		}
		var statusErr *policy.StatusError
		if !errors.As(err, &statusErr) {
			t.Fatalf("Complete() call %d: error = %v, want wrapping *policy.StatusError", i, err)
		}
		if statusErr.StatusCode != 429 && statusErr.StatusCode != 503 {
			t.Fatalf("Complete() call %d: status = %d, want 429 or 503", i, statusErr.StatusCode)
		}
		if policy.Classify(err) != policy.Retryable {
			t.Fatalf("Complete() call %d: Classify(err) = %v, want Retryable", i, policy.Classify(err))
		}
	}
}

func TestFaultProvider_LatencyRateOne_DelaysByConfiguredAmount(t *testing.T) {
	inner := stubProvider{resp: provider.ModelResponse{Content: "ok"}}
	delay := 30 * time.Millisecond
	p := NewFaultProvider(inner, Config{LatencyRate: 1, Latency: delay}, 1)

	start := time.Now()
	resp, err := p.Complete(context.Background(), provider.Request{})
	elapsed := time.Since(start)

	if err != nil {
		t.Fatalf("Complete() unexpected error: %v", err)
	}
	if resp.Content != "ok" {
		t.Fatalf("Complete() = %+v, want passthrough after latency", resp)
	}
	if elapsed < delay {
		t.Fatalf("Complete() took %v, want at least %v", elapsed, delay)
	}
}

func TestFaultProvider_LatencyRespectsContextCancellation(t *testing.T) {
	inner := stubProvider{resp: provider.ModelResponse{Content: "ok"}}
	p := NewFaultProvider(inner, Config{LatencyRate: 1, Latency: time.Hour}, 1)

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Millisecond)
	defer cancel()

	start := time.Now()
	_, err := p.Complete(ctx, provider.Request{})
	elapsed := time.Since(start)

	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("Complete() error = %v, want wrapping %v", err, context.DeadlineExceeded)
	}
	if elapsed > 500*time.Millisecond {
		t.Fatalf("Complete() took %v, want well under the 1h latency", elapsed)
	}
}

func TestFaultProvider_Deterministic_SameSeedSameSequence(t *testing.T) {
	inner := stubProvider{resp: provider.ModelResponse{Content: "ok"}}
	cfg := Config{ProviderErrorRate: 0.5}

	run := func(seed int64) []bool {
		p := NewFaultProvider(inner, cfg, seed)
		var outcomes []bool
		for i := 0; i < 30; i++ {
			_, err := p.Complete(context.Background(), provider.Request{})
			outcomes = append(outcomes, err != nil)
		}
		return outcomes
	}

	a := run(42)
	b := run(42)
	for i := range a {
		if a[i] != b[i] {
			t.Fatalf("outcome[%d] differs between two runs with the same seed: %v vs %v", i, a, b)
		}
	}
}

func TestFaultTool_RateZero_NeverInjects(t *testing.T) {
	inner := stubTool{out: tools.ToolOutput{Result: "ok"}}
	tool := NewFaultTool(inner, Config{}, 1)

	for i := 0; i < 50; i++ {
		out, err := tool.Execute(context.Background(), tools.ToolCall{Name: "echo"})
		if err != nil {
			t.Fatalf("Execute() unexpected error: %v", err)
		}
		if out.Result != "ok" {
			t.Fatalf("Execute() = %+v, want passthrough", out)
		}
	}
}

func TestFaultTool_MalformedOutputRateOne_AlwaysCorrupts(t *testing.T) {
	inner := stubTool{out: tools.ToolOutput{Result: "correct"}}
	tool := NewFaultTool(inner, Config{MalformedOutputRate: 1}, 1)

	for i := 0; i < 20; i++ {
		out, err := tool.Execute(context.Background(), tools.ToolCall{Name: "echo"})
		if err != nil {
			t.Fatalf("Execute() unexpected error: %v", err)
		}
		if out.Result == "correct" {
			t.Fatalf("Execute() returned the real output, want malformed")
		}
	}
}

func TestFaultTool_TimeoutRateOne_HangsUntilContextDone(t *testing.T) {
	inner := stubTool{out: tools.ToolOutput{Result: "ok"}}
	tool := NewFaultTool(inner, Config{ToolTimeoutRate: 1}, 1)

	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Millisecond)
	defer cancel()

	start := time.Now()
	_, err := tool.Execute(ctx, tools.ToolCall{Name: "echo"})
	elapsed := time.Since(start)

	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("Execute() error = %v, want wrapping %v", err, context.DeadlineExceeded)
	}
	if elapsed > 500*time.Millisecond {
		t.Fatalf("Execute() took %v, want returning promptly once ctx is done", elapsed)
	}
}

func TestFaultTool_Deterministic_SameSeedSameSequence(t *testing.T) {
	inner := stubTool{out: tools.ToolOutput{Result: "ok"}}
	cfg := Config{MalformedOutputRate: 0.5}

	run := func(seed int64) []bool {
		tool := NewFaultTool(inner, cfg, seed)
		var outcomes []bool
		for i := 0; i < 30; i++ {
			out, _ := tool.Execute(context.Background(), tools.ToolCall{Name: "echo"})
			outcomes = append(outcomes, out.Result != "ok")
		}
		return outcomes
	}

	a := run(7)
	b := run(7)
	for i := range a {
		if a[i] != b[i] {
			t.Fatalf("outcome[%d] differs between two runs with the same seed: %v vs %v", i, a, b)
		}
	}
}
