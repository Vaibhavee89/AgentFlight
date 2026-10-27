package api

import (
	"context"
	"fmt"
	"net/http"
	"sort"
	"strconv"
	"time"

	"github.com/Vaibhavee89/agentflight/internal/storage"
)

const (
	defaultListLimit = 50
	maxListLimit     = 200
)

var validStatuses = map[string]bool{
	statusRunning: true, statusCompleted: true, statusCancelled: true,
	statusFailed: true, statusInterrupted: true,
}

type runListItem struct {
	ID          string     `json:"id"`
	Status      string     `json:"status"`
	StartedAt   *time.Time `json:"started_at"`
	FinishedAt  *time.Time `json:"finished_at"`
	Steps       int        `json:"steps"`
	ToolCalls   int        `json:"tool_calls"`
	Retries     int        `json:"retries"`
	TotalTokens int        `json:"total_tokens"`
}

// handleList serves GET /runs?limit=&status=: newest first, filtered by
// derived status, capped by limit.
func (s *Server) handleList(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	limit := defaultListLimit
	if v := q.Get("limit"); v != "" {
		n, err := strconv.Atoi(v)
		if err != nil || n < 1 || n > maxListLimit {
			writeError(w, http.StatusBadRequest, fmt.Sprintf("limit must be an integer from 1 to %d", maxListLimit))
			return
		}
		limit = n
	}
	status := q.Get("status")
	if status != "" && !validStatuses[status] {
		writeError(w, http.StatusBadRequest, "status must be one of running, completed, cancelled, failed, interrupted")
		return
	}

	items, err := s.listRuns(r.Context())
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}

	out := make([]runListItem, 0, len(items))
	for _, it := range items {
		if status == "" || it.Status == status {
			out = append(out, it)
		}
	}
	if len(out) > limit {
		out = out[:limit]
	}
	writeJSON(w, http.StatusOK, out)
}

// listRuns merges the event log with this process's registry. The registry
// wins for status, because it knows about runs that are still executing.
func (s *Server) listRuns(ctx context.Context) ([]runListItem, error) {
	summaries, err := s.cfg.Store.ListRunSummaries(ctx)
	if err != nil {
		return nil, err
	}

	byID := make(map[string]runListItem, len(summaries))
	for _, sum := range summaries {
		byID[sum.RunID] = itemFromSummary(sum)
	}

	for id, reg := range s.runs.allSnapshots() {
		it, ok := byID[id]
		if !ok {
			it = runListItem{ID: id}
		}
		it.Status = reg.status
		byID[id] = it
	}

	items := make([]runListItem, 0, len(byID))
	for _, it := range byID {
		if it.Status == "" {
			it.Status = statusInterrupted
		}
		items = append(items, it)
	}
	sort.Slice(items, func(i, j int) bool {
		a, b := items[i].StartedAt, items[j].StartedAt
		if a == nil || b == nil {
			if a != nil {
				return true
			}
			if b != nil {
				return false
			}
			return items[i].ID > items[j].ID
		}
		if !a.Equal(*b) {
			return a.After(*b)
		}
		return items[i].ID > items[j].ID
	})
	return items, nil
}

func itemFromSummary(sum storage.RunSummary) runListItem {
	it := runListItem{
		ID:          sum.RunID,
		Status:      sum.FinalStatus,
		Steps:       sum.Steps,
		ToolCalls:   sum.ToolCalls,
		Retries:     sum.Retries,
		TotalTokens: sum.InputTokens + sum.OutputTokens,
	}
	if !sum.StartedAt.IsZero() {
		t := sum.StartedAt
		it.StartedAt = &t
	}
	if !sum.FinishedAt.IsZero() {
		t := sum.FinishedAt
		it.FinishedAt = &t
	}
	return it
}
