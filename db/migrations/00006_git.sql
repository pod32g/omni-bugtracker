-- +goose Up
-- +goose StatementBegin

CREATE TABLE git_commits (
    id           UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    repo         TEXT NOT NULL,
    sha          TEXT NOT NULL,
    author       TEXT NOT NULL DEFAULT '',
    message      TEXT NOT NULL DEFAULT '',
    url          TEXT NOT NULL DEFAULT '',
    committed_at TIMESTAMPTZ,
    created_at   TIMESTAMPTZ NOT NULL DEFAULT now(),
    UNIQUE (repo, sha)
);

CREATE TABLE issue_commits (
    issue_id  UUID NOT NULL REFERENCES issues(id) ON DELETE CASCADE,
    commit_id UUID NOT NULL REFERENCES git_commits(id) ON DELETE CASCADE,
    verb      ref_verb NOT NULL DEFAULT 'refs',
    PRIMARY KEY (issue_id, commit_id)
);

CREATE TABLE pull_requests (
    id         UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    repo       TEXT NOT NULL,
    number     INTEGER NOT NULL,
    url        TEXT NOT NULL DEFAULT '',
    title      TEXT NOT NULL DEFAULT '',
    state      TEXT NOT NULL DEFAULT 'open',   -- open|merged|closed
    merged_at  TIMESTAMPTZ,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    UNIQUE (repo, number)
);

CREATE TABLE issue_pull_requests (
    issue_id UUID NOT NULL REFERENCES issues(id) ON DELETE CASCADE,
    pr_id    UUID NOT NULL REFERENCES pull_requests(id) ON DELETE CASCADE,
    verb     ref_verb NOT NULL DEFAULT 'refs',
    PRIMARY KEY (issue_id, pr_id)
);

-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
DROP TABLE IF EXISTS issue_pull_requests;
DROP TABLE IF EXISTS pull_requests;
DROP TABLE IF EXISTS issue_commits;
DROP TABLE IF EXISTS git_commits;
-- +goose StatementEnd
