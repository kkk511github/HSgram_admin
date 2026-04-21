package httpapi

import (
	"database/sql"
	"encoding/json"
	"errors"
	"net/http"
	"strconv"
	"strings"

	"hsgram-admin/backend/internal/store"
)

func (h *Handler) handleSupportThreads(w http.ResponseWriter, r *http.Request, admin store.AdminUser) {
	if r.Method != http.MethodGet {
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}

	threads, err := h.store.ListSupportThreads(r.Context())
	if err != nil {
		writeError(w, http.StatusInternalServerError, "load support threads failed")
		return
	}

	writeJSON(w, http.StatusOK, apiResponse{OK: true, Data: threads})
}

func (h *Handler) handleSupportThreadRoutes(w http.ResponseWriter, r *http.Request, admin store.AdminUser) {
	path := strings.TrimPrefix(r.URL.Path, "/api/admin/support/threads/")
	parts := strings.Split(strings.Trim(path, "/"), "/")
	if len(parts) == 0 || parts[0] == "" {
		writeError(w, http.StatusNotFound, "support thread not found")
		return
	}

	userID, err := strconv.ParseInt(parts[0], 10, 64)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid user id")
		return
	}

	if len(parts) == 1 {
		h.handleSupportThreadDetail(w, r, admin, userID)
		return
	}

	switch parts[1] {
	case "reply":
		h.handleSupportThreadReply(w, r, admin, userID)
	default:
		writeError(w, http.StatusNotFound, "route not found")
	}
}

func (h *Handler) handleSupportThreadDetail(w http.ResponseWriter, r *http.Request, admin store.AdminUser, userID int64) {
	if r.Method != http.MethodGet {
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}

	detail, err := h.store.GetSupportThread(r.Context(), userID)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			writeError(w, http.StatusNotFound, "support thread not found")
			return
		}
		writeError(w, http.StatusInternalServerError, "load support thread failed")
		return
	}

	writeJSON(w, http.StatusOK, apiResponse{OK: true, Data: detail})
}

func (h *Handler) handleSupportThreadReply(w http.ResponseWriter, r *http.Request, admin store.AdminUser, userID int64) {
	if r.Method != http.MethodPost {
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	if h.msg == nil {
		writeError(w, http.StatusServiceUnavailable, "message rpc unavailable")
		return
	}

	var request struct {
		Message string `json:"message"`
	}
	if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
		writeError(w, http.StatusBadRequest, "invalid json body")
		return
	}

	messageText := strings.TrimSpace(request.Message)
	if messageText == "" {
		writeError(w, http.StatusBadRequest, "message is required")
		return
	}

	if err := h.msg.SendTextMessage(r.Context(), store.SupportSystemUserID, userID, messageText); err != nil {
		writeError(w, http.StatusInternalServerError, "send support reply failed")
		return
	}

	_ = h.store.CreateAuditLog(r.Context(), admin, "support.reply", userID, map[string]any{
		"message": messageText,
	})

	writeJSON(w, http.StatusOK, apiResponse{OK: true})
}
