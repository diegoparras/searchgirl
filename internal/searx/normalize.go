package searx

import (
	"encoding/json"
	"net/url"
	"sort"
	"strings"
	"time"
	"unicode/utf8"
)

// Public, normalized shapes — the Searchgirl contract. Independent of the raw
// SearXNG payload so the API, the MCP tools and the UI never see upstream
// churn.

type Result struct {
	Rank      int      `json:"rank"`
	Title     string   `json:"title"`
	URL       string   `json:"url"`
	Domain    string   `json:"domain"`
	Snippet   string   `json:"snippet"`
	Engines   []string `json:"engines"`
	Score     float64  `json:"score"`
	Published string   `json:"published,omitempty"`
	Thumbnail string   `json:"thumbnail,omitempty"`
}

type InfoboxLink struct {
	Title string `json:"title"`
	URL   string `json:"url"`
}

type InfoboxAttr struct {
	Label string `json:"label"`
	Value string `json:"value"`
}

type Infobox struct {
	Title      string        `json:"title"`
	Content    string        `json:"content"`
	Image      string        `json:"img,omitempty"`
	Links      []InfoboxLink `json:"urls,omitempty"`
	Attributes []InfoboxAttr `json:"attributes,omitempty"`
}

type Meta struct {
	Total         int      `json:"total"`
	EnginesFailed []string `json:"engines_failed,omitempty"`
	TookMs        int64    `json:"took_ms"`
	Cached        bool     `json:"cached"`
}

type Response struct {
	Query       string    `json:"query"`
	Category    string    `json:"category"`
	Page        int       `json:"page"`
	Language    string    `json:"language"`
	Results     []Result  `json:"results"`
	Answers     []string  `json:"answers"`
	Infoboxes   []Infobox `json:"infoboxes"`
	Suggestions []string  `json:"suggestions"`
	Corrections []string  `json:"corrections"`
	Meta        Meta      `json:"meta"`
}

const maxSnippet = 320

// Clone returns a copy whose top-level slices are safe to trim/mutate — the
// cache shares one Response across requests (p.ej. la tool MCP recorta
// Results a max_results).
func (r *Response) Clone() *Response {
	out := *r
	out.Results = append([]Result(nil), r.Results...)
	out.Answers = append([]string(nil), r.Answers...)
	out.Infoboxes = append([]Infobox(nil), r.Infoboxes...)
	out.Suggestions = append([]string(nil), r.Suggestions...)
	out.Corrections = append([]string(nil), r.Corrections...)
	out.Meta.EnginesFailed = append([]string(nil), r.Meta.EnginesFailed...)
	return &out
}

// normalize converts the raw SearXNG payload into the Searchgirl shape:
// dedup by canonical URL (merging engines, keeping the best score), stable
// order by score, derived domain, ISO dates, bounded snippets.
func normalize(raw *rawResponse, p Params) *Response {
	resp := &Response{
		Query:       raw.Query,
		Category:    p.Category,
		Page:        p.Page,
		Language:    p.Language,
		Results:     []Result{},
		Answers:     []string{},
		Infoboxes:   []Infobox{},
		Suggestions: []string{},
		Corrections: []string{},
	}
	if resp.Query == "" {
		resp.Query = p.Query
	}
	if resp.Page == 0 {
		resp.Page = 1
	}

	// --- results: dedup + merge ---
	byKey := map[string]*Result{}
	var order []string
	for _, r := range raw.Results {
		if r.URL == "" {
			continue
		}
		key := canonicalURL(r.URL)
		engines := r.Engines
		if len(engines) == 0 && r.Engine != "" {
			engines = []string{r.Engine}
		}
		if got, ok := byKey[key]; ok {
			got.Engines = mergeEngines(got.Engines, engines)
			if r.Score > got.Score {
				got.Score = r.Score
			}
			if got.Snippet == "" && r.Content != "" {
				got.Snippet = clip(r.Content, maxSnippet)
			}
			if got.Published == "" {
				got.Published = isoDate(r.PublishedDate)
			}
			if got.Thumbnail == "" {
				got.Thumbnail = firstNonEmpty(r.Thumbnail, r.ThumbnailSrc, r.ImgSrc)
			}
			continue
		}
		byKey[key] = &Result{
			Title:     strings.TrimSpace(r.Title),
			URL:       r.URL,
			Domain:    domainOf(r.URL),
			Snippet:   clip(r.Content, maxSnippet),
			Engines:   mergeEngines(nil, engines),
			Score:     r.Score,
			Published: isoDate(r.PublishedDate),
			Thumbnail: firstNonEmpty(r.Thumbnail, r.ThumbnailSrc, r.ImgSrc),
		}
		order = append(order, key)
	}
	for _, key := range order {
		resp.Results = append(resp.Results, *byKey[key])
	}
	// Stable: score desc, ties keep SearXNG's order.
	sort.SliceStable(resp.Results, func(i, j int) bool { return resp.Results[i].Score > resp.Results[j].Score })
	for i := range resp.Results {
		resp.Results[i].Rank = i + 1
	}
	resp.Meta.Total = len(resp.Results)

	// --- lenient fields ---
	for _, a := range raw.Answers {
		if s := flexiString(a, "answer"); s != "" {
			resp.Answers = append(resp.Answers, s)
		}
	}
	for _, c := range raw.Corrections {
		if s := flexiString(c, "correction", "corrected", "value"); s != "" {
			resp.Corrections = append(resp.Corrections, s)
		}
	}
	resp.Suggestions = append(resp.Suggestions, raw.Suggestions...)
	for _, u := range raw.UnresponsiveEngines {
		if name := unresponsiveName(u); name != "" {
			resp.Meta.EnginesFailed = append(resp.Meta.EnginesFailed, name)
		}
	}
	sort.Strings(resp.Meta.EnginesFailed)

	for _, ib := range raw.Infoboxes {
		box := Infobox{
			Title:   strings.TrimSpace(ib.Infobox),
			Content: strings.TrimSpace(ib.Content),
			Image:   ib.ImgSrc,
		}
		for _, u := range ib.URLs {
			if u.URL != "" {
				box.Links = append(box.Links, InfoboxLink(u))
			}
		}
		for _, a := range ib.Attributes {
			if a.Label != "" && a.Value != "" {
				box.Attributes = append(box.Attributes, InfoboxAttr(a))
			}
		}
		if box.Title != "" || box.Content != "" {
			resp.Infoboxes = append(resp.Infoboxes, box)
		}
	}
	return resp
}

