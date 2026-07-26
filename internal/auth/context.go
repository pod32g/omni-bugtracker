// Package auth handles Omni-Identity JWT validation, API-token authentication,
// and role-based access control. Omni-Identity is the only identity provider.
package auth

import (
	"context"

	"github.com/omni/bugtracker/internal/domain"
)

// SessionCookie holds the Omni-Identity access token (JWT) after browser login.
// Set httpOnly by the OIDC callback; read by the auth middleware as a bearer fallback.
const SessionCookie = "obt_session"

type ctxKey struct{}

// Principal is the authenticated caller for a request.
type Principal struct {
	UserID      string
	IdentitySub string
	Email       string
	DisplayName string
	Role        domain.Role
	Scopes      []string // for API tokens
	ViaToken    bool
	// TokenID identifies the API token the request arrived on (empty for sessions).
	// Rate limiting budgets each token separately from its owner's browser session.
	TokenID string
}

// WithPrincipal stores the principal on the request context.
func WithPrincipal(ctx context.Context, p *Principal) context.Context {
	return context.WithValue(ctx, ctxKey{}, p)
}

// FromContext returns the principal, or nil if unauthenticated.
func FromContext(ctx context.Context) *Principal {
	p, _ := ctx.Value(ctxKey{}).(*Principal)
	return p
}
