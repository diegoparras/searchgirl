package main

import (
	"context"
	"flag"
	"fmt"
	"net/http"
	"os"
	"time"

	"github.com/diegoparras/searchgirl"
	"github.com/diegoparras/searchgirl/internal/answer"
	"github.com/diegoparras/searchgirl/internal/api"
	"github.com/diegoparras/searchgirl/internal/auth"
	"github.com/diegoparras/searchgirl/internal/fetch"
	"github.com/diegoparras/searchgirl/internal/llm"
	"github.com/diegoparras/searchgirl/internal/mcpsrv"
	"github.com/diegoparras/searchgirl/internal/novedades"
	"github.com/diegoparras/searchgirl/internal/search"
	"github.com/diegoparras/searchgirl/internal/tokens"
	"github.com/diegoparras/searchgirl/internal/web"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// cmdServe runs Searchgirl. Empty -http = MCP over stdio (an LLM client
// launches the binary per session). With -http it is the long-running
// service: UI at /, JSON API at /api/*, MCP at /mcp.
func cmdServe(args []string) error {
	// Lo primero que se ve en los registros al levantar: version y que cambia
	// (CHANGELOG embebido). Va a stderr: en modo stdio, stdout es del MCP.
	cartelArranque(os.Stderr, searchgirl.Changelog, version)

	fs := flag.NewFlagSet("serve", flag.ExitOnError)
	httpAddr := fs.String("http", os.Getenv("SEARCHGIRL_ADDR"), "serve over HTTP on this address (e.g. :8080); empty = MCP over stdio")
	_ = fs.Parse(args)

	svc := search.FromEnv()
	reader := fetch.FromEnv()
	// El proveedor LLM es runtime-switchable: el Store envuelve al provider
	// activo (elegido desde la UI) y cae al de env cuando no hay nada guardado.
	store := llm.NewStore(os.Getenv("SEARCHGIRL_CONFIG_DIR"), llm.FromEnv())
	ans := &answer.Engine{Search: svc, Reader: reader, Provider: store}
	mcpOpts := mcpsrv.Options{Version: version, Reader: reader, Answer: ans}
	srv := mcpsrv.New(svc, mcpOpts)

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

	// Por HTTP, cada llamada MCP se identifica por su token: es lo que decide
	// qué motores privados de SearXNG puede usar (SEARCHGIRL_SEARXNG_TOKENS).
	mcpOpts.Caller = authn.TokenName
	srv = mcpsrv.New(svc, mcpOpts)
	handler := mcp.NewStreamableHTTPHandler(func(*http.Request) *mcp.Server { return srv }, nil)
	mux := http.NewServeMux()
	mux.Handle("/mcp", handler)
	mux.HandleFunc("/healthz", func(w http.ResponseWriter, r *http.Request) { _, _ = w.Write([]byte("ok")) })
	apiSrv := api.New(svc, version)
	apiSrv.Novedades = novedades.Parsear(searchgirl.Changelog)
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
	var h http.Handler = callerFromToken(authn.TokenName, mux)
	h = authn.Gate(h)                       // auth (cookie o Bearer)
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

// callerFromToken deja en el contexto del pedido el nombre del token Bearer
// que lo trae, para que la búsqueda (API y Respuesta IA) sepa quién pregunta
// y le habilite sólo sus motores privados. Una sesión de la interfaz no trae
// token: busca sin motores privados.
func callerFromToken(name func(http.Header) (string, bool), next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if n, ok := name(r.Header); ok {
			r = r.WithContext(search.WithCaller(r.Context(), n))
		}
		next.ServeHTTP(w, r)
	})
}
