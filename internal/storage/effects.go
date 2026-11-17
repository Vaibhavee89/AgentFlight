package storage

import (
	"context"
	"fmt"
	"time"
)

// Effect is one side effect recorded under an idempotency key.
type Effect struct {
	Key       string
	Tool      string
	Payload   string
	CreatedAt time.Time
}

// ApplyEffect records an effect under key and reports whether it was new. A
// second call with a key that is already recorded changes nothing and returns
// applied=false, so a tool that performs its effect by calling this runs it
// once however many times the same operation is attempted. The insert is a
// single statement, so the key and the effect cannot be recorded separately.
func (s *SQLiteStore) ApplyEffect(ctx context.Context, key, tool, payload string) (bool, error) {
	res, err := s.db.ExecContext(ctx,
		`INSERT INTO effects (idempotency_key, tool, payload, created_at) VALUES (?, ?, ?, ?)
		 ON CONFLICT (idempotency_key) DO NOTHING`,
		key, tool, payload, time.Now().UTC().Format(time.RFC3339Nano))
	if err != nil {
		return false, fmt.Errorf("storage: apply effect %q: %w", key, err)
	}
	n, err := res.RowsAffected()
	if err != nil {
		return false, fmt.Errorf("storage: apply effect %q: rows affected: %w", key, err)
	}
	return n == 1, nil
}

// Effects returns every recorded effect, oldest first.
func (s *SQLiteStore) Effects(ctx context.Context) ([]Effect, error) {
	rows, err := s.db.QueryContext(ctx,
		`SELECT idempotency_key, tool, payload, created_at FROM effects ORDER BY created_at, idempotency_key`)
	if err != nil {
		return nil, fmt.Errorf("storage: list effects: %w", err)
	}
	defer rows.Close()

	var out []Effect
	for rows.Next() {
		var e Effect
		var created string
		if err := rows.Scan(&e.Key, &e.Tool, &e.Payload, &created); err != nil {
			return nil, fmt.Errorf("storage: scan effect: %w", err)
		}
		if e.CreatedAt, err = time.Parse(time.RFC3339Nano, created); err != nil {
			return nil, fmt.Errorf("storage: parse effect time %q: %w", created, err)
		}
		out = append(out, e)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("storage: list effects: %w", err)
	}
	return out, nil
}
