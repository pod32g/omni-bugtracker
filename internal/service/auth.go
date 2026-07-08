package service

import (
	"context"

	"github.com/omni/bugtracker/internal/auth"
)

// Auth implements the HTTP middleware's Authenticator port.
type Auth struct {
	repo Repository
}

func NewAuth(repo Repository) *Auth {
	return &Auth{repo: repo}
}

// AuthenticateToken resolves a hashed API token into a Principal and records usage.
func (a *Auth) AuthenticateToken(ctx context.Context, tokenHash []byte) (*auth.Principal, error) {
	tp, err := a.repo.GetUserByToken(ctx, tokenHash)
	if err != nil {
		return nil, err
	}
	go func() { _ = a.repo.TouchToken(context.WithoutCancel(ctx), tp.TokenID) }()
	return &auth.Principal{
		UserID:      tp.User.ID.String(),
		IdentitySub: tp.User.IdentitySub,
		Email:       tp.User.Email,
		DisplayName: tp.User.DisplayName,
		Role:        tp.User.Role,
		Scopes:      tp.Scopes,
		ViaToken:    true,
	}, nil
}

// SyncUser lazily mirrors the Omni-Identity subject and returns its Principal.
func (a *Auth) SyncUser(ctx context.Context, c *auth.Claims) (*auth.Principal, error) {
	u, err := a.repo.UpsertUser(ctx, UpsertUserParams{
		IdentitySub: c.Subject,
		Email:       c.Email,
		DisplayName: c.Name,
	})
	if err != nil {
		return nil, err
	}
	role := u.Role
	if c.Role != "" {
		role = c.Role // Identity is authoritative for role claims
	}
	return &auth.Principal{
		UserID:      u.ID.String(),
		IdentitySub: u.IdentitySub,
		Email:       u.Email,
		DisplayName: u.DisplayName,
		Role:        role,
	}, nil
}
