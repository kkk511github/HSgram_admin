package store

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"time"
)

const inviteCodeSettingsKey = "signup_invite_settings"

type InviteCodeSettingsRecord struct {
	Enabled   bool  `json:"enabled"`
	UpdatedAt int64 `json:"updatedAt"`
}

func DefaultInviteCodeSettingsRecord() InviteCodeSettingsRecord {
	return InviteCodeSettingsRecord{
		Enabled:   false,
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
