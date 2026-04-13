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

	_ "github.com/go-sql-driver/mysql"
	"golang.org/x/crypto/bcrypt"
)

var ErrInvalidCredentials = errors.New("invalid credentials")

type Store struct {
	db                   *sql.DB
	sessionActiveColumn  string
	hasLegacySessionMeta bool
}

type AdminUser struct {
	ID       int64  `json:"id"`
	Username string `json:"username"`
	Role     string `json:"role"`
}

type UserListFilter struct {
	Query      string
	Page       int
	PageSize   int
	Restricted *bool
	Deleted    *bool
}

type UserSummary struct {
	ID                int64     `json:"id"`
	FirstName         string    `json:"firstName"`
	LastName          string    `json:"lastName"`
	DisplayName       string    `json:"displayName"`
	Username          string    `json:"username"`
	Phone             string    `json:"phone"`
	CountryCode       string    `json:"countryCode"`
	Verified          bool      `json:"verified"`
	Support           bool      `json:"support"`
	Scam              bool      `json:"scam"`
	Fake              bool      `json:"fake"`
	IsBot             bool      `json:"isBot"`
	State             int32     `json:"state"`
	Restricted        bool      `json:"restricted"`
	RestrictionReason string    `json:"restrictionReason"`
	Deleted           bool      `json:"deleted"`
	DeleteReason      string    `json:"deleteReason"`
	CreatedAt         time.Time `json:"createdAt"`
	UpdatedAt         time.Time `json:"updatedAt"`
}

type UserSession struct {
	ID            int64  `json:"id"`
	AuthKeyID     int64  `json:"authKeyId"`
	Hash          int64  `json:"hash"`
	DeviceModel   string `json:"deviceModel"`
	Platform      string `json:"platform"`
	SystemVersion string `json:"systemVersion"`
	APIID         int32  `json:"apiId"`
	AppName       string `json:"appName"`
	AppVersion    string `json:"appVersion"`
	IP            string `json:"ip"`
	Country       string `json:"country"`
	Region        string `json:"region"`
	DateCreated   int64  `json:"dateCreated"`
	DateActive    int64  `json:"dateActive"`
	Deleted       bool   `json:"deleted"`
}

type AuditLog struct {
	ID            int64           `json:"id"`
	ActorAdminID  int64           `json:"actorAdminId"`
	ActorUsername string          `json:"actorUsername"`
	ActorRole     string          `json:"actorRole"`
	Action        string          `json:"action"`
	TargetUserID  int64           `json:"targetUserId"`
	Metadata      json.RawMessage `json:"metadata"`
	CreatedAt     time.Time       `json:"createdAt"`
}

type UserDetail struct {
	UserSummary
	About          string        `json:"about"`
	AccountDaysTTL int32         `json:"accountDaysTtl"`
	PhotoID        int64         `json:"photoId"`
	Sessions       []UserSession `json:"sessions"`
	AuditLogs      []AuditLog    `json:"auditLogs"`
}

type UserListResult struct {
	Items    []UserSummary `json:"items"`
	Total    int           `json:"total"`
	Page     int           `json:"page"`
	PageSize int           `json:"pageSize"`
}

func Open(dsn string) (*Store, error) {
	db, err := sql.Open("mysql", dsn)
	if err != nil {
		return nil, fmt.Errorf("open mysql: %w", err)
	}
	db.SetConnMaxLifetime(10 * time.Minute)
	db.SetMaxIdleConns(5)
	db.SetMaxOpenConns(10)

	if err := db.Ping(); err != nil {
		return nil, fmt.Errorf("ping mysql: %w", err)
	}

	store := &Store{
		db:                   db,
		sessionActiveColumn:  "date_active",
		hasLegacySessionMeta: false,
	}

	if err := store.detectAuthUserColumns(context.Background()); err != nil {
		return nil, err
	}

	return store, nil
}

func (s *Store) Close() error {
	return s.db.Close()
}

