-- +goose Up
-- +goose StatementBegin

-- Iterations. Milestones say what; iterations say when. A milestone has a due date
-- and nothing else, so there has never been a way to name "the two weeks we are
-- working on now" — which is what the board and the dashboard both silently assume.

CREATE TYPE iteration_state AS ENUM ('planned', 'active', 'completed');

CREATE TABLE iterations (
    id         UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    project_id UUID NOT NULL REFERENCES projects(id) ON DELETE CASCADE,
    name       TEXT NOT NULL,
    starts_on  DATE NOT NULL,
    ends_on    DATE NOT NULL,
    state      iteration_state NOT NULL DEFAULT 'planned',
    goal       TEXT NOT NULL DEFAULT '',
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    CHECK (ends_on >= starts_on)
);

CREATE UNIQUE INDEX idx_iterations_name ON iterations (project_id, lower(name));

-- At most one active iteration per project. "The current iteration" has to resolve
-- to exactly one thing or `iteration:current` means nothing — and a partial unique
-- index is the only place that can actually be guaranteed.
CREATE UNIQUE INDEX idx_iterations_one_active ON iterations (project_id)
    WHERE state = 'active';

CREATE INDEX idx_iterations_project ON iterations (project_id, starts_on DESC);

-- Membership is a column on the issue rather than a join table: an issue belongs to
-- at most one iteration at a time, and a join table would let it be in two, which is
-- exactly the state a burndown cannot represent.
ALTER TABLE issues ADD COLUMN iteration_id UUID REFERENCES iterations(id) ON DELETE SET NULL;
CREATE INDEX idx_issues_iteration ON issues (iteration_id) WHERE iteration_id IS NOT NULL;

-- Burndown is stored, not recomputed. Deriving it from current state would redraw
-- history every time an issue was re-estimated or moved out — a chart that rewrites
-- its own past is worse than no chart, because people trust it.
CREATE TABLE iteration_snapshots (
    iteration_id      UUID NOT NULL REFERENCES iterations(id) ON DELETE CASCADE,
    on_date           DATE NOT NULL,
    remaining_issues  INTEGER NOT NULL,
    remaining_minutes INTEGER NOT NULL,
    total_issues      INTEGER NOT NULL,
    total_minutes     INTEGER NOT NULL,
    PRIMARY KEY (iteration_id, on_date)
);

-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
DROP TABLE IF EXISTS iteration_snapshots;
DROP INDEX IF EXISTS idx_issues_iteration;
ALTER TABLE issues DROP COLUMN IF EXISTS iteration_id;
DROP TABLE IF EXISTS iterations;
DROP TYPE IF EXISTS iteration_state;
-- +goose StatementEnd
