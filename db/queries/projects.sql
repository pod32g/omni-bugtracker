-- name: CreateProject :one
INSERT INTO projects (key, name, description_md, default_assignee_id)
VALUES (@key, @name, @description_md, @default_assignee_id)
RETURNING *;

-- name: GetProjectByKey :one
SELECT * FROM projects WHERE key = @key;

-- name: GetProject :one
SELECT * FROM projects WHERE id = @id;

-- name: ListProjects :many
SELECT * FROM projects
WHERE (@include_archived::bool OR is_archived = FALSE)
ORDER BY key
LIMIT @lim OFFSET @off;

-- name: UpdateProject :one
UPDATE projects
SET name                = COALESCE(sqlc.narg('name'), name),
    description_md       = COALESCE(sqlc.narg('description_md'), description_md),
    default_assignee_id  = COALESCE(sqlc.narg('default_assignee_id'), default_assignee_id),
    is_archived          = COALESCE(sqlc.narg('is_archived'), is_archived),
    updated_at           = now()
WHERE id = @id
RETURNING *;

-- name: AllocateIssueNumber :one
-- Atomically reserve the next per-project issue number. Run inside the issue-create tx.
UPDATE projects
SET next_issue_number = next_issue_number + 1,
    updated_at        = now()
WHERE id = @id
RETURNING next_issue_number - 1 AS number;

-- name: CreateComponent :one
INSERT INTO components (project_id, name, description_md, lead_id)
VALUES (@project_id, @name, @description_md, @lead_id)
RETURNING *;

-- name: ListComponents :many
SELECT * FROM components WHERE project_id = @project_id ORDER BY name;

-- name: CreateLabel :one
INSERT INTO labels (project_id, name, color, description)
VALUES (@project_id, @name, @color, @description)
RETURNING *;

-- name: ListLabels :many
SELECT * FROM labels
WHERE project_id = @project_id OR project_id IS NULL
ORDER BY name;

-- name: CreateMilestone :one
INSERT INTO milestones (project_id, title, description_md, due_on)
VALUES (@project_id, @title, @description_md, @due_on)
RETURNING *;

-- name: CreateRelease :one
INSERT INTO releases (project_id, version, name, notes_md, git_tag, created_by)
VALUES (@project_id, @version, @name, @notes_md, @git_tag, @created_by)
RETURNING *;

-- name: PublishRelease :one
UPDATE releases
SET state = 'published', released_at = now(), notes_md = COALESCE(sqlc.narg('notes_md'), notes_md)
WHERE id = @id
RETURNING *;
