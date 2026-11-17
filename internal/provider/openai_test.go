package provider

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/Vaibhavee89/agentflight/internal/policy"
	"github.com/Vaibhavee89/agentflight/internal/tools"
)

func TestBuildOpenAIRequest(t *testing.T) {
	tests := []struct {
		name      string
		req       Request
		wantRoles string
		wantTool  string
	}{
		{name: "prompt only", req: Request{Prompt: "hi"}, wantRoles: "user"},
		{
			name: "tool result follows an assistant tool_calls turn",
			req: Request{Prompt: "hi", ToolResults: []ToolResult{
				{Call: tools.ToolCall{Name: "echo", Input: json.RawMessage(`{"text":"x"}`)}, Output: tools.ToolOutput{Result: "x"}},
			}},
			wantRoles: "user,assistant,tool",
			wantTool:  "x",
		},
		{
			name: "failed tool is reported as an error string",
			req: Request{Prompt: "hi", ToolResults: []ToolResult{
				{Call: tools.ToolCall{Name: "calculator"}, Err: errors.New("boom")},
			}},
			wantRoles: "user,assistant,tool",
			wantTool:  "error: boom",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := buildOpenAIRequest("m", nil, tt.req)
			var roles []string
			for _, m := range got.Messages {
				roles = append(roles, m.Role)
			}
			if strings.Join(roles, ",") != tt.wantRoles {
				t.Fatalf("roles = %v, want %s", roles, tt.wantRoles)
			}
			if tt.wantTool == "" {
				return
			}
			call := got.Messages[1].ToolCalls[0]
			result := got.Messages[2]
			if result.ToolCallID != call.ID || result.Content != tt.wantTool {
				t.Fatalf("tool message = %+v, want id %q content %q", result, call.ID, tt.wantTool)
			}
		})
	}
}

func TestOpenAICompatProvider_Complete(t *testing.T) {
	tests := []struct {
		name         string
		status       int
		body         string
		wantContent  string
		wantCallName string
		wantInput    string
		wantClass    policy.Disposition
		checkClass   bool
		wantErr      bool
	}{
		{
			name:        "text answer",
			status:      200,
			body:        `{"choices":[{"message":{"role":"assistant","content":"hello"}}],"usage":{"prompt_tokens":3,"completion_tokens":2}}`,
			wantContent: "hello",
		},
		{
			name:         "tool call with string arguments",
			status:       200,
			body:         `{"choices":[{"message":{"role":"assistant","content":"","tool_calls":[{"id":"c1","type":"function","function":{"name":"echo","arguments":"{\"text\":\"hi\"}"}}]}}]}`,
			wantCallName: "echo",
			wantInput:    `{"text":"hi"}`,
		},
		{
			name:         "invalid arguments are passed on as a json string",
			status:       200,
			body:         `{"choices":[{"message":{"tool_calls":[{"id":"c1","type":"function","function":{"name":"echo","arguments":"not json"}}]}}]}`,
			wantCallName: "echo",
			wantInput:    `"not json"`,
		},
		{name: "rate limited is retryable", status: 429, body: `{"error":"slow"}`, wantErr: true, checkClass: true, wantClass: policy.Retryable},
		{name: "bad request is terminal", status: 400, body: `{"error":"nope"}`, wantErr: true, checkClass: true, wantClass: policy.Terminal},
		{name: "no choices", status: 200, body: `{"choices":[]}`, wantErr: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var gotPath, gotAuth string
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				gotPath, gotAuth = r.URL.Path, r.Header.Get("authorization")
				w.WriteHeader(tt.status)
				w.Write([]byte(tt.body))
			}))
			defer srv.Close()

			p := NewOpenAICompatProvider(srv.URL+"/v1/", "m",
				WithOpenAICompatClient(srv.Client()), WithOpenAICompatAPIKey("k"),
				WithOpenAICompatTools([]tools.ToolSpec{{Name: "echo"}}))

			resp, err := p.Complete(context.Background(), Request{Prompt: "hi"})
			if gotPath != "/v1/chat/completions" || gotAuth != "Bearer k" {
				t.Fatalf("path = %q, auth = %q", gotPath, gotAuth)
			}
			if tt.wantErr {
				if err == nil {
					t.Fatal("expected error, got nil")
				}
				if tt.checkClass && policy.Classify(err) != tt.wantClass {
					t.Fatalf("Classify = %v, want %v", policy.Classify(err), tt.wantClass)
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if resp.Content != tt.wantContent {
				t.Fatalf("Content = %q, want %q", resp.Content, tt.wantContent)
			}
			if tt.wantCallName != "" {
				if len(resp.ToolCalls) != 1 || resp.ToolCalls[0].Name != tt.wantCallName || string(resp.ToolCalls[0].Input) != tt.wantInput {
					t.Fatalf("ToolCalls = %+v, want %s %s", resp.ToolCalls, tt.wantCallName, tt.wantInput)
				}
			}
		})
	}
}
