-- name: UpsertCommit :one
INSERT INTO git_commits (repo, sha, author, message, url, committed_at)
VALUES (@repo, @sha, @author, @message, @url, @committed_at)
ON CONFLICT (repo, sha) DO UPDATE SET message = EXCLUDED.message
RETURNING *;

-- name: LinkCommitToIssue :exec
INSERT INTO issue_commits (issue_id, commit_id, verb) VALUES (@issue_id, @commit_id, @verb)
ON CONFLICT (issue_id, commit_id) DO UPDATE SET verb = EXCLUDED.verb;

-- name: UpsertPullRequest :one
INSERT INTO pull_requests (repo, number, url, title, state, merged_at)
VALUES (@repo, @number, @url, @title, @state, @merged_at)
ON CONFLICT (repo, number) DO UPDATE
    SET state = EXCLUDED.state, title = EXCLUDED.title,
        merged_at = EXCLUDED.merged_at, updated_at = now()
RETURNING *;

-- name: LinkPullRequestToIssue :exec
INSERT INTO issue_pull_requests (issue_id, pr_id, verb) VALUES (@issue_id, @pr_id, @verb)
ON CONFLICT (issue_id, pr_id) DO UPDATE SET verb = EXCLUDED.verb;

-- name: ListCommitsForIssue :many
SELECT c.*, ic.verb FROM git_commits c
JOIN issue_commits ic ON ic.commit_id = c.id
WHERE ic.issue_id = @issue_id
ORDER BY c.committed_at DESC;

-- name: ListIssuesForRelease :many
SELECT * FROM issues
WHERE release_id = @release_id OR version_fixed = @version
ORDER BY type, priority;
