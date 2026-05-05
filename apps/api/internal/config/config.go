// Package config loads runtime configuration from environment variables and
// fails fast on missing required values. Anti-pattern from section 18:
// "embedding model dimension mismatch with database column" — we apply the
// same fail-fast principle here so the service refuses to boot if it is
// misconfigured.
package config

import (
	"errors"
	"fmt"
	"os"
	"strconv"
	"strings"
	"time"
)

type Config struct {
	Env      string
	HTTPAddr string
	LogLevel string

	PostgresURL string
	RedisURL    string
	RabbitMQURL string

	RabbitMQDialTimeout    time.Duration
	RabbitMQPublishTimeout time.Duration

	S3Endpoint        string
	S3Region          string
	S3AccessKeyID     string
	S3SecretAccessKey string
	S3Bucket          string
	S3ForcePathStyle  bool
	S3PresignExpiry   time.Duration

	BetterAuthSecret string
	WebOrigin        string

	// Worker callback (POST /internal/jobs/callback) — bearer + per-job
	// HMAC. ADR-0004.
	JobCallbackToken      string
	JobCallbackHMACSecret string

	OTLPEndpoint string
	ServiceName  string

	RateLimitUserPerMin   int
	RateLimitAPIKeyPerMin int
}

// Load reads required configuration from the environment. It returns the
// joined error of all missing or malformed values so the operator sees the
// full list at once rather than fixing them one at a time.
func Load() (*Config, error) {
	var errs []error

	cfg := &Config{
		Env:                   getEnv("API_ENV", "development"),
		HTTPAddr:              getEnv("API_HTTP_ADDR", ":8080"),
		LogLevel:              getEnv("API_LOG_LEVEL", "info"),
		PostgresURL:           required("POSTGRES_URL", &errs),
		RedisURL:              getEnv("REDIS_URL", "redis://localhost:6379/0"),
		RabbitMQURL:           getEnv("RABBITMQ_URL", "amqp://visionloop:visionloop@localhost:5672/"),
		RabbitMQDialTimeout:   time.Duration(getEnvInt("RABBITMQ_DIAL_TIMEOUT_SECONDS", 10, &errs)) * time.Second,
		RabbitMQPublishTimeout: time.Duration(getEnvInt("RABBITMQ_PUBLISH_TIMEOUT_SECONDS", 5, &errs)) * time.Second,
		S3Endpoint:            getEnv("S3_ENDPOINT", ""),
		S3Region:              getEnv("S3_REGION", "auto"),
		S3AccessKeyID:         required("S3_ACCESS_KEY_ID", &errs),
		S3SecretAccessKey:     required("S3_SECRET_ACCESS_KEY", &errs),
		S3Bucket:              required("S3_BUCKET", &errs),
		S3ForcePathStyle:      getEnvBool("S3_FORCE_PATH_STYLE", true),
		S3PresignExpiry:       time.Duration(getEnvInt("S3_PRESIGN_EXPIRY_SECONDS", 900, &errs)) * time.Second,
		BetterAuthSecret:      required("BETTER_AUTH_SECRET", &errs),
		WebOrigin:             getEnv("WEB_ORIGIN", "http://localhost:3000"),
		JobCallbackToken:      required("JOB_CALLBACK_TOKEN", &errs),
		JobCallbackHMACSecret: required("JOB_CALLBACK_HMAC_SECRET", &errs),
		OTLPEndpoint:          getEnv("OTEL_EXPORTER_OTLP_ENDPOINT", ""),
		ServiceName:           getEnv("OTEL_SERVICE_NAME", "visionloop-api"),
		RateLimitUserPerMin:   getEnvInt("RATE_LIMIT_USER_PER_MIN", 100, &errs),
		RateLimitAPIKeyPerMin: getEnvInt("RATE_LIMIT_API_KEY_PER_MIN", 1000, &errs),
	}

	if len(cfg.BetterAuthSecret) > 0 && len(cfg.BetterAuthSecret) < 32 {
		errs = append(errs, errors.New("BETTER_AUTH_SECRET must be at least 32 bytes"))
	}
	if len(cfg.JobCallbackToken) > 0 && len(cfg.JobCallbackToken) < 32 {
		errs = append(errs, errors.New("JOB_CALLBACK_TOKEN must be at least 32 bytes"))
	}
	if len(cfg.JobCallbackHMACSecret) > 0 && len(cfg.JobCallbackHMACSecret) < 32 {
		errs = append(errs, errors.New("JOB_CALLBACK_HMAC_SECRET must be at least 32 bytes"))
	}

	if len(errs) > 0 {
		return nil, errors.Join(errs...)
	}
	return cfg, nil
}

func required(key string, errs *[]error) string {
	v := os.Getenv(key)
	if strings.TrimSpace(v) == "" {
		*errs = append(*errs, fmt.Errorf("missing required env var %s", key))
	}
	return v
}

func getEnv(key, fallback string) string {
	if v, ok := os.LookupEnv(key); ok && v != "" {
		return v
	}
	return fallback
}

func getEnvBool(key string, fallback bool) bool {
	v, ok := os.LookupEnv(key)
	if !ok {
		return fallback
	}
	switch strings.ToLower(v) {
	case "1", "true", "yes", "on":
		return true
	case "0", "false", "no", "off":
		return false
	default:
		return fallback
	}
}

func getEnvInt(key string, fallback int, errs *[]error) int {
	v, ok := os.LookupEnv(key)
	if !ok || v == "" {
		return fallback
	}
	n, err := strconv.Atoi(v)
	if err != nil {
		*errs = append(*errs, fmt.Errorf("env %s must be an integer: %w", key, err))
		return fallback
	}
	return n
}
