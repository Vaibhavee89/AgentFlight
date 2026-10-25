package builtin

import (
	"context"
	"encoding/json"
	"errors"
	"testing"

	"github.com/Vaibhavee89/agentflight/internal/tools"
)

func TestCalculator_Execute(t *testing.T) {
	tests := []struct {
		name       string
		input      string
		want       string
		wantErr    error
		wantDecode bool
	}{
		{name: "add", input: `{"operation":"add","a":2,"b":3}`, want: "5"},
		{name: "sub", input: `{"operation":"sub","a":5,"b":3}`, want: "2"},
		{name: "mul", input: `{"operation":"mul","a":4,"b":3}`, want: "12"},
		{name: "div", input: `{"operation":"div","a":10,"b":2}`, want: "5"},
		{name: "div by zero", input: `{"operation":"div","a":10,"b":0}`, wantErr: ErrDivisionByZero},
		{name: "unknown operation", input: `{"operation":"mod","a":1,"b":1}`, wantErr: ErrUnknownOperation},
		{name: "invalid json", input: `not json`, wantDecode: true},
	}

	c := NewCalculator()
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			out, err := c.Execute(context.Background(), tools.ToolCall{
				Name:  "calculator",
				Input: json.RawMessage(tt.input),
			})
			if tt.wantDecode {
				if err == nil {
					t.Fatalf("Execute() expected decode error, got nil")
				}
				return
			}
			if tt.wantErr != nil {
				if !errors.Is(err, tt.wantErr) {
					t.Fatalf("Execute() error = %v, want %v", err, tt.wantErr)
				}
				return
			}
			if err != nil {
				t.Fatalf("Execute() unexpected error: %v", err)
			}
			if out.Result != tt.want {
				t.Fatalf("Execute() = %q, want %q", out.Result, tt.want)
			}
		})
	}
}

func TestCalculator_Spec(t *testing.T) {
	spec := NewCalculator().Spec()
	if spec.Name != "calculator" {
		t.Fatalf("Spec().Name = %q, want %q", spec.Name, "calculator")
	}
}
