package automessage

import (
	"context"
	"crypto/sha1"
	"encoding/binary"
	"errors"
	"fmt"
	"log"
	"strings"
	"time"

	"hsgram-admin/backend/internal/store"

	"github.com/zeromicro/go-zero/core/stores/kv"
)

const (
	MinIntervalMinutes = 5
	MaxIntervalMinutes = 1440
	MaxItems           = 100
	MaxContentLength   = 1000
	DefaultBatchSize   = 500
	DefaultLockTTL     = 120 * time.Second
)

const (
	CodeNoPermission      = "AUTO_MESSAGE_NO_PERMISSION"
	CodeNoEnabledItems    = "AUTO_MESSAGE_NO_ENABLED_ITEMS"
	CodeInvalidInterval   = "AUTO_MESSAGE_INVALID_INTERVAL"
	CodeContentEmpty      = "AUTO_MESSAGE_CONTENT_EMPTY"
	CodeContentTooLong    = "AUTO_MESSAGE_CONTENT_TOO_LONG"
	CodeItemLimitExceeded = "AUTO_MESSAGE_ITEM_LIMIT_EXCEEDED"
	CodeGroupDissolved    = "AUTO_MESSAGE_GROUP_DISSOLVED"
	CodeSystemMuted       = "AUTO_MESSAGE_SYSTEM_MUTED"
	CodeNoSendPermission  = "AUTO_MESSAGE_NO_SEND_PERMISSION"
	CodeSendFailed        = "AUTO_MESSAGE_SEND_FAILED"
	CodeLockFailed        = "AUTO_MESSAGE_LOCK_FAILED"
	CodeConfigNotFound    = "AUTO_MESSAGE_CONFIG_NOT_FOUND"
	CodeItemNotFound      = "AUTO_MESSAGE_ITEM_NOT_FOUND"
	CodeConflict          = "AUTO_MESSAGE_VERSION_CONFLICT"
)

type Error struct {
	Code    string
	Message string
	Err     error
}

func (e *Error) Error() string {
	if e == nil {
		return ""
	}
	if e.Message != "" {
		return e.Message
	}
	return e.Code
}

func (e *Error) Unwrap() error {
	if e == nil {
		return nil
	}
	return e.Err
}

func NewError(code, message string, err error) *Error {
	return &Error{Code: code, Message: message, Err: err}
}

type Sender interface {
	SendGroupTextMessage(ctx context.Context, senderUserID, groupID int64, text, bizID string) (int64, error)
}

type Store interface {
	GetAutoMessagePermission(ctx context.Context, groupID, actorUserID int64) (store.AutoMessageGroupPermission, error)
	GetAutoMessageConfigDetail(ctx context.Context, groupID int64) (*store.AutoMessageConfigDetail, error)
	EnsureAutoMessageConfig(ctx context.Context, groupID, actorUserID int64) (*store.AutoMessageConfig, error)
	SaveAutoMessageConfig(ctx context.Context, groupID, actorUserID int64, params store.SaveAutoMessageConfigParams, now time.Time) (*store.AutoMessageConfigDetail, error)
	EnableAutoMessageConfig(ctx context.Context, groupID, actorUserID int64, intervalMinutes int, now time.Time) (*store.AutoMessageConfigDetail, error)
	DisableAutoMessageConfig(ctx context.Context, groupID, actorUserID int64) (*store.AutoMessageConfigDetail, error)
	AddAutoMessageItem(ctx context.Context, groupID, actorUserID int64, input store.AutoMessageItemInput) (*store.AutoMessageConfigDetail, error)
	UpdateAutoMessageItem(ctx context.Context, groupID, actorUserID, itemID int64, input store.AutoMessageItemInput) (*store.AutoMessageConfigDetail, error)
	DeleteAutoMessageItem(ctx context.Context, groupID, actorUserID, itemID int64) (*store.AutoMessageConfigDetail, error)
	SortAutoMessageItems(ctx context.Context, groupID, actorUserID int64, itemIDs []int64) (*store.AutoMessageConfigDetail, error)
	ListAutoMessageLogs(ctx context.Context, groupID int64, page, pageSize int) (store.AutoMessageLogList, error)
	ListDueAutoMessageConfigs(ctx context.Context, now time.Time, limit, shardTotal, shardIndex int) ([]store.AutoMessageConfig, error)
	ProcessDueAutoMessageConfig(ctx context.Context, configID int64, now time.Time, send func(store.AutoMessageSendCandidate) store.AutoMessageSendResult) error
}

