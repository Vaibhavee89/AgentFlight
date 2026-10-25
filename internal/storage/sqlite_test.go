package storage

import (
	"context"
	"encoding/json"
	"path/filepath"
	"testing"
	"time"

	"github.com/Vaibhavee89/agentflight/internal/recorder"
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
