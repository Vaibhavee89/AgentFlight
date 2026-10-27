package storage

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	goruntime "runtime"
	"sort"
	"time"

	_ "modernc.org/sqlite"

	"github.com/Vaibhavee89/agentflight/internal/recorder"
	"github.com/Vaibhavee89/agentflight/internal/recovery"
)

// SQLiteStore persists recorder events to a SQLite database using the
// append-only schema in migrations/001_init.sql, applied automatically by
// Open. It runs in WAL mode.
type SQLiteStore struct {
	db *sql.DB
}

// migrationsDir resolves the migrations directory relative to this source
// file, so it is found regardless of the caller's working directory.
func migrationsDir() string {
	_, file, _, _ := goruntime.Caller(0)
	return filepath.Join(filepath.Dir(file), "..", "..", "migrations")
}

// applyMigrations runs every migrations/*.sql file in name order. Each file
// must be idempotent (CREATE ... IF NOT EXISTS), so reopening a database is safe.
func applyMigrations(ctx context.Context, db *sql.DB) error {
	files, err := filepath.Glob(filepath.Join(migrationsDir(), "*.sql"))
	if err != nil {
		return fmt.Errorf("storage: list migrations: %w", err)
	}
	sort.Strings(files)
	for _, f := range files {
		schema, err := os.ReadFile(f)
		if err != nil {
			return fmt.Errorf("storage: read migration %s: %w", filepath.Base(f), err)
		}
		if _, err := db.ExecContext(ctx, string(schema)); err != nil {
			return fmt.Errorf("storage: apply migration %s: %w", filepath.Base(f), err)
		}
	}
	return nil
}

// Open opens (creating if necessary) a SQLite database at path, enables WAL
// mode, and applies every migration in migrations/.
func Open(ctx context.Context, path string) (*SQLiteStore, error) {
	db, err := sql.Open("sqlite", path)
	if err != nil {
		return nil, fmt.Errorf("storage: open %s: %w", path, err)
	}
	// SQLite allows only one writer at a time; routing every query through
	// a single connection avoids SQLITE_BUSY errors from concurrent
	// goroutines competing across pooled connections.
	db.SetMaxOpenConns(1)

	if _, err := db.ExecContext(ctx, "PRAGMA journal_mode = WAL;"); err != nil {
		db.Close()
		return nil, fmt.Errorf("storage: enable WAL mode: %w", err)
	}
	// Under WAL, NORMAL stays crash-consistent but skips the fsync on every
	// commit; the last few commits can be lost on power failure.
	if _, err := db.ExecContext(ctx, "PRAGMA synchronous = NORMAL;"); err != nil {
		db.Close()
		return nil, fmt.Errorf("storage: set synchronous: %w", err)
	}

	if err := applyMigrations(ctx, db); err != nil {
		db.Close()
		return nil, err
	}

	return &SQLiteStore{db: db}, nil
}

// Close closes the underlying database.
func (s *SQLiteStore) Close() error {
	return s.db.Close()
}

// Append inserts event into the append-only events table. It never updates
// or deletes an existing row.
func (s *SQLiteStore) Append(ctx context.Context, event recorder.Event) error {
	_, err := s.db.ExecContext(ctx,
		`INSERT INTO events (run_id, seq, version, type, time, payload) VALUES (?, ?, ?, ?, ?, ?)`,
		event.RunID, event.Seq, event.Version, string(event.Type),
		event.Time.UTC().Format(time.RFC3339Nano), []byte(event.Payload),
	)
	if err != nil {
		return fmt.Errorf("storage: append event run=%s seq=%d: %w", event.RunID, event.Seq, err)
	}
	return nil
}

// Events returns all events recorded for runID, ordered by seq ascending.
func (s *SQLiteStore) Events(ctx context.Context, runID string) ([]recorder.Event, error) {
	rows, err := s.db.QueryContext(ctx,
		`SELECT run_id, seq, version, type, time, payload FROM events WHERE run_id = ? ORDER BY seq ASC`,
		runID,
	)
	if err != nil {
		return nil, fmt.Errorf("storage: query events for run %s: %w", runID, err)
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

// LastSeq returns the highest seq stored for runID, or 0 if it has no events.
func (s *SQLiteStore) LastSeq(ctx context.Context, runID string) (int64, error) {
	var seq int64
	err := s.db.QueryRowContext(ctx,
		`SELECT COALESCE(MAX(seq), 0) FROM events WHERE run_id = ?`, runID,
	).Scan(&seq)
	if err != nil {
		return 0, fmt.Errorf("storage: last seq for run %s: %w", runID, err)
	}
	return seq, nil
}

// SaveCheckpoint inserts a new checkpoint row for cp.RunID at seq cp.Step.
func (s *SQLiteStore) SaveCheckpoint(ctx context.Context, cp recovery.Checkpoint) error {
	data, err := json.Marshal(cp)
	if err != nil {
		return fmt.Errorf("storage: encode checkpoint run=%s step=%d: %w", cp.RunID, cp.Step, err)
	}
	_, err = s.db.ExecContext(ctx,
		`INSERT INTO checkpoints (run_id, seq, state, created_at) VALUES (?, ?, ?, ?)`,
		cp.RunID, cp.Step, data, cp.SavedAt.UTC().Format(time.RFC3339Nano),
	)
	if err != nil {
		return fmt.Errorf("storage: save checkpoint run=%s step=%d: %w", cp.RunID, cp.Step, err)
	}
	return nil
}

// LatestCheckpoint returns the highest-seq checkpoint saved for runID, or
// found=false if none has been saved.
func (s *SQLiteStore) LatestCheckpoint(ctx context.Context, runID string) (recovery.Checkpoint, bool, error) {
	var data []byte
	err := s.db.QueryRowContext(ctx,
		`SELECT state FROM checkpoints WHERE run_id = ? ORDER BY seq DESC LIMIT 1`, runID,
	).Scan(&data)
	if errors.Is(err, sql.ErrNoRows) {
		return recovery.Checkpoint{}, false, nil
	}
	if err != nil {
		return recovery.Checkpoint{}, false, fmt.Errorf("storage: load latest checkpoint for run %s: %w", runID, err)
	}

	var cp recovery.Checkpoint
	if err := json.Unmarshal(data, &cp); err != nil {
		return recovery.Checkpoint{}, false, fmt.Errorf("storage: decode checkpoint for run %s: %w", runID, err)
	}
	return cp, true, nil
}
