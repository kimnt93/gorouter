CREATE TABLE IF NOT EXISTS catalog_prices (
    model TEXT PRIMARY KEY,
    name TEXT NOT NULL DEFAULT '',
    provider TEXT NOT NULL DEFAULT '',
    context_length INTEGER NOT NULL DEFAULT 0 CHECK (context_length >= 0),
    cache_supported BOOLEAN NOT NULL DEFAULT FALSE,
    input_per_m DOUBLE PRECISION NOT NULL DEFAULT 0 CHECK (input_per_m >= 0),
    output_per_m DOUBLE PRECISION NOT NULL DEFAULT 0 CHECK (output_per_m >= 0),
    cached_input_per_m DOUBLE PRECISION NOT NULL DEFAULT 0 CHECK (cached_input_per_m >= 0),
    cache_write_per_m DOUBLE PRECISION NOT NULL DEFAULT 0 CHECK (cache_write_per_m >= 0),
    source TEXT NOT NULL,
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE INDEX IF NOT EXISTS idx_catalog_prices_source ON catalog_prices (source);
