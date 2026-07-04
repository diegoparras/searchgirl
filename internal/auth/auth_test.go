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
	a := &Auth{enabled: true, token: "secreto"}
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

func TestMeReportsMode(t *testing.T) {
	a := &Auth{enabled: true, token: "t"}
	rec := httptest.NewRecorder()
	a.handleMe(rec, httptest.NewRequest(http.MethodGet, "/auth/me", nil))
	body := rec.Body.String()
	if !strings.Contains(body, `"mode":"token"`) || !strings.Contains(body, `"authenticated":false`) {
		t.Fatalf("me = %s", body)
	}
}
