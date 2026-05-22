package httpapi

import (
	"net/http"
	"strconv"
	"strings"

	"hsgram-admin/backend/internal/store"
)

func (h *Handler) handleAntiSpamFalsePositives(w http.ResponseWriter, r *http.Request, admin store.AdminUser) {
	if r.Method != http.MethodGet {
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	if h.store == nil {
		writeError(w, http.StatusServiceUnavailable, "user store unavailable")
		return
	}

	query := r.URL.Query()
	channelID, ok := parseOptionalInt64Query(firstQueryValue(query.Get("channel_id"), query.Get("channelId")))
	if !ok {
		writeError(w, http.StatusBadRequest, "invalid channel id")
		return
	}
	reporterUserID, ok := parseOptionalInt64Query(firstQueryValue(query.Get("reporter_user_id"), query.Get("reporterUserId")))
	if !ok {
		writeError(w, http.StatusBadRequest, "invalid reporter user id")
		return
	}

	items, err := h.store.ListAntiSpamFalsePositives(r.Context(), store.AntiSpamFalsePositiveFilter{
		ChannelID:      channelID,
		ReporterUserID: reporterUserID,
		Limit:          parseIntWithDefault(query.Get("limit"), 50),
		Offset:         parseIntWithDefault(query.Get("offset"), 0),
	})
	if err != nil {
		writeError(w, http.StatusInternalServerError, "load anti-spam false positives failed")
		return
	}

	writeJSON(w, http.StatusOK, apiResponse{
		OK:   true,
		Data: items,
	})
}

func firstQueryValue(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return value
		}
	}
	return ""
}

func parseOptionalInt64Query(raw string) (int64, bool) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return 0, true
	}
	value, err := strconv.ParseInt(raw, 10, 64)
	if err != nil || value <= 0 {
		return 0, false
	}
	return value, true
}
