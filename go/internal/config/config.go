// Package config loads runtime settings from environment variables.
// Every setting has a default that works with docker-compose.yml, so the
// server starts locally with no .env at all.
package config

import (
	"errors"
	"fmt"
	"os"
	"strconv"
	"time"
)

type Config struct {
	HTTPAddr        string
	DatabaseURL     string
	ShutdownTimeout time.Duration
	MaxUploadBytes  int64

	MinIO MinIOConfig

	MLServiceAddr string // gRPC address of ml-service, used by the prediction proxy

	SlackWebhookURL   string
	DiscordWebhookURL string
	AlertCooldown     time.Duration

	// APIKey protects /api/v1 when set (X-API-Key or Bearer). Empty = open.
	APIKey string

	PredictionLogBuffer int

	Drift    DriftConfig
	Rollback RollbackConfig
}

type DriftConfig struct {
	Enabled    bool
	Interval   time.Duration
	Window     time.Duration
	MinSamples int
	Workers    int
	FeaturePSI float64
	OverallPSI float64
}

type RollbackConfig struct {
	Auto            bool
	CheckInterval   time.Duration
	Window          time.Duration
	MinRequests     int
	MaxErrorRate    float64
	MaxErrorDelta   float64
	MaxPositiveDiff float64
}

type MinIOConfig struct {
	Endpoint  string
	AccessKey string
	SecretKey string
	Bucket    string
	UseSSL    bool
}

func Load() (Config, error) {
	cfg := Config{
		HTTPAddr:          getEnv("HTTP_ADDR", ":8080"),
		DatabaseURL:       getEnv("DATABASE_URL", "postgres://driftline:driftline@localhost:5432/driftline?sslmode=disable"),
		MLServiceAddr:     getEnv("ML_SERVICE_ADDR", "localhost:50051"),
		SlackWebhookURL:   os.Getenv("SLACK_WEBHOOK_URL"),
		DiscordWebhookURL: os.Getenv("DISCORD_WEBHOOK_URL"),
		MinIO: MinIOConfig{
			Endpoint:  getEnv("MINIO_ENDPOINT", "localhost:9000"),
			AccessKey: getEnv("MINIO_ACCESS_KEY", "minioadmin"),
			SecretKey: getEnv("MINIO_SECRET_KEY", "minioadmin"),
			Bucket:    getEnv("MINIO_BUCKET", "driftline-artifacts"),
		},
	}

	var err error
	if cfg.MinIO.UseSSL, err = getBool("MINIO_USE_SSL", false); err != nil {
		return Config{}, err
	}
	if cfg.ShutdownTimeout, err = getDuration("SHUTDOWN_TIMEOUT", 10*time.Second); err != nil {
		return Config{}, err
	}
	if cfg.AlertCooldown, err = getDuration("ALERT_COOLDOWN", 30*time.Minute); err != nil {
		return Config{}, err
	}
	maxMB, err := getInt("MAX_UPLOAD_MB", 100)
	if err != nil {
		return Config{}, err
	}
	cfg.MaxUploadBytes = int64(maxMB) << 20

	cfg.APIKey = os.Getenv("API_KEY")

	// Collect every parse error instead of stopping at the first, so a bad
	// .env is fixed in one go.
	p := &parser{}
	cfg.PredictionLogBuffer = p.int("PREDICTION_LOG_BUFFER", 10000)
	cfg.Drift = DriftConfig{
		Enabled:    p.bool("DRIFT_ENABLED", true),
		Interval:   p.duration("DRIFT_INTERVAL", time.Minute),
		Window:     p.duration("DRIFT_WINDOW", 15*time.Minute),
		MinSamples: p.int("DRIFT_MIN_SAMPLES", 100),
		Workers:    p.int("DRIFT_WORKERS", 4),
		FeaturePSI: p.float("DRIFT_FEATURE_PSI", 0.25),
		OverallPSI: p.float("DRIFT_OVERALL_PSI", 0.1),
	}
	cfg.Rollback = RollbackConfig{
		Auto:            p.bool("AUTO_ROLLBACK", true),
		CheckInterval:   p.duration("ROLLBACK_CHECK_INTERVAL", 30*time.Second),
		Window:          p.duration("ROLLBACK_WINDOW", 5*time.Minute),
		MinRequests:     p.int("ROLLBACK_MIN_REQUESTS", 50),
		MaxErrorRate:    p.float("ROLLBACK_MAX_ERROR_RATE", 0.05),
		MaxErrorDelta:   p.float("ROLLBACK_MAX_ERROR_DELTA", 0.03),
		MaxPositiveDiff: p.float("ROLLBACK_MAX_POSITIVE_DIFF", 0.25),
	}
	if p.err != nil {
		return Config{}, p.err
	}
	return cfg, nil
}

type parser struct{ err error }

func (p *parser) keep(err error) {
	if err != nil {
		p.err = errors.Join(p.err, err)
	}
}

func (p *parser) int(key string, fb int) int {
	v, err := getInt(key, fb)
	p.keep(err)
	return v
}

func (p *parser) bool(key string, fb bool) bool {
	v, err := getBool(key, fb)
	p.keep(err)
	return v
}

func (p *parser) duration(key string, fb time.Duration) time.Duration {
	v, err := getDuration(key, fb)
	p.keep(err)
	return v
}

func (p *parser) float(key string, fb float64) float64 {
	raw := os.Getenv(key)
	if raw == "" {
		return fb
	}
	v, err := strconv.ParseFloat(raw, 64)
	if err != nil {
		p.keep(fmt.Errorf("config: %s=%q is not a number: %w", key, raw, err))
		return fb
	}
	return v
}

func getEnv(key, fallback string) string {
	if v, ok := os.LookupEnv(key); ok && v != "" {
		return v
	}
	return fallback
}

func getBool(key string, fallback bool) (bool, error) {
	v := os.Getenv(key)
	if v == "" {
		return fallback, nil
	}
	b, err := strconv.ParseBool(v)
	if err != nil {
		return false, fmt.Errorf("config: %s=%q is not a bool: %w", key, v, err)
	}
	return b, nil
}

func getInt(key string, fallback int) (int, error) {
	v := os.Getenv(key)
	if v == "" {
		return fallback, nil
	}
	n, err := strconv.Atoi(v)
	if err != nil {
		return 0, fmt.Errorf("config: %s=%q is not an int: %w", key, v, err)
	}
	return n, nil
}

func getDuration(key string, fallback time.Duration) (time.Duration, error) {
	v := os.Getenv(key)
	if v == "" {
		return fallback, nil
	}
	d, err := time.ParseDuration(v)
	if err != nil {
		return 0, fmt.Errorf("config: %s=%q is not a duration (e.g. 30s, 5m): %w", key, v, err)
	}
	return d, nil
}
