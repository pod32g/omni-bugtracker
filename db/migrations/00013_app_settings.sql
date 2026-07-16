-- +goose Up
-- +goose StatementBegin

-- Generic runtime settings (behavioral config that admins change without a redeploy).
-- One row per setting key; the value is JSONB so a setting can carry structured data.
CREATE TABLE app_settings (
    key        TEXT PRIMARY KEY,
    value      JSONB NOT NULL DEFAULT '{}',
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
DROP TABLE IF EXISTS app_settings;
-- +goose StatementEnd
