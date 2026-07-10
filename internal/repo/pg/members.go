package pg

import (
	"context"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/omni/bugtracker/internal/domain"
)

// ── project membership ──

func (s *Store) ListProjectMembers(ctx context.Context, projectKey string) ([]domain.ProjectMember, error) {
	const q = `
		SELECT u.id, u.email, u.display_name, u.avatar_url, pm.role, pm.created_at
		FROM project_members pm
		JOIN projects p ON p.id = pm.project_id
		JOIN users u ON u.id = pm.user_id
		WHERE p.key = $1
		ORDER BY pm.created_at`
	rows, err := s.pool.Query(ctx, q, projectKey)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []domain.ProjectMember
	for rows.Next() {
		var m domain.ProjectMember
		if err := rows.Scan(&m.User.ID, &m.User.Email, &m.User.DisplayName, &m.User.AvatarURL,
			&m.Role, &m.CreatedAt); err != nil {
			return nil, err
		}
		out = append(out, m)
	}
	return out, rows.Err()
}

// GetProjectRole returns the user's project role, with found=false when the
// user is not a member.
func (s *Store) GetProjectRole(ctx context.Context, projectKey string, userID uuid.UUID) (domain.Role, bool, error) {
	const q = `
		SELECT pm.role FROM project_members pm
		JOIN projects p ON p.id = pm.project_id
		WHERE p.key = $1 AND pm.user_id = $2`
	var role domain.Role
	err := s.pool.QueryRow(ctx, q, projectKey, userID).Scan(&role)
	if err == pgx.ErrNoRows {
		return "", false, nil
	}
	if err != nil {
		return "", false, err
	}
	return role, true, nil
}

func (s *Store) UpsertProjectMember(ctx context.Context, projectKey string, userID uuid.UUID, role domain.Role) (domain.ProjectMember, error) {
	const q = `
		INSERT INTO project_members (project_id, user_id, role)
		SELECT p.id, $2, $3::app_role FROM projects p WHERE p.key = $1
		ON CONFLICT (project_id, user_id) DO UPDATE SET role = EXCLUDED.role
		RETURNING user_id, role, created_at`
	var m domain.ProjectMember
	if err := s.pool.QueryRow(ctx, q, projectKey, userID, string(role)).
		Scan(&m.User.ID, &m.Role, &m.CreatedAt); err != nil {
		return domain.ProjectMember{}, err
	}
	// Hydrate the user profile for the response.
	const uq = `SELECT email, display_name, avatar_url FROM users WHERE id = $1`
	if err := s.pool.QueryRow(ctx, uq, m.User.ID).
		Scan(&m.User.Email, &m.User.DisplayName, &m.User.AvatarURL); err != nil {
		return domain.ProjectMember{}, err
	}
	return m, nil
}

func (s *Store) RemoveProjectMember(ctx context.Context, projectKey string, userID uuid.UUID) (bool, error) {
	tag, err := s.pool.Exec(ctx,
		`DELETE FROM project_members pm USING projects p
		 WHERE p.id = pm.project_id AND p.key = $1 AND pm.user_id = $2`, projectKey, userID)
	if err != nil {
		return false, err
	}
	return tag.RowsAffected() > 0, nil
}
