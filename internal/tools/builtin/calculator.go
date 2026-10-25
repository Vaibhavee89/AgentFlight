package builtin

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/Vaibhavee89/agentflight/internal/tools"
)

// ErrUnknownOperation indicates a calculator input named an unsupported operation.
var ErrUnknownOperation = errors.New("calculator: unknown operation")

// ErrDivisionByZero indicates a calculator division input had a zero divisor.
var ErrDivisionByZero = errors.New("calculator: division by zero")

// calcInput is the JSON input accepted by the calculator tool.
type calcInput struct {
	Operation string  `json:"operation"`
	A         float64 `json:"a"`
	B         float64 `json:"b"`
}

type calculatorTool struct{}

// NewCalculator returns a tool that performs add/sub/mul/div on two operands a and b.
func NewCalculator() tools.Tool {
	return calculatorTool{}
}

func (calculatorTool) Spec() tools.ToolSpec {
	return tools.ToolSpec{
		Name:        "calculator",
		Description: "Performs add, sub, mul, or div on two numeric operands a and b.",
	}
}

func (calculatorTool) Execute(ctx context.Context, call tools.ToolCall) (tools.ToolOutput, error) {
	if err := ctx.Err(); err != nil {
		return tools.ToolOutput{}, fmt.Errorf("calculator: %w", err)
	}

	var in calcInput
	if err := json.Unmarshal(call.Input, &in); err != nil {
		return tools.ToolOutput{}, fmt.Errorf("calculator: decode input: %w", err)
	}

	var result float64
	switch in.Operation {
	case "add":
		result = in.A + in.B
	case "sub":
		result = in.A - in.B
	case "mul":
		result = in.A * in.B
	case "div":
		if in.B == 0 {
			return tools.ToolOutput{}, fmt.Errorf("calculator: %w", ErrDivisionByZero)
		}
		result = in.A / in.B
	default:
		return tools.ToolOutput{}, fmt.Errorf("calculator: operation %q: %w", in.Operation, ErrUnknownOperation)
	}

	return tools.ToolOutput{Result: fmt.Sprintf("%g", result)}, nil
}
