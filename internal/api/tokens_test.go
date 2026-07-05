package api

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/diegoparras/searchgirl/internal/search"
	"github.com/diegoparras/searchgirl/internal/tokens"
)

func newTokensAPI(t *testing.T, admin bool) (*httptest.Server, *tokens.Store) {
	t.Helper()
	store := tokens.New(t.TempDir())
	srv := &Server{Search: &search.Service{DefaultLanguage: "es"}, Tokens: store, Version: "test"}
	srv.IsAdmin = func(*http.Request) bool { return admin }
	mux := http.NewServeMux()
	srv.Mount(mux)
	app := httptest.NewServer(mux)
	t.Cleanup(app.Close)
	return app, store
}

func TestTokensGatedToAdmin(t *testing.T) {
	app, _ := newTokensAPI(t, false)
	for _, m := range []string{"GET", "POST", "DELETE"} {
		req, _ := http.NewRequest(m, app.URL+"/api/tokens", strings.NewReader(`{"label":"x"}`))
		resp, _ := http.DefaultClient.Do(req)
		resp.Body.Close()
		if resp.StatusCode != http.StatusForbidden {
			t.Errorf("%s /api/tokens sin admin = %d, want 403", m, resp.StatusCode)
		}
	}
}

func TestTokensIssueListRevoke(t *testing.T) {
	app, store := newTokensAPI(t, true)

	// POST emite: la respuesta trae el secreto UNA vez.
	resp, _ := http.Post(app.URL+"/api/tokens", "application/json", strings.NewReader(`{"label":"Claude Code","expires_days":0}`))
	var issued struct {
		OK    bool   `json:"ok"`
		Token string `json:"token"`
		Item  struct {
			ID    string `json:"id"`
			Label string `json:"label"`
		} `json:"item"`
	}
	_ = json.NewDecoder(resp.Body).Decode(&issued)
	resp.Body.Close()
	if !issued.OK || !strings.HasPrefix(issued.Token, "sg_") || issued.Item.Label != "Claude Code" {
		t.Fatalf("issue = %+v", issued)
	}

	// El token emitido verifica en el store.
	if _, ok := store.Verify(issued.Token); !ok {
		t.Error("el token emitido debe verificar")
	}

	// GET lista sin el secreto.
	g, _ := http.Get(app.URL + "/api/tokens")
	var listed struct {
		Tokens []map[string]any `json:"tokens"`
	}
	body := new(strings.Builder)
	_ = json.NewDecoder(io.TeeReader(g.Body, body)).Decode(&listed)
	g.Body.Close()
	if len(listed.Tokens) != 1 {
		t.Fatalf("list = %d", len(listed.Tokens))
	}
	if strings.Contains(body.String(), issued.Token) {
		t.Errorf("GET /api/tokens filtró el secreto: %s", body)
	}

	// DELETE revoca.
	req, _ := http.NewRequest(http.MethodDelete, app.URL+"/api/tokens?id="+issued.Item.ID, nil)
	d, _ := http.DefaultClient.Do(req)
	d.Body.Close()
	if d.StatusCode != http.StatusOK {
		t.Fatalf("delete = %d", d.StatusCode)
	}
	if _, ok := store.Verify(issued.Token); ok {
		t.Error("un token revocado no debe verificar")
	}
}
