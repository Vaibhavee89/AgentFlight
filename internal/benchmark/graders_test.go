package benchmark

import (
	"context"
	"encoding/json"
	"errors"
	"testing"

	"github.com/Vaibhavee89/agentflight/internal/recorder"
	"github.com/Vaibhavee89/agentflight/internal/runtime"
)

func TestExactMatch_Grade(t *testing.T) {
	tests := []struct {
		name     string
		content  string
		want     string
		wantPass bool
	}{
		{name: "exact match passes", content: "42", want: "42", wantPass: true},
		{name: "mismatch fails", content: "42", want: "43", wantPass: false},
		{name: "case mismatch fails", content: "Hi", want: "hi", wantPass: false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			task := Task{Grader: GraderSpec{Type: "exact_match", Want: tt.want}}
			result := RunResult{RunResult: runtime.RunResult{Content: tt.content}}

			g, err := GraderFor(task.Grader)
			if err != nil {
				t.Fatalf("GraderFor() error: %v", err)
			}
			grade, err := g.Grade(context.Background(), task, result)
			if err != nil {
				t.Fatalf("Grade() unexpected error: %v", err)
			}
			if grade.Pass != tt.wantPass {
				t.Fatalf("Grade() = %+v, want Pass=%v", grade, tt.wantPass)
			}
		})
	}
}

func TestContains_Grade(t *testing.T) {
	tests := []struct {
		name     string
		content  string
		want     string
		wantPass bool
	}{
		{name: "substring present passes", content: "the result is 3 exactly", want: "3", wantPass: true},
		{name: "substring absent fails", content: "the result is 4", want: "3", wantPass: false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			task := Task{Grader: GraderSpec{Type: "contains", Want: tt.want}}
			result := RunResult{RunResult: runtime.RunResult{Content: tt.content}}

			g, err := GraderFor(task.Grader)
			if err != nil {
				t.Fatalf("GraderFor() error: %v", err)
			}
			grade, err := g.Grade(context.Background(), task, result)
			if err != nil {
				t.Fatalf("Grade() unexpected error: %v", err)
			}
			if grade.Pass != tt.wantPass {
				t.Fatalf("Grade() = %+v, want Pass=%v", grade, tt.wantPass)
			}
		})
	}
}

func TestRegex_Grade(t *testing.T) {
	tests := []struct {
		name     string
		content  string
		pattern  string
		wantPass bool
		wantErr  bool
	}{
		{name: "pattern matches", content: "The answer is 25.", pattern: `^The answer is \d+\.$`, wantPass: true},
		{name: "pattern does not match", content: "25 is the answer", pattern: `^The answer is \d+\.$`, wantPass: false},
		{name: "invalid pattern errors", content: "anything", pattern: `(unclosed`, wantErr: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			task := Task{Grader: GraderSpec{Type: "regex", Want: tt.pattern}}
			result := RunResult{RunResult: runtime.RunResult{Content: tt.content}}

			g, err := GraderFor(task.Grader)
			if err != nil {
				t.Fatalf("GraderFor() error: %v", err)
			}
			grade, err := g.Grade(context.Background(), task, result)
			if tt.wantErr {
				if err == nil {
					t.Fatalf("Grade() expected error, got nil")
				}
				return
			}
			if err != nil {
				t.Fatalf("Grade() unexpected error: %v", err)
			}
			if grade.Pass != tt.wantPass {
				t.Fatalf("Grade() = %+v, want Pass=%v", grade, tt.wantPass)
			}
		})
	}
}

func toolCallEvent(t *testing.T, name string) recorder.Event {
	t.Helper()
	payload, err := json.Marshal(map[string]any{
		"step": 0, "index": 0,
		"call": map[string]any{"Name": name},
	})
	if err != nil {
		t.Fatalf("marshal tool_call payload: %v", err)
	}
	return recorder.Event{Type: recorder.EventToolCall, Payload: payload}
}

func TestToolCalled_Grade(t *testing.T) {
	tests := []struct {
		name     string
		events   []recorder.Event
		tool     string
		wantPass bool
	}{
		{
			name:     "tool was called",
			events:   []recorder.Event{toolCallEvent(t, "echo"), toolCallEvent(t, "calculator")},
			tool:     "calculator",
			wantPass: true,
		},
		{
			name:     "tool was never called",
			events:   []recorder.Event{toolCallEvent(t, "echo")},
			tool:     "calculator",
			wantPass: false,
		},
		{
			name:     "no events at all",
			events:   nil,
			tool:     "echo",
			wantPass: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			task := Task{Grader: GraderSpec{Type: "tool_called", Tool: tt.tool}}
			result := RunResult{Events: tt.events}

			g, err := GraderFor(task.Grader)
			if err != nil {
				t.Fatalf("GraderFor() error: %v", err)
			}
			grade, err := g.Grade(context.Background(), task, result)
			if err != nil {
				t.Fatalf("Grade() unexpected error: %v", err)
			}
			if grade.Pass != tt.wantPass {
				t.Fatalf("Grade() = %+v, want Pass=%v", grade, tt.wantPass)
			}
		})
	}
}

func TestGraderFor_UnknownType(t *testing.T) {
	_, err := GraderFor(GraderSpec{Type: "no-such-grader"})
	if !errors.Is(err, ErrUnknownGraderType) {
		t.Fatalf("GraderFor() error = %v, want wrapping %v", err, ErrUnknownGraderType)
	}
}
