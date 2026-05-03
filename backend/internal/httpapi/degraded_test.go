package httpapi

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"hsgram-admin/backend/internal/store"
)

func TestFeaturesReflectRuntimeDependencies(t *testing.T) {
	h := &Handler{}

	features := h.features()
	if features.Support {
		t.Fatalf("support must be disabled when message RPC is unavailable")
	}
	if features.Broadcasts {
		t.Fatalf("broadcasts must be disabled without broadcast service")
	}
	if features.FullSessionRevoke {
		t.Fatalf("full session revoke must be disabled without authsession RPC")
	}
}

func TestHealthzReportsDegradedDependencies(t *testing.T) {
	handler := New(nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, UpdateConfig{
		ReleasesDir: t.TempDir(),
	})

	req := httptest.NewRequest(http.MethodGet, "/api/healthz", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("unexpected status: %d body=%s", rec.Code, rec.Body.String())
	}

	var response apiResponse
	if err := json.NewDecoder(rec.Body).Decode(&response); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	data := response.Data.(map[string]any)
	if data["status"] != "degraded" {
		t.Fatalf("expected degraded health status, got %#v", data["status"])
	}
}

func TestSupportReplyUnavailableIsStructured(t *testing.T) {
	h := &Handler{}

	req := httptest.NewRequest(http.MethodPost, "/api/admin/support/threads/42/reply", strings.NewReader(`{"message":"hello"}`))
	rec := httptest.NewRecorder()
	h.handleSupportThreadReply(rec, req, store.AdminUser{ID: 1, Role: "admin"}, 42)

	if rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("unexpected status: %d body=%s", rec.Code, rec.Body.String())
	}
	var response apiResponse
	if err := json.NewDecoder(rec.Body).Decode(&response); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if response.Code != "message_rpc_unavailable" {
		t.Fatalf("unexpected code: %q", response.Code)
	}
}

func TestSessionOperationsReportMissingDependencies(t *testing.T) {
	h := &Handler{}

	if degraded := h.sessionOperationDegraded(); len(degraded) != 4 {
		t.Fatalf("expected all session dependencies to be degraded, got %#v", degraded)
	}

	keyIDs, degraded, err := h.revokeUserSessions(context.Background(), 42)
	if err == nil {
		t.Fatalf("expected missing store/authsession to return an error")
	}
	if keyIDs != nil {
		t.Fatalf("expected no key ids from unavailable revoke path, got %#v", keyIDs)
	}
	if len(degraded) != 1 || degraded[0] != "authsession_rpc_unavailable" {
		t.Fatalf("expected authsession degraded marker, got %#v", degraded)
	}
}

func TestUnbanSuccessResponseMessageIsReadable(t *testing.T) {
	rec := httptest.NewRecorder()

	writeUnbanSuccess(rec, []string{"sync_rpc_popup_failed"})

	if rec.Code != http.StatusOK {
		t.Fatalf("unexpected status: %d body=%s", rec.Code, rec.Body.String())
	}
	var response apiResponse
	if err := json.NewDecoder(rec.Body).Decode(&response); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if response.Code != "" {
		t.Fatalf("expected no error code on success, got %q", response.Code)
	}
	data := response.Data.(map[string]any)
	if data["message"] != "该账号已解封" {
		t.Fatalf("unexpected unban message: %#v", data["message"])
	}
	if data["popupDelivered"] != false {
		t.Fatalf("expected popupDelivered=false when popup is degraded, got %#v", data["popupDelivered"])
	}
}
