package store

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/teamgram/proto/mtproto"
)

const (
	AutoMessageSendModeSequence = "sequence"
	AutoMessageFirstSendDelay   = "delay"
	AutoMessageContentTypeText  = "text"

	AutoMessageStatusSuccess = "success"
	AutoMessageStatusFailed  = "failed"
)

var (
	ErrAutoMessageConfigNotFound = errors.New("auto message config not found")
	ErrAutoMessageItemNotFound   = errors.New("auto message item not found")
	ErrAutoMessageConflict       = errors.New("auto message config version conflict")
	ErrAutoMessageGroupDissolved = errors.New("auto message group dissolved")
)

type AutoMessageConfig struct {
	ID               int64      `json:"id"`
	GroupID          int64      `json:"groupId"`
	Enabled          bool       `json:"enabled"`
	IntervalMinutes  int        `json:"intervalMinutes"`
	SendMode         string     `json:"sendMode"`
	FirstSendMode    string     `json:"firstSendMode"`
	CurrentIndex     int        `json:"currentIndex"`
	NextSendAt       *time.Time `json:"nextSendAt,omitempty"`
	LastSendAt       *time.Time `json:"lastSendAt,omitempty"`
	LastSendStatus   string     `json:"lastSendStatus,omitempty"`
	LastErrorCode    string     `json:"lastErrorCode,omitempty"`
	LastErrorMessage string     `json:"lastErrorMessage,omitempty"`
	AdminsCanManage  bool       `json:"adminsCanManage"`
	Version          int        `json:"version"`
	CreatedBy        int64      `json:"createdBy"`
	UpdatedBy        int64      `json:"updatedBy"`
	CreatedAt        time.Time  `json:"createdAt"`
	UpdatedAt        time.Time  `json:"updatedAt"`
}

type AutoMessageItem struct {
	ID          int64     `json:"id"`
	ConfigID    int64     `json:"configId"`
	GroupID     int64     `json:"groupId"`
	SortOrder   int       `json:"sortOrder"`
	Content     string    `json:"content"`
	ContentType string    `json:"contentType"`
	Enabled     bool      `json:"enabled"`
	CreatedBy   int64     `json:"createdBy"`
	UpdatedBy   int64     `json:"updatedBy"`
	CreatedAt   time.Time `json:"createdAt"`
	UpdatedAt   time.Time `json:"updatedAt"`
}

type AutoMessageLog struct {
	ID              int64     `json:"id"`
	GroupID         int64     `json:"groupId"`
	ConfigID        int64     `json:"configId"`
	ItemID          int64     `json:"itemId,omitempty"`
	ContentSnapshot string    `json:"contentSnapshot,omitempty"`
	SendIndex       int       `json:"sendIndex"`
	SentAt          time.Time `json:"sentAt"`
	Status          string    `json:"status"`
	ErrorCode       string    `json:"errorCode,omitempty"`
	ErrorMessage    string    `json:"errorMessage,omitempty"`
	MessageID       int64     `json:"messageId,omitempty"`
	CreatedAt       time.Time `json:"createdAt"`
}

type AutoMessageConfigDetail struct {
	Config           AutoMessageConfig `json:"config"`
	Items            []AutoMessageItem `json:"items"`
	EnabledItemCount int               `json:"enabledItemCount"`
	NextItem         *AutoMessageItem  `json:"nextItem,omitempty"`
}

type AutoMessageLogList struct {
	Items    []AutoMessageLog `json:"items"`
	Total    int              `json:"total"`
	Page     int              `json:"page"`
	PageSize int              `json:"pageSize"`
}

type AutoMessageItemInput struct {
	ID          int64  `json:"id"`
	SortOrder   int    `json:"sortOrder"`
	Content     string `json:"content"`
	ContentType string `json:"contentType"`
	Enabled     bool   `json:"enabled"`
}

type SaveAutoMessageConfigParams struct {
	Enabled         bool
	IntervalMinutes int
	SendMode        string
	FirstSendMode   string
	AdminsCanManage bool
	Version         int
	Items           []AutoMessageItemInput
}

type AutoMessageGroupPermission struct {
	GroupID          int64
	CreatorUserID    int64
	Deactivated      bool
	ParticipantType  int32
	ParticipantState int32
	AdminsCanManage  bool
}

func (p AutoMessageGroupPermission) CanManage(actorUserID int64) bool {
	if actorUserID == 0 || p.Deactivated {
		return false
	}
	if p.CreatorUserID == actorUserID || p.ParticipantType == mtproto.ChatMemberCreator {
		return p.ParticipantState == mtproto.ChatMemberStateNormal
	}
	if p.ParticipantType == mtproto.ChatMemberAdmin {
		return p.ParticipantState == mtproto.ChatMemberStateNormal && p.AdminsCanManage
	}
	return false
}

