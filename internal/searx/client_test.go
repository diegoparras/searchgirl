package searx

import (
	"context"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"
)

// fixture mimics a real SearXNG format=json payload: mixed engine/engines
// fields, a duplicate URL from another engine, null publishedDate, tuple-style
// unresponsive_engines and object-style answers (current SearXNG).
const fixture = `{
  "query": "gophers",
  "number_of_results": 0,
  "results": [
    {
      "title": "Go (programming language) - Wikipedia",
      "url": "https://en.wikipedia.org/wiki/Go_(programming_language)",
      "content": "Go is a statically typed, compiled high-level programming language.",
      "engine": "wikipedia",
      "engines": ["wikipedia", "duckduckgo"],
      "score": 8.5,
      "category": "general",
      "publishedDate": null
    },
    {
      "title": "The Go Programming Language",
      "url": "https://go.dev/",
      "content": "Build simple, secure, scalable systems with Go.",
      "engine": "duckduckgo",
      "engines": ["duckduckgo"],
      "score": 6.0,
      "category": "general",
      "publishedDate": "2026-05-01T10:30:00"
    },
    {
      "title": "The Go Programming Language (mirror)",
      "url": "https://go.dev/#main",
      "content": "",
      "engine": "brave",
      "score": 3.0,
      "category": "general"
    }
  ],
  "answers": [{"answer": "Go is a programming language designed at Google.", "engine": "duckduckgo"}],
  "corrections": [],
  "infoboxes": [
    {
      "infobox": "Go",
      "id": "https://en.wikipedia.org/wiki/Go_(programming_language)",
      "content": "Statically typed compiled language.",
      "img_src": "",
      "urls": [{"title": "Official site", "url": "https://go.dev/"}],
      "engine": "wikipedia",
      "attributes": [{"label": "Designed by", "value": "Griesemer, Pike, Thompson"}]
    }
  ],
  "suggestions": ["golang tutorial", "go generics"],
  "unresponsive_engines": [["qwant", "too many requests"], ["startpage", "timeout"]]
}`

func newTestServer(t *testing.T, wantQuery url.Values, status int, body string) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/search" {
			got := r.URL.Query()
			for k := range wantQuery {
				if got.Get(k) != wantQuery.Get(k) {
					t.Errorf("query param %s = %q, want %q", k, got.Get(k), wantQuery.Get(k))
				}
			}
			w.WriteHeader(status)
			_, _ = w.Write([]byte(body))
			return
		}
		http.NotFound(w, r)
	}))
}

func TestSearchNormalizes(t *testing.T) {
	want := url.Values{}
	want.Set("q", "gophers")
	want.Set("format", "json")
	want.Set("categories", "general")
	want.Set("language", "es")
	want.Set("time_range", "month")
	want.Set("safesearch", "1")
	want.Set("pageno", "2")
	ts := newTestServer(t, want, http.StatusOK, fixture)
	defer ts.Close()

	c := New(ts.URL, 5*time.Second)
	resp, err := c.Search(context.Background(), Params{
		Query: "gophers", Category: "general", Language: "es",
		TimeRange: "month", SafeSearch: 1, Page: 2,
	})
	if err != nil {
		t.Fatal(err)
	}

	// go.dev y go.dev/#main se fusionan (fragment removido) => 2 resultados.
	if len(resp.Results) != 2 {
		t.Fatalf("results = %d, want 2 (dedup by canonical URL)", len(resp.Results))
	}
	// Orden por score: wikipedia (8.5) primero.
	if resp.Results[0].Domain != "en.wikipedia.org" || resp.Results[0].Rank != 1 {
		t.Errorf("rank 1 = %s (%d)", resp.Results[0].Domain, resp.Results[0].Rank)
	}
	godev := resp.Results[1]
	if godev.Domain != "go.dev" {
		t.Fatalf("rank 2 domain = %q", godev.Domain)
	}
	// Engines fusionados de las dos entradas duplicadas.
	if len(godev.Engines) != 2 { // brave + duckduckgo
		t.Errorf("merged engines = %v", godev.Engines)
	}
	if godev.Published != "2026-05-01" {
		t.Errorf("published = %q, want 2026-05-01", godev.Published)
	}
	if resp.Results[0].Published != "" {
		t.Errorf("null publishedDate should stay empty, got %q", resp.Results[0].Published)
	}
	if len(resp.Answers) != 1 || resp.Answers[0] == "" {
		t.Errorf("object-style answer not decoded: %v", resp.Answers)
	}
	if len(resp.Meta.EnginesFailed) != 2 || resp.Meta.EnginesFailed[0] != "qwant" {
		t.Errorf("engines_failed = %v", resp.Meta.EnginesFailed)
	}
	if len(resp.Infoboxes) != 1 || resp.Infoboxes[0].Title != "Go" || len(resp.Infoboxes[0].Attributes) != 1 {
		t.Errorf("infobox = %+v", resp.Infoboxes)
	}
	if len(resp.Suggestions) != 2 {
		t.Errorf("suggestions = %v", resp.Suggestions)
	}
	if resp.Meta.Total != 2 || resp.Meta.TookMs < 0 {
		t.Errorf("meta = %+v", resp.Meta)
	}
	if resp.Category != "general" || resp.Page != 2 || resp.Language != "es" {
		t.Errorf("echo params: %s/%d/%s", resp.Category, resp.Page, resp.Language)
	}
}

func TestSearch403ExplainsFormats(t *testing.T) {
	ts := newTestServer(t, url.Values{}, http.StatusForbidden, "")
	defer ts.Close()
	c := New(ts.URL, time.Second)
	_, err := c.Search(context.Background(), Params{Query: "x"})
	if err == nil || !strings.Contains(err.Error(), "search.formats") {
		t.Fatalf("403 should mention settings.yml formats, got: %v", err)
	}
}

func TestSearchEmptyQuery(t *testing.T) {
	c := New("http://invalid.test", time.Second)
	if _, err := c.Search(context.Background(), Params{Query: "  "}); err == nil {
		t.Fatal("empty query must fail before any network call")
	}
}

func TestSuggest(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/autocompleter" {
			http.NotFound(w, r)
			return
		}
		if q := r.URL.Query().Get("q"); q != "gol" {
			t.Errorf("q = %q", q)
		}
		_, _ = w.Write([]byte(`["gol", ["golang", "goleador"]]`))
	}))
	defer ts.Close()
	c := New(ts.URL, time.Second)
	got, err := c.Suggest(context.Background(), "gol")
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 2 || got[0] != "golang" {
		t.Fatalf("suggestions = %v", got)
	}
}

func TestEnginesGroupsByCategory(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/config" {
			http.NotFound(w, r)
			return
		}
		_, _ = w.Write([]byte(`{"engines": [
			{"name": "duckduckgo", "categories": ["general"], "shortcut": "ddg", "enabled": true},
			{"name": "brave", "categories": ["general", "news"], "shortcut": "br", "enabled": true},
			{"name": "google", "categories": ["general"], "shortcut": "go", "enabled": false}
		]}`))
	}))
	defer ts.Close()
	c := New(ts.URL, time.Second)
	got, err := c.Engines(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(got["general"]) != 2 {
		t.Errorf("general engines = %v (disabled must be excluded)", got["general"])
	}
	if len(got["news"]) != 1 || got["news"][0].Name != "brave" {
		t.Errorf("news engines = %v", got["news"])
	}
}
