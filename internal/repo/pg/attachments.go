package pg

import (
	"context"
	"errors"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/omni/bugtracker/internal/domain"
	"github.com/omni/bugtracker/internal/service"
)

// ── attachments (metadata; bytes live on local disk) ──

const selectAttachment = `
	SELECT a.id, a.issue_id, a.filename, a.content_type, a.size_bytes, a.upload_object_key,
	       a.created_at, u.id, u.display_name, u.email, p.key
	FROM attachments a
	LEFT JOIN users u ON u.id = a.uploader_id
	JOIN issues i ON i.id = a.issue_id
	JOIN projects p ON p.id = i.project_id`

func scanAttachment(row scanner) (domain.Attachment, error) {
	var a domain.Attachment
	var uploaderID *uuid.UUID
	var uploaderName, uploaderEmail *string
	err := row.Scan(&a.ID, &a.IssueID, &a.Filename, &a.ContentType, &a.SizeBytes, &a.ObjectKey,
		&a.CreatedAt, &uploaderID, &uploaderName, &uploaderEmail, &a.ProjectKey)
	if err != nil {
		return domain.Attachment{}, err
	}
	if uploaderID != nil {
		a.Uploader = &domain.User{ID: *uploaderID, DisplayName: deref(uploaderName), Email: deref(uploaderEmail)}
	}
	return a, nil
}

// CreateAttachment inserts the metadata row and records the timeline entry in
// one transaction. The caller has already persisted the bytes.
func (s *Store) CreateAttachment(ctx context.Context, in service.CreateAttachmentInput) (domain.Attachment, error) {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return domain.Attachment{}, err
	}
	defer tx.Rollback(ctx) //nolint:errcheck

	const q = `
		INSERT INTO attachments (issue_id, uploader_id, filename, content_type, size_bytes, upload_object_key, checksum)
		VALUES ($1, $2, $3, $4, $5, $6, $7)
		RETURNING id`
	var id uuid.UUID
	if err := tx.QueryRow(ctx, q, in.IssueID, in.UploaderID, in.Filename, in.ContentType,
		in.SizeBytes, in.ObjectKey, in.Checksum).Scan(&id); err != nil {
		return domain.Attachment{}, err
	}
	if err := recordActivity(ctx, tx, in.IssueID, in.UploaderID, "attachment.added", "attachment", id); err != nil {
		return domain.Attachment{}, err
	}
	if err := tx.Commit(ctx); err != nil {
		return domain.Attachment{}, err
	}
	return scanAttachment(s.pool.QueryRow(ctx, selectAttachment+` WHERE a.id = $1`, id))
}

func (s *Store) ListAttachmentsForIssue(ctx context.Context, issueID uuid.UUID) ([]domain.Attachment, error) {
	rows, err := s.pool.Query(ctx, selectAttachment+` WHERE a.issue_id = $1 ORDER BY a.created_at`, issueID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []domain.Attachment
	for rows.Next() {
		a, err := scanAttachment(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, a)
	}
	return out, rows.Err()
}

func (s *Store) GetAttachment(ctx context.Context, id uuid.UUID) (domain.Attachment, error) {
	return scanAttachment(s.pool.QueryRow(ctx, selectAttachment+` WHERE a.id = $1`, id))
}

// DeleteAttachment removes the metadata row and reports the object key so the
// caller can unlink the bytes.
func (s *Store) DeleteAttachment(ctx context.Context, id uuid.UUID) (string, bool, error) {
	var key string
	err := s.pool.QueryRow(ctx,
		`DELETE FROM attachments WHERE id = $1 RETURNING upload_object_key`, id).Scan(&key)
	if errors.Is(err, pgx.ErrNoRows) {
		return "", false, nil
	}
	if err != nil {
		return "", false, err
	}
	return key, true, nil
}
