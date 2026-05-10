CREATE TABLE IF NOT EXISTS admin_users (
  id BIGINT NOT NULL AUTO_INCREMENT PRIMARY KEY,
  username VARCHAR(64) NOT NULL,
  password_hash VARCHAR(255) NOT NULL,
  role VARCHAR(32) NOT NULL DEFAULT 'super_admin',
  is_active TINYINT(1) NOT NULL DEFAULT 1,
  created_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
  updated_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP ON UPDATE CURRENT_TIMESTAMP,
  UNIQUE KEY uk_admin_users_username (username)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci;

CREATE TABLE IF NOT EXISTS admin_audit_logs (
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
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci;

CREATE TABLE IF NOT EXISTS admin_runtime_settings (
  setting_key VARCHAR(64) NOT NULL PRIMARY KEY,
  setting_value_json JSON NOT NULL,
  created_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
  updated_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP ON UPDATE CURRENT_TIMESTAMP
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci;

CREATE TABLE IF NOT EXISTS group_auto_message_config (
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
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci;

CREATE TABLE IF NOT EXISTS group_auto_message_items (
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
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci;

CREATE TABLE IF NOT EXISTS group_auto_message_logs (
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
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci;
