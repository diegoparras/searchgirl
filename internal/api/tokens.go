package api

import (
	"encoding/json"
	"net/http"
	"strings"
	"time"
)

// The "Conexión MCP" panel: issue/list/revoke Bearer tokens for the API and
// the MCP endpoint. Admin-gated, like the model settings.

func (s *Server) handleTokens(w http.ResponseWriter, r *http.Request) {
	if !s.isAdmin(r) {
		writeErr(w, http.StatusForbidden, "solo un administrador puede gestionar los tokens")
		return
	}
	switch r.Method {
	case http.MethodGet:
		writeJSON(w, http.StatusOK, map[string]any{
			"tokens":    s.Tokens.List(),
			"persisted": s.Tokens.Persisted(),
		})
	case http.MethodPost:
		var in struct {
			Label       string `json:"label"`
			ExpiresDays int    `json:"expires_days"`
		}
		if err := json.NewDecoder(r.Body).Decode(&in); err != nil {
			writeErr(w, http.StatusBadRequest, "cuerpo inválido")
			return
		}
		today := time.Now().UTC().Format("2006-01-02")
		secret, item, err := s.Tokens.Create(strings.TrimSpace(in.Label), in.ExpiresDays, today)
		if err != nil {
			writeErr(w, http.StatusInternalServerError, "no se pudo emitir el token")
			return
		}
		// El secreto viaja UNA sola vez, acá.
		writeJSON(w, http.StatusOK, map[string]any{"ok": true, "token": secret, "item": item})
	case http.MethodDelete:
		id := r.URL.Query().Get("id")
		if id == "" || !s.Tokens.Revoke(id) {
			writeErr(w, http.StatusNotFound, "no existe ese token")
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{"ok": true, "id": id})
	default:
		writeErr(w, http.StatusMethodNotAllowed, "método no permitido")
	}
}
