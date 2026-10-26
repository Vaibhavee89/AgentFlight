package mcp

import (
	"context"
	"encoding/json"
	"testing"

	mcpsdk "github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/Vaibhavee89/agentflight/internal/tools"
)

func TestDemoServer_AddTool(t *testing.T) {
	tests := []struct {
		name    string
		input   string
		want    string
		wantErr bool
	}{
		{name: "adds numbers", input: `{"a":2,"b":3}`, want: "5"},
		{name: "fractions", input: `{"a":0.5,"b":0.25}`, want: "0.75"},
		{name: "non-numeric arguments are a tool error", input: `{"a":"x","b":1}`, wantErr: true},
	}

	serverTransport, clientTransport := mcpsdk.NewInMemoryTransports()
	ctx := context.Background()
	serverSession, err := demoServer().Connect(ctx, serverTransport, nil)
	if err != nil {
		t.Fatalf("server connect: %v", err)
	}
	defer serverSession.Close()
	session, err := mcpsdk.NewClient(ClientInfo, nil).Connect(ctx, clientTransport, nil)
	if err != nil {
		t.Fatalf("client connect: %v", err)
	}
	defer session.Close()

	reg := tools.NewRegistry()
	if _, err := Register(ctx, reg, session); err != nil {
		t.Fatalf("Register: %v", err)
	}
	add, ok := reg.Get("add")
	if !ok {
		t.Fatalf("add not registered")
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			out, err := add.Execute(ctx, tools.ToolCall{Name: "add", Input: json.RawMessage(tt.input)})
			if tt.wantErr {
				if err == nil {
					t.Fatalf("Execute() returned nil error, want a tool-reported error")
				}
				return
			}
			if err != nil {
				t.Fatalf("Execute() unexpected error: %v", err)
			}
			if out.Result != tt.want {
				t.Fatalf("Result = %q, want %q", out.Result, tt.want)
			}
		})
	}
}
