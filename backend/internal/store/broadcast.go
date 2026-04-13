package store

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"
)

const (
	BroadcastSystemUserID    int64 = 778000
	BroadcastSystemUsername        = "hsgram_notice"
	BroadcastSystemPhone           = "42778"
	BroadcastSystemFirstName       = "HSgram"
	BroadcastSystemLastName        = "Notice"

	BroadcastTargetTypeAll      = "all"
	BroadcastTargetTypeSelected = "selected"
	BroadcastTargetTypeFiltered = "filtered"

	BroadcastStatusPending   = "pending"
	BroadcastStatusRunning   = "running"
	BroadcastStatusCompleted = "completed"
	BroadcastStatusFailed    = "failed"

	BroadcastDeliverySent   = "sent"
	BroadcastDeliveryFailed = "failed"
)

type BroadcastFilters struct {
	Restricted  *bool  `json:"restricted,omitempty"`
	Deleted     *bool  `json:"deleted,omitempty"`
	IsBot       *bool  `json:"isBot,omitempty"`
	CountryCode string `json:"countryCode,omitempty"`
}

type BroadcastTargetSpec struct {
	Type            string           `json:"type"`
	ResolvedUserIDs []int64          `json:"resolvedUserIds,omitempty"`
	SourceValues    []string         `json:"sourceValues,omitempty"`
	Filters         BroadcastFilters `json:"filters,omitempty"`
}

type BroadcastPreview struct {
	Count       int           `json:"count"`
	SampleUsers []UserSummary `json:"sampleUsers"`
	Unresolved  []string      `json:"unresolved"`
}

type BroadcastRecord struct {
	ID                int64               `json:"id"`
	SenderUserID      int64               `json:"senderUserId"`
	SenderDisplayName string              `json:"senderDisplayName"`
	MessageText       string              `json:"messageText"`
	TargetType        string              `json:"targetType"`
	TargetSpec        json.RawMessage     `json:"targetSpec"`
	Status            string              `json:"status"`
	TotalTargets      int                 `json:"totalTargets"`
	SuccessCount      int                 `json:"successCount"`
	FailureCount      int                 `json:"failureCount"`
	ErrorMessage      string              `json:"errorMessage"`
	CreatedByAdminID  int64               `json:"createdByAdminId"`
	CreatedByUsername string              `json:"createdByUsername"`
	CreatedByRole     string              `json:"createdByRole"`
	StartedAt         *time.Time          `json:"startedAt,omitempty"`
	CompletedAt       *time.Time          `json:"completedAt,omitempty"`
	CreatedAt         time.Time           `json:"createdAt"`
	UpdatedAt         time.Time           `json:"updatedAt"`
	Deliveries        []BroadcastDelivery `json:"deliveries,omitempty"`
}

type BroadcastDelivery struct {
	ID                int64      `json:"id"`
	BroadcastID       int64      `json:"broadcastId"`
	TargetUserID      int64      `json:"targetUserId"`
	TargetDisplayName string     `json:"targetDisplayName"`
	Status            string     `json:"status"`
	ErrorMessage      string     `json:"errorMessage"`
	SentAt            *time.Time `json:"sentAt,omitempty"`
	CreatedAt         time.Time  `json:"createdAt"`
	UpdatedAt         time.Time  `json:"updatedAt"`
}

type BroadcastRecipient struct {
	UserID      int64  `json:"userId"`
	DisplayName string `json:"displayName"`
	Username    string `json:"username"`
	Phone       string `json:"phone"`
	CountryCode string `json:"countryCode"`
	Restricted  bool   `json:"restricted"`
	Deleted     bool   `json:"deleted"`
	IsBot       bool   `json:"isBot"`
}

