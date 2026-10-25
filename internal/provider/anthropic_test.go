package provider

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"

	"github.com/Vaibhavee89/agentflight/internal/policy"
)

func TestAnthropicProvider_Complete_SuccessWithToolUse(t *testing.T) {
	const body = `{
		"content": [
			{"type": "text", "text": "Let me check that."},
			{"type": "tool_use", "id": "tool_1", "name": "echo", "input": {"text":"hi"}}
		],
		"usage": {"input_tokens": 10, "output_tokens": 5}
	}`

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost || r.URL.Path != "/v1/messages" {
			t.Errorf("unexpected request: %s %s", r.Method, r.URL.Path)
		}
		if got := r.Header.Get("x-api-key"); got != "test-key" {
			t.Errorf("x-api-key = %q, want %q", got, "test-key")
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		w.Write([]byte(body))
	}))
	defer srv.Close()

	p := NewAnthropicProvider("claude-3-5-haiku-20241022",
		WithAnthropicBaseURL(srv.URL),
		WithAnthropicAPIKey("test-key"),
		WithAnthropicClient(srv.Client()),
	)

	resp, err := p.Complete(context.Background(), Request{Prompt: "hi"})
	if err != nil {
		t.Fatalf("Complete() unexpected error: %v", err)
	}
	if resp.Content != "Let me check that." {
		t.Fatalf("Content = %q, want %q", resp.Content, "Let me check that.")
	}
	if len(resp.ToolCalls) != 1 || resp.ToolCalls[0].Name != "echo" {
		t.Fatalf("ToolCalls = %+v, want one call named echo", resp.ToolCalls)
	}
	var in struct {
		Text string `json:"text"`
	}
	if err := json.Unmarshal(resp.ToolCalls[0].Input, &in); err != nil {
		t.Fatalf("unmarshal tool input: %v", err)
	}
	if in.Text != "hi" {
		t.Fatalf("tool input text = %q, want %q", in.Text, "hi")
	}
	if resp.Usage.InputTokens != 10 || resp.Usage.OutputTokens != 5 {
		t.Fatalf("Usage = %+v, want {10 5}", resp.Usage)
	}
}

func TestAnthropicProvider_Complete_RetryableThenSuccess(t *testing.T) {
	var calls int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if atomic.AddInt32(&calls, 1) == 1 {
			w.WriteHeader(http.StatusTooManyRequests)
			w.Write([]byte(`{"type":"error","error":{"type":"rate_limit_error","message":"slow down"}}`))
			return
		}
		w.WriteHeader(http.StatusOK)
		w.Write([]byte(`{"content":[{"type":"text","text":"ok"}],"usage":{"input_tokens":1,"output_tokens":1}}`))
	}))
	defer srv.Close()

	p := NewAnthropicProvider("claude-3-5-haiku-20241022", WithAnthropicBaseURL(srv.URL), WithAnthropicClient(srv.Client()))

	_, err := p.Complete(context.Background(), Request{Prompt: "hi"})
	if err == nil {
		t.Fatalf("Complete() expected error on first (429) call, got nil")
	}
	if policy.Classify(err) != policy.Retryable {
		t.Fatalf("Classify(err) = %v, want Retryable", policy.Classify(err))
	}

	resp, err := p.Complete(context.Background(), Request{Prompt: "hi"})
	if err != nil {
		t.Fatalf("Complete() unexpected error on second call: %v", err)
	}
	if resp.Content != "ok" {
		t.Fatalf("Content = %q, want %q", resp.Content, "ok")
	}
	if calls != 2 {
		t.Fatalf("server called %d times, want 2", calls)
	}
}

func TestAnthropicProvider_Complete_TerminalClientError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusBadRequest)
		w.Write([]byte(`{"type":"error","error":{"type":"invalid_request_error","message":"bad request"}}`))
	}))
	defer srv.Close()

	p := NewAnthropicProvider("claude-3-5-haiku-20241022", WithAnthropicBaseURL(srv.URL), WithAnthropicClient(srv.Client()))

	_, err := p.Complete(context.Background(), Request{Prompt: "hi"})
	if err == nil {
		t.Fatalf("Complete() expected error, got nil")
	}
	if policy.Classify(err) != policy.Terminal {
		t.Fatalf("Classify(err) = %v, want Terminal", policy.Classify(err))
	}

	var statusErr *policy.StatusError
	if !errors.As(err, &statusErr) || statusErr.StatusCode != http.StatusBadRequest {
		t.Fatalf("Complete() error = %v, want wrapping *policy.StatusError{400}", err)
	}
}

func TestAnthropicProvider_Complete_ContextCancelledDuringSlowRequest(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		select {
		case <-r.Context().Done():
		case <-time.After(500 * time.Millisecond):
		}
	}))
	defer srv.Close()

	p := NewAnthropicProvider("claude-3-5-haiku-20241022", WithAnthropicBaseURL(srv.URL), WithAnthropicClient(srv.Client()))

	ctx, cancel := context.WithCancel(context.Background())
	go func() {
		time.Sleep(20 * time.Millisecond)
		cancel()
	}()

	start := time.Now()
	_, err := p.Complete(ctx, Request{Prompt: "hi"})
	elapsed := time.Since(start)
	srv.CloseClientConnections() // let the blocked handler unwind before deferred srv.Close()

	if !errors.Is(err, context.Canceled) {
		t.Fatalf("Complete() error = %v, want wrapping %v", err, context.Canceled)
	}
	if elapsed > 200*time.Millisecond {
		t.Fatalf("Complete() took %v, want well under the server's 5s delay", elapsed)
	}
}
