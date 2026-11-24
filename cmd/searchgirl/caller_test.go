package main

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/diegoparras/searchgirl/internal/search"
)

// El middleware de la API deja el nombre del token en el contexto; sin token
// (una sesión de la interfaz) no deja nada.
func TestCallerFromToken(t *testing.T) {
	name := func(h http.Header) (string, bool) {
		if h.Get("Authorization") == "Bearer s3cr3t" {
			return "secretaria", true
		}
		return "", false
	}
	var got string
	h := callerFromToken(name, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		got = search.CallerFrom(r.Context())
	}))

	req := httptest.NewRequest(http.MethodGet, "/api/search?q=x", nil)
	req.Header.Set("Authorization", "Bearer s3cr3t")
	h.ServeHTTP(httptest.NewRecorder(), req)
	if got != "secretaria" {
		t.Errorf("con token: caller = %q, want secretaria", got)
	}

	h.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(http.MethodGet, "/api/search?q=x", nil))
	if got != "" {
		t.Errorf("sin token: caller = %q, want vacío", got)
	}
}
