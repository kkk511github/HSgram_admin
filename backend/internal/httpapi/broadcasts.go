package httpapi

import (
	"database/sql"
	"encoding/json"
	"errors"
	"net/http"
	"strconv"
	"strings"

	"hsgram-admin/backend/internal/broadcast"
	"hsgram-admin/backend/internal/store"
)

func (h *Handler) handleBroadcastPreview(w http.ResponseWriter, r *http.Request, admin store.AdminUser) {
	if !h.ensureBroadcastsEnabled(w) {
		return
	}
	if r.Method != http.MethodPost {
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}

	req, ok := decodeBroadcastRequest(w, r)
	if !ok {
		return
	}

	preview, _, err := h.broadcasts.Preview(r.Context(), req)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}

	writeJSON(w, http.StatusOK, apiResponse{
		OK:   true,
		Data: preview,
	})
}

func (h *Handler) handleBroadcastRoutes(w http.ResponseWriter, r *http.Request, admin store.AdminUser) {
	if !h.ensureBroadcastsEnabled(w) {
		return
	}
	path := strings.TrimPrefix(r.URL.Path, "/api/admin/broadcasts")
	path = strings.Trim(path, "/")

	if path == "" {
		switch r.Method {
		case http.MethodGet:
			h.handleBroadcastList(w, r, admin)
		case http.MethodPost:
			h.handleBroadcastSend(w, r, admin)
		default:
			writeError(w, http.StatusMethodNotAllowed, "method not allowed")
		}
		return
	}

	id, err := strconv.ParseInt(strings.Split(path, "/")[0], 10, 64)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid broadcast id")
		return
	}
	if r.Method != http.MethodGet {
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}

	h.handleBroadcastDetail(w, r, admin, id)
}

func (h *Handler) handleBroadcastList(w http.ResponseWriter, r *http.Request, admin store.AdminUser) {
	items, err := h.store.ListBroadcasts(r.Context(), 50)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "load broadcasts failed")
		return
	}

	writeJSON(w, http.StatusOK, apiResponse{
		OK:   true,
		Data: items,
	})
}

func (h *Handler) handleBroadcastDetail(w http.ResponseWriter, r *http.Request, admin store.AdminUser, broadcastID int64) {
	record, err := h.store.GetBroadcast(r.Context(), broadcastID)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			writeError(w, http.StatusNotFound, "broadcast not found")
			return
		}
		writeError(w, http.StatusInternalServerError, "load broadcast failed")
		return
	}

	deliveries, err := h.store.ListBroadcastDeliveries(r.Context(), broadcastID, 500)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "load deliveries failed")
		return
	}
	record.Deliveries = deliveries

	writeJSON(w, http.StatusOK, apiResponse{
		OK:   true,
		Data: record,
	})
}

func (h *Handler) handleBroadcastSend(w http.ResponseWriter, r *http.Request, admin store.AdminUser) {
	req, ok := decodeBroadcastRequest(w, r)
	if !ok {
		return
	}

	record, preview, err := h.broadcasts.Enqueue(r.Context(), admin, req)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}

	if h.store != nil {
		_ = h.store.CreateAuditLog(r.Context(), admin, "broadcast.enqueue", 0, map[string]any{
			"broadcastId":  record.ID,
			"targetType":   req.TargetType,
			"targetCount":  preview.Count,
			"messageText":  req.MessageText,
			"senderUserId": store.BroadcastSystemUserID,
		})
	}

	writeJSON(w, http.StatusOK, apiResponse{
		OK: true,
		Data: map[string]any{
			"broadcast": record,
			"preview":   preview,
		},
	})
}

func decodeBroadcastRequest(w http.ResponseWriter, r *http.Request) (broadcast.Request, bool) {
	if r.Method != http.MethodPost {
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
		return broadcast.Request{}, false
	}

	var req broadcast.Request
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid json body")
		return broadcast.Request{}, false
	}

	return req, true
}

func (h *Handler) ensureBroadcastsEnabled(w http.ResponseWriter) bool {
	if h.broadcasts != nil {
		return true
	}

	writeErrorCode(w, http.StatusServiceUnavailable, "broadcast_unavailable", "broadcast feature is disabled")
	return false
}
