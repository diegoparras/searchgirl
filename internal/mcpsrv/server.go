// Package mcpsrv exposes Searchgirl to LLM clients over MCP — the same
// search service as the REST API and the UI, behind tools. Transport is the
// caller's choice (stdio or streamable HTTP), exactly like COGO.
package mcpsrv

import (
	"context"
	"fmt"
	"strings"

	"github.com/diegoparras/searchgirl/internal/answer"
	"github.com/diegoparras/searchgirl/internal/fetch"
	"github.com/diegoparras/searchgirl/internal/search"
	"github.com/diegoparras/searchgirl/internal/searx"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

type Options struct {
	Version string
	Reader  *fetch.Reader
	Answer  *answer.Engine
}

// New builds the MCP server. One `search` tool with a category parameter
// (not one tool per category): same endpoint semantics upstream, smaller
// schema for the model.
func New(svc *search.Service, opts Options) *mcp.Server {
	s := mcp.NewServer(&mcp.Implementation{Name: "searchgirl", Version: opts.Version}, nil)

	mcp.AddTool(s, &mcp.Tool{
		Name: "search",
		Description: "Web metasearch via SearXNG (privacy-respecting, multi-engine). Returns titles, " +
			"URLs and snippets ranked by relevance. Use category=news with time_range for recent events.",
	}, func(ctx context.Context, req *mcp.CallToolRequest, in searchIn) (*mcp.CallToolResult, any, error) {
		q := search.Query{
			Query:     in.Query,
			Category:  in.Category,
			Language:  in.Language,
			TimeRange: in.TimeRange,
			Page:      in.Page,
		}
		if in.SafeSearch != nil {
			q.SafeSearch = in.SafeSearch
		}
		resp, err := svc.Search(ctx, q)
		if err != nil {
			return errResult(err), nil, nil
		}
		max := in.MaxResults
		if max <= 0 {
			max = 10
		}
		if max > 30 {
			max = 30
		}
		if len(resp.Results) > max {
			resp.Results = resp.Results[:max]
		}
		return textResult(renderSearch(resp)), resp, nil
	})

	if opts.Reader != nil {
		mcp.AddTool(s, &mcp.Tool{
			Name: "url_read",
			Description: "Fetch a public URL and return its main content converted to Markdown. " +
				"Use it to read a page found with search before citing it.",
		}, func(ctx context.Context, req *mcp.CallToolRequest, in urlReadIn) (*mcp.CallToolResult, any, error) {
			doc, err := opts.Reader.Read(ctx, in.URL, in.MaxLength, in.Raw)
			if err != nil {
				return errResult(err), nil, nil
			}
			var b strings.Builder
			if doc.Title != "" {
				fmt.Fprintf(&b, "# %s\n\n", doc.Title)
			}
			fmt.Fprintf(&b, "Fuente: %s\n\n%s", doc.URL, doc.Markdown)
			return textResult(b.String()), doc, nil
		})
	}

	// answer solo se registra con un LLM configurado: un cliente MCP nunca ve
	// una tool que va a fallar sí o sí.
	if opts.Answer != nil && opts.Answer.Available() {
		mcp.AddTool(s, &mcp.Tool{
			Name: "answer",
			Description: "Search the web and synthesize a concise answer with numbered citations [1][2] " +
				"backed by the sources. Use fetch_pages=true for a deeper (slower) read of each source.",
		}, func(ctx context.Context, req *mcp.CallToolRequest, in answerIn) (*mcp.CallToolResult, any, error) {
			res, err := opts.Answer.Answer(ctx, answer.Request{
				Query: in.Query, Category: in.Category, Language: in.Language,
				MaxSources: in.MaxSources, FetchPages: in.FetchPages,
			})
			if err != nil {
				return errResult(err), nil, nil
			}
			var b strings.Builder
			b.WriteString(res.Answer + "\n\nFuentes:\n")
			for _, src := range res.Sources {
				fmt.Fprintf(&b, "[%d] %s — %s\n", src.N, src.Title, src.URL)
			}
			return textResult(b.String()), res, nil
		})
	}

	return s
}

type answerIn struct {
	Query      string `json:"query" jsonschema:"the question to answer"`
	Category   string `json:"category,omitempty" jsonschema:"search category; default general"`
	Language   string `json:"language,omitempty" jsonschema:"language code like es or en"`
	MaxSources int    `json:"max_sources,omitempty" jsonschema:"how many sources to synthesize from; default 6, max 10"`
	FetchPages bool   `json:"fetch_pages,omitempty" jsonschema:"true to fetch each source page for a deeper answer (slower)"`
}

type urlReadIn struct {
	URL       string `json:"url" jsonschema:"the absolute http(s) URL to read"`
	MaxLength int    `json:"max_length,omitempty" jsonschema:"max output characters; default 20000"`
	Raw       bool   `json:"raw,omitempty" jsonschema:"true to skip content extraction and convert the whole page"`
}

type searchIn struct {
	Query      string `json:"query" jsonschema:"the search terms"`
	Category   string `json:"category,omitempty" jsonschema:"one of general, news, images, videos, science, it, files, map, music; default general"`
	Language   string `json:"language,omitempty" jsonschema:"language code like es, en, de, or all; default is the instance default"`
	TimeRange  string `json:"time_range,omitempty" jsonschema:"restrict results to day, week, month or year"`
	SafeSearch *int   `json:"safesearch,omitempty" jsonschema:"0 off, 1 moderate, 2 strict"`
	MaxResults int    `json:"max_results,omitempty" jsonschema:"max results to return; default 10, max 30"`
	Page       int    `json:"page,omitempty" jsonschema:"result page, starting at 1"`
}

// renderSearch prints results as a compact numbered Markdown list — the shape
// LLMs cite well. The full normalized JSON travels as structured content.
func renderSearch(r *searx.Response) string {
	var b strings.Builder
	if len(r.Results) == 0 {
		fmt.Fprintf(&b, "No results for %q (category %s).\n", r.Query, r.Category)
	} else {
		fmt.Fprintf(&b, "Results for %q (category %s):\n\n", r.Query, r.Category)
		for _, res := range r.Results {
			fmt.Fprintf(&b, "%d. %s\n   %s\n", res.Rank, res.Title, res.URL)
			line := res.Snippet
			if res.Published != "" && line != "" {
				line = res.Published + " — " + line
			} else if res.Published != "" {
				line = res.Published
			}
			if line != "" {
				fmt.Fprintf(&b, "   %s\n", line)
			}
		}
	}
	for _, a := range r.Answers {
		fmt.Fprintf(&b, "\nDirect answer: %s\n", a)
	}
	if len(r.Suggestions) > 0 {
		fmt.Fprintf(&b, "\nRelated: %s\n", strings.Join(r.Suggestions, " · "))
	}
	if len(r.Meta.EnginesFailed) > 0 {
		fmt.Fprintf(&b, "\n(engines that did not respond: %s)\n", strings.Join(r.Meta.EnginesFailed, ", "))
	}
	return b.String()
}

func textResult(s string) *mcp.CallToolResult {
	return &mcp.CallToolResult{Content: []mcp.Content{&mcp.TextContent{Text: s}}}
}

// errResult reports a tool error inside the result (IsError) so the LLM sees
// it — not as a protocol-level failure.
func errResult(err error) *mcp.CallToolResult {
	return &mcp.CallToolResult{IsError: true, Content: []mcp.Content{&mcp.TextContent{Text: "searchgirl: " + err.Error()}}}
}