func (s *Store) EnsureBroadcastSchema(ctx context.Context) error {
	statements := []string{
		`CREATE TABLE IF NOT EXISTS admin_broadcasts (
			id BIGINT NOT NULL AUTO_INCREMENT PRIMARY KEY,
			sender_user_id BIGINT NOT NULL,
			sender_display_name VARCHAR(128) NOT NULL,
			message_text TEXT NOT NULL,
			target_type VARCHAR(16) NOT NULL,
			target_spec_json JSON NOT NULL,
			status VARCHAR(16) NOT NULL DEFAULT 'pending',
			total_targets INT NOT NULL DEFAULT 0,
			success_count INT NOT NULL DEFAULT 0,
			failure_count INT NOT NULL DEFAULT 0,
			error_message VARCHAR(255) NOT NULL DEFAULT '',
			created_by_admin_id BIGINT NOT NULL,
			created_by_username VARCHAR(64) NOT NULL,
			created_by_role VARCHAR(32) NOT NULL,
			started_at TIMESTAMP NULL DEFAULT NULL,
			completed_at TIMESTAMP NULL DEFAULT NULL,
			created_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
			updated_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP ON UPDATE CURRENT_TIMESTAMP,
			KEY idx_admin_broadcasts_status_created_at (status, created_at)
		) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci;`,
		`CREATE TABLE IF NOT EXISTS admin_broadcast_deliveries (
			id BIGINT NOT NULL AUTO_INCREMENT PRIMARY KEY,
			broadcast_id BIGINT NOT NULL,
			target_user_id BIGINT NOT NULL,
			target_display_name VARCHAR(128) NOT NULL DEFAULT '',
			status VARCHAR(16) NOT NULL,
			error_message VARCHAR(255) NOT NULL DEFAULT '',
			sent_at TIMESTAMP NULL DEFAULT NULL,
			created_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
			updated_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP ON UPDATE CURRENT_TIMESTAMP,
			UNIQUE KEY uk_admin_broadcast_delivery_target (broadcast_id, target_user_id),
			KEY idx_admin_broadcast_deliveries_broadcast (broadcast_id),
			KEY idx_admin_broadcast_deliveries_status (status)
		) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci;`,
	}

	for _, stmt := range statements {
		if _, err := s.db.ExecContext(ctx, stmt); err != nil {
			return fmt.Errorf("ensure broadcast schema: %w", err)
		}
	}

	return nil
}

func (s *Store) EnsureBroadcastSystemUser(ctx context.Context) error {
	const query = `
		INSERT INTO users (
			id, user_type, access_hash, secret_key_id, first_name, last_name,
			username, phone, country_code, verified, support, scam, fake, premium,
			about, state, is_bot, account_days_ttl, photo_id, restricted,
			restriction_reason, archive_and_mute_new_noncontact_peers, deleted, delete_reason
		) VALUES (?, 4, ?, ?, ?, ?, ?, ?, '', 1, 1, 0, 0, 0, '', 0, 0, 180, 0, 0, '', 0, 0, '')
		ON DUPLICATE KEY UPDATE
			first_name = VALUES(first_name),
			last_name = VALUES(last_name),
			username = VALUES(username),
			phone = VALUES(phone),
			verified = VALUES(verified),
			support = VALUES(support),
			user_type = VALUES(user_type),
			deleted = 0,
			delete_reason = ''`

	_, err := s.db.ExecContext(
		ctx,
		query,
		BroadcastSystemUserID,
		int64(6599886787491911852),
		int64(6895602324158323007),
		BroadcastSystemFirstName,
		BroadcastSystemLastName,
		BroadcastSystemUsername,
		BroadcastSystemPhone,
	)
	if err != nil {
		return fmt.Errorf("ensure broadcast system user: %w", err)
	}

	return nil
}

func (s *Store) ResetRunningBroadcasts(ctx context.Context) error {
	_, err := s.db.ExecContext(ctx, `
		UPDATE admin_broadcasts
		SET status = 'pending', error_message = ''
		WHERE status = 'running'`)
	return err
}

