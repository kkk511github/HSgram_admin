package httpapi

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"math"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/teamgram/teamgram-server/pkg/riskctrl"
	"hsgram-admin/backend/internal/auth"
	"hsgram-admin/backend/internal/authsessionrpc"
	"hsgram-admin/backend/internal/broadcast"
	"hsgram-admin/backend/internal/gatewayrpc"
	"hsgram-admin/backend/internal/invitecodes"
	"hsgram-admin/backend/internal/messenger"
	"hsgram-admin/backend/internal/risk"
	"hsgram-admin/backend/internal/statusrpc"
	stickerimport "hsgram-admin/backend/internal/stickers"
	"hsgram-admin/backend/internal/store"
	"hsgram-admin/backend/internal/syncrpc"
	"hsgram-admin/backend/webui"
)

type Handler struct {
	tokens       *auth.Manager
	store        *store.Store
	broadcasts   broadcastService
	authsessions authsessionService
	sync         syncService
	status       statusService
	gateway      gatewayService
	risk         *risk.Service
	invites      *invitecodes.Service
	msg          messageService
	stickers     stickerService
	updates      UpdateConfig
	web          http.Handler
	releases     http.Handler
	logins       *loginLimiter
}

type messageService interface {
	SendTextMessage(ctx context.Context, senderUserID, targetUserID int64, text string) error
}

type stickerService interface {
	ImportTelegramSet(ctx context.Context, req stickerimport.ImportRequest) (*stickerimport.ImportResult, error)
	ListSets(ctx context.Context) ([]stickerimport.SetSummary, error)
	GetSet(ctx context.Context, id int64) (*stickerimport.SetDetail, error)
	DisableSet(ctx context.Context, id int64) error
}

type broadcastService interface {
	Preview(ctx context.Context, req broadcast.Request) (store.BroadcastPreview, store.BroadcastTargetSpec, error)
	Enqueue(ctx context.Context, admin store.AdminUser, req broadcast.Request) (*store.BroadcastRecord, store.BroadcastPreview, error)
}

type authsessionService interface {
	ResetAllAuthorizations(ctx context.Context, userID int64) ([]int64, error)
}

type syncService interface {
	PushUserPopup(ctx context.Context, userID int64, message string) error
	PushResetAuthorization(ctx context.Context, userID int64, authKeyIDs []int64) error
}

type statusService interface {
	GetOnlineAuthKeys(ctx context.Context, userID int64) ([]int64, error)
}

type gatewayService interface {
	ForceDisconnectAuthKeys(ctx context.Context, authKeyIDs []int64) error
}

type apiResponse struct {
	OK    bool   `json:"ok"`
	Data  any    `json:"data,omitempty"`
	Code  string `json:"code,omitempty"`
	Error string `json:"error,omitempty"`
}

type featureFlags struct {
	Broadcasts         bool `json:"broadcasts"`
	Support            bool `json:"support"`
	FullSessionRevoke  bool `json:"fullSessionRevoke"`
	RealtimeDisconnect bool `json:"realtimeDisconnect"`
}

type dependencyState struct {
	Name   string `json:"name"`
	Status string `json:"status"`
	Reason string `json:"reason,omitempty"`
}

