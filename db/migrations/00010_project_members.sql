-- +goose Up
-- +goose StatementBegin

-- Per-project membership. A row elevates the user's capabilities inside that
-- project: effective permission = global role OR project role (checked in the
-- HTTP layer). Global owner/admin remain unrestricted everywhere. Reuses the
-- app_role enum — owner/admin at project scope read as "project admin".
CREATE TABLE project_members (
    project_id UUID NOT NULL REFERENCES projects(id) ON DELETE CASCADE,
    user_id    UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    role       app_role NOT NULL DEFAULT 'member',
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    PRIMARY KEY (project_id, user_id)
);
CREATE INDEX idx_project_members_user ON project_members (user_id);

-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
DROP TABLE IF EXISTS project_members;
-- +goose StatementEnd
