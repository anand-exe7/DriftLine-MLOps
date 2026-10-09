-- gen_random_uuid() is built into PostgreSQL 13+.
CREATE TABLE IF NOT EXISTS users (
    id            UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    email         TEXT NOT NULL UNIQUE,
    name          TEXT NOT NULL DEFAULT '',
    role          TEXT NOT NULL DEFAULT 'viewer' CHECK (role IN ('admin', 'engineer', 'viewer')),
    password_hash TEXT NOT NULL DEFAULT '',
    created_at    TIMESTAMPTZ NOT NULL DEFAULT now()
);
