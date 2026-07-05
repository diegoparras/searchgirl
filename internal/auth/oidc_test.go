package auth

import (
	"context"
	"crypto/rand"
	"crypto/rsa"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"
	"time"

	"github.com/coreos/go-oidc/v3/oidc"
	"github.com/go-jose/go-jose/v4"
	"golang.org/x/oauth2"
)

// fakeIssuer is a minimal OIDC provider for tests: discovery + JWKS + a token
// endpoint that returns an id_token we sign locally. It lets us exercise the
// full handleCallback path (exchange → verify → nonce) without a real Lockatus.
type fakeIssuer struct {
	srv      *httptest.Server
	key      *rsa.PrivateKey
	signer   jose.Signer
	clientID string
	// next id_token claims the /token endpoint will mint
	nextClaims map[string]any
}

func newFakeIssuer(t *testing.T, clientID string) *fakeIssuer {
	t.Helper()
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatal(err)
	}
	signer, err := jose.NewSigner(
		jose.SigningKey{Algorithm: jose.RS256, Key: key},
		(&jose.SignerOptions{}).WithType("JWT").WithHeader("kid", "test-key"),
	)
	if err != nil {
		t.Fatal(err)
	}
	fi := &fakeIssuer{key: key, signer: signer, clientID: clientID}

	mux := http.NewServeMux()
	mux.HandleFunc("/.well-known/openid-configuration", func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{
			"issuer":                                fi.srv.URL,
			"authorization_endpoint":                fi.srv.URL + "/authorize",
			"token_endpoint":                        fi.srv.URL + "/token",
			"jwks_uri":                              fi.srv.URL + "/jwks",
			"id_token_signing_alg_values_supported": []string{"RS256"},
		})
	})
	mux.HandleFunc("/jwks", func(w http.ResponseWriter, r *http.Request) {
		jwks := jose.JSONWebKeySet{Keys: []jose.JSONWebKey{{
			Key: key.Public(), KeyID: "test-key", Algorithm: "RS256", Use: "sig",
		}}}
		_ = json.NewEncoder(w).Encode(jwks)
	})
	mux.HandleFunc("/token", func(w http.ResponseWriter, r *http.Request) {
		raw, err := fi.signer.Sign(mustJSON(fi.nextClaims))
		if err != nil {
			http.Error(w, err.Error(), 500)
			return
		}
		idToken, _ := raw.CompactSerialize()
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"access_token": "at", "token_type": "Bearer", "id_token": idToken,
		})
	})
	fi.srv = httptest.NewServer(mux)
	return fi
}

func mustJSON(v any) []byte { b, _ := json.Marshal(v); return b }

func (fi *fakeIssuer) close() { fi.srv.Close() }

// standardClaims builds a valid id_token payload; callers override fields.
func (fi *fakeIssuer) standardClaims(nonce string) map[string]any {
	now := time.Now()
	return map[string]any{
		"iss":   fi.srv.URL,
		"aud":   fi.clientID,
		"sub":   "user-123",
		"email": "diego@example.org",
		"role":  "admin",
		"nonce": nonce,
		"exp":   now.Add(time.Hour).Unix(),
		"iat":   now.Unix(),
	}
}

// federatedAuth wires an Auth against the fake issuer, mirroring FromEnv's
// federated branch but without env plumbing.
func (fi *fakeIssuer) federatedAuth(t *testing.T) *Auth {
	t.Helper()
	provider, err := oidc.NewProvider(context.Background(), fi.srv.URL)
	if err != nil {
		t.Fatal(err)
	}
	return &Auth{
		enabled: true, federated: true, secret: []byte("test-secret"),
		oauth2: oauth2.Config{
			ClientID: fi.clientID, RedirectURL: "http://localhost/auth/callback",
			Endpoint: provider.Endpoint(), Scopes: []string{oidc.ScopeOpenID, "email"},
		},
		verifier: provider.Verifier(&oidc.Config{ClientID: fi.clientID}),
		flows:    map[string]flow{},
	}
}

