// Package middleware holds cross-cutting HTTP middleware: authentication, request
// logging, panic recovery, and rate limiting.
package middleware

import (
	"context"
	"net/http"
	"strings"

	"github.com/omni/bugtracker/internal/auth"
)

// Authenticator resolves credentials into a Principal. Implemented in the service layer
// (kept as an interface here to avoid a dependency on the repo/generated code).
type Authenticator interface {
	// AuthenticateToken resolves a presented API token (already hashed by the caller).
	AuthenticateToken(ctx context.Context, tokenHash []byte) (*auth.Principal, error)
	// SyncUser upserts an Omni-Identity subject and returns its principal.
	SyncUser(ctx context.Context, c *auth.Claims) (*auth.Principal, error)
}

// Auth validates the bearer credential (JWT or API token) and attaches a Principal.
func Auth(verifier *auth.Verifier, authn Authenticator) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			raw := bearer(r)
			if raw == "" {
				unauthorized(w)
				return
			}

			var (
				principal *auth.Principal
				err       error
			)
			if auth.LooksLikeAPIToken(raw) {
				principal, err = authn.AuthenticateToken(r.Context(), auth.HashToken(raw))
			} else {
				var claims *auth.Claims
				claims, err = verifier.Verify(r.Context(), raw)
				if err == nil {
					principal, err = authn.SyncUser(r.Context(), claims)
				}
			}
			if err != nil || principal == nil {
				unauthorized(w)
				return
			}
			next.ServeHTTP(w, r.WithContext(auth.WithPrincipal(r.Context(), principal)))
		})
	}
}

// bearer returns the credential from the Authorization header, or the httpOnly
// session cookie set by the browser login flow.
func bearer(r *http.Request) string {
	if h := r.Header.Get("Authorization"); strings.HasPrefix(h, "Bearer ") {
		return strings.TrimSpace(h[len("Bearer "):])
	}
	if c, err := r.Cookie(auth.SessionCookie); err == nil {
		return c.Value
	}
	return ""
}

func unauthorized(w http.ResponseWriter) {
	w.Header().Set("Content-Type", "application/problem+json")
	w.WriteHeader(http.StatusUnauthorized)
	_, _ = w.Write([]byte(`{"title":"unauthorized","status":401}`))
}
