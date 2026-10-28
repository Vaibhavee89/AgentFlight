// Package api exposes agent runs over HTTP and coordinates their lifecycle:
// starting, inspecting, cancelling, resuming, and draining on shutdown.
package api

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"time"

	"github.com/Vaibhavee89/agentflight/internal/policy"
	"github.com/Vaibhavee89/agentflight/internal/recorder"
	"github.com/Vaibhavee89/agentflight/internal/runtime"
	"github.com/Vaibhavee89/agentflight/internal/storage"
	"github.com/Vaibhavee89/agentflight/internal/webui"
)

const maxBodyBytes = 64 << 10

// Config wires a Server to its collaborators.
type Config struct {
	// Store holds the event log and checkpoints, and must outlive the Server.
	Store *storage.SQLiteStore
	// Provider builds the model provider for each run, so runs never share
	// provider state.
	Provider func() runtime.ModelProvider
	// Tools executes tool calls for every run.
	Tools runtime.ToolExecutor
	// MaxSteps bounds each run. Zero uses the runtime default.
	MaxSteps int
	// NewPolicy, if set, builds a policy for each run. A fresh policy per run
	// keeps its retry counter scoped to that run.
	NewPolicy func() policy.Policy
	// Logger receives server and runtime diagnostics. Nil discards them.
	Logger *slog.Logger
	// BenchDir holds harnessbench output served under /bench/. Empty disables
	// those routes' data (they return 404).
	BenchDir string
	// CORSOrigin, if non-empty, is the single browser origin allowed to call
	// the API cross-origin, for local frontend development. Empty disables CORS.
	CORSOrigin string
	// PollInterval is how often a stream checks the store for new events.
	// Zero means 250ms.
	PollInterval time.Duration
	// Heartbeat is how often an idle stream sends a comment line. Zero means 15s.
	Heartbeat time.Duration
}

// Server runs agent executions and serves their state over HTTP.
type Server struct {
	cfg    Config
	logger *slog.Logger
	rec    *recorder.Recorder
	runs   *registry
}

// NewServer builds a Server. All runs share one Recorder, so each run's
// sequence numbers stay consistent for the life of the process.
func NewServer(cfg Config) *Server {
	logger := cfg.Logger
	if logger == nil {
		logger = slog.New(slog.NewTextHandler(io.Discard, nil))
	}
	if cfg.PollInterval <= 0 {
		cfg.PollInterval = 250 * time.Millisecond
	}
	if cfg.Heartbeat <= 0 {
		cfg.Heartbeat = 15 * time.Second
	}
	return &Server{
		cfg:    cfg,
		logger: logger,
		rec:    recorder.NewRecorder(cfg.Store),
		runs:   newRegistry(),
	}
}

// Handler returns the HTTP routes for the server.
func (s *Server) Handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("POST /runs", s.handleStart)
	mux.HandleFunc("GET /runs", s.handleList)
	mux.HandleFunc("GET /runs/{id}", s.handleGet)
	mux.HandleFunc("GET /runs/{id}/events", s.handleEvents)
	mux.HandleFunc("GET /runs/{id}/stream", s.handleStream)
	mux.HandleFunc("POST /runs/{id}/cancel", s.handleCancel)
	mux.HandleFunc("POST /runs/{id}/resume", s.handleResume)
	mux.HandleFunc("POST /runs/{id}/replay", s.handleReplay)
	mux.HandleFunc("GET /bench/results", s.handleBenchResults)
	mux.HandleFunc("GET /bench/compare", s.handleBenchCompare)
	mux.HandleFunc("GET /healthz", s.handleHealth)
	mux.HandleFunc("GET /ui", func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, webui.Prefix+"/", http.StatusMovedPermanently)
	})
	mux.Handle(webui.Prefix+"/", webui.Handler())
	return s.withCORS(mux)
}

type startRequest struct {
	Prompt string `json:"prompt"`
}

type runView struct {
	ID      string `json:"id"`
	Status  string `json:"status"`
	Content string `json:"content,omitempty"`
	Steps   int    `json:"steps,omitempty"`
	Error   string `json:"error,omitempty"`
}

type eventView struct {
	Seq     int64           `json:"seq"`
	Type    string          `json:"type"`
	Version int             `json:"version"`
	Time    string          `json:"time"`
	Payload json.RawMessage `json:"payload"`
}

func (s *Server) handleStart(w http.ResponseWriter, r *http.Request) {
	var body startRequest
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, maxBodyBytes)).Decode(&body); err != nil {
		writeError(w, http.StatusBadRequest, "invalid JSON body: "+err.Error())
		return
	}
	if body.Prompt == "" {
		writeError(w, http.StatusBadRequest, "prompt is required")
		return
	}

	id, err := newRunID()
	if err != nil {
		writeError(w, http.StatusInternalServerError, "generate run id: "+err.Error())
		return
	}

	s.launch(id, func(ctx context.Context, rt *runtime.Runtime) (runtime.RunResult, error) {
		return rt.Run(ctx, id, body.Prompt)
	})
	writeJSON(w, http.StatusAccepted, runView{ID: id, Status: statusRunning})
}

func (s *Server) handleGet(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	view, ok, err := s.view(r.Context(), id)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	if !ok {
		writeError(w, http.StatusNotFound, "run not found")
		return
	}
	writeJSON(w, http.StatusOK, view)
}

