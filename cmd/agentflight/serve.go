package main

import (
	"context"
	"flag"
	"fmt"
	"log/slog"
	"net"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/Vaibhavee89/agentflight/api"
	"github.com/Vaibhavee89/agentflight/internal/benchmark/fault"
	"github.com/Vaibhavee89/agentflight/internal/policy"
	"github.com/Vaibhavee89/agentflight/internal/provider"
	"github.com/Vaibhavee89/agentflight/internal/runtime"
	"github.com/Vaibhavee89/agentflight/internal/storage"
	"github.com/Vaibhavee89/agentflight/internal/tools"
	"github.com/Vaibhavee89/agentflight/internal/tools/builtin"
)

// runServe implements `agentflight serve`: the HTTP API backed by the echo
// mock model by default (no API key needed) or by Claude with --provider
// anthropic, until SIGINT or SIGTERM, then a drain of
// active runs before exit.
func runServe(args []string) {
	fs := flag.NewFlagSet("serve", flag.ExitOnError)
	addr := fs.String("addr", "127.0.0.1:8080", "address to listen on")
	dbPath := fs.String("db", "agentflight.db", "path to the SQLite event store")
	providerName := fs.String("provider", "mock", "model backend: mock (offline echo), anthropic (needs ANTHROPIC_API_KEY), or openai-compat (Ollama, LM Studio, vLLM)")
	model := fs.String("model", "", "model id; defaults to claude-opus-5-5 for anthropic and qwen2.5:3b for openai-compat")
	baseURL := fs.String("base-url", "http://localhost:11434/v1", "API root for --provider openai-compat (default is Ollama); set OPENAI_API_KEY if the server needs a key")
	mockLatency := fs.Duration("mock-latency", 0, "delay per mock model call")
	shutdownTimeout := fs.Duration("shutdown-timeout", 15*time.Second, "how long to wait for active runs on shutdown")
	benchDir := fs.String("bench-dir", "docs", "directory with harnessbench output, served under /bench/")
	corsOrigin := fs.String("cors-origin", "", "single browser origin allowed to call the API; empty disables CORS")
	demoFaults := fs.Float64("demo-faults", 0, "probability in [0,1] that a tool call hangs (retried by the policy)")
	demoSeed := fs.Int64("demo-seed", 1, "seed for --demo-faults")
	fs.Parse(args)

	if *demoFaults < 0 || *demoFaults > 1 {
		fmt.Fprintln(os.Stderr, "--demo-faults must be between 0 and 1")
		os.Exit(2)
	}

	switch *providerName {
	case "mock", "anthropic", "openai-compat":
	default:
		fmt.Fprintf(os.Stderr, "--provider must be mock, anthropic or openai-compat, got %q\n", *providerName)
		os.Exit(2)
	}
	if *model == "" {
		*model = map[string]string{"anthropic": "claude-opus-5-5", "openai-compat": "qwen2.5:3b"}[*providerName]
	}
	if *providerName == "anthropic" && os.Getenv("ANTHROPIC_API_KEY") == "" {
		fmt.Fprintln(os.Stderr, "--provider anthropic needs the ANTHROPIC_API_KEY environment variable")
		os.Exit(2)
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	store, err := storage.Open(ctx, *dbPath)
	if err != nil {
		fmt.Fprintf(os.Stderr, "open store: %v\n", err)
		os.Exit(1)
	}
	defer store.Close()

	reg := tools.NewRegistry()
	if err := reg.Register(builtin.NewEcho()); err != nil {
		fmt.Fprintf(os.Stderr, "register tools: %v\n", err)
		os.Exit(1)
	}
	if err := reg.Register(builtin.NewCalculator()); err != nil {
		fmt.Fprintf(os.Stderr, "register tools: %v\n", err)
		os.Exit(1)
	}

	var toolExec runtime.ToolExecutor = reg
	var newPolicy func() policy.Policy
	if *demoFaults > 0 {
		toolExec = fault.NewFaultTool(reg, fault.Config{
			ToolTimeoutRate:     *demoFaults,
			MalformedOutputRate: *demoFaults,
		}, *demoSeed)
		newPolicy = func() policy.Policy {
			return policy.NewDefaultPolicy([]string{"echo", "calculator"},
				policy.WithMaxAttempts(3),
				policy.WithTimeout(30*time.Millisecond),
				policy.WithBackoff(5*time.Millisecond, 50*time.Millisecond),
			)
		}
	}

	newProvider := func() runtime.ModelProvider { return provider.EchoProvider{Latency: *mockLatency} }
	if *providerName == "anthropic" {
		newProvider = func() runtime.ModelProvider {
			return provider.NewAnthropicProvider(*model, provider.WithAnthropicTools(reg.Specs()))
		}
	}

	if *providerName == "openai-compat" {
		newProvider = func() runtime.ModelProvider {
			return provider.NewOpenAICompatProvider(*baseURL, *model,
				provider.WithOpenAICompatTools(reg.Specs()),
				provider.WithOpenAICompatAPIKey(os.Getenv("OPENAI_API_KEY")))
		}
	}

	logger := slog.New(slog.NewTextHandler(os.Stderr, nil))
	srv := api.NewServer(api.Config{
		Store:      store,
		Provider:   newProvider,
		Tools:      toolExec,
		MaxSteps:   10,
		NewPolicy:  newPolicy,
		Logger:     logger,
		BenchDir:   *benchDir,
		CORSOrigin: *corsOrigin,
	})

	ln, err := net.Listen("tcp", *addr)
	if err != nil {
		fmt.Fprintf(os.Stderr, "listen on %s: %v\n", *addr, err)
		os.Exit(1)
	}
	logger.Info("serving", "addr", ln.Addr().String(), "db", *dbPath, "provider", *providerName, "model", *model, "demo_faults", *demoFaults)

	if err := api.Serve(ctx, ln, srv, *shutdownTimeout); err != nil {
		fmt.Fprintf(os.Stderr, "serve: %v\n", err)
		os.Exit(1)
	}
	logger.Info("shut down cleanly")
}