func (s *Store) ResolveSelectedRecipients(ctx context.Context, raw string) ([]BroadcastRecipient, []string, error) {
	tokens := splitIdentifiers(raw)
	if len(tokens) == 0 {
		return nil, nil, nil
	}

	recipients := make([]BroadcastRecipient, 0, len(tokens))
	unresolved := make([]string, 0)
	seen := make(map[int64]struct{}, len(tokens))

	for _, token := range tokens {
		recipient, err := s.lookupRecipientByIdentifier(ctx, token)
		if err != nil {
			if errors.Is(err, sql.ErrNoRows) {
				unresolved = append(unresolved, token)
				continue
			}
			return nil, nil, err
		}
		if _, ok := seen[recipient.UserID]; ok {
			continue
		}
		seen[recipient.UserID] = struct{}{}
		recipients = append(recipients, recipient)
	}

	return recipients, unresolved, nil
}

func (s *Store) PreviewBroadcastTargets(ctx context.Context, spec BroadcastTargetSpec) (BroadcastPreview, error) {
	count, err := s.CountRecipients(ctx, spec)
	if err != nil {
		return BroadcastPreview{}, err
	}

	sample, err := s.ListRecipients(ctx, spec, 0, 10)
	if err != nil {
		return BroadcastPreview{}, err
	}

	sampleUsers := make([]UserSummary, 0, len(sample))
	for _, recipient := range sample {
		sampleUsers = append(sampleUsers, UserSummary{
			ID:          recipient.UserID,
			DisplayName: recipient.DisplayName,
			Username:    recipient.Username,
			Phone:       recipient.Phone,
			CountryCode: recipient.CountryCode,
			Restricted:  recipient.Restricted,
			Deleted:     recipient.Deleted,
			IsBot:       recipient.IsBot,
		})
	}

	return BroadcastPreview{
		Count:       count,
		SampleUsers: sampleUsers,
	}, nil
}

func (s *Store) CountRecipients(ctx context.Context, spec BroadcastTargetSpec) (int, error) {
	query, args, err := buildRecipientQuery(spec, false, 0, 0)
	if err != nil {
		return 0, err
	}

	var count int
	if err := s.db.QueryRowContext(ctx, query, args...).Scan(&count); err != nil {
		return 0, err
	}
	return count, nil
}

func (s *Store) ListRecipients(ctx context.Context, spec BroadcastTargetSpec, offset, limit int) ([]BroadcastRecipient, error) {
	query, args, err := buildRecipientQuery(spec, true, offset, limit)
	if err != nil {
		return nil, err
	}

	rows, err := s.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	recipients := make([]BroadcastRecipient, 0)
	for rows.Next() {
		var recipient BroadcastRecipient
		var firstName, lastName string
		if err := rows.Scan(
			&recipient.UserID,
			&firstName,
			&lastName,
			&recipient.Username,
			&recipient.Phone,
			&recipient.CountryCode,
			&recipient.Restricted,
			&recipient.Deleted,
			&recipient.IsBot,
		); err != nil {
			return nil, err
		}
		recipient.DisplayName = joinName(firstName, lastName)
		recipients = append(recipients, recipient)
	}

	return recipients, rows.Err()
}

func (s *Store) CreateBroadcast(ctx context.Context, admin AdminUser, messageText string, spec BroadcastTargetSpec, totalTargets int) (*BroadcastRecord, error) {
	payload, err := json.Marshal(spec)
	if err != nil {
		return nil, err
	}

	result, err := s.db.ExecContext(ctx, `
		INSERT INTO admin_broadcasts (
			sender_user_id, sender_display_name, message_text, target_type, target_spec_json,
			status, total_targets, created_by_admin_id, created_by_username, created_by_role
		) VALUES (?, ?, ?, ?, ?, 'pending', ?, ?, ?, ?)`,
		BroadcastSystemUserID,
		joinName(BroadcastSystemFirstName, BroadcastSystemLastName),
		messageText,
		spec.Type,
		payload,
		totalTargets,
		admin.ID,
		admin.Username,
		admin.Role,
	)
	if err != nil {
		return nil, err
	}

	id, err := result.LastInsertId()
	if err != nil {
		return nil, err
	}

	return s.GetBroadcast(ctx, id)
}

