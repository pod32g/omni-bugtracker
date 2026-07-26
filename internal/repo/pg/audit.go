package pg

import (
	"context"

	"github.com/omni/bugtracker/internal/domain"
	"github.com/omni/bugtracker/internal/service"
)

// RecordAudit appends one entry. Never returns a domain error to the caller's critical
// path — see the service-layer wrapper, which logs and continues: failing a role change
// because the audit insert failed would be the wrong trade.
func (s *Store) RecordAudit(ctx context.Context, in service.AuditEntry) error {
	_, err := s.pool.Exec(ctx,
		`INSERT INTO audit_log
		   (actor_id, actor_email, action, target_type, target_id, target_label,
		    details, ip, user_agent, via_token, token_id)
		 VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11)`,
		in.ActorID, in.ActorEmail, in.Action, in.TargetType, in.TargetID, in.TargetLabel,
		in.Details, in.IP, in.UserAgent, in.ViaToken, in.TokenID)
	return err
}

// ListAudit returns a filtered page of the log, newest first, plus the unpaged total.
func (s *Store) ListAudit(ctx context.Context, f service.AuditFilter) ([]domain.AuditEntry, int, error) {
	// Every predicate is optional and expressed as "unset OR matches", so one query
	// shape serves every combination without string building.
	const where = `
		WHERE ($1 = '' OR a.action = $1)
		  AND ($2 = '' OR a.target_type = $2)
		  AND ($3::uuid IS NULL OR a.actor_id = $3::uuid)
		  AND ($4::timestamptz IS NULL OR a.created_at >= $4::timestamptz)
		  AND ($5::timestamptz IS NULL OR a.created_at <= $5::timestamptz)`

	args := []any{f.Action, f.TargetType, f.ActorID, f.Since, f.Until}

	var total int
	if err := s.pool.QueryRow(ctx,
		`SELECT count(*) FROM audit_log a`+where, args...).Scan(&total); err != nil {
		return nil, 0, err
	}

	rows, err := s.pool.Query(ctx,
		`SELECT a.id, a.actor_email, u.display_name, a.action, a.target_type, a.target_id,
		        a.target_label, a.details, a.ip, a.via_token, a.created_at
		   FROM audit_log a LEFT JOIN users u ON u.id = a.actor_id`+where+`
		  ORDER BY a.created_at DESC LIMIT $6 OFFSET $7`,
		append(args, clampLimit(f.Limit), clampOffset(f.Offset))...)
	if err != nil {
		return nil, 0, err
	}
	defer rows.Close()

	var out []domain.AuditEntry
	for rows.Next() {
		var e domain.AuditEntry
		var displayName *string
		if err := rows.Scan(&e.ID, &e.ActorEmail, &displayName, &e.Action, &e.TargetType,
			&e.TargetID, &e.TargetLabel, &e.Details, &e.IP, &e.ViaToken, &e.CreatedAt); err != nil {
			return nil, 0, err
		}
		e.ActorName = deref(displayName)
		out = append(out, e)
	}
	return out, total, rows.Err()
}
