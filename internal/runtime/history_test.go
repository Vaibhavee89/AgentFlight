package runtime

import (
	"context"
	"encoding/json"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/Vaibhavee89/agentflight/internal/provider"
	"github.com/Vaibhavee89/agentflight/internal/recorder"
	"github.com/Vaibhavee89/agentflight/internal/tools"
)

// capturingProvider replays scripted responses and keeps every request it was
// given. After the script ends it blocks until ctx is cancelled, if blockAfter.
type capturingProvider struct {
	mu         sync.Mutex
	script     []provider.ModelResponse
	requests   []provider.Request
	blockAfter bool
}

func (p *capturingProvider) Complete(ctx context.Context, req provider.Request) (provider.ModelResponse, error) {
	p.mu.Lock()
	p.requests = append(p.requests, req)
	n := len(p.requests)
	p.mu.Unlock()

	if n <= len(p.script) {
		return p.script[n-1], nil
	}
	if p.blockAfter {
		<-ctx.Done()
		return provider.ModelResponse{}, ctx.Err()
	}
	return provider.ModelResponse{Content: "done"}, nil
}

func (p *capturingProvider) seen() []provider.Request {
	p.mu.Lock()
	defer p.mu.Unlock()
	return append([]provider.Request(nil), p.requests...)
}

func echoCall(text string) tools.ToolCall {
	return tools.ToolCall{Name: "echo", Input: json.RawMessage(`{"text":"` + text + `"}`)}
}

// exchangeTexts flattens a request's tool exchanges to the echoed text of each
// call, one slice per step, so tests can compare whole conversations.
func exchangeTexts(req provider.Request) [][]string {
	var out [][]string
	for _, ex := range req.Exchanges() {
		var texts []string
		for _, tr := range ex {
			texts = append(texts, tr.Output.Result)
		}
		out = append(out, texts)
	}
	return out
}

func TestRuntime_Run_EachStepSeesEveryEarlierStep(t *testing.T) {
	tests := []struct {
		name string
		// toolSteps is the number of steps in which the model calls tools
		// before it answers.
		toolSteps int
	}{
		{name: "one tool step", toolSteps: 1},
		{name: "two tool steps", toolSteps: 2},
		{name: "four tool steps", toolSteps: 4},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var script []provider.ModelResponse
			for i := 0; i < tt.toolSteps; i++ {
				// Two parallel calls per step, so ordering inside a step is covered too.
				script = append(script, provider.ModelResponse{ToolCalls: []tools.ToolCall{
					echoCall(string(rune('a' + i))), echoCall(string(rune('A' + i))),
				}})
			}
			p := &capturingProvider{script: script}
			rt := NewRuntime(p, newRegistry(t), nil, WithMaxSteps(20))

			if _, err := rt.Run(context.Background(), "run-hist", "prompt"); err != nil {
				t.Fatalf("Run() error: %v", err)
			}

			reqs := p.seen()
			if len(reqs) != tt.toolSteps+1 {
				t.Fatalf("model called %d times, want %d", len(reqs), tt.toolSteps+1)
			}
			for call, req := range reqs {
				got := exchangeTexts(req)
				if len(got) != call {
					t.Fatalf("call %d saw %d earlier steps, want %d: %v", call, len(got), call, got)
				}
				for step, texts := range got {
					want := []string{string(rune('a' + step)), string(rune('A' + step))}
					if len(texts) != 2 || texts[0] != want[0] || texts[1] != want[1] {
						t.Fatalf("call %d, step %d = %v, want %v", call, step, texts, want)
					}
				}
			}
		})
	}
}

// TestRuntime_Resume_KeepsEarlierSteps checks the history is in the checkpoint,
// so a resumed run still gives the model its whole conversation.
func TestRuntime_Resume_KeepsEarlierSteps(t *testing.T) {
	backend := &memoryBackend{}
	rec := recorder.NewRecorder(backend)
	const runID = "run-resume-hist"

	// First process: two tool steps, then it stalls on the third model call.
	stalled := &capturingProvider{
		script: []provider.ModelResponse{
			{ToolCalls: []tools.ToolCall{echoCall("first")}},
			{ToolCalls: []tools.ToolCall{echoCall("second")}},
		},
		blockAfter: true,
	}
	ctx, cancel := context.WithCancel(context.Background())
	rt := NewRuntime(stalled, newRegistry(t), rec, WithCheckpoints(backend), WithEventReader(backend), WithMaxSteps(20))
	done := make(chan error, 1)
	go func() {
		_, err := rt.Run(ctx, runID, "prompt")
		done <- err
	}()
	for len(stalled.seen()) < 3 {
		time.Sleep(time.Millisecond)
	}
	cancel()
	if err := <-done; !errors.Is(err, ErrCancelled) {
		t.Fatalf("Run() error = %v, want ErrCancelled", err)
	}

	// Second process: resumes, and its first request must hold both earlier steps.
	resumed := &capturingProvider{}
	fresh := NewRuntime(resumed, newRegistry(t), rec, WithCheckpoints(backend), WithEventReader(backend), WithMaxSteps(20))
	if _, err := fresh.Resume(context.Background(), runID); err != nil {
		t.Fatalf("Resume() error: %v", err)
	}

	reqs := resumed.seen()
	if len(reqs) == 0 {
		t.Fatal("resumed run never called the model")
	}
	got := exchangeTexts(reqs[0])
	if len(got) != 2 || got[0][0] != "first" || got[1][0] != "second" {
		t.Fatalf("first request after resume has steps %v, want [[first] [second]]", got)
	}
}
