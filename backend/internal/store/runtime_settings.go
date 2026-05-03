package store

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"
)

const inviteCodeSettingsKey = "signup_invite_settings"
const defaultAdminContactsSettingsKey = "default_admin_contact_user_ids"

type InviteCodeSettingsRecord struct {
	Enabled   bool  `json:"enabled"`
	UpdatedAt int64 `json:"updatedAt"`
}

type DefaultAdminContactsSettingsRecord struct {
	UserIDs   []int64 `json:"userIds"`
	UpdatedAt int64   `json:"updatedAt"`
}

func DefaultInviteCodeSettingsRecord() InviteCodeSettingsRecord {
	return InviteCodeSettingsRecord{
		Enabled:   false,
		UpdatedAt: 0,
	}
}

func DefaultAdminContactsSettingsRecordValue() DefaultAdminContactsSettingsRecord {
	return DefaultAdminContactsSettingsRecord{
		UserIDs:   []int64{},
		UpdatedAt: 0,
	}
}

func (s *Store) EnsureRuntimeSettingsSchema(ctx context.Context) error {
	_, err := s.db.ExecContext(ctx, `
		CREATE TABLE IF NOT EXISTS admin_runtime_settings (
			setting_key VARCHAR(64) NOT NULL PRIMARY KEY,
			setting_value_json JSON NOT NULL,
			created_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
			updated_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP ON UPDATE CURRENT_TIMESTAMP
		) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci;`)
	if err != nil {
		return fmt.Errorf("ensure runtime settings schema: %w", err)
	}
	return nil
}

func (s *Store) GetInviteCodeSettings(ctx context.Context) (InviteCodeSettingsRecord, bool, error) {
	settings := DefaultInviteCodeSettingsRecord()

	var raw []byte
	err := s.db.QueryRowContext(ctx, `
		SELECT setting_value_json
		FROM admin_runtime_settings
		WHERE setting_key = ?`, inviteCodeSettingsKey).Scan(&raw)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return settings, false, nil
		}
		return settings, false, err
	}

	if len(raw) == 0 {
		return settings, false, nil
	}
	if err := json.Unmarshal(raw, &settings); err != nil {
		return DefaultInviteCodeSettingsRecord(), false, err
	}
	return settings, true, nil
}

func (s *Store) SaveInviteCodeSettings(ctx context.Context, enabled bool) (InviteCodeSettingsRecord, error) {
	settings := InviteCodeSettingsRecord{
		Enabled:   enabled,
		UpdatedAt: time.Now().Unix(),
	}

	payload, err := json.Marshal(&settings)
	if err != nil {
		return settings, fmt.Errorf("marshal invite settings: %w", err)
	}

	_, err = s.db.ExecContext(ctx, `
		INSERT INTO admin_runtime_settings (setting_key, setting_value_json)
		VALUES (?, ?)
		ON DUPLICATE KEY UPDATE
			setting_value_json = VALUES(setting_value_json)`, inviteCodeSettingsKey, string(payload))
	if err != nil {
		return settings, fmt.Errorf("save invite settings: %w", err)
	}

	return settings, nil
}

func (s *Store) GetDefaultAdminContactsSettings(ctx context.Context) (DefaultAdminContactsSettingsRecord, bool, error) {
	settings := DefaultAdminContactsSettingsRecordValue()

	var raw []byte
	err := s.db.QueryRowContext(ctx, `
		SELECT setting_value_json
		FROM admin_runtime_settings
		WHERE setting_key = ?`, defaultAdminContactsSettingsKey).Scan(&raw)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return settings, false, nil
		}
		return settings, false, err
	}

	if len(raw) == 0 {
		return settings, false, nil
	}
	if err := json.Unmarshal(raw, &settings); err != nil {
		return DefaultAdminContactsSettingsRecordValue(), false, err
	}
	settings.UserIDs = normalizeUserIDList(settings.UserIDs)
	return settings, true, nil
}

func (s *Store) SaveDefaultAdminContactsSettings(ctx context.Context, userIDs []int64) (DefaultAdminContactsSettingsRecord, error) {
	userIDs = normalizeUserIDList(userIDs)
	if len(userIDs) > 0 {
		existing, err := s.ExistingUserIDs(ctx, userIDs)
		if err != nil {
			return DefaultAdminContactsSettingsRecordValue(), err
		}
		if len(existing) != len(userIDs) {
			return DefaultAdminContactsSettingsRecordValue(), fmt.Errorf("some user ids do not exist")
		}
	}

	settings := DefaultAdminContactsSettingsRecord{
		UserIDs:   userIDs,
		UpdatedAt: time.Now().Unix(),
	}

	payload, err := json.Marshal(&settings)
	if err != nil {
		return settings, fmt.Errorf("marshal default admin contacts settings: %w", err)
	}

	_, err = s.db.ExecContext(ctx, `
		INSERT INTO admin_runtime_settings (setting_key, setting_value_json)
		VALUES (?, ?)
		ON DUPLICATE KEY UPDATE
			setting_value_json = VALUES(setting_value_json)`, defaultAdminContactsSettingsKey, string(payload))
	if err != nil {
		return settings, fmt.Errorf("save default admin contacts settings: %w", err)
	}

	return settings, nil
}

func (s *Store) ExistingUserIDs(ctx context.Context, userIDs []int64) ([]int64, error) {
	userIDs = normalizeUserIDList(userIDs)
	if len(userIDs) == 0 {
		return []int64{}, nil
	}

	placeholders := make([]string, 0, len(userIDs))
	args := make([]any, 0, len(userIDs))
	for _, id := range userIDs {
		placeholders = append(placeholders, "?")
		args = append(args, id)
	}

	rows, err := s.db.QueryContext(ctx, `
		SELECT id
		FROM users
		WHERE deleted = 0
			AND id IN (`+strings.Join(placeholders, ",")+`)`, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	existing := make([]int64, 0, len(userIDs))
	for rows.Next() {
		var id int64
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}
		existing = append(existing, id)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}

	return normalizeUserIDList(existing), nil
}

func normalizeUserIDList(userIDs []int64) []int64 {
	if len(userIDs) == 0 {
		return []int64{}
	}

	seen := make(map[int64]struct{}, len(userIDs))
	result := make([]int64, 0, len(userIDs))
	for _, id := range userIDs {
		if id <= 0 {
			continue
		}
		if _, ok := seen[id]; ok {
			continue
		}
		seen[id] = struct{}{}
		result = append(result, id)
	}
	return result
}
