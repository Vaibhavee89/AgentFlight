package main

import (
	"context"
	"fmt"
	"sync"

	"github.com/Vaibhavee89/agentflight/internal/provider"
	"github.com/Vaibhavee89/agentflight/internal/recorder"
	"github.com/Vaibhavee89/agentflight/internal/runtime"
	"github.com/Vaibhavee89/agentflight/internal/storage"
	"github.com/Vaibhavee89/agentflight/internal/tools"
	"github.com/Vaibhavee89/agentflight/internal/tools/builtin"
)

// scriptedProvider implements runtime.ModelProvider by replaying the
// scripted responses recorded under whichever prompt it is given, in
// order. Each instance is built fresh for exactly one task run (see
// newRuntimeFactory), so its cursor never needs to be reset.
type scriptedProvider struct {
	byPrompt map[string][]provider.ModelResponse

	mu   sync.Mutex
	next int
}

func (p *scriptedProvider) Complete(ctx context.Context, req provider.Request) (provider.ModelResponse, error) {
	p.mu.Lock()
	defer p.mu.Unlock()

	script := p.byPrompt[req.Prompt]
	if p.next >= len(script) {
		return provider.ModelResponse{}, fmt.Errorf("harnessbench: no scripted response for prompt %q at step %d", req.Prompt, p.next)
	}
	resp := script[p.next]
	p.next++
	return resp, nil
}

// newRuntimeFactory returns a function building a fresh Runtime for one
// task run: a scriptedProvider sharing the (read-only) byPrompt scripts, a
// registry with every built-in tool (since any task in the suite may call
// either), and a Recorder writing through to the shared SQLite store so the
// run can later be replayed with `agentflight replay`.
func newRuntimeFactory(byPrompt map[string][]provider.ModelResponse, store *storage.SQLiteStore, maxSteps int) func() *runtime.Runtime {
	return func() *runtime.Runtime {
		p := &scriptedProvider{byPrompt: byPrompt}

		reg := tools.NewRegistry()
		reg.Register(builtin.NewEcho())
		reg.Register(builtin.NewCalculator())

		rec := recorder.NewRecorder(store)

		return runtime.NewRuntime(p, reg, rec, runtime.WithMaxSteps(maxSteps))
	}
}
