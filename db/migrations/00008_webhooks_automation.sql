-- +goose Up
-- +goose StatementBegin

CREATE TABLE webhooks (
    id         UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    project_id UUID REFERENCES projects(id) ON DELETE CASCADE,  -- NULL = all projects
    url        TEXT NOT NULL,
    secret     TEXT NOT NULL DEFAULT '',        -- HMAC signing secret
    events     TEXT[] NOT NULL DEFAULT '{}',    -- subscribed event types
    is_active  BOOLEAN NOT NULL DEFAULT TRUE,
    created_by UUID REFERENCES users(id) ON DELETE SET NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE TABLE webhook_deliveries (
    id            UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    webhook_id    UUID NOT NULL REFERENCES webhooks(id) ON DELETE CASCADE,
    event_type    TEXT NOT NULL,
    payload       JSONB NOT NULL,
    status        delivery_status NOT NULL DEFAULT 'pending',
    response_code INTEGER,
    attempt       INTEGER NOT NULL DEFAULT 0,
    next_retry_at TIMESTAMPTZ,
    created_at    TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at    TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE INDEX idx_webhook_deliveries_webhook ON webhook_deliveries (webhook_id, created_at DESC);
CREATE INDEX idx_webhook_deliveries_retry   ON webhook_deliveries (next_retry_at)
    WHERE status = 'failed';

CREATE TABLE automation_rules (
    id         UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    project_id UUID REFERENCES projects(id) ON DELETE CASCADE,  -- NULL = all projects
    name       TEXT NOT NULL,
    is_active  BOOLEAN NOT NULL DEFAULT TRUE,
    priority   INTEGER NOT NULL DEFAULT 100,    -- lower runs first
    trigger    JSONB NOT NULL,                  -- { event, conditions: <AST> }
    actions    JSONB NOT NULL,                  -- ordered action list
    created_by UUID REFERENCES users(id) ON DELETE SET NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE INDEX idx_automation_rules_active ON automation_rules (is_active, priority);

CREATE TABLE automation_runs (
    id         UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    rule_id    UUID NOT NULL REFERENCES automation_rules(id) ON DELETE CASCADE,
    issue_id   UUID REFERENCES issues(id) ON DELETE SET NULL,
    status     TEXT NOT NULL,                   -- matched|skipped|error
    log        JSONB NOT NULL DEFAULT '{}',
    ran_at     TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE INDEX idx_automation_runs_rule ON automation_runs (rule_id, ran_at DESC);

-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
DROP TABLE IF EXISTS automation_runs;
DROP TABLE IF EXISTS automation_rules;
DROP TABLE IF EXISTS webhook_deliveries;
DROP TABLE IF EXISTS webhooks;
-- +goose StatementEnd
