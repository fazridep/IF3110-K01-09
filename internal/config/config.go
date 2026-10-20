package config
import (
	"fmt"
	"os"
	"time"
)

type Config struct {
	AppPort string
	AppEnv  string
	MetadataDBURL string
	SessionIdleTimeout     time.Duration
	SessionAbsoluteTimeout time.Duration
	SessionRotationInterval time.Duration
	SessionGracePeriod     time.Duration
}

// jika berjalan di production
func (c *Config) IsProduction() bool {
	return c.AppEnv == "production"
}

func Load() (*Config, error) {
	cfg := &Config{
		AppPort:                 envOrDefault("APP_PORT", "8080"),
		AppEnv:                  envOrDefault("APP_ENV", "development"),
		MetadataDBURL:           os.Getenv("METADATA_DB_URL"),
		SessionIdleTimeout:     30 * time.Minute,
		SessionAbsoluteTimeout: 8 * time.Hour,
		SessionRotationInterval: 15 * time.Minute,
		SessionGracePeriod:     30 * time.Second,
	}

	if v := os.Getenv("SESSION_IDLE_TIMEOUT"); v != "" {
		d, err := time.ParseDuration(v)
		if err != nil {
			return nil, fmt.Errorf("config: SESSION_IDLE_TIMEOUT tidak valid: %w", err)
		}
		cfg.SessionIdleTimeout = d
	}
	if v := os.Getenv("SESSION_ABSOLUTE_TIMEOUT"); v != "" {
		d, err := time.ParseDuration(v)
		if err != nil {
			return nil, fmt.Errorf("config: SESSION_ABSOLUTE_TIMEOUT tidak valid: %w", err)
		}
		cfg.SessionAbsoluteTimeout = d
	}
	if v := os.Getenv("SESSION_ROTATION_INTERVAL"); v != "" {
		d, err := time.ParseDuration(v)
		if err != nil {
			return nil, fmt.Errorf("config: SESSION_ROTATION_INTERVAL tidak valid: %w", err)
		}
		cfg.SessionRotationInterval = d
	}
	if v := os.Getenv("SESSION_GRACE_PERIOD"); v != "" {
		d, err := time.ParseDuration(v)
		if err != nil {
			return nil, fmt.Errorf("config: SESSION_GRACE_PERIOD tidak valid: %w", err)
		}
		cfg.SessionGracePeriod = d
	}
	if cfg.MetadataDBURL == "" {
		return nil, fmt.Errorf("config: METADATA_DB_URL wajib diisi")
	}
	return cfg, nil
}

func envOrDefault(key, def string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return def
}