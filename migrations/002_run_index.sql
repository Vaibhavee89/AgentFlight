-- Covers per-run aggregates (counts by type, run boundaries) without scanning
-- the whole events table. Additive only; the events table is unchanged.
CREATE INDEX IF NOT EXISTS events_run_type_seq ON events (run_id, type, seq);