func (s *Store) EnsureAutoMessageSchema(ctx context.Context) error {
	statements := []string{
		`CREATE TABLE IF NOT EXISTS group_auto_message_config (
			id BIGINT NOT NULL AUTO_INCREMENT PRIMARY KEY,
			group_id BIGINT NOT NULL,
			enabled TINYINT(1) NOT NULL DEFAULT 0,
			interval_minutes INT NOT NULL DEFAULT 30,
			send_mode VARCHAR(32) NOT NULL DEFAULT 'sequence',
			first_send_mode VARCHAR(32) NOT NULL DEFAULT 'delay',
			current_index INT NOT NULL DEFAULT 0,
			next_send_at TIMESTAMP NULL DEFAULT NULL,
			last_send_at TIMESTAMP NULL DEFAULT NULL,
			last_send_status VARCHAR(32) NOT NULL DEFAULT '',
			last_error_code VARCHAR(64) NOT NULL DEFAULT '',
			last_error_message VARCHAR(512) NOT NULL DEFAULT '',
			admins_can_manage TINYINT(1) NOT NULL DEFAULT 1,
			version INT NOT NULL DEFAULT 0,
			created_by BIGINT NOT NULL,
			updated_by BIGINT NOT NULL,
			created_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
			updated_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP ON UPDATE CURRENT_TIMESTAMP,
			deleted_at TIMESTAMP NULL DEFAULT NULL,
			UNIQUE KEY uk_group_auto_message_config_group (group_id),
			KEY idx_group_auto_message_config_due (enabled, next_send_at, deleted_at),
			KEY idx_group_auto_message_config_updated (updated_at)
		) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci;`,
		`CREATE TABLE IF NOT EXISTS group_auto_message_items (
			id BIGINT NOT NULL AUTO_INCREMENT PRIMARY KEY,
			config_id BIGINT NOT NULL,
			group_id BIGINT NOT NULL,
			sort_order INT NOT NULL,
			content VARCHAR(1000) NOT NULL,
			content_type VARCHAR(32) NOT NULL DEFAULT 'text',
			enabled TINYINT(1) NOT NULL DEFAULT 1,
			created_by BIGINT NOT NULL,
			updated_by BIGINT NOT NULL,
			created_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
			updated_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP ON UPDATE CURRENT_TIMESTAMP,
			deleted_at TIMESTAMP NULL DEFAULT NULL,
			KEY idx_group_auto_message_items_config_order (config_id, enabled, deleted_at, sort_order, id),
			KEY idx_group_auto_message_items_group (group_id),
			KEY idx_group_auto_message_items_updated (updated_at)
		) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci;`,
		`CREATE TABLE IF NOT EXISTS group_auto_message_logs (
			id BIGINT NOT NULL AUTO_INCREMENT PRIMARY KEY,
			group_id BIGINT NOT NULL,
			config_id BIGINT NOT NULL,
			item_id BIGINT NULL,
			content_snapshot VARCHAR(1000) NULL,
			send_index INT NULL,
			sent_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
			status VARCHAR(32) NOT NULL,
			error_code VARCHAR(64) NOT NULL DEFAULT '',
			error_message VARCHAR(512) NOT NULL DEFAULT '',
			message_id BIGINT NULL,
			created_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
			KEY idx_group_auto_message_logs_group_sent (group_id, sent_at, id),
			KEY idx_group_auto_message_logs_config_sent (config_id, sent_at, id),
			KEY idx_group_auto_message_logs_status (status)
		) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci;`,
	}

	for _, stmt := range statements {
		if _, err := s.db.ExecContext(ctx, stmt); err != nil {
			return fmt.Errorf("ensure auto message schema: %w", err)
		}
	}

	return nil
}

func (s *Store) GetAutoMessagePermission(ctx context.Context, groupID, actorUserID int64) (AutoMessageGroupPermission, error) {
	const query = `
		SELECT c.id, c.creator_user_id, c.deactivated,
		       COALESCE(p.participant_type, -1),
		       COALESCE(p.state, -1),
		       COALESCE(cfg.admins_can_manage, 1)
		FROM chats c
		LEFT JOIN chat_participants p ON p.chat_id = c.id AND p.user_id = ?
		LEFT JOIN group_auto_message_config cfg ON cfg.group_id = c.id AND cfg.deleted_at IS NULL
		WHERE c.id = ?`

	var p AutoMessageGroupPermission
	if err := s.db.QueryRowContext(ctx, query, actorUserID, groupID).Scan(
		&p.GroupID,
		&p.CreatorUserID,
		&p.Deactivated,
		&p.ParticipantType,
		&p.ParticipantState,
		&p.AdminsCanManage,
	); err != nil {
		return AutoMessageGroupPermission{}, err
	}
	return p, nil
}

