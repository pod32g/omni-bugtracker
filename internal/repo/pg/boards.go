package pg

import (
	"context"
	"errors"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/omni/bugtracker/internal/domain"
	"github.com/omni/bugtracker/internal/service"
)

// ── configurable boards ──

// defaultColumns mirrors the workflow the hardcoded board used to show.
var defaultColumns = []struct {
	name     string
	statuses []string
}{
	{"Backlog", []string{"open", "reopened"}},
	{"In Progress", []string{"in_progress"}},
	{"Blocked", []string{"blocked"}},
	{"In Review", []string{"ready_for_review"}},
	{"Done", []string{"resolved", "closed"}},
}

func (s *Store) loadBoard(ctx context.Context, id uuid.UUID) (domain.Board, error) {
	var b domain.Board
	err := s.pool.QueryRow(ctx, `
		SELECT b.id, p.key, b.name, b.swimlane, b.created_at
		FROM boards b JOIN projects p ON p.id = b.project_id
		WHERE b.id = $1`, id).
		Scan(&b.ID, &b.ProjectKey, &b.Name, &b.Swimlane, &b.CreatedAt)
	if err != nil {
		return domain.Board{}, err
	}
	rows, err := s.pool.Query(ctx, `
		SELECT id, name, statuses, wip_limit, position
		FROM board_columns WHERE board_id = $1 ORDER BY position, created_at`, id)
	if err != nil {
		return domain.Board{}, err
	}
	defer rows.Close()
	for rows.Next() {
		var c domain.BoardColumn
		if err := rows.Scan(&c.ID, &c.Name, &c.Statuses, &c.WipLimit, &c.Position); err != nil {
			return domain.Board{}, err
		}
		b.Columns = append(b.Columns, c)
	}
	return b, rows.Err()
}

// GetOrCreateBoard returns the project's board, seeding the default workflow
// columns on first access.
func (s *Store) GetOrCreateBoard(ctx context.Context, projectKey string) (domain.Board, error) {
	var id uuid.UUID
	err := s.pool.QueryRow(ctx, `
		SELECT b.id FROM boards b JOIN projects p ON p.id = b.project_id
		WHERE p.key = $1 ORDER BY b.created_at LIMIT 1`, projectKey).Scan(&id)
	if errors.Is(err, pgx.ErrNoRows) {
		tx, err := s.pool.Begin(ctx)
		if err != nil {
			return domain.Board{}, err
		}
		defer tx.Rollback(ctx) //nolint:errcheck
		if err := tx.QueryRow(ctx, `
			INSERT INTO boards (project_id) SELECT id FROM projects WHERE key = $1
			RETURNING id`, projectKey).Scan(&id); err != nil {
			return domain.Board{}, err
		}
		for i, col := range defaultColumns {
			if _, err := tx.Exec(ctx, `
				INSERT INTO board_columns (board_id, name, statuses, position)
				VALUES ($1, $2, $3, $4)`, id, col.name, col.statuses, i); err != nil {
				return domain.Board{}, err
			}
		}
		if err := tx.Commit(ctx); err != nil {
			return domain.Board{}, err
		}
	} else if err != nil {
		return domain.Board{}, err
	}
	return s.loadBoard(ctx, id)
}

func (s *Store) UpdateBoard(ctx context.Context, id uuid.UUID, name, swimlane *string) (domain.Board, error) {
	if _, err := s.pool.Exec(ctx, `
		UPDATE boards SET name = COALESCE($2, name), swimlane = COALESCE($3, swimlane)
		WHERE id = $1`, id, name, swimlane); err != nil {
		return domain.Board{}, err
	}
	return s.loadBoard(ctx, id)
}

func (s *Store) CreateBoardColumn(ctx context.Context, boardID uuid.UUID, in service.BoardColumnInput) (domain.Board, error) {
	if _, err := s.pool.Exec(ctx, `
		INSERT INTO board_columns (board_id, name, statuses, wip_limit, position)
		VALUES ($1, $2, $3, $4,
		        (SELECT COALESCE(MAX(position), -1) + 1 FROM board_columns WHERE board_id = $1))`,
		boardID, in.Name, in.Statuses, in.WipLimit); err != nil {
		return domain.Board{}, err
	}
	return s.loadBoard(ctx, boardID)
}

func (s *Store) UpdateBoardColumn(ctx context.Context, columnID uuid.UUID, in service.UpdateBoardColumnInput) (domain.Board, error) {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return domain.Board{}, err
	}
	defer tx.Rollback(ctx) //nolint:errcheck

	var boardID uuid.UUID
	var curPos int
	if err := tx.QueryRow(ctx,
		`SELECT board_id, position FROM board_columns WHERE id = $1 FOR UPDATE`, columnID).
		Scan(&boardID, &curPos); err != nil {
		return domain.Board{}, err
	}
	if _, err := tx.Exec(ctx, `
		UPDATE board_columns SET
		  name      = COALESCE($2, name),
		  statuses  = COALESCE($3, statuses),
		  wip_limit = CASE WHEN $5 THEN NULL ELSE COALESCE($4, wip_limit) END
		WHERE id = $1`, columnID, in.Name, in.Statuses, in.WipLimit, in.ClearWip); err != nil {
		return domain.Board{}, err
	}
	// Reorder by swapping with whatever occupies the target position.
	if in.Position != nil && *in.Position != curPos {
		if _, err := tx.Exec(ctx, `
			UPDATE board_columns SET position = $3
			WHERE board_id = $1 AND position = $2 AND id <> $4`,
			boardID, *in.Position, curPos, columnID); err != nil {
			return domain.Board{}, err
		}
		if _, err := tx.Exec(ctx,
			`UPDATE board_columns SET position = $2 WHERE id = $1`, columnID, *in.Position); err != nil {
			return domain.Board{}, err
		}
	}
	if err := tx.Commit(ctx); err != nil {
		return domain.Board{}, err
	}
	return s.loadBoard(ctx, boardID)
}

func (s *Store) DeleteBoardColumn(ctx context.Context, columnID uuid.UUID) (domain.Board, bool, error) {
	var boardID uuid.UUID
	err := s.pool.QueryRow(ctx,
		`DELETE FROM board_columns WHERE id = $1 RETURNING board_id`, columnID).Scan(&boardID)
	if errors.Is(err, pgx.ErrNoRows) {
		return domain.Board{}, false, nil
	}
	if err != nil {
		return domain.Board{}, false, err
	}
	b, err := s.loadBoard(ctx, boardID)
	return b, true, err
}
