package benchmark

import (
	"context"
	"sync"
	"testing"

	"github.com/Vaibhavee89/agentflight/internal/provider"
	"github.com/Vaibhavee89/agentflight/internal/recorder"
	"github.com/Vaibhavee89/agentflight/internal/runtime"
	"github.com/Vaibhavee89/agentflight/internal/tools"
	"github.com/Vaibhavee89/agentflight/internal/tools/builtin"
)

// memoryStore is a minimal in-memory recorder.Store and runtime.EventReader,
// used here only to let Runtime record events a grader can read back.
type memoryStore struct {
	mu     sync.Mutex
	events []recorder.Event
}

func (m *memoryStore) Append(ctx context.Context, event recorder.Event) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.events = append(m.events, event)
	return nil
}

func (m *memoryStore) Events(ctx context.Context, runID string) ([]recorder.Event, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	var out []recorder.Event
	for _, e := range m.events {
		if e.RunID == runID {
			out = append(out, e)
		}
	}
	return out, nil
}

func newToolRegistry(t *testing.T, names []string) *tools.Registry {
	t.Helper()
	reg := tools.NewRegistry()
	for _, name := range names {
		var tool tools.Tool
		switch name {
		case "echo":
			tool = builtin.NewEcho()
		case "calculator":
			tool = builtin.NewCalculator()
		default:
			t.Fatalf("unknown tool %q in task fixture", name)
		}
		if err := reg.Register(tool); err != nil {
			t.Fatalf("register tool %q: %v", name, err)
		}
	}
	return reg
}

// TestBasicSuite_RunsOfflineAndPasses runs every task in basic.jsonl against
// its scripted responses in basic.mock.json through the real Runtime and
// its own tools, and checks each one grades as a pass. This is both a
// sanity check on the fixtures and a demonstration that the suite runs
// fully offline and deterministically.
func TestBasicSuite_RunsOfflineAndPasses(t *testing.T) {
	tasks, err := LoadTasks("../../benchmarks/basic.jsonl")
	if err != nil {
		t.Fatalf("LoadTasks() error: %v", err)
	}
	scripts, err := LoadMockScripts("../../benchmarks/basic.mock.json")
	if err != nil {
		t.Fatalf("LoadMockScripts() error: %v", err)
	}

	for _, task := range tasks {
		t.Run(task.ID, func(t *testing.T) {
			script, ok := scripts[task.ID]
			if !ok {
				t.Fatalf("no mock script for task %q", task.ID)
			}

			store := &memoryStore{}
			rec := recorder.NewRecorder(store)
			p := provider.NewMockProvider(script, nil)
			reg := newToolRegistry(t, task.Tools)

			rt := runtime.NewRuntime(p, reg, rec, runtime.WithMaxSteps(len(script)+1))

			ctx, cancel := context.WithTimeout(context.Background(), task.Timeout)
			defer cancel()

			runResult, err := rt.Run(ctx, task.ID, task.Prompt)
			if err != nil {
				t.Fatalf("Run() error: %v", err)
			}

			events, err := store.Events(context.Background(), task.ID)
			if err != nil {
				t.Fatalf("Events() error: %v", err)
			}

			grader, err := GraderFor(task.Grader)
			if err != nil {
				t.Fatalf("GraderFor() error: %v", err)
			}
			grade, err := grader.Grade(context.Background(), task, RunResult{RunResult: runResult, Events: events})
			if err != nil {
				t.Fatalf("Grade() error: %v", err)
			}
			if !grade.Pass {
				t.Fatalf("task %q failed grading: %s (content=%q)", task.ID, grade.Reason, runResult.Content)
			}
		})
	}
}
