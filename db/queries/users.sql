-- name: UpsertUserByIdentity :one
-- Lazily mirror an Omni-Identity subject on authenticated request.
INSERT INTO users (identity_sub, email, display_name, avatar_url)
VALUES (@identity_sub, @email, @display_name, @avatar_url)
ON CONFLICT (identity_sub) DO UPDATE
    SET email        = EXCLUDED.email,
        display_name = EXCLUDED.display_name,
        avatar_url   = EXCLUDED.avatar_url,
        last_seen_at = now(),
        updated_at   = now()
RETURNING *;

-- name: GetUser :one
SELECT * FROM users WHERE id = @id;

-- name: GetUserByIdentity :one
SELECT * FROM users WHERE identity_sub = @identity_sub;

-- name: ListUsers :many
SELECT * FROM users
WHERE is_active = TRUE
ORDER BY display_name
LIMIT @lim OFFSET @off;

-- name: CreateAPIToken :one
INSERT INTO api_tokens (user_id, name, token_hash, scopes, expires_at)
VALUES (@user_id, @name, @token_hash, @scopes, @expires_at)
RETURNING *;

-- name: GetAPITokenByHash :one
SELECT * FROM api_tokens
WHERE token_hash = @token_hash
  AND revoked_at IS NULL
  AND (expires_at IS NULL OR expires_at > now());

-- name: TouchAPIToken :exec
UPDATE api_tokens SET last_used_at = now() WHERE id = @id;

-- name: RevokeAPIToken :exec
UPDATE api_tokens SET revoked_at = now() WHERE id = @id AND user_id = @user_id;
