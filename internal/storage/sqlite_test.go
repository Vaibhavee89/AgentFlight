package storage

import (
	"context"
	"encoding/json"
	"path/filepath"
	"testing"
	"time"

	"github.com/Vaibhavee89/agentflight/internal/provider"
	"github.com/Vaibhavee89/agentflight/internal/recorder"
	"github.com/Vaibhavee89/agentflight/internal/recovery"
	"github.com/Vaibhavee89/agentflight/internal/tools"
)

func openTestStore(t *testing.T) *SQLiteStore {
	t.Helper()
	path := filepath.Join(t.TempDir(), "test.db")
	store, err := Open(context.Background(), path)
	if err != nil {
		t.Fatalf("Open() error: %v", err)
	}
	t.Cleanup(func() { store.Close() })
	return store
}

func TestOpen_EnablesWALMode(t *testing.T) {
	store := openTestStore(t)

	var mode string
	if err := store.db.QueryRowContext(context.Background(), "PRAGMA journal_mode;").Scan(&mode); err != nil {
		t.Fatalf("query journal_mode: %v", err)
	}
	if mode != "wal" {
		t.Fatalf("journal_mode = %q, want %q", mode, "wal")
	}
}

func TestSQLiteStore_AppendAndEvents(t *testing.T) {
	store := openTestStore(t)
	ctx := context.Background()

	want := []recorder.Event{
		{RunID: "run-1", Seq: 1, Version: recorder.CurrentVersion, Type: recorder.EventRunStarted, Time: time.Now().UTC(), Payload: json.RawMessage(`{"prompt":"hi"}`)},
		{RunID: "run-1", Seq: 2, Version: recorder.CurrentVersion, Type: recorder.EventRunFinished, Time: time.Now().UTC(), Payload: json.RawMessage(`{"content":"ok"}`)},
	}
	for _, e := range want {
		if err := store.Append(ctx, e); err != nil {
			t.Fatalf("Append() error: %v", err)
		}
	}

	got, err := store.Events(ctx, "run-1")
	if err != nil {
		t.Fatalf("Events() error: %v", err)
	}
	if len(got) != len(want) {
		t.Fatalf("got %d events, want %d", len(got), len(want))
	}
	for i := range want {
		if got[i].Seq != want[i].Seq || got[i].Type != want[i].Type || string(got[i].Payload) != string(want[i].Payload) {
			t.Fatalf("event[%d] = %+v, want %+v", i, got[i], want[i])
		}
	}
}

func TestSQLiteStore_EventsAreAppendOnly(t *testing.T) {
	store := openTestStore(t)
	ctx := context.Background()

	event := recorder.Event{RunID: "run-1", Seq: 1, Version: recorder.CurrentVersion, Type: recorder.EventRunStarted, Time: time.Now().UTC(), Payload: json.RawMessage(`{}`)}
	if err := store.Append(ctx, event); err != nil {
		t.Fatalf("Append() error: %v", err)
	}

	if _, err := store.db.ExecContext(ctx, "UPDATE events SET type = 'tampered' WHERE run_id = 'run-1'"); err == nil {
		t.Fatalf("UPDATE on events succeeded, want error")
	}
	if _, err := store.db.ExecContext(ctx, "DELETE FROM events WHERE run_id = 'run-1'"); err == nil {
		t.Fatalf("DELETE on events succeeded, want error")
	}
}

func TestSQLiteStore_DuplicateSeqRejected(t *testing.T) {
	store := openTestStore(t)
	ctx := context.Background()

	event := recorder.Event{RunID: "run-1", Seq: 1, Version: recorder.CurrentVersion, Type: recorder.EventRunStarted, Time: time.Now().UTC(), Payload: json.RawMessage(`{}`)}
	if err := store.Append(ctx, event); err != nil {
		t.Fatalf("Append() first insert: %v", err)
	}
	if err := store.Append(ctx, event); err == nil {
		t.Fatalf("Append() duplicate seq succeeded, want primary key violation")
	}
}

func TestSQLiteStore_LatestCheckpoint_NoneSaved(t *testing.T) {
	store := openTestStore(t)
	_, found, err := store.LatestCheckpoint(context.Background(), "no-such-run")
	if err != nil {
		t.Fatalf("LatestCheckpoint() error: %v", err)
	}
	if found {
		t.Fatalf("LatestCheckpoint() found = true, want false")
	}
}

func TestSQLiteStore_SaveCheckpointAndLatestCheckpoint(t *testing.T) {
	store := openTestStore(t)
	ctx := context.Background()

	first := recovery.Checkpoint{
		RunID:   "run-1",
		Step:    1,
		Request: provider.Request{Prompt: "hi"},
		SavedAt: time.Now().UTC(),
	}
	second := recovery.Checkpoint{
		RunID:   "run-1",
		Step:    2,
		Request: provider.Request{Prompt: "hi", ToolResults: []provider.ToolResult{{Call: tools.ToolCall{Name: "echo"}}}},
		SavedAt: time.Now().UTC(),
	}
	if err := store.SaveCheckpoint(ctx, first); err != nil {
		t.Fatalf("SaveCheckpoint() first: %v", err)
	}
	if err := store.SaveCheckpoint(ctx, second); err != nil {
		t.Fatalf("SaveCheckpoint() second: %v", err)
	}

	got, found, err := store.LatestCheckpoint(ctx, "run-1")
	if err != nil {
		t.Fatalf("LatestCheckpoint() error: %v", err)
	}
	if !found {
		t.Fatalf("LatestCheckpoint() found = false, want true")
	}
	if got.Step != second.Step || got.Request.Prompt != second.Request.Prompt {
		t.Fatalf("LatestCheckpoint() = %+v, want matching %+v", got, second)
	}
	if len(got.Request.ToolResults) != 1 || got.Request.ToolResults[0].Call.Name != "echo" {
		t.Fatalf("LatestCheckpoint().Request.ToolResults = %+v, want one echo call", got.Request.ToolResults)
	}
}

// A cancelled run saves a checkpoint for the step it was already on, which is
// the one saved after the previous step. Re-saving the same step must succeed.
func TestSQLiteStore_ResaveCheckpointSameStep(t *testing.T) {
	store := openTestStore(t)
	ctx := context.Background()
	first := recovery.Checkpoint{RunID: "run-1", Step: 1, Request: provider.Request{Prompt: "hi"}, SavedAt: time.Now().UTC()}
	if err := store.SaveCheckpoint(ctx, first); err != nil {
		t.Fatalf("first SaveCheckpoint: %v", err)
	}
	again := first
	again.Request = provider.Request{Prompt: "hi again"}
	if err := store.SaveCheckpoint(ctx, again); err != nil {
		t.Fatalf("re-save of step 1 failed: %v", err)
	}
	got, found, err := store.LatestCheckpoint(ctx, "run-1")
	if err != nil || !found {
		t.Fatalf("LatestCheckpoint found=%v err=%v", found, err)
	}
	if got.Request.Prompt != "hi again" {
		t.Fatalf("latest prompt = %q, want the re-saved request", got.Request.Prompt)
	}
}
