-- A model runs at most one primary deployment plus one canary; the canary's
-- traffic_percent is carved out of the primary's. Rollback uses role history
-- to find the previous primary.
ALTER TABLE deployments
    ADD COLUMN IF NOT EXISTS role TEXT NOT NULL DEFAULT 'primary' CHECK (role IN ('primary', 'canary')),
    ADD COLUMN IF NOT EXISTS status_reason TEXT NOT NULL DEFAULT '';

CREATE INDEX IF NOT EXISTS deployments_status ON deployments (status) WHERE status = 'running';
