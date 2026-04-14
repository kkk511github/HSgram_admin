package store

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"time"
)

const (
	ReleasePlatformAndroid = "android"
	ReleasePlatformPC      = "pc"

	ReleaseStatusDraft     = "draft"
	ReleaseStatusPublished = "published"
)

type AppRelease struct {
	ID                int64      `json:"id"`
	Platform          string     `json:"platform"`
	Version           string     `json:"version"`
	VersionCode       int        `json:"versionCode"`
	Filename          string     `json:"filename"`
	StoragePath       string     `json:"storagePath"`
	FileSize          int64      `json:"fileSize"`
	SHA256            string     `json:"sha256"`
	Changelog         string     `json:"changelog"`
	Status            string     `json:"status"`
	IsLatest          bool       `json:"isLatest"`
	CreatedByAdminID  int64      `json:"createdByAdminId"`
	CreatedByUsername string     `json:"createdByUsername"`
	CreatedByRole     string     `json:"createdByRole"`
	PublishedAt       *time.Time `json:"publishedAt,omitempty"`
	CreatedAt         time.Time  `json:"createdAt"`
	UpdatedAt         time.Time  `json:"updatedAt"`
}

type CreateAppReleaseParams struct {
	Platform    string
	Version     string
	VersionCode int
	Filename    string
	StoragePath string
	FileSize    int64
	SHA256      string
	Changelog   string
}

func NormalizeReleasePlatform(platform string) (string, error) {
	switch strings.ToLower(strings.TrimSpace(platform)) {
	case ReleasePlatformAndroid:
		return ReleasePlatformAndroid, nil
	case ReleasePlatformPC:
		return ReleasePlatformPC, nil
	default:
		return "", fmt.Errorf("unsupported platform")
	}
}

func (s *Store) EnsureReleaseSchema(ctx context.Context) error {
	statements := []string{
		`CREATE TABLE IF NOT EXISTS admin_app_releases (
			id BIGINT NOT NULL AUTO_INCREMENT PRIMARY KEY,
			platform VARCHAR(16) NOT NULL,
			version VARCHAR(64) NOT NULL,
			version_code INT NOT NULL,
			filename VARCHAR(255) NOT NULL,
			storage_path VARCHAR(255) NOT NULL,
			file_size BIGINT NOT NULL,
			sha256 VARCHAR(64) NOT NULL,
			changelog TEXT NOT NULL,
			status VARCHAR(16) NOT NULL DEFAULT 'draft',
			created_by_admin_id BIGINT NOT NULL,
			created_by_username VARCHAR(64) NOT NULL,
			created_by_role VARCHAR(32) NOT NULL,
			published_at TIMESTAMP NULL DEFAULT NULL,
			created_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
			updated_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP ON UPDATE CURRENT_TIMESTAMP,
			KEY idx_admin_app_releases_platform_created (platform, created_at),
			KEY idx_admin_app_releases_status (status),
			UNIQUE KEY uk_admin_app_releases_storage_path (storage_path)
		) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci;`,
		`CREATE TABLE IF NOT EXISTS admin_app_latest (
			platform VARCHAR(16) NOT NULL PRIMARY KEY,
			release_id BIGINT NOT NULL,
			updated_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP ON UPDATE CURRENT_TIMESTAMP,
			KEY idx_admin_app_latest_release_id (release_id)
		) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci;`,
	}

	for _, stmt := range statements {
		if _, err := s.db.ExecContext(ctx, stmt); err != nil {
			return fmt.Errorf("ensure release schema: %w", err)
		}
	}
	return nil
}

func (s *Store) CreateAppRelease(ctx context.Context, admin AdminUser, params CreateAppReleaseParams) (*AppRelease, error) {
	platform, err := NormalizeReleasePlatform(params.Platform)
	if err != nil {
		return nil, err
	}
	if strings.TrimSpace(params.Version) == "" {
		return nil, fmt.Errorf("version is required")
	}
	if params.VersionCode <= 0 {
		return nil, fmt.Errorf("version code is required")
	}
	if strings.TrimSpace(params.Filename) == "" || strings.TrimSpace(params.StoragePath) == "" {
		return nil, fmt.Errorf("release file is required")
	}
	if params.FileSize <= 0 {
		return nil, fmt.Errorf("release file is empty")
	}
	if strings.TrimSpace(params.SHA256) == "" {
		return nil, fmt.Errorf("sha256 is required")
	}

	result, err := s.db.ExecContext(ctx, `
		INSERT INTO admin_app_releases (
			platform, version, version_code, filename, storage_path, file_size, sha256, changelog,
			status, created_by_admin_id, created_by_username, created_by_role
		) VALUES (?, ?, ?, ?, ?, ?, ?, ?, 'draft', ?, ?, ?)`,
		platform,
		strings.TrimSpace(params.Version),
		params.VersionCode,
		strings.TrimSpace(params.Filename),
		strings.TrimSpace(params.StoragePath),
		params.FileSize,
		strings.TrimSpace(params.SHA256),
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
		SELECT r.id, r.platform, r.version, r.version_code, r.filename, r.storage_path, r.file_size, r.sha256,
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
		SELECT r.id, r.platform, r.version, r.version_code, r.filename, r.storage_path, r.file_size, r.sha256,
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

	row := s.db.QueryRowContext(ctx, `
		SELECT r.id, r.platform, r.version, r.version_code, r.filename, r.storage_path, r.file_size, r.sha256,
		       r.changelog, r.status, 1 AS is_latest,
		       r.created_by_admin_id, r.created_by_username, r.created_by_role,
		       r.published_at, r.created_at, r.updated_at
		FROM admin_app_latest l
		INNER JOIN admin_app_releases r ON r.id = l.release_id
		WHERE l.platform = ?`, platform)

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

	var platform string
	if err := tx.QueryRowContext(ctx, `
		SELECT platform FROM admin_app_releases WHERE id = ? FOR UPDATE`, id,
	).Scan(&platform); err != nil {
		return nil, err
	}

	if _, err := tx.ExecContext(ctx, `
		UPDATE admin_app_releases
		SET status = CASE WHEN id = ? THEN 'published' ELSE 'draft' END,
		    published_at = CASE WHEN id = ? THEN CURRENT_TIMESTAMP ELSE published_at END
		WHERE platform = ?`, id, id, platform); err != nil {
		return nil, err
	}

	if _, err := tx.ExecContext(ctx, `
		INSERT INTO admin_app_latest (platform, release_id)
		VALUES (?, ?)
		ON DUPLICATE KEY UPDATE release_id = VALUES(release_id)`, platform, id); err != nil {
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
		&release.Version,
		&release.VersionCode,
		&release.Filename,
		&release.StoragePath,
		&release.FileSize,
		&release.SHA256,
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
	return release, nil
}

func IsReleaseNotFound(err error) bool {
	return errors.Is(err, sql.ErrNoRows)
}
