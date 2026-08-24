-- +goose Up
-- +goose StatementBegin

-- The sprint commitment, snapshotted when the iteration starts.
--
-- Velocity derived both numbers from issues whose iteration_id *currently* points at
-- the iteration:
--
--     count(*) FILTER (WHERE status IN ('resolved','closed'))  -- done
--     count(id)                                                -- "planned"
--
-- and carry-over moves out exactly the issues that are not resolved or closed. So the
-- moment a sprint was closed with carry-over, the only issues still pointing at it
-- were the finished ones, planned collapsed onto done, and the panel rendered
-- "12/12 done". Every sprint that carried anything over reported 100%, and the ones
-- that carried the most looked identical to the ones that carried nothing — in the
-- number retrospectives are run on, failing in the most flattering direction.
--
-- Commitment is a fact about a moment in time, so it is stored rather than
-- re-derived, for the same reason 00025 gives for storing burndown: a chart that
-- rewrites its own past is worse than no chart, because people trust it.
--
-- NULL means "never snapshotted", which is true of every iteration that existed
-- before this migration and of any that was completed without being activated. The
-- velocity query falls back to the old derivation for those and marks them, rather
-- than inventing a baseline nobody recorded.
ALTER TABLE iterations
    ADD COLUMN committed_issues  INTEGER,
    ADD COLUMN committed_minutes INTEGER,
    ADD COLUMN committed_at      TIMESTAMPTZ;

-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
ALTER TABLE iterations
    DROP COLUMN IF EXISTS committed_issues,
    DROP COLUMN IF EXISTS committed_minutes,
    DROP COLUMN IF EXISTS committed_at;
-- +goose StatementEnd
