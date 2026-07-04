// Package searx is the HTTP client for the internal SearXNG service and the
// normalization layer that turns its JSON into the Searchgirl shape. SearXNG
// is consumed strictly over HTTP (format=json) — Searchgirl never links its
// code, which keeps the AGPL boundary clean.
package searx

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"sort"
	"strconv"
	"strings"
	"time"
)

// Categories Searchgirl exposes, in UI order. SearXNG knows more; these are
// the ones every default instance answers for.
var Categories = []string{"general", "news", "images", "videos", "science", "it", "files", "map", "music"}

var TimeRanges = []string{"day", "week", "month", "year"}

func ValidCategory(c string) bool {
	for _, k := range Categories {
		if c == k {
			return true
		}
	}
	return false
}

func ValidTimeRange(t string) bool {
	if t == "" {
		return true
	}
	for _, k := range TimeRanges {
		if t == k {
			return true
		}
	}
	return false
}

type Client struct {
	BaseURL string
	HTTP    *http.Client
}

func New(baseURL string, timeout time.Duration) *Client {
	if timeout <= 0 {
		timeout = 10 * time.Second
	}
	return &Client{
		BaseURL: strings.TrimRight(baseURL, "/"),
		HTTP:    &http.Client{Timeout: timeout},
	}
}

type Params struct {
	Query      string
	Category   string
	Language   string
	TimeRange  string
	SafeSearch int
	Page       int
	Engines    []string
}

// Search runs one metasearch and returns the normalized response.
func (c *Client) Search(ctx context.Context, p Params) (*Response, error) {
	if strings.TrimSpace(p.Query) == "" {
		return nil, fmt.Errorf("empty query")
	}
	v := url.Values{}
	v.Set("q", p.Query)
	v.Set("format", "json")
	if p.Category != "" {
		v.Set("categories", p.Category)
	}
	if p.Language != "" {
		v.Set("language", p.Language)
	}
	if p.TimeRange != "" {
		v.Set("time_range", p.TimeRange)
	}
	v.Set("safesearch", strconv.Itoa(p.SafeSearch))
	if p.Page > 1 {
		v.Set("pageno", strconv.Itoa(p.Page))
	}
	if len(p.Engines) > 0 {
		v.Set("engines", strings.Join(p.Engines, ","))
	}

	start := time.Now()
	body, err := c.get(ctx, "/search?"+v.Encode())
	if err != nil {
		return nil, err
	}
	var raw rawResponse
	if err := json.Unmarshal(body, &raw); err != nil {
		return nil, fmt.Errorf("searxng: bad JSON (is 'json' listed under search.formats in settings.yml?): %w", err)
	}
	resp := normalize(&raw, p)
	resp.Meta.TookMs = time.Since(start).Milliseconds()
	return resp, nil
}

// Suggest proxies SearXNG's /autocompleter (OpenSearch suggestions shape:
// ["query", ["s1", "s2", ...]]).
func (c *Client) Suggest(ctx context.Context, q string) ([]string, error) {
	if strings.TrimSpace(q) == "" {
		return []string{}, nil
	}
	v := url.Values{}
	v.Set("q", q)
	body, err := c.get(ctx, "/autocompleter?"+v.Encode())
	if err != nil {
		return nil, err
	}
	var arr []json.RawMessage
	if err := json.Unmarshal(body, &arr); err != nil || len(arr) < 2 {
		return []string{}, nil // autocomplete off upstream is not an error
	}
	var suggestions []string
	if err := json.Unmarshal(arr[1], &suggestions); err != nil {
		return []string{}, nil
	}
	return suggestions, nil
}

// EngineInfo is one enabled engine as reported by SearXNG's /config.
type EngineInfo struct {
	Name       string   `json:"name"`
	Categories []string `json:"categories"`
	Shortcut   string   `json:"shortcut"`
}

// Engines returns the enabled engines grouped by category.
func (c *Client) Engines(ctx context.Context) (map[string][]EngineInfo, error) {
	body, err := c.get(ctx, "/config")
	if err != nil {
		return nil, err
	}
	var cfg struct {
		Engines []struct {
			Name       string   `json:"name"`
			Categories []string `json:"categories"`
			Shortcut   string   `json:"shortcut"`
			Enabled    bool     `json:"enabled"`
		} `json:"engines"`
	}
	if err := json.Unmarshal(body, &cfg); err != nil {
		return nil, fmt.Errorf("searxng /config: %w", err)
	}
	out := map[string][]EngineInfo{}
	for _, e := range cfg.Engines {
		if !e.Enabled {
			continue
		}
		for _, cat := range e.Categories {
			if !ValidCategory(cat) {
				continue
			}
			out[cat] = append(out[cat], EngineInfo{Name: e.Name, Categories: e.Categories, Shortcut: e.Shortcut})
		}
	}
	for cat := range out {
		sort.Slice(out[cat], func(i, j int) bool { return out[cat][i].Name < out[cat][j].Name })
	}
	return out, nil
}

// Healthy pings SearXNG's own /healthz.
func (c *Client) Healthy(ctx context.Context) bool {
	_, err := c.get(ctx, "/healthz")
	return err == nil
}

func (c *Client) get(ctx context.Context, path string) ([]byte, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.BaseURL+path, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Accept", "application/json")
	resp, err := c.HTTP.Do(req)
	if err != nil {
		return nil, fmt.Errorf("searxng unreachable at %s: %w", c.BaseURL, err)
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(io.LimitReader(resp.Body, 8<<20))
	if err != nil {
		return nil, err
	}
	switch resp.StatusCode {
	case http.StatusOK:
		return body, nil
	case http.StatusForbidden:
		return nil, fmt.Errorf("searxng returned 403 — enable the json format in settings.yml (search.formats: [html, json])")
	case http.StatusTooManyRequests:
		return nil, fmt.Errorf("searxng rate-limited the request — disable the limiter (server.limiter: false) for internal use")
	default:
		return nil, fmt.Errorf("searxng http %d", resp.StatusCode)
	}
}
