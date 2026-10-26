package mcp

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"os"
	"strings"

	mcpsdk "github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/Vaibhavee89/agentflight/internal/policy"
	"github.com/Vaibhavee89/agentflight/internal/tools"
)

// ErrToolReported marks a result in which the MCP server flagged the tool
// call as failed. It is terminal: retrying the same arguments would fail the
// same way, and the tool's own message is included in the error text.
var ErrToolReported = errors.New("mcp: tool reported an error")

// Tool is one tool served by an MCP session, exposed as a tools.Tool.
type Tool struct {
	session *mcpsdk.ClientSession
	def     *mcpsdk.Tool
	schema  json.RawMessage
}

// newTool wraps def, encoding its input schema once so Spec stays infallible.
func newTool(session *mcpsdk.ClientSession, def *mcpsdk.Tool) (*Tool, error) {
	schema, err := json.Marshal(def.InputSchema)
	if err != nil {
		return nil, fmt.Errorf("mcp: encode input schema for %q: %w", def.Name, err)
	}
	return &Tool{session: session, def: def, schema: schema}, nil
}

// Spec returns the tool's name, description, and input schema.
func (t *Tool) Spec() tools.ToolSpec {
	return tools.ToolSpec{Name: t.def.Name, Description: t.def.Description, InputSchema: t.schema}
}

// Execute calls the tool on the server with call.Input as its arguments. ctx
// governs the call, so cancellation and a per-tool deadline from the policy
// both stop it.
func (t *Tool) Execute(ctx context.Context, call tools.ToolCall) (tools.ToolOutput, error) {
	params := &mcpsdk.CallToolParams{Name: t.def.Name}
	if len(call.Input) > 0 {
		params.Arguments = json.RawMessage(call.Input)
	}

	res, err := t.session.CallTool(ctx, params)
	if err != nil {
		return tools.ToolOutput{}, mapError(ctx, t.def.Name, err)
	}

	out := resultText(res)
	if res.IsError {
		return tools.ToolOutput{Result: out}, fmt.Errorf("mcp: tool %q: %s: %w", t.def.Name, out, ErrToolReported)
	}
	return tools.ToolOutput{Result: out}, nil
}

// mapError translates a failed CallTool into the project's typed errors:
//   - the call's deadline expired      -> policy.ErrToolTimeout (retryable)
//   - the call's context was cancelled -> context.Canceled (terminal)
//   - the connection or transport broke -> policy.ErrTransient (retryable)
//   - anything else, including JSON-RPC errors from the server -> terminal
//
// The timeout case deliberately does not wrap context.DeadlineExceeded:
// policy.Classify checks that first and would call the timeout terminal.
func mapError(ctx context.Context, name string, err error) error {
	switch {
	case errors.Is(ctx.Err(), context.DeadlineExceeded):
		return fmt.Errorf("mcp: tool %q: %w", name, policy.ErrToolTimeout)
	case ctx.Err() != nil:
		return fmt.Errorf("mcp: tool %q: %w", name, ctx.Err())
	case isTransportFailure(err):
		return fmt.Errorf("mcp: tool %q: %w: %w", name, policy.ErrTransient, err)
	default:
		return fmt.Errorf("mcp: tool %q: %w", name, err)
	}
}

func isTransportFailure(err error) bool {
	var netErr net.Error
	return errors.Is(err, mcpsdk.ErrConnectionClosed) ||
		errors.Is(err, io.EOF) ||
		errors.Is(err, io.ErrUnexpectedEOF) ||
		errors.Is(err, io.ErrClosedPipe) ||
		errors.Is(err, os.ErrClosed) ||
		errors.As(err, &netErr)
}

// resultText joins the text content of a result. If the result has no text
// but has structured content, that is returned as JSON instead.
func resultText(res *mcpsdk.CallToolResult) string {
	var b strings.Builder
	for _, c := range res.Content {
		if tc, ok := c.(*mcpsdk.TextContent); ok {
			b.WriteString(tc.Text)
		}
	}
	if b.Len() == 0 && res.StructuredContent != nil {
		if data, err := json.Marshal(res.StructuredContent); err == nil {
			return string(data)
		}
	}
	return b.String()
}