func New(tokens *auth.Manager, userStore *store.Store, broadcasts *broadcast.Service, authsessions *authsessionrpc.Client, syncClient *syncrpc.Client, statusClient *statusrpc.Client, gatewayClient *gatewayrpc.Client, riskService *risk.Service, inviteService *invitecodes.Service, msgClient *messenger.Client, stickerSvc stickerService, updates UpdateConfig) http.Handler {
	handler := &Handler{
		tokens:   tokens,
		store:    userStore,
		risk:     riskService,
		invites:  inviteService,
		updates:  updates,
		web:      webui.NewHandler(),
		releases: http.StripPrefix("/releases/", http.FileServer(http.Dir(updates.ReleasesDir))),
		logins:   newLoginLimiter(5, 15*time.Minute, 15*time.Minute),
	}
	if broadcasts != nil {
		handler.broadcasts = broadcasts
	}
	if authsessions != nil {
		handler.authsessions = authsessions
	}
	if syncClient != nil {
		handler.sync = syncClient
	}
	if statusClient != nil {
		handler.status = statusClient
	}
	if gatewayClient != nil {
		handler.gateway = gatewayClient
	}
	if msgClient != nil {
		handler.msg = msgClient
	}
	if stickerSvc != nil {
		handler.stickers = stickerSvc
	}

	mux := http.NewServeMux()
	mux.HandleFunc("/api/healthz", handler.handleHealthz)
	mux.HandleFunc("/api/updates/android/latest", handler.handleAndroidUpdateLatest)
	mux.HandleFunc("/api/updates/pc/latest", handler.handlePCUpdateLatest)
	mux.HandleFunc("/api/updates/windows/latest", handler.handlePCUpdateLatest)
	mux.HandleFunc("/api/admin/login", handler.handleLogin)
	mux.Handle("/api/admin/me", handler.requireAuth(handler.handleMe))
	mux.Handle("/api/admin/releases/upload", handler.requireSuperAdmin(handler.handleReleaseUpload))
	mux.Handle("/api/admin/releases", handler.requireSuperAdmin(handler.handleReleaseRoutes))
	mux.Handle("/api/admin/releases/", handler.requireSuperAdmin(handler.handleReleaseRoutes))
	mux.Handle("/api/admin/users", handler.requireAuth(handler.handleUsers))
	mux.Handle("/api/admin/users/", handler.requireAuth(handler.handleUserRoutes))
	mux.Handle("/api/admin/risk/settings", handler.requireSuperAdmin(handler.handleRiskSettings))
	mux.Handle("/api/admin/risk/signup-ip-stats", handler.requireSuperAdmin(handler.handleRiskSignupIPStats))
	mux.Handle("/api/admin/invite-codes", handler.requireSuperAdmin(handler.handleInviteCodes))
	mux.Handle("/api/admin/invite-code-settings", handler.requireSuperAdmin(handler.handleInviteCodeSettings))
	mux.Handle("/api/admin/invite-codes/", handler.requireSuperAdmin(handler.handleInviteCodeRoutes))
	mux.Handle("/api/admin/default-admin-contacts", handler.requireSuperAdmin(handler.handleDefaultAdminContacts))
	mux.Handle("/api/admin/support/threads", handler.requireAuth(handler.handleSupportThreads))
	mux.Handle("/api/admin/support/threads/", handler.requireAuth(handler.handleSupportThreadRoutes))
	mux.Handle("/api/admin/broadcasts/preview", handler.requireSuperAdmin(handler.handleBroadcastPreview))
	mux.Handle("/api/admin/broadcasts", handler.requireSuperAdmin(handler.handleBroadcastRoutes))
	mux.Handle("/api/admin/broadcasts/", handler.requireSuperAdmin(handler.handleBroadcastRoutes))
	mux.Handle("/api/admin/stickers/import", handler.requireSuperAdmin(handler.handleStickerImport))
	mux.Handle("/api/admin/stickers/sets", handler.requireSuperAdmin(handler.handleStickerSets))
	mux.Handle("/api/admin/stickers/sets/", handler.requireSuperAdmin(handler.handleStickerSetRoutes))
	mux.HandleFunc("/api/", handler.handleAPINotFound)
	mux.HandleFunc("/td/current", handler.handleTDesktopCurrent)
	mux.Handle("/releases/", handler.releases)
	mux.Handle("/", handler.web)
	return withSecurityHeaders(withJSONDefaults(mux))
}

func (h *Handler) handleAPINotFound(w http.ResponseWriter, r *http.Request) {
	writeError(w, http.StatusNotFound, "route not found")
}

func (h *Handler) handleHealthz(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}

	writeJSON(w, http.StatusOK, apiResponse{
		OK: true,
		Data: map[string]any{
			"status":       h.healthStatus(),
			"features":     h.features(),
			"dependencies": h.dependencies(),
		},
	})
}

