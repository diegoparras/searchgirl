package mcpsrv

import (
	"context"
	"net/http"
	"testing"

	"github.com/diegoparras/searchgirl/internal/search"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// La herramienta MCP toma el nombre del token de los headers de la llamada y
// lo deja en el contexto que recibe el servicio de búsqueda.
func TestWithCallerFromCallHeaders(t *testing.T) {
	opts := Options{Caller: func(h http.Header) (string, bool) {
		if h.Get("Authorization") == "Bearer s3cr3t" {
			return "secretaria", true
		}
		return "", false
	}}
	call := func(auth string) *mcp.CallToolRequest {
		h := http.Header{}
		if auth != "" {
			h.Set("Authorization", auth)
		}
		return &mcp.CallToolRequest{Extra: &mcp.RequestExtra{Header: h}}
	}

	if got := search.CallerFrom(opts.withCaller(context.Background(), call("Bearer s3cr3t"))); got != "secretaria" {
		t.Errorf("token válido: caller = %q, want secretaria", got)
	}
	if got := search.CallerFrom(opts.withCaller(context.Background(), call("Bearer otro"))); got != "" {
		t.Errorf("token desconocido: caller = %q, want vacío", got)
	}
	// stdio: sin headers ni Caller configurado.
	if got := search.CallerFrom(Options{}.withCaller(context.Background(), &mcp.CallToolRequest{})); got != "" {
		t.Errorf("stdio: caller = %q, want vacío", got)
	}
	if got := search.CallerFrom(opts.withCaller(context.Background(), nil)); got != "" {
		t.Errorf("sin request: caller = %q, want vacío", got)
	}
}
