-- +goose Up
-- +goose StatementBegin

-- Users can edit their own display name and avatar. Until now both were mirrored
-- from the Omni-Identity id_token on every login, so a locally-set name would be
-- silently reverted the next time the user signed in. profile_overridden records
-- "this person has said what they want to be called" and makes the OIDC enrichment
-- stop writing over it — the email and the identity subject stay IdP-owned.
ALTER TABLE users ADD COLUMN profile_overridden BOOLEAN NOT NULL DEFAULT FALSE;

-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
ALTER TABLE users DROP COLUMN IF EXISTS profile_overridden;
-- +goose StatementEnd
