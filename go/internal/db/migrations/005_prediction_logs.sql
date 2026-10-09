-- High-volume append-only table; BIGSERIAL keeps inserts cheap and ordered.
CREATE TABLE IF NOT EXISTS prediction_logs (
    id               BIGSERIAL PRIMARY KEY,
    request_id       TEXT NOT NULL,
    model_version_id UUID NOT NULL REFERENCES model_versions(id) ON DELETE CASCADE,
    deployment_id    UUID REFERENCES deployments(id) ON DELETE SET NULL,
    features         JSONB NOT NULL,
    prediction       INT,
    probabilities    DOUBLE PRECISION[] NOT NULL DEFAULT '{}',
    latency_ms       BIGINT NOT NULL DEFAULT 0,
    status_code      INT NOT NULL DEFAULT 0,
    error_message    TEXT NOT NULL DEFAULT '',
    created_at       TIMESTAMPTZ NOT NULL DEFAULT now()
);

-- The drift scheduler reads "all predictions for version X in window [a, b)".
CREATE INDEX IF NOT EXISTS prediction_logs_version_time ON prediction_logs (model_version_id, created_at);
