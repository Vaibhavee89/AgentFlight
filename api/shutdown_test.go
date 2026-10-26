package api

import (
	"context"
	"encoding/json"
	"net"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"testing"
	"time"

	"github.com/Vaibhavee89/agentflight/internal/recorder"
	"github.com/Vaibhavee89/agentflight/internal/runtime"
)

// TestServe_SIGTERMCancelsActiveRunAndRecordsFinalEvent starts the server,
// starts a run that will not finish for a long time, sends this process a real
// SIGTERM, and asserts that the server drains cleanly and that the run's final
// state was recorded as cancelled.
func TestServe_SIGTERMCancelsActiveRunAndRecordsFinalEvent(t *testing.T) {
	srv, store := newTestServer(t, 30*time.Second)

	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	base := "http://" + ln.Addr().String()

	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGTERM)
	defer stop()

	serveErr := make(chan error, 1)
	go func() { serveErr <- Serve(ctx, ln, srv, 10*time.Second) }()

	id := startRun(t, base, "long running")
	waitForStatus(t, base, id, statusRunning)

	if err := syscall.Kill(os.Getpid(), syscall.SIGTERM); err != nil {
		t.Fatalf("send SIGTERM: %v", err)
	}

	select {
	case err := <-serveErr:
		if err != nil {
			t.Fatalf("Serve() returned error after clean drain: %v", err)
		}
	case <-time.After(15 * time.Second):
		t.Fatalf("Serve() did not return after SIGTERM")
	}

	events, err := store.Events(context.Background(), id)
	if err != nil {
		t.Fatalf("Events: %v", err)
	}
	var finalStatus string
	for _, e := range events {
		if e.Type != recorder.EventRunFinished {
			continue
		}
		var final runtime.RunResult
		if err := json.Unmarshal(e.Payload, &final); err != nil {
			t.Fatalf("decode run_finished: %v", err)
		}
		finalStatus = final.Status
	}
	if finalStatus != statusCancelled {
		t.Fatalf("final run_finished status = %q, want %q; events: %d", finalStatus, statusCancelled, len(events))
	}

	if _, err := http.Get(base + "/healthz"); err == nil {
		t.Fatalf("server still accepting requests after shutdown")
	}
}

// TestServe_ContextCancelStopsListener checks that once shutdown begins, the
// listener refuses new connections.
func TestServe_ContextCancelStopsListener(t *testing.T) {
	srv, _ := newTestServer(t, 0)
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	addr := ln.Addr().String()

	ctx, cancel := context.WithCancel(context.Background())
	serveErr := make(chan error, 1)
	go func() { serveErr <- Serve(ctx, ln, srv, 5*time.Second) }()

	cancel()
	if err := <-serveErr; err != nil {
		t.Fatalf("Serve() error: %v", err)
	}

	conn, err := net.DialTimeout("tcp", addr, time.Second)
	if err == nil {
		conn.Close()
		t.Fatalf("listener still accepting connections after shutdown")
	}
}
