-- +goose Up
-- +goose StatementBegin

-- The in-app inbox. Every notification the tracker produced went straight out to
-- Omni-Notify and nowhere else, so inside the app there was no bell, no unread count
-- and no list — the only way to learn someone had replied was to remember which issues
-- you were watching and go look.
--
-- Written by the same fan-out step that pushes outward, not instead of it.
CREATE TABLE notifications (
    id         UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    user_id    UUID NOT NULL REFERENCES users (id) ON DELETE CASCADE,
    issue_id   UUID NOT NULL REFERENCES issues (id) ON DELETE CASCADE,
    event_type TEXT NOT NULL,
    actor_id   UUID REFERENCES users (id) ON DELETE SET NULL,
    read_at    TIMESTAMPTZ,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

-- The inbox reads one user's rows newest-first; the badge counts their unread ones.
CREATE INDEX idx_notifications_user ON notifications (user_id, created_at DESC);

-- At most one UNREAD row per user, issue and event type. Ten comments on an issue you
-- watch is one thing to look at, not ten, and a badge that counts events rather than
-- things-needing-attention is one people learn to ignore. A repeat bumps the existing
-- row instead of adding another; once read, the next event starts a fresh row.
--
-- This index is also what makes the ON CONFLICT in RecordNotifications work, so it is
-- load-bearing rather than an optimisation.
CREATE UNIQUE INDEX idx_notifications_one_unread
    ON notifications (user_id, issue_id, event_type) WHERE read_at IS NULL;

-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
DROP TABLE IF EXISTS notifications;
-- +goose StatementEnd
