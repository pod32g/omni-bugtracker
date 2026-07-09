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
	// The tracker's DB is authoritative for its own RBAC roles (managed via the
	// Members admin / promoted by an owner). We intentionally do NOT let the identity
	// token's `role` claim override it — Omni-Identity is the IdP, not the role store.
	return &auth.Principal{
		UserID:      u.ID.String(),
		IdentitySub: u.IdentitySub,
		Email:       u.Email,
		DisplayName: u.DisplayName,
		Role:        u.Role,
	}, nil
}
