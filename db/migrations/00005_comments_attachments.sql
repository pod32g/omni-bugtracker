-- +goose Up
-- +goose StatementBegin

CREATE TABLE comments (
    id         UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    issue_id   UUID NOT NULL REFERENCES issues(id) ON DELETE CASCADE,
    author_id  UUID REFERENCES users(id) ON DELETE SET NULL,
    body_md    TEXT NOT NULL,
    edited_at  TIMESTAMPTZ,
    deleted_at TIMESTAMPTZ,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    fts        tsvector GENERATED ALWAYS AS (to_tsvector('english', coalesce(body_md, ''))) STORED
);
CREATE INDEX idx_comments_issue ON comments (issue_id, created_at) WHERE deleted_at IS NULL;
CREATE INDEX idx_comments_fts   ON comments USING GIN (fts);

-- Attachment bytes live in Omni-Upload; we store metadata + object key only.
CREATE TABLE attachments (
    id             UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    issue_id       UUID REFERENCES issues(id) ON DELETE CASCADE,
    comment_id     UUID REFERENCES comments(id) ON DELETE CASCADE,
    uploader_id    UUID REFERENCES users(id) ON DELETE SET NULL,
    filename       TEXT NOT NULL,
    content_type   TEXT NOT NULL,
    size_bytes     BIGINT NOT NULL,
    upload_object_key TEXT NOT NULL,
    checksum       TEXT NOT NULL DEFAULT '',
    created_at     TIMESTAMPTZ NOT NULL DEFAULT now(),
    CONSTRAINT attachment_parent CHECK (issue_id IS NOT NULL OR comment_id IS NOT NULL)
);
CREATE INDEX idx_attachments_issue   ON attachments (issue_id);
CREATE INDEX idx_attachments_comment ON attachments (comment_id);

-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
DROP TABLE IF EXISTS attachments;
DROP TABLE IF EXISTS comments;
-- +goose StatementEnd
