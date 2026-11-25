// Package search is the orchestration layer: it applies Searchgirl's
// defaults and validation on top of the raw searx client. The API, the MCP
// tools and (later) the answer pipeline all go through here, so behavior is
// identical no matter which face made the request.
package search

import (
	"context"
	"fmt"
	"io"
	"os"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/diegoparras/searchgirl/internal/cache"
	"github.com/diegoparras/searchgirl/internal/searx"
)

type Service struct {
	Client          *searx.Client
	DefaultLanguage string
	DefaultSafe     int

	// Caches opcionales (nil = sin caché): búsquedas con TTL corto, catálogo
	// de engines con TTL largo.
	SearchCache  *cache.Cache
	EnginesCache *cache.Cache

	// EngineTokens habilita motores privados de SearXNG por cliente: nombre
	// del token Bearer (en minúsculas) -> tokens que se le pasan a SearXNG.
	// Quien no figura busca sin motores privados.
	EngineTokens map[string][]string

	// Log recibe una línea por cada búsqueda que llega a SearXNG (no las que
	// salen de la caché): quién buscó, si fue con motores privados, qué
	// motores trajeron resultados y cuáles fallaron. Nunca la consulta en sí.
	// nil = sin log.
	Log io.Writer

	// NoLanguageEngines son motores que se consultan sin idioma, en un pedido
	// aparte a la vez que el principal, y se sacan del principal. DuckDuckGo
	// pide CAPTCHA cuando la búsqueda pide una región (es -> es-es) y responde
	// bien sin ella. Sólo aplica si el motor está en la categoría (o se pidió
	// por engines) y la búsqueda tiene idioma; nil = nunca.
	NoLanguageEngines []string
}

// FromEnv builds the service the way the container runs it:
//
//	SEARXNG_URL                  base URL of the internal SearXNG (default http://searxng:8080)
//	SEARXNG_TIMEOUT              per-search timeout (default 10s)
//	SEARCHGIRL_DEFAULT_LANGUAGE  default language (default es)
//	SEARCHGIRL_SAFESEARCH        default safesearch 0|1|2 (default 0)
//	SEARCHGIRL_SEARXNG_TOKENS    motores privados por cliente: nombre:token,...
//	SEARCHGIRL_SIN_IDIOMA        motores que se consultan sin idioma (default duckduckgo; "-" = ninguno)
func FromEnv() *Service {
	base := os.Getenv("SEARXNG_URL")
	if base == "" {
		base = "http://searxng:8080"
	}
	timeout := 10 * time.Second
	if t := os.Getenv("SEARXNG_TIMEOUT"); t != "" {
		if d, err := time.ParseDuration(t); err == nil && d > 0 {
			timeout = d
		}
	}
	lang := os.Getenv("SEARCHGIRL_DEFAULT_LANGUAGE")
	if lang == "" {
		lang = "es"
	}
	safe := 0
	if s := os.Getenv("SEARCHGIRL_SAFESEARCH"); s != "" {
		if n, err := strconv.Atoi(s); err == nil && n >= 0 && n <= 2 {
			safe = n
		}
	}
	cacheTTL := 5 * time.Minute
	if t := os.Getenv("SEARCHGIRL_CACHE_TTL"); t != "" {
		if d, err := time.ParseDuration(t); err == nil {
			cacheTTL = d // 0 desactiva
		}
	}
	cacheMax := 512
	if m := os.Getenv("SEARCHGIRL_CACHE_MAX"); m != "" {
		if n, err := strconv.Atoi(m); err == nil && n > 0 {
			cacheMax = n
		}
	}
	return &Service{
		Client:          searx.New(base, timeout),
		DefaultLanguage: lang,
		DefaultSafe:     safe,
		SearchCache:     cache.New(cacheTTL, cacheMax),
		EnginesCache:    cache.New(time.Hour, 4),
		EngineTokens:    ParseEngineTokens(os.Getenv("SEARCHGIRL_SEARXNG_TOKENS")),
		Log:             os.Stderr,

		NoLanguageEngines: ParseEngineList(os.Getenv("SEARCHGIRL_SIN_IDIOMA"), "duckduckgo"),
	}
}

