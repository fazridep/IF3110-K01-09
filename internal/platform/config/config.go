package config

import (
	"errors"
	"fmt"
	"os"
	"strconv"
	"time"
)

type Config struct {
	AppPort       string
	AppEnv        string
	MetadataDBURL string

	// CredentialEncryptionKey kunci AES-GCM buat password koneksi database
	// target. Opsional di Load supaya `migrate` tetap jalan tanpa kunci ini;
	// server yang mewajibkan (lihat main.go).
	CredentialEncryptionKey string

	LogLevel      string
	TraceExporter string
	ServiceName   string
	SPADir        string

	WorkerConcurrency  int
	WorkerMaxAttempts  int
	WorkerPollInterval time.Duration
	WorkerLeaseTTL     time.Duration

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
		AppPort:                 envOr("APP_PORT", "8080"),
		AppEnv:                  envOr("APP_ENV", "development"),
		MetadataDBURL:           os.Getenv("METADATA_DB_URL"),
		CredentialEncryptionKey: os.Getenv("CREDENTIAL_ENCRYPTION_KEY"),
		LogLevel:                envOr("LOG_LEVEL", "info"),
		TraceExporter:           envOr("OTEL_TRACES_EXPORTER", "console"),
		ServiceName:             envOr("OTEL_SERVICE_NAME", "db-platform-server"),
		SPADir:                  envOr("SPA_DIR", "frontend/dist"),
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
		{"WORKER_POLL_INTERVAL", time.Second, &cfg.WorkerPollInterval},
		{"WORKER_LEASE_TTL", 30 * time.Second, &cfg.WorkerLeaseTTL},
	}
	for _, d := range durations {
		v, err := durationEnv(d.key, d.def)
		if err != nil {
			return nil, err
		}
		*d.dst = v
	}

	ints := []struct {
		key string
		def int
		dst *int
	}{
		{"WORKER_CONCURRENCY", 4, &cfg.WorkerConcurrency},
		{"WORKER_MAX_ATTEMPTS", 3, &cfg.WorkerMaxAttempts},
	}
	for _, n := range ints {
		v, err := intEnv(n.key, n.def)
		if err != nil {
			return nil, err
		}
		*n.dst = v
	}
	return cfg, nil
}

func envOr(key, def string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return def
}

func intEnv(key string, def int) (int, error) {
	v := os.Getenv(key)
	if v == "" {
		return def, nil
	}
	n, err := strconv.Atoi(v)
	if err != nil || n <= 0 {
		return 0, fmt.Errorf("config: %s harus bilangan positif, sekarang %q", key, v)
	}
	return n, nil
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
