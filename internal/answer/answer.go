// Package answer is the "Respuesta IA" pipeline (Perplexica-lite): search →
// top-N sources → optional page fetch → prompt with numbered sources → cited
// Markdown answer. It only exists when an LLM provider is configured; without
// one, Searchgirl works exactly the same minus this mode.
package answer

import (
	"context"
	"fmt"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/diegoparras/searchgirl/internal/fetch"
	"github.com/diegoparras/searchgirl/internal/llm"
	"github.com/diegoparras/searchgirl/internal/search"
)

const (
	defaultMaxSources = 6
	maxSourceChars    = 4000 // per fetched page, keeps the prompt bounded
	fetchTimeout      = 8 * time.Second
)

type Engine struct {
	Search   *search.Service
	Reader   *fetch.Reader
	Provider llm.Provider
}

func (e *Engine) Available() bool { return e.Provider != nil && e.Provider.Available() }

type Request struct {
	Query      string `json:"query"`
	Category   string `json:"category,omitempty"`
	Language   string `json:"language,omitempty"`
	MaxSources int    `json:"max_sources,omitempty"`
	FetchPages bool   `json:"fetch_pages,omitempty"`
}

type Source struct {
	N      int    `json:"n"`
	Title  string `json:"title"`
	URL    string `json:"url"`
	Domain string `json:"domain"`
}

type Result struct {
	Answer  string   `json:"answer"`
	Sources []Source `json:"sources"`
	Model   string   `json:"model"`
	TookMs  int64    `json:"took_ms"`
}

func (e *Engine) Answer(ctx context.Context, req Request) (*Result, error) {
	if !e.Available() {
		return nil, fmt.Errorf("no LLM provider configured")
	}
	if strings.TrimSpace(req.Query) == "" {
		return nil, fmt.Errorf("empty query")
	}
	start := time.Now()

	maxSources := req.MaxSources
	if maxSources <= 0 {
		maxSources = defaultMaxSources
	}
	if maxSources > 10 {
		maxSources = 10
	}

	resp, err := e.Search.Search(ctx, search.Query{Query: req.Query, Category: req.Category, Language: req.Language})
	if err != nil {
		return nil, err
	}
	if len(resp.Results) == 0 {
		return nil, fmt.Errorf("no hay resultados para sintetizar")
	}
	top := resp.Results
	if len(top) > maxSources {
		top = top[:maxSources]
	}

	sources := make([]Source, len(top))
	texts := make([]SourceText, len(top))
	for i, r := range top {
		sources[i] = Source{N: i + 1, Title: r.Title, URL: r.URL, Domain: r.Domain}
		texts[i] = SourceText{N: i + 1, Title: r.Title, Domain: r.Domain, Text: r.Snippet}
	}

	// Optional deep mode: fetch the pages concurrently and use an excerpt
	// instead of the snippet. Failures fall back to the snippet silently.
	if req.FetchPages && e.Reader != nil {
		var wg sync.WaitGroup
		for i := range top {
			wg.Add(1)
			go func(i int) {
				defer wg.Done()
				fctx, cancel := context.WithTimeout(ctx, fetchTimeout)
				defer cancel()
				if doc, err := e.Reader.Read(fctx, top[i].URL, maxSourceChars, false); err == nil && doc.Markdown != "" {
					texts[i].Text = doc.Markdown
				}
			}(i)
		}
		wg.Wait()
	}

	raw, err := e.Provider.Complete(ctx, BuildPrompt(req.Query, texts))
	if err != nil {
		return nil, fmt.Errorf("llm: %w", err)
	}

	return &Result{
		Answer:  scrubCitations(strings.TrimSpace(raw), len(sources)),
		Sources: sources,
		Model:   e.Provider.Name(),
		TookMs:  time.Since(start).Milliseconds(),
	}, nil
}

var citeRe = regexp.MustCompile(`\[(\d{1,2})\]`)

// scrubCitations drops citations that point past the source list — a model
// citing [9] with 6 sources is hallucinating the reference.
func scrubCitations(s string, n int) string {
	return citeRe.ReplaceAllStringFunc(s, func(m string) string {
		num, err := strconv.Atoi(strings.Trim(m, "[]"))
		if err != nil || num < 1 || num > n {
			return ""
		}
		return m
	})
}
