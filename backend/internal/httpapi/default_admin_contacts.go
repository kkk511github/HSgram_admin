package httpapi

import (
	"encoding/json"
	"net/http"

	"hsgram-admin/backend/internal/store"
)

func (h *Handler) handleDefaultAdminContacts(w http.ResponseWriter, r *http.Request, admin store.AdminUser) {
	switch r.Method {
	case http.MethodGet:
		settings, _, err := h.store.GetDefaultAdminContactsSettings(r.Context())
		if err != nil {
			writeError(w, http.StatusInternalServerError, "load default admin contacts failed")
			return
		}
		writeJSON(w, http.StatusOK, apiResponse{OK: true, Data: settings})
	case http.MethodPut, http.MethodPost:
		var request struct {
			UserIDs []int64 `json:"userIds"`
		}
		if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
			writeError(w, http.StatusBadRequest, "invalid json body")
			return
		}

		settings, err := h.store.SaveDefaultAdminContactsSettings(r.Context(), request.UserIDs)
		if err != nil {
			writeError(w, http.StatusBadRequest, "save default admin contacts failed: "+err.Error())
			return
		}

		_ = h.store.CreateAuditLog(r.Context(), admin, "default_admin_contacts.update", 0, map[string]any{
			"userIds": settings.UserIDs,
		})
		writeJSON(w, http.StatusOK, apiResponse{OK: true, Data: settings})
	default:
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
	}
}
