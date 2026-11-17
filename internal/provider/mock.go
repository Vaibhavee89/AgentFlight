package provider

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"sync"

	"github.com/Vaibhavee89/agentflight/internal/tools"
)

// Request is the input to a model provider for one completion step. It carries
// the whole conversation so far: the prompt, the tool exchanges of every earlier
// step in History, and the latest step's in ToolResults.
type Request struct {
	Prompt string
	// History holds the tool exchanges of the steps before the latest one,
	// oldest first. It is empty on the first two model calls of a run.
	History []Exchange `json:",omitempty"`
	// ToolResults holds the latest step's tool calls and their outcomes.
	ToolResults []ToolResult
}

// Exchange is one earlier step's tool calls and what came back from each.
type Exchange struct {
	ToolResults []ToolResult
}

// Exchanges returns every step's tool exchange in conversation order: History
// first, then the latest ToolResults. Steps with no tool calls are omitted.
func (r Request) Exchanges() [][]ToolResult {
	out := make([][]ToolResult, 0, len(r.History)+1)
	for _, ex := range r.History {
		if len(ex.ToolResults) > 0 {
			out = append(out, ex.ToolResults)
		}
	}
	if len(r.ToolResults) > 0 {
		out = append(out, r.ToolResults)
	}
	return out
}

// ToolResult pairs a previously requested tool call with its execution outcome.
type ToolResult struct {
	Call   tools.ToolCall
	Output tools.ToolOutput
	Err    error
}

// errNotRecorded stands in for a tool error read back from a recording made
// before errors were stored as text, when the message was not kept.
var errNotRecorded = errors.New("tool failed (error message was not recorded)")

// toolResultWire is ToolResult's JSON form. Err is its message as a string, or
// null; an error value on its own encodes as {} and cannot be read back.
type toolResultWire struct {
	Call   tools.ToolCall
	Output tools.ToolOutput
	Err    *string
}

// MarshalJSON encodes r with Err as its message.
func (r ToolResult) MarshalJSON() ([]byte, error) {
	w := toolResultWire{Call: r.Call, Output: r.Output}
	if r.Err != nil {
		msg := r.Err.Error()
		w.Err = &msg
	}
	return json.Marshal(w)
}

// UnmarshalJSON decodes the form MarshalJSON writes. It also accepts the old
// {} form for a failed tool, reading it as a generic error.
func (r *ToolResult) UnmarshalJSON(data []byte) error {
	var w struct {
		Call   tools.ToolCall
		Output tools.ToolOutput
		Err    json.RawMessage
	}
	if err := json.Unmarshal(data, &w); err != nil {
		return fmt.Errorf("provider: decode tool result: %w", err)
	}
	r.Call, r.Output, r.Err = w.Call, w.Output, nil

	switch {
	case len(w.Err) == 0 || string(w.Err) == "null":
	case w.Err[0] == '"':
		var msg string
		if err := json.Unmarshal(w.Err, &msg); err != nil {
			return fmt.Errorf("provider: decode tool result error: %w", err)
		}
		r.Err = errors.New(msg)
	default:
		r.Err = errNotRecorded
	}
	return nil
}

// Usage reports the token counts a provider billed for a completion call.
type Usage struct {
	InputTokens  int
	OutputTokens int
}

// ModelResponse is a single response a provider can return for a completion
// call. ToolCalls is non-empty when the model wants tools executed before it
// continues.
type ModelResponse struct {
	Content   string
	ToolCalls []tools.ToolCall
	Usage     Usage
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
