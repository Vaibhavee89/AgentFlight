-- Side effects performed by tools that honour an idempotency key. The primary
-- key is what makes a repeat of the same operation a no-op: inserting the key
-- and recording the effect is one atomic statement.
CREATE TABLE IF NOT EXISTS effects (
    idempotency_key TEXT NOT NULL PRIMARY KEY,
    tool            TEXT NOT NULL,
    payload         BLOB NOT NULL,
    created_at      TEXT NOT NULL
);
