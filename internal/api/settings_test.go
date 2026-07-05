package api

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/diegoparras/searchgirl/internal/llm"
	"github.com/diegoparras/searchgirl/internal/search"
)

func newSettingsAPI(t *testing.T, admin bool) (*httptest.Server, *llm.Store) {
	t.Helper()
	store := llm.NewStore(t.TempDir(), llm.Noop{})
	srv := &Server{Search: &search.Service{DefaultLanguage: "es"}, Store: store, Version: "test"}
	srv.IsAdmin = func(*http.Request) bool { return admin }
	mux := http.NewServeMux()
	srv.Mount(mux)
	app := httptest.NewServer(mux)
	t.Cleanup(app.Close)
	return app, store
}

func TestSettingsGatedToAdmin(t *testing.T) {
	app, _ := newSettingsAPI(t, false) // no-admin
	for _, tc := range []struct {
		method, path, body string
	}{
		{"GET", "/api/settings", ""},
		{"POST", "/api/settings", `{"base_url":"http://x/v1","model":"m"}`},
		{"POST", "/api/settings/test", ""},
		{"POST", "/api/settings/models", `{"base_url":"http://x/v1"}`},
	} {
		req, _ := http.NewRequest(tc.method, app.URL+tc.path, strings.NewReader(tc.body))
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		resp.Body.Close()
		if resp.StatusCode != http.StatusForbidden {
			t.Errorf("%s %s sin admin = %d, want 403", tc.method, tc.path, resp.StatusCode)
		}
	}
}

func TestSettingsSaveAndSnapshot(t *testing.T) {
	app, store := newSettingsAPI(t, true)

	// POST guarda; la respuesta es el snapshot (sin key).
	resp, err := http.Post(app.URL+"/api/settings", "application/json",
		strings.NewReader(`{"base_url":"http://ollama:11434/v1","model":"qwen2.5:7b","api_key":"secreto"}`))
	if err != nil {
		t.Fatal(err)
	}
	var snap map[string]any
	_ = json.NewDecoder(resp.Body).Decode(&snap)
	resp.Body.Close()
	if snap["model"] != "qwen2.5:7b" || snap["has_key"] != true {
		t.Fatalf("snapshot = %v", snap)
	}
	if b, _ := json.Marshal(snap); strings.Contains(string(b), "secreto") {
		t.Errorf("el snapshot filtró la API key: %s", b)
	}
	if !store.Available() {
		t.Error("el store debe quedar configurado tras el POST")
	}

	// GET devuelve lo mismo, sin key.
	g, _ := http.Get(app.URL + "/api/settings")
	buf, _ := io.ReadAll(g.Body)
	g.Body.Close()
	if strings.Contains(string(buf), "secreto") {
		t.Errorf("GET /api/settings filtró la key: %s", buf)
	}
}

func TestSettingsModels(t *testing.T) {
	// Backend fake que actúa como endpoint OpenAI-compatible.
	backend := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/models" {
			_, _ = w.Write([]byte(`{"data":[{"id":"qwen2.5:7b"},{"id":"nomic-embed-text"}]}`))
			return
		}
		http.NotFound(w, r)
	}))
	defer backend.Close()

	app, _ := newSettingsAPI(t, true)
	resp, err := http.Post(app.URL+"/api/settings/models", "application/json",
		strings.NewReader(`{"base_url":"`+backend.URL+`"}`))
	if err != nil {
		t.Fatal(err)
	}
	var out struct {
		OK     bool `json:"ok"`
		Models []struct {
			ID          string `json:"id"`
			Recommended bool   `json:"recommended"`
		} `json:"models"`
	}
	_ = json.NewDecoder(resp.Body).Decode(&out)
	resp.Body.Close()
	if !out.OK || len(out.Models) != 2 {
		t.Fatalf("models = %+v", out)
	}
	byID := map[string]bool{}
	for _, m := range out.Models {
		byID[m.ID] = m.Recommended
	}
	if !byID["qwen2.5:7b"] {
		t.Error("qwen2.5:7b debería estar recomendado")
	}
	if byID["nomic-embed-text"] {
		t.Error("un modelo de embeddings NO debe recomendarse")
	}
}

func TestRecommendModel(t *testing.T) {
	yes := []string{"qwen2.5:7b", "deepseek-chat", "llama-3.1-8b-instruct", "gemma2:9b", "claude-haiku-4-5", "gpt-4o-mini", "mistral-small"}
	no := []string{"nomic-embed-text", "whisper-large", "qwen2.5-vl:7b", "clip-vit", "bge-reranker", "text-embedding-3-small"}
	for _, m := range yes {
		if !recommendModel(m) {
			t.Errorf("%q debería recomendarse", m)
		}
	}
	for _, m := range no {
		if recommendModel(m) {
			t.Errorf("%q NO debería recomendarse", m)
		}
	}
}