// seedFlow registers a pending login so the callback's state lookup succeeds.
func (a *Auth) seedFlow(state, verifier, nonce string) {
	a.flows[state] = flow{verifier: verifier, nonce: nonce, exp: time.Now().Add(10 * time.Minute)}
}

func doCallback(a *Auth, state string) *httptest.ResponseRecorder {
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/auth/callback?"+url.Values{
		"state": {state}, "code": {"the-code"},
	}.Encode(), nil)
	a.handleCallback(rec, req)
	return rec
}

func TestOIDCCallbackRejectsUnknownState(t *testing.T) {
	fi := newFakeIssuer(t, "searchgirl")
	defer fi.close()
	a := fi.federatedAuth(t)
	// Sin seedFlow: el state no existe → 400 antes de cualquier red.
	rec := doCallback(a, "no-existe")
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("state desconocido = %d, want 400", rec.Code)
	}
	if len(rec.Result().Cookies()) != 0 {
		t.Fatal("no debe emitir sesión con state inválido")
	}
}

func TestOIDCCallbackRejectsExpiredState(t *testing.T) {
	fi := newFakeIssuer(t, "searchgirl")
	defer fi.close()
	a := fi.federatedAuth(t)
	a.flows["s1"] = flow{verifier: "v", nonce: "n", exp: time.Now().Add(-time.Minute)} // expirado
	rec := doCallback(a, "s1")
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("state expirado = %d, want 400", rec.Code)
	}
}

func TestOIDCCallbackRejectsNonceMismatch(t *testing.T) {
	fi := newFakeIssuer(t, "searchgirl")
	defer fi.close()
	a := fi.federatedAuth(t)
	a.seedFlow("s1", oauth2.GenerateVerifier(), "nonce-esperado")
	fi.nextClaims = fi.standardClaims("nonce-ATACANTE") // el token trae otro nonce
	rec := doCallback(a, "s1")
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("nonce mismatch = %d, want 401", rec.Code)
	}
	if len(rec.Result().Cookies()) != 0 {
		t.Fatal("nonce mismatch no debe emitir sesión (anti-replay)")
	}
}

func TestOIDCCallbackRejectsWrongAudience(t *testing.T) {
	fi := newFakeIssuer(t, "searchgirl")
	defer fi.close()
	a := fi.federatedAuth(t)
	a.seedFlow("s1", oauth2.GenerateVerifier(), "n")
	claims := fi.standardClaims("n")
	claims["aud"] = "otra-app" // token emitido para otro cliente
	fi.nextClaims = claims
	rec := doCallback(a, "s1")
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("audiencia incorrecta = %d, want 401", rec.Code)
	}
}

func TestOIDCCallbackHappyPathSetsSession(t *testing.T) {
	fi := newFakeIssuer(t, "searchgirl")
	defer fi.close()
	a := fi.federatedAuth(t)
	a.seedFlow("s1", oauth2.GenerateVerifier(), "n")
	fi.nextClaims = fi.standardClaims("n")
	rec := doCallback(a, "s1")
	if rec.Code != http.StatusFound {
		t.Fatalf("login válido = %d, want 302", rec.Code)
	}
	cookies := rec.Result().Cookies()
	if len(cookies) == 0 {
		t.Fatal("login válido debe emitir cookie de sesión")
	}
	// La sesión emitida debe validar y traer el email/role del token.
	r := httptest.NewRequest(http.MethodGet, "/", nil)
	r.AddCookie(cookies[0])
	s := a.session(r)
	if s == nil || s.Email != "diego@example.org" || s.Role != "admin" {
		t.Fatalf("sesión = %+v", s)
	}
	// El state ya se consumió: reusarlo (replay) falla.
	rec2 := doCallback(a, "s1")
	if rec2.Code != http.StatusBadRequest {
		t.Fatalf("replay del state = %d, want 400", rec2.Code)
	}
}
