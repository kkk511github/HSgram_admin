package config

import (
	"fmt"
	"os"
	"strings"
	"time"
)

type Config struct {
	ListenAddr         string
	DatabaseDSN        string
	EnableBroadcasts   bool
	MsgRPCAddr         string
	AuthsessionRPCAddr string
	SyncRPCAddr        string
	RedisAddr          string
	RedisPass          string
	ReleasesDir        string
	PublicBaseURL      string
	JWTSecret          string
	TokenTTL           time.Duration
	HTTPReadTimeout    time.Duration
	HTTPWriteTimeout   time.Duration
	BootstrapUsername  string
	BootstrapPassword  string
}

func Load() (Config, error) {
	ttl := getEnv("ADMIN_TOKEN_TTL", "12h")
	tokenTTL, err := time.ParseDuration(ttl)
	if err != nil {
		return Config{}, fmt.Errorf("parse ADMIN_TOKEN_TTL: %w", err)
	}

	httpReadTimeout, err := time.ParseDuration(getEnv("ADMIN_HTTP_READ_TIMEOUT", "30m"))
	if err != nil {
		return Config{}, fmt.Errorf("parse ADMIN_HTTP_READ_TIMEOUT: %w", err)
	}
	httpWriteTimeout, err := time.ParseDuration(getEnv("ADMIN_HTTP_WRITE_TIMEOUT", "30m"))
	if err != nil {
		return Config{}, fmt.Errorf("parse ADMIN_HTTP_WRITE_TIMEOUT: %w", err)
	}

	cfg := Config{
		ListenAddr:         getEnv("ADMIN_LISTEN_ADDR", ":8088"),
		DatabaseDSN:        os.Getenv("ADMIN_DATABASE_DSN"),
		EnableBroadcasts:   getEnvBool("ADMIN_ENABLE_BROADCASTS", true),
		MsgRPCAddr:         strings.TrimSpace(os.Getenv("ADMIN_MSG_RPC_ADDR")),
		AuthsessionRPCAddr: strings.TrimSpace(os.Getenv("ADMIN_AUTHSESSION_RPC_ADDR")),
		SyncRPCAddr:        strings.TrimSpace(getEnv("ADMIN_SYNC_RPC_ADDR", "hsgram_server-teamgram-1:20420")),
		RedisAddr:          strings.TrimSpace(getEnv("ADMIN_REDIS_ADDR", "redis:6379")),
		RedisPass:          os.Getenv("ADMIN_REDIS_PASS"),
		ReleasesDir:        strings.TrimSpace(getEnv("ADMIN_RELEASES_DIR", "./releases")),
		PublicBaseURL:      strings.TrimRight(strings.TrimSpace(os.Getenv("ADMIN_PUBLIC_BASE_URL")), "/"),
		JWTSecret:          os.Getenv("ADMIN_JWT_SECRET"),
		TokenTTL:           tokenTTL,
		HTTPReadTimeout:    httpReadTimeout,
		HTTPWriteTimeout:   httpWriteTimeout,
		BootstrapUsername:  getEnv("ADMIN_BOOTSTRAP_USERNAME", "admin"),
		BootstrapPassword:  os.Getenv("ADMIN_BOOTSTRAP_PASSWORD"),
	}

	if cfg.DatabaseDSN == "" {
		return Config{}, fmt.Errorf("ADMIN_DATABASE_DSN is required")
	}
	if cfg.JWTSecret == "" {
		return Config{}, fmt.Errorf("ADMIN_JWT_SECRET is required")
	}
	if cfg.BootstrapPassword == "" {
		return Config{}, fmt.Errorf("ADMIN_BOOTSTRAP_PASSWORD is required")
	}

	return cfg, nil
}

func getEnv(key, fallback string) string {
	if value := os.Getenv(key); value != "" {
		return value
	}
	return fallback
}

func getEnvBool(key string, fallback bool) bool {
	value := strings.TrimSpace(os.Getenv(key))
	if value == "" {
		return fallback
	}

	switch strings.ToLower(value) {
	case "1", "true", "yes", "on":
		return true
	case "0", "false", "no", "off":
		return false
	default:
		return fallback
	}
}
