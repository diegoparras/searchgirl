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
	federated    bool     // OIDC (Lockatus) active
	localUser    [32]byte // sha256 of SEARCHGIRL_USER (local login, standalone only)
	localPass    [32]byte // sha256 of SEARCHGIRL_PASS
	local        bool
	tokens       []apiToken // SEARCHGIRL_MCP_TOKEN: Bearer secrets for programmatic clients (MCP/API)
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

// apiToken is one named Bearer secret. Several can coexist (one per client:
// "claude:abc..., n8n:def..."), so revoking one is removing it from the .env
// and restarting — without rotating the others.
type apiToken struct {
	name string
	hash [32]byte
}

// parseTokens reads SEARCHGIRL_MCP_TOKEN: a comma-separated list where each
// item is "name:secret" or a bare secret (auto-named token1, token2…).
func parseTokens(s string) []apiToken {
	var out []apiToken
	for _, item := range strings.Split(s, ",") {
		item = strings.TrimSpace(item)
		if item == "" {
			continue
		}
		name, secret := fmt.Sprintf("token%d", len(out)+1), item
		if i := strings.Index(item, ":"); i > 0 {
			name, secret = strings.TrimSpace(item[:i]), strings.TrimSpace(item[i+1:])
		}
		if secret == "" {
			continue
		}
		out = append(out, apiToken{name: name, hash: sha256.Sum256([]byte(secret))})
	}
	return out
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

// FromEnv builds auth from the environment. Three independent mechanisms;
// any one of them authorizes a protected request:
//
//   - SEARCHGIRL_MCP_TOKEN: a shared Bearer token — the simple way to secure
//     the MCP + API for a programmatic client on a VPS.
//   - SEARCHGIRL_USER + SEARCHGIRL_PASS: local login (one user, from .env)
//     with the Escriba login card. Standalone only: in federated mode these
//     are IGNORED — the suite contract forbids a local backdoor next to SSO.
//   - AUTH_MODE=federado + LOCKATUS_*: OIDC/Lockatus session cookie.
//
// Humans use local login or SSO; machines use the token. With nothing set,
// auth is Disabled (standalone: safe only on loopback or behind a firewall).
func FromEnv(ctx context.Context) (*Auth, error) {
	tokens := parseTokens(os.Getenv("SEARCHGIRL_MCP_TOKEN"))
	if os.Getenv("AUTH_MODE") != "federado" {
		user, pass := os.Getenv("SEARCHGIRL_USER"), os.Getenv("SEARCHGIRL_PASS")
		if user != "" && pass != "" {
			secret := []byte(os.Getenv("SECRET_KEY"))
			if len(secret) == 0 {
				secret = make([]byte, 32)
				_, _ = rand.Read(secret) // ephemeral: sessions reset on restart
			}
			a := &Auth{
				enabled: true, local: true, tokens: tokens,
				localUser: sha256.Sum256([]byte(user)), localPass: sha256.Sum256([]byte(pass)),
				secret: secret, cookieSecure: os.Getenv("COOKIE_SECURE") == "1",
			}
			return a, nil
		}
		if len(tokens) == 0 {
			return Disabled(), nil
		}
		return &Auth{enabled: true, tokens: tokens}, nil // token-only, no OIDC
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
		tokens:       tokens,
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

// IsAdmin reports whether the request may change privileged settings (the LLM
// model panel). Standalone-open (no auth, loopback) is your own instance → yes.
// With auth, only a session whose role is "admin" (local login is always admin;
// federated takes the role from Lockatus). A Bearer-token-only client (an
// agent) is not an admin — model config is a UI action, not an MCP one.
func (a *Auth) IsAdmin(r *http.Request) bool {
	if !a.enabled {
		return true
	}
	if s := a.session(r); s != nil {
		return s.Role == "admin"
	}
	return false
}

// RegisterRoutes adds the auth endpoints. /auth/me is always present (the SPA
// uses it to decide whether to show the login screen); the flow routes only
// exist when federated.
func (a *Auth) RegisterRoutes(mux *http.ServeMux) {
	mux.HandleFunc("/auth/me", a.handleMe)
	if a.local {
		// Login local (solo standalone): un usuario, desde el .env. En modo
		// federado estas rutas NO existen — sin puerta de atrás junto al SSO.
		mux.HandleFunc("POST /auth/login", a.handleLocalLogin)
		mux.HandleFunc("/auth/logout", a.handleLogout)
		return
	}
	if !a.federated {
		return
	}
	mux.HandleFunc("/auth/login", a.handleLogin)
	mux.HandleFunc("/auth/callback", a.handleCallback)
	mux.HandleFunc("/auth/logout", a.handleLogout)
}

// handleLocalLogin validates the single .env user in constant time and seeds
// the same HMAC session cookie the federated flow uses.
func (a *Auth) handleLocalLogin(w http.ResponseWriter, r *http.Request) {
	var in struct {
		User     string `json:"user"`
		Password string `json:"password"`
	}
	if err := json.NewDecoder(r.Body).Decode(&in); err != nil {
		http.Error(w, "bad request", http.StatusBadRequest)
		return
	}
	u, p := sha256.Sum256([]byte(in.User)), sha256.Sum256([]byte(in.Password))
	okU := subtle.ConstantTimeCompare(u[:], a.localUser[:])
	okP := subtle.ConstantTimeCompare(p[:], a.localPass[:])
	if okU&okP != 1 {
		w.Header().Set("Content-Type", "application/json; charset=utf-8")
		w.WriteHeader(http.StatusUnauthorized)
		_, _ = w.Write([]byte(`{"error":"usuario o contraseña incorrectos"}`))
		return
	}
	a.setSession(w, session{Email: in.User, Role: "admin", Exp: time.Now().Add(sessionTTL).UnixMilli()})
	w.WriteHeader(http.StatusNoContent)
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

// authorized accepts a valid session cookie (browser: OIDC or local login)
// or a matching Bearer token (programmatic MCP client). Any is sufficient.
func (a *Auth) authorized(r *http.Request) bool {
	if (a.federated || a.local) && a.session(r) != nil {
		return true
	}
	_, ok := a.bearerToken(r)
	return ok
}

// bearerToken matches the Authorization: Bearer header against every
// configured token (constant-time compare over sha256, no early-exit timing
// leak) and returns the matched token's name.
func (a *Auth) bearerToken(r *http.Request) (string, bool) {
	const p = "Bearer "
	h := r.Header.Get("Authorization")
	if len(a.tokens) == 0 || !strings.HasPrefix(h, p) {
		return "", false
	}
	got := sha256.Sum256([]byte(strings.TrimSpace(h[len(p):])))
	name, matched := "", false
	for _, t := range a.tokens { // sin corte temprano: se comparan todos
		if subtle.ConstantTimeCompare(got[:], t.hash[:]) == 1 {
			name, matched = t.name, true
		}
	}
	return name, matched
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
	// Los errores del intercambio/verificación se loguean con detalle
	// server-side pero al cliente le llega un mensaje genérico: no filtrar
	// URLs del issuer, client-id ni el motivo exacto del rechazo (recon).
	tok, err := a.oauth2.Exchange(r.Context(), r.URL.Query().Get("code"), oauth2.VerifierOption(fl.verifier))
	if err != nil {
		fmt.Fprintf(os.Stderr, "searchgirl auth: token exchange failed: %v\n", err)
		http.Error(w, "authentication failed", http.StatusBadGateway)
		return
	}
	rawID, _ := tok.Extra("id_token").(string)
	if rawID == "" {
		fmt.Fprintln(os.Stderr, "searchgirl auth: no id_token in token response")
		http.Error(w, "authentication failed", http.StatusBadGateway)
		return
	}
	idToken, err := a.verifier.Verify(r.Context(), rawID)
	if err != nil {
		fmt.Fprintf(os.Stderr, "searchgirl auth: id_token verification failed: %v\n", err)
		http.Error(w, "authentication failed", http.StatusUnauthorized)
		return
	}
	if idToken.Nonce != fl.nonce {
		http.Error(w, "authentication failed", http.StatusUnauthorized)
		return
	}
	var c claims
	if err := idToken.Claims(&c); err != nil {
		fmt.Fprintf(os.Stderr, "searchgirl auth: id_token claims decode failed: %v\n", err)
		http.Error(w, "authentication failed", http.StatusBadGateway)
		return
	}
	a.setSession(w, session{Email: c.Email, Name: c.Name, Role: c.Role, Exp: time.Now().Add(sessionTTL).UnixMilli()})
	http.Redirect(w, r, "/", http.StatusFound)
}

// handleLogout clears the cookie and shows a "session closed" screen — it
// does NOT bounce back to the SSO (the hub still has a session and would
// re-enter on its own). Re-entering is user-initiated via the button.
func (a *Auth) handleLogout(w http.ResponseWriter, r *http.Request) {
	a.clearSession(w)
	// En local el login vive en la propia SPA ("/" muestra el gate); en
	// federado, /auth/login redirige al hub.
	back := "/auth/login"
	if a.local {
		back = "/"
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	_, _ = w.Write([]byte(strings.ReplaceAll(sessionClosedHTML, "{{BACK}}", back)))
}

func (a *Auth) handleMe(w http.ResponseWriter, r *http.Request) {
	resp := map[string]any{
		"enabled":       a.enabled,
		"mode":          a.Mode(),
		"authenticated": !a.enabled || a.authorized(r),
	}
	if a.federated || a.local {
		if s := a.session(r); s != nil {
			resp["email"] = s.Email
			resp["name"] = s.Name
			resp["role"] = s.Role
		}
	}
	if name, ok := a.bearerToken(r); ok {
		resp["token_name"] = name // qué cliente programático está autenticado
	}
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	_ = json.NewEncoder(w).Encode(resp)
}

// Mode names the active auth mechanism, for the startup banner and /auth/me.
func (a *Auth) Mode() string {
	if a.federated {
		return "federated"
	}
	if a.local {
		return "local"
	}
	if len(a.tokens) > 0 {
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
<a class="login-sso" href="{{BACK}}">Volver a entrar</a>
</div></div></body></html>`
