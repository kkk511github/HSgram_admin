package httpapi

import (
	"context"
	"database/sql"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	reactionconfig "hsgram-admin/backend/internal/reactions"
	"hsgram-admin/backend/internal/store"
)

type fakeReactionConfigService struct {
	reactions []reactionconfig.Reaction
	keywords  []reactionconfig.EmojiKeyword
	patchID   int64
}

func (s *fakeReactionConfigService) ListReactions(ctx context.Context) ([]reactionconfig.Reaction, error) {
	return s.reactions, nil
}

func (s *fakeReactionConfigService) UpsertReaction(ctx context.Context, reaction reactionconfig.Reaction) (*reactionconfig.Reaction, error) {
	reaction.ID = 9
	s.reactions = append(s.reactions, reaction)
	return &reaction, nil
}

func (s *fakeReactionConfigService) PatchReaction(ctx context.Context, id int64, patch reactionconfig.ReactionPatch) (*reactionconfig.Reaction, error) {
	s.patchID = id
	if id == 404 {
		return nil, sql.ErrNoRows
	}
	item := reactionconfig.Reaction{ID: id, Reaction: "🔥", Title: "Fire", Enabled: true}
	if patch.Enabled != nil {
		item.Enabled = *patch.Enabled
	}
	return &item, nil
}

func (s *fakeReactionConfigService) ListEmojiKeywords(ctx context.Context, langCode string) ([]reactionconfig.EmojiKeyword, error) {
	return s.keywords, nil
}

func (s *fakeReactionConfigService) UpsertEmojiKeyword(ctx context.Context, keyword reactionconfig.EmojiKeyword) (*reactionconfig.EmojiKeyword, error) {
	s.keywords = append(s.keywords, keyword)
	return &keyword, nil
}

func TestAdminReactionsRequiresConfiguredService(t *testing.T) {
	h := &Handler{}
	req := httptest.NewRequest(http.MethodGet, "/api/admin/reactions", nil)
	rec := httptest.NewRecorder()

	h.handleReactions(rec, req, store.AdminUser{ID: 7, Role: "super_admin"})

	if rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("status = %d body=%s", rec.Code, rec.Body.String())
	}
	var response apiResponse
	if err := json.NewDecoder(rec.Body).Decode(&response); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if response.Code != "reactions_config_unavailable" {
		t.Fatalf("code = %q, want reactions_config_unavailable", response.Code)
	}
}

func TestAdminReactionsListCreateAndPatch(t *testing.T) {
	svc := &fakeReactionConfigService{
		reactions: []reactionconfig.Reaction{{ID: 1, Reaction: "❤️", Title: "Love", Enabled: true}},
	}
	h := &Handler{reactions: svc}

	listReq := httptest.NewRequest(http.MethodGet, "/api/admin/reactions", nil)
	listRec := httptest.NewRecorder()
	h.handleReactions(listRec, listReq, store.AdminUser{ID: 7, Role: "super_admin"})
	if listRec.Code != http.StatusOK {
		t.Fatalf("list status = %d body=%s", listRec.Code, listRec.Body.String())
	}

	createReq := httptest.NewRequest(http.MethodPost, "/api/admin/reactions", strings.NewReader(`{"reaction":"👏","title":"Clap","enabled":true,"sortOrder":20}`))
	createRec := httptest.NewRecorder()
	h.handleReactions(createRec, createReq, store.AdminUser{ID: 7, Role: "super_admin"})
	if createRec.Code != http.StatusOK {
		t.Fatalf("create status = %d body=%s", createRec.Code, createRec.Body.String())
	}
	if len(svc.reactions) != 2 || svc.reactions[1].Reaction != "👏" {
		t.Fatalf("create did not pass reaction payload: %#v", svc.reactions)
	}

	patchReq := httptest.NewRequest(http.MethodPatch, "/api/admin/reactions/9", strings.NewReader(`{"enabled":false}`))
	patchRec := httptest.NewRecorder()
	h.handleReactionRoutes(patchRec, patchReq, store.AdminUser{ID: 7, Role: "super_admin"})
	if patchRec.Code != http.StatusOK {
		t.Fatalf("patch status = %d body=%s", patchRec.Code, patchRec.Body.String())
	}
	if svc.patchID != 9 {
		t.Fatalf("patch id = %d, want 9", svc.patchID)
	}
}

func TestAdminEmojiKeywordListAndUpsert(t *testing.T) {
	svc := &fakeReactionConfigService{
		keywords: []reactionconfig.EmojiKeyword{{LangCode: "zh", Keyword: "笑", Emoticons: []string{"😂"}, Version: 1, Enabled: true}},
	}
	h := &Handler{reactions: svc}

	listReq := httptest.NewRequest(http.MethodGet, "/api/admin/emoji-keywords?lang=zh", nil)
	listRec := httptest.NewRecorder()
	h.handleEmojiKeywords(listRec, listReq, store.AdminUser{ID: 7, Role: "super_admin"})
	if listRec.Code != http.StatusOK {
		t.Fatalf("list status = %d body=%s", listRec.Code, listRec.Body.String())
	}

	createReq := httptest.NewRequest(http.MethodPost, "/api/admin/emoji-keywords", strings.NewReader(`{"langCode":"en","keyword":"fire","emoticons":["🔥"],"version":2,"enabled":true}`))
	createRec := httptest.NewRecorder()
	h.handleEmojiKeywords(createRec, createReq, store.AdminUser{ID: 7, Role: "super_admin"})
	if createRec.Code != http.StatusOK {
		t.Fatalf("create status = %d body=%s", createRec.Code, createRec.Body.String())
	}
	if len(svc.keywords) != 2 || svc.keywords[1].Keyword != "fire" {
		t.Fatalf("create did not pass keyword payload: %#v", svc.keywords)
	}
}