func (s *Store) GetAutoMessageConfigDetail(ctx context.Context, groupID int64) (*AutoMessageConfigDetail, error) {
	cfg, err := s.getAutoMessageConfigByGroup(ctx, groupID)
	if err != nil {
		return nil, err
	}
	items, err := s.listAutoMessageItems(ctx, cfg.ID, false)
	if err != nil {
		return nil, err
	}
	detail := &AutoMessageConfigDetail{
		Config:           *cfg,
		Items:            items,
		EnabledItemCount: countEnabledItems(items),
	}
	if detail.EnabledItemCount > 0 {
		enabled := enabledItems(items)
		idx := cfg.CurrentIndex
		if idx < 0 || idx >= len(enabled) {
			idx = 0
		}
		detail.NextItem = &enabled[idx]
	}
	return detail, nil
}

func (s *Store) EnsureAutoMessageConfig(ctx context.Context, groupID, actorUserID int64) (*AutoMessageConfig, error) {
	cfg, err := s.getAutoMessageConfigByGroup(ctx, groupID)
	if err == nil {
		return cfg, nil
	}
	if !errors.Is(err, ErrAutoMessageConfigNotFound) {
		return nil, err
	}
	const query = `
		INSERT INTO group_auto_message_config (
			group_id, enabled, interval_minutes, send_mode, first_send_mode,
			current_index, admins_can_manage, created_by, updated_by
		) VALUES (?, 0, 30, 'sequence', 'delay', 0, 1, ?, ?)`
	result, err := s.db.ExecContext(ctx, query, groupID, actorUserID, actorUserID)
	if err != nil {
		return nil, err
	}
	id, _ := result.LastInsertId()
	cfg, err = s.getAutoMessageConfigByGroup(ctx, groupID)
	if err != nil {
		return nil, err
	}
	if cfg.ID == 0 {
		cfg.ID = id
	}
	return cfg, nil
}

func (s *Store) SaveAutoMessageConfig(ctx context.Context, groupID, actorUserID int64, params SaveAutoMessageConfigParams, now time.Time) (*AutoMessageConfigDetail, error) {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	defer rollbackQuietly(tx)

	cfg, err := s.ensureAutoMessageConfigTx(ctx, tx, groupID, actorUserID)
	if err != nil {
		return nil, err
	}
	if params.Version > 0 && params.Version != cfg.Version {
		return nil, ErrAutoMessageConflict
	}

	existing, err := listAutoMessageItemsTx(ctx, tx, cfg.ID, true)
	if err != nil {
		return nil, err
	}
	seen := make(map[int64]struct{}, len(params.Items))
	for i, item := range params.Items {
		sortOrder := item.SortOrder
		if sortOrder <= 0 {
			sortOrder = i + 1
		}
		contentType := strings.TrimSpace(item.ContentType)
		if contentType == "" {
			contentType = AutoMessageContentTypeText
		}
		if item.ID > 0 {
			if !itemBelongsTo(existing, item.ID) {
				return nil, ErrAutoMessageItemNotFound
			}
			if _, err = tx.ExecContext(ctx, `
				UPDATE group_auto_message_items
				SET sort_order = ?, content = ?, content_type = ?, enabled = ?, updated_by = ?
				WHERE id = ? AND config_id = ? AND deleted_at IS NULL`,
				sortOrder, item.Content, contentType, item.Enabled, actorUserID, item.ID, cfg.ID); err != nil {
				return nil, err
			}
			seen[item.ID] = struct{}{}
			continue
		}
		if _, err = tx.ExecContext(ctx, `
			INSERT INTO group_auto_message_items (
				config_id, group_id, sort_order, content, content_type, enabled, created_by, updated_by
			) VALUES (?, ?, ?, ?, ?, ?, ?, ?)`,
			cfg.ID, groupID, sortOrder, item.Content, contentType, item.Enabled, actorUserID, actorUserID); err != nil {
			return nil, err
		}
	}
	for _, item := range existing {
		if _, ok := seen[item.ID]; ok {
			continue
		}
		if _, err = tx.ExecContext(ctx, `
			UPDATE group_auto_message_items
			SET deleted_at = CURRENT_TIMESTAMP, updated_by = ?
			WHERE id = ? AND config_id = ? AND deleted_at IS NULL`,
			actorUserID, item.ID, cfg.ID); err != nil {
			return nil, err
		}
	}

	nextSendAt := sql.NullTime{}
	currentIndex := cfg.CurrentIndex
	if params.Enabled {
		currentIndex = 0
		nextSendAt = sql.NullTime{Time: now.Add(time.Duration(params.IntervalMinutes) * time.Minute), Valid: true}
	} else if cfg.NextSendAt != nil {
		nextSendAt = sql.NullTime{Time: *cfg.NextSendAt, Valid: true}
	}

	if _, err = tx.ExecContext(ctx, `
		UPDATE group_auto_message_config
		SET enabled = ?, interval_minutes = ?, send_mode = ?, first_send_mode = ?,
		    current_index = ?, next_send_at = ?, admins_can_manage = ?,
		    updated_by = ?, version = version + 1
		WHERE id = ? AND deleted_at IS NULL`,
		params.Enabled, params.IntervalMinutes, params.SendMode, params.FirstSendMode,
		currentIndex, nullTimeArg(nextSendAt), params.AdminsCanManage,
		actorUserID, cfg.ID); err != nil {
		return nil, err
	}

	if err = tx.Commit(); err != nil {
		return nil, err
	}
	return s.GetAutoMessageConfigDetail(ctx, groupID)
}

