package search

import (
	"context"
	"net/http"
	"net/http/httptest"
	"reflect"
	"sort"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/diegoparras/searchgirl/internal/cache"
	"github.com/diegoparras/searchgirl/internal/searx"
)

func TestParseEngineTokens(t *testing.T) {
	got := ParseEngineTokens(" SecretarIA:brave1 , secretaria:otro, custodia: ,:huerfano, sinDosPuntos, n8n:x ")
	want := map[string][]string{
		"secretaria": {"brave1", "otro"},
		"n8n":        {"x"},
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("ParseEngineTokens = %v, want %v", got, want)
	}
	if len(ParseEngineTokens("")) != 0 {
		t.Error("vacío debe dar un mapa vacío")
	}
}

// fakeSearx registra el parámetro "tokens" de cada pedido que le llega.
type fakeSearx struct {
	mu     sync.Mutex
	tokens []string
}

func (f *fakeSearx) server(t *testing.T) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		f.mu.Lock()
		f.tokens = append(f.tokens, r.URL.Query().Get("tokens"))
		f.mu.Unlock()
		_, _ = w.Write([]byte(`{"query":"x","results":[]}`))
	}))
}

func (f *fakeSearx) calls() []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]string(nil), f.tokens...)
}

// Sólo el cliente con tokens configurados se los pasa a SearXNG; el resto, la
// interfaz incluida, busca sin motores privados. Y la caché no mezcla: la
// misma búsqueda con y sin tokens son dos pedidos a SearXNG.
func TestSearchPassesTokensOnlyToConfiguredCaller(t *testing.T) {
	f := &fakeSearx{}
	ts := f.server(t)
	defer ts.Close()

	s := &Service{
		Client:       searx.New(ts.URL, 5*time.Second),
		SearchCache:  cache.New(time.Minute, 16),
		EngineTokens: ParseEngineTokens("secretaria:brave-interno"),
	}
	buscar := func(ctx context.Context) {
		t.Helper()
		if _, err := s.Search(ctx, Query{Query: "universidad de buenos aires"}); err != nil {
			t.Fatal(err)
		}
	}

	buscar(WithCaller(context.Background(), "SecretarIA")) // sin distinguir mayúsculas
	buscar(WithCaller(context.Background(), "custodia"))   // no la sirve la caché de SecretarIA
	buscar(context.Background())                           // interfaz: igual que custodia, de la caché
	buscar(WithCaller(context.Background(), "secretaria")) // repetida: de la caché de SecretarIA

	want := []string{"brave-interno", ""}
	if got := f.calls(); !reflect.DeepEqual(got, want) {
		t.Errorf("tokens recibidos por SearXNG = %q, want %q", got, want)
	}
}

// DuckDuckGo pide CAPTCHA si la búsqueda pide una región: se le pregunta
// aparte, sin idioma ni categoría, se lo saca del pedido principal y los
// resultados se juntan. Con language=all va todo en un pedido.
func TestSearchAsksNoLanguageEnginesApart(t *testing.T) {
	var mu sync.Mutex
	var pedidos []string
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/config" {
			_, _ = w.Write([]byte(`{"engines":[
				{"name":"bing","categories":["general","web"],"enabled":true},
				{"name":"duckduckgo","categories":["general","web"],"enabled":true}]}`))
			return
		}
		q := r.URL.Query()
		mu.Lock()
		pedidos = append(pedidos, "categories="+q.Get("categories")+" language="+q.Get("language")+
			" engines="+q.Get("engines")+" disabled="+q.Get("disabled_engines"))
		mu.Unlock()
		if q.Get("engines") == "duckduckgo" {
			_, _ = w.Write([]byte(`{"query":"x","results":[
				{"url":"https://a.example/","title":"A","engines":["duckduckgo"],"score":1},
				{"url":"https://c.example/","title":"C","engines":["duckduckgo"],"score":0.5}]}`))
			return
		}
		_, _ = w.Write([]byte(`{"query":"x","results":[
			{"url":"https://a.example/","title":"A","engines":["bing"],"score":1},
			{"url":"https://b.example/","title":"B","engines":["bing"],"score":0.8}]}`))
	}))
	defer ts.Close()

	var log strings.Builder
	s := &Service{Client: searx.New(ts.URL, 5*time.Second), DefaultLanguage: "es", NoLanguageEngines: []string{"duckduckgo"}, Log: &log}
	resp, err := s.Search(context.Background(), Query{Query: "empanadas salteñas"})
	if err != nil {
		t.Fatal(err)
	}
	mu.Lock()
	got := append([]string(nil), pedidos...)
	pedidos = nil
	mu.Unlock()
	sort.Strings(got)
	want := []string{
		"categories= language=all engines=duckduckgo disabled=",
		"categories=general language=es engines= disabled=duckduckgo__general",
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("pedidos a SearXNG =\n%q\nwant\n%q", got, want)
	}
	if len(resp.Results) != 3 || resp.Results[0].URL != "https://a.example/" || !reflect.DeepEqual(resp.Results[0].Engines, []string{"bing", "duckduckgo"}) || resp.Language != "es" {
		t.Errorf("resultados juntados mal: %+v (language %q)", resp.Results, resp.Language)
	}
	if !strings.Contains(log.String(), "resultados=3 motores=bing:2,duckduckgo:2 fallaron=-") {
		t.Errorf("log = %q", log.String())
	}

	if _, err := s.Search(context.Background(), Query{Query: "empanadas", Language: "all"}); err != nil {
		t.Fatal(err)
	}
	mu.Lock()
	defer mu.Unlock()
	if want := []string{"categories=general language=all engines= disabled="}; !reflect.DeepEqual(pedidos, want) {
		t.Errorf("con language=all, pedidos = %q, want %q", pedidos, want)
	}
}

