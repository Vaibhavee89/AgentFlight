package fault

import (
	"context"
	"fmt"
	"math/rand"
	"sync"
	"time"

	"github.com/Vaibhavee89/agentflight/internal/policy"
	"github.com/Vaibhavee89/agentflight/internal/provider"
	"github.com/Vaibhavee89/agentflight/internal/tools"
)

// ModelProvider matches the shape of runtime.ModelProvider, so
// FaultProvider can wrap any provider usable with Runtime without
// importing it.
type ModelProvider interface {
	Complete(ctx context.Context, req provider.Request) (provider.ModelResponse, error)
}

// ToolExecutor matches the shape of runtime.ToolExecutor, so FaultTool can
// wrap any tool executor (including a *tools.Registry) usable with Runtime
// without importing it.
type ToolExecutor interface {
	Execute(ctx context.Context, call tools.ToolCall) (tools.ToolOutput, error)
}

// Config controls fault injection rates (each a probability in [0,1]) and
// their parameters.
type Config struct {
	// ProviderErrorRate is the chance a provider call fails with a
	// transient 429 or 503 (*policy.StatusError) instead of calling
	// through to the wrapped provider.
	ProviderErrorRate float64
	// LatencyRate is the chance a provider call is delayed by Latency
	// before calling through.
	LatencyRate float64
	Latency     time.Duration
	// ToolTimeoutRate is the chance a tool call hangs until ctx is done,
	// instead of calling through to the wrapped executor.
	ToolTimeoutRate float64
	// MalformedOutputRate is the chance a tool call returns corrupted
	// output instead of calling through to the wrapped executor.
	MalformedOutputRate float64
}

// FaultProvider wraps a ModelProvider, injecting transient 429/503 errors
// and added latency at the rates in Config, using a seeded RNG so the
// exact sequence of faults is reproducible. It is safe for concurrent use.
type FaultProvider struct {
	inner  ModelProvider
	config Config

	mu  sync.Mutex
	rng *rand.Rand
}

// NewFaultProvider wraps inner, injecting faults per config. The sequence
// of fault decisions is fully determined by seed.
func NewFaultProvider(inner ModelProvider, config Config, seed int64) *FaultProvider {
	return &FaultProvider{inner: inner, config: config, rng: rand.New(rand.NewSource(seed))}
}

// Complete injects latency and/or a transient error per Config before
// calling through to the wrapped provider.
func (p *FaultProvider) Complete(ctx context.Context, req provider.Request) (provider.ModelResponse, error) {
	if p.trigger(p.config.LatencyRate) {
		select {
		case <-time.After(p.config.Latency):
		case <-ctx.Done():
			return provider.ModelResponse{}, ctx.Err()
		}
	}

	if p.trigger(p.config.ProviderErrorRate) {
		codes := []int{429, 503}
		code := codes[p.intn(len(codes))]
		return provider.ModelResponse{}, fmt.Errorf("fault: injected transient provider error: %w", &policy.StatusError{StatusCode: code})
	}

	return p.inner.Complete(ctx, req)
}

func (p *FaultProvider) trigger(rate float64) bool {
	if rate <= 0 {
		return false
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.rng.Float64() < rate
}

func (p *FaultProvider) intn(n int) int {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.rng.Intn(n)
}

// FaultTool wraps a ToolExecutor, injecting hangs (simulating a timeout)
// and malformed output at the rates in Config, using a seeded RNG so the
// exact sequence of faults is reproducible. It is safe for concurrent use.
type FaultTool struct {
	inner  ToolExecutor
	config Config

	mu  sync.Mutex
	rng *rand.Rand
}

// NewFaultTool wraps inner, injecting faults per config. The sequence of
// fault decisions is fully determined by seed.
func NewFaultTool(inner ToolExecutor, config Config, seed int64) *FaultTool {
	return &FaultTool{inner: inner, config: config, rng: rand.New(rand.NewSource(seed))}
}

// Execute injects a hang or malformed output per Config before calling
// through to the wrapped executor. A hang blocks until ctx is done and
// returns ctx.Err(), simulating a tool that never responds within its
// caller's timeout.
func (t *FaultTool) Execute(ctx context.Context, call tools.ToolCall) (tools.ToolOutput, error) {
	if t.trigger(t.config.ToolTimeoutRate) {
		<-ctx.Done()
		return tools.ToolOutput{}, ctx.Err()
	}

	if t.trigger(t.config.MalformedOutputRate) {
		return tools.ToolOutput{Result: "\x00<malformed output>\x00"}, nil
	}

	return t.inner.Execute(ctx, call)
}

func (t *FaultTool) trigger(rate float64) bool {
	if rate <= 0 {
		return false
	}
	t.mu.Lock()
	defer t.mu.Unlock()
	return t.rng.Float64() < rate
}