func (s *Store) EnableAutoMessageConfig(ctx context.Context, groupID, actorUserID int64, intervalMinutes int, now time.Time) (*AutoMessageConfigDetail, error) {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	defer rollbackQuietly(tx)

	cfg, err := s.ensureAutoMessageConfigTx(ctx, tx, groupID, actorUserID)
	if err != nil {
		return nil, err
	}
	if intervalMinutes <= 0 {
		intervalMinutes = cfg.IntervalMinutes
	}
	items, err := listAutoMessageItemsTx(ctx, tx, cfg.ID, false)
	if err != nil {
		return nil, err
	}
	if countEnabledItems(items) == 0 {
		return nil, ErrAutoMessageItemNotFound
	}
	_, err = tx.ExecContext(ctx, `
		UPDATE group_auto_message_config
		SET enabled = 1, interval_minutes = ?, current_index = 0,
		    next_send_at = ?, updated_by = ?, version = version + 1
		WHERE id = ? AND deleted_at IS NULL`,
		intervalMinutes, now.Add(time.Duration(intervalMinutes)*time.Minute), actorUserID, cfg.ID)
	if err != nil {
		return nil, err
	}
	if err = tx.Commit(); err != nil {
		return nil, err
	}
	return s.GetAutoMessageConfigDetail(ctx, groupID)
}

func (s *Store) DisableAutoMessageConfig(ctx context.Context, groupID, actorUserID int64) (*AutoMessageConfigDetail, error) {
	cfg, err := s.EnsureAutoMessageConfig(ctx, groupID, actorUserID)
	if err != nil {
		return nil, err
	}
	if _, err = s.db.ExecContext(ctx, `
		UPDATE group_auto_message_config
		SET enabled = 0, updated_by = ?, version = version + 1
		WHERE id = ? AND deleted_at IS NULL`,
		actorUserID, cfg.ID); err != nil {
		return nil, err
	}
	return s.GetAutoMessageConfigDetail(ctx, groupID)
}

func (s *Store) AddAutoMessageItem(ctx context.Context, groupID, actorUserID int64, input AutoMessageItemInput) (*AutoMessageConfigDetail, error) {
	cfg, err := s.EnsureAutoMessageConfig(ctx, groupID, actorUserID)
	if err != nil {
		return nil, err
	}
	count, err := s.countAutoMessageItems(ctx, cfg.ID)
	if err != nil {
		return nil, err
	}
	sortOrder := input.SortOrder
	if sortOrder <= 0 {
		sortOrder = count + 1
	}
	contentType := strings.TrimSpace(input.ContentType)
	if contentType == "" {
		contentType = AutoMessageContentTypeText
	}
	_, err = s.db.ExecContext(ctx, `
		INSERT INTO group_auto_message_items (
			config_id, group_id, sort_order, content, content_type, enabled, created_by, updated_by
		) VALUES (?, ?, ?, ?, ?, ?, ?, ?)`,
		cfg.ID, groupID, sortOrder, input.Content, contentType, input.Enabled, actorUserID, actorUserID)
	if err != nil {
		return nil, err
	}
	return s.GetAutoMessageConfigDetail(ctx, groupID)
}

func (s *Store) UpdateAutoMessageItem(ctx context.Context, groupID, actorUserID, itemID int64, input AutoMessageItemInput) (*AutoMessageConfigDetail, error) {
	cfg, err := s.EnsureAutoMessageConfig(ctx, groupID, actorUserID)
	if err != nil {
		return nil, err
	}
	contentType := strings.TrimSpace(input.ContentType)
	if contentType == "" {
		contentType = AutoMessageContentTypeText
	}
	result, err := s.db.ExecContext(ctx, `
		UPDATE group_auto_message_items
		SET content = ?, content_type = ?, enabled = ?, updated_by = ?
		WHERE id = ? AND config_id = ? AND group_id = ? AND deleted_at IS NULL`,
		input.Content, contentType, input.Enabled, actorUserID, itemID, cfg.ID, groupID)
	if err != nil {
		return nil, err
	}
	if affected, _ := result.RowsAffected(); affected == 0 {
		return nil, ErrAutoMessageItemNotFound
	}
	if err = s.normalizeAutoMessageCurrentIndex(ctx, cfg.ID); err != nil {
		return nil, err
	}
	return s.GetAutoMessageConfigDetail(ctx, groupID)
}