func (s *Store) ClaimNextBroadcast(ctx context.Context) (*BroadcastRecord, error) {
	row := s.db.QueryRowContext(ctx, `
		SELECT id
		FROM admin_broadcasts
		WHERE status = 'pending'
		ORDER BY created_at ASC, id ASC
		LIMIT 1`)

	var id int64
	if err := row.Scan(&id); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, nil
		}
		return nil, err
	}

	result, err := s.db.ExecContext(ctx, `
		UPDATE admin_broadcasts
		SET status = 'running', error_message = '', started_at = CURRENT_TIMESTAMP
		WHERE id = ? AND status = 'pending'`, id)
	if err != nil {
		return nil, err
	}
	rowsAffected, err := result.RowsAffected()
	if err != nil {
		return nil, err
	}
	if rowsAffected == 0 {
		return nil, nil
	}

	return s.GetBroadcast(ctx, id)
}

func (s *Store) UpdateBroadcastProgress(ctx context.Context, broadcastID int64, successCount, failureCount int) error {
	_, err := s.db.ExecContext(ctx, `
		UPDATE admin_broadcasts
		SET success_count = ?, failure_count = ?
		WHERE id = ?`, successCount, failureCount, broadcastID)
	return err
}

func (s *Store) MarkBroadcastCompleted(ctx context.Context, broadcastID int64, successCount, failureCount int) error {
	_, err := s.db.ExecContext(ctx, `
		UPDATE admin_broadcasts
		SET status = 'completed', success_count = ?, failure_count = ?, completed_at = CURRENT_TIMESTAMP, error_message = ''
		WHERE id = ?`, successCount, failureCount, broadcastID)
	return err
}

func (s *Store) MarkBroadcastFailed(ctx context.Context, broadcastID int64, successCount, failureCount int, errorMessage string) error {
	_, err := s.db.ExecContext(ctx, `
		UPDATE admin_broadcasts
		SET status = 'failed', success_count = ?, failure_count = ?, completed_at = CURRENT_TIMESTAMP, error_message = ?
		WHERE id = ?`, successCount, failureCount, strings.TrimSpace(errorMessage), broadcastID)
	return err
}

func (s *Store) UpsertBroadcastDelivery(ctx context.Context, broadcastID int64, recipient BroadcastRecipient, status string, errorMessage string, sent bool) error {
	var sentAt any
	if sent {
		sentAt = time.Now()
	} else {
		sentAt = nil
	}

	_, err := s.db.ExecContext(ctx, `
		INSERT INTO admin_broadcast_deliveries (
			broadcast_id, target_user_id, target_display_name, status, error_message, sent_at
		) VALUES (?, ?, ?, ?, ?, ?)
		ON DUPLICATE KEY UPDATE
			target_display_name = VALUES(target_display_name),
			status = VALUES(status),
			error_message = VALUES(error_message),
			sent_at = VALUES(sent_at)`,
		broadcastID,
		recipient.UserID,
		recipient.DisplayName,
		status,
		strings.TrimSpace(errorMessage),
		sentAt,
	)
	return err
}

