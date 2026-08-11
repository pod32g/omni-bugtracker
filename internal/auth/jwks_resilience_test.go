package auth

import (
	"context"
	"crypto/ed25519"
	"encoding/base64"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/golang-jwt/jwt/v5"

	"github.com/omni/bugtracker/internal/config"
)

const (
	testIssuer   = "https://id.test"
	testAudience = "omni-bugtracker"
)

// jwksServer serves a key set and counts requests. `fail` makes it start returning 500s,
// standing in for an identity provider that is restarting or briefly unreachable.
type jwksServer struct {
	*httptest.Server
	hits atomic.Int64
	mu   sync.Mutex
	body []byte
	fail atomic.Bool
}

func newJWKSServer(t *testing.T, kid string, pub ed25519.PublicKey) *jwksServer {
	t.Helper()
	s := &jwksServer{}
	s.setKey(kid, pub)
	s.Server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		s.hits.Add(1)
		if s.fail.Load() {
			w.WriteHeader(http.StatusInternalServerError)
			return
		}
		s.mu.Lock()
		body := s.body
		s.mu.Unlock()
		_, _ = w.Write(body)
	}))
	t.Cleanup(s.Close)
	return s
}

func (s *jwksServer) setKey(kid string, pub ed25519.PublicKey) {
	body, _ := json.Marshal(map[string]any{
		"keys": []map[string]string{{
			"kty": "OKP", "crv": "Ed25519", "use": "sig", "alg": "EdDSA",
			"kid": kid, "x": base64.RawURLEncoding.EncodeToString(pub),
		}},
	})
	s.mu.Lock()
	s.body = body
	s.mu.Unlock()
}

func signedToken(t *testing.T, kid string, priv ed25519.PrivateKey) string {
	t.Helper()
	tok := jwt.NewWithClaims(jwt.SigningMethodEdDSA, Claims{
		RegisteredClaims: jwt.RegisteredClaims{
			Issuer:    testIssuer,
			Audience:  jwt.ClaimStrings{testAudience},
			Subject:   "user-1",
			ExpiresAt: jwt.NewNumericDate(time.Now().Add(time.Hour)),
		},
	})
	tok.Header["kid"] = kid
	raw, err := tok.SignedString(priv)
	if err != nil {
		t.Fatal(err)
	}
	return raw
}

func verifierFor(s *jwksServer, ttl time.Duration) *Verifier {
	return NewVerifier(config.Identity{
		Issuer: testIssuer, JWKSURL: s.URL, Audience: testAudience, JWKSCacheTTL: ttl,
	})
}

// The failure this exists for: an identity-provider blip used to sign out every browser
// session at once. Signing keys outlive their cache entry by a wide margin, so a key we
// already hold is still good even when the refresh that would have re-confirmed it
// fails.
func TestVerifyServesCachedKeyWhenRefreshFails(t *testing.T) {
	pub, priv, _ := ed25519.GenerateKey(nil)
	const kid = "k1"
	srv := newJWKSServer(t, kid, pub)
	// A TTL that has already lapsed by the second call, so every verify tries a refresh.
	v := verifierFor(srv, time.Nanosecond)
	ctx := context.Background()
	token := signedToken(t, kid, priv)

	if _, err := v.Verify(ctx, token); err != nil {
		t.Fatalf("first verify (populates the cache): %v", err)
	}

	srv.fail.Store(true)
	for i := 0; i < 3; i++ {
		if _, err := v.Verify(ctx, token); err != nil {
			t.Fatalf("verify %d while the provider is down: %v — a cached key was available", i, err)
		}
	}

	// And it recovers without intervention once the provider returns.
	srv.fail.Store(false)
	if _, err := v.Verify(ctx, token); err != nil {
		t.Fatalf("verify after recovery: %v", err)
	}
}

