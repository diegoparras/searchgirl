// Package auth is the OPTIONAL federation accessory: OIDC login against
// Lockatus, adapted from COGO's. It is OFF in standalone (AUTH_MODE !=
// "federado"), so Searchgirl serves with no auth at all.
//
// It mirrors how the Escriba suite federates: a PUBLIC client with PKCE S256
// (no client secret), authorization-code flow, and a signed HMAC session
// cookie. Searchgirl has no local login, so the contract's "block local login
// in the server" rule is satisfied by construction.
package auth

import (
	"context"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"os"
	"strings"
	"sync"
	"time"

	"github.com/coreos/go-oidc/v3/oidc"
	"golang.org/x/oauth2"
)

const cookieName = "searchgirl_session"
const sessionTTL = 12 * time.Hour

type Auth struct {
	enabled      bool
	federated    bool   // OIDC (Lockatus) active
	token        string // SEARCHGIRL_MCP_TOKEN: shared Bearer secret for programmatic clients (MCP/API)
	secret       []byte
	cookieSecure bool

	oauth2   oauth2.Config
	verifier *oidc.IDTokenVerifier

	mu    sync.Mutex
	flows map[string]flow // login state -> PKCE verifier + nonce
}

type flow struct {
	verifier string
	nonce    string
	exp      time.Time
}

type claims struct {
	Email string `json:"email"`
	Name  string `json:"name"`
	Role  string `json:"role"`
}

type session struct {
	Email string `json:"email"`
	Name  string `json:"name"`
	Role  string `json:"role"`
	Exp   int64  `json:"exp"`
}

// Disabled returns an auth that gates nothing (standalone).
func Disabled() *Auth { return &Auth{enabled: false} }

// FromEnv builds auth from the environment. Two independent mechanisms,
// either of which authorizes a protected request:
//
//   - SEARCHGIRL_MCP_TOKEN: a shared Bearer token — the simple way to secure
//     the MCP + API for a programmatic client on a VPS.
//   - AUTH_MODE=federado + LOCKATUS_*: OIDC/Lockatus session cookie (the
//     browser path). They compose: OIDC for humans, the token for machines.
//
// With neither set, auth is Disabled (standalone: safe only on loopback or
// behind your own firewall).
func FromEnv(ctx context.Context) (*Auth, error) {
	token := os.Getenv("SEARCHGIRL_MCP_TOKEN")
	if os.Getenv("AUTH_MODE") != "federado" {
		if token == "" {
			return Disabled(), nil
		}
		return &Auth{enabled: true, token: token}, nil // token-only, no OIDC
	}
	issuer := os.Getenv("LOCKATUS_ISSUER")
	clientID := os.Getenv("LOCKATUS_CLIENT_ID")
	redirect := os.Getenv("LOCKATUS_REDIRECT_URI")
	if issuer == "" || clientID == "" || redirect == "" {
		return nil, errors.New("AUTH_MODE=federado needs LOCKATUS_ISSUER, LOCKATUS_CLIENT_ID and LOCKATUS_REDIRECT_URI")
	}
	provider, err := oidc.NewProvider(ctx, issuer)
	if err != nil {
		return nil, fmt.Errorf("lockatus discovery failed (%s): %w", issuer, err)
	}
	secret := []byte(os.Getenv("SECRET_KEY"))
	if len(secret) == 0 {
		secret = make([]byte, 32)
		_, _ = rand.Read(secret) // ephemeral: sessions reset on restart
	}
	return &Auth{
		enabled:      true,
		federated:    true,
		token:        token,
		secret:       secret,
		cookieSecure: os.Getenv("COOKIE_SECURE") == "1",
		oauth2: oauth2.Config{
			ClientID:    clientID,
			RedirectURL: redirect,
			Endpoint:    provider.Endpoint(),
			Scopes:      []string{oidc.ScopeOpenID, "email"},
		},
		verifier: provider.Verifier(&oidc.Config{ClientID: clientID}),
		flows:    map[string]flow{},
	}, nil
}

