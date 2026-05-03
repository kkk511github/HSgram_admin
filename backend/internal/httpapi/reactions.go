package httpapi

import (
	"database/sql"
	"encoding/json"
	"errors"
	"net/http"
	"strconv"
	"strings"

	reactionconfig "hsgram-admin/backend/internal/reactions"
	"hsgram-admin/backend/internal/store"
)

func (h *Handler) handleReactions(w http.ResponseWriter, r *http.Request, admin store.AdminUser) {
	if h.reactions == nil {
		writeErrorCode(w, http.StatusServiceUnavailable, "reactions_config_unavailable", "reactions config service unavailable")
		return
	}
	switch r.Method {
	case http.MethodGet:
		items, err := h.reactions.ListReactions(r.Context())
		if err != nil {
			writeError(w, http.StatusInternalServerError, "load reactions failed")
			return
		}
		writeJSON(w, http.StatusOK, apiResponse{OK: true, Data: items})
	case http.MethodPost:
		var req reactionconfig.Reaction
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			writeError(w, http.StatusBadRequest, "invalid json body")
			return
		}
		item, err := h.reactions.UpsertReaction(r.Context(), req)
		if err != nil {
			writeError(w, http.StatusBadRequest, err.Error())
			return
		}
		writeJSON(w, http.StatusOK, apiResponse{OK: true, Data: item})
	default:
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
	}
}

func (h *Handler) handleReactionRoutes(w http.ResponseWriter, r *http.Request, admin store.AdminUser) {
	if h.reactions == nil {
		writeErrorCode(w, http.StatusServiceUnavailable, "reactions_config_unavailable", "reactions config service unavailable")
		return
	}
	path := strings.Trim(strings.TrimPrefix(r.URL.Path, "/api/admin/reactions/"), "/")
	id, err := strconv.ParseInt(path, 10, 64)
	if err != nil || id <= 0 {
		writeError(w, http.StatusBadRequest, "invalid reaction id")
		return
	}
	if r.Method != http.MethodPatch {
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	var req reactionconfig.ReactionPatch
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid json body")
		return
	}
	item, err := h.reactions.PatchReaction(r.Context(), id, req)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			writeError(w, http.StatusNotFound, "reaction not found")
			return
		}
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, apiResponse{OK: true, Data: item})
}

func (h *Handler) handleEmojiKeywords(w http.ResponseWriter, r *http.Request, admin store.AdminUser) {
	if h.reactions == nil {
		writeErrorCode(w, http.StatusServiceUnavailable, "reactions_config_unavailable", "reactions config service unavailable")
		return
	}
	switch r.Method {
	case http.MethodGet:
		items, err := h.reactions.ListEmojiKeywords(r.Context(), r.URL.Query().Get("lang"))
		if err != nil {
			writeError(w, http.StatusInternalServerError, "load emoji keywords failed")
			return
		}
		writeJSON(w, http.StatusOK, apiResponse{OK: true, Data: items})
	case http.MethodPost:
		var req reactionconfig.EmojiKeyword
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			writeError(w, http.StatusBadRequest, "invalid json body")
			return
		}
		item, err := h.reactions.UpsertEmojiKeyword(r.Context(), req)
		if err != nil {
			writeError(w, http.StatusBadRequest, err.Error())
			return
		}
		writeJSON(w, http.StatusOK, apiResponse{OK: true, Data: item})
	default:
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
	}
}