type Options struct {
	SenderUserID int64
	BatchSize    int
	ShardTotal   int
	ShardIndex   int
	LockTTL      time.Duration
	TickInterval time.Duration
}

type Service struct {
	store  Store
	sender Sender
	kv     kv.Store
	opts   Options
	now    func() time.Time
}

func New(store Store, sender Sender, kvStore kv.Store, opts Options) *Service {
	if opts.SenderUserID == 0 {
		opts.SenderUserID = storepkgBroadcastSystemUserID()
	}
	if opts.BatchSize <= 0 {
		opts.BatchSize = DefaultBatchSize
	}
	if opts.LockTTL <= 0 {
		opts.LockTTL = DefaultLockTTL
	}
	if opts.TickInterval <= 0 {
		opts.TickInterval = time.Minute
	}
	if opts.ShardTotal <= 0 {
		opts.ShardTotal = 1
	}
	if opts.ShardIndex < 0 || opts.ShardIndex >= opts.ShardTotal {
		opts.ShardIndex = 0
	}
	return &Service{
		store:  store,
		sender: sender,
		kv:     kvStore,
		opts:   opts,
		now:    time.Now,
	}
}

func storepkgBroadcastSystemUserID() int64 {
	return store.BroadcastSystemUserID
}

func (s *Service) GetConfig(ctx context.Context, actorUserID, groupID int64) (*store.AutoMessageConfigDetail, error) {
	if err := s.requireManagePermission(ctx, actorUserID, groupID); err != nil {
		return nil, err
	}
	if _, err := s.store.EnsureAutoMessageConfig(ctx, groupID, actorUserID); err != nil {
		return nil, err
	}
	return s.store.GetAutoMessageConfigDetail(ctx, groupID)
}

func (s *Service) SaveConfig(ctx context.Context, actorUserID, groupID int64, params store.SaveAutoMessageConfigParams) (*store.AutoMessageConfigDetail, error) {
	if err := s.requireManagePermission(ctx, actorUserID, groupID); err != nil {
		return nil, err
	}
	if err := validateConfigParams(params); err != nil {
		return nil, err
	}
	return s.store.SaveAutoMessageConfig(ctx, groupID, actorUserID, normalizeConfigParams(params), s.now())
}

func (s *Service) Enable(ctx context.Context, actorUserID, groupID int64, intervalMinutes int) (*store.AutoMessageConfigDetail, error) {
	if err := s.requireManagePermission(ctx, actorUserID, groupID); err != nil {
		return nil, err
	}
	if intervalMinutes > 0 && !validInterval(intervalMinutes) {
		return nil, NewError(CodeInvalidInterval, "interval minutes must be between 5 and 1440", nil)
	}
	detail, err := s.store.EnableAutoMessageConfig(ctx, groupID, actorUserID, intervalMinutes, s.now())
	if errors.Is(err, store.ErrAutoMessageItemNotFound) {
		return nil, NewError(CodeNoEnabledItems, "at least one enabled item is required", err)
	}
	return detail, mapStoreError(err)
}

func (s *Service) Disable(ctx context.Context, actorUserID, groupID int64) (*store.AutoMessageConfigDetail, error) {
	if err := s.requireManagePermission(ctx, actorUserID, groupID); err != nil {
		return nil, err
	}
	return s.store.DisableAutoMessageConfig(ctx, groupID, actorUserID)
}

func (s *Service) AddItem(ctx context.Context, actorUserID, groupID int64, input store.AutoMessageItemInput) (*store.AutoMessageConfigDetail, error) {
	if err := s.requireManagePermission(ctx, actorUserID, groupID); err != nil {
		return nil, err
	}
	detail, err := s.store.GetAutoMessageConfigDetail(ctx, groupID)
	if err != nil && !errors.Is(err, store.ErrAutoMessageConfigNotFound) {
		return nil, err
	}
	if detail != nil && len(detail.Items) >= MaxItems {
		return nil, NewError(CodeItemLimitExceeded, "auto message item limit exceeded", nil)
	}
	if err := validateItemInput(input); err != nil {
		return nil, err
	}
	return s.store.AddAutoMessageItem(ctx, groupID, actorUserID, normalizeItemInput(input))
}

func (s *Service) UpdateItem(ctx context.Context, actorUserID, groupID, itemID int64, input store.AutoMessageItemInput) (*store.AutoMessageConfigDetail, error) {
	if err := s.requireManagePermission(ctx, actorUserID, groupID); err != nil {
		return nil, err
	}
	if err := validateItemInput(input); err != nil {
		return nil, err
	}
	detail, err := s.store.UpdateAutoMessageItem(ctx, groupID, actorUserID, itemID, normalizeItemInput(input))
	return detail, mapStoreError(err)
}

