package httpapi

import (
	"database/sql"
	"encoding/json"
	"errors"
	"net/http"
	"strconv"
	"strings"

	"hsgram-admin/backend/internal/automessage"
	"hsgram-admin/backend/internal/store"
)

type groupActorHandler func(http.ResponseWriter, *http.Request, int64)

func (h *Handler) requireGroupActor(next groupActorHandler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		userID, ok := actorUserIDFromRequest(r)
		if !ok {
			writeErrorCode(w, http.StatusUnauthorized, automessage.CodeNoPermission, "missing actor user id")
			return
		}
		next(w, r, userID)
	})
}

func actorUserIDFromRequest(r *http.Request) (int64, bool) {
	raw := strings.TrimSpace(r.Header.Get("X-HSgram-User-ID"))
	if raw == "" {
		authHeader := strings.TrimSpace(r.Header.Get("Authorization"))
		if strings.HasPrefix(strings.ToLower(authHeader), "user ") {
			raw = strings.TrimSpace(authHeader[len("User "):])
		}
	}
	if raw == "" {
		raw = strings.TrimSpace(r.URL.Query().Get("actor_user_id"))
	}
	id, err := strconv.ParseInt(raw, 10, 64)
	return id, err == nil && id > 0
}

func (h *Handler) handleGroupAutoMessageRoutes(w http.ResponseWriter, r *http.Request, actorUserID int64) {
	if h.autoMessages == nil {
		writeError(w, http.StatusServiceUnavailable, "auto message service unavailable")
		return
	}
	path := strings.TrimPrefix(r.URL.Path, "/groups/")
	parts := strings.Split(strings.Trim(path, "/"), "/")
	if len(parts) < 2 || parts[1] != "auto-messages" {
		writeError(w, http.StatusNotFound, "route not found")
		return
	}
	groupID, err := strconv.ParseInt(parts[0], 10, 64)
	if err != nil || groupID <= 0 {
		writeError(w, http.StatusBadRequest, "invalid group id")
		return
	}

	if len(parts) == 3 && parts[2] == "config" {
		switch r.Method {
		case http.MethodGet:
			h.handleAutoMessageGetConfig(w, r, actorUserID, groupID)
		case http.MethodPost:
			h.handleAutoMessageSaveConfig(w, r, actorUserID, groupID)
		default:
			writeError(w, http.StatusMethodNotAllowed, "method not allowed")
		}
		return
	}
	if len(parts) == 3 && parts[2] == "enable" {
		h.handleAutoMessageEnable(w, r, actorUserID, groupID)
		return
	}
	if len(parts) == 3 && parts[2] == "disable" {
		h.handleAutoMessageDisable(w, r, actorUserID, groupID)
		return
	}
	if len(parts) >= 3 && parts[2] == "items" {
		h.handleAutoMessageItemRoutes(w, r, actorUserID, groupID, parts[3:])
		return
	}
	if len(parts) == 3 && parts[2] == "logs" {
		h.handleAutoMessageLogs(w, r, actorUserID, groupID)
		return
	}

	writeError(w, http.StatusNotFound, "route not found")
}

func (h *Handler) handleAutoMessageGetConfig(w http.ResponseWriter, r *http.Request, actorUserID, groupID int64) {
	detail, err := h.autoMessages.GetConfig(r.Context(), actorUserID, groupID)
	if err != nil {
		writeAutoMessageError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, apiResponse{OK: true, Data: autoMessagePayload(detail)})
}

