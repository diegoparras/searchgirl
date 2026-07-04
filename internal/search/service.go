// Package search is the orchestration layer: it applies Searchgirl's
// defaults and validation on top of the raw searx client. The API, the MCP
// tools and (later) the answer pipeline all go through here, so behavior is
// identical no matter which face made the request.
package search

import (
	"context"
	"fmt"
	"os"
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
}

// FromEnv builds the service the way the container runs it:
//
//	SEARXNG_URL                  base URL of the internal SearXNG (default http://searxng:8080)
//	SEARXNG_TIMEOUT              per-search timeout (default 10s)
//	SEARCHGIRL_DEFAULT_LANGUAGE  default language (default es)
//	SEARCHGIRL_SAFESEARCH        default safesearch 0|1|2 (default 0)
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
	}
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
	}
	key := fmt.Sprintf("%s|%s|%s|%s|%d|%d|%s",
		strings.ToLower(params.Query), params.Category, params.Language,
		params.TimeRange, params.SafeSearch, params.Page, strings.Join(params.Engines, ","))
	val, cached, err := s.SearchCache.Do(key, func() (any, error) {
		return s.Client.Search(ctx, params)
	})
	if err != nil {
		return nil, err
	}
	// Clone: la caché comparte la instancia y los llamadores recortan slices.
	resp := val.(*searx.Response).Clone()
	resp.Meta.Cached = cached
	return resp, nil
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
