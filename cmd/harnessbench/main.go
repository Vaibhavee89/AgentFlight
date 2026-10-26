package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/Vaibhavee89/agentflight/internal/benchmark"
	"github.com/Vaibhavee89/agentflight/internal/provider"
	"github.com/Vaibhavee89/agentflight/internal/runtime"
	"github.com/Vaibhavee89/agentflight/internal/tools"
	"github.com/Vaibhavee89/agentflight/internal/tools/builtin"
)

const version = "0.0.1-dev"

func main() {
	if len(os.Args) < 2 {
		fmt.Println("harnessbench " + version)
		return
	}

	switch os.Args[1] {
	case "run":
		runSuite(os.Args[2:])
	default:
		fmt.Println("harnessbench " + version)
	}
}

// runSuite implements `harnessbench run -suite <path.jsonl> [-concurrency N]
// [-repeats N] [-out path.json] [-timeout d]`: it loads the suite and its
// sibling *.mock.json script, runs it offline through benchmark.Runner, and
// writes the results as JSON.
func runSuite(args []string) {
	fs := flag.NewFlagSet("run", flag.ExitOnError)
	suite := fs.String("suite", "", "path to a benchmark suite JSONL file")
	concurrency := fs.Int("concurrency", 1, "maximum number of tasks to run at once")
	repeats := fs.Int("repeats", 1, "number of times to run each task")
	out := fs.String("out", "results.json", "path to write JSON results to")
	timeout := fs.Duration("timeout", 30*time.Second, "default per-task timeout when a task sets none")
	fs.Parse(args)

	if *suite == "" {
		fmt.Fprintln(os.Stderr, "usage: harnessbench run -suite <path.jsonl> [-concurrency N] [-repeats N] [-out path.json] [-timeout d]")
		os.Exit(2)
	}

	tasks, err := benchmark.LoadTasks(*suite)
	if err != nil {
		fmt.Fprintf(os.Stderr, "load suite: %v\n", err)
		os.Exit(1)
	}

	mockPath := strings.TrimSuffix(*suite, filepath.Ext(*suite)) + ".mock.json"
	scripts, err := benchmark.LoadMockScripts(mockPath)
	if err != nil {
		fmt.Fprintf(os.Stderr, "load mock scripts %s: %v\n", mockPath, err)
		os.Exit(1)
	}

	byPrompt := make(map[string][]provider.ModelResponse, len(tasks))
	maxSteps := 10
	for _, task := range tasks {
		script := scripts[task.ID]
		byPrompt[task.Prompt] = script
		if len(script) > maxSteps {
			maxSteps = len(script)
		}
	}

	runner := benchmark.NewRunner(benchmark.Config{
		Concurrency:    *concurrency,
		PerTaskTimeout: *timeout,
		Repeats:        *repeats,
	})

	results, runErr := runner.Run(context.Background(), tasks, newRuntimeFactory(byPrompt, maxSteps))
	if runErr != nil {
		fmt.Fprintf(os.Stderr, "run suite: %v\n", runErr)
	}

	if err := writeResults(*out, results); err != nil {
		fmt.Fprintf(os.Stderr, "write results: %v\n", err)
		os.Exit(1)
	}

	var passed, failed int
	for _, r := range results {
		if r.Err == nil {
			passed++
		} else {
			failed++
		}
	}
	fmt.Printf("ran %d task executions: %d ok, %d failed; results written to %s\n", len(results), passed, failed, *out)

	if runErr != nil {
		os.Exit(1)
	}
}

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
// task run: a scriptedProvider sharing the (read-only) byPrompt scripts,
// and a registry with every built-in tool, since any task in the suite may
// call either.
func newRuntimeFactory(byPrompt map[string][]provider.ModelResponse, maxSteps int) func() *runtime.Runtime {
	return func() *runtime.Runtime {
		p := &scriptedProvider{byPrompt: byPrompt}

		reg := tools.NewRegistry()
		reg.Register(builtin.NewEcho())
		reg.Register(builtin.NewCalculator())

		return runtime.NewRuntime(p, reg, nil, runtime.WithMaxSteps(maxSteps))
	}
}

type outputResult struct {
	TaskID     string `json:"task_id"`
	Repeat     int    `json:"repeat"`
	Content    string `json:"content"`
	Steps      int    `json:"steps"`
	Status     string `json:"status"`
	Error      string `json:"error,omitempty"`
	DurationMS int64  `json:"duration_ms"`
}

func writeResults(path string, results []benchmark.TaskResult) error {
	out := make([]outputResult, len(results))
	for i, r := range results {
		o := outputResult{
			TaskID:     r.TaskID,
			Repeat:     r.Repeat,
			Content:    r.Result.Content,
			Steps:      r.Result.Steps,
			Status:     r.Result.Status,
			DurationMS: r.Duration.Milliseconds(),
		}
		if r.Err != nil {
			o.Error = r.Err.Error()
		}
		out[i] = o
	}

	data, err := json.MarshalIndent(out, "", "  ")
	if err != nil {
		return fmt.Errorf("encode results: %w", err)
	}
	if err := os.WriteFile(path, data, 0o644); err != nil {
		return fmt.Errorf("write %s: %w", path, err)
	}
	return nil
}
