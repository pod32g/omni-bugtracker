-- +goose NO TRANSACTION
-- +goose Up

-- Reverse indexes on the join tables.
--
-- Each of these tables is indexed on its leading column only — issue_labels and
-- issue_components by their composite primary key, issue_relations by its
-- UNIQUE (from_issue, to_issue, kind). Postgres can use a composite index for a
-- predicate on its *first* column, so every lookup in the other direction is a
-- sequential scan of the whole table:
--
--   * `label:` filters and the per-label counts on the labels settings page scan
--     issue_labels once per label rendered;
--   * deleting a label or a component scans the join table to cascade;
--   * the open_blockers subquery on every issue read correlates on to_issue, which
--     nothing indexes at all — so the cost of loading a page of issues grows with the
--     total number of relations in the install, not with the page.
--
-- CONCURRENTLY, and therefore NO TRANSACTION: these tables are read on every issue
-- list, and a plain CREATE INDEX takes a lock that would stall the whole tracker for
-- the duration of the build. The trade is that a failure leaves an INVALID index
-- behind, which has to be dropped before retrying — goose cannot roll this back for us,
-- which is exactly why the down migration drops by name.
CREATE INDEX CONCURRENTLY IF NOT EXISTS idx_issue_labels_label
    ON issue_labels (label_id);

CREATE INDEX CONCURRENTLY IF NOT EXISTS idx_issue_components_component
    ON issue_components (component_id);

-- kind is included because the blocker check filters on it: an index-only scan
-- answers "does anything block this issue" without touching the heap.
CREATE INDEX CONCURRENTLY IF NOT EXISTS idx_issue_relations_to
    ON issue_relations (to_issue, kind);

-- +goose Down
DROP INDEX CONCURRENTLY IF EXISTS idx_issue_relations_to;
DROP INDEX CONCURRENTLY IF EXISTS idx_issue_components_component;
DROP INDEX CONCURRENTLY IF EXISTS idx_issue_labels_label;
