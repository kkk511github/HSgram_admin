package httpapi

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"hsgram-admin/backend/internal/broadcast"
	"hsgram-admin/backend/internal/store"
)

type recordingMessageService struct {
	sender int64
	target int64
	text   string
	err    error
	calls  int
}

func (s *recordingMessageService) SendTextMessage(ctx context.Context, senderUserID, targetUserID int64, text string) error {
	s.calls++
	s.sender = senderUserID
	s.target = targetUserID
	s.text = text
	return s.err
}

type recordingAuthsessionService struct {
	keys  []int64
	err   error
	calls int
	user  int64
}

func (s *recordingAuthsessionService) ResetAllAuthorizations(ctx context.Context, userID int64) ([]int64, error) {
	s.calls++
	s.user = userID
	return s.keys, s.err
}

type recordingSyncService struct {
	popupUser  int64
	popupText  string
	resetUser  int64
	resetKeys  []int64
	popupErr   error
	resetErr   error
	popupCalls int
	resetCalls int
}

func (s *recordingSyncService) PushUserPopup(ctx context.Context, userID int64, message string) error {
	s.popupCalls++
	s.popupUser = userID
	s.popupText = message
	return s.popupErr
}

func (s *recordingSyncService) PushResetAuthorization(ctx context.Context, userID int64, authKeyIDs []int64) error {
	s.resetCalls++
	s.resetUser = userID
	s.resetKeys = append([]int64(nil), authKeyIDs...)
	return s.resetErr
}

type recordingStatusService struct {
	keys  []int64
	err   error
	calls int
	user  int64
}

func (s *recordingStatusService) GetOnlineAuthKeys(ctx context.Context, userID int64) ([]int64, error) {
	s.calls++
	s.user = userID
	return s.keys, s.err
}

type recordingGatewayService struct {
	keys  []int64
	err   error
	calls int
}

type recordingBroadcastService struct {
	req   broadcast.Request
	admin store.AdminUser
	err   error
	calls int
}

func (s *recordingBroadcastService) Preview(ctx context.Context, req broadcast.Request) (store.BroadcastPreview, store.BroadcastTargetSpec, error) {
	return store.BroadcastPreview{Count: 2}, store.BroadcastTargetSpec{Type: req.TargetType}, nil
}

func (s *recordingBroadcastService) Enqueue(ctx context.Context, admin store.AdminUser, req broadcast.Request) (*store.BroadcastRecord, store.BroadcastPreview, error) {
	s.calls++
	s.req = req
	s.admin = admin
	if s.err != nil {
		return nil, store.BroadcastPreview{}, s.err
	}
	return &store.BroadcastRecord{ID: 88, Status: "pending", MessageText: req.MessageText}, store.BroadcastPreview{Count: 2}, nil
}

func (s *recordingGatewayService) ForceDisconnectAuthKeys(ctx context.Context, authKeyIDs []int64) error {
	s.calls++
	s.keys = append([]int64(nil), authKeyIDs...)
	return s.err
}

func TestSupportReplyCallsMessageRPCAndReportsFailures(t *testing.T) {
	msg := &recordingMessageService{}
	h := &Handler{msg: msg}
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/api/admin/support/threads/42/reply", strings.NewReader(`{"message":" hello "}`))

	h.handleSupportThreadReply(rec, req, store.AdminUser{ID: 1, Username: "admin"}, 42)
	if rec.Code != http.StatusOK {
		t.Fatalf("unexpected status: %d body=%s", rec.Code, rec.Body.String())
	}
	if msg.calls != 1 || msg.sender != store.SupportSystemUserID || msg.target != 42 || msg.text != "hello" {
		t.Fatalf("message RPC was not called with expected parameters: %#v", msg)
	}

	msg.err = errors.New("rpc down")
	rec = httptest.NewRecorder()
	req = httptest.NewRequest(http.MethodPost, "/api/admin/support/threads/42/reply", strings.NewReader(`{"message":"hello"}`))
	h.handleSupportThreadReply(rec, req, store.AdminUser{}, 42)
	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("expected RPC failure status, got %d body=%s", rec.Code, rec.Body.String())
	}
	var payload apiResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &payload); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if payload.OK || payload.Error == "" {
		t.Fatalf("expected structured error response, got %#v", payload)
	}
}

