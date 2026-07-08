-- name: RecordActivity :one
INSERT INTO activity (issue_id, actor_id, verb, entity_type, entity_id, changes)
VALUES (@issue_id, @actor_id, @verb, @entity_type, @entity_id, @changes)
RETURNING *;

-- name: ListIssueActivity :many
SELECT * FROM activity
WHERE issue_id = @issue_id
ORDER BY occurred_at DESC
LIMIT @lim OFFSET @off;

-- name: ListRecentActivity :many
SELECT * FROM activity
ORDER BY occurred_at DESC
LIMIT @lim OFFSET @off;
