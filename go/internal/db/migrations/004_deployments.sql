CREATE TABLE IF NOT EXISTS deployments (
    id               UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    model_version_id UUID NOT NULL REFERENCES model_versions(id) ON DELETE RESTRICT,
    environment      TEXT NOT NULL DEFAULT 'production',
    status           TEXT NOT NULL DEFAULT 'pending'
                     CHECK (status IN ('pending', 'running', 'stopped', 'failed', 'rolled_back')),
    -- share of traffic routed here by the canary proxy
    traffic_percent  INT NOT NULL DEFAULT 100 CHECK (traffic_percent BETWEEN 0 AND 100),
    endpoint         TEXT NOT NULL DEFAULT '',
    container_id     TEXT NOT NULL DEFAULT '',
    started_at       TIMESTAMPTZ,
    stopped_at       TIMESTAMPTZ,
    created_at       TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at       TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE INDEX IF NOT EXISTS deployments_version_status ON deployments (model_version_id, status);