func (s *Store) DeleteAutoMessageItem(ctx context.Context, groupID, actorUserID, itemID int64) (*AutoMessageConfigDetail, error) {
	cfg, err := s.EnsureAutoMessageConfig(ctx, groupID, actorUserID)
	if err != nil {
		return nil, err
	}
	result, err := s.db.ExecContext(ctx, `
		UPDATE group_auto_message_items
		SET deleted_at = CURRENT_TIMESTAMP, updated_by = ?
		WHERE id = ? AND config_id = ? AND group_id = ? AND deleted_at IS NULL`,
		actorUserID, itemID, cfg.ID, groupID)
	if err != nil {
		return nil, err
	}
	if affected, _ := result.RowsAffected(); affected == 0 {
		return nil, ErrAutoMessageItemNotFound
	}
	if err = s.normalizeAutoMessageCurrentIndex(ctx, cfg.ID); err != nil {
		return nil, err
	}
	return s.GetAutoMessageConfigDetail(ctx, groupID)
}

func (s *Store) SortAutoMessageItems(ctx context.Context, groupID, actorUserID int64, itemIDs []int64) (*AutoMessageConfigDetail, error) {
	cfg, err := s.EnsureAutoMessageConfig(ctx, groupID, actorUserID)
	if err != nil {
		return nil, err
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	defer rollbackQuietly(tx)
	existing, err := listAutoMessageItemsTx(ctx, tx, cfg.ID, true)
	if err != nil {
		return nil, err
	}
	if len(itemIDs) != len(existing) {
		return nil, ErrAutoMessageItemNotFound
	}
	seen := make(map[int64]struct{}, len(itemIDs))
	for order, itemID := range itemIDs {
		if !itemBelongsTo(existing, itemID) {
			return nil, ErrAutoMessageItemNotFound
		}
		if _, ok := seen[itemID]; ok {
			return nil, ErrAutoMessageItemNotFound
		}
		seen[itemID] = struct{}{}
		if _, err = tx.ExecContext(ctx, `
			UPDATE group_auto_message_items
			SET sort_order = ?, updated_by = ?
			WHERE id = ? AND config_id = ? AND deleted_at IS NULL`,
			order+1, actorUserID, itemID, cfg.ID); err != nil {
			return nil, err
		}
	}
	if _, err = tx.ExecContext(ctx, `
		UPDATE group_auto_message_config
		SET version = version + 1, updated_by = ?
		WHERE id = ?`, actorUserID, cfg.ID); err != nil {
		return nil, err
	}
	if err = tx.Commit(); err != nil {
		return nil, err
	}
	if err = s.normalizeAutoMessageCurrentIndex(ctx, cfg.ID); err != nil {
		return nil, err
	}
	return s.GetAutoMessageConfigDetail(ctx, groupID)
}

func (s *Store) ListAutoMessageLogs(ctx context.Context, groupID int64, page, pageSize int) (AutoMessageLogList, error) {
	if page < 1 {
		page = 1
	}
	if pageSize < 1 {
		pageSize = 20
	}
	if pageSize > 100 {
		pageSize = 100
	}
	var total int
	if err := s.db.QueryRowContext(ctx, `
		SELECT COUNT(*) FROM group_auto_message_logs WHERE group_id = ?`, groupID).Scan(&total); err != nil {
		return AutoMessageLogList{}, err
	}
	rows, err := s.db.QueryContext(ctx, `
		SELECT id, group_id, config_id, COALESCE(item_id, 0), COALESCE(content_snapshot, ''),
		       COALESCE(send_index, 0), sent_at, status, error_code, error_message,
		       COALESCE(message_id, 0), created_at
		FROM group_auto_message_logs
		WHERE group_id = ?
		ORDER BY sent_at DESC, id DESC
		LIMIT ? OFFSET ?`, groupID, pageSize, (page-1)*pageSize)
	if err != nil {
		return AutoMessageLogList{}, err
	}
	defer rows.Close()
	items := make([]AutoMessageLog, 0, pageSize)
	for rows.Next() {
		var item AutoMessageLog
		if err = rows.Scan(
			&item.ID,
			&item.GroupID,
			&item.ConfigID,
			&item.ItemID,
			&item.ContentSnapshot,
			&item.SendIndex,
			&item.SentAt,
			&item.Status,
			&item.ErrorCode,
			&item.ErrorMessage,
			&item.MessageID,
			&item.CreatedAt,
		); err != nil {
			return AutoMessageLogList{}, err
		}
		items = append(items, item)
	}
	return AutoMessageLogList{Items: items, Total: total, Page: page, PageSize: pageSize}, rows.Err()
}

func (s *Store) ListDueAutoMessageConfigs(ctx context.Context, now time.Time, limit, shardTotal, shardIndex int) ([]AutoMessageConfig, error) {
	if limit <= 0 {
		limit = 500
	}
	args := []any{now}
	shardClause := ""
	if shardTotal > 1 {
		shardClause = " AND MOD(group_id, ?) = ?"
		args = append(args, shardTotal, shardIndex)
	}
	args = append(args, limit)
	rows, err := s.db.QueryContext(ctx, `
		SELECT id, group_id, enabled, interval_minutes, send_mode, first_send_mode,
		       current_index, next_send_at, last_send_at, last_send_status,
		       last_error_code, last_error_message, admins_can_manage, version,
		       created_by, updated_by, created_at, updated_at
		FROM group_auto_message_config
		WHERE enabled = 1 AND deleted_at IS NULL AND next_send_at IS NOT NULL AND next_send_at <= ?`+shardClause+`
		ORDER BY next_send_at ASC, id ASC
		LIMIT ?`, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var configs []AutoMessageConfig
	for rows.Next() {
		cfg, err := scanAutoMessageConfig(rows)
		if err != nil {
			return nil, err
		}
		configs = append(configs, *cfg)
	}
	return configs, rows.Err()
}

type AutoMessageSendCandidate struct {
	Config    AutoMessageConfig
	Item      AutoMessageItem
	SendIndex int
	BizID     string
}

type AutoMessageSendResult struct {
	Success      bool
	MessageID    int64
	ErrorCode    string
	ErrorMessage string
}

func (s *Store) ProcessDueAutoMessageConfig(ctx context.Context, configID int64, now time.Time, send func(AutoMessageSendCandidate) AutoMessageSendResult) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer rollbackQuietly(tx)

	cfg, err := getAutoMessageConfigByIDTx(ctx, tx, configID, true)
	if err != nil {
		return err
	}
	if !cfg.Enabled || cfg.NextSendAt == nil || cfg.NextSendAt.After(now) {
		return tx.Commit()
	}

	active, err := isAutoMessageGroupActiveTx(ctx, tx, cfg.GroupID)
	if err != nil {
		return err
	}
	if !active {
		sentAt := now
		if _, err = insertAutoMessageLogTx(ctx, tx, AutoMessageLog{
			GroupID:      cfg.GroupID,
			ConfigID:     cfg.ID,
			SentAt:       sentAt,
			Status:       AutoMessageStatusFailed,
			ErrorCode:    "AUTO_MESSAGE_GROUP_DISSOLVED",
			ErrorMessage: "group is dissolved",
		}); err != nil {
			return err
		}
		_, err = tx.ExecContext(ctx, `
			UPDATE group_auto_message_config
			SET enabled = 0, next_send_at = NULL, last_send_at = ?, last_send_status = ?,
			    last_error_code = ?, last_error_message = ?, version = version + 1
			WHERE id = ?`, sentAt, AutoMessageStatusFailed, "AUTO_MESSAGE_GROUP_DISSOLVED", "group is dissolved", cfg.ID)
		if err != nil {
			return err
		}
		return tx.Commit()
	}

	items, err := listAutoMessageItemsTx(ctx, tx, cfg.ID, false)
	if err != nil {
		return err
	}
	enabled := enabledItems(items)
	if len(enabled) == 0 {
		next := now.Add(time.Duration(cfg.IntervalMinutes) * time.Minute)
		if _, err = insertAutoMessageLogTx(ctx, tx, AutoMessageLog{
			GroupID:      cfg.GroupID,
			ConfigID:     cfg.ID,
			SentAt:       now,
			Status:       AutoMessageStatusFailed,
			ErrorCode:    "AUTO_MESSAGE_NO_ENABLED_ITEMS",
			ErrorMessage: "no enabled auto message items",
		}); err != nil {
			return err
		}
		_, err = tx.ExecContext(ctx, `
			UPDATE group_auto_message_config
			SET next_send_at = ?, last_send_at = ?, last_send_status = ?,
			    last_error_code = ?, last_error_message = ?, version = version + 1
			WHERE id = ?`, next, now, AutoMessageStatusFailed, "AUTO_MESSAGE_NO_ENABLED_ITEMS", "no enabled auto message items", cfg.ID)
		if err != nil {
			return err
		}
		return tx.Commit()
	}
	currentIndex := cfg.CurrentIndex
	if currentIndex < 0 || currentIndex >= len(enabled) {
		currentIndex = 0
	}
	item := enabled[currentIndex]
	scheduledAt := cfg.NextSendAt.UTC().Unix()
	candidate := AutoMessageSendCandidate{
		Config:    *cfg,
		Item:      item,
		SendIndex: currentIndex + 1,
		BizID:     fmt.Sprintf("auto_message:%d:%d:%d:%d", cfg.ID, scheduledAt, currentIndex, item.ID),
	}

	result := send(candidate)
	status := AutoMessageStatusFailed
	if result.Success {
		status = AutoMessageStatusSuccess
	}
	if _, err = insertAutoMessageLogTx(ctx, tx, AutoMessageLog{
		GroupID:         cfg.GroupID,
		ConfigID:        cfg.ID,
		ItemID:          item.ID,
		ContentSnapshot: item.Content,
		SendIndex:       candidate.SendIndex,
		SentAt:          now,
		Status:          status,
		ErrorCode:       result.ErrorCode,
		ErrorMessage:    result.ErrorMessage,
		MessageID:       result.MessageID,
	}); err != nil {
		return err
	}

	nextSendAt := now.Add(time.Duration(cfg.IntervalMinutes) * time.Minute)
	nextIndex := currentIndex
	if result.Success {
		nextIndex = (currentIndex + 1) % len(enabled)
	}
	_, err = tx.ExecContext(ctx, `
		UPDATE group_auto_message_config
		SET current_index = ?, next_send_at = ?, last_send_at = ?, last_send_status = ?,
		    last_error_code = ?, last_error_message = ?, version = version + 1
		WHERE id = ?`,
		nextIndex, nextSendAt, now, status, result.ErrorCode, result.ErrorMessage, cfg.ID)
	if err != nil {
		return err
	}
	return tx.Commit()
}

func (s *Store) getAutoMessageConfigByGroup(ctx context.Context, groupID int64) (*AutoMessageConfig, error) {
	row := s.db.QueryRowContext(ctx, `
		SELECT id, group_id, enabled, interval_minutes, send_mode, first_send_mode,
		       current_index, next_send_at, last_send_at, last_send_status,
		       last_error_code, last_error_message, admins_can_manage, version,
		       created_by, updated_by, created_at, updated_at
		FROM group_auto_message_config
		WHERE group_id = ? AND deleted_at IS NULL`, groupID)
	cfg, err := scanAutoMessageConfig(row)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrAutoMessageConfigNotFound
	}
	return cfg, err
}

