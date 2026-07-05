package main

import (
	"context"
	"flag"
	"fmt"
	"net/http"
	"os"
	"time"

	"github.com/diegoparras/searchgirl/internal/answer"
	"github.com/diegoparras/searchgirl/internal/api"
	"github.com/diegoparras/searchgirl/internal/auth"
	"github.com/diegoparras/searchgirl/internal/fetch"
	"github.com/diegoparras/searchgirl/internal/llm"
	"github.com/diegoparras/searchgirl/internal/mcpsrv"
	"github.com/diegoparras/searchgirl/internal/search"
	"github.com/diegoparras/searchgirl/internal/tokens"
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
	// El proveedor LLM es runtime-switchable: el Store envuelve al provider
	// activo (elegido desde la UI) y cae al de env cuando no hay nada guardado.
	store := llm.NewStore(os.Getenv("SEARCHGIRL_CONFIG_DIR"), llm.FromEnv())
	ans := &answer.Engine{Search: svc, Reader: reader, Provider: store}
	srv := mcpsrv.New(svc, mcpsrv.Options{Version: version, Reader: reader, Answer: ans})

	if *httpAddr == "" {
		return srv.Run(context.Background(), &mcp.StdioTransport{})
	}

	authn, err := auth.FromEnv(context.Background())
	if err != nil {
		return err
	}
	// Tokens emitidos desde la UI (panel Conexión MCP): conviven con los de
	// env. auth los valida vía el verifier.
	tokStore := tokens.New(os.Getenv("SEARCHGIRL_CONFIG_DIR"))
	authn.SetVerifier(tokStore.Verify)
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
	apiSrv.Store = store
	apiSrv.Tokens = tokStore
	apiSrv.IsAdmin = authn.IsAdmin
	apiSrv.LLMAvailable = store.Available
	apiSrv.LLMModel = store.Name
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

	// No exponemos la URL interna de SearXNG en logs (topología interna): solo
	// si está configurada. El detalle vive en la env, no en docker logs.
	fmt.Fprintf(os.Stderr, "searchgirl: serving on %s [auth=%s] — API at /api, MCP at /mcp (searxng backend configured)\n", *httpAddr, authn.Mode())

	// Timeouts explícitos contra Slowloris y conexiones colgadas. ReadHeader
	// corto (headers deben llegar rápido); Write generoso porque una búsqueda
	// puede tardar mientras SearXNG consulta motores; el modo Respuesta IA usa
	// su propio timeout de request, así que 120s de Write lo cubre.
	server := &http.Server{
		Addr:              *httpAddr,
		Handler:           h,
		ReadHeaderTimeout: 10 * time.Second,
		ReadTimeout:       30 * time.Second,
		WriteTimeout:      120 * time.Second,
		IdleTimeout:       90 * time.Second,
		MaxHeaderBytes:    1 << 20,
	}
	return server.ListenAndServe()
}
