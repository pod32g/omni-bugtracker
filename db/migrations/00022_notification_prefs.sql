-- +goose Up
-- +goose StatementBegin

-- Per-user notification routing. Until now there was exactly one rule, hardcoded in the
-- notify worker: recipients are the watchers minus the actor. No way to say "tell me
-- about mentions but not every label change", no per-issue muting, and watchers are
-- added automatically on report, comment and assign — so the volume only went up.
CREATE TABLE notification_prefs (
    user_id    UUID NOT NULL REFERENCES users (id) ON DELETE CASCADE,
    event_type TEXT NOT NULL,
    -- off | inbox | push | both. An absent row means the built-in default for that
    -- event, so a user who has never opened settings behaves sensibly and the table
    -- only ever holds deliberate choices.
    channel    TEXT NOT NULL CHECK (channel IN ('off', 'inbox', 'push', 'both')),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    PRIMARY KEY (user_id, event_type)
);

-- Mute one issue without unwatching it. Unwatching stops the replies you want as well
-- as the ones you do not, which is why people stop watching things entirely.
CREATE TABLE issue_mutes (
    issue_id   UUID NOT NULL REFERENCES issues (id) ON DELETE CASCADE,
    user_id    UUID NOT NULL REFERENCES users (id) ON DELETE CASCADE,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    PRIMARY KEY (issue_id, user_id)
);

-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
DROP TABLE IF EXISTS issue_mutes;
DROP TABLE IF EXISTS notification_prefs;
-- +goose StatementEnd
