package pg

import (
	"context"
	"strings"

	"github.com/google/uuid"

	"github.com/omni/bugtracker/internal/domain"
	"github.com/omni/bugtracker/internal/service"
)

// ── webhooks (outbound event subscriptions) ──

const selectWebhook = `
	SELECT w.id, COALESCE(p.key, ''), w.url, w.secret <> '', w.events, w.is_active, w.created_at
	FROM webhooks w
	LEFT JOIN projects p ON p.id = w.project_id`

func scanWebhook(row scanner) (domain.Webhook, error) {
	var wh domain.Webhook
	err := row.Scan(&wh.ID, &wh.ProjectKey, &wh.URL, &wh.HasSecret, &wh.Events, &wh.IsActive, &wh.CreatedAt)
	return wh, err
}

func (s *Store) ListWebhooks(ctx context.Context) ([]domain.Webhook, error) {
	rows, err := s.pool.Query(ctx, selectWebhook+` ORDER BY w.created_at`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []domain.Webhook
	for rows.Next() {
		wh, err := scanWebhook(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, wh)
	}
	return out, rows.Err()
}

func (s *Store) CreateWebhook(ctx context.Context, in service.CreateWebhookInput) (domain.Webhook, error) {
	events := in.Events
	if events == nil {
		events = []string{}
	}
	const q = `
		INSERT INTO webhooks (project_id, url, secret, events, created_by)
		VALUES ((SELECT id FROM projects WHERE key = NULLIF($1, '')), $2, $3, $4, $5)
		RETURNING id`
	var id uuid.UUID
	if err := s.pool.QueryRow(ctx, q, strings.TrimSpace(in.ProjectKey), in.URL, in.Secret, events, in.CreatedBy).
		Scan(&id); err != nil {
		return domain.Webhook{}, err
	}
	return scanWebhook(s.pool.QueryRow(ctx, selectWebhook+` WHERE w.id = $1`, id))
}

func (s *Store) UpdateWebhook(ctx context.Context, in service.UpdateWebhookInput) (domain.Webhook, error) {
	const q = `
		UPDATE webhooks SET
		  url       = COALESCE($2, url),
		  secret    = COALESCE($3, secret),
		  events    = COALESCE($4, events),
		  is_active = COALESCE($5, is_active)
		WHERE id = $1
		RETURNING id`
	var id uuid.UUID
	if err := s.pool.QueryRow(ctx, q, in.ID, in.URL, in.Secret, in.Events, in.IsActive).Scan(&id); err != nil {
		return domain.Webhook{}, err
	}
	return scanWebhook(s.pool.QueryRow(ctx, selectWebhook+` WHERE w.id = $1`, id))
}

func (s *Store) DeleteWebhook(ctx context.Context, id uuid.UUID) (bool, error) {
	tag, err := s.pool.Exec(ctx, `DELETE FROM webhooks WHERE id = $1`, id)
	if err != nil {
		return false, err
	}
	return tag.RowsAffected() > 0, nil
}

func (s *Store) ListWebhookDeliveries(ctx context.Context, webhookID uuid.UUID, limit int32) ([]domain.WebhookDelivery, error) {
	rows, err := s.pool.Query(ctx, `
		SELECT id, event_type, status, response_code, attempt, created_at
		FROM webhook_deliveries WHERE webhook_id = $1
		ORDER BY created_at DESC LIMIT $2`, webhookID, clampLimit(limit))
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []domain.WebhookDelivery
	for rows.Next() {
		var d domain.WebhookDelivery
		if err := rows.Scan(&d.ID, &d.EventType, &d.Status, &d.ResponseCode, &d.Attempt, &d.CreatedAt); err != nil {
			return nil, err
		}
		out = append(out, d)
	}
	return out, rows.Err()
}

func (s *Store) GetWebhookDelivery(ctx context.Context, id uuid.UUID) (domain.WebhookDelivery, error) {
	var d domain.WebhookDelivery
	err := s.pool.QueryRow(ctx, `
		SELECT id, webhook_id, event_type, status, response_code, attempt, payload, created_at
		FROM webhook_deliveries WHERE id = $1`, id).
		Scan(&d.ID, &d.WebhookID, &d.EventType, &d.Status, &d.ResponseCode, &d.Attempt, &d.Payload, &d.CreatedAt)
	return d, err
}

// ResetWebhookDelivery flips a delivery back to pending before a manual redelivery.
func (s *Store) ResetWebhookDelivery(ctx context.Context, id uuid.UUID) error {
	_, err := s.pool.Exec(ctx,
		`UPDATE webhook_deliveries SET status = 'pending', updated_at = now() WHERE id = $1`, id)
	return err
}
