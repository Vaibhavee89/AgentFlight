// Command mcp runs an agent whose only tool is served by an MCP server over
// stdio. With no arguments it starts the demo server as a subprocess (itself,
// re-run with "serve") and runs one agent loop against it. Needs no network
// and no API key.
package main

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"os/signal"
	"syscall"
	"time"

	"github.com/Vaibhavee89/agentflight/internal/policy"
	"github.com/Vaibhavee89/agentflight/internal/provider"
	"github.com/Vaibhavee89/agentflight/internal/runtime"
	"github.com/Vaibhavee89/agentflight/internal/tools"
	"github.com/Vaibhavee89/agentflight/internal/tools/mcp"
)

func main() {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	if len(os.Args) > 1 && os.Args[1] == "serve" {
		if err := mcp.ServeDemoStdio(ctx); err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(1)
		}
		return
	}

	if err := run(ctx); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func run(ctx context.Context) error {
	self, err := os.Executable()
	if err != nil {
		return fmt.Errorf("locate own binary: %w", err)
	}

	// exec.Command, not CommandContext: the session owns the subprocess and
	// closes it via session.Close.
	session, err := mcp.ConnectStdio(ctx, exec.Command(self, "serve"))
	if err != nil {
		return err
	}
	defer session.Close()

	reg := tools.NewRegistry()
	names, err := mcp.Register(ctx, reg, session)
	if err != nil {
		return err
	}
	fmt.Printf("connected over stdio; server offers tools: %v\n", names)

	pol := policy.NewDefaultPolicy([]string{"add"},
		policy.WithMaxAttempts(2),
		policy.WithTimeout(5*time.Second),
	)
	rt := runtime.NewRuntime(&addThenAnswer{}, reg, nil,
		runtime.WithPolicy(pol),
		runtime.WithMaxSteps(4),
	)

	res, err := rt.Run(ctx, "mcp-example", "what is 2 plus 3?")
	if err != nil {
		return fmt.Errorf("run: %w", err)
	}
	fmt.Printf("agent answer (%d steps): %s\n", res.Steps, res.Content)
	return nil
}

// addThenAnswer is a scripted model: it asks for add(2, 3) on the first step,
// then answers with the tool's result from the request it receives.
type addThenAnswer struct{}

func (addThenAnswer) Complete(ctx context.Context, req provider.Request) (provider.ModelResponse, error) {
	if len(req.ToolResults) == 0 {
		input, err := json.Marshal(map[string]float64{"a": 2, "b": 3})
		if err != nil {
			return provider.ModelResponse{}, err
		}
		return provider.ModelResponse{ToolCalls: []tools.ToolCall{{Name: "add", Input: input}}}, nil
	}
	tr := req.ToolResults[0]
	if tr.Err != nil {
		return provider.ModelResponse{Content: "the tool failed: " + tr.Err.Error()}, nil
	}
	return provider.ModelResponse{Content: "2 + 3 = " + tr.Output.Result}, nil
}

var _ runtime.ModelProvider = addThenAnswer{}
