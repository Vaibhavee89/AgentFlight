package api

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strconv"
	"time"

	"github.com/Vaibhavee89/agentflight/internal/recorder"
)

const (
	maxPageLimit   = 1000
	streamBatch    = 500
	heartbeatFrame = ": heartbeat\n\n"
)

// parseCursor reads the resume position: the Last-Event-ID header, which
// browsers' EventSource sends on reconnect, takes precedence over ?after=.
func parseCursor(r *http.Request) (int64, error) {
	raw := r.Header.Get("Last-Event-ID")
	if raw == "" {
		raw = r.URL.Query().Get("after")
	}
	if raw == "" {
		return 0, nil
	}
	n, err := strconv.ParseInt(raw, 10, 64)
	if err != nil || n < 0 {
		return 0, fmt.Errorf("cursor must be a non-negative sequence number")
	}
	return n, nil
}

// handleEventsPage serves GET /runs/{id}/events?after=&limit= with cursor
// paging: events with seq > after, at most limit of them (0 = up to 1000).
func (s *Server) handleEventsPage(w http.ResponseWriter, r *http.Request, id string) {
	after, err := parseCursor(r)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	limit := 0
	if v := r.URL.Query().Get("limit"); v != "" {
		n, err := strconv.Atoi(v)
		if err != nil || n < 1 || n > maxPageLimit {
			writeError(w, http.StatusBadRequest, fmt.Sprintf("limit must be an integer from 1 to %d", maxPageLimit))
			return
		}
		limit = n
	}
	if limit == 0 {
		limit = maxPageLimit
	}

	found, err := s.exists(r.Context(), id)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	if !found {
		writeError(w, http.StatusNotFound, "run not found")
		return
	}
	events, err := s.cfg.Store.EventsAfter(r.Context(), id, after, limit)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, toEventViews(events))
}

func toEventViews(events []recorder.Event) []eventView {
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
	return out
}

// handleStream serves GET /runs/{id}/stream as Server-Sent Events. Each event
// is sent with its seq as the SSE id, so a reconnecting client resumes with
// Last-Event-ID. The stream ends after run_finished, or when the run is no
// longer executing and has nothing left to send. A client disconnect ends it
// at once.
func (s *Server) handleStream(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	after, err := parseCursor(r)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	found, err := s.exists(r.Context(), id)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	if !found {
		writeError(w, http.StatusNotFound, "run not found")
		return
	}
	flusher, ok := w.(http.Flusher)
	if !ok {
		writeError(w, http.StatusInternalServerError, "streaming is not supported by this connection")
		return
	}

	h := w.Header()
	h.Set("Content-Type", "text/event-stream")
	h.Set("Cache-Control", "no-cache")
	h.Set("Connection", "keep-alive")
	h.Set("X-Accel-Buffering", "no")
	w.WriteHeader(http.StatusOK)
	flusher.Flush()

	poll := time.NewTicker(s.cfg.PollInterval)
	defer poll.Stop()
	beat := time.NewTicker(s.cfg.Heartbeat)
	defer beat.Stop()

	for {
		// Check liveness before reading the log. If the run is already
		// finished, every event it wrote is visible to the read below. Reading
		// first would race with a run that finishes in between, and its
		// run_finished would never be sent.
		active := s.runs.active(id)
		events, err := s.cfg.Store.EventsAfter(r.Context(), id, after, streamBatch)
		if err != nil {
			if r.Context().Err() == nil {
				s.logger.Error("stream read failed", "run_id", id, "error", err)
			}
			return
		}
		for _, e := range events {
			if err := writeSSE(w, e); err != nil {
				return
			}
			flusher.Flush()
			after = e.Seq
			if e.Type == recorder.EventRunFinished {
				return
			}
		}
		if len(events) == streamBatch {
			continue
		}
		if len(events) == 0 && !active {
			return
		}

		select {
		case <-r.Context().Done():
			return
		case <-beat.C:
			if _, err := fmt.Fprint(w, heartbeatFrame); err != nil {
				return
			}
			flusher.Flush()
		case <-poll.C:
		}
	}
}

func writeSSE(w http.ResponseWriter, e recorder.Event) error {
	data, err := json.Marshal(toEventViews([]recorder.Event{e})[0])
	if err != nil {
		return err
	}
	_, err = fmt.Fprintf(w, "id: %d\nevent: %s\ndata: %s\n\n", e.Seq, e.Type, data)
	return err
}
