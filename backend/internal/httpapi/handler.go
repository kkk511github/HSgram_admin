package httpapi

import (
	"database/sql"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strconv"
	"strings"
	"time"

	"hsgram-admin/backend/internal/auth"
	"hsgram-admin/backend/internal/broadcast"
	"hsgram-admin/backend/internal/store"
	"hsgram-admin/backend/webui"
)

type Handler struct {
	tokens     *auth.Manager
	store      *store.Store
	broadcasts *broadcast.Service
	updates    UpdateConfig
	web        http.Handler
	releases   http.Handler
	logins     *loginLimiter
}

type apiResponse struct {
	OK    bool   `json:"ok"`
	Data  any    `json:"data,omitempty"`
	Error string `json:"error,omitempty"`
}

type featureFlags struct {
	Broadcasts bool `json:"broadcasts"`
}

func New(tokens *auth.Manager, userStore *store.Store, broadcasts *broadcast.Service, updates UpdateConfig) http.Handler {
	handler := &Handler{
		tokens:     tokens,
		store:      userStore,
		broadcasts: broadcasts,
		updates:    updates,
		web:        webui.NewHandler(),
		releases:   http.StripPrefix("/releases/", http.FileServer(http.Dir(updates.ReleasesDir))),
		logins:     newLoginLimiter(5, 15*time.Minute, 15*time.Minute),
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

	_ = h.store.CreateAuditLog(r.Context(), admin, "user.ban", userID, map[string]any{
		"reason": request.Reason,
	})

	writeJSON(w, http.StatusOK, apiResponse{OK: true})
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

	_ = h.store.CreateAuditLog(r.Context(), admin, "user.unban", userID, nil)

	writeJSON(w, http.StatusOK, apiResponse{OK: true})
}

func (h *Handler) handleKickSessions(w http.ResponseWriter, r *http.Request, admin store.AdminUser, userID int64) {
	if r.Method != http.MethodPost {
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}

	affected, err := h.store.KickUserSessions(r.Context(), userID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "kick sessions failed")
		return
	}

	_ = h.store.CreateAuditLog(r.Context(), admin, "user.kick_sessions", userID, map[string]any{
		"affected": affected,
	})

	writeJSON(w, http.StatusOK, apiResponse{
		OK: true,
		Data: map[string]any{
			"affected": affected,
		},
	})
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
