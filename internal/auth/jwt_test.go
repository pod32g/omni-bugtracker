package auth

import (
	"context"
	"crypto/ed25519"
	"encoding/base64"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/golang-jwt/jwt/v5"

	"github.com/omni/bugtracker/internal/config"
)

// TestVerifyEd25519 proves the verifier accepts EdDSA/Ed25519 tokens (as Omni-Identity
// mints them) validated against an OKP JWKS, and enforces issuer + audience.
func TestVerifyEd25519(t *testing.T) {
	pub, priv, err := ed25519.GenerateKey(nil)
	if err != nil {
		t.Fatal(err)
	}
	const kid = "test-key-1"

	// Serve a JWKS containing the Ed25519 public key (OKP).
	jwksJSON, _ := json.Marshal(map[string]any{
		"keys": []map[string]string{{
			"kty": "OKP", "crv": "Ed25519", "use": "sig", "alg": "EdDSA",
			"kid": kid, "x": base64.RawURLEncoding.EncodeToString(pub),
		}},
	})
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write(jwksJSON)
	}))
	defer srv.Close()

	const issuer = "https://id.test"
	const audience = "omni-bugtracker"
	v := NewVerifier(config.Identity{
		Issuer: issuer, JWKSURL: srv.URL, Audience: audience, JWKSCacheTTL: time.Minute,
	})

	mint := func(iss, aud, sub string) string {
		tok := jwt.NewWithClaims(jwt.SigningMethodEdDSA, jwt.MapClaims{
			"iss": iss, "aud": aud, "sub": sub,
			"iat": time.Now().Unix(), "exp": time.Now().Add(time.Hour).Unix(),
			"token_use": "access", "scope": "openid email profile",
		})
		tok.Header["kid"] = kid
		s, err := tok.SignedString(priv)
		if err != nil {
			t.Fatal(err)
		}
		return s
	}

	t.Run("valid token", func(t *testing.T) {
		claims, err := v.Verify(context.Background(), mint(issuer, audience, "user-123"))
		if err != nil {
			t.Fatalf("expected valid, got %v", err)
		}
		if claims.Subject != "user-123" {
			t.Fatalf("sub = %q", claims.Subject)
		}
		if claims.Role != "member" {
			t.Fatalf("role default = %q, want member", claims.Role)
		}
	})

	t.Run("wrong audience rejected", func(t *testing.T) {
		if _, err := v.Verify(context.Background(), mint(issuer, "someone-else", "u")); err == nil {
			t.Fatal("expected audience rejection")
		}
	})

	t.Run("wrong issuer rejected", func(t *testing.T) {
		if _, err := v.Verify(context.Background(), mint("https://evil", audience, "u")); err == nil {
			t.Fatal("expected issuer rejection")
		}
	})

	t.Run("tampered signature rejected", func(t *testing.T) {
		tok := mint(issuer, audience, "u") + "x"
		if _, err := v.Verify(context.Background(), tok); err == nil {
			t.Fatal("expected signature rejection")
		}
	})
}
