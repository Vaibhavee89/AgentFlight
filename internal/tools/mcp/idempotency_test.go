package mcp

import (
	"context"
	"testing"

	mcpsdk "github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/Vaibhavee89/agentflight/internal/tools"
)

func TestTool_Execute_ForwardsIdempotencyKeyInMeta(t *testing.T) {
	tests := []struct {
		name    string
		key     string
		wantKey string
	}{
		{name: "key is passed in _meta", key: "run-1:0:0:abc", wantKey: "run-1:0:0:abc"},
		{name: "no key sends no _meta entry", key: "", wantKey: "<none>"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ctx := context.Background()
			srv := mcpsdk.NewServer(&mcpsdk.Implementation{Name: "meta", Version: "0.0.1"}, nil)
			srv.AddTool(&mcpsdk.Tool{
				Name:        "whoami",
				InputSchema: objectSchema(map[string]any{}),
			}, func(ctx context.Context, req *mcpsdk.CallToolRequest) (*mcpsdk.CallToolResult, error) {
				got := "<none>"
				if v, ok := req.Params.GetMeta()[IdempotencyKeyMeta].(string); ok {
					got = v
				}
				return textResult(got), nil
			})
			serverTransport, clientTransport := mcpsdk.NewInMemoryTransports()
			serverSession, err := srv.Connect(ctx, serverTransport, nil)
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

			out, err := reg.Execute(ctx, tools.ToolCall{Name: "whoami", IdempotencyKey: tt.key})
			if err != nil {
				t.Fatalf("Execute: %v", err)
			}
			if out.Result != tt.wantKey {
				t.Fatalf("server saw key %q, want %q", out.Result, tt.wantKey)
			}
		})
	}
}
