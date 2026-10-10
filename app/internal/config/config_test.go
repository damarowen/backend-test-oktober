package config

import (
	"strings"
	"testing"
	"time"
)

const validSecret = "0123456789abcdef0123456789abcdef"

func setRequired(t *testing.T) {
	t.Helper()
	t.Setenv("DB_DSN", "app:app@tcp(localhost:3306)/appdb?parseTime=true")
	t.Setenv("JWT_SECRET", validSecret)
}

func TestLoad_Defaults(t *testing.T) {
	setRequired(t)
	t.Setenv("APP_PORT", "")
	t.Setenv("JWT_TTL", "")

	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load() unexpected error: %v", err)
	}
	if cfg.Port != "8080" {
		t.Errorf("Port: got %q, want %q", cfg.Port, "8080")
	}
	if cfg.JWTTTL != 24*time.Hour {
		t.Errorf("JWTTTL: got %v, want %v", cfg.JWTTTL, 24*time.Hour)
	}
}

func TestLoad_MissingJWTSecret_ReturnsError(t *testing.T) {
	setRequired(t)
	t.Setenv("JWT_SECRET", "")

	_, err := Load()
	if err == nil {
		t.Fatal("Load() expected error, got nil")
	}
	if !strings.Contains(err.Error(), "JWT_SECRET") {
		t.Errorf("error should mention JWT_SECRET, got: %v", err)
	}
}

func TestLoad_ShortJWTSecret_ReturnsError(t *testing.T) {
	setRequired(t)
	t.Setenv("JWT_SECRET", "short")

	_, err := Load()
	if err == nil {
		t.Fatal("Load() expected error for short JWT_SECRET, got nil")
	}
	if !strings.Contains(err.Error(), "JWT_SECRET") {
		t.Errorf("error should mention JWT_SECRET, got: %v", err)
	}
}

func TestLoad_MissingDBDSN_ReturnsError(t *testing.T) {
	setRequired(t)
	t.Setenv("DB_DSN", "")

	_, err := Load()
	if err == nil {
		t.Fatal("Load() expected error, got nil")
	}
	if !strings.Contains(err.Error(), "DB_DSN") {
		t.Errorf("error should mention DB_DSN, got: %v", err)
	}
}

func TestLoad_CustomValues(t *testing.T) {
	setRequired(t)
	t.Setenv("APP_PORT", "9999")
	t.Setenv("JWT_TTL", "1h30m")

	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load() unexpected error: %v", err)
	}
	if cfg.Port != "9999" {
		t.Errorf("Port: got %q, want %q", cfg.Port, "9999")
	}
	if cfg.JWTTTL != 90*time.Minute {
		t.Errorf("JWTTTL: got %v, want %v", cfg.JWTTTL, 90*time.Minute)
	}
}

func TestLoad_InvalidJWTTTL_ReturnsError(t *testing.T) {
	setRequired(t)
	t.Setenv("JWT_TTL", "not-a-duration")

	if _, err := Load(); err == nil {
		t.Fatal("Load() expected error for invalid JWT_TTL, got nil")
	}
}
