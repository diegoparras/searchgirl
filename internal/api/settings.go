package api

import (
	"encoding/json"
	"net/http"
	"sort"
	"strings"

	"github.com/diegoparras/searchgirl/internal/llm"
)

// The LLM settings panel: pick a provider and model from the UI at runtime.
// All handlers are admin-gated — only an admin (or loopback standalone) may
// change which model the Respuesta IA mode uses.

func (s *Server) handleSettings(w http.ResponseWriter, r *http.Request) {
	if !s.isAdmin(r) {
		writeErr(w, http.StatusForbidden, "solo un administrador puede cambiar el modelo")
		return
	}
	switch r.Method {
	case http.MethodGet:
		writeJSON(w, http.StatusOK, s.Store.Snapshot())
	case http.MethodPost:
		var set llm.Settings
		if err := json.NewDecoder(r.Body).Decode(&set); err != nil {
			writeErr(w, http.StatusBadRequest, "cuerpo inválido")
			return
		}
		set.BaseURL = strings.TrimSpace(set.BaseURL)
		set.Model = strings.TrimSpace(set.Model)
		if err := s.Store.Save(set); err != nil {
			writeErr(w, http.StatusInternalServerError, "no se pudo guardar la configuración")
			return
		}
		writeJSON(w, http.StatusOK, s.Store.Snapshot())
	default:
		writeErr(w, http.StatusMethodNotAllowed, "método no permitido")
	}
}

func (s *Server) handleTestLLM(w http.ResponseWriter, r *http.Request) {
	if !s.isAdmin(r) {
		writeErr(w, http.StatusForbidden, "solo un administrador puede probar el modelo")
		return
	}
	if !s.Store.Available() {
		writeJSON(w, http.StatusOK, map[string]any{"ok": false, "error": "no hay modelo configurado"})
		return
	}
	if _, err := s.Store.Complete(r.Context(), "Reply with the single word: ok"); err != nil {
		writeJSON(w, http.StatusOK, map[string]any{"ok": false, "error": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true, "name": s.Store.Name()})
}

func (s *Server) handleModels(w http.ResponseWriter, r *http.Request) {
	if !s.isAdmin(r) {
		writeErr(w, http.StatusForbidden, "solo un administrador puede listar modelos")
		return
	}
	var in llm.Settings
	_ = json.NewDecoder(r.Body).Decode(&in)
	in.BaseURL = strings.TrimSpace(in.BaseURL)
	ids, err := s.Store.ModelsFor(r.Context(), in.BaseURL, in.APIKey)
	if err != nil {
		writeJSON(w, http.StatusOK, map[string]any{"ok": false, "error": err.Error()})
		return
	}
	sort.Strings(ids)
	type m struct {
		ID          string `json:"id"`
		Recommended bool   `json:"recommended"`
	}
	out := make([]m, 0, len(ids))
	rec := 0
	for _, id := range ids {
		ok := recommendModel(id)
		if ok {
			rec++
		}
		out = append(out, m{ID: id, Recommended: ok})
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true, "models": out, "count": len(ids), "recommended": rec})
}

// recommendModel is a heuristic for the Respuesta IA use case (search
// synthesis with citations): a capable chat/instruct model, NOT an
// embedding/audio/vision/rerank one — those can't write a cited answer.
func recommendModel(id string) bool {
	s := strings.ToLower(id)
	for _, bad := range []string{"embed", "whisper", "tts", "audio", "moderation", "rerank",
		"dall-e", "stable-diffusion", "flux", "clip", "bge", "e5-", "guard", "llava", "vl:", "-vl", "vision", "reranker"} {
		if strings.Contains(s, bad) {
			return false
		}
	}
	for _, k := range []string{"claude", "gpt-4", "gpt-4o", "o1-", "o3-", "o4-", "deepseek",
		"qwen2.5", "qwen-2.5", "qwen2", "qwen3", "qwen-3", "llama-3", "llama3", "gemma2", "gemma-2",
		"gemma3", "mistral", "mixtral", "command-r", "grok", "gemini-1.5", "gemini-2", "phi-4"} {
		if strings.Contains(s, k) {
			return true
		}
	}
	if strings.Contains(s, "instruct") || strings.Contains(s, "chat") {
		for _, sz := range []string{"70b", "72b", "32b", "27b", "14b", "9b", "8b", "7b"} {
			if strings.Contains(s, sz) {
				return true
			}
		}
	}
	return false
}
