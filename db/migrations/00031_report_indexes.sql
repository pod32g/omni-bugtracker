-- +goose NO TRANSACTION
-- +goose Up

-- The reports page counts issues created and resolved per week over a window.
--
-- Both halves now compare the raw timestamp against a range (see reports.go), which is
-- sargable — but only created_at had an index behind it. Resolution is the more
-- interesting series of the two, and it was the one doing a full scan.
--
-- Partial, because resolved_at is NULL for everything still open: on a healthy backlog
-- that is a large fraction of the table, and excluding it makes the index smaller than
-- the column it covers. It also matches the query, which cannot see NULLs through a
-- range predicate anyway.
CREATE INDEX CONCURRENTLY IF NOT EXISTS idx_issues_resolved_at
    ON issues (resolved_at)
 WHERE resolved_at IS NOT NULL AND deleted_at IS NULL;

-- +goose Down
DROP INDEX CONCURRENTLY IF EXISTS idx_issues_resolved_at;
