package stickers

import (
	"context"
	"fmt"
)

func (s *Service) EnsureSchema(ctx context.Context) error {
	statements := []string{
		`CREATE TABLE IF NOT EXISTS sticker_sets (
			id BIGINT NOT NULL AUTO_INCREMENT PRIMARY KEY,
			access_hash BIGINT NOT NULL,
			short_name VARCHAR(128) NOT NULL,
			title VARCHAR(255) NOT NULL,
			sticker_type VARCHAR(32) NOT NULL DEFAULT 'regular',
			source VARCHAR(255) NOT NULL DEFAULT '',
			source_platform VARCHAR(32) NOT NULL DEFAULT '',
			hash BIGINT NOT NULL DEFAULT 0,
			created_by BIGINT NOT NULL DEFAULT 0,
			disabled_at TIMESTAMP NULL DEFAULT NULL,
			created_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
			updated_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP ON UPDATE CURRENT_TIMESTAMP,
			UNIQUE KEY uk_sticker_sets_short_name (short_name),
			KEY idx_sticker_sets_disabled (disabled_at)
		) ENGINE=InnoDB AUTO_INCREMENT=950000000001 DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci;`,
		`CREATE TABLE IF NOT EXISTS stickers (
			id BIGINT NOT NULL AUTO_INCREMENT PRIMARY KEY,
			set_id BIGINT NOT NULL,
			access_hash BIGINT NOT NULL,
			emoji VARCHAR(64) NOT NULL DEFAULT '',
			format VARCHAR(32) NOT NULL,
			mime_type VARCHAR(128) NOT NULL,
			width INT NOT NULL DEFAULT 0,
			height INT NOT NULL DEFAULT 0,
			size BIGINT NOT NULL DEFAULT 0,
			storage_key VARCHAR(255) NOT NULL,
			thumb_storage_key VARCHAR(255) NOT NULL DEFAULT '',
			sha256 CHAR(64) NOT NULL,
			position INT NOT NULL DEFAULT 0,
			keywords JSON NULL,
			telegram_file_id VARCHAR(255) NOT NULL DEFAULT '',
			telegram_file_unique_id VARCHAR(255) NOT NULL DEFAULT '',
			created_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
			updated_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP ON UPDATE CURRENT_TIMESTAMP,
			UNIQUE KEY uk_stickers_set_unique (set_id, telegram_file_unique_id),
			KEY idx_stickers_sha_size_mime (sha256, size, mime_type),
			KEY idx_stickers_set_position (set_id, position),
			CONSTRAINT fk_stickers_set_id FOREIGN KEY (set_id) REFERENCES sticker_sets(id) ON DELETE CASCADE
		) ENGINE=InnoDB AUTO_INCREMENT=960000000001 DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci;`,
		`CREATE TABLE IF NOT EXISTS user_sticker_sets (
			user_id BIGINT NOT NULL,
			set_id BIGINT NOT NULL,
			installed TINYINT(1) NOT NULL DEFAULT 1,
			archived TINYINT(1) NOT NULL DEFAULT 0,
			sort_order INT NOT NULL DEFAULT 0,
			created_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
			updated_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP ON UPDATE CURRENT_TIMESTAMP,
			PRIMARY KEY (user_id, set_id),
			KEY idx_user_sticker_sets_user_order (user_id, installed, sort_order),
			CONSTRAINT fk_user_sticker_sets_set_id FOREIGN KEY (set_id) REFERENCES sticker_sets(id) ON DELETE CASCADE
		) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci;`,
		`CREATE TABLE IF NOT EXISTS recent_stickers (
			user_id BIGINT NOT NULL,
			sticker_id BIGINT NOT NULL,
			attached TINYINT(1) NOT NULL DEFAULT 0,
			used_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
			PRIMARY KEY (user_id, sticker_id, attached),
			KEY idx_recent_stickers_user_used (user_id, attached, used_at),
			CONSTRAINT fk_recent_stickers_sticker_id FOREIGN KEY (sticker_id) REFERENCES stickers(id) ON DELETE CASCADE
		) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci;`,
		`CREATE TABLE IF NOT EXISTS sticker_emoji_index (
			emoji VARCHAR(64) NOT NULL,
			sticker_id BIGINT NOT NULL,
			set_id BIGINT NOT NULL,
			weight INT NOT NULL DEFAULT 100,
			source VARCHAR(32) NOT NULL DEFAULT 'sticker_import',
			created_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
			PRIMARY KEY (emoji, sticker_id),
			KEY idx_sticker_emoji_index_lookup (emoji, weight),
			KEY idx_sticker_emoji_index_set (set_id),
			CONSTRAINT fk_sticker_emoji_index_sticker_id FOREIGN KEY (sticker_id) REFERENCES stickers(id) ON DELETE CASCADE,
			CONSTRAINT fk_sticker_emoji_index_set_id FOREIGN KEY (set_id) REFERENCES sticker_sets(id) ON DELETE CASCADE
		) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci;`,
	}
	for _, stmt := range statements {
		if _, err := s.db.ExecContext(ctx, stmt); err != nil {
			return fmt.Errorf("ensure sticker schema: %w", err)
		}
	}
	return nil
}
