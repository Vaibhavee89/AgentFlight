package provider

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"

	"github.com/Vaibhavee89/agentflight/internal/policy"
	"github.com/Vaibhavee89/agentflight/internal/tools"
)

// OpenAICompatProvider implements ModelProvider against any server that speaks
// the OpenAI chat completions API: Ollama, LM Studio, vLLM, llama.cpp, and
// hosted open-model services. It uses only net/http.
type OpenAICompatProvider struct {
	apiKey  string
	model   string
	baseURL string
	client  *http.Client
	tools   []openAITool
}

// OpenAICompatOption configures an OpenAICompatProvider.
type OpenAICompatOption func(*OpenAICompatProvider)

// WithOpenAICompatAPIKey sets a bearer token. Local servers such as Ollama
// do not need one.
func WithOpenAICompatAPIKey(key string) OpenAICompatOption {
	return func(p *OpenAICompatProvider) { p.apiKey = key }
}

// WithOpenAICompatClient overrides the *http.Client used to send requests.
func WithOpenAICompatClient(c *http.Client) OpenAICompatOption {
	return func(p *OpenAICompatProvider) { p.client = c }
}

// WithOpenAICompatTools advertises specs to the model. A spec with no
// InputSchema is sent with an empty object schema.
func WithOpenAICompatTools(specs []tools.ToolSpec) OpenAICompatOption {
	return func(p *OpenAICompatProvider) {
		p.tools = make([]openAITool, 0, len(specs))
		for _, spec := range specs {
			schema := spec.InputSchema
			if len(schema) == 0 {
				schema = json.RawMessage(`{"type":"object"}`)
			}
			p.tools = append(p.tools, openAITool{
				Type:     "function",
				Function: openAIFunctionDef{Name: spec.Name, Description: spec.Description, Parameters: schema},
			})
		}
	}
}

// NewOpenAICompatProvider builds a provider for model at baseURL, which is the
// API root including any version path, for example http://localhost:11434/v1.
func NewOpenAICompatProvider(baseURL, model string, opts ...OpenAICompatOption) *OpenAICompatProvider {
	p := &OpenAICompatProvider{
		model:   model,
		baseURL: strings.TrimRight(baseURL, "/"),
		client:  http.DefaultClient,
	}
	for _, opt := range opts {
		opt(p)
	}
	return p
}

type openAIRequest struct {
	Model    string          `json:"model"`
	Messages []openAIMessage `json:"messages"`
	Tools    []openAITool    `json:"tools,omitempty"`
}

type openAITool struct {
	Type     string            `json:"type"`
	Function openAIFunctionDef `json:"function"`
}

type openAIFunctionDef struct {
	Name        string          `json:"name"`
	Description string          `json:"description,omitempty"`
	Parameters  json.RawMessage `json:"parameters"`
}

type openAIMessage struct {
	Role       string           `json:"role"`
	Content    string           `json:"content"`
	ToolCalls  []openAIToolCall `json:"tool_calls,omitempty"`
	ToolCallID string           `json:"tool_call_id,omitempty"`
}

type openAIToolCall struct {
	ID       string             `json:"id"`
	Type     string             `json:"type"`
	Function openAIFunctionCall `json:"function"`
}

// openAIFunctionCall carries arguments as a JSON-encoded string, as the API
// specifies.
type openAIFunctionCall struct {
	Name      string `json:"name"`
	Arguments string `json:"arguments"`
}

type openAIResponse struct {
	Choices []struct {
		Message openAIMessage `json:"message"`
	} `json:"choices"`
	Usage struct {
		PromptTokens     int `json:"prompt_tokens"`
		CompletionTokens int `json:"completion_tokens"`
	} `json:"usage"`
}