func (s *Store) ensureAutoMessageConfigTx(ctx context.Context, tx *sql.Tx, groupID, actorUserID int64) (*AutoMessageConfig, error) {
	row := tx.QueryRowContext(ctx, `
		SELECT id, group_id, enabled, interval_minutes, send_mode, first_send_mode,
		       current_index, next_send_at, last_send_at, last_send_status,
		       last_error_code, last_error_message, admins_can_manage, version,
		       created_by, updated_by, created_at, updated_at
		FROM group_auto_message_config
		WHERE group_id = ? AND deleted_at IS NULL
		FOR UPDATE`, groupID)
	cfg, err := scanAutoMessageConfig(row)
	if err == nil {
		return cfg, nil
	}
	if !errors.Is(err, sql.ErrNoRows) {
		return nil, err
	}
	result, err := tx.ExecContext(ctx, `
		INSERT INTO group_auto_message_config (
			group_id, enabled, interval_minutes, send_mode, first_send_mode,
			current_index, admins_can_manage, created_by, updated_by
		) VALUES (?, 0, 30, 'sequence', 'delay', 0, 1, ?, ?)`, groupID, actorUserID, actorUserID)
	if err != nil {
		return nil, err
	}
	id, _ := result.LastInsertId()
	return getAutoMessageConfigByIDTx(ctx, tx, id, true)
}

