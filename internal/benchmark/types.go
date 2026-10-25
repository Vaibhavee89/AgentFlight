package benchmark

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/Vaibhavee89/agentflight/internal/recorder"
	"github.com/Vaibhavee89/agentflight/internal/runtime"
)

// GraderSpec names which Grader a Task uses and its parameters: Want is the
// expected value or pattern for ExactMatch/Contains/Regex, and Tool is the
// tool name ToolCalled checks for.
type GraderSpec struct {
	Type string `json:"type"`
	Want string `json:"want,omitempty"`
	Tool string `json:"tool,omitempty"`
}

// Task is one benchmark scenario: a prompt, the tools it may use, how to
// grade the result, a timeout, and free-form tags.
type Task struct {
	ID      string
	Prompt  string
	Tools   []string
	Grader  GraderSpec
	Timeout time.Duration
	Tags    []string
}

// taskWire is Task's JSON encoding; Timeout is a duration string (e.g.
// "30s") rather than raw nanoseconds, for hand-written JSONL files.
type taskWire struct {
	ID      string     `json:"id"`
	Prompt  string     `json:"prompt"`
	Tools   []string   `json:"tools"`
	Grader  GraderSpec `json:"grader"`
	Timeout string     `json:"timeout,omitempty"`
	Tags    []string   `json:"tags,omitempty"`
}

// UnmarshalJSON decodes a Task, parsing its timeout string with
// time.ParseDuration.
func (t *Task) UnmarshalJSON(data []byte) error {
	var wire taskWire
	if err := json.Unmarshal(data, &wire); err != nil {
		return fmt.Errorf("benchmark: decode task: %w", err)
	}

	var timeout time.Duration
	if wire.Timeout != "" {
		d, err := time.ParseDuration(wire.Timeout)
		if err != nil {
			return fmt.Errorf("benchmark: task %q: invalid timeout %q: %w", wire.ID, wire.Timeout, err)
		}
		timeout = d
	}

	*t = Task{
		ID:      wire.ID,
		Prompt:  wire.Prompt,
		Tools:   wire.Tools,
		Grader:  wire.Grader,
		Timeout: timeout,
		Tags:    wire.Tags,
	}
	return nil
}

// MarshalJSON encodes a Task, writing its timeout as a duration string.
func (t Task) MarshalJSON() ([]byte, error) {
	wire := taskWire{
		ID:     t.ID,
		Prompt: t.Prompt,
		Tools:  t.Tools,
		Grader: t.Grader,
		Tags:   t.Tags,
	}
	if t.Timeout > 0 {
		wire.Timeout = t.Timeout.String()
	}
	return json.Marshal(wire)
}

// RunResult is what a Grader sees: a run's final result plus its full
// recorded event log, so graders can inspect tool calls as well as content.
type RunResult struct {
	runtime.RunResult
	Events []recorder.Event
}

// Grade is a grader's verdict on one task run.
type Grade struct {
	Pass   bool
	Reason string
}

// Grader decides whether a run satisfies a task.
type Grader interface {
	Grade(ctx context.Context, task Task, result RunResult) (Grade, error)
}

// ErrUnknownGraderType indicates a GraderSpec names a Type with no
// registered Grader.
var ErrUnknownGraderType = errors.New("benchmark: unknown grader type")
