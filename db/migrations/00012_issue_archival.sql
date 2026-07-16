-- +goose Up
-- +goose StatementBegin

-- Archival: hide finished issues from default lists + full-text search without
-- deleting them. Distinct from status (an issue stays 'closed') and from soft-delete
-- (deleted_at) — archived issues remain recoverable and reachable by their key.
ALTER TABLE issues ADD COLUMN archived_at TIMESTAMPTZ;

-- Partial index for the common "active" working set (not deleted, not archived).
CREATE INDEX idx_issues_active ON issues (project_id, status)
    WHERE deleted_at IS NULL AND archived_at IS NULL;

-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
DROP INDEX IF EXISTS idx_issues_active;
ALTER TABLE issues DROP COLUMN IF EXISTS archived_at;
-- +goose StatementEnd
