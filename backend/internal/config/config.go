package config

import (
	"fmt"
	"os"
	"strings"
	"time"
)

type Config struct {
	ListenAddr        string
	DatabaseDSN       string
	EnableBroadcasts  bool
	MsgRPCAddr        string
	JWTSecret         string
	TokenTTL          time.Duration
	BootstrapUsername string
	BootstrapPassword string
}

func Load() (Config, error) {
	ttl := getEnv("ADMIN_TOKEN_TTL", "12h")
	tokenTTL, err := time.ParseDuration(ttl)
	if err != nil {
		return Config{}, fmt.Errorf("parse ADMIN_TOKEN_TTL: %w", err)
	}

	cfg := Config{
		ListenAddr:        getEnv("ADMIN_LISTEN_ADDR", ":8088"),
		DatabaseDSN:       os.Getenv("ADMIN_DATABASE_DSN"),
		EnableBroadcasts:  getEnvBool("ADMIN_ENABLE_BROADCASTS", false),
		MsgRPCAddr:        strings.TrimSpace(os.Getenv("ADMIN_MSG_RPC_ADDR")),
		JWTSecret:         os.Getenv("ADMIN_JWT_SECRET"),
		TokenTTL:          tokenTTL,
		BootstrapUsername: getEnv("ADMIN_BOOTSTRAP_USERNAME", "admin"),
		BootstrapPassword: os.Getenv("ADMIN_BOOTSTRAP_PASSWORD"),
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
	if cfg.EnableBroadcasts && cfg.MsgRPCAddr == "" {
		return Config{}, fmt.Errorf("ADMIN_MSG_RPC_ADDR is required when ADMIN_ENABLE_BROADCASTS=true")
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
