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
	"github.com/Vaibhavee89/agentflight/internal/provider"
	"github.com/Vaibhavee89/agentflight/internal/runtime"
	"github.com/Vaibhavee89/agentflight/internal/storage"
	"github.com/Vaibhavee89/agentflight/internal/tools"
	"github.com/Vaibhavee89/agentflight/internal/tools/builtin"
)

// runServe implements `agentflight serve`: it runs the HTTP API backed by
// the echo mock provider (no API key needed) until SIGINT or SIGTERM, then
// drains active runs before exiting.
func runServe(args []string) {
	fs := flag.NewFlagSet("serve", flag.ExitOnError)
	addr := fs.String("addr", "127.0.0.1:8080", "address to listen on")
	dbPath := fs.String("db", "agentflight.db", "path to the SQLite event store")
	mockLatency := fs.Duration("mock-latency", 0, "delay per mock model call (raise it to see cancellation in action)")
	shutdownTimeout := fs.Duration("shutdown-timeout", 15*time.Second, "how long to wait for active runs on shutdown")
	fs.Parse(args)

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

	logger := slog.New(slog.NewTextHandler(os.Stderr, nil))
	srv := api.NewServer(api.Config{
		Store:    store,
		Provider: func() runtime.ModelProvider { return provider.EchoProvider{Latency: *mockLatency} },
		Tools:    reg,
		MaxSteps: 10,
		Logger:   logger,
	})

	ln, err := net.Listen("tcp", *addr)
	if err != nil {
		fmt.Fprintf(os.Stderr, "listen on %s: %v\n", *addr, err)
		os.Exit(1)
	}
	logger.Info("serving", "addr", ln.Addr().String(), "db", *dbPath)

	if err := api.Serve(ctx, ln, srv, *shutdownTimeout); err != nil {
		fmt.Fprintf(os.Stderr, "serve: %v\n", err)
		os.Exit(1)
	}
	logger.Info("shut down cleanly")
}
