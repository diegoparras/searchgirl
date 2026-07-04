package mcpsrv

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/diegoparras/searchgirl/internal/fetch"
	"github.com/diegoparras/searchgirl/internal/search"
	"github.com/diegoparras/searchgirl/internal/searx"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

func toolText(res *mcp.CallToolResult) string {
	var b strings.Builder
	for _, c := range res.Content {
		if tc, ok := c.(*mcp.TextContent); ok {
			b.WriteString(tc.Text)
		}
	}
	return b.String()
}

// TestMCPServer drives the real server over an in-memory transport, the same
// way an LLM client would: search against a fake SearXNG, url_read against a
// local page.
func TestMCPServer(t *testing.T) {
	backend := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/search":
			_, _ = w.Write([]byte(`{"query": "gophers", "results": [
				{"title": "The Go site", "url": "https://go.dev/", "content": "Build with Go.", "engine": "ddg", "score": 5},
				{"title": "Wiki", "url": "https://en.wikipedia.org/wiki/Go", "content": "Lang.", "engine": "wikipedia", "score": 3}
			], "answers": [], "corrections": [], "infoboxes": [], "suggestions": ["golang"], "unresponsive_engines": []}`))
		case "/page":
			w.Header().Set("Content-Type", "text/html")
			_, _ = w.Write([]byte(`<html><head><title>Doc</title></head><body><article><h1>Hola</h1><p>Contenido útil.</p></article></body></html>`))
		default:
			http.NotFound(w, r)
		}
	}))
	defer backend.Close()

	svc := &search.Service{Client: searx.New(backend.URL, time.Second), DefaultLanguage: "es"}
	reader := &fetch.Reader{MaxBytes: 1 << 20, AllowPrivate: true} // httptest is loopback

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	clientT, serverT := mcp.NewInMemoryTransports()
	go func() { _ = New(svc, Options{Version: "test", Reader: reader}).Run(ctx, serverT) }()

	client := mcp.NewClient(&mcp.Implementation{Name: "test", Version: "0"}, nil)
	cs, err := client.Connect(ctx, clientT, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer cs.Close()

	// search: numbered markdown with titles and URLs.
	res, err := cs.CallTool(ctx, &mcp.CallToolParams{Name: "search", Arguments: map[string]any{"query": "gophers", "max_results": 1}})
	if err != nil {
		t.Fatal(err)
	}
	if res.IsError {
		t.Fatalf("search errored: %s", toolText(res))
	}
	txt := toolText(res)
	if !strings.Contains(txt, "1. The Go site") || !strings.Contains(txt, "https://go.dev/") {
		t.Fatalf("search output:\n%s", txt)
	}
	if strings.Contains(txt, "Wiki") {
		t.Errorf("max_results=1 must trim the list:\n%s", txt)
	}

	// search with a bad category: an in-band tool error, not a protocol error.
	res, err = cs.CallTool(ctx, &mcp.CallToolParams{Name: "search", Arguments: map[string]any{"query": "x", "category": "nope"}})
	if err != nil {
		t.Fatal(err)
	}
	if !res.IsError {
		t.Fatal("bad category must be IsError")
	}

	// url_read: markdown with the title as heading.
	res, err = cs.CallTool(ctx, &mcp.CallToolParams{Name: "url_read", Arguments: map[string]any{"url": backend.URL + "/page"}})
	if err != nil {
		t.Fatal(err)
	}
	if res.IsError {
		t.Fatalf("url_read errored: %s", toolText(res))
	}
	txt = toolText(res)
	if !strings.Contains(txt, "# Doc") || !strings.Contains(txt, "Contenido útil.") {
		t.Fatalf("url_read output:\n%s", txt)
	}

	// tool listing: exactly search + url_read (answer only appears with an LLM).
	tools, err := cs.ListTools(ctx, &mcp.ListToolsParams{})
	if err != nil {
		t.Fatal(err)
	}
	names := map[string]bool{}
	for _, tl := range tools.Tools {
		names[tl.Name] = true
	}
	if !names["search"] || !names["url_read"] || len(tools.Tools) != 2 {
		t.Fatalf("tools = %v", names)
	}
}