func (h *Handler) handleLogin(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}

	var request struct {
		Username string `json:"username"`
		Password string `json:"password"`
	}
	if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
		writeError(w, http.StatusBadRequest, "invalid json body")
		return
	}

	ip := clientIP(r)
	if retryAfter, ok := h.logins.Allow(ip, request.Username); !ok {
		w.Header().Set("Retry-After", formatRetryAfter(retryAfter))
		writeError(w, http.StatusTooManyRequests, "too many login attempts, try again later")
		return
	}

	admin, err := h.store.AuthenticateAdmin(r.Context(), strings.TrimSpace(request.Username), request.Password)
	if err != nil {
		statusCode := http.StatusInternalServerError
		message := "login failed"
		if errors.Is(err, store.ErrInvalidCredentials) {
			h.logins.RecordFailure(ip, request.Username)
			statusCode = http.StatusUnauthorized
			message = "invalid username or password"
		}
		writeError(w, statusCode, message)
		return
	}
	h.logins.RecordSuccess(ip, request.Username)

	token, expiresAt, err := h.tokens.Issue(admin.ID, admin.Username, admin.Role)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "create token failed")
		return
	}

	_ = h.store.CreateAuditLog(r.Context(), *admin, "admin.login", 0, nil)

	writeJSON(w, http.StatusOK, apiResponse{
		OK: true,
		Data: map[string]any{
			"token":        token,
			"expiresAt":    expiresAt,
			"admin":        admin,
			"features":     h.features(),
			"dependencies": h.dependencies(),
		},
	})
}

func (h *Handler) handleMe(w http.ResponseWriter, r *http.Request, admin store.AdminUser) {
	writeJSON(w, http.StatusOK, apiResponse{
		OK: true,
		Data: map[string]any{
			"admin":        admin,
			"features":     h.features(),
			"dependencies": h.dependencies(),
		},
	})
}

func (h *Handler) handleUsers(w http.ResponseWriter, r *http.Request, admin store.AdminUser) {
	if r.Method != http.MethodGet {
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}

	query := r.URL.Query()
	filter := store.UserListFilter{
		Query:    query.Get("q"),
		Page:     parseIntWithDefault(query.Get("page"), 1),
		PageSize: parseIntWithDefault(query.Get("pageSize"), 20),
	}
	if value, ok := parseOptionalBool(query.Get("restricted")); ok {
		filter.Restricted = &value
	}
	if value, ok := parseOptionalBool(query.Get("deleted")); ok {
		filter.Deleted = &value
	}

	result, err := h.store.ListUsers(r.Context(), filter)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "load users failed")
		return
	}

	writeJSON(w, http.StatusOK, apiResponse{
		OK:   true,
		Data: result,
	})
}

func (h *Handler) handleUserRoutes(w http.ResponseWriter, r *http.Request, admin store.AdminUser) {
	path := strings.TrimPrefix(r.URL.Path, "/api/admin/users/")
	parts := strings.Split(strings.Trim(path, "/"), "/")
	if len(parts) == 0 || parts[0] == "" {
		writeError(w, http.StatusNotFound, "user not found")
		return
	}

	userID, err := strconv.ParseInt(parts[0], 10, 64)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid user id")
		return
	}

	if len(parts) == 1 {
		h.handleUserDetail(w, r, admin, userID)
		return
	}

	switch parts[1] {
	case "ban":
		h.handleBan(w, r, admin, userID)
	case "unban":
		h.handleUnban(w, r, admin, userID)
	case "kick-sessions":
		h.handleKickSessions(w, r, admin, userID)
	default:
		writeError(w, http.StatusNotFound, "route not found")
	}
}

func (h *Handler) handleUserDetail(w http.ResponseWriter, r *http.Request, admin store.AdminUser, userID int64) {
	if r.Method != http.MethodGet {
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}

	detail, err := h.store.GetUserDetail(r.Context(), userID)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			writeError(w, http.StatusNotFound, "user not found")
			return
		}
		writeError(w, http.StatusInternalServerError, "load user detail failed")
		return
	}

	writeJSON(w, http.StatusOK, apiResponse{
		OK:   true,
		Data: detail,
	})
}

