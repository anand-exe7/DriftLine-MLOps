CREATE TABLE IF NOT EXISTS drift_reports (
    id               UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    model_version_id UUID NOT NULL REFERENCES model_versions(id) ON DELETE CASCADE,
    window_start     TIMESTAMPTZ NOT NULL,
    window_end       TIMESTAMPTZ NOT NULL,
    sample_size      INT NOT NULL DEFAULT 0,
    overall_psi      DOUBLE PRECISION NOT NULL DEFAULT 0,
    drift_detected   BOOLEAN NOT NULL DEFAULT false,
    -- {"Age": {"psi": 0.31, "ks_stat": 0.12, "ks_p": 0.001}, ...}
    feature_scores   JSONB NOT NULL DEFAULT '{}'::jsonb,
    created_at       TIMESTAMPTZ NOT NULL DEFAULT now(),
    CHECK (window_end > window_start)
);

CREATE INDEX IF NOT EXISTS drift_reports_version_time ON drift_reports (model_version_id, created_at DESC);
