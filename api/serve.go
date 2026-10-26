package api

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/http"
	"time"
)

// Serve accepts HTTP requests on ln until ctx is done. On shutdown it stops
// accepting requests, cancels active runs, and waits up to shutdownTimeout for
// each to record its final state. It returns nil on a clean drain, and an
// error if the listener failed or runs were still active at the deadline.
func Serve(ctx context.Context, ln net.Listener, s *Server, shutdownTimeout time.Duration) error {
	httpServer := &http.Server{Handler: s.Handler()}

	serveErr := make(chan error, 1)
	go func() { serveErr <- httpServer.Serve(ln) }()

	select {
	case err := <-serveErr:
		return fmt.Errorf("api: serve: %w", err)
	case <-ctx.Done():
	}

	shutdownCtx, cancel := context.WithTimeout(context.Background(), shutdownTimeout)
	defer cancel()

	if err := httpServer.Shutdown(shutdownCtx); err != nil {
		return fmt.Errorf("api: stop accepting requests: %w", err)
	}
	if err := <-serveErr; err != nil && !errors.Is(err, http.ErrServerClosed) {
		return fmt.Errorf("api: serve: %w", err)
	}
	return s.Drain(shutdownCtx)
}
