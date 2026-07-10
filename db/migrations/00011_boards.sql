-- +goose Up
-- +goose StatementBegin

-- Configurable Kanban boards: one per project (auto-created on first view),
-- with columns mapping to one or more workflow statuses. Dropping a card on a
-- column transitions the issue to the column's first status.
CREATE TABLE boards (
    id         UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    project_id UUID NOT NULL REFERENCES projects(id) ON DELETE CASCADE,
    name       TEXT NOT NULL DEFAULT 'Board',
    swimlane   TEXT NOT NULL DEFAULT 'none',   -- none | assignee | priority
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    UNIQUE (project_id, name)
);

CREATE TABLE board_columns (
    id        UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    board_id  UUID NOT NULL REFERENCES boards(id) ON DELETE CASCADE,
    name      TEXT NOT NULL,
    statuses  TEXT[] NOT NULL,                 -- issue_status values (validated at API)
    wip_limit INTEGER,                         -- NULL = unlimited
    position  INTEGER NOT NULL DEFAULT 0,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE INDEX idx_board_columns_board ON board_columns (board_id, position);

-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
DROP TABLE IF EXISTS board_columns;
DROP TABLE IF EXISTS boards;
-- +goose StatementEnd