func mergeAuthKeyIDs(groups ...[]int64) []int64 {
	seen := make(map[int64]struct{})
	merged := make([]int64, 0)
	for _, group := range groups {
		for _, keyID := range group {
			if keyID == 0 {
				continue
			}
			if _, ok := seen[keyID]; ok {
				continue
			}
			seen[keyID] = struct{}{}
			merged = append(merged, keyID)
		}
	}
	return merged
}

func (h *Handler) onlineAuthKeyIDs(ctx context.Context, userID int64) ([]int64, []string) {
	if h.status == nil {
		return nil, nil
	}
	ids, err := h.status.GetOnlineAuthKeys(ctx, userID)
	if err != nil {
		log.Printf("admin-api: load online sessions for %d failed: %v", userID, err)
		return nil, []string{"status_rpc_failed"}
	}
	return mergeAuthKeyIDs(ids), nil
}

func (h *Handler) sessionOperationDegraded() []string {
	var degraded []string
	if h.status == nil {
		degraded = append(degraded, "status_rpc_unavailable")
	}
	if h.sync == nil {
		degraded = append(degraded, "sync_rpc_unavailable")
	}
	if h.gateway == nil {
		degraded = append(degraded, "gateway_rpc_unavailable")
	}
	if h.authsessions == nil {
		degraded = append(degraded, "authsession_rpc_unavailable")
	}
	return degraded
}

func (h *Handler) revokeUserSessions(ctx context.Context, userID int64) ([]int64, []string, error) {
	if h.authsessions != nil {
		keyIDs, err := h.authsessions.ResetAllAuthorizations(ctx, userID)
		return keyIDs, nil, err
	}

	if h.store == nil {
		return nil, []string{"authsession_rpc_unavailable"}, errors.New("user store unavailable")
	}
	if _, err := h.store.KickUserSessions(ctx, userID); err != nil {
		return nil, []string{"authsession_rpc_unavailable"}, err
	}
	return nil, []string{"authsession_rpc_unavailable"}, nil
}

func (h *Handler) forceDisconnectAuthKeys(ctx context.Context, keyIDs []int64) []string {
	if h.gateway == nil || len(keyIDs) == 0 {
		return nil
	}
	if err := h.gateway.ForceDisconnectAuthKeys(ctx, keyIDs); err != nil {
		log.Printf("admin-api: force disconnect auth keys %v failed: %v", keyIDs, err)
		return []string{"gateway_rpc_failed"}
	}
	return nil
}

func (h *Handler) pushAdminPopup(ctx context.Context, userID int64, message string) []string {
	if h.sync == nil || strings.TrimSpace(message) == "" {
		return nil
	}
	if err := h.sync.PushUserPopup(ctx, userID, message); err != nil {
		log.Printf("admin-api: push popup to %d failed: %v", userID, err)
		return []string{"sync_rpc_popup_failed"}
	}
	return nil
}

const (
	banPopupMessage   = "该账号已被封禁，如有疑问请联系客服"
	unbanPopupMessage = "该账号已解封，可以正常使用"
)

func kickPopupMessage(blockSeconds int) string {
	if blockSeconds <= 0 {
		return "您已被管理员踢下线"
	}
	minutes := int(math.Ceil(float64(blockSeconds) / 60.0))
	if minutes < 1 {
		minutes = 1
	}
	return fmt.Sprintf("您已被管理员踢下线，%d 分钟内暂时无法登录", minutes)
}

func (h *Handler) pushResetAuthorization(ctx context.Context, userID int64, keyIDs []int64) []string {
	if h.sync == nil || len(keyIDs) == 0 {
		return nil
	}
	if err := h.sync.PushResetAuthorization(ctx, userID, keyIDs); err != nil {
		log.Printf("admin-api: push reset authorization to %d failed: %v", userID, err)
		return []string{"sync_rpc_reset_failed"}
	}
	return nil
}

func (h *Handler) currentRiskSettings(ctx context.Context) riskctrl.Settings {
	if h.risk == nil {
		return riskctrl.DefaultSettings()
	}
	settings, err := h.risk.GetSettings(ctx)
	if err != nil {
		log.Printf("admin-api: load risk settings failed: %v", err)
		return riskctrl.DefaultSettings()
	}
	return settings
}

