package service

import (
	"context"
	"errors"

	"github.com/omni/bugtracker/internal/auth"
)

// ErrUserDeactivated is returned when the credential is valid but the account behind
// it has been deactivated. The middleware turns any authentication error into a bare
// 401, which is the right answer here: whether an account exists is not something an
// unauthenticated caller gets to learn.
var ErrUserDeactivated = errors.New("user is deactivated")

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
		AvatarURL:   tp.User.AvatarURL,
		Role:        tp.User.Role,
		Scopes:      tp.Scopes,
		ViaToken:    true,
		TokenID:     tp.TokenID.String(),
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
	// A valid identity token is not by itself authorisation to use this tracker.
	// Omni-Identity may still be issuing tokens for somebody this install has
	// deactivated — the IdP is shared, and offboarding here must not depend on
	// offboarding there having happened first.
	// UpsertUser always selects the column, so nil here would mean the query changed
	// underneath us — fail closed rather than let a nil read as "active".
	if u.IsActive == nil || !*u.IsActive {
		return nil, ErrUserDeactivated
	}
	// The tracker's DB is authoritative for its own RBAC roles (managed via the
	// Members admin / promoted by an owner). We intentionally do NOT let the identity
	// token's `role` claim override it — Omni-Identity is the IdP, not the role store.
	return &auth.Principal{
		UserID:      u.ID.String(),
		IdentitySub: u.IdentitySub,
		Email:       u.Email,
		DisplayName: u.DisplayName,
		AvatarURL:   u.AvatarURL,
		Role:        u.Role,
	}, nil
}
