-- +goose Up
-- +goose StatementBegin

-- Cross-references: "same root cause as BUG-33" written in a description or a comment.
-- Distinct from `issue_relations`, which is a deliberate typed link (blocks, duplicates)
-- created through the relations UI. A reference is incidental, derived from prose, and
-- recomputed whenever that prose changes — so it is never edited directly.
CREATE TABLE issue_references (
    id                UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    source_issue_id   UUID NOT NULL REFERENCES issues (id) ON DELETE CASCADE,
    target_issue_id   UUID NOT NULL REFERENCES issues (id) ON DELETE CASCADE,
    -- NULL = the reference came from the source issue's own body; otherwise the comment
    -- that carried it. Recomputation is scoped by this, so re-editing a description does
    -- not drop references made in the comments below it.
    source_comment_id UUID REFERENCES comments (id) ON DELETE CASCADE,
    created_at        TIMESTAMPTZ NOT NULL DEFAULT now(),

    CONSTRAINT issue_references_no_self CHECK (source_issue_id <> target_issue_id)
);

-- The "Referenced by" panel reads by target.
CREATE INDEX idx_issue_references_target ON issue_references (target_issue_id);
-- Recomputation deletes by source, then by comment.
CREATE INDEX idx_issue_references_source ON issue_references (source_issue_id, source_comment_id);

-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
DROP TABLE IF EXISTS issue_references;
-- +goose StatementEnd
