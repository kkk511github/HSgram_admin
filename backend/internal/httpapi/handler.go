package httpapi

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"io"
	"log"
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
	"hsgram-admin/backend/internal/risk"
	"hsgram-admin/backend/internal/statusrpc"
	"hsgram-admin/backend/internal/store"
	"hsgram-admin/backend/internal/syncrpc"
	"hsgram-admin/backend/webui"
)

type Handler struct {
	tokens       *auth.Manager
	store        *store.Store
	broadcasts   *broadcast.Service
	authsessions *authsessionrpc.Client
	sync         *syncrpc.Client
	status       *statusrpc.Client
	gateway      *gatewayrpc.Client
	risk         *risk.Service
	invites      *invitecodes.Service
	updates      UpdateConfig
	web          http.Handler
	releases     http.Handler
	logins       *loginLimiter
}

type apiResponse struct {
	OK    bool   `json:"ok"`
	Data  any    `json:"data,omitempty"`
	Error string `json:"error,omitempty"`
}

type featureFlags struct {
	Broadcasts bool `json:"broadcasts"`
}

func New(tokens *auth.Manager, userStore *store.Store, broadcasts *broadcast.Service, authsessions *authsessionrpc.Client, syncClient *syncrpc.Client, statusClient *statusrpc.Client, gatewayClient *gatewayrpc.Client, riskService *risk.Service, inviteService *invitecodes.Service, updates UpdateConfig) http.Handler {
	handler := &Handler{
		tokens:       tokens,
		store:        userStore,
		broadcasts:   broadcasts,
		authsessions: authsessions,
		sync:         syncClient,
		status:       statusClient,
		gateway:      gatewayClient,
		risk:         riskService,
		invites:      inviteService,
		updates:      updates,
		web:          webui.NewHandler(),
		releases:     http.StripPrefix("/releases/", http.FileServer(http.Dir(updates.ReleasesDir))),
		logins:       newLoginLimiter(5, 15*time.Minute, 15*time.Minute),
	}

	mux := http.NewServeMux()
	mux.HandleFunc("/api/healthz", handler.handleHealthz)
	mux.HandleFunc("/api/updates/android/latest", handler.handleAndroidUpdateLatest)
	mux.HandleFunc("/api/updates/pc/latest", handler.handlePCUpdateLatest)
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
	mux.Handle("/api/admin/broadcasts/preview", handler.requireSuperAdmin(handler.handleBroadcastPreview))
	mux.Handle("/api/admin/broadcasts", handler.requireSuperAdmin(handler.handleBroadcastRoutes))
	mux.Handle("/api/admin/broadcasts/", handler.requireSuperAdmin(handler.handleBroadcastRoutes))
	mux.HandleFunc("/td/current", handler.handleTDesktopCurrent)
	mux.Handle("/releases/", handler.releases)
	mux.Handle("/", handler.web)
	return withSecurityHeaders(withJSONDefaults(mux))
}

func (h *Handler) handleHealthz(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}

	writeJSON(w, http.StatusOK, apiResponse{
		OK: true,
		Data: map[string]any{
			"status": "ok",
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
			"token":     token,
			"expiresAt": expiresAt,
			"admin":     admin,
			"features":  h.features(),
		},
	})
}

