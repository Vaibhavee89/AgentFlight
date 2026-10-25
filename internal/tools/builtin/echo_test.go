package builtin

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/Vaibhavee89/agentflight/internal/tools"
)

func TestEcho_Execute(t *testing.T) {
	tests := []struct {
		name    string
		input   string
		want    string
		wantErr bool
	}{
		{name: "returns input text", input: `{"text":"hello"}`, want: "hello"},
		{name: "empty text", input: `{"text":""}`, want: ""},
		{name: "invalid json", input: `not json`, wantErr: true},
	}

	e := NewEcho()
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			out, err := e.Execute(context.Background(), tools.ToolCall{
				Name:  "echo",
				Input: json.RawMessage(tt.input),
			})
			if tt.wantErr {
				if err == nil {
					t.Fatalf("Execute() expected error, got nil")
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

func TestEcho_Spec(t *testing.T) {
	spec := NewEcho().Spec()
	if spec.Name != "echo" {
		t.Fatalf("Spec().Name = %q, want %q", spec.Name, "echo")
	}
}
