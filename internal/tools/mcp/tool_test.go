package mcp

import (
	"context"
	"encoding/json"
	"errors"
	"testing"
	"time"

	mcpsdk "github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/Vaibhavee89/agentflight/internal/policy"
	"github.com/Vaibhavee89/agentflight/internal/tools"
)

func TestRegister_ListsToolsWithSpecAndSchema(t *testing.T) {
	fs := newFakeServer(t)
	reg := tools.NewRegistry()

	names, err := Register(context.Background(), reg, fs.session)
	if err != nil {
		t.Fatalf("Register: %v", err)
	}
	want := map[string]bool{"echo": true, "fail": true, "hang": true}
	if len(names) != len(want) {
		t.Fatalf("registered %v, want 3 tools", names)
	}
	for _, n := range names {
		if !want[n] {
			t.Fatalf("unexpected registered tool %q", n)
		}
	}

	got, ok := reg.Get("echo")
	if !ok {
		t.Fatalf("echo not in registry")
	}
	if spec := got.Spec(); spec.Name != "echo" || spec.Description != "returns the text argument" {
		t.Fatalf("Spec() = %+v", spec)
	}
	schema := got.Spec().InputSchema
	var parsed struct {
		Type       string         `json:"type"`
		Properties map[string]any `json:"properties"`
	}
	if err := json.Unmarshal(schema, &parsed); err != nil {
		t.Fatalf("decode schema: %v", err)
	}
	if parsed.Type != "object" || parsed.Properties["text"] == nil {
		t.Fatalf("schema = %s, want object with a text property", schema)
	}
}

func TestRegister_DuplicateNameRejected(t *testing.T) {
	fs := newFakeServer(t)
	reg := tools.NewRegistry()
	if _, err := Register(context.Background(), reg, fs.session); err != nil {
		t.Fatalf("first Register: %v", err)
	}
	_, err := Register(context.Background(), reg, fs.session)
	if !errors.Is(err, tools.ErrDuplicateTool) {
		t.Fatalf("second Register error = %v, want wrapping %v", err, tools.ErrDuplicateTool)
	}
}

func TestExecute(t *testing.T) {
	tests := []struct {
		name        string
		tool        string
		input       string
		wantResult  string
		wantErr     error
		wantClass   policy.Disposition
		classifyErr bool
	}{
		{name: "echo returns argument text", tool: "echo", input: `{"text":"hello"}`, wantResult: "hello"},
		{
			name:        "tool-reported error is terminal",
			tool:        "fail",
			input:       `{}`,
			wantResult:  "disk is full",
			wantErr:     ErrToolReported,
			wantClass:   policy.Terminal,
			classifyErr: true,
		},
	}

	fs := newFakeServer(t)
	reg := registryWithFake(t, fs)

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tool, ok := reg.Get(tt.tool)
			if !ok {
				t.Fatalf("tool %q not registered", tt.tool)
			}
			out, err := tool.Execute(context.Background(), tools.ToolCall{Name: tt.tool, Input: json.RawMessage(tt.input)})
			if out.Result != tt.wantResult {
				t.Fatalf("Result = %q, want %q", out.Result, tt.wantResult)
			}
			if tt.wantErr == nil {
				if err != nil {
					t.Fatalf("Execute() unexpected error: %v", err)
				}
				return
			}
			if !errors.Is(err, tt.wantErr) {
				t.Fatalf("Execute() error = %v, want wrapping %v", err, tt.wantErr)
			}
			if tt.classifyErr && policy.Classify(err) != tt.wantClass {
				t.Fatalf("Classify(%v) = %v, want %v", err, policy.Classify(err), tt.wantClass)
			}
		})
	}
}

func hangTool(t *testing.T, fs *fakeServer) *Tool {
	t.Helper()
	reg := registryWithFake(t, fs)
	tool, ok := reg.Get("hang")
	if !ok {
		t.Fatalf("hang not registered")
	}
	return tool.(*Tool)
}

func TestExecute_DeadlineIsToolTimeoutAndRetryable(t *testing.T) {
	hang := hangTool(t, newFakeServer(t))
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Millisecond)
	defer cancel()

	_, err := hang.Execute(ctx, tools.ToolCall{Name: "hang", Input: json.RawMessage(`{}`)})
	if !errors.Is(err, policy.ErrToolTimeout) {
		t.Fatalf("Execute() error = %v, want wrapping %v", err, policy.ErrToolTimeout)
	}
	if got := policy.Classify(err); got != policy.Retryable {
		t.Fatalf("Classify() = %v, want Retryable", got)
	}
}

func TestExecute_CancellationIsTerminal(t *testing.T) {
	hang := hangTool(t, newFakeServer(t))
	ctx, cancel := context.WithCancel(context.Background())
	time.AfterFunc(20*time.Millisecond, cancel)

	_, err := hang.Execute(ctx, tools.ToolCall{Name: "hang", Input: json.RawMessage(`{}`)})
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("Execute() error = %v, want wrapping %v", err, context.Canceled)
	}
	if got := policy.Classify(err); got != policy.Terminal {
		t.Fatalf("Classify() = %v, want Terminal", got)
	}
}

func TestExecute_BrokenTransportIsTransientAndRetryable(t *testing.T) {
	fs := newFakeServer(t)
	reg := registryWithFake(t, fs)
	echo, _ := reg.Get("echo")
	if err := fs.session.Close(); err != nil {
		t.Fatalf("close session: %v", err)
	}

	_, err := echo.Execute(context.Background(), tools.ToolCall{Name: "echo", Input: json.RawMessage(`{"text":"x"}`)})
	if !errors.Is(err, policy.ErrTransient) {
		t.Fatalf("Execute() error = %v, want wrapping %v", err, policy.ErrTransient)
	}
	if got := policy.Classify(err); got != policy.Retryable {
		t.Fatalf("Classify() = %v, want Retryable", got)
	}
}

func TestExecute_UnknownToolIsTerminal(t *testing.T) {
	fs := newFakeServer(t)
	ghost := &Tool{
		session: fs.session,
		def:     &mcpsdk.Tool{Name: "no-such-tool", InputSchema: objectSchema(map[string]any{})},
	}

	_, err := ghost.Execute(context.Background(), tools.ToolCall{Name: "no-such-tool", Input: json.RawMessage(`{}`)})
	if err == nil {
		t.Fatalf("Execute() on an unknown tool returned nil error")
	}
	if got := policy.Classify(err); got != policy.Terminal {
		t.Fatalf("Classify(%v) = %v, want Terminal", err, got)
	}
}