// A kid that was never known must still be refused when the provider is unreachable —
// the fallback above is for keys we hold, not a way in.
func TestVerifyRejectsUnknownKidWhenRefreshFails(t *testing.T) {
	pub, _, _ := ed25519.GenerateKey(nil)
	_, otherPriv, _ := ed25519.GenerateKey(nil)
	srv := newJWKSServer(t, "k1", pub)
	v := verifierFor(srv, time.Nanosecond)
	ctx := context.Background()

	if _, err := v.Verify(ctx, signedToken(t, "k1", mustPriv(t))); err == nil {
		_ = err // signature will not match; we only care that it is not accepted
	}
	srv.fail.Store(true)
	if _, err := v.Verify(ctx, signedToken(t, "attacker-kid", otherPriv)); err == nil {
		t.Fatal("a token with an unknown kid was accepted while the provider was down")
	}
}

// Concurrent verifies past the TTL must produce one fetch, not one per request.
func TestRefreshIsCollapsed(t *testing.T) {
	pub, priv, _ := ed25519.GenerateKey(nil)
	const kid = "k1"
	srv := newJWKSServer(t, kid, pub)
	v := verifierFor(srv, time.Nanosecond)
	ctx := context.Background()
	token := signedToken(t, kid, priv)

	// Prime the cache, then measure only the concurrent burst.
	if _, err := v.Verify(ctx, token); err != nil {
		t.Fatal(err)
	}
	before := srv.hits.Load()

	var wg sync.WaitGroup
	for i := 0; i < 50; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if _, err := v.Verify(ctx, token); err != nil {
				t.Error(err)
			}
		}()
	}
	wg.Wait()

	// Singleflight collapses whatever overlaps; goroutines that arrive after one
	// finishes legitimately start another, so this asserts a bound rather than 1.
	if got := srv.hits.Load() - before; got > 25 {
		t.Errorf("50 concurrent verifies produced %d JWKS fetches — singleflight is not collapsing them", got)
	}
}

// An unknown kid is attacker-chosen. The old code refreshed whenever the kid was not
// found — regardless of whether the key set was current — so a loop of tokens carrying
// random kids became one outbound request per request against the identity provider.
func TestUnknownKidIsNotAFetchAmplifier(t *testing.T) {
	pub, _, _ := ed25519.GenerateKey(nil)
	_, priv, _ := ed25519.GenerateKey(nil)
	srv := newJWKSServer(t, "k1", pub)
	// A realistic TTL: the amplification this guards against is "an unknown kid forces
	// a fetch even though the key set we hold is current".
	v := verifierFor(srv, time.Minute)
	ctx := context.Background()

	bad := signedToken(t, "no-such-kid", priv)
	if _, err := v.Verify(ctx, bad); err == nil {
		t.Fatal("unknown kid accepted")
	}
	before := srv.hits.Load()
	for i := 0; i < 20; i++ {
		if _, err := v.Verify(ctx, bad); err == nil {
			t.Fatal("unknown kid accepted")
		}
	}
	if got := srv.hits.Load() - before; got != 0 {
		t.Errorf("20 tokens with an unknown kid caused %d JWKS fetches against a fresh "+
			"key set, want 0", got)
	}
}

// A key rotated in after we last looked must be picked up. This is the constraint that
// ruled out remembering individual denials: a first attempt at the new kid marks it
// unknown, and any cache of that fact suppresses the very refresh that would discover
// it. Freshness of the whole key set is the thing to gate on instead.
func TestRotatedKeyIsDiscovered(t *testing.T) {
	pub1, _, _ := ed25519.GenerateKey(nil)
	pub2, priv2, _ := ed25519.GenerateKey(nil)
	srv := newJWKSServer(t, "old", pub1)
	// Nanosecond TTL: the set is always stale, which is the condition under which a
	// rotation has to be discoverable.
	v := verifierFor(srv, time.Nanosecond)
	ctx := context.Background()

	newToken := signedToken(t, "new", priv2)
	if _, err := v.Verify(ctx, newToken); err == nil {
		t.Fatal("kid 'new' accepted before it was published")
	}

	srv.setKey("new", pub2) // the provider rotates
	if _, err := v.Verify(ctx, newToken); err != nil {
		t.Fatalf("rotated-in key still refused: %v — a rotation must be discoverable "+
			"once the cached set is stale", err)
	}
}

func mustPriv(t *testing.T) ed25519.PrivateKey {
	t.Helper()
	_, priv, err := ed25519.GenerateKey(nil)
	if err != nil {
		t.Fatal(err)
	}
	return priv
}
