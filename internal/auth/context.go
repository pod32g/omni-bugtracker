// Package auth handles Omni-Identity JWT validation, API-token authentication,
// and role-based access control. Omni-Identity is the only identity provider.
package auth

import (
	"context"

	"github.com/omni/bugtracker/internal/domain"
)

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
