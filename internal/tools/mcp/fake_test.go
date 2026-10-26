package mcp

import (
	"context"
	"encoding/json"
	"testing"

	mcpsdk "github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/Vaibhavee89/agentflight/internal/tools"
)

// fakeServer is an in-process MCP server offering three tools over the SDK's
// in-memory transport. No network and no subprocess are involved.
//   - echo:  returns its "text" argument.
//   - fail:  returns a tool-level error result (IsError set).
//   - hang:  blocks until its call context is done.
type fakeServer struct {
	server        *mcpsdk.Server
	session       *mcpsdk.ClientSession
	serverSession *mcpsdk.ServerSession
}

func textResult(text string) *mcpsdk.CallToolResult {
	return &mcpsdk.CallToolResult{Content: []mcpsdk.Content{&mcpsdk.TextContent{Text: text}}}
}

func objectSchema(props map[string]any) map[string]any {
	return map[string]any{"type": "object", "properties": props}
}

// buildFakeServer returns an MCP server offering the echo, fail, and hang tools.
func buildFakeServer() *mcpsdk.Server {
	srv := mcpsdk.NewServer(&mcpsdk.Implementation{Name: "fake", Version: "0.0.1"}, nil)
	srv.AddTool(&mcpsdk.Tool{
		Name:        "echo",
		Description: "returns the text argument",
		InputSchema: objectSchema(map[string]any{"text": map[string]any{"type": "string"}}),
	}, func(ctx context.Context, req *mcpsdk.CallToolRequest) (*mcpsdk.CallToolResult, error) {
		var in struct {
			Text string `json:"text"`
		}
		if err := json.Unmarshal(req.Params.Arguments, &in); err != nil {
			return nil, err
		}
		return textResult(in.Text), nil
	})
	srv.AddTool(&mcpsdk.Tool{
		Name:        "fail",
		Description: "always reports a tool error",
		InputSchema: objectSchema(map[string]any{}),
	}, func(ctx context.Context, req *mcpsdk.CallToolRequest) (*mcpsdk.CallToolResult, error) {
		res := textResult("disk is full")
		res.IsError = true
		return res, nil
	})
	srv.AddTool(&mcpsdk.Tool{
		Name:        "hang",
		Description: "never returns on its own",
		InputSchema: objectSchema(map[string]any{}),
	}, func(ctx context.Context, req *mcpsdk.CallToolRequest) (*mcpsdk.CallToolResult, error) {
		<-ctx.Done()
		return nil, ctx.Err()
	})
	return srv
}

func newFakeServer(t *testing.T) *fakeServer {
	t.Helper()
	ctx := context.Background()

	srv := buildFakeServer()
	serverTransport, clientTransport := mcpsdk.NewInMemoryTransports()
	serverSession, err := srv.Connect(ctx, serverTransport, nil)
	if err != nil {
		t.Fatalf("server connect: %v", err)
	}
	client := mcpsdk.NewClient(ClientInfo, nil)
	session, err := client.Connect(ctx, clientTransport, nil)
	if err != nil {
		t.Fatalf("client connect: %v", err)
	}
	t.Cleanup(func() {
		session.Close()
		serverSession.Close()
	})
	return &fakeServer{server: srv, session: session, serverSession: serverSession}
}

// registryWithFake returns a registry holding every tool the fake server offers.
func registryWithFake(t *testing.T, fs *fakeServer) *tools.Registry {
	t.Helper()
	reg := tools.NewRegistry()
	if _, err := Register(context.Background(), reg, fs.session); err != nil {
		t.Fatalf("Register: %v", err)
	}
	return reg
}
