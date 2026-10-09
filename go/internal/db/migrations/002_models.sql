-- A "model" is a named prediction task (e.g. loan_default_xgb); its trained
-- artifacts live in model_versions.
CREATE TABLE IF NOT EXISTS models (
    id          UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    name        TEXT NOT NULL UNIQUE CHECK (name ~ '^[A-Za-z0-9][A-Za-z0-9._-]{0,127}$'),
    description TEXT NOT NULL DEFAULT '',
    task_type   TEXT NOT NULL DEFAULT 'binary_classification',
    created_by  UUID REFERENCES users(id) ON DELETE SET NULL,
    created_at  TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at  TIMESTAMPTZ NOT NULL DEFAULT now()
);