// Complete sends req to the chat completions endpoint and maps the reply to a
// ModelResponse. HTTP 429 and 5xx responses and network errors are wrapped so
// policy.Classify treats them as Retryable; other 4xx responses are Terminal.
func (p *OpenAICompatProvider) Complete(ctx context.Context, req Request) (ModelResponse, error) {
	body, err := json.Marshal(buildOpenAIRequest(p.model, p.tools, req))
	if err != nil {
		return ModelResponse{}, fmt.Errorf("openai-compat: encode request: %w", err)
	}

	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, p.baseURL+"/chat/completions", bytes.NewReader(body))
	if err != nil {
		return ModelResponse{}, fmt.Errorf("openai-compat: build request: %w", err)
	}
	httpReq.Header.Set("content-type", "application/json")
	if p.apiKey != "" {
		httpReq.Header.Set("authorization", "Bearer "+p.apiKey)
	}

	resp, err := p.client.Do(httpReq)
	if err != nil {
		return ModelResponse{}, fmt.Errorf("openai-compat: request: %w", err)
	}
	defer resp.Body.Close()

	data, err := io.ReadAll(resp.Body)
	if err != nil {
		return ModelResponse{}, fmt.Errorf("openai-compat: read response: %w", err)
	}
	if resp.StatusCode != http.StatusOK {
		msg := strings.TrimSpace(string(data))
		return ModelResponse{}, fmt.Errorf("openai-compat: status %d: %s: %w", resp.StatusCode, msg, &policy.StatusError{StatusCode: resp.StatusCode})
	}

	var wire openAIResponse
	if err := json.Unmarshal(data, &wire); err != nil {
		return ModelResponse{}, fmt.Errorf("openai-compat: decode response: %w", err)
	}
	if len(wire.Choices) == 0 {
		return ModelResponse{}, fmt.Errorf("openai-compat: response has no choices")
	}

	msg := wire.Choices[0].Message
	out := ModelResponse{
		Content: msg.Content,
		Usage:   Usage{InputTokens: wire.Usage.PromptTokens, OutputTokens: wire.Usage.CompletionTokens},
	}
	for _, tc := range msg.ToolCalls {
		out.ToolCalls = append(out.ToolCalls, tools.ToolCall{Name: tc.Function.Name, Input: toolArguments(tc.Function.Arguments)})
	}
	return out, nil
}

// toolArguments turns the arguments string into a tool input. Small models
// sometimes emit invalid JSON; that is passed on as a JSON string so the tool
// fails with a decode error the model can read, instead of failing the run.
func toolArguments(args string) json.RawMessage {
	if strings.TrimSpace(args) == "" {
		return json.RawMessage(`{}`)
	}
	if json.Valid([]byte(args)) {
		return json.RawMessage(args)
	}
	quoted, _ := json.Marshal(args) // marshaling a string cannot fail
	return quoted
}

// buildOpenAIRequest converts req into one chat completions request. Tool
// results from the prior step go in as an assistant turn holding the tool
// calls, then one role "tool" message per result. Ids are derived from
// position so a resumed or replayed run sends an identical request.
func buildOpenAIRequest(model string, specs []openAITool, req Request) openAIRequest {
	messages := []openAIMessage{{Role: "user", Content: req.Prompt}}

	if len(req.ToolResults) > 0 {
		calls := make([]openAIToolCall, 0, len(req.ToolResults))
		results := make([]openAIMessage, 0, len(req.ToolResults))
		for i, tr := range req.ToolResults {
			id := toolUseID(i)
			args := string(tr.Call.Input)
			if args == "" {
				args = "{}"
			}
			calls = append(calls, openAIToolCall{ID: id, Type: "function", Function: openAIFunctionCall{Name: tr.Call.Name, Arguments: args}})

			content := tr.Output.Result
			if tr.Err != nil {
				content = "error: " + tr.Err.Error()
			}
			results = append(results, openAIMessage{Role: "tool", ToolCallID: id, Content: content})
		}
		messages = append(messages, openAIMessage{Role: "assistant", ToolCalls: calls})
		messages = append(messages, results...)
	}

	return openAIRequest{Model: model, Messages: messages, Tools: specs}
}
