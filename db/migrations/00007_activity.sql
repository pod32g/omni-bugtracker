-- +goose Up
-- +goose StatementBegin

-- Append-only activity/audit log. Every mutation writes one row (same tx as the change).
-- Powers both the per-issue timeline and the global audit history.
CREATE TABLE activity (
    id          UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    issue_id    UUID REFERENCES issues(id) ON DELETE CASCADE,
    actor_id    UUID REFERENCES users(id) ON DELETE SET NULL,
    verb        TEXT NOT NULL,                 -- e.g. issue.created, issue.status_changed
    entity_type TEXT NOT NULL,                 -- issue|comment|attachment|release|...
    entity_id   UUID,
    changes     JSONB NOT NULL DEFAULT '{}',   -- {field: {from, to}}
    occurred_at TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE INDEX idx_activity_issue    ON activity (issue_id, occurred_at DESC);
CREATE INDEX idx_activity_actor    ON activity (actor_id, occurred_at DESC);
CREATE INDEX idx_activity_occurred ON activity USING BRIN (occurred_at);

-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
DROP TABLE IF EXISTS activity;
-- +goose StatementEnd