func TestBroadcastSendCallsBroadcastServiceAndReportsFailures(t *testing.T) {
	broadcasts := &recordingBroadcastService{}
	h := &Handler{broadcasts: broadcasts}
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/api/admin/broadcasts", strings.NewReader(`{"messageText":"hello users","targetType":"all"}`))

	h.handleBroadcastSend(rec, req, store.AdminUser{ID: 1, Username: "admin"})
	if rec.Code != http.StatusOK {
		t.Fatalf("unexpected status: %d body=%s", rec.Code, rec.Body.String())
	}
	if broadcasts.calls != 1 || broadcasts.req.MessageText != "hello users" || broadcasts.req.TargetType != store.BroadcastTargetTypeAll {
		t.Fatalf("broadcast service was not called with expected request: %#v", broadcasts)
	}

	broadcasts.err = errors.New("enqueue failed")
	rec = httptest.NewRecorder()
	req = httptest.NewRequest(http.MethodPost, "/api/admin/broadcasts", strings.NewReader(`{"messageText":"hello users","targetType":"all"}`))
	h.handleBroadcastSend(rec, req, store.AdminUser{})
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected enqueue failure status, got %d body=%s", rec.Code, rec.Body.String())
	}
	var payload apiResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &payload); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if payload.OK || payload.Error == "" {
		t.Fatalf("expected structured broadcast error response, got %#v", payload)
	}
}

func TestSessionRPCSuccessAndFailurePathsAreObservable(t *testing.T) {
	auth := &recordingAuthsessionService{keys: []int64{10, 20}}
	status := &recordingStatusService{keys: []int64{20, 30}}
	syncClient := &recordingSyncService{}
	gateway := &recordingGatewayService{}
	h := &Handler{authsessions: auth, status: status, sync: syncClient, gateway: gateway}

	onlineKeys, statusDegraded := h.onlineAuthKeyIDs(context.Background(), 7)
	if len(statusDegraded) != 0 || len(onlineKeys) != 2 || status.calls != 1 || status.user != 7 {
		t.Fatalf("unexpected online session result keys=%v degraded=%v status=%#v", onlineKeys, statusDegraded, status)
	}
	resetKeys, revokeDegraded, err := h.revokeUserSessions(context.Background(), 7)
	if err != nil || len(revokeDegraded) != 0 || len(resetKeys) != 2 || auth.calls != 1 || auth.user != 7 {
		t.Fatalf("unexpected revoke result keys=%v degraded=%v err=%v auth=%#v", resetKeys, revokeDegraded, err, auth)
	}
	if degraded := h.pushAdminPopup(context.Background(), 7, "kick"); len(degraded) != 0 || syncClient.popupCalls != 1 || syncClient.popupUser != 7 || syncClient.popupText != "kick" {
		t.Fatalf("unexpected popup result degraded=%v sync=%#v", degraded, syncClient)
	}
	if degraded := h.pushResetAuthorization(context.Background(), 7, []int64{10, 20, 30}); len(degraded) != 0 || syncClient.resetCalls != 1 || syncClient.resetUser != 7 {
		t.Fatalf("unexpected reset push result degraded=%v sync=%#v", degraded, syncClient)
	}
	if degraded := h.forceDisconnectAuthKeys(context.Background(), []int64{10, 20, 30}); len(degraded) != 0 || gateway.calls != 1 || len(gateway.keys) != 3 {
		t.Fatalf("unexpected gateway result degraded=%v gateway=%#v", degraded, gateway)
	}

	status.err = errors.New("status failed")
	if keys, degraded := h.onlineAuthKeyIDs(context.Background(), 7); keys != nil || len(degraded) != 1 || degraded[0] != "status_rpc_failed" {
		t.Fatalf("expected status failure degraded marker, keys=%v degraded=%v", keys, degraded)
	}
	auth.err = errors.New("auth failed")
	if keys, degraded, err := h.revokeUserSessions(context.Background(), 7); err == nil || len(keys) != 2 || len(degraded) != 0 {
		t.Fatalf("expected auth failure to surface, keys=%v degraded=%v err=%v", keys, degraded, err)
	}
	syncClient.popupErr = errors.New("sync failed")
	if degraded := h.pushAdminPopup(context.Background(), 7, "kick"); len(degraded) != 1 || degraded[0] != "sync_rpc_popup_failed" {
		t.Fatalf("expected popup failure degraded marker, got %v", degraded)
	}
	gateway.err = errors.New("gateway failed")
	if degraded := h.forceDisconnectAuthKeys(context.Background(), []int64{10}); len(degraded) != 1 || degraded[0] != "gateway_rpc_failed" {
		t.Fatalf("expected gateway failure degraded marker, got %v", degraded)
	}
}
