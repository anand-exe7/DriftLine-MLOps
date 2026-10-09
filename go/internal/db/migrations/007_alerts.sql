CREATE TABLE IF NOT EXISTS alerts (
    id               UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    model_version_id UUID REFERENCES model_versions(id) ON DELETE CASCADE,
    drift_report_id  UUID REFERENCES drift_reports(id) ON DELETE SET NULL,
    severity         TEXT NOT NULL CHECK (severity IN ('info', 'warning', 'critical')),
    channel          TEXT NOT NULL,          -- "slack", "discord", ...
    message          TEXT NOT NULL,
    status           TEXT NOT NULL CHECK (status IN ('sent', 'failed')),
    error            TEXT NOT NULL DEFAULT '',
    created_at       TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE INDEX IF NOT EXISTS alerts_version_time ON alerts (model_version_id, created_at DESC);
