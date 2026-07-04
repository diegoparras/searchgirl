package auth

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func okHandler() http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { _, _ = w.Write([]byte("ok")) })
}

func TestDisabledGatesNothing(t *testing.T) {
	a := Disabled()
	srv := httptest.NewServer(a.Gate(okHandler()))
	defer srv.Close()
	for _, path := range []string{"/api/search?q=x", "/mcp", "/thumb", "/"} {
		resp, err := http.Get(srv.URL + path)
		if err != nil {
			t.Fatal(err)
		}
		resp.Body.Close()
		if resp.StatusCode != http.StatusOK {
			t.Errorf("%s = %d in standalone, want 200", path, resp.StatusCode)
		}
	}
}

func TestTokenGate(t *testing.T) {
	a := &Auth{enabled: true, tokens: parseTokens("secreto")}
	srv := httptest.NewServer(a.Gate(okHandler()))
	defer srv.Close()

	// Sin token: 401 en rutas protegidas, 200 en el shell.
	for path, want := range map[string]int{"/api/search": 401, "/mcp": 401, "/thumb": 401, "/": 200, "/app.css": 200, "/healthz": 200} {
		resp, _ := http.Get(srv.URL + path)
		resp.Body.Close()
		if resp.StatusCode != want {
			t.Errorf("GET %s = %d, want %d", path, resp.StatusCode, want)
		}
	}

	// Con el Bearer correcto: pasa.
	req, _ := http.NewRequest(http.MethodGet, srv.URL+"/api/search", nil)
	req.Header.Set("Authorization", "Bearer secreto")
	resp, _ := http.DefaultClient.Do(req)
	resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Errorf("bearer correcto = %d, want 200", resp.StatusCode)
	}

	// Con un Bearer incorrecto: 401.
	req.Header.Set("Authorization", "Bearer otro")
	resp, _ = http.DefaultClient.Do(req)
	resp.Body.Close()
	if resp.StatusCode != http.StatusUnauthorized {
		t.Errorf("bearer incorrecto = %d, want 401", resp.StatusCode)
	}
}

func TestSessionCookieRoundTripAndTamper(t *testing.T) {
	a := &Auth{enabled: true, federated: true, secret: []byte("k"), flows: map[string]flow{}}

	rec := httptest.NewRecorder()
	a.setSession(rec, session{Email: "diego@example.org", Role: "admin", Exp: time.Now().Add(time.Hour).UnixMilli()})
	cookie := rec.Result().Cookies()[0]

	r := httptest.NewRequest(http.MethodGet, "/", nil)
	r.AddCookie(cookie)
	if s := a.session(r); s == nil || s.Email != "diego@example.org" {
		t.Fatalf("session roundtrip failed: %+v", s)
	}

	// Manipular el payload debe invalidar la firma.
	parts := strings.SplitN(cookie.Value, ".", 2)
	tampered := &http.Cookie{Name: cookie.Name, Value: parts[0] + "x." + parts[1]}
	r2 := httptest.NewRequest(http.MethodGet, "/", nil)
	r2.AddCookie(tampered)
	if a.session(r2) != nil {
		t.Fatal("tampered cookie must not validate")
	}

	// Expirada: inválida.
	rec2 := httptest.NewRecorder()
	a.setSession(rec2, session{Email: "x@x", Exp: time.Now().Add(-time.Minute).UnixMilli()})
	r3 := httptest.NewRequest(http.MethodGet, "/", nil)
	r3.AddCookie(rec2.Result().Cookies()[0])
	if a.session(r3) != nil {
		t.Fatal("expired session must not validate")
	}
}

