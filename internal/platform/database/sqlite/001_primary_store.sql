CREATE TABLE config_records (
    entity TEXT NOT NULL,
    key TEXT NOT NULL,
    payload BLOB NOT NULL,
    PRIMARY KEY (entity, key)
);

CREATE TABLE usage_events (
    id TEXT PRIMARY KEY,
    ts TEXT NOT NULL,
    payload BLOB NOT NULL
);
CREATE INDEX usage_events_order_idx ON usage_events(ts DESC, id DESC);

CREATE TABLE audit_events (
    id TEXT PRIMARY KEY,
    ts TEXT NOT NULL,
    payload BLOB NOT NULL
);
CREATE INDEX audit_events_order_idx ON audit_events(ts DESC, id DESC);
