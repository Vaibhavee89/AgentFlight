package benchmark

import (
	"context"
	"encoding/json"
	"fmt"
	"regexp"
	"strings"

	"github.com/Vaibhavee89/agentflight/internal/recorder"
)

// ExactMatch passes when a run's final content is exactly task.Grader.Want.
type ExactMatch struct{}

func (ExactMatch) Grade(ctx context.Context, task Task, result RunResult) (Grade, error) {
	if result.Content == task.Grader.Want {
		return Grade{Pass: true}, nil
	}
	return Grade{Pass: false, Reason: fmt.Sprintf("content %q != want %q", result.Content, task.Grader.Want)}, nil
}

// Contains passes when a run's final content contains task.Grader.Want as a substring.
type Contains struct{}

func (Contains) Grade(ctx context.Context, task Task, result RunResult) (Grade, error) {
	if strings.Contains(result.Content, task.Grader.Want) {
		return Grade{Pass: true}, nil
	}
	return Grade{Pass: false, Reason: fmt.Sprintf("content %q does not contain %q", result.Content, task.Grader.Want)}, nil
}

// Regex passes when a run's final content matches the task.Grader.Want pattern.
type Regex struct{}

func (Regex) Grade(ctx context.Context, task Task, result RunResult) (Grade, error) {
	re, err := regexp.Compile(task.Grader.Want)
	if err != nil {
		return Grade{}, fmt.Errorf("benchmark: compile regex %q: %w", task.Grader.Want, err)
	}
	if re.MatchString(result.Content) {
		return Grade{Pass: true}, nil
	}
	return Grade{Pass: false, Reason: fmt.Sprintf("content %q does not match pattern %q", result.Content, task.Grader.Want)}, nil
}

// ToolCalled passes when the run's event log recorded a call to the tool
// named by task.Grader.Tool.
type ToolCalled struct{}

func (ToolCalled) Grade(ctx context.Context, task Task, result RunResult) (Grade, error) {
	for _, e := range result.Events {
		if e.Type != recorder.EventToolCall {
			continue
		}
		var payload struct {
			Call struct {
				Name string
			} `json:"call"`
		}
		if err := json.Unmarshal(e.Payload, &payload); err != nil {
			return Grade{}, fmt.Errorf("benchmark: decode tool_call event seq %d: %w", e.Seq, err)
		}
		if payload.Call.Name == task.Grader.Tool {
			return Grade{Pass: true}, nil
		}
	}
	return Grade{Pass: false, Reason: fmt.Sprintf("tool %q was never called", task.Grader.Tool)}, nil
}

// GraderFor returns the Grader named by spec.Type, or an error wrapping
// ErrUnknownGraderType if spec.Type names no registered Grader.
func GraderFor(spec GraderSpec) (Grader, error) {
	switch spec.Type {
	case "exact_match":
		return ExactMatch{}, nil
	case "contains":
		return Contains{}, nil
	case "regex":
		return Regex{}, nil
	case "tool_called":
		return ToolCalled{}, nil
	default:
		return nil, fmt.Errorf("benchmark: type %q: %w", spec.Type, ErrUnknownGraderType)
	}
}
