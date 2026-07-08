-- name: CreateIssue :one
INSERT INTO issues (
    project_id, number, type, title, description_md, status, severity, priority,
    reporter_id, assignee_id, milestone_id, version_affected,
    repro_steps_md, expected_md, actual_md, environment_md, fields, source, dedupe_key
) VALUES (
    @project_id, @number, @type, @title, @description_md, @status, @severity, @priority,
    @reporter_id, @assignee_id, @milestone_id, @version_affected,
    @repro_steps_md, @expected_md, @actual_md, @environment_md, @fields, @source, sqlc.narg('dedupe_key')
)
RETURNING *;

-- name: GetIssueByNumber :one
SELECT i.* FROM issues i
JOIN projects p ON p.id = i.project_id
WHERE p.key = @project_key AND i.number = @number AND i.deleted_at IS NULL;

-- name: GetIssue :one
SELECT * FROM issues WHERE id = @id AND deleted_at IS NULL;

-- name: GetIssueByDedupe :one
SELECT * FROM issues
WHERE project_id = @project_id AND dedupe_key = @dedupe_key AND deleted_at IS NULL;

-- name: ListIssues :many
-- Baseline list; the service layer composes richer WHEREs from the filter grammar.
SELECT * FROM issues
WHERE deleted_at IS NULL
  AND (sqlc.narg('project_id')::uuid IS NULL OR project_id = sqlc.narg('project_id'))
  AND (sqlc.narg('status')::issue_status IS NULL OR status = sqlc.narg('status'))
  AND (sqlc.narg('assignee_id')::uuid IS NULL OR assignee_id = sqlc.narg('assignee_id'))
  AND (sqlc.narg('type')::issue_type IS NULL OR type = sqlc.narg('type'))
  AND (sqlc.narg('severity')::severity IS NULL OR severity = sqlc.narg('severity'))
ORDER BY created_at DESC
LIMIT @lim OFFSET @off;

-- name: SearchIssues :many
SELECT *, ts_rank(fts, websearch_to_tsquery('english', @q)) AS rank
FROM issues
WHERE deleted_at IS NULL AND fts @@ websearch_to_tsquery('english', @q)
ORDER BY rank DESC
LIMIT @lim OFFSET @off;

-- name: UpdateIssue :one
UPDATE issues
SET title            = COALESCE(sqlc.narg('title'), title),
    description_md    = COALESCE(sqlc.narg('description_md'), description_md),
    status            = COALESCE(sqlc.narg('status'), status),
    severity          = COALESCE(sqlc.narg('severity'), severity),
    priority          = COALESCE(sqlc.narg('priority'), priority),
    assignee_id       = COALESCE(sqlc.narg('assignee_id'), assignee_id),
    milestone_id      = COALESCE(sqlc.narg('milestone_id'), milestone_id),
    release_id        = COALESCE(sqlc.narg('release_id'), release_id),
    version_fixed     = COALESCE(sqlc.narg('version_fixed'), version_fixed),
    git_commit_sha    = COALESCE(sqlc.narg('git_commit_sha'), git_commit_sha),
    pull_request_url  = COALESCE(sqlc.narg('pull_request_url'), pull_request_url),
    resolved_at       = CASE WHEN sqlc.narg('status') IN ('resolved','closed') AND resolved_at IS NULL
                             THEN now() ELSE resolved_at END,
    closed_at         = CASE WHEN sqlc.narg('status') = 'closed' THEN now() ELSE closed_at END,
    updated_at        = now()
WHERE id = @id AND deleted_at IS NULL
RETURNING *;

-- name: SoftDeleteIssue :exec
UPDATE issues SET deleted_at = now() WHERE id = @id;

-- name: AddIssueLabel :exec
INSERT INTO issue_labels (issue_id, label_id) VALUES (@issue_id, @label_id)
ON CONFLICT DO NOTHING;

-- name: RemoveIssueLabel :exec
DELETE FROM issue_labels WHERE issue_id = @issue_id AND label_id = @label_id;

-- name: AddIssueComponent :exec
INSERT INTO issue_components (issue_id, component_id) VALUES (@issue_id, @component_id)
ON CONFLICT DO NOTHING;

-- name: LinkIssues :exec
INSERT INTO issue_relations (from_issue, to_issue, kind) VALUES (@from_issue, @to_issue, @kind)
ON CONFLICT DO NOTHING;

-- name: ListIssueLabels :many
SELECT l.* FROM labels l JOIN issue_labels il ON il.label_id = l.id WHERE il.issue_id = @issue_id;

-- name: AddWatcher :exec
INSERT INTO issue_watchers (issue_id, user_id) VALUES (@issue_id, @user_id) ON CONFLICT DO NOTHING;

-- name: ListWatchers :many
SELECT user_id FROM issue_watchers WHERE issue_id = @issue_id;
