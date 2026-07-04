// Package api is the JSON face of Searchgirl: thin handlers over
// internal/search. Every response is JSON; errors come back as
// {"error": "..."} with a matching status code.
package api

import (
	"context"
	"encoding/json"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/diegoparras/searchgirl/internal/answer"
	"github.com/diegoparras/searchgirl/internal/fetch"
	"github.com/diegoparras/searchgirl/internal/search"
)

type Server struct {
	Search  *search.Service
	Reader  *fetch.Reader
	Answer  *answer.Engine
	Version string

	// AuthMode and LLM info are injected by cmd/serve so this package stays
	// decoupled from auth and llm. Nil-safe defaults apply.
	AuthMode     func() string
	LLMAvailable func() bool
	LLMModel     func() string
}

func New(svc *search.Service, version string) *Server {
	return &Server{Search: svc, Version: version}
}

func (s *Server) Mount(mux *http.ServeMux) {
	mux.HandleFunc("GET /api/search", s.handleSearch)
	mux.HandleFunc("GET /api/suggest", s.handleSuggest)
	mux.HandleFunc("GET /api/engines", s.handleEngines)
	mux.HandleFunc("GET /api/categories", s.handleCategories)
	mux.HandleFunc("GET /api/config", s.handleConfig)
	mux.HandleFunc("POST /api/read", s.handleRead)
	mux.HandleFunc("POST /api/answer", s.handleAnswer)
	if s.Reader != nil {
		mux.HandleFunc("GET /thumb", s.Reader.ServeThumb)
	}
}

func (s *Server) handleAnswer(w http.ResponseWriter, r *http.Request) {
	if s.Answer == nil || !s.Answer.Available() {
		writeErr(w, http.StatusServiceUnavailable, "no hay un modelo configurado (ANTHROPIC_API_KEY o LLM_BASE_URL+LLM_MODEL)")
		return
	}
	var req answer.Request
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil || strings.TrimSpace(req.Query) == "" {
		writeErr(w, http.StatusBadRequest, `body must be {"query": "..."}`)
		return
	}
	res, err := s.Answer.Answer(r.Context(), req)
	if err != nil {
		status := http.StatusBadGateway
		if isBadRequest(err) {
			status = http.StatusBadRequest
		}
		writeErr(w, status, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, res)
}

func (s *Server) handleRead(w http.ResponseWriter, r *http.Request) {
	if s.Reader == nil {
		writeErr(w, http.StatusServiceUnavailable, "url reading is not enabled")
		return
	}
	var in struct {
		URL       string `json:"url"`
		MaxLength int    `json:"max_length"`
		Raw       bool   `json:"raw"`
	}
	if err := json.NewDecoder(r.Body).Decode(&in); err != nil || strings.TrimSpace(in.URL) == "" {
		writeErr(w, http.StatusBadRequest, `body must be {"url": "https://..."}`)
		return
	}
	doc, err := s.Reader.Read(r.Context(), in.URL, in.MaxLength, in.Raw)
	if err != nil {
		writeErr(w, http.StatusBadGateway, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, doc)
}

func (s *Server) handleSearch(w http.ResponseWriter, r *http.Request) {
	qs := r.URL.Query()
	q := search.Query{
		Query:     qs.Get("q"),
		Category:  qs.Get("category"),
		Language:  qs.Get("language"),
		TimeRange: qs.Get("time_range"),
	}
	if strings.TrimSpace(q.Query) == "" {
		writeErr(w, http.StatusBadRequest, "missing required parameter: q")
		return
	}
	if v := qs.Get("safesearch"); v != "" {
		n, err := strconv.Atoi(v)
		if err != nil {
			writeErr(w, http.StatusBadRequest, "safesearch must be 0, 1 or 2")
			return
		}
		q.SafeSearch = &n
	}
	if v := qs.Get("page"); v != "" {
		n, err := strconv.Atoi(v)
		if err != nil || n < 1 {
			writeErr(w, http.StatusBadRequest, "page must be a positive integer")
			return
		}
		q.Page = n
	}
	if v := qs.Get("engines"); v != "" {
		for _, e := range strings.Split(v, ",") {
			if e = strings.TrimSpace(e); e != "" {
				q.Engines = append(q.Engines, e)
			}
		}
	}
	resp, err := s.Search.Search(r.Context(), q)
	if err != nil {
		status := http.StatusBadGateway
		if isBadRequest(err) {
			status = http.StatusBadRequest
		}
		writeErr(w, status, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, resp)
}

func (s *Server) handleSuggest(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query().Get("q")
	suggestions, err := s.Search.Suggest(r.Context(), q)
	if err != nil {
		writeErr(w, http.StatusBadGateway, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"query": q, "suggestions": suggestions})
}

func (s *Server) handleEngines(w http.ResponseWriter, r *http.Request) {
	engines, err := s.Search.Engines(r.Context())
	if err != nil {
		writeErr(w, http.StatusBadGateway, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, engines)
}

func (s *Server) handleCategories(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, s.Search.Categories())
}

func (s *Server) handleConfig(w http.ResponseWriter, r *http.Request) {
	authMode := "standalone"
	if s.AuthMode != nil {
		authMode = s.AuthMode()
	}
	llmAvailable, llmModel := false, ""
	if s.LLMAvailable != nil {
		llmAvailable = s.LLMAvailable()
	}
	if llmAvailable && s.LLMModel != nil {
		llmModel = s.LLMModel()
	}
	ctx, cancel := context.WithTimeout(r.Context(), 2*time.Second)
	defer cancel()
	writeJSON(w, http.StatusOK, map[string]any{
		"name":             "Searchgirl",
		"version":          s.Version,
		"tagline":          "By SearXNG",
		"role":             "Buscador de la suite",
		"auth_mode":        authMode,
		"default_language": s.Search.DefaultLanguage,
		"llm":              map[string]any{"available": llmAvailable, "model": llmModel},
		"searxng_ok":       s.Search.Healthy(ctx),
	})
}

// isBadRequest tells validation errors (the caller's fault) apart from
// upstream failures. Validation errors come from internal/search before any
// network call.
func isBadRequest(err error) bool {
	msg := err.Error()
	for _, marker := range []string{"empty query", "unknown category", "unknown time_range", "safesearch must be"} {
		if strings.Contains(msg, marker) {
			return true
		}
	}
	return false
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

func writeErr(w http.ResponseWriter, status int, msg string) {
	writeJSON(w, status, map[string]string{"error": msg})
}
