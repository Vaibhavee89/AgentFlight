package builtin

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/Vaibhavee89/agentflight/internal/tools"
)

// echoInput is the JSON input accepted by the echo tool.
type echoInput struct {
	Text string `json:"text"`
}

type echoTool struct{}

// NewEcho returns a tool that returns its input text unchanged.
func NewEcho() tools.Tool {
	return echoTool{}
}

func (echoTool) Spec() tools.ToolSpec {
	return tools.ToolSpec{
		Name:        "echo",
		Description: "Returns the given text unchanged.",
	}
}

func (echoTool) Execute(ctx context.Context, call tools.ToolCall) (tools.ToolOutput, error) {
	if err := ctx.Err(); err != nil {
		return tools.ToolOutput{}, fmt.Errorf("echo: %w", err)
	}

	var in echoInput
	if err := json.Unmarshal(call.Input, &in); err != nil {
		return tools.ToolOutput{}, fmt.Errorf("echo: decode input: %w", err)
	}

	return tools.ToolOutput{Result: in.Text}, nil
}
