package config

import (
	"errors"
	"fmt"
	"os"
	"time"
)

type Config struct {
	AppPort       string
	AppEnv        string
	MetadataDBURL string

	SessionIdleTimeout      time.Duration
	SessionAbsoluteTimeout  time.Duration
	SessionRotationInterval time.Duration
	SessionGracePeriod      time.Duration
}

func (c *Config) IsProduction() bool {
	return c.AppEnv == "production"
}

func Load() (*Config, error) {
	cfg := &Config{
		AppPort:       envOr("APP_PORT", "8080"),
		AppEnv:        envOr("APP_ENV", "development"),
		MetadataDBURL: os.Getenv("METADATA_DB_URL"),
	}
	if cfg.MetadataDBURL == "" {
		return nil, errors.New("config: METADATA_DB_URL wajib diisi")
	}

	durations := []struct {
		key string
		def time.Duration
		dst *time.Duration
	}{
		{"SESSION_IDLE_TIMEOUT", 30 * time.Minute, &cfg.SessionIdleTimeout},
		{"SESSION_ABSOLUTE_TIMEOUT", 8 * time.Hour, &cfg.SessionAbsoluteTimeout},
		{"SESSION_ROTATION_INTERVAL", 15 * time.Minute, &cfg.SessionRotationInterval},
		{"SESSION_GRACE_PERIOD", 30 * time.Second, &cfg.SessionGracePeriod},
	}
	for _, d := range durations {
		v, err := durationEnv(d.key, d.def)
		if err != nil {
			return nil, err
		}
		*d.dst = v
	}
	return cfg, nil
}

func envOr(key, def string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return def
}

func durationEnv(key string, def time.Duration) (time.Duration, error) {
	v := os.Getenv(key)
	if v == "" {
		return def, nil
	}
	d, err := time.ParseDuration(v)
	if err != nil {
		return 0, fmt.Errorf("config: %s tidak valid: %w", key, err)
	}
	return d, nil
}
