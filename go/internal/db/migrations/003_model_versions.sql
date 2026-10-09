CREATE TABLE IF NOT EXISTS model_versions (
    id              UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    model_id        UUID NOT NULL REFERENCES models(id) ON DELETE CASCADE,
    version         TEXT NOT NULL CHECK (version ~ '^[A-Za-z0-9][A-Za-z0-9._-]{0,127}$'),
    framework       TEXT NOT NULL DEFAULT '',
    -- MinIO prefix holding model.onnx + feature_schema.json, "{model}/{version}"
    artifact_key    TEXT NOT NULL,
    artifact_size   BIGINT NOT NULL DEFAULT 0,
    checksum_sha256 TEXT NOT NULL DEFAULT '',
    feature_schema  JSONB NOT NULL DEFAULT '{}'::jsonb,
    metrics         JSONB NOT NULL DEFAULT '{}'::jsonb,
    -- training-distribution stats the drift engine compares live traffic against
    baseline_stats  JSONB,
    stage           TEXT NOT NULL DEFAULT 'registered'
                    CHECK (stage IN ('registered', 'staging', 'production', 'archived')),
    created_at      TIMESTAMPTZ NOT NULL DEFAULT now(),
    promoted_at     TIMESTAMPTZ,
    UNIQUE (model_id, version)
);

-- At most one production version per model; enforced by the DB, not just app code.
CREATE UNIQUE INDEX IF NOT EXISTS model_versions_one_production
    ON model_versions (model_id) WHERE stage = 'production';
