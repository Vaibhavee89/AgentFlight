package storage

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	"github.com/Vaibhavee89/agentflight/internal/recorder"
)

func evAt(runID string, seq int64, typ recorder.EventType, payload string) recorder.Event {
	return recorder.Event{
		RunID: runID, Seq: seq, Version: recorder.CurrentVersion, Type: typ,
		Time: time.Date(2026, 1, 1, 0, 0, int(seq), 0, time.UTC), Payload: []byte(payload),
	}
}

func TestListRunSummaries(t *testing.T) {
	tests := []struct {
		name       string
		events     []recorder.Event
		wantSteps  int
		wantCalls  int
		wantRetry  int
		wantTokens int
		wantStatus string
		wantEnd    bool
	}{
		{
			name: "completed run",
			events: []recorder.Event{
				evAt("r", 1, recorder.EventRunStarted, `{"prompt":"p"}`),
				evAt("r", 2, recorder.EventModelResponse, `{}`),
				evAt("r", 3, recorder.EventToolCall, `{}`),
				evAt("r", 4, recorder.EventModelResponse, `{}`),
				evAt("r", 5, recorder.EventRunFinished, `{"Status":"completed","Retries":1,"Usage":{"InputTokens":10,"OutputTokens":5}}`),
			},
			wantSteps: 2, wantCalls: 1, wantRetry: 1, wantTokens: 15, wantStatus: "completed", wantEnd: true,
		},
		{
			name: "cancelled then resumed sums both segments",
			events: []recorder.Event{
				evAt("r", 1, recorder.EventRunStarted, `{"prompt":"p"}`),
				evAt("r", 2, recorder.EventModelResponse, `{}`),
				evAt("r", 3, recorder.EventRunFinished, `{"Status":"cancelled","Retries":2,"Usage":{"InputTokens":4,"OutputTokens":1}}`),
				evAt("r", 4, recorder.EventModelResponse, `{}`),
				evAt("r", 5, recorder.EventRunFinished, `{"Status":"completed","Retries":0,"Usage":{"InputTokens":6,"OutputTokens":2}}`),
			},
			wantSteps: 2, wantCalls: 0, wantRetry: 2, wantTokens: 13, wantStatus: "completed", wantEnd: true,
		},
		{
			name: "killed run has no final event",
			events: []recorder.Event{
				evAt("r", 1, recorder.EventRunStarted, `{"prompt":"p"}`),
				evAt("r", 2, recorder.EventModelResponse, `{}`),
			},
			wantSteps: 1, wantStatus: "", wantEnd: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			store, err := Open(context.Background(), filepath.Join(t.TempDir(), "runs.db"))
			if err != nil {
				t.Fatalf("Open: %v", err)
			}
			defer store.Close()
			for _, e := range tt.events {
				if err := store.Append(context.Background(), e); err != nil {
					t.Fatalf("Append: %v", err)
				}
			}

			sums, err := store.ListRunSummaries(context.Background())
			if err != nil {
				t.Fatalf("ListRunSummaries: %v", err)
			}
			if len(sums) != 1 {
				t.Fatalf("got %d summaries, want 1", len(sums))
			}
			s := sums[0]
			if s.Steps != tt.wantSteps || s.ToolCalls != tt.wantCalls || s.Retries != tt.wantRetry {
				t.Fatalf("counts = steps %d calls %d retries %d, want %d %d %d", s.Steps, s.ToolCalls, s.Retries, tt.wantSteps, tt.wantCalls, tt.wantRetry)
			}
			if got := s.InputTokens + s.OutputTokens; got != tt.wantTokens {
				t.Fatalf("tokens = %d, want %d", got, tt.wantTokens)
			}
			if s.FinalStatus != tt.wantStatus {
				t.Fatalf("FinalStatus = %q, want %q", s.FinalStatus, tt.wantStatus)
			}
			if s.StartedAt.IsZero() {
				t.Fatalf("StartedAt not set")
			}
			if (!s.FinishedAt.IsZero()) != tt.wantEnd {
				t.Fatalf("FinishedAt set = %v, want %v", !s.FinishedAt.IsZero(), tt.wantEnd)
			}
		})
	}
}

func TestEventsAfter(t *testing.T) {
	store, err := Open(context.Background(), filepath.Join(t.TempDir(), "after.db"))
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	defer store.Close()
	for i := int64(1); i <= 5; i++ {
		if err := store.Append(context.Background(), evAt("r", i, recorder.EventToolCall, `{}`)); err != nil {
			t.Fatalf("Append: %v", err)
		}
	}
	tests := []struct {
		name  string
		after int64
		limit int
		want  int
	}{
		{name: "from start, no limit", after: 0, limit: 0, want: 5},
		{name: "limit applies", after: 0, limit: 2, want: 2},
		{name: "cursor skips earlier events", after: 3, limit: 0, want: 2},
		{name: "cursor past the end", after: 9, limit: 0, want: 0},
		{name: "other run has nothing", after: 0, limit: 0, want: 0},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			runID := "r"
			if tt.name == "other run has nothing" {
				runID = "absent"
			}
			got, err := store.EventsAfter(context.Background(), runID, tt.after, tt.limit)
			if err != nil {
				t.Fatalf("EventsAfter: %v", err)
			}
			if len(got) != tt.want {
				t.Fatalf("got %d events, want %d", len(got), tt.want)
			}
			for i := 1; i < len(got); i++ {
				if got[i].Seq <= got[i-1].Seq {
					t.Fatalf("seqs not increasing: %d then %d", got[i-1].Seq, got[i].Seq)
				}
			}
		})
	}
}