func (h *Handler) handleBan(w http.ResponseWriter, r *http.Request, admin store.AdminUser, userID int64) {
	if r.Method != http.MethodPost {
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}

	var request struct {
		Reason string `json:"reason"`
	}
	if err := json.NewDecoder(r.Body).Decode(&request); err != nil && !errors.Is(err, io.EOF) {
		writeError(w, http.StatusBadRequest, "invalid json body")
		return
	}

	if err := h.store.SetUserRestricted(r.Context(), userID, true, request.Reason); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			writeError(w, http.StatusNotFound, "user not found")
			return
		}
		writeError(w, http.StatusInternalServerError, "ban user failed")
		return
	}

	onlineKeyIDs, statusDegraded := h.onlineAuthKeyIDs(r.Context(), userID)
	popupDegraded := h.pushAdminPopup(r.Context(), userID, banPopupMessage)
	resetKeyIDs, revokeDegraded, err := h.revokeUserSessions(r.Context(), userID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "ban user failed")
		return
	}
	keyIDs := mergeAuthKeyIDs(onlineKeyIDs, resetKeyIDs)
	resetDegraded := h.pushResetAuthorization(r.Context(), userID, keyIDs)
	var disconnectDegraded []string
	if len(keyIDs) > 0 {
		time.Sleep(350 * time.Millisecond)
		disconnectDegraded = h.forceDisconnectAuthKeys(r.Context(), keyIDs)
	}
	degraded := uniqueStrings(append(append(append(append(append(h.sessionOperationDegraded(), statusDegraded...), popupDegraded...), revokeDegraded...), resetDegraded...), disconnectDegraded...))

	_ = h.store.CreateAuditLog(r.Context(), admin, "user.ban", userID, map[string]any{
		"reason":        request.Reason,
		"affected":      len(keyIDs),
		"forced_logout": len(degraded) == 0,
		"degraded":      degraded,
	})

	message := "该账号已被封禁并强制下线"
	if len(degraded) > 0 {
		message = "该账号已被封禁，会话处理处于降级状态"
	}
	writeJSON(w, http.StatusOK, apiResponse{OK: true, Data: map[string]any{
		"affected":          len(keyIDs),
		"message":           message,
		"degraded":          degraded,
		"fullyDisconnected": len(degraded) == 0,
	}})
}

func (h *Handler) handleUnban(w http.ResponseWriter, r *http.Request, admin store.AdminUser, userID int64) {
	if r.Method != http.MethodPost {
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}

	if err := h.store.SetUserRestricted(r.Context(), userID, false, ""); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			writeError(w, http.StatusNotFound, "user not found")
			return
		}
		writeError(w, http.StatusInternalServerError, "unban user failed")
		return
	}

	if h.risk != nil {
		_ = h.risk.ClearKickLoginBlock(r.Context(), userID)
	}

	popupDegraded := h.pushAdminPopup(r.Context(), userID, unbanPopupMessage)

	_ = h.store.CreateAuditLog(r.Context(), admin, "user.unban", userID, map[string]any{
		"degraded": popupDegraded,
	})

	writeUnbanSuccess(w, popupDegraded)
}

func writeUnbanSuccess(w http.ResponseWriter, degraded []string) {
	writeJSON(w, http.StatusOK, apiResponse{OK: true, Data: map[string]any{
		"message":        "该账号已解封",
		"popupDelivered": len(degraded) == 0,
		"degraded":       degraded,
	}})
}