// ParseEngineList lee una lista de motores separados por comas. Vacía da def;
// "-" da ninguno.
func ParseEngineList(s, def string) []string {
	s = strings.TrimSpace(s)
	if s == "" {
		s = def
	}
	if s == "-" {
		return nil
	}
	var out []string
	for _, e := range strings.Split(s, ",") {
		if e = strings.TrimSpace(e); e != "" {
			out = append(out, e)
		}
	}
	return out
}

// Query is a search request before defaults. Zero values mean "use default".
type Query struct {
	Query      string
	Category   string
	Language   string
	TimeRange  string
	SafeSearch *int // nil = default
	Page       int
	Engines    []string
}

func (s *Service) Search(ctx context.Context, q Query) (*searx.Response, error) {
	if strings.TrimSpace(q.Query) == "" {
		return nil, fmt.Errorf("empty query")
	}
	if q.Category == "" {
		q.Category = "general"
	}
	if !searx.ValidCategory(q.Category) {
		return nil, fmt.Errorf("unknown category %q (valid: %s)", q.Category, strings.Join(searx.Categories, ", "))
	}
	if !searx.ValidTimeRange(q.TimeRange) {
		return nil, fmt.Errorf("unknown time_range %q (valid: %s)", q.TimeRange, strings.Join(searx.TimeRanges, ", "))
	}
	if q.Language == "" {
		q.Language = s.DefaultLanguage
	}
	safe := s.DefaultSafe
	if q.SafeSearch != nil {
		if *q.SafeSearch < 0 || *q.SafeSearch > 2 {
			return nil, fmt.Errorf("safesearch must be 0, 1 or 2")
		}
		safe = *q.SafeSearch
	}
	if q.Page < 1 {
		q.Page = 1
	}
	params := searx.Params{
		Query:      strings.TrimSpace(q.Query),
		Category:   q.Category,
		Language:   q.Language,
		TimeRange:  q.TimeRange,
		SafeSearch: safe,
		Page:       q.Page,
		Engines:    q.Engines,
		Tokens:     s.EngineTokens[strings.ToLower(CallerFrom(ctx))],
	}
	// Los tokens van en la clave: un resultado con motores privados no se le
	// sirve desde la caché a quien no los tiene, ni al revés.
	key := fmt.Sprintf("%s|%s|%s|%s|%d|%d|%s|%s",
		strings.ToLower(params.Query), params.Category, params.Language,
		params.TimeRange, params.SafeSearch, params.Page, strings.Join(params.Engines, ","),
		strings.Join(params.Tokens, ","))
	caller := CallerFrom(ctx)
	val, cached, err := s.SearchCache.Do(key, func() (any, error) {
		resp, err := s.searchSplit(ctx, params)
		s.logSearch(caller, params, resp, err)
		return resp, err
	})
	if err != nil {
		return nil, err
	}
	// Clone: la caché comparte la instancia y los llamadores recortan slices.
	resp := val.(*searx.Response).Clone()
	resp.Meta.Cached = cached
	return resp, nil
}

// searchSplit hace la búsqueda en SearXNG. Si algún motor de
// NoLanguageEngines entraría, lo saca del pedido principal y le pregunta
// aparte, sin idioma y sin categoría (con categoría, SearXNG sumaría todos
// los motores de ella), y junta los resultados. Si el pedido aparte falla, el
// motor cuenta como fallado y queda lo del principal.
func (s *Service) searchSplit(ctx context.Context, p searx.Params) (*searx.Response, error) {
	sin := s.noLanguageFor(ctx, p)
	if len(sin) == 0 {
		return s.Client.Search(ctx, p)
	}
	main := p
	main.DisabledEngines = sin
	main.Engines = without(p.Engines, sin)
	extra := searx.Params{
		Query: p.Query, Language: "all", TimeRange: p.TimeRange, SafeSearch: p.SafeSearch,
		Page: p.Page, Engines: sin, Tokens: p.Tokens,
	}

	type res struct {
		resp *searx.Response
		err  error
	}
	ch := make(chan res, 1)
	go func() {
		r, err := s.Client.Search(ctx, extra)
		ch <- res{r, err}
	}()
	resp, err := s.Client.Search(ctx, main)
	other := <-ch
	if err != nil {
		return nil, err
	}
	if other.err != nil {
		other.resp = &searx.Response{Meta: searx.Meta{EnginesFailed: sin}}
	}
	merged := searx.Merge(resp, other.resp)
	merged.Language = p.Language
	return merged, nil
}

