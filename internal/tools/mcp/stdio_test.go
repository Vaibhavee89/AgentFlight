package mcp

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"testing"
	"time"

	mcpsdk "github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/Vaibhavee89/agentflight/internal/tools"
)

const helperEnv = "AGENTFLIGHT_MCP_HELPER"

// TestStdioHelperProcess is not a test of its own. TestConnectStdio launches
// the test binary running only this function, which then serves the fake MCP
// server over its stdin and stdout. Without the environment variable it does
// nothing, so a normal test run skips it.
func TestStdioHelperProcess(t *testing.T) {
	if os.Getenv(helperEnv) != "1" {
		t.Skip("helper process only")
	}
	if err := buildFakeServer().Run(context.Background(), &mcpsdk.StdioTransport{}); err != nil {
		fmt.Fprintln(os.Stderr, "helper:", err)
		os.Exit(1)
	}
	os.Exit(0)
}

// TestConnectStdio connects to a real subprocess over stdin and stdout,
// lists its tools, and calls one. The subprocess is this test binary, so no
// network access or external program is needed.
func TestConnectStdio(t *testing.T) {
	cmd := exec.Command(os.Args[0], "-test.run=^TestStdioHelperProcess$")
	cmd.Env = append(os.Environ(), helperEnv+"=1")

	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()

	session, err := ConnectStdio(ctx, cmd)
	if err != nil {
		t.Fatalf("ConnectStdio: %v", err)
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

	echo, ok := reg.Get("echo")
	if !ok {
		t.Fatalf("echo not registered")
	}
	out, err := echo.Execute(ctx, tools.ToolCall{Name: "echo", Input: json.RawMessage(`{"text":"over stdio"}`)})
	if err != nil {
		t.Fatalf("Execute over stdio: %v", err)
	}
	if out.Result != "over stdio" {
		t.Fatalf("Result = %q, want %q", out.Result, "over stdio")
	}
}
