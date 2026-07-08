-- name: CreateComment :one
INSERT INTO comments (issue_id, author_id, body_md)
VALUES (@issue_id, @author_id, @body_md)
RETURNING *;

-- name: ListComments :many
SELECT * FROM comments
WHERE issue_id = @issue_id AND deleted_at IS NULL
ORDER BY created_at
LIMIT @lim OFFSET @off;

-- name: UpdateComment :one
UPDATE comments SET body_md = @body_md, edited_at = now()
WHERE id = @id AND author_id = @author_id AND deleted_at IS NULL
RETURNING *;

-- name: SoftDeleteComment :exec
UPDATE comments SET deleted_at = now() WHERE id = @id;

-- name: CreateAttachment :one
INSERT INTO attachments (issue_id, comment_id, uploader_id, filename, content_type, size_bytes, upload_object_key, checksum)
VALUES (@issue_id, @comment_id, @uploader_id, @filename, @content_type, @size_bytes, @upload_object_key, @checksum)
RETURNING *;

-- name: ListAttachmentsForIssue :many
SELECT * FROM attachments WHERE issue_id = @issue_id ORDER BY created_at;
