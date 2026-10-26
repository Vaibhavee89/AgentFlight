package mcp

import (
	"context"
	"fmt"
	"net/http"
	"os/exec"

	mcpsdk "github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/Vaibhavee89/agentflight/internal/tools"
)

// ClientInfo identifies this client to MCP servers during initialization.
var ClientInfo = &mcpsdk.Implementation{Name: "agentflight", Version: "0.1.0"}

// ConnectStdio starts cmd and connects to it as an MCP client over its
// stdin and stdout. Use exec.Command rather than exec.CommandContext: the
// session owns the process and shuts it down via Close, and a context-bound
// command would be killed as soon as ctx is cancelled mid-run.
func ConnectStdio(ctx context.Context, cmd *exec.Cmd) (*mcpsdk.ClientSession, error) {
	return connect(ctx, &mcpsdk.CommandTransport{Command: cmd})
}

// ConnectHTTP connects to an MCP server's streamable HTTP endpoint. A nil
// httpClient uses http.DefaultClient.
func ConnectHTTP(ctx context.Context, endpoint string, httpClient *http.Client) (*mcpsdk.ClientSession, error) {
	return connect(ctx, &mcpsdk.StreamableClientTransport{Endpoint: endpoint, HTTPClient: httpClient})
}

func connect(ctx context.Context, t mcpsdk.Transport) (*mcpsdk.ClientSession, error) {
	client := mcpsdk.NewClient(ClientInfo, nil)
	session, err := client.Connect(ctx, t, nil)
	if err != nil {
		return nil, fmt.Errorf("mcp: connect: %w", err)
	}
	return session, nil
}

// Register lists every tool the session offers, following pagination, and
// registers each as a Tool in reg. It returns the names registered, in the
// order the server listed them. A name already in reg is an error.
func Register(ctx context.Context, reg *tools.Registry, session *mcpsdk.ClientSession) ([]string, error) {
	var names []string
	cursor := ""
	for {
		res, err := session.ListTools(ctx, &mcpsdk.ListToolsParams{Cursor: cursor})
		if err != nil {
			return names, fmt.Errorf("mcp: list tools: %w", err)
		}
		for _, def := range res.Tools {
			tool, err := newTool(session, def)
			if err != nil {
				return names, err
			}
			if err := reg.Register(tool); err != nil {
				return names, fmt.Errorf("mcp: register tool %q: %w", def.Name, err)
			}
			names = append(names, def.Name)
		}
		if res.NextCursor == "" {
			return names, nil
		}
		cursor = res.NextCursor
	}
}
