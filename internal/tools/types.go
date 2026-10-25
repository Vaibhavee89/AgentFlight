package tools

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
)

// ToolSpec describes a tool's identity and purpose to a caller.
type ToolSpec struct {
	Name        string
	Description string
}

// ToolCall is a request to execute a named tool with a JSON-encoded input.
type ToolCall struct {
	Name  string
	Input json.RawMessage
}

// ToolOutput is the result of executing a tool.
type ToolOutput struct {
	Result string
}

// Tool is a callable capability that can be registered with and executed by a Registry.
type Tool interface {
	Spec() ToolSpec
	Execute(ctx context.Context, call ToolCall) (ToolOutput, error)
}

// ErrDuplicateTool indicates a tool name was already registered.
var ErrDuplicateTool = errors.New("tools: duplicate tool name")

// ErrToolNotFound indicates a Registry has no tool registered under the given name.
type ErrToolNotFound struct {
	Name string
}

func (e *ErrToolNotFound) Error() string {
	return fmt.Sprintf("tools: tool %q not found", e.Name)
}
