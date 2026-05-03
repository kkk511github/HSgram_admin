package httpapi

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	stickerimport "hsgram-admin/backend/internal/stickers"
	"hsgram-admin/backend/internal/store"
)

type fakeStickerService struct {
	req   stickerimport.ImportRequest
	calls int
	err   error
}

func (s *fakeStickerService) ImportTelegramSet(ctx context.Context, req stickerimport.ImportRequest) (*stickerimport.ImportResult, error) {
	s.calls++
	s.req = req
	if s.err != nil {
		return nil, s.err
	}
	return &stickerimport.ImportResult{SetID: 1, ShortName: "licensed_set", StickerCount: 1}, nil
}

func (s *fakeStickerService) ListSets(ctx context.Context) ([]stickerimport.SetSummary, error) {
	return []stickerimport.SetSummary{{ID: 1, ShortName: "licensed_set"}}, nil
}

func (s *fakeStickerService) GetSet(ctx context.Context, id int64) (*stickerimport.SetDetail, error) {
	return &stickerimport.SetDetail{SetSummary: stickerimport.SetSummary{ID: id, ShortName: "licensed_set"}}, nil
}

func (s *fakeStickerService) DisableSet(ctx context.Context, id int64) error {
	return nil
}

func TestStickerImportRequiresConfiguredService(t *testing.T) {
	h := &Handler{}
	req := httptest.NewRequest(http.MethodPost, "/api/admin/stickers/import", strings.NewReader(`{"shortName":"licensed_set"}`))
	rec := httptest.NewRecorder()

	h.handleStickerImport(rec, req, store.AdminUser{ID: 7, Role: "super_admin"})

	if rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("unexpected status: %d body=%s", rec.Code, rec.Body.String())
	}
	var response apiResponse
	if err := json.NewDecoder(rec.Body).Decode(&response); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if response.Code != "sticker_import_unavailable" {
		t.Fatalf("unexpected code: %q", response.Code)
	}
}

func TestStickerImportPassesAuthorizationStatement(t *testing.T) {
	svc := &fakeStickerService{}
	h := &Handler{stickers: svc}
	req := httptest.NewRequest(http.MethodPost, "/api/admin/stickers/import", strings.NewReader(`{
		"shortName":"https://t.me/addstickers/licensed_set",
		"source":"creator upload",
		"authorizationStatement":"owned or licensed"
	}`))
	rec := httptest.NewRecorder()

	h.handleStickerImport(rec, req, store.AdminUser{ID: 7, Role: "super_admin"})

	if rec.Code != http.StatusOK {
		t.Fatalf("unexpected status: %d body=%s", rec.Code, rec.Body.String())
	}
	if svc.calls != 1 {
		t.Fatalf("expected import call")
	}
	if svc.req.AdminID != 7 || svc.req.AuthorizationStatement != "owned or licensed" {
		t.Fatalf("unexpected import request: %#v", svc.req)
	}
}
