package api

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/diegoparras/searchgirl/internal/search"
	"github.com/diegoparras/searchgirl/internal/searx"
)

// fakeSearx stands in for the SearXNG container.
func fakeSearx(t *testing.T) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/search":
			_, _ = w.Write([]byte(`{"query": "q", "results": [
				{"title": "T", "url": "https://example.org/", "content": "c", "engine": "ddg", "score": 1}
			], "answers": [], "corrections": [], "infoboxes": [], "suggestions": [], "unresponsive_engines": []}`))
		case "/autocompleter":
			_, _ = w.Write([]byte(`["q", ["qa", "qb"]]`))
		case "/healthz":
			_, _ = w.Write([]byte("OK"))
		default:
			http.NotFound(w, r)
		}
	}))
}

func newAPI(t *testing.T) (*httptest.Server, func()) {
	sx := fakeSearx(t)
	svc := &search.Service{Client: searx.New(sx.URL, time.Second), DefaultLanguage: "es"}
	mux := http.NewServeMux()
	New(svc, "9.9.9").Mount(mux)
	app := httptest.NewServer(mux)
	return app, func() { app.Close(); sx.Close() }
}

func TestSearchRequiresQ(t *testing.T) {
	app, done := newAPI(t)
	defer done()
	resp, err := http.Get(app.URL + "/api/search")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400", resp.StatusCode)
	}
	var e map[string]string
	_ = json.NewDecoder(resp.Body).Decode(&e)
	if e["error"] == "" {
		t.Fatal("error body missing")
	}
}

func TestSearchBadCategoryIs400(t *testing.T) {
	app, done := newAPI(t)
	defer done()
	resp, err := http.Get(app.URL + "/api/search?q=x&category=nope")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400", resp.StatusCode)
	}
}

func TestSearchOK(t *testing.T) {
	app, done := newAPI(t)
	defer done()
	resp, err := http.Get(app.URL + "/api/search?q=hola")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d", resp.StatusCode)
	}
	var out searx.Response
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		t.Fatal(err)
	}
	if len(out.Results) != 1 || out.Results[0].Domain != "example.org" {
		t.Fatalf("results = %+v", out.Results)
	}
	if out.Language != "es" {
		t.Errorf("default language not applied: %q", out.Language)
	}
}

func TestConfigShape(t *testing.T) {
	app, done := newAPI(t)
	defer done()
	resp, err := http.Get(app.URL + "/api/config")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	var out map[string]any
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		t.Fatal(err)
	}
	if out["name"] != "Searchgirl" || out["version"] != "9.9.9" || out["tagline"] != "By SearXNG" {
		t.Fatalf("config = %v", out)
	}
	if out["auth_mode"] != "standalone" {
		t.Errorf("auth_mode default = %v", out["auth_mode"])
	}
	llm, _ := out["llm"].(map[string]any)
	if llm == nil || llm["available"] != false {
		t.Errorf("llm block = %v", out["llm"])
	}
}
