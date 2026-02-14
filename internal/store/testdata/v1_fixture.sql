CREATE TABLE schema_version (
    version INTEGER PRIMARY KEY,
    applied_at TEXT NOT NULL
);

INSERT INTO schema_version(version, applied_at)
VALUES (1, '2026-01-01T00:00:00Z');

CREATE TABLE items (
    id TEXT PRIMARY KEY,
    type TEXT NOT NULL,
    number INTEGER NOT NULL,
    title TEXT NOT NULL,
    body TEXT NOT NULL,
    author TEXT NOT NULL DEFAULT '',
    state TEXT NOT NULL DEFAULT 'open',
    labels TEXT NOT NULL DEFAULT '[]',
    files TEXT NOT NULL DEFAULT '[]',
    url TEXT NOT NULL DEFAULT '',
    created_at TEXT NOT NULL,
    updated_at TEXT NOT NULL
);

CREATE INDEX idx_items_type ON items(type);
CREATE INDEX idx_items_number ON items(number);
CREATE INDEX idx_items_state ON items(state);
