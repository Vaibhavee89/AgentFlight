package storage

import (
	"context"
	"encoding/json"
	"fmt"
	"math"
	"time"

	"github.com/Vaibhavee89/agentflight/internal/recorder"
)

// RunSummary aggregates one run's event log. Counts cover every execution
// segment, so a run that was cancelled and resumed reports both.
type RunSummary struct {
	RunID        string
	StartedAt    time.Time
	FinishedAt   time.Time // zero if no run_finished has been recorded
	Steps        int       // model_response events: one per provider step
	ToolCalls    int
	Retries      int // summed over run_finished payloads
	InputTokens  int // summed over run_finished payloads
	OutputTokens int
	FinalStatus  string // status from the last run_finished payload, or ""
}

// finalPayload is the subset of runtime.RunResult that run_finished carries.
type finalPayload struct {
	Status  string
	Retries int
	Usage   struct {
		InputTokens  int
		OutputTokens int
	}
}

// ListRunSummaries returns a summary for every run that has events, in no
// particular order. Callers sort and filter.
func (s *SQLiteStore) ListRunSummaries(ctx context.Context) ([]RunSummary, error) {
	byRun := make(map[string]*RunSummary)
	get := func(id string) *RunSummary {
		r, ok := byRun[id]
		if !ok {
			r = &RunSummary{RunID: id}
			byRun[id] = r
		}
		return r
	}

	counts, err := s.db.QueryContext(ctx,
		`SELECT run_id, SUM(type = 'model_response'), SUM(type = 'tool_call') FROM events GROUP BY run_id`)
	if err != nil {
		return nil, fmt.Errorf("storage: count events per run: %w", err)
	}
	for counts.Next() {
		var id string
		var steps, calls int
		if err := counts.Scan(&id, &steps, &calls); err != nil {
			counts.Close()
			return nil, fmt.Errorf("storage: scan counts: %w", err)
		}
		r := get(id)
		r.Steps, r.ToolCalls = steps, calls
	}
	if err := counts.Close(); err != nil {
		return nil, fmt.Errorf("storage: close counts: %w", err)
	}
	if err := counts.Err(); err != nil {
		return nil, fmt.Errorf("storage: iterate counts: %w", err)
	}

	bounds, err := s.db.QueryContext(ctx,
		`SELECT run_id, type, time, payload FROM events
		 WHERE type IN ('run_started', 'run_finished') ORDER BY run_id, seq`)
	if err != nil {
		return nil, fmt.Errorf("storage: load run boundaries: %w", err)
	}
	defer bounds.Close()
	for bounds.Next() {
		var id, typ, timeStr string
		var payload []byte
		if err := bounds.Scan(&id, &typ, &timeStr, &payload); err != nil {
			return nil, fmt.Errorf("storage: scan boundary: %w", err)
		}
		t, err := time.Parse(time.RFC3339Nano, timeStr)
		if err != nil {
			return nil, fmt.Errorf("storage: parse event time %q: %w", timeStr, err)
		}
		r := get(id)
		switch recorder.EventType(typ) {
		case recorder.EventRunStarted:
			if r.StartedAt.IsZero() {
				r.StartedAt = t
			}
		case recorder.EventRunFinished:
			var fp finalPayload
			if err := json.Unmarshal(payload, &fp); err != nil {
				return nil, fmt.Errorf("storage: decode run_finished for run %s: %w", id, err)
			}
			r.FinishedAt = t
			r.FinalStatus = fp.Status
			r.Retries += fp.Retries
			r.InputTokens += fp.Usage.InputTokens
			r.OutputTokens += fp.Usage.OutputTokens
		}
	}
	if err := bounds.Err(); err != nil {
		return nil, fmt.Errorf("storage: iterate boundaries: %w", err)
	}

	out := make([]RunSummary, 0, len(byRun))
	for _, r := range byRun {
		out = append(out, *r)
	}
	return out, nil
}

// EventsAfter returns up to limit events for runID with seq greater than
// after, in seq order. A limit of zero or less means no limit.
func (s *SQLiteStore) EventsAfter(ctx context.Context, runID string, after int64, limit int) ([]recorder.Event, error) {
	if limit <= 0 {
		limit = math.MaxInt32
	}
	rows, err := s.db.QueryContext(ctx,
		`SELECT run_id, seq, version, type, time, payload FROM events
		 WHERE run_id = ? AND seq > ? ORDER BY seq ASC LIMIT ?`, runID, after, limit)
	if err != nil {
		return nil, fmt.Errorf("storage: query events after %d for run %s: %w", after, runID, err)
	}
	defer rows.Close()

	var events []recorder.Event
	for rows.Next() {
		var (
			e       recorder.Event
			typ     string
			timeStr string
			payload []byte
		)
		if err := rows.Scan(&e.RunID, &e.Seq, &e.Version, &typ, &timeStr, &payload); err != nil {
			return nil, fmt.Errorf("storage: scan event: %w", err)
		}
		t, err := time.Parse(time.RFC3339Nano, timeStr)
		if err != nil {
			return nil, fmt.Errorf("storage: parse event time %q: %w", timeStr, err)
		}
		e.Type = recorder.EventType(typ)
		e.Time = t
		e.Payload = json.RawMessage(payload)
		events = append(events, e)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("storage: iterate events for run %s: %w", runID, err)
	}
	return events, nil
}
