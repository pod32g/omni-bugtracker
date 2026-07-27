-- +goose Up
-- +goose StatementBegin

-- Estimates and time tracking. Milestones and releases have only ever been able to
-- count issues, which quietly assumes they are all the same size.

ALTER TABLE issues ADD COLUMN estimate_minutes INTEGER
    CHECK (estimate_minutes IS NULL OR estimate_minutes > 0);

-- Spent time is the sum of entries, never a column on the issue. A running total
-- somebody overwrites loses the correction; a ledger keeps "3h" and "actually 5h,
-- the retry loop" as two facts, which is what makes the number defensible later.
CREATE TABLE time_entries (
    id         UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    issue_id   UUID NOT NULL REFERENCES issues(id) ON DELETE CASCADE,
    user_id    UUID REFERENCES users(id) ON DELETE SET NULL,
    minutes    INTEGER NOT NULL CHECK (minutes > 0),
    -- The day the work happened, which is often not the day it was logged.
    spent_on   DATE NOT NULL DEFAULT CURRENT_DATE,
    note       TEXT NOT NULL DEFAULT '',
    created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE INDEX idx_time_entries_issue ON time_entries (issue_id, spent_on DESC);
CREATE INDEX idx_time_entries_user  ON time_entries (user_id, spent_on DESC);

-- issue_time is the per-issue rollup every other rollup builds on. Spent time is a
-- LEFT JOIN away everywhere it is needed, so no caller has to remember to aggregate
-- and none of them can disagree about what "spent" means.
CREATE VIEW issue_time AS
SELECT issue_id, sum(minutes)::INTEGER AS spent_minutes, count(*)::INTEGER AS entry_count
  FROM time_entries
 GROUP BY issue_id;

-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
DROP VIEW IF EXISTS issue_time;
DROP TABLE IF EXISTS time_entries;
ALTER TABLE issues DROP COLUMN IF EXISTS estimate_minutes;
-- +goose StatementEnd
