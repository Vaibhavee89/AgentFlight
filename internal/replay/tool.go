package replay

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/Vaibhavee89/agentflight/internal/recorder"
	"github.com/Vaibhavee89/agentflight/internal/tools"
)

type toolResultRecord struct {
	seq    int64
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
	// mismatch is the first divergence seen. The runtime treats a tool error as
	// a result and carries on, so Replay checks this after the run to make sure
	// a mismatch cannot pass silently.
	mismatch *MismatchError
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
			return nil, fmt.Errorf("replay: decode tool_result seq %d: %w: %w", e.Seq, ErrCorruptRecording, err)
		}
		results = append(results, toolResultRecord{seq: e.Seq, name: payload.Name, output: payload.Output, errMsg: payload.Error})
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
		m := &MismatchError{
			Seq:    rec.seq,
			Detail: fmt.Sprintf("tool_result %d: recorded call %q, got call %q", index, rec.name, call.Name),
		}
		if t.mismatch == nil {
			t.mismatch = m
		}
		return tools.ToolOutput{}, m
	}
	if rec.errMsg != "" {
		return tools.ToolOutput{}, errors.New(rec.errMsg)
	}
	return rec.output, nil
}
