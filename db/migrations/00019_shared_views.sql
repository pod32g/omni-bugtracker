-- +goose Up
-- +goose StatementBegin

-- Shared team views. `is_shared` has been on this table since it was created and
-- nothing ever read it: saved searches were personal, so a team that agreed on what
-- "needs triage" means had to tell everyone the filter string and hope they typed it
-- the same way.
ALTER TABLE saved_searches ADD COLUMN project_id  UUID REFERENCES projects (id) ON DELETE CASCADE;
ALTER TABLE saved_searches ADD COLUMN description TEXT NOT NULL DEFAULT '';
ALTER TABLE saved_searches ADD COLUMN sort        TEXT NOT NULL DEFAULT '';
ALTER TABLE saved_searches ADD COLUMN position    INT  NOT NULL DEFAULT 0;
-- The view the issue list opens with when no filter is given. At most one per project.
ALTER TABLE saved_searches ADD COLUMN is_default  BOOLEAN NOT NULL DEFAULT FALSE;

-- A shared view belongs to a project; a personal one does not. Enforced rather than
-- assumed, because a shared view with no project would be visible to nobody and
-- deletable by no one.
ALTER TABLE saved_searches ADD CONSTRAINT saved_searches_shared_has_project
    CHECK ((is_shared AND project_id IS NOT NULL) OR (NOT is_shared AND project_id IS NULL));

-- Name uniqueness moves from "per user" to "per user, personal" + "per project, shared":
-- two people must not both create "Triage" in the same project, and a shared view's name
-- is not the creator's to reserve globally.
ALTER TABLE saved_searches DROP CONSTRAINT IF EXISTS saved_searches_user_id_name_key;
CREATE UNIQUE INDEX idx_saved_searches_personal_name
    ON saved_searches (user_id, lower(name)) WHERE NOT is_shared;
CREATE UNIQUE INDEX idx_saved_searches_shared_name
    ON saved_searches (project_id, lower(name)) WHERE is_shared;

-- One default per project, enforced by the index rather than by application code that
-- would have to remember to clear the old one under concurrency.
CREATE UNIQUE INDEX idx_saved_searches_one_default
    ON saved_searches (project_id) WHERE is_default;

CREATE INDEX idx_saved_searches_project ON saved_searches (project_id, position) WHERE is_shared;

-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
DROP INDEX IF EXISTS idx_saved_searches_project;
DROP INDEX IF EXISTS idx_saved_searches_one_default;
DROP INDEX IF EXISTS idx_saved_searches_shared_name;
DROP INDEX IF EXISTS idx_saved_searches_personal_name;
ALTER TABLE saved_searches DROP CONSTRAINT IF EXISTS saved_searches_shared_has_project;
ALTER TABLE saved_searches DROP COLUMN IF EXISTS is_default;
ALTER TABLE saved_searches DROP COLUMN IF EXISTS position;
ALTER TABLE saved_searches DROP COLUMN IF EXISTS sort;
ALTER TABLE saved_searches DROP COLUMN IF EXISTS description;
ALTER TABLE saved_searches DROP COLUMN IF EXISTS project_id;
ALTER TABLE saved_searches ADD CONSTRAINT saved_searches_user_id_name_key UNIQUE (user_id, name);
-- +goose StatementEnd
