package service

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"log/slog"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"

	"github.com/omni/bugtracker/internal/auth"
	"github.com/omni/bugtracker/internal/config"
)

// OIDC implements the browser login flow as a Backend-For-Frontend: the API is a public
// PKCE client but performs the authorization-code exchange server-side (Omni-Identity
// emits no CORS headers, so the SPA cannot call the token endpoint directly). The
// resulting access token is handed to the browser as an httpOnly session cookie.
type OIDC struct {
	cfg    config.Identity
	repo   Repository
	http   *http.Client
	logger *slog.Logger
}

func NewOIDC(cfg config.Identity, repo Repository, logger *slog.Logger) *OIDC {
	return &OIDC{cfg: cfg, repo: repo, http: &http.Client{Timeout: 10 * time.Second}, logger: logger}
}

const (
	oauthTmpCookie = "obt_oauth"   // short-lived PKCE state/verifier carrier
	refreshCookie  = "obt_refresh" // long-lived refresh token (Path=/auth only)
	refreshMaxAge  = 30 * 24 * 3600
)

// Router returns the unauthenticated /auth endpoints, mounted at the root.
func (o *OIDC) Router() http.Handler {
	r := chi.NewRouter()
	r.Get("/login", o.login)
	r.Get("/callback", o.callback)
	r.Post("/refresh", o.refresh)
	r.Get("/logout", o.logout)
	return r
}

// setSession writes the httpOnly access-token cookie (and refresh cookie when present).
func (o *OIDC) setSession(w http.ResponseWriter, tok *tokenResp) {
	maxAge := tok.ExpiresIn
	if maxAge <= 0 {
		maxAge = 900
	}
	http.SetCookie(w, &http.Cookie{
		Name: auth.SessionCookie, Value: tok.AccessToken, Path: "/",
		HttpOnly: true, SameSite: http.SameSiteLaxMode, MaxAge: maxAge,
	})
	if tok.RefreshToken != "" {
		http.SetCookie(w, &http.Cookie{
			Name: refreshCookie, Value: tok.RefreshToken, Path: "/auth",
			HttpOnly: true, SameSite: http.SameSiteLaxMode, MaxAge: refreshMaxAge,
		})
	}
}

func (o *OIDC) login(w http.ResponseWriter, r *http.Request) {
	if !o.cfg.LoginEnabled() {
		http.Error(w, "login not configured", http.StatusNotImplemented)
		return
	}
	verifier := randToken()
	state := randToken()
	challenge := pkceChallenge(verifier)

	blob, _ := json.Marshal(map[string]string{"state": state, "verifier": verifier})
	http.SetCookie(w, &http.Cookie{
		Name:     oauthTmpCookie,
		Value:    base64.RawURLEncoding.EncodeToString(blob),
		Path:     "/auth",
		HttpOnly: true,
		SameSite: http.SameSiteLaxMode,
		MaxAge:   600,
	})

	q := url.Values{}
	q.Set("response_type", "code")
	q.Set("client_id", o.cfg.ClientID)
	q.Set("redirect_uri", o.cfg.RedirectURI)
	q.Set("scope", o.cfg.Scopes)
	q.Set("state", state)
	q.Set("code_challenge", challenge)
	q.Set("code_challenge_method", "S256")
	http.Redirect(w, r, o.cfg.AuthorizeURL+"?"+q.Encode(), http.StatusFound)
}

func (o *OIDC) callback(w http.ResponseWriter, r *http.Request) {
	c, err := r.Cookie(oauthTmpCookie)
	if err != nil {
		http.Error(w, "missing login state — restart sign-in", http.StatusBadRequest)
		return
	}
	clearCookie(w, oauthTmpCookie, "/auth")

	blob, _ := base64.RawURLEncoding.DecodeString(c.Value)
	var saved struct{ State, Verifier string }
	if err := json.Unmarshal(blob, &saved); err != nil || saved.State == "" {
		http.Error(w, "invalid login state", http.StatusBadRequest)
		return
	}
	if r.URL.Query().Get("state") != saved.State {
		http.Error(w, "state mismatch", http.StatusBadRequest)
		return
	}
	if e := r.URL.Query().Get("error"); e != "" {
		http.Error(w, "identity error: "+e, http.StatusBadRequest)
		return
	}

	tok, err := o.exchange(r.Context(), r.URL.Query().Get("code"), saved.Verifier)
	if err != nil {
		o.logger.Warn("token exchange failed", "err", err)
		http.Error(w, "token exchange failed", http.StatusBadGateway)
		return
	}

	o.setSession(w, tok)

	// Best-effort profile enrichment from the id_token (access tokens carry no email/name).
	o.enrich(r.Context(), tok.IDToken)

	http.Redirect(w, r, "/", http.StatusFound)
}

