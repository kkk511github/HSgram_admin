package store

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/go-sql-driver/mysql"
)

const (
	ReleasePlatformAndroid = "android"
	ReleasePlatformWindows = "windows"
	ReleasePlatformPC      = ReleasePlatformWindows

	ReleaseChannelStable   = "stable"
	ReleaseChannelBeta     = "beta"
	ReleaseChannelInternal = "internal"

	ReleaseArchUniversal     = "universal"
	ReleaseArchAndroidArm64  = "arm64-v8a"
	ReleaseArchAndroidArmV7  = "armeabi-v7a"
	ReleaseArchAndroidX8664  = "x86_64"
	ReleaseArchWindowsX64    = "win-x64"
	ReleaseArchWindowsArm64  = "win-arm64"
	ReleaseUpdateOptional    = "optional"
	ReleaseUpdateRecommended = "recommended"
	ReleaseUpdateRequired    = "required"

	ReleaseStatusDraft     = "draft"
	ReleaseStatusPublished = "published"
)

type AppRelease struct {
	ID                      int64      `json:"id"`
	Platform                string     `json:"platform"`
	Channel                 string     `json:"channel"`
	Arch                    string     `json:"arch"`
	Version                 string     `json:"version"`
	VersionCode             int        `json:"versionCode"`
	MinSupportedVersionCode int        `json:"minSupportedVersionCode"`
	UpdateLevel             string     `json:"updateLevel"`
	Title                   string     `json:"title"`
	Filename                string     `json:"filename"`
	StoragePath             string     `json:"storagePath"`
	StorageKey              string     `json:"storageKey"`
	FileSize                int64      `json:"fileSize"`
	SHA256                  string     `json:"sha256"`
	MimeType                string     `json:"mimeType"`
	DownloadURL             string     `json:"downloadUrl"`
	PublicDownloadURL       string     `json:"publicDownloadUrl"`
	Changelog               string     `json:"changelog"`
	Status                  string     `json:"status"`
	IsLatest                bool       `json:"isLatest"`
	CreatedByAdminID        int64      `json:"createdByAdminId"`
	CreatedByUsername       string     `json:"createdByUsername"`
	CreatedByRole           string     `json:"createdByRole"`
	PublishedAt             *time.Time `json:"publishedAt,omitempty"`
	CreatedAt               time.Time  `json:"createdAt"`
	UpdatedAt               time.Time  `json:"updatedAt"`
}

type CreateAppReleaseParams struct {
	Platform                string
	Channel                 string
	Arch                    string
	Version                 string
	VersionCode             int
	MinSupportedVersionCode int
	UpdateLevel             string
	Title                   string
	Filename                string
	StoragePath             string
	StorageKey              string
	FileSize                int64
	SHA256                  string
	MimeType                string
	DownloadURL             string
	PublicDownloadURL       string
	Changelog               string
}

func NormalizeReleasePlatform(platform string) (string, error) {
	switch strings.ToLower(strings.TrimSpace(platform)) {
	case ReleasePlatformAndroid:
		return ReleasePlatformAndroid, nil
	case ReleasePlatformWindows, "pc", "desktop", "win":
		return ReleasePlatformWindows, nil
	default:
		return "", fmt.Errorf("不支持的平台")
	}
}

func NormalizeReleaseChannel(channel string) (string, error) {
	switch strings.ToLower(strings.TrimSpace(channel)) {
	case "", ReleaseChannelStable:
		return ReleaseChannelStable, nil
	case ReleaseChannelBeta:
		return ReleaseChannelBeta, nil
	case ReleaseChannelInternal:
		return ReleaseChannelInternal, nil
	default:
		return "", fmt.Errorf("不支持的发布渠道")
	}
}

func NormalizeReleaseUpdateLevel(level string) (string, error) {
	switch strings.ToLower(strings.TrimSpace(level)) {
	case "", ReleaseUpdateOptional:
		return ReleaseUpdateOptional, nil
	case ReleaseUpdateRecommended, "recommend":
		return ReleaseUpdateRecommended, nil
	case ReleaseUpdateRequired, "force", "forced":
		return ReleaseUpdateRequired, nil
	default:
		return "", fmt.Errorf("不支持的更新级别")
	}
}

