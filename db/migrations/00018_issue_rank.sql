-- +goose Up
-- +goose StatementBegin

-- Manual ordering within a board column. Dropping a card only ever changed its status;
-- cards inside a column came back in whatever order the query produced, so "what should
-- I pick up next" was not something the board could express.
--
-- A lexicographic key rather than an integer position: inserting between two cards is
-- one row write and never a renumbering of the column, which matters because the board
-- is shared and two people reordering at once must not fight over every row.
--
-- NULL means never ranked, which is every existing issue — the board falls back to
-- updated_at for those, so no backfill is needed and an unranked project behaves
-- exactly as it does today.
ALTER TABLE issues ADD COLUMN rank TEXT;

-- Board reads are (project, rank); the partial index skips the unranked majority.
CREATE INDEX idx_issues_rank ON issues (project_id, rank)
    WHERE rank IS NOT NULL AND deleted_at IS NULL;

-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
DROP INDEX IF EXISTS idx_issues_rank;
ALTER TABLE issues DROP COLUMN IF EXISTS rank;
-- +goose StatementEnd
