-- +goose Up
-- +goose StatementBegin

-- Runtime-editable integration credentials/config (secret is a reference, never plaintext).
CREATE TABLE integration_configs (
    id         UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    kind       TEXT NOT NULL,                  -- logging|metrics|notify|search|upload|git_provider
    name       TEXT NOT NULL DEFAULT '',
    config     JSONB NOT NULL DEFAULT '{}',
    secret_ref TEXT NOT NULL DEFAULT '',       -- pointer into secret store / env, not the secret
    is_active  BOOLEAN NOT NULL DEFAULT TRUE,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    UNIQUE (kind, name)
);

CREATE TABLE saved_searches (
    id         UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    user_id    UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    name       TEXT NOT NULL,
    query      TEXT NOT NULL,                  -- GitHub-style filter string
    is_shared  BOOLEAN NOT NULL DEFAULT FALSE,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    UNIQUE (user_id, name)
);

-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
DROP TABLE IF EXISTS saved_searches;
DROP TABLE IF EXISTS integration_configs;
-- +goose StatementEnd
