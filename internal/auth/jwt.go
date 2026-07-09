package auth

import (
	"context"
	"crypto"
	"crypto/ed25519"
	"crypto/rsa"
	"encoding/base64"
	"encoding/binary"
	"encoding/json"
	"fmt"
	"math/big"
	"net/http"
	"sync"
	"time"

	"github.com/golang-jwt/jwt/v5"

	"github.com/omni/bugtracker/internal/config"
	"github.com/omni/bugtracker/internal/domain"
)

// Verifier validates Omni-Identity JWTs against a cached JWKS. Supports both RSA
// (RS256) and Ed25519 (EdDSA) signing keys — Omni-Identity currently signs with EdDSA.
type Verifier struct {
	cfg    config.Identity
	http   *http.Client
	mu     sync.RWMutex
	keys   map[string]crypto.PublicKey
	loaded time.Time
}

func NewVerifier(cfg config.Identity) *Verifier {
	return &Verifier{
		cfg:  cfg,
		http: &http.Client{Timeout: 5 * time.Second},
		keys: map[string]crypto.PublicKey{},
	}
}

// Claims are the subset of OIDC claims we map to a Principal.
type Claims struct {
	jwt.RegisteredClaims
	Email string      `json:"email"`
	Name  string      `json:"name"`
	Role  domain.Role `json:"role"`
}

// Verify parses and validates the token, returning mapped claims.
func (v *Verifier) Verify(ctx context.Context, raw string) (*Claims, error) {
	claims := &Claims{}
	_, err := jwt.ParseWithClaims(raw, claims, func(t *jwt.Token) (any, error) {
		switch t.Method.(type) {
		case *jwt.SigningMethodRSA, *jwt.SigningMethodEd25519:
			// supported
		default:
			return nil, fmt.Errorf("unexpected signing method %q", t.Header["alg"])
		}
		kid, _ := t.Header["kid"].(string)
		return v.keyFor(ctx, kid)
	},
		jwt.WithIssuer(v.cfg.Issuer),
		jwt.WithAudience(v.cfg.Audience),
		jwt.WithValidMethods([]string{"RS256", "EdDSA"}),
	)
	if err != nil {
		return nil, fmt.Errorf("verify token: %w", err)
	}
	return claims, nil
}

func (v *Verifier) keyFor(ctx context.Context, kid string) (crypto.PublicKey, error) {
	v.mu.RLock()
	key, ok := v.keys[kid]
	fresh := time.Since(v.loaded) < v.cfg.JWKSCacheTTL
	v.mu.RUnlock()
	if ok && fresh {
		return key, nil
	}
	if err := v.refresh(ctx); err != nil {
		return nil, err
	}
	v.mu.RLock()
	defer v.mu.RUnlock()
	if key, ok := v.keys[kid]; ok {
		return key, nil
	}
	return nil, fmt.Errorf("unknown key id %q", kid)
}

type jwks struct {
	Keys []struct {
		Kid string `json:"kid"`
		Kty string `json:"kty"`
		// RSA
		N string `json:"n"`
		E string `json:"e"`
		// OKP (Ed25519)
		Crv string `json:"crv"`
		X   string `json:"x"`
	} `json:"keys"`
}

func (v *Verifier) refresh(ctx context.Context) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, v.cfg.JWKSURL, nil)
	if err != nil {
		return err
	}
	resp, err := v.http.Do(req)
	if err != nil {
		return fmt.Errorf("fetch jwks: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("jwks status %d", resp.StatusCode)
	}
	var set jwks
	if err := json.NewDecoder(resp.Body).Decode(&set); err != nil {
		return fmt.Errorf("decode jwks: %w", err)
	}

	keys := make(map[string]crypto.PublicKey, len(set.Keys))
	for _, k := range set.Keys {
		switch k.Kty {
		case "RSA":
			if pub, err := parseRSA(k.N, k.E); err == nil {
				keys[k.Kid] = pub
			}
		case "OKP":
			if k.Crv == "Ed25519" {
				if pub, err := parseEd25519(k.X); err == nil {
					keys[k.Kid] = pub
				}
			}
		}
	}

	v.mu.Lock()
	v.keys = keys
	v.loaded = time.Now()
	v.mu.Unlock()
	return nil
}

func parseRSA(nStr, eStr string) (*rsa.PublicKey, error) {
	nb, err := base64.RawURLEncoding.DecodeString(nStr)
	if err != nil {
		return nil, err
	}
	eb, err := base64.RawURLEncoding.DecodeString(eStr)
	if err != nil {
		return nil, err
	}
	// Left-pad exponent to 8 bytes for uint64 decoding.
	padded := make([]byte, 8)
	copy(padded[8-len(eb):], eb)
	return &rsa.PublicKey{
		N: new(big.Int).SetBytes(nb),
		E: int(binary.BigEndian.Uint64(padded)),
	}, nil
}

func parseEd25519(xStr string) (ed25519.PublicKey, error) {
	xb, err := base64.RawURLEncoding.DecodeString(xStr)
	if err != nil {
		return nil, err
	}
	if len(xb) != ed25519.PublicKeySize {
		return nil, fmt.Errorf("bad ed25519 key length %d", len(xb))
	}
	return ed25519.PublicKey(xb), nil
}