func (h *Handler) handleKickSessions(w http.ResponseWriter, r *http.Request, admin store.AdminUser, userID int64) {
	if r.Method != http.MethodPost {
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}

	settings := h.currentRiskSettings(r.Context())
	if h.risk != nil {
		_ = h.risk.SetKickLoginBlock(r.Context(), userID, time.Duration(settings.KickLoginBlockSeconds)*time.Second)
	}
	onlineKeyIDs, statusDegraded := h.onlineAuthKeyIDs(r.Context(), userID)
	popupDegraded := h.pushAdminPopup(r.Context(), userID, kickPopupMessage(settings.KickLoginBlockSeconds))
	resetKeyIDs, revokeDegraded, err := h.revokeUserSessions(r.Context(), userID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "kick sessions failed")
		return
	}
	keyIDs := mergeAuthKeyIDs(onlineKeyIDs, resetKeyIDs)
	resetDegraded := h.pushResetAuthorization(r.Context(), userID, keyIDs)
	var disconnectDegraded []string
	if len(keyIDs) > 0 {
		time.Sleep(350 * time.Millisecond)
		disconnectDegraded = h.forceDisconnectAuthKeys(r.Context(), keyIDs)
	}
	degraded := uniqueStrings(append(append(append(append(append(h.sessionOperationDegraded(), statusDegraded...), popupDegraded...), revokeDegraded...), resetDegraded...), disconnectDegraded...))

	_ = h.store.CreateAuditLog(r.Context(), admin, "user.kick_sessions", userID, map[string]any{
		"affected":            len(keyIDs),
		"login_block_seconds": settings.KickLoginBlockSeconds,
		"degraded":            degraded,
	})

	message := "您已被管理员踢下线，五分钟内限制登录"
	if len(degraded) > 0 {
		message = "已写入登录限制，会话踢下线处于降级状态"
	}
	writeJSON(w, http.StatusOK, apiResponse{
		OK: true,
		Data: map[string]any{
			"affected":          len(keyIDs),
			"loginBlockSeconds": settings.KickLoginBlockSeconds,
			"message":           message,
			"degraded":          degraded,
			"fullyDisconnected": len(degraded) == 0,
		},
	})
}

func (h *Handler) handleRiskSettings(w http.ResponseWriter, r *http.Request, admin store.AdminUser) {
	if h.risk == nil {
		writeError(w, http.StatusServiceUnavailable, "risk control is not configured")
		return
	}

	switch r.Method {
	case http.MethodGet:
		settings, err := h.risk.GetSettings(r.Context())
		if err != nil {
			writeError(w, http.StatusInternalServerError, "load risk settings failed")
			return
		}
		writeJSON(w, http.StatusOK, apiResponse{OK: true, Data: settings})
	case http.MethodPost:
		var request riskctrl.Settings
		if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
			writeError(w, http.StatusBadRequest, "invalid json body")
			return
		}
		settings, err := h.risk.UpdateSettings(r.Context(), request)
		if err != nil {
			writeError(w, http.StatusInternalServerError, "save risk settings failed")
			return
		}
		_ = h.store.CreateAuditLog(r.Context(), admin, "risk.settings.update", 0, map[string]any{
			"kick_login_block_seconds": settings.KickLoginBlockSeconds,
			"signup_ip_daily_limit":    settings.SignupIPDailyLimit,
		})
		writeJSON(w, http.StatusOK, apiResponse{OK: true, Data: settings})
	default:
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
	}
}

func (h *Handler) handleRiskSignupIPStats(w http.ResponseWriter, r *http.Request, admin store.AdminUser) {
	if r.Method != http.MethodGet {
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	if h.risk == nil {
		writeError(w, http.StatusServiceUnavailable, "risk control is not configured")
		return
	}

	limit := parseIntWithDefault(r.URL.Query().Get("limit"), 50)
	items, err := h.risk.GetTodaySignupStats(r.Context(), limit)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "load signup ip stats failed")
		return
	}

	writeJSON(w, http.StatusOK, apiResponse{OK: true, Data: map[string]any{
		"date":  time.Now().Format("2006-01-02"),
		"items": items,
	}})
}

func (h *Handler) requireAuth(next func(http.ResponseWriter, *http.Request, store.AdminUser)) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		authHeader := strings.TrimSpace(r.Header.Get("Authorization"))
		if !strings.HasPrefix(strings.ToLower(authHeader), "bearer ") {
			writeError(w, http.StatusUnauthorized, "missing bearer token")
			return
		}

		tokenString := strings.TrimSpace(authHeader[len("Bearer "):])
		claims, err := h.tokens.Parse(tokenString)
		if err != nil {
			writeError(w, http.StatusUnauthorized, "invalid token")
			return
		}

		next(w, r, store.AdminUser{
			ID:       claims.AdminID,
			Username: claims.Username,
			Role:     claims.Role,
		})
	})
}

