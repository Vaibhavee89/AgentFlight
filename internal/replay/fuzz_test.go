package replay

import (
	"context"
	"encoding/json"
	"errors"
	"testing"
	"time"

	"github.com/Vaibhavee89/agentflight/internal/provider"
	"github.com/Vaibhavee89/agentflight/internal/recorder"
	"github.com/Vaibhavee89/agentflight/internal/runtime"
	"github.com/Vaibhavee89/agentflight/internal/tools"
)

// FuzzReplayInput feeds arbitrary recorded-event payloads into the replay
// machinery. Construction and matching must never panic, and any failure
// must be a typed replay/runtime error rather than an untyped one.
func FuzzReplayInput(f *testing.F) {
	// Seed from a genuinely recorded run so the corpus reflects real payload shapes.
	store := &memoryStore{}
	reg := newEchoRegistry(f)
	rt := runtime.NewRuntime(provider.NewMockProvider([]provider.ModelResponse{
		{ToolCalls: []tools.ToolCall{{Name: "echo", Input: json.RawMessage(`{"text":"hi"}`)}}},
		{Content: "done"},
	}, nil), reg, recorder.NewRecorder(store), runtime.WithMaxSteps(5))
	if _, err := rt.Run(context.Background(), "seed-run", "hi"); err != nil {
		f.Fatalf("seed run: %v", err)
	}
	var reqP, respP, toolP string
	for _, e := range store.events {
		switch e.Type {
		case recorder.EventModelRequest:
			if reqP == "" {
				reqP = string(e.Payload)
			}
		case recorder.EventModelResponse:
			if respP == "" {
				respP = string(e.Payload)
			}
		case recorder.EventToolResult:
			if toolP == "" {
				toolP = string(e.Payload)
			}
		}
	}
	f.Add(reqP, respP, toolP, "hi")
	f.Add(`not json`, `not json`, `not json`, "")
	f.Add(`null`, `null`, `null`, "x")
	f.Add(reqP, `{"ToolCalls":[{"Name":"","Input":null}]}`, `{"name":"","error":"boom"}`, "hi")

	f.Fuzz(func(t *testing.T, req, resp, toolRes, prompt string) {
		base := time.Unix(0, 0).UTC()
		events := []recorder.Event{
			{RunID: "fuzz", Seq: 1, Version: recorder.CurrentVersion, Type: recorder.EventRunStarted, Time: base, Payload: json.RawMessage(`{"prompt":"` + prompt + `"}`)},
			{RunID: "fuzz", Seq: 2, Version: recorder.CurrentVersion, Type: recorder.EventModelRequest, Time: base, Payload: json.RawMessage(req)},
			{RunID: "fuzz", Seq: 3, Version: recorder.CurrentVersion, Type: recorder.EventModelResponse, Time: base, Payload: json.RawMessage(resp)},
			{RunID: "fuzz", Seq: 4, Version: recorder.CurrentVersion, Type: recorder.EventToolResult, Time: base, Payload: json.RawMessage(toolRes)},
		}

		rp, err := NewReplayProvider(events)
		if err != nil {
			if !isTypedReplayErr(err) {
				t.Fatalf("NewReplayProvider returned an untyped error: %v", err)
			}
			return
		}
		rt, err := NewReplayTool(events)
		if err != nil {
			if !isTypedReplayErr(err) {
				t.Fatalf("NewReplayTool returned an untyped error: %v", err)
			}
			return
		}

		_, err = rp.Complete(context.Background(), provider.Request{Prompt: prompt})
		if err != nil && !isTypedReplayErr(err) {
			t.Fatalf("Complete() returned an untyped error: %v", err)
		}

		_, _ = rt.Execute(context.Background(), tools.ToolCall{Name: "echo"})
	})
}

// isTypedReplayErr reports whether err wraps one of the replay package's or
// runtime's typed sentinels; any other error from a replay call is a bug.
func isTypedReplayErr(err error) bool {
	return errors.Is(err, runtime.ErrReplayMismatch) || errors.Is(err, ErrReplayExhausted) || errors.Is(err, ErrCorruptRecording)
}