func (a *Auth) Enabled() bool { return a.enabled }

// RegisterRoutes adds the auth endpoints. /auth/me is always present (the SPA
// uses it to decide whether to show the login screen); the flow routes only
// exist when federated.
func (a *Auth) RegisterRoutes(mux *http.ServeMux) {
	mux.HandleFunc("/auth/me", a.handleMe)
	if !a.enabled {
		return
	}
	mux.HandleFunc("/auth/login", a.handleLogin)
	mux.HandleFunc("/auth/callback", a.handleCallback)
	mux.HandleFunc("/auth/logout", a.handleLogout)
}

// Gate blocks the protected paths when auth is on and the request is
// unauthenticated. Static assets, /auth/* and /healthz stay open so the login
// screen can render.
func (a *Auth) Gate(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if a.enabled && protected(r.URL.Path) && !a.authorized(r) {
			http.Error(w, "unauthorized", http.StatusUnauthorized)
			return
		}
		next.ServeHTTP(w, r)
	})
}

// authorized accepts either a valid OIDC session cookie (browser) or a
// matching Bearer token (programmatic MCP client). Either is sufficient.
func (a *Auth) authorized(r *http.Request) bool {
	if a.federated && a.session(r) != nil {
		return true
	}
	return a.token != "" && a.bearerOK(r)
}

// bearerOK compares the Authorization: Bearer header to the configured token
// in constant time (no early-exit timing leak).
func (a *Auth) bearerOK(r *http.Request) bool {
	const p = "Bearer "
	h := r.Header.Get("Authorization")
	if !strings.HasPrefix(h, p) {
		return false
	}
	got := strings.TrimSpace(h[len(p):])
	return subtle.ConstantTimeCompare([]byte(got), []byte(a.token)) == 1
}

// protected covers everything that spends resources or reaches the network on
// behalf of the caller: the JSON API, the MCP endpoint and the thumbnail
// proxy (an open /thumb would be a free outbound proxy).
func protected(path string) bool {
	return strings.HasPrefix(path, "/api/") || path == "/mcp" || path == "/thumb"
}

func (a *Auth) handleLogin(w http.ResponseWriter, r *http.Request) {
	state, verifier, nonce := randToken(), oauth2.GenerateVerifier(), randToken()
	a.mu.Lock()
	a.gcLocked()
	a.flows[state] = flow{verifier: verifier, nonce: nonce, exp: time.Now().Add(10 * time.Minute)}
	a.mu.Unlock()
	url := a.oauth2.AuthCodeURL(state, oauth2.S256ChallengeOption(verifier), oauth2.SetAuthURLParam("nonce", nonce))
	http.Redirect(w, r, url, http.StatusFound)
}

func (a *Auth) handleCallback(w http.ResponseWriter, r *http.Request) {
	state := r.URL.Query().Get("state")
	a.mu.Lock()
	fl, ok := a.flows[state]
	delete(a.flows, state)
	a.mu.Unlock()
	if !ok || time.Now().After(fl.exp) {
		http.Error(w, "invalid or expired login state", http.StatusBadRequest)
		return
	}
	tok, err := a.oauth2.Exchange(r.Context(), r.URL.Query().Get("code"), oauth2.VerifierOption(fl.verifier))
	if err != nil {
		http.Error(w, "token exchange failed: "+err.Error(), http.StatusBadGateway)
		return
	}
	rawID, _ := tok.Extra("id_token").(string)
	if rawID == "" {
		http.Error(w, "no id_token in token response", http.StatusBadGateway)
		return
	}
	idToken, err := a.verifier.Verify(r.Context(), rawID)
	if err != nil {
		http.Error(w, "id_token verification failed: "+err.Error(), http.StatusUnauthorized)
		return
	}
	if idToken.Nonce != fl.nonce {
		http.Error(w, "nonce mismatch", http.StatusUnauthorized)
		return
	}
	var c claims
	_ = idToken.Claims(&c)
	a.setSession(w, session{Email: c.Email, Name: c.Name, Role: c.Role, Exp: time.Now().Add(sessionTTL).UnixMilli()})
	http.Redirect(w, r, "/", http.StatusFound)
}