func (h *Handler) handleAutoMessageSaveConfig(w http.ResponseWriter, r *http.Request, actorUserID, groupID int64) {
	var request struct {
		Enabled         bool                     `json:"enabled"`
		IntervalMinutes int                      `json:"interval_minutes"`
		SendMode        string                   `json:"send_mode"`
		FirstSendMode   string                   `json:"first_send_mode"`
		AdminsCanManage *bool                    `json:"admins_can_manage"`
		Version         int                      `json:"version"`
		Items           []autoMessageItemRequest `json:"message_items"`
	}
	if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
		writeError(w, http.StatusBadRequest, "invalid json body")
		return
	}
	adminsCanManage := true
	if request.AdminsCanManage != nil {
		adminsCanManage = *request.AdminsCanManage
	}
	params := store.SaveAutoMessageConfigParams{
		Enabled:         request.Enabled,
		IntervalMinutes: request.IntervalMinutes,
		SendMode:        request.SendMode,
		FirstSendMode:   request.FirstSendMode,
		AdminsCanManage: adminsCanManage,
		Version:         request.Version,
		Items:           autoMessageItemInputs(request.Items),
	}
	detail, err := h.autoMessages.SaveConfig(r.Context(), actorUserID, groupID, params)
	if err != nil {
		writeAutoMessageError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, apiResponse{OK: true, Data: autoMessagePayload(detail)})
}

func (h *Handler) handleAutoMessageEnable(w http.ResponseWriter, r *http.Request, actorUserID, groupID int64) {
	if r.Method != http.MethodPost {
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	var request struct {
		IntervalMinutes int `json:"interval_minutes"`
	}
	_ = json.NewDecoder(r.Body).Decode(&request)
	detail, err := h.autoMessages.Enable(r.Context(), actorUserID, groupID, request.IntervalMinutes)
	if err != nil {
		writeAutoMessageError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, apiResponse{OK: true, Data: autoMessagePayload(detail)})
}

func (h *Handler) handleAutoMessageDisable(w http.ResponseWriter, r *http.Request, actorUserID, groupID int64) {
	if r.Method != http.MethodPost {
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	detail, err := h.autoMessages.Disable(r.Context(), actorUserID, groupID)
	if err != nil {
		writeAutoMessageError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, apiResponse{OK: true, Data: autoMessagePayload(detail)})
}

func (h *Handler) handleAutoMessageItemRoutes(w http.ResponseWriter, r *http.Request, actorUserID, groupID int64, parts []string) {
	if len(parts) == 0 {
		if r.Method != http.MethodPost {
			writeError(w, http.StatusMethodNotAllowed, "method not allowed")
			return
		}
		var request autoMessageItemRequest
		if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
			writeError(w, http.StatusBadRequest, "invalid json body")
			return
		}
		detail, err := h.autoMessages.AddItem(r.Context(), actorUserID, groupID, autoMessageItemInput(request))
		if err != nil {
			writeAutoMessageError(w, err)
			return
		}
		writeJSON(w, http.StatusOK, apiResponse{OK: true, Data: autoMessagePayload(detail)})
		return
	}
	if len(parts) == 1 && parts[0] == "sort" {
		h.handleAutoMessageSortItems(w, r, actorUserID, groupID)
		return
	}

	itemID, err := strconv.ParseInt(parts[0], 10, 64)
	if err != nil || itemID <= 0 {
		writeError(w, http.StatusBadRequest, "invalid item id")
		return
	}
	switch r.Method {
	case http.MethodPut:
		var request autoMessageItemRequest
		if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
			writeError(w, http.StatusBadRequest, "invalid json body")
			return
		}
		detail, err := h.autoMessages.UpdateItem(r.Context(), actorUserID, groupID, itemID, autoMessageItemInput(request))
		if err != nil {
			writeAutoMessageError(w, err)
			return
		}
		writeJSON(w, http.StatusOK, apiResponse{OK: true, Data: autoMessagePayload(detail)})
	case http.MethodDelete:
		detail, err := h.autoMessages.DeleteItem(r.Context(), actorUserID, groupID, itemID)
		if err != nil {
			writeAutoMessageError(w, err)
			return
		}
		writeJSON(w, http.StatusOK, apiResponse{OK: true, Data: autoMessagePayload(detail)})
	default:
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
	}
}

func (h *Handler) handleAutoMessageSortItems(w http.ResponseWriter, r *http.Request, actorUserID, groupID int64) {
	if r.Method != http.MethodPost {
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	var request struct {
		ItemIDs []int64 `json:"item_ids"`
	}
	if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
		writeError(w, http.StatusBadRequest, "invalid json body")
		return
	}
	detail, err := h.autoMessages.SortItems(r.Context(), actorUserID, groupID, request.ItemIDs)
	if err != nil {
		writeAutoMessageError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, apiResponse{OK: true, Data: autoMessagePayload(detail)})
}

