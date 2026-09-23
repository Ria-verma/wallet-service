package config

import (
	"fmt"
	"os"
	"strconv"
	"time"
)

type Config struct {
	DatabaseURL         string
	JWTSecret           string
	Port                string
	InitialBalancePaise int64
	TokenTTL            time.Duration
	DBTimeout           time.Duration
	ClaimWindow         time.Duration
}

// Load reads configuration from the environment and fails fast on anything
// missing or malformed, so a misconfigured instance never serves traffic.
func Load() (Config, error) {
	cfg := Config{
		Port:                getEnv("PORT", "8080"),
		InitialBalancePaise: 100000,
		TokenTTL:            24 * time.Hour,
		DBTimeout:           5 * time.Second,
		ClaimWindow:         24 * time.Hour,
	}

	cfg.DatabaseURL = os.Getenv("DATABASE_URL")
	if cfg.DatabaseURL == "" {
		return Config{}, fmt.Errorf("DATABASE_URL is required")
	}

	cfg.JWTSecret = os.Getenv("JWT_SECRET")
	if len(cfg.JWTSecret) < 16 {
		return Config{}, fmt.Errorf("JWT_SECRET is required (min 16 chars)")
	}

	if v := os.Getenv("INITIAL_BALANCE_PAISE"); v != "" {
		n, err := strconv.ParseInt(v, 10, 64)
		if err != nil || n < 0 {
			return Config{}, fmt.Errorf("INITIAL_BALANCE_PAISE must be a non-negative integer, got %q", v)
		}
		cfg.InitialBalancePaise = n
	}

	if v := os.Getenv("CLAIM_WINDOW"); v != "" {
		d, err := time.ParseDuration(v)
		if err != nil || d <= 0 {
			return Config{}, fmt.Errorf("CLAIM_WINDOW must be a positive duration (e.g. 24h), got %q", v)
		}
		cfg.ClaimWindow = d
	}

	return cfg, nil
}

func getEnv(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}
