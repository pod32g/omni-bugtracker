-- +goose Up
-- +goose StatementBegin

-- Emoji reactions. Agreeing with somebody cost a comment saying "+1", which notified
-- every watcher — the most common thing anyone wants to express was also the noisiest.
--
-- The target is either a comment or the issue body, so exactly one of the two columns
-- is set. A single table keeps the read path one query instead of a union.
CREATE TABLE reactions (
    id         UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    issue_id   UUID NOT NULL REFERENCES issues (id) ON DELETE CASCADE,
    comment_id UUID REFERENCES comments (id) ON DELETE CASCADE,
    user_id    UUID NOT NULL REFERENCES users (id) ON DELETE CASCADE,
    emoji      TEXT NOT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),

    -- The emoji set is closed and enforced here rather than only in the handler: the
    -- column is rendered directly into the page, and an open text column reachable by
    -- any commenter is an invitation nobody needs.
    CONSTRAINT reactions_known_emoji CHECK (emoji IN ('+1', '-1', 'tada', 'confused', 'heart', 'rocket', 'eyes'))
);

-- One of each emoji per person per target. Clicking again removes it, so this is the
-- toggle's correctness rather than an optimisation.
CREATE UNIQUE INDEX idx_reactions_unique_comment
    ON reactions (comment_id, user_id, emoji) WHERE comment_id IS NOT NULL;
CREATE UNIQUE INDEX idx_reactions_unique_issue
    ON reactions (issue_id, user_id, emoji) WHERE comment_id IS NULL;

-- The issue page reads every reaction for an issue in one go.
CREATE INDEX idx_reactions_issue ON reactions (issue_id);

-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
DROP TABLE IF EXISTS reactions;
-- +goose StatementEnd