func (s *Store) EnsureSchema(ctx context.Context) error {
	statements := []string{
		`CREATE TABLE IF NOT EXISTS admin_users (
			id BIGINT NOT NULL AUTO_INCREMENT PRIMARY KEY,
			username VARCHAR(64) NOT NULL,
			password_hash VARCHAR(255) NOT NULL,
			role VARCHAR(32) NOT NULL DEFAULT 'super_admin',
			is_active TINYINT(1) NOT NULL DEFAULT 1,
			created_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
			updated_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP ON UPDATE CURRENT_TIMESTAMP,
			UNIQUE KEY uk_admin_users_username (username)
		) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci;`,
		`CREATE TABLE IF NOT EXISTS admin_audit_logs (
			id BIGINT NOT NULL AUTO_INCREMENT PRIMARY KEY,
			actor_admin_id BIGINT NOT NULL,
			actor_username VARCHAR(64) NOT NULL,
			actor_role VARCHAR(32) NOT NULL,
			action VARCHAR(64) NOT NULL,
			target_user_id BIGINT NOT NULL DEFAULT 0,
			metadata_json JSON NULL,
			created_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
			KEY idx_admin_audit_logs_target_user (target_user_id),
			KEY idx_admin_audit_logs_created_at (created_at)
		) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci;`,
	}

	for _, stmt := range statements {
		if _, err := s.db.ExecContext(ctx, stmt); err != nil {
			return fmt.Errorf("ensure schema: %w", err)
		}
	}

	return nil
}

func (s *Store) EnsureBootstrapAdmin(ctx context.Context, username, password string) error {
	username = strings.TrimSpace(username)
	if username == "" || password == "" {
		return nil
	}

	hash, err := bcrypt.GenerateFromPassword([]byte(password), bcrypt.DefaultCost)
	if err != nil {
		return fmt.Errorf("hash bootstrap password: %w", err)
	}

	const query = `
		INSERT INTO admin_users (username, password_hash, role, is_active)
		VALUES (?, ?, 'super_admin', 1)
		ON DUPLICATE KEY UPDATE
			password_hash = VALUES(password_hash),
			role = VALUES(role),
			is_active = VALUES(is_active)`

	if _, err := s.db.ExecContext(ctx, query, username, string(hash)); err != nil {
		return fmt.Errorf("upsert bootstrap admin: %w", err)
	}

	return nil
}

func (s *Store) AuthenticateAdmin(ctx context.Context, username, password string) (*AdminUser, error) {
	const query = `
		SELECT id, username, password_hash, role, is_active
		FROM admin_users
		WHERE username = ?`

	var (
		admin        AdminUser
		passwordHash string
		isActive     bool
	)
	if err := s.db.QueryRowContext(ctx, query, username).Scan(
		&admin.ID,
		&admin.Username,
		&passwordHash,
		&admin.Role,
		&isActive,
	); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, ErrInvalidCredentials
		}
		return nil, err
	}

	if !isActive {
		return nil, ErrInvalidCredentials
	}
	if err := bcrypt.CompareHashAndPassword([]byte(passwordHash), []byte(password)); err != nil {
		return nil, ErrInvalidCredentials
	}

	return &admin, nil
}

func (s *Store) ListUsers(ctx context.Context, filter UserListFilter) (UserListResult, error) {
	page := filter.Page
	if page < 1 {
		page = 1
	}
	pageSize := filter.PageSize
	if pageSize < 1 {
		pageSize = 20
	}
	if pageSize > 100 {
		pageSize = 100
	}

	where, args := buildUserFilters(filter)
	whereClause := ""
	if len(where) > 0 {
		whereClause = " WHERE " + strings.Join(where, " AND ")
	}

	countQuery := `SELECT COUNT(*) FROM users` + whereClause
	var total int
	if err := s.db.QueryRowContext(ctx, countQuery, args...).Scan(&total); err != nil {
		return UserListResult{}, err
	}

	listArgs := append([]any{}, args...)
	listArgs = append(listArgs, pageSize, (page-1)*pageSize)
	listQuery := `
		SELECT id, first_name, last_name, username, phone, country_code,
		       verified, support, scam, fake, is_bot, state,
		       restricted, restriction_reason, deleted, delete_reason,
		       created_at, updated_at
		FROM users` + whereClause + `
		ORDER BY updated_at DESC, id DESC
		LIMIT ? OFFSET ?`

	rows, err := s.db.QueryContext(ctx, listQuery, listArgs...)
	if err != nil {
		return UserListResult{}, err
	}
	defer rows.Close()

	items := make([]UserSummary, 0, pageSize)
	for rows.Next() {
		var user UserSummary
		if err := rows.Scan(
			&user.ID,
			&user.FirstName,
			&user.LastName,
			&user.Username,
			&user.Phone,
			&user.CountryCode,
			&user.Verified,
			&user.Support,
			&user.Scam,
			&user.Fake,
			&user.IsBot,
			&user.State,
			&user.Restricted,
			&user.RestrictionReason,
			&user.Deleted,
			&user.DeleteReason,
			&user.CreatedAt,
			&user.UpdatedAt,
		); err != nil {
			return UserListResult{}, err
		}
		user.DisplayName = joinName(user.FirstName, user.LastName)
		items = append(items, user)
	}
	if err := rows.Err(); err != nil {
		return UserListResult{}, err
	}

	return UserListResult{
		Items:    items,
		Total:    total,
		Page:     page,
		PageSize: pageSize,
	}, nil
}