func (s *Service) DeleteItem(ctx context.Context, actorUserID, groupID, itemID int64) (*store.AutoMessageConfigDetail, error) {
	if err := s.requireManagePermission(ctx, actorUserID, groupID); err != nil {
		return nil, err
	}
	detail, err := s.store.DeleteAutoMessageItem(ctx, groupID, actorUserID, itemID)
	return detail, mapStoreError(err)
}

func (s *Service) SortItems(ctx context.Context, actorUserID, groupID int64, itemIDs []int64) (*store.AutoMessageConfigDetail, error) {
	if err := s.requireManagePermission(ctx, actorUserID, groupID); err != nil {
		return nil, err
	}
	if len(itemIDs) == 0 {
		return nil, NewError(CodeItemNotFound, "item ids are required", nil)
	}
	detail, err := s.store.SortAutoMessageItems(ctx, groupID, actorUserID, itemIDs)
	return detail, mapStoreError(err)
}

func (s *Service) Logs(ctx context.Context, actorUserID, groupID int64, page, pageSize int) (store.AutoMessageLogList, error) {
	if err := s.requireManagePermission(ctx, actorUserID, groupID); err != nil {
		return store.AutoMessageLogList{}, err
	}
	return s.store.ListAutoMessageLogs(ctx, groupID, page, pageSize)
}

func (s *Service) Run(ctx context.Context) {
	ticker := time.NewTicker(s.opts.TickInterval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			s.ProcessDue(ctx)
		}
	}
}

func (s *Service) ProcessDue(ctx context.Context) {
	if s.sender == nil {
		return
	}
	now := s.now()
	configs, err := s.store.ListDueAutoMessageConfigs(ctx, now, s.opts.BatchSize, s.opts.ShardTotal, s.opts.ShardIndex)
	if err != nil {
		log.Printf("auto-message: list due configs failed: %v", err)
		return
	}
	for _, cfg := range configs {
		if err = s.processOne(ctx, cfg, now); err != nil {
			log.Printf("auto-message: process config=%d group=%d failed: %v", cfg.ID, cfg.GroupID, err)
		}
	}
}

func (s *Service) processOne(ctx context.Context, cfg store.AutoMessageConfig, now time.Time) error {
	lockKey := fmt.Sprintf("auto_message:%d", cfg.GroupID)
	requestID := fmt.Sprintf("%d:%d", cfg.ID, time.Now().UnixNano())
	locked, err := s.kv.SetnxExCtx(ctx, lockKey, requestID, int(s.opts.LockTTL/time.Second))
	if err != nil {
		return NewError(CodeLockFailed, "acquire auto message lock failed", err)
	}
	if !locked {
		return nil
	}
	defer s.releaseLock(context.Background(), lockKey, requestID)

	return s.store.ProcessDueAutoMessageConfig(ctx, cfg.ID, now, func(candidate store.AutoMessageSendCandidate) store.AutoMessageSendResult {
		messageID, sendErr := s.sender.SendGroupTextMessage(
			ctx,
			s.opts.SenderUserID,
			candidate.Config.GroupID,
			candidate.Item.Content,
			candidate.BizID,
		)
		if sendErr != nil {
			code := CodeSendFailed
			msg := sendErr.Error()
			if strings.Contains(strings.ToLower(msg), "muted") {
				code = CodeSystemMuted
			} else if strings.Contains(strings.ToLower(msg), "write forbidden") ||
				strings.Contains(strings.ToLower(msg), "not participant") ||
				strings.Contains(strings.ToLower(msg), "forbidden") {
				code = CodeNoSendPermission
			}
			return store.AutoMessageSendResult{
				Success:      false,
				ErrorCode:    code,
				ErrorMessage: truncate(msg, 512),
			}
		}
		return store.AutoMessageSendResult{Success: true, MessageID: messageID}
	})
}

func (s *Service) releaseLock(ctx context.Context, key, requestID string) {
	const script = `
if redis.call("GET", KEYS[1]) == ARGV[1] then
  return redis.call("DEL", KEYS[1])
end
return 0`
	if _, err := s.kv.EvalCtx(ctx, script, key, requestID); err != nil {
		log.Printf("auto-message: release lock %s failed: %v", key, err)
	}
}

