package store

import (
	"context"
	"fmt"
	"strings"
	"time"
)

const (
	SupportSystemUserID    int64 = 779000
	SupportSystemUsername        = "hsgram_support"
	SupportSystemPhone           = "42779"
	SupportSystemFirstName       = "HSgram"
	SupportSystemLastName        = "Support"

	SupportDirectionIn  = "in"
	SupportDirectionOut = "out"
)

type SupportThreadSummary struct {
	UserID          int64     `json:"userId"`
	DisplayName     string    `json:"displayName"`
	Username        string    `json:"username"`
	Phone           string    `json:"phone"`
	LastMessageText string    `json:"lastMessageText"`
	LastDirection   string    `json:"lastDirection"`
	LastMessageAt   time.Time `json:"lastMessageAt"`
	UnreadCount     int       `json:"unreadCount"`
}

type SupportMessageRecord struct {
	ID              int64     `json:"id"`
	UserID          int64     `json:"userId"`
	DialogMessageID int64     `json:"dialogMessageId"`
	SenderUserID    int64     `json:"senderUserId"`
	ReceiverUserID  int64     `json:"receiverUserId"`
	Direction       string    `json:"direction"`
	MessageText     string    `json:"messageText"`
	MessageDate     int64     `json:"messageDate"`
	CreatedAt       time.Time `json:"createdAt"`
}

type SupportThreadDetail struct {
	Thread   SupportThreadSummary   `json:"thread"`
	Messages []SupportMessageRecord `json:"messages"`
}

func (s *Store) EnsureSupportSchema(ctx context.Context) error {
	statements := []string{
		`CREATE TABLE IF NOT EXISTS admin_support_threads (
			user_id BIGINT NOT NULL PRIMARY KEY,
			last_message_text TEXT NOT NULL,
			last_message_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
			last_sender_user_id BIGINT NOT NULL DEFAULT 0,
			last_direction VARCHAR(8) NOT NULL DEFAULT 'in',
			unread_count INT NOT NULL DEFAULT 0,
			created_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
			updated_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP ON UPDATE CURRENT_TIMESTAMP,
			KEY idx_admin_support_threads_last_message_at (last_message_at),
			KEY idx_admin_support_threads_unread_count (unread_count)
		) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci;`,
		`CREATE TABLE IF NOT EXISTS admin_support_messages (
			id BIGINT NOT NULL AUTO_INCREMENT PRIMARY KEY,
			user_id BIGINT NOT NULL,
			dialog_message_id BIGINT NOT NULL,
			sender_user_id BIGINT NOT NULL,
			receiver_user_id BIGINT NOT NULL,
			direction VARCHAR(8) NOT NULL,
			message_text TEXT NOT NULL,
			message_date BIGINT NOT NULL DEFAULT 0,
			created_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
			updated_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP ON UPDATE CURRENT_TIMESTAMP,
			UNIQUE KEY uk_admin_support_messages_dialog (dialog_message_id),
			KEY idx_admin_support_messages_user_id_id (user_id, id),
			KEY idx_admin_support_messages_user_id_date (user_id, message_date)
		) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci;`,
	}

	for _, stmt := range statements {
		if _, err := s.db.ExecContext(ctx, stmt); err != nil {
			return fmt.Errorf("ensure support schema: %w", err)
		}
	}

	return nil
}

func (s *Store) EnsureSupportSystemUser(ctx context.Context) error {
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
		SupportSystemUserID,
		int64(6599886787491911853),
		int64(6895602324158323008),
		SupportSystemFirstName,
		SupportSystemLastName,
		SupportSystemUsername,
		SupportSystemPhone,
	)
	if err != nil {
		return fmt.Errorf("ensure support system user: %w", err)
	}

	return nil
}

