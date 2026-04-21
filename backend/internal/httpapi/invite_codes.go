package httpapi

import (
	"encoding/json"
	"errors"
	"net/http"
	"strings"

	"hsgram-admin/backend/internal/invitecodes"
	"hsgram-admin/backend/internal/store"
)

func (h *Handler) handleInviteCodes(w http.ResponseWriter, r *http.Request, admin store.AdminUser) {
	if h.invites == nil {
		writeError(w, http.StatusServiceUnavailable, "invite code service unavailable")
		return
	}

	switch r.Method {
	case http.MethodGet:
		items, err := h.invites.ListCodes(r.Context())
		if err != nil {
			writeError(w, http.StatusInternalServerError, "load invite codes failed")
			return
		}
		writeJSON(w, http.StatusOK, apiResponse{OK: true, Data: items})
	case http.MethodPost:
		var req invitecodes.CreateRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			writeError(w, http.StatusBadRequest, "invalid json body")
			return
		}

		item, err := h.invites.CreateCode(r.Context(), admin.Username, req)
		if err != nil {
			switch {
			case errors.Is(err, invitecodes.ErrCodeExists):
				writeError(w, http.StatusConflict, "invite code already exists")
			case errors.Is(err, invitecodes.ErrCodeInvalid):
				writeError(w, http.StatusBadRequest, "invite code is invalid")
			default:
				writeError(w, http.StatusInternalServerError, "create invite code failed")
			}
			return
		}

		_ = h.store.CreateAuditLog(r.Context(), admin, "invite_code.create", 0, map[string]any{
			"code":    item.Code,
			"maxUses": item.MaxUses,
			"note":    item.Note,
		})
		writeJSON(w, http.StatusOK, apiResponse{OK: true, Data: item})
	default:
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
	}
}

func (h *Handler) handleInviteCodeSettings(w http.ResponseWriter, r *http.Request, admin store.AdminUser) {
	if h.invites == nil {
		writeError(w, http.StatusServiceUnavailable, "invite code service unavailable")
		return
	}

	switch r.Method {
	case http.MethodGet:
		settings, found, err := h.store.GetInviteCodeSettings(r.Context())
		if err != nil {
			writeError(w, http.StatusInternalServerError, "load invite settings failed")
			return
		}
		if found {
			writeJSON(w, http.StatusOK, apiResponse{OK: true, Data: settings})
			return
		}

		cacheSettings, err := h.invites.GetSettings(r.Context())
		if err != nil {
			writeError(w, http.StatusInternalServerError, "load invite settings failed")
			return
		}
		if cacheSettings.UpdatedAt != 0 {
			if persisted, err := h.store.SaveInviteCodeSettings(r.Context(), cacheSettings.Enabled); err == nil {
				settings = persisted
			} else {
				settings = store.InviteCodeSettingsRecord{Enabled: cacheSettings.Enabled, UpdatedAt: cacheSettings.UpdatedAt}
			}
		} else {
			settings = store.InviteCodeSettingsRecord{Enabled: cacheSettings.Enabled, UpdatedAt: cacheSettings.UpdatedAt}
		}
		writeJSON(w, http.StatusOK, apiResponse{OK: true, Data: settings})
	case http.MethodPost:
		var request struct {
			Enabled bool `json:"enabled"`
		}
		if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
			writeError(w, http.StatusBadRequest, "invalid json body")
			return
		}

		settings, err := h.store.SaveInviteCodeSettings(r.Context(), request.Enabled)
		if err != nil {
			writeError(w, http.StatusInternalServerError, "save invite settings failed")
			return
		}
		cacheSettings, err := h.invites.UpdateSettings(r.Context(), request.Enabled)
		if err != nil {
			writeError(w, http.StatusInternalServerError, "save invite settings failed")
			return
		}
		if cacheSettings.UpdatedAt > settings.UpdatedAt {
			settings.UpdatedAt = cacheSettings.UpdatedAt
		}

		_ = h.store.CreateAuditLog(r.Context(), admin, "invite_code.settings.update", 0, map[string]any{
			"enabled": settings.Enabled,
		})
		writeJSON(w, http.StatusOK, apiResponse{OK: true, Data: settings})
	default:
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
	}
}

func (h *Handler) handleInviteCodeRoutes(w http.ResponseWriter, r *http.Request, admin store.AdminUser) {
	if h.invites == nil {
		writeError(w, http.StatusServiceUnavailable, "invite code service unavailable")
		return
	}

	path := strings.TrimPrefix(r.URL.Path, "/api/admin/invite-codes/")
	parts := strings.Split(strings.Trim(path, "/"), "/")
	if len(parts) == 0 || parts[0] == "" {
		writeError(w, http.StatusNotFound, "invite code not found")
		return
	}

	code := parts[0]
	if len(parts) == 1 {
		if r.Method != http.MethodGet {
			writeError(w, http.StatusMethodNotAllowed, "method not allowed")
			return
		}
		detail, err := h.invites.GetDetail(r.Context(), code)
		if err != nil {
			switch {
			case errors.Is(err, invitecodes.ErrCodeNotFound):
				writeError(w, http.StatusNotFound, "invite code not found")
			case errors.Is(err, invitecodes.ErrCodeInvalid):
				writeError(w, http.StatusBadRequest, "invite code is invalid")
			default:
				writeError(w, http.StatusInternalServerError, "load invite code detail failed")
			}
			return
		}
		writeJSON(w, http.StatusOK, apiResponse{OK: true, Data: detail})
		return
	}

	if r.Method != http.MethodPost {
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}

	var (
		enabled bool
		action  string
	)
	switch parts[1] {
	case "enable":
		enabled = true
		action = "invite_code.enable"
	case "disable":
		enabled = false
		action = "invite_code.disable"
	default:
		writeError(w, http.StatusNotFound, "route not found")
		return
	}

	item, err := h.invites.SetCodeEnabled(r.Context(), code, enabled)
	if err != nil {
		switch {
		case errors.Is(err, invitecodes.ErrCodeNotFound):
			writeError(w, http.StatusNotFound, "invite code not found")
		case errors.Is(err, invitecodes.ErrCodeInvalid):
			writeError(w, http.StatusBadRequest, "invite code is invalid")
		default:
			writeError(w, http.StatusInternalServerError, "update invite code failed")
		}
		return
	}

	_ = h.store.CreateAuditLog(r.Context(), admin, action, 0, map[string]any{
		"code":    item.Code,
		"enabled": item.Enabled,
	})
	writeJSON(w, http.StatusOK, apiResponse{OK: true, Data: item})
}
