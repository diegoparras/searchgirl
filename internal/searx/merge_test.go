package searx

import (
	"context"
	"net/http"
	"net/http/httptest"
	"reflect"
	"testing"
	"time"
)

// Merge hace lo que SearXNG habría hecho con las dos búsquedas en un pedido:
// un resultado repetido queda una vez, con los motores de los dos y la suma
// de puntajes, y el orden vuelve a ser por puntaje.
func TestMergeDedupsAndReranks(t *testing.T) {
	main := &Response{
		Language: "es",
		Results: []Result{
			{Rank: 1, URL: "https://a.example/", Engines: []string{"bing"}, Score: 3},
			{Rank: 2, URL: "https://b.example/", Engines: []string{"bing"}, Score: 2},
		},
		Answers: []string{"uno"},
		Meta:    Meta{EnginesFailed: []string{"yahoo"}, TookMs: 300},
	}
	extra := &Response{
		Results: []Result{
			{Rank: 1, URL: "http://b.example", Engines: []string{"duckduckgo"}, Score: 2, Snippet: "de ddg"},
			{Rank: 2, URL: "https://c.example/", Engines: []string{"duckduckgo"}, Score: 1},
		},
		Answers: []string{"uno", "dos"},
		Meta:    Meta{EnginesFailed: []string{"yahoo"}, TookMs: 900},
	}
	got := Merge(main, extra)

	var urls []string
	for _, r := range got.Results {
		urls = append(urls, r.URL)
	}
	if want := []string{"https://b.example/", "https://a.example/", "https://c.example/"}; !reflect.DeepEqual(urls, want) {
		t.Fatalf("orden = %v, want %v", urls, want)
	}
	b := got.Results[0]
	if b.Rank != 1 || b.Score != 4 || !reflect.DeepEqual(b.Engines, []string{"bing", "duckduckgo"}) || b.Snippet != "de ddg" {
		t.Errorf("repetido mal juntado: %+v", b)
	}
	if got.Meta.Total != 3 || got.Meta.TookMs != 900 || got.Language != "es" {
		t.Errorf("meta = %+v language=%q", got.Meta, got.Language)
	}
	if !reflect.DeepEqual(got.Meta.EnginesFailed, []string{"yahoo"}) || !reflect.DeepEqual(got.Answers, []string{"uno", "dos"}) {
		t.Errorf("fallaron=%v answers=%v", got.Meta.EnginesFailed, got.Answers)
	}
	if len(main.Results) != 2 || main.Results[1].Score != 2 {
		t.Error("Merge no debe tocar main")
	}
}

// DisabledEngines viaja como disabled_engines=motor__categoria; sin categoría
// no se manda.
func TestSearchSendsDisabledEngines(t *testing.T) {
	var got []string
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		got = append(got, r.URL.Query().Get("disabled_engines"))
		_, _ = w.Write([]byte(`{"query":"x","results":[]}`))
	}))
	defer ts.Close()
	c := New(ts.URL, 5*time.Second)
	for _, p := range []Params{
		{Query: "x", Category: "general", DisabledEngines: []string{"duckduckgo", "mojeek"}},
		{Query: "x", DisabledEngines: []string{"duckduckgo"}},
	} {
		if _, err := c.Search(context.Background(), p); err != nil {
			t.Fatal(err)
		}
	}
	if want := []string{"duckduckgo__general,mojeek__general", ""}; !reflect.DeepEqual(got, want) {
		t.Errorf("disabled_engines = %q, want %q", got, want)
	}
}
