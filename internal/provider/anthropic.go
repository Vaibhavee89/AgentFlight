package provider

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"

	"github.com/Vaibhavee89/agentflight/internal/policy"
	"github.com/Vaibhavee89/agentflight/internal/tools"
)

const (
	defaultAnthropicBaseURL = "https://api.anthropic.com"
	anthropicVersion        = "2023-06-01"
	anthropicMaxTokens      = 1024
)

// AnthropicProvider implements ModelProvider against the Anthropic Messages
// API using only net/http (no SDK).
type AnthropicProvider struct {
	apiKey  string
	model   string
	baseURL string
	client  *http.Client
}

// AnthropicOption configures an AnthropicProvider.
type AnthropicOption func(*AnthropicProvider)

// WithAnthropicAPIKey overrides the API key read from ANTHROPIC_API_KEY.
func WithAnthropicAPIKey(key string) AnthropicOption {
	return func(p *AnthropicProvider) { p.apiKey = key }
}

// WithAnthropicBaseURL overrides the API base URL, so tests can point it at
// an httptest.Server.
func WithAnthropicBaseURL(url string) AnthropicOption {
	return func(p *AnthropicProvider) { p.baseURL = url }
}

// WithAnthropicClient overrides the *http.Client used to send requests.
func WithAnthropicClient(c *http.Client) AnthropicOption {
	return func(p *AnthropicProvider) { p.client = c }
}

// NewAnthropicProvider builds an AnthropicProvider for model, reading the
// API key from the ANTHROPIC_API_KEY environment variable unless
// overridden with WithAnthropicAPIKey.
func NewAnthropicProvider(model string, opts ...AnthropicOption) *AnthropicProvider {
	p := &AnthropicProvider{
		apiKey:  os.Getenv("ANTHROPIC_API_KEY"),
		model:   model,
		baseURL: defaultAnthropicBaseURL,
		client:  http.DefaultClient,
	}
	for _, opt := range opts {
		opt(p)
	}
	return p
}

type anthropicRequest struct {
	Model     string             `json:"model"`
	MaxTokens int                `json:"max_tokens"`
	Messages  []anthropicMessage `json:"messages"`
}

type anthropicMessage struct {
	Role    string             `json:"role"`
	Content []anthropicContent `json:"content"`
}

// anthropicContent is a single content block, used for both text and
// tool_use (response) and tool_result (request) blocks.
type anthropicContent struct {
	Type      string          `json:"type"`
	Text      string          `json:"text,omitempty"`
	ID        string          `json:"id,omitempty"`
	Name      string          `json:"name,omitempty"`
	Input     json.RawMessage `json:"input,omitempty"`
	ToolUseID string          `json:"tool_use_id,omitempty"`
	Content   string          `json:"content,omitempty"`
}

type anthropicResponse struct {
	Content []anthropicContent `json:"content"`
	Usage   anthropicUsage     `json:"usage"`
}

type anthropicUsage struct {
	InputTokens  int `json:"input_tokens"`
	OutputTokens int `json:"output_tokens"`
}

type anthropicErrorResponse struct {
	Error struct {
		Type    string `json:"type"`
		Message string `json:"message"`
	} `json:"error"`
}

// Complete sends req to the Anthropic Messages API and maps the response
// back to a ModelResponse. HTTP 429 and 5xx responses, and network errors,
// are wrapped so policy.Classify treats them as Retryable; other 4xx
// responses are wrapped so policy.Classify treats them as Terminal.
func (p *AnthropicProvider) Complete(ctx context.Context, req Request) (ModelResponse, error) {
	body, err := json.Marshal(buildAnthropicRequest(p.model, req))
	if err != nil {
		return ModelResponse{}, fmt.Errorf("anthropic: encode request: %w", err)
	}

	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, p.baseURL+"/v1/messages", bytes.NewReader(body))
	if err != nil {
		return ModelResponse{}, fmt.Errorf("anthropic: build request: %w", err)
	}
	httpReq.Header.Set("content-type", "application/json")
	httpReq.Header.Set("x-api-key", p.apiKey)
	httpReq.Header.Set("anthropic-version", anthropicVersion)

	resp, err := p.client.Do(httpReq)
	if err != nil {
		return ModelResponse{}, fmt.Errorf("anthropic: request: %w", err)
	}
	defer resp.Body.Close()

	data, err := io.ReadAll(resp.Body)
	if err != nil {
		return ModelResponse{}, fmt.Errorf("anthropic: read response: %w", err)
	}

	if resp.StatusCode != http.StatusOK {
		return ModelResponse{}, anthropicStatusError(resp.StatusCode, data)
	}

	var wire anthropicResponse
	if err := json.Unmarshal(data, &wire); err != nil {
		return ModelResponse{}, fmt.Errorf("anthropic: decode response: %w", err)
	}

	return toModelResponse(wire), nil
}

// buildAnthropicRequest converts req into one Messages API request. Tool
// results from a prior step, if any, are sent as a follow-up user message.
func buildAnthropicRequest(model string, req Request) anthropicRequest {
	messages := []anthropicMessage{
		{Role: "user", Content: []anthropicContent{{Type: "text", Text: req.Prompt}}},
	}

	if len(req.ToolResults) > 0 {
		blocks := make([]anthropicContent, 0, len(req.ToolResults))
		for _, tr := range req.ToolResults {
			content := tr.Output.Result
			if tr.Err != nil {
				content = "error: " + tr.Err.Error()
			}
			blocks = append(blocks, anthropicContent{
				Type:      "tool_result",
				ToolUseID: tr.Call.Name,
				Content:   content,
			})
		}
		messages = append(messages, anthropicMessage{Role: "user", Content: blocks})
	}

	return anthropicRequest{Model: model, MaxTokens: anthropicMaxTokens, Messages: messages}
}

// toModelResponse maps a Messages API response's text and tool_use content
// blocks to a ModelResponse, concatenating text blocks and parsing usage.
func toModelResponse(wire anthropicResponse) ModelResponse {
	var text strings.Builder
	var calls []tools.ToolCall

	for _, block := range wire.Content {
		switch block.Type {
		case "text":
			text.WriteString(block.Text)
		case "tool_use":
			calls = append(calls, tools.ToolCall{Name: block.Name, Input: block.Input})
		}
	}

	return ModelResponse{
		Content:   text.String(),
		ToolCalls: calls,
		Usage: Usage{
			InputTokens:  wire.Usage.InputTokens,
			OutputTokens: wire.Usage.OutputTokens,
		},
	}
}

// anthropicStatusError wraps a non-200 response as a *policy.StatusError, so
// policy.Classify treats 429 and 5xx as Retryable and other 4xx as Terminal.
func anthropicStatusError(statusCode int, body []byte) error {
	message := strings.TrimSpace(string(body))
	var errResp anthropicErrorResponse
	if err := json.Unmarshal(body, &errResp); err == nil && errResp.Error.Message != "" {
		message = errResp.Error.Message
	}
	return fmt.Errorf("anthropic: status %d: %s: %w", statusCode, message, &policy.StatusError{StatusCode: statusCode})
}