// Si el pedido aparte falla, DuckDuckGo cuenta como fallado y queda lo del
// principal.
func TestSearchNoLanguageEngineFailureKeepsMain(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.URL.Path == "/config":
			_, _ = w.Write([]byte(`{"engines":[{"name":"duckduckgo","categories":["general"],"enabled":true}]}`))
		case r.URL.Query().Get("engines") == "duckduckgo":
			w.WriteHeader(http.StatusInternalServerError)
		default:
			_, _ = w.Write([]byte(`{"query":"x","results":[{"url":"https://b.example/","title":"B","engines":["bing"]}]}`))
		}
	}))
	defer ts.Close()
	s := &Service{Client: searx.New(ts.URL, 5*time.Second), DefaultLanguage: "es", NoLanguageEngines: []string{"duckduckgo"}}
	resp, err := s.Search(context.Background(), Query{Query: "x"})
	if err != nil {
		t.Fatal(err)
	}
	if len(resp.Results) != 1 || !reflect.DeepEqual(resp.Meta.EnginesFailed, []string{"duckduckgo"}) {
		t.Errorf("results=%+v fallaron=%v", resp.Results, resp.Meta.EnginesFailed)
	}
}

func TestParseEngineList(t *testing.T) {
	for _, c := range []struct {
		in   string
		want []string
	}{
		{"", []string{"duckduckgo"}},
		{"-", nil},
		{" duckduckgo , mojeek,", []string{"duckduckgo", "mojeek"}},
	} {
		if got := ParseEngineList(c.in, "duckduckgo"); !reflect.DeepEqual(got, c.want) {
			t.Errorf("ParseEngineList(%q) = %v, want %v", c.in, got, c.want)
		}
	}
}

// Cada búsqueda que llega a SearXNG deja una línea con el cliente, si fue con
// motores privados, qué motores respondieron y cuáles fallaron; las que salen
// de la caché no. La consulta nunca aparece en el log.
func TestSearchLogsEnginesPerCaller(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"query":"x","results":[
			{"url":"https://a.example/","title":"A","engines":["braveapi","duckduckgo"]},
			{"url":"https://b.example/","title":"B","engines":["braveapi"]}
		],"unresponsive_engines":[["bing","CAPTCHA"]]}`))
	}))
	defer ts.Close()

	var log strings.Builder
	s := &Service{
		Client:       searx.New(ts.URL, 5*time.Second),
		SearchCache:  cache.New(time.Minute, 16),
		EngineTokens: ParseEngineTokens("secretaria:brave-interno"),
		Log:          &log,
	}
	for _, ctx := range []context.Context{
		WithCaller(context.Background(), "secretaria"),
		WithCaller(context.Background(), "secretaria"), // de la caché: sin línea
		context.Background(),
	} {
		if _, err := s.Search(ctx, Query{Query: "consulta privada"}); err != nil {
			t.Fatal(err)
		}
	}

	want := "searchgirl: busqueda cliente=secretaria privados=si categoria=general pagina=1 resultados=2 motores=braveapi:2,duckduckgo:1 fallaron=bing\n" +
		"searchgirl: busqueda cliente=- privados=no categoria=general pagina=1 resultados=2 motores=braveapi:2,duckduckgo:1 fallaron=bing\n"
	if got := log.String(); got != want {
		t.Errorf("log =\n%s\nwant\n%s", got, want)
	}
	if strings.Contains(log.String(), "consulta privada") {
		t.Error("la consulta no debe aparecer en el log")
	}
}
