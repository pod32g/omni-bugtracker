-- +goose Up
-- +goose StatementBegin

-- Due dates and SLAs. Severity has so far been an adjective with no consequences:
-- nothing in the schema said what "critical" obliges anyone to do, or by when.

-- due_at is the deliberate, per-issue promise. first_response_at is a stamp rather
-- than a derivation: a response time inferred from the comment table changes meaning
-- whenever a comment is edited or deleted. resolved_at already exists and is already
-- stamped on transition, so the SLA reads it instead of adding a second one.
ALTER TABLE issues ADD COLUMN due_at            TIMESTAMPTZ;
ALTER TABLE issues ADD COLUMN first_response_at TIMESTAMPTZ;

-- Backfill first responses from the comment history. Imperfect — an edited comment
-- carries its original created_at, which is what we want, but a deleted one is gone —
-- and still far better than every historical issue reading as never answered.
UPDATE issues i SET first_response_at = fr.at
  FROM (SELECT c.issue_id, min(c.created_at) AS at
          FROM comments c JOIN issues x ON x.id = c.issue_id
         WHERE c.deleted_at IS NULL AND c.author_id IS DISTINCT FROM x.reporter_id
         GROUP BY c.issue_id) fr
 WHERE fr.issue_id = i.id;

CREATE INDEX idx_issues_due_at ON issues (due_at)
    WHERE due_at IS NOT NULL AND deleted_at IS NULL;

-- A policy is (project, severity?, type?) → two budgets in minutes. NULL means "any",
-- so a project can set one catch-all row and override just critical.
CREATE TABLE sla_policies (
    id                 UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    project_id         UUID NOT NULL REFERENCES projects(id) ON DELETE CASCADE,
    severity           severity,
    issue_type         issue_type,
    response_minutes   INTEGER NOT NULL CHECK (response_minutes   > 0),
    resolution_minutes INTEGER NOT NULL CHECK (resolution_minutes > 0),
    is_active          BOOLEAN NOT NULL DEFAULT TRUE,
    created_at         TIMESTAMPTZ NOT NULL DEFAULT now()
);

-- One policy per (project, severity, type) combination. NULLs are values here rather
-- than unknowns — under a plain UNIQUE a project could hold two conflicting
-- catch-alls, because NULL <> NULL — so each NULL-ness combination gets its own
-- partial index. Normalising with COALESCE(severity::TEXT, '*') would have been
-- shorter and is not allowed: the enum-to-text cast is only STABLE, not IMMUTABLE.
CREATE UNIQUE INDEX idx_sla_policies_sev_type ON sla_policies (project_id, severity, issue_type)
    WHERE severity IS NOT NULL AND issue_type IS NOT NULL;
CREATE UNIQUE INDEX idx_sla_policies_sev ON sla_policies (project_id, severity)
    WHERE severity IS NOT NULL AND issue_type IS NULL;
CREATE UNIQUE INDEX idx_sla_policies_type ON sla_policies (project_id, issue_type)
    WHERE severity IS NULL AND issue_type IS NOT NULL;
CREATE UNIQUE INDEX idx_sla_policies_any ON sla_policies (project_id)
    WHERE severity IS NULL AND issue_type IS NULL;

-- Exactly-once escalation. A warning that re-fires every fifteen minutes trains people
-- to ignore it, so each (issue, kind) may be emitted once; the primary key is what
-- makes the claim atomic rather than a read-then-write race between worker runs.
CREATE TABLE issue_sla_events (
    issue_id   UUID NOT NULL REFERENCES issues(id) ON DELETE CASCADE,
    kind       TEXT NOT NULL,
    emitted_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    PRIMARY KEY (issue_id, kind)
);

-- issue_sla resolves each issue to its most specific active policy and derives the
-- deadlines and state from it. A view rather than columns: the answer depends on
-- now(), and a stored copy would be stale between worker runs — the list, the filter
-- grammar and the escalation job would each disagree about what is late.
CREATE VIEW issue_sla AS
SELECT
    i.id AS issue_id,
    pol.id AS policy_id,
    i.created_at + make_interval(secs => pol.response_minutes   * 60) AS response_due,
    i.created_at + make_interval(secs => pol.resolution_minutes * 60) AS resolution_due,
    CASE
        WHEN i.first_response_at IS NOT NULL THEN
            CASE WHEN i.first_response_at
                      <= i.created_at + make_interval(secs => pol.response_minutes * 60)
                 THEN 'met' ELSE 'breached' END
        WHEN now() > i.created_at + make_interval(secs => pol.response_minutes * 60)
            THEN 'breached'
        WHEN now() >= i.created_at + make_interval(secs => pol.response_minutes * 60 * 0.75)
            THEN 'at_risk'
        ELSE 'ok'
    END AS response_state,
    CASE
        -- resolved_at is deliberately not cleared on reopen (the reports series
        -- depends on it surviving), so the SLA only counts it while the issue is
        -- actually finished — a reopened issue is back on the clock.
        WHEN i.status IN ('resolved', 'closed') AND i.resolved_at IS NOT NULL THEN
            CASE WHEN i.resolved_at
                      <= i.created_at + make_interval(secs => pol.resolution_minutes * 60)
                 THEN 'met' ELSE 'breached' END
        WHEN now() > i.created_at + make_interval(secs => pol.resolution_minutes * 60)
            THEN 'breached'
        WHEN now() >= i.created_at + make_interval(secs => pol.resolution_minutes * 60 * 0.75)
            THEN 'at_risk'
        ELSE 'ok'
    END AS resolution_state
FROM issues i
JOIN LATERAL (
    SELECT s.*
      FROM sla_policies s
     WHERE s.project_id = i.project_id
       AND s.is_active
       AND (s.severity   IS NULL OR s.severity   = i.severity)
       AND (s.issue_type IS NULL OR s.issue_type = i.type)
     -- Most specific wins: an exact severity+type row beats a severity-only row,
     -- which beats the project catch-all.
     ORDER BY (s.severity IS NOT NULL)::INT + (s.issue_type IS NOT NULL)::INT DESC,
              (s.severity IS NOT NULL)::INT DESC
     LIMIT 1
) pol ON TRUE
WHERE i.deleted_at IS NULL;

-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
DROP VIEW IF EXISTS issue_sla;
DROP TABLE IF EXISTS issue_sla_events;
DROP TABLE IF EXISTS sla_policies;
DROP INDEX IF EXISTS idx_issues_due_at;
ALTER TABLE issues DROP COLUMN IF EXISTS first_response_at;
ALTER TABLE issues DROP COLUMN IF EXISTS due_at;
-- +goose StatementEnd