func NormalizeReleaseArch(platform, arch string) (string, error) {
	platform, err := NormalizeReleasePlatform(platform)
	if err != nil {
		return "", err
	}
	switch strings.ToLower(strings.TrimSpace(arch)) {
	case "", ReleaseArchUniversal:
		return ReleaseArchUniversal, nil
	}
	switch platform {
	case ReleasePlatformAndroid:
		switch strings.ToLower(strings.TrimSpace(arch)) {
		case ReleaseArchAndroidArm64, "arm64":
			return ReleaseArchAndroidArm64, nil
		case ReleaseArchAndroidArmV7, "armv7", "armeabi":
			return ReleaseArchAndroidArmV7, nil
		case ReleaseArchAndroidX8664, "x64":
			return ReleaseArchAndroidX8664, nil
		}
	case ReleasePlatformWindows:
		switch strings.ToLower(strings.TrimSpace(arch)) {
		case ReleaseArchWindowsX64, "x64", "x86_64", "amd64":
			return ReleaseArchWindowsX64, nil
		case ReleaseArchWindowsArm64, "arm64", "aarch64":
			return ReleaseArchWindowsArm64, nil
		}
	}
	return "", fmt.Errorf("不支持的架构")
}

func (s *Store) EnsureReleaseSchema(ctx context.Context) error {
	statements := []string{
		`CREATE TABLE IF NOT EXISTS admin_app_releases (
			id BIGINT NOT NULL AUTO_INCREMENT PRIMARY KEY,
			platform VARCHAR(16) NOT NULL,
			channel VARCHAR(32) NOT NULL DEFAULT 'stable',
			arch VARCHAR(32) NOT NULL DEFAULT 'universal',
			version VARCHAR(64) NOT NULL,
			version_code INT NOT NULL,
			min_supported_version_code INT NOT NULL DEFAULT 0,
			update_level VARCHAR(16) NOT NULL DEFAULT 'optional',
			title VARCHAR(255) NOT NULL DEFAULT '',
			filename VARCHAR(255) NOT NULL,
			storage_path VARCHAR(255) NOT NULL,
			storage_key VARCHAR(255) NOT NULL DEFAULT '',
			file_size BIGINT NOT NULL,
			sha256 VARCHAR(64) NOT NULL,
			mime_type VARCHAR(128) NOT NULL DEFAULT '',
			download_url VARCHAR(1024) NOT NULL DEFAULT '',
			public_download_url VARCHAR(1024) NOT NULL DEFAULT '',
			changelog TEXT NOT NULL,
			status VARCHAR(16) NOT NULL DEFAULT 'draft',
			created_by_admin_id BIGINT NOT NULL,
			created_by_username VARCHAR(64) NOT NULL,
			created_by_role VARCHAR(32) NOT NULL,
			published_at TIMESTAMP NULL DEFAULT NULL,
			created_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
			updated_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP ON UPDATE CURRENT_TIMESTAMP,
			KEY idx_admin_app_releases_platform_created (platform, channel, arch, created_at),
			KEY idx_admin_app_releases_status (status),
			UNIQUE KEY uk_admin_app_releases_storage_path (storage_path)
		) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci;`,
		`CREATE TABLE IF NOT EXISTS admin_app_latest (
			platform VARCHAR(16) NOT NULL,
			channel VARCHAR(32) NOT NULL DEFAULT 'stable',
			arch VARCHAR(32) NOT NULL DEFAULT 'universal',
			release_id BIGINT NOT NULL,
			updated_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP ON UPDATE CURRENT_TIMESTAMP,
			PRIMARY KEY (platform, channel, arch),
			KEY idx_admin_app_latest_release_id (release_id)
		) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci;`,
	}

	for _, stmt := range statements {
		if _, err := s.db.ExecContext(ctx, stmt); err != nil {
			return fmt.Errorf("ensure release schema: %w", err)
		}
	}
	return s.migrateReleaseSchema(ctx)
}

