package provider

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/Vaibhavee89/agentflight/internal/tools"
)

// EchoProvider is an offline stand-in for a model. On a request with no tool
// results it asks for one echo call carrying the prompt; once tool results are
// present it answers with them. It is stateless: each decision depends only on
// the request, so a resumed run gets the same behavior as the original.
type EchoProvider struct {
	// Latency is how long each call takes before responding, honoring ctx.
	Latency time.Duration
}

// Complete implements the model step described on EchoProvider.
func (p EchoProvider) Complete(ctx context.Context, req Request) (ModelResponse, error) {
	if p.Latency > 0 {
		timer := time.NewTimer(p.Latency)
		defer timer.Stop()
		select {
		case <-timer.C:
		case <-ctx.Done():
			return ModelResponse{}, fmt.Errorf("echo provider: %w", ctx.Err())
		}
	}

	if len(req.ToolResults) == 0 {
		input, err := json.Marshal(map[string]string{"text": req.Prompt})
		if err != nil {
			return ModelResponse{}, fmt.Errorf("echo provider: encode input: %w", err)
		}
		return ModelResponse{ToolCalls: []tools.ToolCall{{Name: "echo", Input: input}}}, nil
	}

	return ModelResponse{Content: "echo: " + req.ToolResults[0].Output.Result}, nil
}
