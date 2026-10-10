package config

import (
	"fmt"
	"os"
	"time"
)

const (
	defaultPort      = "8080"
	defaultJWTTTL    = 24 * time.Hour
	minJWTSecretSize = 32
)

// Config holds all runtime configuration, loaded exclusively from environment
// variables so no secrets live in source.
type Config struct {
	// Port is the HTTP listen port (APP_PORT, default "8080").
	Port string
	// DBDSN is the MySQL data source name (DB_DSN, required).
	DBDSN string
	// JWTSecret is the HMAC signing key (JWT_SECRET, required, >= 32 bytes).
	JWTSecret string
	// JWTTTL is the issued token lifetime (JWT_TTL, default 24h).
	JWTTTL time.Duration
}

// Load reads configuration from the environment and validates it. It returns a
// descriptive error when a required value is missing or invalid.
func Load() (Config, error) {
	cfg := Config{
		Port:      getenvDefault("APP_PORT", defaultPort),
		DBDSN:     os.Getenv("DB_DSN"),
		JWTSecret: os.Getenv("JWT_SECRET"),
	}

	if cfg.DBDSN == "" {
		return Config{}, fmt.Errorf("config: DB_DSN is required")
	}
	if cfg.JWTSecret == "" {
		return Config{}, fmt.Errorf("config: JWT_SECRET is required")
	}
	if len(cfg.JWTSecret) < minJWTSecretSize {
		return Config{}, fmt.Errorf("config: JWT_SECRET must be at least %d bytes", minJWTSecretSize)
	}

	rawTTL := os.Getenv("JWT_TTL")
	if rawTTL == "" {
		cfg.JWTTTL = defaultJWTTTL
		return cfg, nil
	}

	ttl, err := time.ParseDuration(rawTTL)
	if err != nil {
		return Config{}, fmt.Errorf("config: invalid JWT_TTL %q: %w", rawTTL, err)
	}
	if ttl <= 0 {
		return Config{}, fmt.Errorf("config: JWT_TTL must be positive, got %q", rawTTL)
	}
	cfg.JWTTTL = ttl

	return cfg, nil
}

func getenvDefault(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}