// noLanguageFor devuelve los motores de NoLanguageEngines que esta búsqueda
// consultaría: los de la categoría según el catálogo de SearXNG o los pedidos
// por engines. Sin idioma (o "all") no hace falta separar nada; si el
// catálogo no responde, tampoco se separa.
func (s *Service) noLanguageFor(ctx context.Context, p searx.Params) []string {
	if len(s.NoLanguageEngines) == 0 || p.Language == "" || p.Language == "all" {
		return nil
	}
	cat, err := s.Engines(ctx)
	if err != nil {
		return nil
	}
	var out []string
	for _, name := range s.NoLanguageEngines {
		in := false
		for _, e := range cat[p.Category] {
			if e.Name == name {
				in = true
				break
			}
		}
		for _, e := range p.Engines {
			if e == name {
				in = true
			}
		}
		if in {
			out = append(out, name)
		}
	}
	return out
}

func without(list, drop []string) []string {
	var out []string
	for _, e := range list {
		keep := true
		for _, d := range drop {
			if e == d {
				keep = false
				break
			}
		}
		if keep {
			out = append(out, e)
		}
	}
	return out
}

// logSearch escribe una línea por búsqueda real a SearXNG, por ejemplo:
//
//	searchgirl: busqueda cliente=secretaria privados=si categoria=general pagina=1 resultados=20 motores=braveapi:20,duckduckgo:7 fallaron=-
//
// motores cuenta en cuántos resultados aparece cada motor (un resultado
// puede venir de varios). Sirve para ver desde el log si un motor privado
// pago, como braveapi, se consultó y respondió.
func (s *Service) logSearch(caller string, p searx.Params, resp *searx.Response, err error) {
	if s.Log == nil {
		return
	}
	if caller == "" {
		caller = "-"
	}
	privados := "no"
	if len(p.Tokens) > 0 {
		privados = "si"
	}
	head := fmt.Sprintf("searchgirl: busqueda cliente=%s privados=%s categoria=%s pagina=%d",
		caller, privados, p.Category, p.Page)
	if err != nil {
		fmt.Fprintf(s.Log, "%s error=%q\n", head, err.Error())
		return
	}
	count := map[string]int{}
	for _, r := range resp.Results {
		for _, e := range r.Engines {
			count[e]++
		}
	}
	names := make([]string, 0, len(count))
	for e := range count {
		names = append(names, e)
	}
	sort.Strings(names)
	motores := make([]string, 0, len(names))
	for _, e := range names {
		motores = append(motores, fmt.Sprintf("%s:%d", e, count[e]))
	}
	fallaron := strings.Join(resp.Meta.EnginesFailed, ",")
	if fallaron == "" {
		fallaron = "-"
	}
	m := strings.Join(motores, ",")
	if m == "" {
		m = "-"
	}
	fmt.Fprintf(s.Log, "%s resultados=%d motores=%s fallaron=%s\n", head, len(resp.Results), m, fallaron)
}

func (s *Service) Suggest(ctx context.Context, q string) ([]string, error) {
	return s.Client.Suggest(ctx, q)
}

func (s *Service) Engines(ctx context.Context) (map[string][]searx.EngineInfo, error) {
	val, _, err := s.EnginesCache.Do("engines", func() (any, error) {
		return s.Client.Engines(ctx)
	})
	if err != nil {
		return nil, err
	}
	return val.(map[string][]searx.EngineInfo), nil
}

func (s *Service) Categories() []string { return searx.Categories }

func (s *Service) Healthy(ctx context.Context) bool { return s.Client.Healthy(ctx) }
