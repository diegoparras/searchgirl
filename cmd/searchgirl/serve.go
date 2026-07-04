package main

import (
	"context"
	"flag"
	"fmt"
	"net/http"
	"os"

	"github.com/diegoparras/searchgirl/internal/answer"
	"github.com/diegoparras/searchgirl/internal/api"
	"github.com/diegoparras/searchgirl/internal/auth"
	"github.com/diegoparras/searchgirl/internal/fetch"
	"github.com/diegoparras/searchgirl/internal/llm"
	"github.com/diegoparras/searchgirl/internal/mcpsrv"
	"github.com/diegoparras/searchgirl/internal/search"
	"github.com/diegoparras/searchgirl/internal/web"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// cmdServe runs Searchgirl. Empty -http = MCP over stdio (an LLM client
// launches the binary per session). With -http it is the long-running
// service: UI at /, JSON API at /api/*, MCP at /mcp.
func cmdServe(args []string) error {
	fs := flag.NewFlagSet("serve", flag.ExitOnError)
	httpAddr := fs.String("http", os.Getenv("SEARCHGIRL_ADDR"), "serve over HTTP on this address (e.g. :8080); empty = MCP over stdio")
	_ = fs.Parse(args)

	svc := search.FromEnv()
	reader := fetch.FromEnv()
	provider := llm.FromEnv()
	ans := &answer.Engine{Search: svc, Reader: reader, Provider: provider}
	srv := mcpsrv.New(svc, mcpsrv.Options{Version: version, Reader: reader, Answer: ans})

	if *httpAddr == "" {
		return srv.Run(context.Background(), &mcp.StdioTransport{})
	}

	authn, err := auth.FromEnv(context.Background())
	if err != nil {
		return err
	}
	// Fail-safe: nunca poner un buscador/proxy sin auth en una interfaz pública.
	if err := checkExposure(*httpAddr, authn); err != nil {
		return err
	}

	handler := mcp.NewStreamableHTTPHandler(func(*http.Request) *mcp.Server { return srv }, nil)
	mux := http.NewServeMux()
	mux.Handle("/mcp", handler)
	mux.HandleFunc("/healthz", func(w http.ResponseWriter, r *http.Request) { _, _ = w.Write([]byte("ok")) })
	apiSrv := api.New(svc, version)
	apiSrv.Reader = reader
	apiSrv.Answer = ans
	apiSrv.LLMAvailable = provider.Available
	apiSrv.LLMModel = provider.Name
	apiSrv.AuthMode = func() string {
		switch authn.Mode() {
		case "federated":
			return "federado"
		case "off":
			return "standalone"
		default:
			return authn.Mode()
		}
	}
	apiSrv.Mount(mux)
	web.New().Mount(mux)      // cara humana: SPA en /
	authn.RegisterRoutes(mux) // accesorio: OIDC login (solo federado)

	tls := os.Getenv("COOKIE_SECURE") == "1"
	var h http.Handler = authn.Gate(mux)    // auth (cookie o Bearer)
	h = newIPLimiterFromEnv().middleware(h) // rate limit por IP (config por .env)
	h = securityHeaders(h, tls)             // headers conservadores

	fmt.Fprintf(os.Stderr, "searchgirl: serving on %s [auth=%s] — API at /api, MCP at /mcp (searxng %s)\n", *httpAddr, authn.Mode(), svc.Client.BaseURL)
	return http.ListenAndServe(*httpAddr, h)
}