func (s *Store) ListSupportThreads(ctx context.Context) ([]SupportThreadSummary, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT
			t.user_id,
			COALESCE(u.first_name, ''),
			COALESCE(u.last_name, ''),
			COALESCE(u.username, ''),
			COALESCE(u.phone, ''),
			t.last_message_text,
			t.last_direction,
			t.last_message_at,
			t.unread_count
		FROM admin_support_threads t
		LEFT JOIN users u ON u.id = t.user_id
		ORDER BY t.last_message_at DESC, t.user_id DESC`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	threads := make([]SupportThreadSummary, 0)
	for rows.Next() {
		var (
			thread              SupportThreadSummary
			firstName, lastName string
		)
		if err := rows.Scan(
			&thread.UserID,
			&firstName,
			&lastName,
			&thread.Username,
			&thread.Phone,
			&thread.LastMessageText,
			&thread.LastDirection,
			&thread.LastMessageAt,
			&thread.UnreadCount,
		); err != nil {
			return nil, err
		}
		thread.DisplayName = buildSupportDisplayName(firstName, lastName, thread.Username, thread.Phone, thread.UserID)
		threads = append(threads, thread)
	}

	if err := rows.Err(); err != nil {
		return nil, err
	}

	return threads, nil
}

func (s *Store) GetSupportThread(ctx context.Context, userID int64) (*SupportThreadDetail, error) {
	var (
		thread              SupportThreadSummary
		firstName, lastName string
	)

	err := s.db.QueryRowContext(ctx, `
		SELECT
			t.user_id,
			COALESCE(u.first_name, ''),
			COALESCE(u.last_name, ''),
			COALESCE(u.username, ''),
			COALESCE(u.phone, ''),
			t.last_message_text,
			t.last_direction,
			t.last_message_at,
			t.unread_count
		FROM admin_support_threads t
		LEFT JOIN users u ON u.id = t.user_id
		WHERE t.user_id = ?`, userID).Scan(
		&thread.UserID,
		&firstName,
		&lastName,
		&thread.Username,
		&thread.Phone,
		&thread.LastMessageText,
		&thread.LastDirection,
		&thread.LastMessageAt,
		&thread.UnreadCount,
	)
	if err != nil {
		return nil, err
	}
	thread.DisplayName = buildSupportDisplayName(firstName, lastName, thread.Username, thread.Phone, thread.UserID)

	rows, err := s.db.QueryContext(ctx, `
		SELECT id, user_id, dialog_message_id, sender_user_id, receiver_user_id, direction, message_text, message_date, created_at
		FROM admin_support_messages
		WHERE user_id = ?
		ORDER BY id ASC
		LIMIT 300`, userID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	messages := make([]SupportMessageRecord, 0)
	for rows.Next() {
		var message SupportMessageRecord
		if err := rows.Scan(
			&message.ID,
			&message.UserID,
			&message.DialogMessageID,
			&message.SenderUserID,
			&message.ReceiverUserID,
			&message.Direction,
			&message.MessageText,
			&message.MessageDate,
			&message.CreatedAt,
		); err != nil {
			return nil, err
		}
		messages = append(messages, message)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}

	if err := s.MarkSupportThreadRead(ctx, userID); err != nil {
		return nil, err
	}
	thread.UnreadCount = 0

	return &SupportThreadDetail{
		Thread:   thread,
		Messages: messages,
	}, nil
}

func (s *Store) MarkSupportThreadRead(ctx context.Context, userID int64) error {
	_, err := s.db.ExecContext(ctx, `
		UPDATE admin_support_threads
		SET unread_count = 0
		WHERE user_id = ?`, userID)
	return err
}

func buildSupportDisplayName(firstName, lastName, username, phone string, userID int64) string {
	displayName := strings.TrimSpace(strings.TrimSpace(firstName) + " " + strings.TrimSpace(lastName))
	if displayName != "" {
		return displayName
	}
	if username = strings.TrimSpace(username); username != "" {
		return username
	}
	if phone = strings.TrimSpace(phone); phone != "" {
		return phone
	}
	return fmt.Sprintf("用户 %d", userID)
}