func (h *Handler) handleAutoMessageLogs(w http.ResponseWriter, r *http.Request, actorUserID, groupID int64) {
	if r.Method != http.MethodGet {
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	page := parseIntWithDefault(r.URL.Query().Get("page"), 1)
	pageSize := parseIntWithDefault(firstNonEmpty(r.URL.Query().Get("page_size"), r.URL.Query().Get("pageSize")), 20)
	logs, err := h.autoMessages.Logs(r.Context(), actorUserID, groupID, page, pageSize)
	if err != nil {
		writeAutoMessageError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, apiResponse{OK: true, Data: logs})
}

type autoMessageItemRequest struct {
	ID          int64  `json:"id"`
	SortOrder   int    `json:"sort_order"`
	Content     string `json:"content"`
	ContentType string `json:"content_type"`
	Enabled     *bool  `json:"enabled"`
}

func autoMessageItemInputs(items []autoMessageItemRequest) []store.AutoMessageItemInput {
	out := make([]store.AutoMessageItemInput, 0, len(items))
	for _, item := range items {
		out = append(out, autoMessageItemInput(item))
	}
	return out
}

func autoMessageItemInput(item autoMessageItemRequest) store.AutoMessageItemInput {
	enabled := true
	if item.Enabled != nil {
		enabled = *item.Enabled
	}
	return store.AutoMessageItemInput{
		ID:          item.ID,
		SortOrder:   item.SortOrder,
		Content:     item.Content,
		ContentType: item.ContentType,
		Enabled:     enabled,
	}
}

func autoMessagePayload(detail *store.AutoMessageConfigDetail) map[string]any {
	if detail == nil {
		return map[string]any{}
	}
	return map[string]any{
		"enabled":            detail.Config.Enabled,
		"interval_minutes":   detail.Config.IntervalMinutes,
		"send_mode":          detail.Config.SendMode,
		"first_send_mode":    detail.Config.FirstSendMode,
		"admins_can_manage":  detail.Config.AdminsCanManage,
		"current_index":      detail.Config.CurrentIndex,
		"next_send_at":       detail.Config.NextSendAt,
		"last_send_at":       detail.Config.LastSendAt,
		"last_send_status":   detail.Config.LastSendStatus,
		"last_error_code":    detail.Config.LastErrorCode,
		"last_error_message": detail.Config.LastErrorMessage,
		"version":            detail.Config.Version,
		"message_items":      detail.Items,
		"enabled_item_count": detail.EnabledItemCount,
		"next_item":          detail.NextItem,
		"limits": map[string]any{
			"min_interval_minutes": automessage.MinIntervalMinutes,
			"max_interval_minutes": automessage.MaxIntervalMinutes,
			"max_items":            automessage.MaxItems,
			"max_content_length":   automessage.MaxContentLength,
		},
	}
}

func writeAutoMessageError(w http.ResponseWriter, err error) {
	var autoErr *automessage.Error
	if errors.As(err, &autoErr) {
		status := http.StatusBadRequest
		switch autoErr.Code {
		case automessage.CodeNoPermission:
			status = http.StatusForbidden
		case automessage.CodeConfigNotFound, automessage.CodeItemNotFound, automessage.CodeGroupDissolved:
			status = http.StatusNotFound
		case automessage.CodeConflict:
			status = http.StatusConflict
		case automessage.CodeLockFailed, automessage.CodeSendFailed:
			status = http.StatusServiceUnavailable
		}
		writeErrorCode(w, status, autoErr.Code, autoErr.Message)
		return
	}
	if errors.Is(err, sql.ErrNoRows) {
		writeErrorCode(w, http.StatusNotFound, automessage.CodeConfigNotFound, "resource not found")
		return
	}
	writeError(w, http.StatusInternalServerError, "auto message operation failed")
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return value
		}
	}
	return ""
}