// handleLogout clears the cookie and shows a "session closed" screen — it
// does NOT bounce back to the SSO (the hub still has a session and would
// re-enter on its own). Re-entering is user-initiated via the button.
func (a *Auth) handleLogout(w http.ResponseWriter, r *http.Request) {
	a.clearSession(w)
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	_, _ = w.Write([]byte(sessionClosedHTML))
}

func (a *Auth) handleMe(w http.ResponseWriter, r *http.Request) {
	resp := map[string]any{
		"enabled":       a.enabled,
		"mode":          a.Mode(),
		"authenticated": !a.enabled || a.authorized(r),
	}
	if a.federated {
		if s := a.session(r); s != nil {
			resp["email"] = s.Email
			resp["name"] = s.Name
			resp["role"] = s.Role
		}
	}
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	_ = json.NewEncoder(w).Encode(resp)
}

// Mode names the active auth mechanism, for the startup banner and /auth/me.
func (a *Auth) Mode() string {
	if a.federated {
		return "federated"
	}
	if a.token != "" {
		return "token"
	}
	return "off"
}

// --- session cookie: base64url(json) "." base64url(hmac-sha256) ---

func (a *Auth) setSession(w http.ResponseWriter, s session) {
	body, _ := json.Marshal(s)
	b := base64.RawURLEncoding.EncodeToString(body)
	a.writeCookie(w, b+"."+a.mac(b), int(sessionTTL.Seconds()))
}

func (a *Auth) clearSession(w http.ResponseWriter) { a.writeCookie(w, "", -1) }

func (a *Auth) writeCookie(w http.ResponseWriter, value string, maxAge int) {
	http.SetCookie(w, &http.Cookie{
		Name: cookieName, Value: value, Path: "/",
		HttpOnly: true, Secure: a.cookieSecure, SameSite: http.SameSiteLaxMode, MaxAge: maxAge,
	})
}

func (a *Auth) session(r *http.Request) *session {
	c, err := r.Cookie(cookieName)
	if err != nil {
		return nil
	}
	parts := strings.SplitN(c.Value, ".", 2)
	if len(parts) != 2 || !hmac.Equal([]byte(parts[1]), []byte(a.mac(parts[0]))) {
		return nil
	}
	raw, err := base64.RawURLEncoding.DecodeString(parts[0])
	if err != nil {
		return nil
	}
	var s session
	if json.Unmarshal(raw, &s) != nil || time.Now().UnixMilli() > s.Exp {
		return nil
	}
	return &s
}

func (a *Auth) mac(body string) string {
	h := hmac.New(sha256.New, a.secret)
	h.Write([]byte(body))
	return base64.RawURLEncoding.EncodeToString(h.Sum(nil))
}

func (a *Auth) gcLocked() {
	now := time.Now()
	for k, v := range a.flows {
		if now.After(v.exp) {
			delete(a.flows, k)
		}
	}
}

func randToken() string {
	b := make([]byte, 24)
	_, _ = rand.Read(b)
	return base64.RawURLEncoding.EncodeToString(b)
}

const sessionClosedHTML = `<!doctype html>
<html lang="es"><head><meta charset="utf-8">
<meta name="viewport" content="width=device-width, initial-scale=1">
<title>Sesión cerrada · Searchgirl</title>
<script>if(localStorage.getItem("searchgirl.theme")==="dark")document.documentElement.dataset.theme="dark"</script>
<link rel="stylesheet" href="/fonts.css">
<link rel="stylesheet" href="/escriba-ui.css">
<link rel="stylesheet" href="/app.css"></head>
<body><div class="login-overlay"><div class="login-card">
<img class="logo" src="/searchgirl.svg" alt="">
<h2>Sesión cerrada</h2>
<p class="login-sub">Cerraste sesión en Searchgirl.</p>
<a class="login-sso" href="/auth/login">Volver a entrar</a>
</div></div></body></html>`
