package policy

import (
	"context"
	"errors"
	"fmt"

	"github.com/Vaibhavee89/agentflight/internal/tools"
)

// Disposition classifies whether a failed operation is worth retrying.
type Disposition int

const (
	// Terminal indicates the failure should not be retried.
	Terminal Disposition = iota
	// Retryable indicates the failure is transient and may succeed if retried.
	Retryable
)

// Policy gates tool calls before they run and classifies their failures.
type Policy interface {
	Before(ctx context.Context, call tools.ToolCall) error
	Classify(err error) Disposition
}

// ToolExecutor executes a single tool call. It matches the shape of
// runtime.ToolExecutor so a *tools.Registry (or anything satisfying
// runtime's interface) can be passed directly.
type ToolExecutor interface {
	Execute(ctx context.Context, call tools.ToolCall) (tools.ToolOutput, error)
}

// ErrPolicyDenied indicates a tool call was rejected by policy before it ran.
var ErrPolicyDenied = errors.New("policy: tool call denied")

// ErrToolTimeout indicates a tool call exceeded its allotted per-tool timeout.
var ErrToolTimeout = errors.New("policy: tool timed out")

// ErrTransient marks a failure of the channel to a tool (for example, a
// dropped connection to an MCP server), as opposed to the tool itself failing.
// Classify treats it as Retryable.
var ErrTransient = errors.New("policy: transient transport failure")

// StatusError represents a provider error carrying an HTTP-like status
// code, used to classify 429 and 5xx responses as retryable.
type StatusError struct {
	StatusCode int
}

func (e *StatusError) Error() string {
	return fmt.Sprintf("policy: provider status %d", e.StatusCode)
}