func (h *Handler) requireSuperAdmin(next func(http.ResponseWriter, *http.Request, store.AdminUser)) http.Handler {
	return h.requireAuth(func(w http.ResponseWriter, r *http.Request, admin store.AdminUser) {
		if admin.Role != "super_admin" {
			writeError(w, http.StatusForbidden, "super admin role required")
			return
		}
		next(w, r, admin)
	})
}

func (h *Handler) features() featureFlags {
	return featureFlags{
		Broadcasts:         h.broadcasts != nil,
		Support:            h.msg != nil,
		FullSessionRevoke:  h.authsessions != nil,
		RealtimeDisconnect: h.status != nil && h.sync != nil && h.gateway != nil,
	}
}

func (h *Handler) dependencies() []dependencyState {
	states := []dependencyState{
		dependencyAvailable("message_rpc", h.msg != nil, "ADMIN_MSG_RPC_ADDR is not configured or the client failed to initialize"),
		dependencyAvailable("broadcast_service", h.broadcasts != nil, "broadcast service requires ADMIN_ENABLE_BROADCASTS=true and message RPC"),
		dependencyAvailable("authsession_rpc", h.authsessions != nil, "ADMIN_AUTHSESSION_RPC_ADDR is not configured"),
		dependencyAvailable("sync_rpc", h.sync != nil, "ADMIN_SYNC_RPC_ADDR is not configured"),
		dependencyAvailable("status_rpc", h.status != nil, "ADMIN_STATUS_RPC_ADDR is not configured"),
		dependencyAvailable("gateway_rpc", h.gateway != nil, "ADMIN_GATEWAY_RPC_ADDR is not configured"),
	}
	return states
}

func dependencyAvailable(name string, ok bool, reason string) dependencyState {
	if ok {
		return dependencyState{Name: name, Status: "available"}
	}
	return dependencyState{Name: name, Status: "degraded", Reason: reason}
}

func (h *Handler) healthStatus() string {
	for _, dep := range h.dependencies() {
		if dep.Status == "degraded" {
			return "degraded"
		}
	}
	return "ok"
}

func uniqueStrings(items []string) []string {
	seen := make(map[string]struct{}, len(items))
	out := make([]string, 0, len(items))
	for _, item := range items {
		item = strings.TrimSpace(item)
		if item == "" {
			continue
		}
		if _, ok := seen[item]; ok {
			continue
		}
		seen[item] = struct{}{}
		out = append(out, item)
	}
	return out
}

func withJSONDefaults(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasPrefix(r.URL.Path, "/api/") {
			w.Header().Set("Content-Type", "application/json; charset=utf-8")
		}
		next.ServeHTTP(w, r)
	})
}

func withSecurityHeaders(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("X-Content-Type-Options", "nosniff")
		w.Header().Set("X-Frame-Options", "DENY")
		w.Header().Set("Referrer-Policy", "strict-origin-when-cross-origin")
		w.Header().Set("Cross-Origin-Opener-Policy", "same-origin")
		w.Header().Set("Cross-Origin-Resource-Policy", "same-origin")
		next.ServeHTTP(w, r)
	})
}

func writeJSON(w http.ResponseWriter, statusCode int, payload apiResponse) {
	w.WriteHeader(statusCode)
	_ = json.NewEncoder(w).Encode(payload)
}

func writeError(w http.ResponseWriter, statusCode int, message string) {
	writeErrorCode(w, statusCode, "", message)
}

func writeErrorCode(w http.ResponseWriter, statusCode int, code string, message string) {
	writeJSON(w, statusCode, apiResponse{
		OK:    false,
		Code:  code,
		Error: message,
	})
}

func parseIntWithDefault(raw string, fallback int) int {
	value, err := strconv.Atoi(raw)
	if err != nil || value <= 0 {
		return fallback
	}
	return value
}

func parseOptionalBool(raw string) (bool, bool) {
	switch strings.ToLower(strings.TrimSpace(raw)) {
	case "true", "1", "yes":
		return true, true
	case "false", "0", "no":
		return false, true
	default:
		return false, false
	}
}
