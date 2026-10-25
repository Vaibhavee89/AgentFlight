package provider

import (
	"context"
	"errors"
	"fmt"
	"sync"

	"github.com/Vaibhavee89/agentflight/internal/tools"
)

// Request is the input to a model provider for one completion step.
type Request struct {
	Prompt      string
	ToolResults []ToolResult
}

// ToolResult pairs a previously requested tool call with its execution outcome.
type ToolResult struct {
	Call   tools.ToolCall
	Output tools.ToolOutput
	Err    error
}

// ModelResponse is a single response a provider can return for a completion
// call. ToolCalls is non-empty when the model wants tools executed before it
// continues.
type ModelResponse struct {
	Content   string
	ToolCalls []tools.ToolCall
}

// ErrScriptExhausted indicates a MockProvider has no more scripted responses or errors.
var ErrScriptExhausted = errors.New("provider: script exhausted")

// MockProvider replays a scripted sequence of responses, optionally interleaved
// with scripted errors, in call order. It is safe for concurrent use.
type MockProvider struct {
	mu        sync.Mutex
	responses []ModelResponse
	errs      []error
	next      int
}

// NewMockProvider builds a MockProvider that replays responses in order on
// successive calls to Complete. errs may be nil or shorter than responses;
// when errs[i] is non-nil, the i-th call returns that error instead of
// responses[i].
func NewMockProvider(responses []ModelResponse, errs []error) *MockProvider {
	return &MockProvider{
		responses: responses,
		errs:      errs,
	}
}

// Complete returns the next scripted response or error, ignoring the
// content of req since the script is deterministic. Once the script is
// exhausted it returns an error wrapping ErrScriptExhausted on every
// subsequent call.
func (m *MockProvider) Complete(ctx context.Context, _ Request) (ModelResponse, error) {
	if err := ctx.Err(); err != nil {
		return ModelResponse{}, fmt.Errorf("mock provider: %w", err)
	}

	m.mu.Lock()
	defer m.mu.Unlock()

	if m.next >= len(m.responses) {
		return ModelResponse{}, fmt.Errorf("mock provider call %d: %w", m.next, ErrScriptExhausted)
	}

	idx := m.next
	m.next++

	if idx < len(m.errs) && m.errs[idx] != nil {
		return ModelResponse{}, m.errs[idx]
	}
	return m.responses[idx], nil
}
