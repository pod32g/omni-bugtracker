-- +goose Up
-- +goose StatementBegin

-- Audit log for privileged actions. Activity is per-issue and read from
-- /issues/{key}/activity; everything happening outside an issue was invisible — role
-- changes, token creation and revocation, project archival and key renames, webhook and
-- automation edits, membership changes, the auto-archive toggle. Nothing in the system
-- could answer "who made that account an owner, and when".
--
-- Append-only by convention and by API: there is no update or delete path. Retention is
-- a deliberate admin action, not something the application does behind your back.
CREATE TABLE audit_log (
    id           UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    actor_id     UUID REFERENCES users (id) ON DELETE SET NULL,
    -- The actor's email at the time of the action. Denormalised on purpose: an audit
    -- entry that becomes anonymous because the account was deleted is worth very little,
    -- and ON DELETE SET NULL would do exactly that.
    actor_email  TEXT NOT NULL DEFAULT '',
    action       TEXT NOT NULL,
    target_type  TEXT NOT NULL,
    target_id    TEXT NOT NULL DEFAULT '',
    -- A human label for the target, captured now, because ids stop resolving once the
    -- thing is gone — and "deleted webhook <uuid>" is the entry you most want to read.
    target_label TEXT NOT NULL DEFAULT '',
    details      JSONB NOT NULL DEFAULT '{}',
    ip           TEXT NOT NULL DEFAULT '',
    user_agent   TEXT NOT NULL DEFAULT '',
    -- Whether the call arrived on an API token, and which one. A privileged change made
    -- by a script is a different fact from one made in a browser.
    via_token    BOOLEAN NOT NULL DEFAULT FALSE,
    token_id     UUID,
    created_at   TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE INDEX idx_audit_log_time ON audit_log (created_at DESC);
CREATE INDEX idx_audit_log_actor ON audit_log (actor_id, created_at DESC);
CREATE INDEX idx_audit_log_action ON audit_log (action, created_at DESC);

-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
DROP TABLE IF EXISTS audit_log;
-- +goose StatementEnd
