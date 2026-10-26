package mcp

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	mcpsdk "github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/Vaibhavee89/agentflight/internal/tools"
)

// TestConnectHTTP serves the fake MCP server over the SDK's streamable HTTP
// handler on a loopback httptest listener, then connects with ConnectHTTP.
// Nothing leaves the test process.
func TestConnectHTTP(t *testing.T) {
	handler := mcpsdk.NewStreamableHTTPHandler(func(*http.Request) *mcpsdk.Server {
		return buildFakeServer()
	}, nil)
	ts := httptest.NewServer(handler)
	defer ts.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()

	session, err := ConnectHTTP(ctx, ts.URL, ts.Client())
	if err != nil {
		t.Fatalf("ConnectHTTP: %v", err)
	}
	defer session.Close()

	reg := tools.NewRegistry()
	names, err := Register(ctx, reg, session)
	if err != nil {
		t.Fatalf("Register: %v", err)
	}
	if len(names) != 3 {
		t.Fatalf("registered %v, want 3 tools", names)
	}

	echo, _ := reg.Get("echo")
	out, err := echo.Execute(ctx, tools.ToolCall{Name: "echo", Input: json.RawMessage(`{"text":"over http"}`)})
	if err != nil {
		t.Fatalf("Execute over HTTP: %v", err)
	}
	if out.Result != "over http" {
		t.Fatalf("Result = %q, want %q", out.Result, "over http")
	}
}