// canonicalURL is the dedup key: scheme-insensitive, lowercase host, no
// fragment, no trailing slash. The original URL is preserved in the result.
func canonicalURL(raw string) string {
	u, err := url.Parse(raw)
	if err != nil {
		return raw
	}
	u.Fragment = ""
	u.Host = strings.ToLower(u.Host)
	u.Scheme = ""
	s := strings.TrimPrefix(u.String(), "//")
	return strings.TrimSuffix(s, "/")
}

func domainOf(raw string) string {
	u, err := url.Parse(raw)
	if err != nil || u.Host == "" {
		return ""
	}
	return strings.TrimPrefix(strings.ToLower(u.Hostname()), "www.")
}

func mergeEngines(into, add []string) []string {
	seen := map[string]bool{}
	out := make([]string, 0, len(into)+len(add))
	for _, e := range append(append([]string{}, into...), add...) {
		e = strings.TrimSpace(e)
		if e == "" || seen[e] {
			continue
		}
		seen[e] = true
		out = append(out, e)
	}
	sort.Strings(out)
	return out
}

func clip(s string, max int) string {
	s = strings.TrimSpace(s)
	if utf8.RuneCountInString(s) <= max {
		return s
	}
	runes := []rune(s)
	return strings.TrimSpace(string(runes[:max])) + "…"
}

var dateLayouts = []string{
	time.RFC3339,
	"2006-01-02T15:04:05",
	"2006-01-02 15:04:05",
	"2006-01-02",
}

// isoDate reduces whatever date shape an engine produced to YYYY-MM-DD.
func isoDate(s string) string {
	s = strings.TrimSpace(s)
	if s == "" {
		return ""
	}
	for _, layout := range dateLayouts {
		if t, err := time.Parse(layout, s); err == nil {
			return t.Format("2006-01-02")
		}
	}
	return ""
}

func firstNonEmpty(vals ...string) string {
	for _, v := range vals {
		if v != "" {
			return v
		}
	}
	return ""
}

// flexiString decodes a value that may be a bare JSON string or an object
// holding the string under one of the given keys (SearXNG changed these
// shapes between releases).
func flexiString(m json.RawMessage, keys ...string) string {
	var s string
	if err := json.Unmarshal(m, &s); err == nil {
		return strings.TrimSpace(s)
	}
	var obj map[string]any
	if err := json.Unmarshal(m, &obj); err != nil {
		return ""
	}
	for _, k := range keys {
		if v, ok := obj[k].(string); ok && strings.TrimSpace(v) != "" {
			return strings.TrimSpace(v)
		}
	}
	return ""
}

// unresponsiveName extracts the engine name from an unresponsive_engines
// entry: ["engine", "reason"] in current SearXNG, or a bare string.
func unresponsiveName(m json.RawMessage) string {
	var pair []any
	if err := json.Unmarshal(m, &pair); err == nil && len(pair) > 0 {
		if name, ok := pair[0].(string); ok {
			return name
		}
		return ""
	}
	var s string
	if err := json.Unmarshal(m, &s); err == nil {
		return s
	}
	return ""
}
