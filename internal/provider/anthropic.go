package provider

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
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
	anthropicMaxTokens      = 4096
)

// ErrAnthropicRefusal indicates the model declined the request
// (stop_reason "refusal"). Retrying the same request will not help.
var ErrAnthropicRefusal = errors.New("anthropic: model refused the request")

// AnthropicProvider implements ModelProvider against the Anthropic Messages
// API using only net/http (no SDK).
type AnthropicProvider struct {
	apiKey  string
	model   string
	baseURL string
	client  *http.Client
	tools   []anthropicTool
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

// WithAnthropicTools advertises specs to the model so it can request them.
// A spec with no InputSchema is sent with an empty object schema.
func WithAnthropicTools(specs []tools.ToolSpec) AnthropicOption {
	return func(p *AnthropicProvider) {
		p.tools = make([]anthropicTool, 0, len(specs))
		for _, spec := range specs {
			schema := spec.InputSchema
			if len(schema) == 0 {
				schema = json.RawMessage(`{"type":"object"}`)
			}
			p.tools = append(p.tools, anthropicTool{Name: spec.Name, Description: spec.Description, InputSchema: schema})
		}
	}
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
	Tools     []anthropicTool    `json:"tools,omitempty"`
}

type anthropicTool struct {
	Name        string          `json:"name"`
	Description string          `json:"description,omitempty"`
	InputSchema json.RawMessage `json:"input_schema"`
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
	IsError   bool            `json:"is_error,omitempty"`
}

type anthropicResponse struct {
	Content    []anthropicContent `json:"content"`
	StopReason string             `json:"stop_reason"`
	Usage      anthropicUsage     `json:"usage"`
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
	body, err := json.Marshal(buildAnthropicRequest(p.model, p.tools, req))
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

	if wire.StopReason == "refusal" {
		return ModelResponse{}, fmt.Errorf("%w", ErrAnthropicRefusal)
	}

	return toModelResponse(wire), nil
}

// toolUseID is the id given to the i-th tool call of a step. A Request does
// not carry the ids the API issued, so the ids are derived from position,
// which keeps the request identical when a run is resumed or replayed. The
// API only needs each tool_result to match a tool_use id in the turn before it.
func toolUseID(i int) string { return fmt.Sprintf("toolu_%d", i) }

// buildAnthropicRequest converts req into one Messages API request. Tool
// results from the prior step are sent the way the API requires: an assistant
// turn holding the tool_use blocks, then a user turn holding the matching
// tool_result blocks. A failed tool is reported with is_error so the model can
// react to it.
func buildAnthropicRequest(model string, specs []anthropicTool, req Request) anthropicRequest {
	messages := []anthropicMessage{
		{Role: "user", Content: []anthropicContent{{Type: "text", Text: req.Prompt}}},
	}

	if len(req.ToolResults) > 0 {
		uses := make([]anthropicContent, 0, len(req.ToolResults))
		results := make([]anthropicContent, 0, len(req.ToolResults))
		for i, tr := range req.ToolResults {
			id := toolUseID(i)
			input := tr.Call.Input
			if len(input) == 0 {
				input = json.RawMessage(`{}`)
			}
			uses = append(uses, anthropicContent{Type: "tool_use", ID: id, Name: tr.Call.Name, Input: input})

			block := anthropicContent{Type: "tool_result", ToolUseID: id, Content: tr.Output.Result}
			if tr.Err != nil {
				block.Content = "error: " + tr.Err.Error()
				block.IsError = true
			}
			results = append(results, block)
		}
		messages = append(messages,
			anthropicMessage{Role: "assistant", Content: uses},
			anthropicMessage{Role: "user", Content: results},
		)
	}

	return anthropicRequest{Model: model, MaxTokens: anthropicMaxTokens, Messages: messages, Tools: specs}
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
