package search

import (
	"context"
	"net/http"
	"net/http/httptest"
	"reflect"
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
