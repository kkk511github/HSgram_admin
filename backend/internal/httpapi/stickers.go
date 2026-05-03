package httpapi

import (
	"database/sql"
	"encoding/json"
	"errors"
	"net/http"
	"strconv"
	"strings"

	stickerimport "hsgram-admin/backend/internal/stickers"
	"hsgram-admin/backend/internal/store"
)

func (h *Handler) handleStickerImport(w http.ResponseWriter, r *http.Request, admin store.AdminUser) {
	if r.Method != http.MethodPost {
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	if h.stickers == nil {
		writeErrorCode(w, http.StatusServiceUnavailable, "sticker_import_unavailable", "sticker import service unavailable")
		return
	}
	var req struct {
		ShortName              string `json:"shortName"`
		Source                 string `json:"source"`
		AuthorizationStatement string `json:"authorizationStatement"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid json body")
		return
	}
	result, err := h.stickers.ImportTelegramSet(r.Context(), stickerimport.ImportRequest{
		ShortName:              req.ShortName,
		Source:                 req.Source,
		AuthorizationStatement: req.AuthorizationStatement,
		AdminID:                admin.ID,
	})
	if err != nil {
		switch {
		case errors.Is(err, stickerimport.ErrImporterUnavailable):
			writeErrorCode(w, http.StatusServiceUnavailable, "sticker_import_unavailable", "sticker import service unavailable")
		case errors.Is(err, stickerimport.ErrAuthorizationNeeded):
			writeErrorCode(w, http.StatusBadRequest, "sticker_authorization_required", "authorization statement is required")
		case errors.Is(err, stickerimport.ErrUnsupportedFormat):
			writeErrorCode(w, http.StatusBadRequest, "sticker_format_unsupported", err.Error())
		default:
			writeError(w, http.StatusBadRequest, err.Error())
		}
		return
	}
	writeJSON(w, http.StatusOK, apiResponse{OK: true, Data: result})
}

func (h *Handler) handleStickerSets(w http.ResponseWriter, r *http.Request, admin store.AdminUser) {
	if r.Method != http.MethodGet {
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	if h.stickers == nil {
		writeErrorCode(w, http.StatusServiceUnavailable, "sticker_import_unavailable", "sticker import service unavailable")
		return
	}
	sets, err := h.stickers.ListSets(r.Context())
	if err != nil {
		writeError(w, http.StatusInternalServerError, "load sticker sets failed")
		return
	}
	writeJSON(w, http.StatusOK, apiResponse{OK: true, Data: sets})
}

func (h *Handler) handleStickerSetRoutes(w http.ResponseWriter, r *http.Request, admin store.AdminUser) {
	if h.stickers == nil {
		writeErrorCode(w, http.StatusServiceUnavailable, "sticker_import_unavailable", "sticker import service unavailable")
		return
	}
	path := strings.Trim(strings.TrimPrefix(r.URL.Path, "/api/admin/stickers/sets/"), "/")
	if path == "" {
		writeError(w, http.StatusNotFound, "route not found")
		return
	}
	idPart := path
	action := ""
	if strings.Contains(path, "/") {
		parts := strings.SplitN(path, "/", 2)
		idPart = parts[0]
		action = parts[1]
	}
	id, err := strconv.ParseInt(idPart, 10, 64)
	if err != nil || id <= 0 {
		writeError(w, http.StatusBadRequest, "invalid sticker set id")
		return
	}
	switch {
	case action == "" && r.Method == http.MethodGet:
		h.handleStickerSetDetail(w, r, id)
	case action == "disable" && r.Method == http.MethodPost:
		h.handleStickerSetDisable(w, r, id)
	default:
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
	}
}

func (h *Handler) handleStickerSetDetail(w http.ResponseWriter, r *http.Request, id int64) {
	detail, err := h.stickers.GetSet(r.Context(), id)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "load sticker set failed")
		return
	}
	if detail == nil {
		writeError(w, http.StatusNotFound, "sticker set not found")
		return
	}
	writeJSON(w, http.StatusOK, apiResponse{OK: true, Data: detail})
}

func (h *Handler) handleStickerSetDisable(w http.ResponseWriter, r *http.Request, id int64) {
	if err := h.stickers.DisableSet(r.Context(), id); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			writeError(w, http.StatusNotFound, "sticker set not found")
			return
		}
		writeError(w, http.StatusInternalServerError, "disable sticker set failed")
		return
	}
	writeJSON(w, http.StatusOK, apiResponse{OK: true})
}