func getAutoMessageConfigByIDTx(ctx context.Context, tx *sql.Tx, id int64, forUpdate bool) (*AutoMessageConfig, error) {
	query := `
		SELECT id, group_id, enabled, interval_minutes, send_mode, first_send_mode,
		       current_index, next_send_at, last_send_at, last_send_status,
		       last_error_code, last_error_message, admins_can_manage, version,
		       created_by, updated_by, created_at, updated_at
		FROM group_auto_message_config
		WHERE id = ? AND deleted_at IS NULL`
	if forUpdate {
		query += " FOR UPDATE"
	}
	cfg, err := scanAutoMessageConfig(tx.QueryRowContext(ctx, query, id))
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrAutoMessageConfigNotFound
	}
	return cfg, err
}

type autoMessageScanner interface {
	Scan(dest ...any) error
}

func scanAutoMessageConfig(row autoMessageScanner) (*AutoMessageConfig, error) {
	var (
		cfg        AutoMessageConfig
		nextSendAt sql.NullTime
		lastSendAt sql.NullTime
	)
	if err := row.Scan(
		&cfg.ID,
		&cfg.GroupID,
		&cfg.Enabled,
		&cfg.IntervalMinutes,
		&cfg.SendMode,
		&cfg.FirstSendMode,
		&cfg.CurrentIndex,
		&nextSendAt,
		&lastSendAt,
		&cfg.LastSendStatus,
		&cfg.LastErrorCode,
		&cfg.LastErrorMessage,
		&cfg.AdminsCanManage,
		&cfg.Version,
		&cfg.CreatedBy,
		&cfg.UpdatedBy,
		&cfg.CreatedAt,
		&cfg.UpdatedAt,
	); err != nil {
		return nil, err
	}
	if nextSendAt.Valid {
		cfg.NextSendAt = &nextSendAt.Time
	}
	if lastSendAt.Valid {
		cfg.LastSendAt = &lastSendAt.Time
	}
	return &cfg, nil
}