func (s *Store) GetUserDetail(ctx context.Context, userID int64) (*UserDetail, error) {
	const userQuery = `
		SELECT id, first_name, last_name, username, phone, country_code,
		       verified, support, scam, fake, is_bot, state,
		       restricted, restriction_reason, deleted, delete_reason,
		       created_at, updated_at, about, account_days_ttl, photo_id
		FROM users
		WHERE id = ?`

	var detail UserDetail
	if err := s.db.QueryRowContext(ctx, userQuery, userID).Scan(
		&detail.ID,
		&detail.FirstName,
		&detail.LastName,
		&detail.Username,
		&detail.Phone,
		&detail.CountryCode,
		&detail.Verified,
		&detail.Support,
		&detail.Scam,
		&detail.Fake,
		&detail.IsBot,
		&detail.State,
		&detail.Restricted,
		&detail.RestrictionReason,
		&detail.Deleted,
		&detail.DeleteReason,
		&detail.CreatedAt,
		&detail.UpdatedAt,
		&detail.About,
		&detail.AccountDaysTTL,
		&detail.PhotoID,
	); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, sql.ErrNoRows
		}
		return nil, err
	}
	detail.DisplayName = joinName(detail.FirstName, detail.LastName)

	sessions, err := s.listUserSessions(ctx, userID)
	if err != nil {
		return nil, err
	}
	detail.Sessions = sessions

	auditLogs, err := s.ListAuditLogsByTargetUser(ctx, userID, 20)
	if err != nil {
		return nil, err
	}
	detail.AuditLogs = auditLogs

	return &detail, nil
}

func (s *Store) SetUserRestricted(ctx context.Context, userID int64, restricted bool, reason string) error {
	const query = `
		UPDATE users
		SET restricted = ?, restriction_reason = ?
		WHERE id = ?`

	result, err := s.db.ExecContext(ctx, query, restricted, strings.TrimSpace(reason), userID)
	if err != nil {
		return err
	}
	rowsAffected, err := result.RowsAffected()
	if err != nil {
		return err
	}
	if rowsAffected == 0 {
		return sql.ErrNoRows
	}

	return nil
}

func (s *Store) KickUserSessions(ctx context.Context, userID int64) (int64, error) {
	query := fmt.Sprintf(`
		UPDATE auth_users
		SET deleted = 1, %s = 0
		WHERE user_id = ? AND deleted = 0`, s.sessionActiveColumn)

	result, err := s.db.ExecContext(ctx, query, userID)
	if err != nil {
		return 0, err
	}

	rowsAffected, err := result.RowsAffected()
	if err != nil {
		return 0, err
	}

	return rowsAffected, nil
}

func (s *Store) CreateAuditLog(ctx context.Context, actor AdminUser, action string, targetUserID int64, metadata map[string]any) error {
	var payload []byte
	if metadata != nil {
		data, err := json.Marshal(metadata)
		if err != nil {
			return err
		}
		payload = data
	}

	const query = `
		INSERT INTO admin_audit_logs (actor_admin_id, actor_username, actor_role, action, target_user_id, metadata_json)
		VALUES (?, ?, ?, ?, ?, ?)`

	_, err := s.db.ExecContext(ctx, query, actor.ID, actor.Username, actor.Role, action, targetUserID, payload)
	return err
}

