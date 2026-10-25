CREATE TABLE IF NOT EXISTS events (
    run_id  TEXT    NOT NULL,
    seq     INTEGER NOT NULL,
    version INTEGER NOT NULL,
    type    TEXT    NOT NULL,
    time    TEXT    NOT NULL,
    payload BLOB    NOT NULL,
    PRIMARY KEY (run_id, seq)
);

CREATE TRIGGER IF NOT EXISTS events_no_update
BEFORE UPDATE ON events
BEGIN
    SELECT RAISE(ABORT, 'events is append-only: UPDATE not allowed');
END;

CREATE TRIGGER IF NOT EXISTS events_no_delete
BEFORE DELETE ON events
BEGIN
    SELECT RAISE(ABORT, 'events is append-only: DELETE not allowed');
END;

CREATE TABLE IF NOT EXISTS checkpoints (
    run_id     TEXT    NOT NULL,
    seq        INTEGER NOT NULL,
    state      BLOB    NOT NULL,
    created_at TEXT    NOT NULL,
    PRIMARY KEY (run_id, seq)
);
