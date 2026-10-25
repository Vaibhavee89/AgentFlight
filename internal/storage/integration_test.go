package storage_test

import (
	"context"
	"encoding/json"
	"path/filepath"
	"testing"

	"github.com/Vaibhavee89/agentflight/internal/provider"
	"github.com/Vaibhavee89/agentflight/internal/recorder"
	"github.com/Vaibhavee89/agentflight/internal/runtime"
	"github.com/Vaibhavee89/agentflight/internal/storage"
	"github.com/Vaibhavee89/agentflight/internal/tools"
	"github.com/Vaibhavee89/agentflight/internal/tools/builtin"
)

// TestEndToEnd_ScriptedRunEventLog runs a scripted two-step agent through
// Runtime with a recorder.Recorder backed by a real SQLiteStore, then reads
// the event log back and checks it records the full run: start, each model
// request/response, each tool call/result, and finish, all decodable and in
// seq order.
func TestEndToEnd_ScriptedRunEventLog(t *testing.T) {
	ctx := context.Background()

	store, err := storage.Open(ctx, filepath.Join(t.TempDir(), "e2e.db"))
	if err != nil {
		t.Fatalf("storage.Open() error: %v", err)
	}
	defer store.Close()

	rec := recorder.NewRecorder(store)

	responses := []provider.ModelResponse{
		{ToolCalls: []tools.ToolCall{{Name: "echo", Input: json.RawMessage(`{"text":"hi"}`)}}},
		{Content: "done"},
	}
	p := provider.NewMockProvider(responses, nil)

	reg := tools.NewRegistry()
	if err := reg.Register(builtin.NewEcho()); err != nil {
		t.Fatalf("register echo: %v", err)
	}

	rt := runtime.NewRuntime(p, reg, rec, runtime.WithMaxSteps(5))

	const runID = "run-e2e"
	result, err := rt.Run(ctx, runID, "prompt")
	if err != nil {
		t.Fatalf("Run() unexpected error: %v", err)
	}
	if result.Content != "done" || result.Steps != 2 {
		t.Fatalf("Run() = %+v, want Content %q Steps 2", result, "done")
	}

	events, err := store.Events(ctx, runID)
	if err != nil {
		t.Fatalf("Events() error: %v", err)
	}

	wantTypes := []recorder.EventType{
		recorder.EventRunStarted,
		recorder.EventModelRequest,
		recorder.EventModelResponse,
		recorder.EventToolCall,
		recorder.EventToolResult,
		recorder.EventModelRequest,
		recorder.EventModelResponse,
		recorder.EventRunFinished,
	}
	if len(events) != len(wantTypes) {
		t.Fatalf("got %d events, want %d: %+v", len(events), len(wantTypes), events)
	}

	for i, e := range events {
		if e.Type != wantTypes[i] {
			t.Fatalf("event[%d].Type = %q, want %q", i, e.Type, wantTypes[i])
		}
		if e.RunID != runID {
			t.Fatalf("event[%d].RunID = %q, want %q", i, e.RunID, runID)
		}
		if e.Seq != int64(i+1) {
			t.Fatalf("event[%d].Seq = %d, want %d", i, e.Seq, i+1)
		}
		if e.Version != recorder.CurrentVersion {
			t.Fatalf("event[%d].Version = %d, want %d", i, e.Version, recorder.CurrentVersion)
		}

		encoded, err := e.Encode()
		if err != nil {
			t.Fatalf("event[%d].Encode() error: %v", i, err)
		}
		decoded, err := recorder.DecodeEvent(encoded)
		if err != nil {
			t.Fatalf("DecodeEvent(event[%d]) error: %v", i, err)
		}
		if decoded.Type != e.Type || decoded.Seq != e.Seq {
			t.Fatalf("DecodeEvent(event[%d]) = %+v, want matching %+v", i, decoded, e)
		}
	}

	var toolResult struct {
		Name   string `json:"name"`
		Output struct {
			Result string `json:"result"`
		} `json:"output"`
	}
	if err := json.Unmarshal(events[4].Payload, &toolResult); err != nil {
		t.Fatalf("unmarshal tool_result payload: %v", err)
	}
	if toolResult.Name != "echo" || toolResult.Output.Result != "hi" {
		t.Fatalf("tool_result payload = %+v, want Name echo Output.Result hi", toolResult)
	}
}