// refresh exchanges the refresh-token cookie for a fresh access token and re-sets the
// session cookie. The SPA calls this on a 401 so sessions survive the 15m token TTL.
func (o *OIDC) refresh(w http.ResponseWriter, r *http.Request) {
	c, err := r.Cookie(refreshCookie)
	if err != nil || c.Value == "" {
		http.Error(w, "no refresh token", http.StatusUnauthorized)
		return
	}
	tok, err := o.refreshExchange(r.Context(), c.Value)
	if err != nil {
		// Refresh failed (expired/revoked) — clear cookies so the SPA shows sign-in.
		clearCookie(w, auth.SessionCookie, "/")
		clearCookie(w, refreshCookie, "/auth")
		http.Error(w, "refresh failed", http.StatusUnauthorized)
		return
	}
	o.setSession(w, tok)
	w.WriteHeader(http.StatusNoContent)
}

func (o *OIDC) logout(w http.ResponseWriter, r *http.Request) {
	clearCookie(w, auth.SessionCookie, "/")
	clearCookie(w, refreshCookie, "/auth")
	dest := o.cfg.PostLogoutURL
	if dest == "" {
		dest = "/"
	}
	http.Redirect(w, r, dest, http.StatusFound)
}

type tokenResp struct {
	AccessToken  string `json:"access_token"`
	IDToken      string `json:"id_token"`
	RefreshToken string `json:"refresh_token"`
	TokenType    string `json:"token_type"`
	ExpiresIn    int    `json:"expires_in"`
}

func (o *OIDC) refreshExchange(ctx context.Context, refreshToken string) (*tokenResp, error) {
	form := url.Values{}
	form.Set("grant_type", "refresh_token")
	form.Set("refresh_token", refreshToken)
	form.Set("client_id", o.cfg.ClientID)
	return o.postToken(ctx, form)
}

func (o *OIDC) exchange(ctx context.Context, code, verifier string) (*tokenResp, error) {
	form := url.Values{}
	form.Set("grant_type", "authorization_code")
	form.Set("code", code)
	form.Set("redirect_uri", o.cfg.RedirectURI)
	form.Set("client_id", o.cfg.ClientID)
	form.Set("code_verifier", verifier)
	return o.postToken(ctx, form)
}

// postToken performs a token-endpoint request and decodes the response.
func (o *OIDC) postToken(ctx context.Context, form url.Values) (*tokenResp, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, o.cfg.TokenURL, strings.NewReader(form.Encode()))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("Accept", "application/json")

	resp, err := o.http.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, &httpStatusError{resp.StatusCode}
	}
	var tr tokenResp
	if err := json.NewDecoder(resp.Body).Decode(&tr); err != nil {
		return nil, err
	}
	return &tr, nil
}

// enrich upserts the user's email/name from the id_token claims (best-effort).
func (o *OIDC) enrich(ctx context.Context, idToken string) {
	parts := strings.Split(idToken, ".")
	if len(parts) != 3 {
		return
	}
	payload, err := base64.RawURLEncoding.DecodeString(parts[1])
	if err != nil {
		return
	}
	var claims struct {
		Sub               string `json:"sub"`
		Email             string `json:"email"`
		Name              string `json:"name"`
		PreferredUsername string `json:"preferred_username"`
	}
	if err := json.Unmarshal(payload, &claims); err != nil || claims.Sub == "" {
		return
	}
	name := claims.Name
	if name == "" {
		name = claims.PreferredUsername
	}
	if _, err := o.repo.UpsertUser(ctx, UpsertUserParams{
		IdentitySub: claims.Sub, Email: claims.Email, DisplayName: name,
	}); err != nil {
		o.logger.Warn("user enrich failed", "err", err)
	}
}

// ── helpers ──

type httpStatusError struct{ code int }

func (e *httpStatusError) Error() string { return "token endpoint status " + itoa(e.code) }

func randToken() string {
	b := make([]byte, 32)
	_, _ = rand.Read(b)
	return base64.RawURLEncoding.EncodeToString(b)
}

func pkceChallenge(verifier string) string {
	sum := sha256.Sum256([]byte(verifier))
	return base64.RawURLEncoding.EncodeToString(sum[:])
}

func clearCookie(w http.ResponseWriter, name, path string) {
	http.SetCookie(w, &http.Cookie{Name: name, Value: "", Path: path, HttpOnly: true, MaxAge: -1})
}

func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	var b [20]byte
	i := len(b)
	neg := n < 0
	if neg {
		n = -n
	}
	for n > 0 {
		i--
		b[i] = byte('0' + n%10)
		n /= 10
	}
	if neg {
		i--
		b[i] = '-'
	}
	return string(b[i:])
}
