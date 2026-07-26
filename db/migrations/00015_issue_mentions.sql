-- +goose Up
-- +goose StatementBegin

-- @mentions written in an issue body or a comment. Recorded rather than derived on the
-- fly so a mention notifies exactly once: notified_at is stamped when the dispatcher
-- picks the row up, and re-saving the same text finds the row already there.
CREATE TABLE issue_mentions (
    id                UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    issue_id          UUID NOT NULL REFERENCES issues (id) ON DELETE CASCADE,
    -- NULL = the mention is in the issue body; otherwise the comment carrying it.
    -- Scopes recomputation, exactly as issue_references does.
    source_comment_id UUID REFERENCES comments (id) ON DELETE CASCADE,
    user_id           UUID NOT NULL REFERENCES users (id) ON DELETE CASCADE,
    mentioned_by      UUID REFERENCES users (id) ON DELETE SET NULL,
    notified_at       TIMESTAMPTZ,
    created_at        TIMESTAMPTZ NOT NULL DEFAULT now()
);

-- The dispatcher claims unnotified mentions for one issue.
CREATE INDEX idx_issue_mentions_pending ON issue_mentions (issue_id) WHERE notified_at IS NULL;
-- Recomputation deletes by source; the inbox reads by user.
CREATE INDEX idx_issue_mentions_source ON issue_mentions (issue_id, source_comment_id);
CREATE INDEX idx_issue_mentions_user ON issue_mentions (user_id, created_at DESC);

-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
DROP TABLE IF EXISTS issue_mentions;
-- +goose StatementEnd