func (s *Store) listAutoMessageItems(ctx context.Context, configID int64, includeDeleted bool) ([]AutoMessageItem, error) {
	return listAutoMessageItemsWithDB(ctx, s.db, configID, includeDeleted)
}

func listAutoMessageItemsTx(ctx context.Context, tx *sql.Tx, configID int64, includeDeleted bool) ([]AutoMessageItem, error) {
	return listAutoMessageItemsWithDB(ctx, tx, configID, includeDeleted)
}

type queryer interface {
	QueryContext(context.Context, string, ...any) (*sql.Rows, error)
}

func listAutoMessageItemsWithDB(ctx context.Context, q queryer, configID int64, includeDeleted bool) ([]AutoMessageItem, error) {
	deletedClause := " AND deleted_at IS NULL"
	if includeDeleted {
		deletedClause = ""
	}
	rows, err := q.QueryContext(ctx, `
		SELECT id, config_id, group_id, sort_order, content, content_type,
		       enabled, created_by, updated_by, created_at, updated_at
		FROM group_auto_message_items
		WHERE config_id = ?`+deletedClause+`
		ORDER BY sort_order ASC, id ASC`, configID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items := make([]AutoMessageItem, 0)
	for rows.Next() {
		var item AutoMessageItem
		if err = rows.Scan(
			&item.ID,
			&item.ConfigID,
			&item.GroupID,
			&item.SortOrder,
			&item.Content,
			&item.ContentType,
			&item.Enabled,
			&item.CreatedBy,
			&item.UpdatedBy,
			&item.CreatedAt,
			&item.UpdatedAt,
		); err != nil {
			return nil, err
		}
		items = append(items, item)
	}
	return items, rows.Err()
}

func (s *Store) countAutoMessageItems(ctx context.Context, configID int64) (int, error) {
	var count int
	err := s.db.QueryRowContext(ctx, `
		SELECT COUNT(*) FROM group_auto_message_items
		WHERE config_id = ? AND deleted_at IS NULL`, configID).Scan(&count)
	return count, err
}

func (s *Store) normalizeAutoMessageCurrentIndex(ctx context.Context, configID int64) error {
	var currentIndex int
	if err := s.db.QueryRowContext(ctx, `
		SELECT current_index FROM group_auto_message_config WHERE id = ?`, configID).Scan(&currentIndex); err != nil {
		return err
	}
	items, err := s.listAutoMessageItems(ctx, configID, false)
	if err != nil {
		return err
	}
	enabledCount := countEnabledItems(items)
	if enabledCount == 0 || currentIndex < enabledCount {
		return nil
	}
	_, err = s.db.ExecContext(ctx, `
		UPDATE group_auto_message_config
		SET current_index = 0, version = version + 1
		WHERE id = ?`, configID)
	return err
}

func isAutoMessageGroupActiveTx(ctx context.Context, tx *sql.Tx, groupID int64) (bool, error) {
	var deactivated bool
	if err := tx.QueryRowContext(ctx, `
		SELECT deactivated FROM chats WHERE id = ?`, groupID).Scan(&deactivated); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return false, nil
		}
		return false, err
	}
	return !deactivated, nil
}

func insertAutoMessageLogTx(ctx context.Context, tx *sql.Tx, log AutoMessageLog) (int64, error) {
	result, err := tx.ExecContext(ctx, `
		INSERT INTO group_auto_message_logs (
			group_id, config_id, item_id, content_snapshot, send_index,
			sent_at, status, error_code, error_message, message_id
		) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		log.GroupID,
		log.ConfigID,
		nullableInt64(log.ItemID),
		nullableString(log.ContentSnapshot),
		nullableInt(log.SendIndex),
		log.SentAt,
		log.Status,
		log.ErrorCode,
		log.ErrorMessage,
		nullableInt64(log.MessageID),
	)
	if err != nil {
		return 0, err
	}
	id, _ := result.LastInsertId()
	return id, nil
}

func enabledItems(items []AutoMessageItem) []AutoMessageItem {
	out := make([]AutoMessageItem, 0, len(items))
	for _, item := range items {
		if item.Enabled {
			out = append(out, item)
		}
	}
	return out
}

func countEnabledItems(items []AutoMessageItem) int {
	count := 0
	for _, item := range items {
		if item.Enabled {
			count++
		}
	}
	return count
}

func itemBelongsTo(items []AutoMessageItem, itemID int64) bool {
	for _, item := range items {
		if item.ID == itemID {
			return true
		}
	}
	return false
}

func rollbackQuietly(tx *sql.Tx) {
	if tx != nil {
		_ = tx.Rollback()
	}
}

func nullTimeArg(t sql.NullTime) any {
	if t.Valid {
		return t.Time
	}
	return nil
}

func nullableInt64(v int64) any {
	if v == 0 {
		return nil
	}
	return v
}

func nullableInt(v int) any {
	if v == 0 {
		return nil
	}
	return v
}

func nullableString(v string) any {
	if strings.TrimSpace(v) == "" {
		return nil
	}
	return v
}
