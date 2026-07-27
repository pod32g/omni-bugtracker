-- +goose Up
-- +goose StatementBegin

-- Issue templates. NewIssueForm hardcodes exactly one shape — the four bug narrative
-- fields, and nothing at all for the other three types — so the quality of a filed
-- issue depends entirely on who filed it, and triage pays for it afterwards.

CREATE TABLE issue_templates (
    id            UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    project_id    UUID NOT NULL REFERENCES projects(id) ON DELETE CASCADE,
    name          TEXT NOT NULL,
    issue_type    issue_type NOT NULL,
    body_md       TEXT NOT NULL DEFAULT '',
    -- Markdown headings that must be present and non-empty in the submitted
    -- description. Stored as the heading text, matched case-insensitively.
    required_sections TEXT[] NOT NULL DEFAULT '{}',
    -- Defaults applied at filing time. Nullable/empty means "don't set it".
    default_labels    TEXT[] NOT NULL DEFAULT '{}',
    default_component TEXT NOT NULL DEFAULT '',
    default_priority  priority,
    default_severity  severity,
    default_assignee_id UUID REFERENCES users(id) ON DELETE SET NULL,
    -- One default per (project, type): the create form has to pick something when the
    -- type changes, and "whichever row came back first" is not a decision.
    is_default    BOOLEAN NOT NULL DEFAULT FALSE,
    position      INTEGER NOT NULL DEFAULT 0,
    created_at    TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE UNIQUE INDEX idx_issue_templates_name ON issue_templates (project_id, issue_type, lower(name));
CREATE UNIQUE INDEX idx_issue_templates_default ON issue_templates (project_id, issue_type)
    WHERE is_default;
CREATE INDEX idx_issue_templates_project ON issue_templates (project_id, issue_type, position);

-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
DROP TABLE IF EXISTS issue_templates;
-- +goose StatementEnd