func (h *Handler) handleMe(w http.ResponseWriter, r *http.Request, admin store.AdminUser) {
	writeJSON(w, http.StatusOK, apiResponse{
		OK: true,
		Data: map[string]any{
			"admin":    admin,
			"features": h.features(),
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

func (h *Handler) onlineAuthKeyIDs(ctx context.Context, userID int64) []int64 {
	if h.status == nil {
		return nil
	}
	ids, err := h.status.GetOnlineAuthKeys(ctx, userID)
	if err != nil {
		log.Printf("admin-api: load online sessions for %d failed: %v", userID, err)
		return nil
	}
	return mergeAuthKeyIDs(ids)
}

func (h *Handler) revokeUserSessions(ctx context.Context, userID int64) ([]int64, error) {
	if h.authsessions != nil {
		return h.authsessions.ResetAllAuthorizations(ctx, userID)
	}

	if _, err := h.store.KickUserSessions(ctx, userID); err != nil {
		return nil, err
	}
	return nil, nil
}

func (h *Handler) forceDisconnectAuthKeys(ctx context.Context, keyIDs []int64) {
	if h.gateway == nil || len(keyIDs) == 0 {
		return
	}
	if err := h.gateway.ForceDisconnectAuthKeys(ctx, keyIDs); err != nil {
		log.Printf("admin-api: force disconnect auth keys %v failed: %v", keyIDs, err)
	}
}

func (h *Handler) pushAdminPopup(ctx context.Context, userID int64, message string) {
	if h.sync == nil || strings.TrimSpace(message) == "" {
		return
	}
	if err := h.sync.PushUserPopup(ctx, userID, message); err != nil {
		log.Printf("admin-api: push popup to %d failed: %v", userID, err)
	}
}

func (h *Handler) pushResetAuthorization(ctx context.Context, userID int64, keyIDs []int64) {
	if h.sync == nil || len(keyIDs) == 0 {
		return
	}
	if err := h.sync.PushResetAuthorization(ctx, userID, keyIDs); err != nil {
		log.Printf("admin-api: push reset authorization to %d failed: %v", userID, err)
	}
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

	onlineKeyIDs := h.onlineAuthKeyIDs(r.Context(), userID)
	h.pushAdminPopup(r.Context(), userID, "该账号已被封禁")
	resetKeyIDs, err := h.revokeUserSessions(r.Context(), userID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "ban user failed")
		return
	}
	keyIDs := mergeAuthKeyIDs(onlineKeyIDs, resetKeyIDs)
	h.pushResetAuthorization(r.Context(), userID, keyIDs)
	if len(keyIDs) > 0 {
		time.Sleep(350 * time.Millisecond)
		h.forceDisconnectAuthKeys(r.Context(), keyIDs)
	}

	_ = h.store.CreateAuditLog(r.Context(), admin, "user.ban", userID, map[string]any{
		"reason":        request.Reason,
		"affected":      len(keyIDs),
		"forced_logout": true,
	})

	writeJSON(w, http.StatusOK, apiResponse{OK: true, Data: map[string]any{"affected": len(keyIDs), "message": "该账号已被封禁并强制下线"}})
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

	_ = h.store.CreateAuditLog(r.Context(), admin, "user.unban", userID, nil)

	writeJSON(w, http.StatusOK, apiResponse{OK: true, Data: map[string]any{"message": "?????"}})
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
	onlineKeyIDs := h.onlineAuthKeyIDs(r.Context(), userID)
	h.pushAdminPopup(r.Context(), userID, "您已被管理员踢下线")
	resetKeyIDs, err := h.revokeUserSessions(r.Context(), userID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "kick sessions failed")
		return
	}
	keyIDs := mergeAuthKeyIDs(onlineKeyIDs, resetKeyIDs)
	h.pushResetAuthorization(r.Context(), userID, keyIDs)
	if len(keyIDs) > 0 {
		time.Sleep(350 * time.Millisecond)
		h.forceDisconnectAuthKeys(r.Context(), keyIDs)
	}

	_ = h.store.CreateAuditLog(r.Context(), admin, "user.kick_sessions", userID, map[string]any{
		"affected":            len(keyIDs),
		"login_block_seconds": settings.KickLoginBlockSeconds,
	})

	writeJSON(w, http.StatusOK, apiResponse{
		OK: true,
		Data: map[string]any{
			"affected":          len(keyIDs),
			"loginBlockSeconds": settings.KickLoginBlockSeconds,
			"message":           "您已被管理员踢下线，五分钟内限制登录",
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
		Broadcasts: h.broadcasts != nil,
	}
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
	writeJSON(w, statusCode, apiResponse{
		OK:    false,
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