func TestLocalLogin(t *testing.T) {
	t.Setenv("AUTH_MODE", "")
	t.Setenv("SEARCHGIRL_USER", "diego")
	t.Setenv("SEARCHGIRL_PASS", "clave-larga-de-prueba")
	t.Setenv("SEARCHGIRL_MCP_TOKEN", "")
	t.Setenv("SECRET_KEY", "k")
	a, err := FromEnv(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	if a.Mode() != "local" || !a.Enabled() {
		t.Fatalf("mode = %q", a.Mode())
	}

	mux := http.NewServeMux()
	mux.HandleFunc("/api/ping", func(w http.ResponseWriter, r *http.Request) { _, _ = w.Write([]byte("pong")) })
	a.RegisterRoutes(mux)
	srv := httptest.NewServer(a.Gate(mux))
	defer srv.Close()

	// Sin sesión: la API está gateada.
	resp, err := http.Get(srv.URL + "/api/ping")
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("api sin sesión = %d, want 401", resp.StatusCode)
	}

	// Credenciales malas: 401.
	resp, err = http.Post(srv.URL+"/auth/login", "application/json", strings.NewReader(`{"user":"diego","password":"otra"}`))
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("login malo = %d, want 401", resp.StatusCode)
	}

	// Credenciales correctas: cookie de sesión y API accesible.
	resp, err = http.Post(srv.URL+"/auth/login", "application/json", strings.NewReader(`{"user":"diego","password":"clave-larga-de-prueba"}`))
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusNoContent || len(resp.Cookies()) == 0 {
		t.Fatalf("login ok = %d cookies=%d", resp.StatusCode, len(resp.Cookies()))
	}
	req, _ := http.NewRequest(http.MethodGet, srv.URL+"/api/ping", nil)
	req.AddCookie(resp.Cookies()[0])
	resp2, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	resp2.Body.Close()
	if resp2.StatusCode != http.StatusOK {
		t.Fatalf("api con sesión = %d, want 200", resp2.StatusCode)
	}
}

func TestFederatedIgnoresLocalCreds(t *testing.T) {
	// El contrato: en federado no hay login local ni aunque estén las vars.
	t.Setenv("AUTH_MODE", "federado")
	t.Setenv("SEARCHGIRL_USER", "diego")
	t.Setenv("SEARCHGIRL_PASS", "x")
	t.Setenv("LOCKATUS_ISSUER", "")
	if _, err := FromEnv(t.Context()); err == nil {
		t.Fatal("federado sin LOCKATUS_* debe fallar, no caer al login local")
	}
}

func TestMeReportsMode(t *testing.T) {
	a := &Auth{enabled: true, tokens: parseTokens("t")}
	rec := httptest.NewRecorder()
	a.handleMe(rec, httptest.NewRequest(http.MethodGet, "/auth/me", nil))
	body := rec.Body.String()
	if !strings.Contains(body, `"mode":"token"`) || !strings.Contains(body, `"authenticated":false`) {
		t.Fatalf("me = %s", body)
	}
}

func TestMultiTokenNamedAndRevocable(t *testing.T) {
	a := &Auth{enabled: true, tokens: parseTokens("claude:abc123, n8n:def456, suelto789")}
	srv := httptest.NewServer(a.Gate(okHandler()))
	defer srv.Close()

	try := func(token string) (int, string) {
		req, _ := http.NewRequest(http.MethodGet, srv.URL+"/api/x", nil)
		if token != "" {
			req.Header.Set("Authorization", "Bearer "+token)
		}
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		defer resp.Body.Close()
		// nombre del token vía /auth/me
		reqMe, _ := http.NewRequest(http.MethodGet, srv.URL+"/auth/me", nil)
		if token != "" {
			reqMe.Header.Set("Authorization", "Bearer "+token)
		}
		rec := httptest.NewRecorder()
		a.handleMe(rec, reqMe)
		return resp.StatusCode, rec.Body.String()
	}

	if code, me := try("abc123"); code != 200 || !strings.Contains(me, `"token_name":"claude"`) {
		t.Fatalf("token claude: code=%d me=%s", code, me)
	}
	if code, me := try("def456"); code != 200 || !strings.Contains(me, `"token_name":"n8n"`) {
		t.Fatalf("token n8n: code=%d me=%s", code, me)
	}
	if code, me := try("suelto789"); code != 200 || !strings.Contains(me, `"token_name":"token3"`) {
		t.Fatalf("token sin nombre: code=%d me=%s", code, me)
	}
	if code, _ := try("abc124"); code != 401 {
		t.Fatalf("token inválido: code=%d, want 401", code)
	}

	// Revocación: sacar "n8n" del .env y el resto sigue andando.
	a.tokens = parseTokens("claude:abc123, suelto789")
	if code, _ := try("def456"); code != 401 {
		t.Fatalf("token revocado debe dar 401, dio %d", code)
	}
	if code, _ := try("abc123"); code != 200 {
		t.Fatalf("token vigente tras revocar otro: %d", code)
	}
}