func (s *Store) ListAuditLogsByTargetUser(ctx context.Context, targetUserID int64, limit int) ([]AuditLog, error) {
	if limit < 1 {
		limit = 20
	}
	if limit > 100 {
		limit = 100
	}

	const query = `
		SELECT id, actor_admin_id, actor_username, actor_role, action, target_user_id, metadata_json, created_at
		FROM admin_audit_logs
		WHERE target_user_id = ?
		ORDER BY created_at DESC, id DESC
		LIMIT ?`

	rows, err := s.db.QueryContext(ctx, query, targetUserID, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	logs := make([]AuditLog, 0, limit)
	for rows.Next() {
		var (
			entry    AuditLog
			metadata sql.NullString
		)
		if err := rows.Scan(
			&entry.ID,
			&entry.ActorAdminID,
			&entry.ActorUsername,
			&entry.ActorRole,
			&entry.Action,
			&entry.TargetUserID,
			&metadata,
			&entry.CreatedAt,
		); err != nil {
			return nil, err
		}
		if metadata.Valid {
			entry.Metadata = json.RawMessage(metadata.String)
		}
		logs = append(logs, entry)
	}

	return logs, rows.Err()
}

func (s *Store) detectAuthUserColumns(ctx context.Context) error {
	const activeColumnQuery = `
		SELECT COUNT(*)
		FROM information_schema.columns
		WHERE table_schema = DATABASE()
		  AND table_name = 'auth_users'
		  AND column_name = 'date_active'`

	var count int
	if err := s.db.QueryRowContext(ctx, activeColumnQuery).Scan(&count); err != nil {
		return fmt.Errorf("detect auth_users active column: %w", err)
	}
	if count == 0 {
		s.sessionActiveColumn = "date_actived"
	}

	const legacyMetaQuery = `
		SELECT COUNT(*)
		FROM information_schema.columns
		WHERE table_schema = DATABASE()
		  AND table_name = 'auth_users'
		  AND column_name = 'device_model'`

	if err := s.db.QueryRowContext(ctx, legacyMetaQuery).Scan(&count); err != nil {
		return fmt.Errorf("detect auth_users metadata columns: %w", err)
	}
	s.hasLegacySessionMeta = count > 0
	return nil
}

func (s *Store) listUserSessions(ctx context.Context, userID int64) ([]UserSession, error) {
	var query string
	if s.hasLegacySessionMeta {
		query = fmt.Sprintf(`
			SELECT id, auth_key_id, hash, device_model, platform, system_version, api_id,
			       app_name, app_version, ip, country, region, date_created, %s AS date_active, deleted
			FROM auth_users
			WHERE user_id = ?
			ORDER BY %s DESC, date_created DESC`, s.sessionActiveColumn, s.sessionActiveColumn)
	} else {
		query = fmt.Sprintf(`
			SELECT id, auth_key_id, hash, '' AS device_model, '' AS platform, '' AS system_version, 0 AS api_id,
			       '' AS app_name, '' AS app_version, '' AS ip, '' AS country, '' AS region, date_created, %s AS date_active, deleted
			FROM auth_users
			WHERE user_id = ?
			ORDER BY %s DESC, date_created DESC`, s.sessionActiveColumn, s.sessionActiveColumn)
	}

	rows, err := s.db.QueryContext(ctx, query, userID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	sessions := make([]UserSession, 0)
	for rows.Next() {
		var session UserSession
		if err := rows.Scan(
			&session.ID,
			&session.AuthKeyID,
			&session.Hash,
			&session.DeviceModel,
			&session.Platform,
			&session.SystemVersion,
			&session.APIID,
			&session.AppName,
			&session.AppVersion,
			&session.IP,
			&session.Country,
			&session.Region,
			&session.DateCreated,
			&session.DateActive,
			&session.Deleted,
		); err != nil {
			return nil, err
		}
		sessions = append(sessions, session)
	}

	return sessions, rows.Err()
}

func buildUserFilters(filter UserListFilter) ([]string, []any) {
	where := make([]string, 0, 3)
	args := make([]any, 0, 6)

	if q := strings.TrimSpace(filter.Query); q != "" {
		parts := make([]string, 0, 4)
		if userID, err := strconv.ParseInt(q, 10, 64); err == nil {
			parts = append(parts, "id = ?")
			args = append(args, userID)
		}

		like := "%" + q + "%"
		parts = append(parts,
			"username LIKE ?",
			"phone LIKE ?",
			"CONCAT(first_name, ' ', last_name) LIKE ?",
		)
		args = append(args, like, like, like)
		where = append(where, "("+strings.Join(parts, " OR ")+")")
	}

	if filter.Restricted != nil {
		where = append(where, "restricted = ?")
		args = append(args, *filter.Restricted)
	}
	if filter.Deleted != nil {
		where = append(where, "deleted = ?")
		args = append(args, *filter.Deleted)
	}

	return where, args
}

func joinName(firstName, lastName string) string {
	fullName := strings.TrimSpace(strings.TrimSpace(firstName) + " " + strings.TrimSpace(lastName))
	if fullName != "" {
		return fullName
	}
	return "-"
}
