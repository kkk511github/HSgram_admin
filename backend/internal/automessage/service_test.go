package automessage

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"hsgram-admin/backend/internal/store"

	"github.com/teamgram/proto/mtproto"
)

func TestSaveConfigRequiresEnabledItemsWhenEnabled(t *testing.T) {
	svc := New(&fakeStore{
		permission: ownerPermission(1001),
	}, nil, nil, Options{})

	_, err := svc.SaveConfig(context.Background(), 1001, 2001, store.SaveAutoMessageConfigParams{
		Enabled:         true,
		IntervalMinutes: 30,
		Items: []store.AutoMessageItemInput{
			{Content: "hello", Enabled: false},
		},
	})
	assertAutoCode(t, err, CodeNoEnabledItems)
}

func TestSaveConfigNormalizesFirstSendDelay(t *testing.T) {
	now := time.Date(2026, 5, 10, 14, 0, 0, 0, time.UTC)
	fs := &fakeStore{permission: ownerPermission(1001)}
	svc := New(fs, nil, nil, Options{})
	svc.now = func() time.Time { return now }

	_, err := svc.SaveConfig(context.Background(), 1001, 2001, store.SaveAutoMessageConfigParams{
		Enabled:         true,
		IntervalMinutes: 30,
		Items: []store.AutoMessageItemInput{
			{Content: "first", Enabled: true},
		},
	})
	if err != nil {
		t.Fatalf("SaveConfig() error = %v", err)
	}
	if fs.saved.FirstSendMode != store.AutoMessageFirstSendDelay {
		t.Fatalf("FirstSendMode = %q, want %q", fs.saved.FirstSendMode, store.AutoMessageFirstSendDelay)
	}
	if fs.saved.SendMode != store.AutoMessageSendModeSequence {
		t.Fatalf("SendMode = %q, want %q", fs.saved.SendMode, store.AutoMessageSendModeSequence)
	}
}

func TestAdminPermissionHonorsAdminsCanManage(t *testing.T) {
	svc := New(&fakeStore{
		permission: store.AutoMessageGroupPermission{
			GroupID:          2001,
			CreatorUserID:    999,
			ParticipantType:  mtproto.ChatMemberAdmin,
			ParticipantState: mtproto.ChatMemberStateNormal,
			AdminsCanManage:  false,
		},
	}, nil, nil, Options{})

	_, err := svc.GetConfig(context.Background(), 1001, 2001)
	assertAutoCode(t, err, CodeNoPermission)
}

func TestValidateRejectsTooLongContent(t *testing.T) {
	content := strings.Repeat("a", MaxContentLength+1)
	err := validateItemInput(store.AutoMessageItemInput{Content: content, Enabled: true})
	assertAutoCode(t, err, CodeContentTooLong)
}

func TestStableRandomIDIsStableAndPositive(t *testing.T) {
	first := StableRandomID("auto_message:1:2:3")
	second := StableRandomID("auto_message:1:2:3")
	if first != second {
		t.Fatalf("StableRandomID not stable: %d != %d", first, second)
	}
	if first <= 0 {
		t.Fatalf("StableRandomID = %d, want positive", first)
	}
}

func ownerPermission(actorUserID int64) store.AutoMessageGroupPermission {
	return store.AutoMessageGroupPermission{
		GroupID:          2001,
		CreatorUserID:    actorUserID,
		ParticipantType:  mtproto.ChatMemberCreator,
		ParticipantState: mtproto.ChatMemberStateNormal,
		AdminsCanManage:  true,
	}
}

func assertAutoCode(t *testing.T, err error, code string) {
	t.Helper()
	var autoErr *Error
	if !errors.As(err, &autoErr) {
		t.Fatalf("error = %v, want auto error %s", err, code)
	}
	if autoErr.Code != code {
		t.Fatalf("error code = %s, want %s", autoErr.Code, code)
	}
}

type fakeStore struct {
	permission store.AutoMessageGroupPermission
	saved      store.SaveAutoMessageConfigParams
}

func (f *fakeStore) GetAutoMessagePermission(context.Context, int64, int64) (store.AutoMessageGroupPermission, error) {
	return f.permission, nil
}

func (f *fakeStore) GetAutoMessageConfigDetail(context.Context, int64) (*store.AutoMessageConfigDetail, error) {
	return &store.AutoMessageConfigDetail{
		Config: store.AutoMessageConfig{
			ID:              1,
			GroupID:         f.permission.GroupID,
			IntervalMinutes: 30,
			SendMode:        store.AutoMessageSendModeSequence,
			FirstSendMode:   store.AutoMessageFirstSendDelay,
		},
	}, nil
}

func (f *fakeStore) EnsureAutoMessageConfig(context.Context, int64, int64) (*store.AutoMessageConfig, error) {
	return &store.AutoMessageConfig{ID: 1, GroupID: f.permission.GroupID, IntervalMinutes: 30}, nil
}

func (f *fakeStore) SaveAutoMessageConfig(_ context.Context, _ int64, _ int64, params store.SaveAutoMessageConfigParams, _ time.Time) (*store.AutoMessageConfigDetail, error) {
	f.saved = params
	return f.GetAutoMessageConfigDetail(context.Background(), f.permission.GroupID)
}

func (f *fakeStore) EnableAutoMessageConfig(context.Context, int64, int64, int, time.Time) (*store.AutoMessageConfigDetail, error) {
	return nil, nil
}

func (f *fakeStore) DisableAutoMessageConfig(context.Context, int64, int64) (*store.AutoMessageConfigDetail, error) {
	return nil, nil
}

func (f *fakeStore) AddAutoMessageItem(context.Context, int64, int64, store.AutoMessageItemInput) (*store.AutoMessageConfigDetail, error) {
	return nil, nil
}

func (f *fakeStore) UpdateAutoMessageItem(context.Context, int64, int64, int64, store.AutoMessageItemInput) (*store.AutoMessageConfigDetail, error) {
	return nil, nil
}

func (f *fakeStore) DeleteAutoMessageItem(context.Context, int64, int64, int64) (*store.AutoMessageConfigDetail, error) {
	return nil, nil
}

func (f *fakeStore) SortAutoMessageItems(context.Context, int64, int64, []int64) (*store.AutoMessageConfigDetail, error) {
	return nil, nil
}

func (f *fakeStore) ListAutoMessageLogs(context.Context, int64, int, int) (store.AutoMessageLogList, error) {
	return store.AutoMessageLogList{}, nil
}

func (f *fakeStore) ListDueAutoMessageConfigs(context.Context, time.Time, int, int, int) ([]store.AutoMessageConfig, error) {
	return nil, nil
}

func (f *fakeStore) ProcessDueAutoMessageConfig(context.Context, int64, time.Time, func(store.AutoMessageSendCandidate) store.AutoMessageSendResult) error {
	return nil
}
