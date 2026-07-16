package pg

import (
	"context"
	"errors"

	"github.com/jackc/pgx/v5"
)

// GetSetting returns the raw JSONB value for a settings key, or (nil, nil) if unset.
func (s *Store) GetSetting(ctx context.Context, key string) ([]byte, error) {
	var value []byte
	err := s.pool.QueryRow(ctx, `SELECT value FROM app_settings WHERE key = $1`, key).Scan(&value)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	return value, err
}

// SetSetting upserts a settings key with a JSONB value.
func (s *Store) SetSetting(ctx context.Context, key string, value []byte) error {
	_, err := s.pool.Exec(ctx,
		`INSERT INTO app_settings (key, value, updated_at) VALUES ($1, $2, now())
		 ON CONFLICT (key) DO UPDATE SET value = EXCLUDED.value, updated_at = now()`,
		key, value)
	return err
}
