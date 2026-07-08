-- +goose Up
-- +goose StatementBegin
CREATE TYPE issue_type    AS ENUM ('bug', 'task', 'feature', 'improvement');
CREATE TYPE issue_status  AS ENUM ('open', 'in_progress', 'blocked', 'ready_for_review', 'resolved', 'closed', 'reopened');
CREATE TYPE severity      AS ENUM ('critical', 'high', 'medium', 'low');
CREATE TYPE priority      AS ENUM ('p0', 'p1', 'p2', 'p3');
CREATE TYPE issue_source  AS ENUM ('human', 'logging', 'metrics', 'api', 'automation', 'git');
CREATE TYPE relation_kind AS ENUM ('blocks', 'blocked_by', 'duplicates', 'relates', 'caused_by');
CREATE TYPE app_role      AS ENUM ('owner', 'admin', 'maintainer', 'member', 'reporter', 'bot');
CREATE TYPE release_state AS ENUM ('draft', 'published');
CREATE TYPE milestone_state AS ENUM ('open', 'closed');
CREATE TYPE ref_verb      AS ENUM ('fixes', 'closes', 'resolves', 'refs', 'related');
CREATE TYPE delivery_status AS ENUM ('pending', 'success', 'failed', 'dead');

-- Extensions used across the schema.
CREATE EXTENSION IF NOT EXISTS pg_trgm;   -- fuzzy label/title matching
-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
DROP TYPE IF EXISTS delivery_status;
DROP TYPE IF EXISTS ref_verb;
DROP TYPE IF EXISTS milestone_state;
DROP TYPE IF EXISTS release_state;
DROP TYPE IF EXISTS app_role;
DROP TYPE IF EXISTS relation_kind;
DROP TYPE IF EXISTS issue_source;
DROP TYPE IF EXISTS priority;
DROP TYPE IF EXISTS severity;
DROP TYPE IF EXISTS issue_status;
DROP TYPE IF EXISTS issue_type;
-- +goose StatementEnd