func (s *Store) ListBroadcasts(ctx context.Context, limit int) ([]BroadcastRecord, error) {
	if limit < 1 {
		limit = 20
	}
	if limit > 100 {
		limit = 100
	}

	rows, err := s.db.QueryContext(ctx, `
		SELECT id, sender_user_id, sender_display_name, message_text, target_type, target_spec_json,
		       status, total_targets, success_count, failure_count, error_message,
		       created_by_admin_id, created_by_username, created_by_role,
		       started_at, completed_at, created_at, updated_at
		FROM admin_broadcasts
		ORDER BY created_at DESC, id DESC
		LIMIT ?`, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	items := make([]BroadcastRecord, 0, limit)
	for rows.Next() {
		record, err := scanBroadcastRecord(rows)
		if err != nil {
			return nil, err
		}
		items = append(items, record)
	}

	return items, rows.Err()
}

func (s *Store) GetBroadcast(ctx context.Context, id int64) (*BroadcastRecord, error) {
	row := s.db.QueryRowContext(ctx, `
		SELECT id, sender_user_id, sender_display_name, message_text, target_type, target_spec_json,
		       status, total_targets, success_count, failure_count, error_message,
		       created_by_admin_id, created_by_username, created_by_role,
		       started_at, completed_at, created_at, updated_at
		FROM admin_broadcasts
		WHERE id = ?`, id)

	record, err := scanBroadcastRecord(row)
	if err != nil {
		return nil, err
	}
	return &record, nil
}

func (s *Store) ListBroadcastDeliveries(ctx context.Context, broadcastID int64, limit int) ([]BroadcastDelivery, error) {
	if limit < 1 {
		limit = 100
	}
	if limit > 500 {
		limit = 500
	}

	rows, err := s.db.QueryContext(ctx, `
		SELECT id, broadcast_id, target_user_id, target_display_name, status, error_message, sent_at, created_at, updated_at
		FROM admin_broadcast_deliveries
		WHERE broadcast_id = ?
		ORDER BY id ASC
		LIMIT ?`, broadcastID, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	items := make([]BroadcastDelivery, 0, limit)
	for rows.Next() {
		var (
			item   BroadcastDelivery
			sentAt sql.NullTime
		)
		if err := rows.Scan(
			&item.ID,
			&item.BroadcastID,
			&item.TargetUserID,
			&item.TargetDisplayName,
			&item.Status,
			&item.ErrorMessage,
			&sentAt,
			&item.CreatedAt,
			&item.UpdatedAt,
		); err != nil {
			return nil, err
		}
		if sentAt.Valid {
			item.SentAt = &sentAt.Time
		}
		items = append(items, item)
	}
	return items, rows.Err()
}

func (s *Store) lookupRecipientByIdentifier(ctx context.Context, token string) (BroadcastRecipient, error) {
	token = strings.TrimSpace(token)
	if token == "" {
		return BroadcastRecipient{}, sql.ErrNoRows
	}

	var (
		query string
		arg   any
	)
	if userID, err := strconv.ParseInt(token, 10, 64); err == nil {
		query = `
			SELECT id, first_name, last_name, username, phone, country_code, restricted, deleted, is_bot
			FROM users WHERE id = ? LIMIT 1`
		arg = userID
	} else {
		query = `
			SELECT id, first_name, last_name, username, phone, country_code, restricted, deleted, is_bot
			FROM users WHERE username = ? OR phone = ? LIMIT 1`
		arg = token
	}

	var (
		recipient BroadcastRecipient
		firstName string
		lastName  string
	)
	if strings.Contains(query, "OR phone") {
		if err := s.db.QueryRowContext(ctx, query, arg, arg).Scan(
			&recipient.UserID,
			&firstName,
			&lastName,
			&recipient.Username,
			&recipient.Phone,
			&recipient.CountryCode,
			&recipient.Restricted,
			&recipient.Deleted,
			&recipient.IsBot,
		); err != nil {
			return BroadcastRecipient{}, err
		}
	} else {
		if err := s.db.QueryRowContext(ctx, query, arg).Scan(
			&recipient.UserID,
			&firstName,
			&lastName,
			&recipient.Username,
			&recipient.Phone,
			&recipient.CountryCode,
			&recipient.Restricted,
			&recipient.Deleted,
			&recipient.IsBot,
		); err != nil {
			return BroadcastRecipient{}, err
		}
	}

	recipient.DisplayName = joinName(firstName, lastName)
	return recipient, nil
}

func buildRecipientQuery(spec BroadcastTargetSpec, includeRows bool, offset, limit int) (string, []any, error) {
	where := make([]string, 0, 6)
	args := make([]any, 0, 8)

	switch spec.Type {
	case BroadcastTargetTypeAll:
		where = append(where, "deleted = 0")
	case BroadcastTargetTypeSelected:
		if len(spec.ResolvedUserIDs) == 0 {
			return "", nil, fmt.Errorf("no selected recipients")
		}
		where = append(where, "id IN ("+placeholders(len(spec.ResolvedUserIDs))+")")
		for _, id := range spec.ResolvedUserIDs {
			args = append(args, id)
		}
	case BroadcastTargetTypeFiltered:
		if spec.Filters.Deleted == nil {
			where = append(where, "deleted = 0")
		}
	default:
		return "", nil, fmt.Errorf("unsupported target type: %s", spec.Type)
	}

	if spec.Type == BroadcastTargetTypeFiltered {
		if spec.Filters.Restricted != nil {
			where = append(where, "restricted = ?")
			args = append(args, *spec.Filters.Restricted)
		}
		if spec.Filters.Deleted != nil {
			where = append(where, "deleted = ?")
			args = append(args, *spec.Filters.Deleted)
		}
		if spec.Filters.IsBot != nil {
			where = append(where, "is_bot = ?")
			args = append(args, *spec.Filters.IsBot)
		}
		if code := strings.TrimSpace(spec.Filters.CountryCode); code != "" {
			where = append(where, "country_code = ?")
			args = append(args, code)
		}
	}

	whereClause := ""
	if len(where) > 0 {
		whereClause = " WHERE " + strings.Join(where, " AND ")
	}

	if !includeRows {
		return "SELECT COUNT(*) FROM users" + whereClause, args, nil
	}

	if limit <= 0 {
		limit = 50
	}
	query := `
		SELECT id, first_name, last_name, username, phone, country_code, restricted, deleted, is_bot
		FROM users` + whereClause + `
		ORDER BY id ASC
		LIMIT ? OFFSET ?`
	args = append(args, limit, offset)
	return query, args, nil
}

func scanBroadcastRecord(scanner interface {
	Scan(dest ...any) error
}) (BroadcastRecord, error) {
	var (
		record      BroadcastRecord
		startedAt   sql.NullTime
		completedAt sql.NullTime
	)
	err := scanner.Scan(
		&record.ID,
		&record.SenderUserID,
		&record.SenderDisplayName,
		&record.MessageText,
		&record.TargetType,
		&record.TargetSpec,
		&record.Status,
		&record.TotalTargets,
		&record.SuccessCount,
		&record.FailureCount,
		&record.ErrorMessage,
		&record.CreatedByAdminID,
		&record.CreatedByUsername,
		&record.CreatedByRole,
		&startedAt,
		&completedAt,
		&record.CreatedAt,
		&record.UpdatedAt,
	)
	if err != nil {
		return BroadcastRecord{}, err
	}
	if startedAt.Valid {
		record.StartedAt = &startedAt.Time
	}
	if completedAt.Valid {
		record.CompletedAt = &completedAt.Time
	}
	return record, nil
}

func splitIdentifiers(raw string) []string {
	values := strings.FieldsFunc(raw, func(r rune) bool {
		switch r {
		case ',', '\n', '\r', '\t', ';', ' ':
			return true
		default:
			return false
		}
	})

	out := make([]string, 0, len(values))
	seen := make(map[string]struct{}, len(values))
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value == "" {
			continue
		}
		if _, ok := seen[value]; ok {
			continue
		}
		seen[value] = struct{}{}
		out = append(out, value)
	}
	return out
}

func placeholders(count int) string {
	if count <= 0 {
		return ""
	}
	return strings.TrimRight(strings.Repeat("?,", count), ",")
}