func (s *Server) handleEvents(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	q := r.URL.Query()
	if q.Has("after") || q.Has("limit") {
		s.handleEventsPage(w, r, id)
		return
	}
	events, err := s.cfg.Store.Events(r.Context(), id)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	if len(events) == 0 {
		writeError(w, http.StatusNotFound, "run not found")
		return
	}
	out := make([]eventView, len(events))
	for i, e := range events {
		out[i] = eventView{
			Seq:     e.Seq,
			Type:    string(e.Type),
			Version: e.Version,
			Time:    e.Time.UTC().Format("2006-01-02T15:04:05.999999999Z07:00"),
			Payload: e.Payload,
		}
	}
	writeJSON(w, http.StatusOK, out)
}

func (s *Server) handleCancel(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	if s.runs.cancel(id) {
		writeJSON(w, http.StatusAccepted, runView{ID: id, Status: "cancelling"})
		return
	}
	if _, found, err := s.view(r.Context(), id); err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	} else if !found {
		writeError(w, http.StatusNotFound, "run not found")
		return
	}
	writeError(w, http.StatusConflict, "run is not active")
}

func (s *Server) handleResume(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	view, found, err := s.view(r.Context(), id)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	if !found {
		writeError(w, http.StatusNotFound, "run not found")
		return
	}
	if view.Status == statusRunning {
		writeError(w, http.StatusConflict, "run is already active")
		return
	}
	if view.Status == statusCompleted || view.Status == statusFailed {
		writeError(w, http.StatusConflict, "run already finished with status "+view.Status)
		return
	}
	_, hasCheckpoint, err := s.cfg.Store.LatestCheckpoint(r.Context(), id)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	if !hasCheckpoint {
		writeError(w, http.StatusConflict, "run has no checkpoint to resume from")
		return
	}

	if !s.launch(id, func(ctx context.Context, rt *runtime.Runtime) (runtime.RunResult, error) {
		return rt.Resume(ctx, id)
	}) {
		writeError(w, http.StatusConflict, "run is already active")
		return
	}
	writeJSON(w, http.StatusAccepted, runView{ID: id, Status: statusRunning})
}

func (s *Server) handleHealth(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, map[string]any{"status": "ok", "active_runs": s.runs.activeCount()})
}

// exists reports whether id is known to this process or has any recorded events.
func (s *Server) exists(ctx context.Context, id string) (bool, error) {
	if _, ok := s.runs.snapshot(id); ok {
		return true, nil
	}
	last, err := s.cfg.Store.LastSeq(ctx, id)
	if err != nil {
		return false, err
	}
	return last > 0, nil
}

// launch runs fn in its own goroutine under a cancellable context derived
// from context.Background, so runs outlive the HTTP request that started them.
// It returns false if the run is already active.
func (s *Server) launch(id string, fn func(ctx context.Context, rt *runtime.Runtime) (runtime.RunResult, error)) bool {
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	if !s.runs.begin(id, cancel, done) {
		cancel()
		return false
	}

	opts := []runtime.Option{
		runtime.WithMaxSteps(s.cfg.MaxSteps),
		runtime.WithCheckpoints(s.cfg.Store),
		runtime.WithEventReader(s.cfg.Store),
		runtime.WithLogger(s.logger),
	}
	if s.cfg.NewPolicy != nil {
		opts = append(opts, runtime.WithPolicy(s.cfg.NewPolicy()))
	}
	rt := runtime.NewRuntime(s.cfg.Provider(), s.cfg.Tools, s.rec, opts...)

	go func() {
		defer close(done)
		defer cancel()
		res, err := fn(ctx, rt)
		s.runs.finish(id, res, err)
		s.logger.Info("run finished", "run_id", id, "err", err)
	}()
	return true
}

// view reports id's current state. Runs this process has seen use the
// registry; others are reconstructed from the event log, where a run without a
// finished event is reported as interrupted.
func (s *Server) view(ctx context.Context, id string) (runView, bool, error) {
	if r, ok := s.runs.snapshot(id); ok {
		return runView{
			ID: id, Status: r.status, Content: r.result.Content,
			Steps: r.result.Steps, Error: r.err,
		}, true, nil
	}

	events, err := s.cfg.Store.Events(ctx, id)
	if err != nil {
		return runView{}, false, fmt.Errorf("load events for run %s: %w", id, err)
	}
	if len(events) == 0 {
		return runView{}, false, nil
	}

	for i := len(events) - 1; i >= 0; i-- {
		if events[i].Type != recorder.EventRunFinished {
			continue
		}
		var final runtime.RunResult
		if err := json.Unmarshal(events[i].Payload, &final); err != nil {
			return runView{}, false, fmt.Errorf("decode run_finished for run %s: %w", id, err)
		}
		return runView{ID: id, Status: final.Status, Content: final.Content, Steps: final.Steps}, true, nil
	}
	return runView{ID: id, Status: statusInterrupted}, true, nil
}

func newRunID() (string, error) {
	b := make([]byte, 8)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return hex.EncodeToString(b), nil
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

func writeError(w http.ResponseWriter, status int, msg string) {
	writeJSON(w, status, map[string]string{"error": msg})
}

// Drain cancels every active run and waits until each has recorded its final
// state, or until ctx expires. It returns an error wrapping ctx's error if
// any run is still active at the deadline.
func (s *Server) Drain(ctx context.Context) error {
	dones := s.runs.cancelAll()
	for i, done := range dones {
		select {
		case <-done:
		case <-ctx.Done():
			return fmt.Errorf("api: %d of %d run(s) still active at shutdown deadline: %w",
				len(dones)-i, len(dones), ctx.Err())
		}
	}
	return nil
}