func (s *Service) requireManagePermission(ctx context.Context, actorUserID, groupID int64) error {
	if actorUserID == 0 {
		return NewError(CodeNoPermission, "actor user id is required", nil)
	}
	p, err := s.store.GetAutoMessagePermission(ctx, groupID, actorUserID)
	if err != nil {
		return mapStoreError(err)
	}
	if p.Deactivated {
		return NewError(CodeGroupDissolved, "group is dissolved", nil)
	}
	if !p.CanManage(actorUserID) {
		return NewError(CodeNoPermission, "no permission to manage auto messages", nil)
	}
	return nil
}

func validateConfigParams(params store.SaveAutoMessageConfigParams) error {
	if !validInterval(params.IntervalMinutes) {
		return NewError(CodeInvalidInterval, "interval minutes must be between 5 and 1440", nil)
	}
	if params.SendMode != "" && params.SendMode != store.AutoMessageSendModeSequence {
		return NewError(CodeInvalidInterval, "send mode is not supported", nil)
	}
	if params.FirstSendMode != "" && params.FirstSendMode != store.AutoMessageFirstSendDelay {
		return NewError(CodeInvalidInterval, "first send mode is not supported", nil)
	}
	if len(params.Items) > MaxItems {
		return NewError(CodeItemLimitExceeded, "auto message item limit exceeded", nil)
	}
	enabledCount := 0
	for _, item := range params.Items {
		if err := validateItemInput(item); err != nil {
			return err
		}
		if item.Enabled {
			enabledCount++
		}
	}
	if params.Enabled && enabledCount == 0 {
		return NewError(CodeNoEnabledItems, "at least one enabled item is required", nil)
	}
	return nil
}

func validateItemInput(input store.AutoMessageItemInput) error {
	content := strings.TrimSpace(input.Content)
	if content == "" {
		return NewError(CodeContentEmpty, "auto message content is required", nil)
	}
	if len([]rune(content)) > MaxContentLength {
		return NewError(CodeContentTooLong, "auto message content is too long", nil)
	}
	contentType := strings.TrimSpace(input.ContentType)
	if contentType != "" && contentType != store.AutoMessageContentTypeText {
		return NewError(CodeInvalidInterval, "content type is not supported", nil)
	}
	return nil
}

func normalizeConfigParams(params store.SaveAutoMessageConfigParams) store.SaveAutoMessageConfigParams {
	params.SendMode = strings.TrimSpace(params.SendMode)
	if params.SendMode == "" {
		params.SendMode = store.AutoMessageSendModeSequence
	}
	params.FirstSendMode = strings.TrimSpace(params.FirstSendMode)
	if params.FirstSendMode == "" {
		params.FirstSendMode = store.AutoMessageFirstSendDelay
	}
	for i := range params.Items {
		params.Items[i] = normalizeItemInput(params.Items[i])
		if params.Items[i].SortOrder <= 0 {
			params.Items[i].SortOrder = i + 1
		}
	}
	return params
}

func normalizeItemInput(input store.AutoMessageItemInput) store.AutoMessageItemInput {
	input.Content = strings.TrimSpace(input.Content)
	input.ContentType = strings.TrimSpace(input.ContentType)
	if input.ContentType == "" {
		input.ContentType = store.AutoMessageContentTypeText
	}
	return input
}

func validInterval(value int) bool {
	return value >= MinIntervalMinutes && value <= MaxIntervalMinutes
}

func mapStoreError(err error) error {
	if err == nil {
		return nil
	}
	switch {
	case errors.Is(err, store.ErrAutoMessageConfigNotFound):
		return NewError(CodeConfigNotFound, "auto message config not found", err)
	case errors.Is(err, store.ErrAutoMessageItemNotFound):
		return NewError(CodeItemNotFound, "auto message item not found", err)
	case errors.Is(err, store.ErrAutoMessageConflict):
		return NewError(CodeConflict, "auto message config was updated by another client", err)
	case errors.Is(err, store.ErrAutoMessageGroupDissolved):
		return NewError(CodeGroupDissolved, "group is dissolved", err)
	default:
		return err
	}
}

func StableRandomID(bizID string) int64 {
	sum := sha1.Sum([]byte(bizID))
	value := int64(binary.LittleEndian.Uint64(sum[:8]))
	if value < 0 {
		value = -value
	}
	if value == 0 {
		value = time.Now().UnixNano()
	}
	return value
}

func truncate(value string, limit int) string {
	if limit <= 0 {
		return ""
	}
	runes := []rune(value)
	if len(runes) <= limit {
		return value
	}
	return string(runes[:limit])
}
