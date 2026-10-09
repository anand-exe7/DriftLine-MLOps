// Package config loads runtime settings from environment variables.
// Every setting has a default that works with docker-compose.yml, so the
// server starts locally with no .env at all.
package config

import (
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

	return cfg, nil
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
