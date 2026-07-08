-- +goose Up
-- +goose StatementBegin

CREATE TABLE projects (
    id                 UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    key                TEXT NOT NULL UNIQUE,          -- e.g. "BUG", "API"; used in issue keys
    name               TEXT NOT NULL,
    description_md     TEXT NOT NULL DEFAULT '',
    default_assignee_id UUID REFERENCES users(id) ON DELETE SET NULL,
    next_issue_number  INTEGER NOT NULL DEFAULT 1,    -- allocated in-tx on issue create
    is_archived        BOOLEAN NOT NULL DEFAULT FALSE,
    settings           JSONB NOT NULL DEFAULT '{}',
    created_at         TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at         TIMESTAMPTZ NOT NULL DEFAULT now(),
    CONSTRAINT project_key_format CHECK (key ~ '^[A-Z][A-Z0-9]{1,9}$')
);

CREATE TABLE components (
    id             UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    project_id     UUID NOT NULL REFERENCES projects(id) ON DELETE CASCADE,
    name           TEXT NOT NULL,
    description_md TEXT NOT NULL DEFAULT '',
    lead_id        UUID REFERENCES users(id) ON DELETE SET NULL,
    created_at     TIMESTAMPTZ NOT NULL DEFAULT now(),
    UNIQUE (project_id, name)
);

-- Labels may be project-scoped (project_id set) or global (project_id NULL).
CREATE TABLE labels (
    id             UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    project_id     UUID REFERENCES projects(id) ON DELETE CASCADE,
    name           TEXT NOT NULL,
    color          TEXT NOT NULL DEFAULT '#8b5cf6',
    description    TEXT NOT NULL DEFAULT '',
    created_at     TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE UNIQUE INDEX idx_labels_scope_name ON labels (COALESCE(project_id, '00000000-0000-0000-0000-000000000000'), lower(name));

CREATE TABLE milestones (
    id             UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    project_id     UUID NOT NULL REFERENCES projects(id) ON DELETE CASCADE,
    title          TEXT NOT NULL,
    description_md TEXT NOT NULL DEFAULT '',
    due_on         DATE,
    state          milestone_state NOT NULL DEFAULT 'open',
    created_at     TIMESTAMPTZ NOT NULL DEFAULT now(),
    UNIQUE (project_id, title)
);

CREATE TABLE releases (
    id             UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    project_id     UUID NOT NULL REFERENCES projects(id) ON DELETE CASCADE,
    version        TEXT NOT NULL,                 -- semver-ish, e.g. "2.1.0"
    name           TEXT NOT NULL DEFAULT '',
    notes_md       TEXT NOT NULL DEFAULT '',
    state          release_state NOT NULL DEFAULT 'draft',
    git_tag        TEXT NOT NULL DEFAULT '',
    released_at    TIMESTAMPTZ,
    created_by     UUID REFERENCES users(id) ON DELETE SET NULL,
    created_at     TIMESTAMPTZ NOT NULL DEFAULT now(),
    UNIQUE (project_id, version)
);

-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
DROP TABLE IF EXISTS releases;
DROP TABLE IF EXISTS milestones;
DROP TABLE IF EXISTS labels;
DROP TABLE IF EXISTS components;
DROP TABLE IF EXISTS projects;
-- +goose StatementEnd
