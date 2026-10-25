package replay

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/Vaibhavee89/agentflight/internal/recorder"
	"github.com/Vaibhavee89/agentflight/internal/runtime"
	"github.com/Vaibhavee89/agentflight/internal/tools"
)

type toolResultRecord struct {
	name   string
	output tools.ToolOutput
	errMsg string
}

// ReplayTool implements runtime.ToolExecutor by returning a run's recorded
// tool_result events in order instead of executing anything. It checks that
// each live call's name matches the recorded call it stands in for, and
// returns an error wrapping runtime.ErrReplayMismatch if they differ.
type ReplayTool struct {
	results []toolResultRecord
	next    int
}

// NewReplayTool builds a ReplayTool from a run's recorded tool_result events.
func NewReplayTool(events []recorder.Event) (*ReplayTool, error) {
	var results []toolResultRecord
	for _, e := range events {
		if e.Type != recorder.EventToolResult {
			continue
		}
		var payload struct {
			Name   string           `json:"name"`
			Output tools.ToolOutput `json:"output"`
			Error  string           `json:"error,omitempty"`
		}
		if err := json.Unmarshal(e.Payload, &payload); err != nil {
			return nil, fmt.Errorf("replay: decode tool_result seq %d: %w", e.Seq, err)
		}
		results = append(results, toolResultRecord{name: payload.Name, output: payload.Output, errMsg: payload.Error})
	}
	return &ReplayTool{results: results}, nil
}

// Execute returns the next recorded tool result, after verifying call.Name
// matches the name recorded for that result.
func (t *ReplayTool) Execute(ctx context.Context, call tools.ToolCall) (tools.ToolOutput, error) {
	if err := ctx.Err(); err != nil {
		return tools.ToolOutput{}, fmt.Errorf("replay: %w", err)
	}
	if t.next >= len(t.results) {
		return tools.ToolOutput{}, fmt.Errorf("replay: no recorded tool_result for call %q at index %d: %w", call.Name, t.next, ErrReplayExhausted)
	}

	rec := t.results[t.next]
	index := t.next
	t.next++

	if rec.name != call.Name {
		return tools.ToolOutput{}, fmt.Errorf(
			"replay: tool_result %d: recorded call %q, got call %q: %w",
			index, rec.name, call.Name, runtime.ErrReplayMismatch,
		)
	}
	if rec.errMsg != "" {
		return tools.ToolOutput{}, errors.New(rec.errMsg)
	}
	return rec.output, nil
}