func (s *Store) CreateAppRelease(ctx context.Context, admin AdminUser, params CreateAppReleaseParams) (*AppRelease, error) {
	platform, err := NormalizeReleasePlatform(params.Platform)
	if err != nil {
		return nil, err
	}
	channel, err := NormalizeReleaseChannel(params.Channel)
	if err != nil {
		return nil, err
	}
	arch, err := NormalizeReleaseArch(platform, params.Arch)
	if err != nil {
		return nil, err
	}
	updateLevel, err := NormalizeReleaseUpdateLevel(params.UpdateLevel)
	if err != nil {
		return nil, err
	}
	if strings.TrimSpace(params.Version) == "" {
		return nil, fmt.Errorf("请填写版本号")
	}
	if params.VersionCode <= 0 {
		return nil, fmt.Errorf("请填写版本编码")
	}
	if params.MinSupportedVersionCode < 0 {
		return nil, fmt.Errorf("最低支持版本编码必须为 0 或正整数")
	}
	if strings.TrimSpace(params.Filename) == "" || strings.TrimSpace(params.StoragePath) == "" {
		return nil, fmt.Errorf("请上传安装包文件")
	}
	if params.FileSize <= 0 {
		return nil, fmt.Errorf("安装包文件为空")
	}
	if strings.TrimSpace(params.SHA256) == "" {
		return nil, fmt.Errorf("缺少 sha256")
	}
	storageKey := strings.TrimSpace(params.StorageKey)
	if storageKey == "" {
		storageKey = strings.TrimSpace(params.StoragePath)
	}

	result, err := s.db.ExecContext(ctx, `
		INSERT INTO admin_app_releases (
			platform, channel, arch, version, version_code, min_supported_version_code,
			update_level, title, filename, storage_path, storage_key, file_size, sha256,
			mime_type, download_url, public_download_url, changelog,
			status, created_by_admin_id, created_by_username, created_by_role
		) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, 'draft', ?, ?, ?)`,
		platform,
		channel,
		arch,
		strings.TrimSpace(params.Version),
		params.VersionCode,
		params.MinSupportedVersionCode,
		updateLevel,
		strings.TrimSpace(params.Title),
		strings.TrimSpace(params.Filename),
		strings.TrimSpace(params.StoragePath),
		storageKey,
		params.FileSize,
		strings.TrimSpace(params.SHA256),
		strings.TrimSpace(params.MimeType),
		strings.TrimSpace(params.DownloadURL),
		strings.TrimSpace(params.PublicDownloadURL),
		strings.TrimSpace(params.Changelog),
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
	return s.GetAppRelease(ctx, id)
}

func (s *Store) ListAppReleases(ctx context.Context, limit int) ([]AppRelease, error) {
	if limit < 1 {
		limit = 50
	}
	if limit > 200 {
		limit = 200
	}

	rows, err := s.db.QueryContext(ctx, `
		SELECT r.id, r.platform, r.channel, r.arch, r.version, r.version_code,
		       r.min_supported_version_code, r.update_level, r.title,
		       r.filename, r.storage_path, r.storage_key, r.file_size, r.sha256,
		       r.mime_type, r.download_url, r.public_download_url,
		       r.changelog, r.status, IF(l.release_id IS NULL, 0, 1) AS is_latest,
		       r.created_by_admin_id, r.created_by_username, r.created_by_role,
		       r.published_at, r.created_at, r.updated_at
		FROM admin_app_releases r
		LEFT JOIN admin_app_latest l ON l.release_id = r.id
		ORDER BY r.created_at DESC, r.id DESC
		LIMIT ?`, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	items := make([]AppRelease, 0, limit)
	for rows.Next() {
		release, err := scanAppRelease(rows)
		if err != nil {
			return nil, err
		}
		items = append(items, release)
	}
	return items, rows.Err()
}

func (s *Store) GetAppRelease(ctx context.Context, id int64) (*AppRelease, error) {
	row := s.db.QueryRowContext(ctx, `
		SELECT r.id, r.platform, r.channel, r.arch, r.version, r.version_code,
		       r.min_supported_version_code, r.update_level, r.title,
		       r.filename, r.storage_path, r.storage_key, r.file_size, r.sha256,
		       r.mime_type, r.download_url, r.public_download_url,
		       r.changelog, r.status, IF(l.release_id IS NULL, 0, 1) AS is_latest,
		       r.created_by_admin_id, r.created_by_username, r.created_by_role,
		       r.published_at, r.created_at, r.updated_at
		FROM admin_app_releases r
		LEFT JOIN admin_app_latest l ON l.release_id = r.id
		WHERE r.id = ?`, id)

	release, err := scanAppRelease(row)
	if err != nil {
		return nil, err
	}
	return &release, nil
}

func (s *Store) GetLatestAppRelease(ctx context.Context, platform string) (*AppRelease, error) {
	platform, err := NormalizeReleasePlatform(platform)
	if err != nil {
		return nil, err
	}
	return s.GetLatestAppReleaseFor(ctx, platform, ReleaseChannelStable, ReleaseArchUniversal)
}

func (s *Store) GetLatestAppReleaseFor(ctx context.Context, platform, channel, arch string) (*AppRelease, error) {
	platform, err := NormalizeReleasePlatform(platform)
	if err != nil {
		return nil, err
	}
	channel, err = NormalizeReleaseChannel(channel)
	if err != nil {
		return nil, err
	}
	arch, err = NormalizeReleaseArch(platform, arch)
	if err != nil {
		return nil, err
	}

	keys := releaseLatestLookupKeys(channel, arch)
	var lastErr error
	for _, key := range keys {
		release, err := s.getLatestAppReleaseExact(ctx, platform, key.channel, key.arch)
		if err == nil {
			return release, nil
		}
		if !errors.Is(err, sql.ErrNoRows) {
			return nil, err
		}
		lastErr = err
	}
	if lastErr == nil {
		lastErr = sql.ErrNoRows
	}
	return nil, lastErr
}

func (s *Store) getLatestAppReleaseExact(ctx context.Context, platform, channel, arch string) (*AppRelease, error) {

	row := s.db.QueryRowContext(ctx, `
		SELECT r.id, r.platform, r.channel, r.arch, r.version, r.version_code,
		       r.min_supported_version_code, r.update_level, r.title,
		       r.filename, r.storage_path, r.storage_key, r.file_size, r.sha256,
		       r.mime_type, r.download_url, r.public_download_url,
		       r.changelog, r.status, 1 AS is_latest,
		       r.created_by_admin_id, r.created_by_username, r.created_by_role,
		       r.published_at, r.created_at, r.updated_at
		FROM admin_app_latest l
		INNER JOIN admin_app_releases r ON r.id = l.release_id
		WHERE l.platform = ? AND l.channel = ? AND l.arch = ?`, platform, channel, arch)

	release, err := scanAppRelease(row)
	if err != nil {
		return nil, err
	}
	return &release, nil
}

func (s *Store) PublishAppRelease(ctx context.Context, id int64) (*AppRelease, error) {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	defer func() {
		if tx != nil {
			_ = tx.Rollback()
		}
	}()

	var platform, channel, arch string
	if err := tx.QueryRowContext(ctx, `
		SELECT platform, channel, arch FROM admin_app_releases WHERE id = ? FOR UPDATE`, id,
	).Scan(&platform, &channel, &arch); err != nil {
		return nil, err
	}

	if _, err := tx.ExecContext(ctx, `
		UPDATE admin_app_releases
		SET status = CASE WHEN id = ? THEN 'published' ELSE 'draft' END,
		    published_at = CASE WHEN id = ? THEN CURRENT_TIMESTAMP ELSE published_at END
		WHERE platform = ? AND channel = ? AND arch = ?`, id, id, platform, channel, arch); err != nil {
		return nil, err
	}

	if _, err := tx.ExecContext(ctx, `
		INSERT INTO admin_app_latest (platform, channel, arch, release_id)
		VALUES (?, ?, ?, ?)
		ON DUPLICATE KEY UPDATE release_id = VALUES(release_id)`, platform, channel, arch, id); err != nil {
		return nil, err
	}

	if err := tx.Commit(); err != nil {
		return nil, err
	}
	tx = nil

	return s.GetAppRelease(ctx, id)
}

func scanAppRelease(scanner interface {
	Scan(dest ...any) error
}) (AppRelease, error) {
	var (
		release     AppRelease
		isLatest    bool
		publishedAt sql.NullTime
	)

	err := scanner.Scan(
		&release.ID,
		&release.Platform,
		&release.Channel,
		&release.Arch,
		&release.Version,
		&release.VersionCode,
		&release.MinSupportedVersionCode,
		&release.UpdateLevel,
		&release.Title,
		&release.Filename,
		&release.StoragePath,
		&release.StorageKey,
		&release.FileSize,
		&release.SHA256,
		&release.MimeType,
		&release.DownloadURL,
		&release.PublicDownloadURL,
		&release.Changelog,
		&release.Status,
		&isLatest,
		&release.CreatedByAdminID,
		&release.CreatedByUsername,
		&release.CreatedByRole,
		&publishedAt,
		&release.CreatedAt,
		&release.UpdatedAt,
	)
	if err != nil {
		return AppRelease{}, err
	}
	release.IsLatest = isLatest
	if publishedAt.Valid {
		release.PublishedAt = &publishedAt.Time
	}
	release.Platform, _ = NormalizeReleasePlatform(release.Platform)
	release.Channel, _ = NormalizeReleaseChannel(release.Channel)
	release.Arch, _ = NormalizeReleaseArch(release.Platform, release.Arch)
	release.UpdateLevel, _ = NormalizeReleaseUpdateLevel(release.UpdateLevel)
	if release.StorageKey == "" {
		release.StorageKey = release.StoragePath
	}
	return release, nil
}

func IsReleaseNotFound(err error) bool {
	return errors.Is(err, sql.ErrNoRows)
}

type releaseLatestKey struct {
	channel string
	arch    string
}

func releaseLatestLookupKeys(channel, arch string) []releaseLatestKey {
	keys := []releaseLatestKey{{channel: channel, arch: arch}}
	add := func(next releaseLatestKey) {
		for _, key := range keys {
			if key == next {
				return
			}
		}
		keys = append(keys, next)
	}
	add(releaseLatestKey{channel: channel, arch: ReleaseArchUniversal})
	add(releaseLatestKey{channel: ReleaseChannelStable, arch: arch})
	add(releaseLatestKey{channel: ReleaseChannelStable, arch: ReleaseArchUniversal})
	return keys
}

func (s *Store) migrateReleaseSchema(ctx context.Context) error {
	columns := []struct {
		table string
		sql   string
	}{
		{"admin_app_releases", "ALTER TABLE admin_app_releases ADD COLUMN channel VARCHAR(32) NOT NULL DEFAULT 'stable' AFTER platform"},
		{"admin_app_releases", "ALTER TABLE admin_app_releases ADD COLUMN arch VARCHAR(32) NOT NULL DEFAULT 'universal' AFTER channel"},
		{"admin_app_releases", "ALTER TABLE admin_app_releases ADD COLUMN min_supported_version_code INT NOT NULL DEFAULT 0 AFTER version_code"},
		{"admin_app_releases", "ALTER TABLE admin_app_releases ADD COLUMN update_level VARCHAR(16) NOT NULL DEFAULT 'optional' AFTER min_supported_version_code"},
		{"admin_app_releases", "ALTER TABLE admin_app_releases ADD COLUMN title VARCHAR(255) NOT NULL DEFAULT '' AFTER update_level"},
		{"admin_app_releases", "ALTER TABLE admin_app_releases ADD COLUMN storage_key VARCHAR(255) NOT NULL DEFAULT '' AFTER storage_path"},
		{"admin_app_releases", "ALTER TABLE admin_app_releases ADD COLUMN mime_type VARCHAR(128) NOT NULL DEFAULT '' AFTER sha256"},
		{"admin_app_releases", "ALTER TABLE admin_app_releases ADD COLUMN download_url VARCHAR(1024) NOT NULL DEFAULT '' AFTER mime_type"},
		{"admin_app_releases", "ALTER TABLE admin_app_releases ADD COLUMN public_download_url VARCHAR(1024) NOT NULL DEFAULT '' AFTER download_url"},
		{"admin_app_latest", "ALTER TABLE admin_app_latest ADD COLUMN channel VARCHAR(32) NOT NULL DEFAULT 'stable' AFTER platform"},
		{"admin_app_latest", "ALTER TABLE admin_app_latest ADD COLUMN arch VARCHAR(32) NOT NULL DEFAULT 'universal' AFTER channel"},
	}
	for _, column := range columns {
		if err := s.execReleaseMigration(ctx, column.sql, 1060); err != nil {
			return fmt.Errorf("migrate %s: %w", column.table, err)
		}
	}

	statements := []string{
		"UPDATE admin_app_releases SET platform = 'windows' WHERE platform = 'pc'",
		"UPDATE admin_app_latest SET platform = 'windows' WHERE platform = 'pc'",
		"UPDATE admin_app_releases SET channel = 'stable' WHERE channel = ''",
		"UPDATE admin_app_releases SET arch = 'universal' WHERE arch = ''",
		"UPDATE admin_app_releases SET update_level = 'optional' WHERE update_level = ''",
		"UPDATE admin_app_releases SET storage_key = storage_path WHERE storage_key = ''",
		"UPDATE admin_app_releases SET download_url = CONCAT('/releases/', storage_path) WHERE download_url = ''",
		"UPDATE admin_app_latest SET channel = 'stable' WHERE channel = ''",
		"UPDATE admin_app_latest SET arch = 'universal' WHERE arch = ''",
	}
	for _, stmt := range statements {
		if _, err := s.db.ExecContext(ctx, stmt); err != nil {
			return fmt.Errorf("migrate release data: %w", err)
		}
	}
	if err := s.ensureLatestPrimaryKey(ctx); err != nil {
		return err
	}
	if err := s.execReleaseMigration(ctx, "ALTER TABLE admin_app_releases ADD KEY idx_admin_app_releases_latest (platform, channel, arch, status)", 1061); err != nil {
		return fmt.Errorf("migrate release index: %w", err)
	}
	return nil
}

func (s *Store) execReleaseMigration(ctx context.Context, statement string, ignoredMySQLErrors ...uint16) error {
	_, err := s.db.ExecContext(ctx, statement)
	if err == nil {
		return nil
	}
	var mysqlErr *mysql.MySQLError
	if errors.As(err, &mysqlErr) {
		for _, code := range ignoredMySQLErrors {
			if mysqlErr.Number == code {
				return nil
			}
		}
	}
	return err
}

func (s *Store) ensureLatestPrimaryKey(ctx context.Context) error {
	rows, err := s.db.QueryContext(ctx, `
		SELECT COLUMN_NAME
		FROM INFORMATION_SCHEMA.STATISTICS
		WHERE TABLE_SCHEMA = DATABASE()
			AND TABLE_NAME = 'admin_app_latest'
			AND INDEX_NAME = 'PRIMARY'
		ORDER BY SEQ_IN_INDEX`)
	if err != nil {
		return fmt.Errorf("inspect admin_app_latest primary key: %w", err)
	}
	defer rows.Close()

	var columns []string
	for rows.Next() {
		var column string
		if err := rows.Scan(&column); err != nil {
			return err
		}
		columns = append(columns, strings.ToLower(column))
	}
	if err := rows.Err(); err != nil {
		return err
	}
	if strings.Join(columns, ",") == "platform,channel,arch" {
		return nil
	}
	if _, err := s.db.ExecContext(ctx, `ALTER TABLE admin_app_latest DROP PRIMARY KEY, ADD PRIMARY KEY (platform, channel, arch)`); err != nil {
		return fmt.Errorf("migrate admin_app_latest primary key: %w", err)
	}
	return nil
}
