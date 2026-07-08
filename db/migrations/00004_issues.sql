-- +goose Up
-- +goose StatementBegin

-- One unified table for bugs, tasks, features and improvements (type discriminator).
CREATE TABLE issues (
    id               UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    project_id       UUID NOT NULL REFERENCES projects(id) ON DELETE CASCADE,
    number           INTEGER NOT NULL,             -- per-project; key = project.key || '-' || number
    type             issue_type NOT NULL DEFAULT 'bug',
    title            TEXT NOT NULL,
    description_md   TEXT NOT NULL DEFAULT '',
    status           issue_status NOT NULL DEFAULT 'open',
    severity         severity,                     -- bugs
    priority         priority NOT NULL DEFAULT 'p2',
    reporter_id      UUID REFERENCES users(id) ON DELETE SET NULL,
    assignee_id      UUID REFERENCES users(id) ON DELETE SET NULL,
    milestone_id     UUID REFERENCES milestones(id) ON DELETE SET NULL,
    release_id       UUID REFERENCES releases(id) ON DELETE SET NULL,
    version_affected TEXT NOT NULL DEFAULT '',
    version_fixed    TEXT NOT NULL DEFAULT '',
    git_commit_sha   TEXT NOT NULL DEFAULT '',
    pull_request_url TEXT NOT NULL DEFAULT '',
    -- Bug-specific narrative (nullable — only bugs typically fill these).
    repro_steps_md   TEXT NOT NULL DEFAULT '',
    expected_md      TEXT NOT NULL DEFAULT '',
    actual_md        TEXT NOT NULL DEFAULT '',
    environment_md   TEXT NOT NULL DEFAULT '',
    fields           JSONB NOT NULL DEFAULT '{}',  -- escape hatch, not a custom-field engine
    source           issue_source NOT NULL DEFAULT 'human',
    dedupe_key       TEXT,                          -- fingerprint for obs-ingested issues
    created_at       TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at       TIMESTAMPTZ NOT NULL DEFAULT now(),
    resolved_at      TIMESTAMPTZ,
    closed_at        TIMESTAMPTZ,
    deleted_at       TIMESTAMPTZ,
    fts              tsvector GENERATED ALWAYS AS (
                         setweight(to_tsvector('english', coalesce(title, '')), 'A') ||
                         setweight(to_tsvector('english', coalesce(description_md, '')), 'B') ||
                         setweight(to_tsvector('english', coalesce(actual_md, '')), 'C') ||
                         setweight(to_tsvector('english', coalesce(repro_steps_md, '')), 'C')
                      ) STORED,
    UNIQUE (project_id, number)
);
CREATE INDEX idx_issues_fts        ON issues USING GIN (fts);
CREATE INDEX idx_issues_status     ON issues (project_id, status) WHERE deleted_at IS NULL;
CREATE INDEX idx_issues_assignee   ON issues (assignee_id, status) WHERE deleted_at IS NULL;
CREATE INDEX idx_issues_milestone  ON issues (milestone_id) WHERE deleted_at IS NULL;
CREATE INDEX idx_issues_release    ON issues (release_id) WHERE deleted_at IS NULL;
CREATE INDEX idx_issues_created    ON issues USING BRIN (created_at);
CREATE UNIQUE INDEX idx_issues_dedupe ON issues (project_id, dedupe_key)
    WHERE dedupe_key IS NOT NULL AND deleted_at IS NULL;

CREATE TABLE issue_components (
    issue_id     UUID NOT NULL REFERENCES issues(id) ON DELETE CASCADE,
    component_id UUID NOT NULL REFERENCES components(id) ON DELETE CASCADE,
    PRIMARY KEY (issue_id, component_id)
);

CREATE TABLE issue_labels (
    issue_id UUID NOT NULL REFERENCES issues(id) ON DELETE CASCADE,
    label_id UUID NOT NULL REFERENCES labels(id) ON DELETE CASCADE,
    PRIMARY KEY (issue_id, label_id)
);

CREATE TABLE issue_relations (
    id         UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    from_issue UUID NOT NULL REFERENCES issues(id) ON DELETE CASCADE,
    to_issue   UUID NOT NULL REFERENCES issues(id) ON DELETE CASCADE,
    kind       relation_kind NOT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    UNIQUE (from_issue, to_issue, kind),
    CONSTRAINT no_self_relation CHECK (from_issue <> to_issue)
);

CREATE TABLE issue_watchers (
    issue_id   UUID NOT NULL REFERENCES issues(id) ON DELETE CASCADE,
    user_id    UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    PRIMARY KEY (issue_id, user_id)
);

-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
DROP TABLE IF EXISTS issue_watchers;
DROP TABLE IF EXISTS issue_relations;
DROP TABLE IF EXISTS issue_labels;
DROP TABLE IF EXISTS issue_components;
DROP TABLE IF EXISTS issues;
-- +goose StatementEnd
