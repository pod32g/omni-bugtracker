-- +goose Up
-- +goose StatementBegin

-- Project-scoped custom fields. The issue shape is fixed, so every project gets the
-- same form whether it tracks web regressions or wallpaper packaging; the only escape
-- hatch has been labels, which are unvalidated strings that cannot be required,
-- sorted or range-queried.

CREATE TYPE field_type AS ENUM (
    'text', 'number', 'select', 'multi_select', 'date', 'user', 'checkbox', 'url'
);

CREATE TABLE field_definitions (
    id           UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    project_id   UUID NOT NULL REFERENCES projects(id) ON DELETE CASCADE,
    -- key is the stable identifier the filter grammar and API use; label is what
    -- people read. Renaming the label must never break a saved search.
    key          TEXT NOT NULL CHECK (key ~ '^[a-z][a-z0-9_]{0,38}$'),
    label        TEXT NOT NULL,
    type         field_type NOT NULL,
    options      TEXT[] NOT NULL DEFAULT '{}',
    required     BOOLEAN NOT NULL DEFAULT FALSE,
    -- Empty means every type; otherwise the field only appears for these issue types.
    applies_to   issue_type[] NOT NULL DEFAULT '{}',
    help_text    TEXT NOT NULL DEFAULT '',
    position     INTEGER NOT NULL DEFAULT 0,
    created_at   TIMESTAMPTZ NOT NULL DEFAULT now(),
    -- select/multi_select without options is a field nobody can fill in.
    CHECK (type NOT IN ('select', 'multi_select') OR cardinality(options) > 0)
);

CREATE UNIQUE INDEX idx_field_definitions_key ON field_definitions (project_id, key);
CREATE INDEX idx_field_definitions_project ON field_definitions (project_id, position);

-- Typed columns rather than one JSON blob, so filtering and sorting stay indexable and
-- a date is a date. Exactly one is populated per row, decided by the definition's type.
CREATE TABLE issue_field_values (
    issue_id      UUID NOT NULL REFERENCES issues(id) ON DELETE CASCADE,
    definition_id UUID NOT NULL REFERENCES field_definitions(id) ON DELETE CASCADE,
    text_value    TEXT,
    number_value  NUMERIC,
    date_value    DATE,
    user_value    UUID REFERENCES users(id) ON DELETE SET NULL,
    bool_value    BOOLEAN,
    -- multi_select only. A separate column rather than reusing text_value keeps the
    -- single-value predicates from having to know about arrays.
    text_values   TEXT[],
    updated_at    TIMESTAMPTZ NOT NULL DEFAULT now(),
    PRIMARY KEY (issue_id, definition_id)
);

CREATE INDEX idx_field_values_definition ON issue_field_values (definition_id);
-- The filter grammar's equality and range predicates go through these.
CREATE INDEX idx_field_values_text   ON issue_field_values (definition_id, text_value)
    WHERE text_value IS NOT NULL;
CREATE INDEX idx_field_values_number ON issue_field_values (definition_id, number_value)
    WHERE number_value IS NOT NULL;
CREATE INDEX idx_field_values_date   ON issue_field_values (definition_id, date_value)
    WHERE date_value IS NOT NULL;

-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
DROP TABLE IF EXISTS issue_field_values;
DROP TABLE IF EXISTS field_definitions;
DROP TYPE IF EXISTS field_type;
-- +goose StatementEnd
