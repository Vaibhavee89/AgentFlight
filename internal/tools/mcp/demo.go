package mcp

import (
	"context"
	"encoding/json"
	"fmt"

	mcpsdk "github.com/modelcontextprotocol/go-sdk/mcp"
)

// demoServer returns a small MCP server offering one tool, "add", that sums
// two numbers. It exists so examples can run against a real stdio server
// without a separate program.
func demoServer() *mcpsdk.Server {
	srv := mcpsdk.NewServer(&mcpsdk.Implementation{Name: "agentflight-demo", Version: "0.1.0"}, nil)
	srv.AddTool(&mcpsdk.Tool{
		Name:        "add",
		Description: "adds two numbers, a and b",
		InputSchema: map[string]any{
			"type": "object",
			"properties": map[string]any{
				"a": map[string]any{"type": "number"},
				"b": map[string]any{"type": "number"},
			},
			"required": []string{"a", "b"},
		},
	}, func(ctx context.Context, req *mcpsdk.CallToolRequest) (*mcpsdk.CallToolResult, error) {
		var in struct {
			A float64 `json:"a"`
			B float64 `json:"b"`
		}
		if err := json.Unmarshal(req.Params.Arguments, &in); err != nil {
			return &mcpsdk.CallToolResult{
				IsError: true,
				Content: []mcpsdk.Content{&mcpsdk.TextContent{Text: "arguments must be numbers a and b"}},
			}, nil
		}
		return &mcpsdk.CallToolResult{
			Content: []mcpsdk.Content{&mcpsdk.TextContent{Text: fmt.Sprintf("%g", in.A+in.B)}},
		}, nil
	})
	return srv
}

// ServeDemoStdio serves the demo server over this process's stdin and stdout
// until the peer disconnects or ctx is done.
func ServeDemoStdio(ctx context.Context) error {
	if err := demoServer().Run(ctx, &mcpsdk.StdioTransport{}); err != nil {
		return fmt.Errorf("mcp: serve demo over stdio: %w", err)
	}
	return nil
}
