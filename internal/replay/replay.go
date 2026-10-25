package replay

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/Vaibhavee89/agentflight/internal/recorder"
	"github.com/Vaibhavee89/agentflight/internal/runtime"
)

// EventReader reads back a run's recorded events in seq order, as both
// recorder.Store and *storage.SQLiteStore do.
type EventReader interface {
	Events(ctx context.Context, runID string) ([]recorder.Event, error)
}

// Replay rebuilds the run recorded under runID in store: it replays the
// original prompt through a Runtime wired with a ReplayProvider and
// ReplayTool (so every provider call and tool call is checked against what
// was recorded, rather than re-executed), and checks that the resulting
// RunResult equals the one originally recorded.
func Replay(ctx context.Context, store EventReader, runID string) (runtime.RunResult, error) {
	events, err := store.Events(ctx, runID)
	if err != nil {
		return runtime.RunResult{}, fmt.Errorf("replay: load events for run %s: %w", runID, err)
	}
	if len(events) == 0 {
		return runtime.RunResult{}, fmt.Errorf("replay: no events recorded for run %s", runID)
	}

	prompt, err := runStartedPrompt(events)
	if err != nil {
		return runtime.RunResult{}, err
	}
	original, err := runFinishedResult(events)
	if err != nil {
		return runtime.RunResult{}, err
	}

	modelProvider, err := NewReplayProvider(events)
	if err != nil {
		return runtime.RunResult{}, err
	}
	tool, err := NewReplayTool(events)
	if err != nil {
		return runtime.RunResult{}, err
	}

	rt := runtime.NewRuntime(modelProvider, tool, nil, runtime.WithMaxSteps(modelProvider.Len()))

	result, err := rt.Run(ctx, runID, prompt)
	if err != nil {
		return runtime.RunResult{}, fmt.Errorf("replay: run %s: %w", runID, err)
	}

	if result != original {
		return runtime.RunResult{}, fmt.Errorf(
			"replay: run %s: final result mismatch: recorded %+v, got %+v: %w",
			runID, original, result, runtime.ErrReplayMismatch,
		)
	}

	return result, nil
}

func runStartedPrompt(events []recorder.Event) (string, error) {
	for _, e := range events {
		if e.Type != recorder.EventRunStarted {
			continue
		}
		var payload struct {
			Prompt string `json:"prompt"`
		}
		if err := json.Unmarshal(e.Payload, &payload); err != nil {
			return "", fmt.Errorf("replay: decode run_started seq %d: %w", e.Seq, err)
		}
		return payload.Prompt, nil
	}
	return "", fmt.Errorf("replay: no run_started event found")
}

func runFinishedResult(events []recorder.Event) (runtime.RunResult, error) {
	for i := len(events) - 1; i >= 0; i-- {
		e := events[i]
		if e.Type != recorder.EventRunFinished {
			continue
		}
		var result runtime.RunResult
		if err := json.Unmarshal(e.Payload, &result); err != nil {
			return runtime.RunResult{}, fmt.Errorf("replay: decode run_finished seq %d: %w", e.Seq, err)
		}
		return result, nil
	}
	return runtime.RunResult{}, fmt.Errorf("replay: no run_finished event found")
}
