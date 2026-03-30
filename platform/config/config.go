package config

import (
	"log/slog"
	"os"
	"strings"
)

// PlatformConfig holds platform-level configuration.
type PlatformConfig struct {
	DatabaseURL       string
	JWTPrivateKeyPath string
	JWTPublicKeyPath  string
	ServerAddr        string
	NatsURL           string
	MinIOEndpoint     string
	MinIOAccessKey    string
	MinIOSecretKey    string
	MinIOBucket       string
	MinIORegion       string
	MinIOUseSSL       bool
	RedisURL          string
	RedisPassword     string
	PlatformMode      string // "b2b" (default) or "b2c"
}

// IsB2C returns true when the platform is configured for individual users (no company concept).
func (c *PlatformConfig) IsB2C() bool {
	return c.PlatformMode == "b2c"
}

// Load reads platform configuration from environment variables.
func Load() *PlatformConfig {
	return &PlatformConfig{
		DatabaseURL:       GetEnv("DATABASE_URL", "postgres://localhost:5432/eden_dev?sslmode=disable"),
		JWTPrivateKeyPath: GetEnv("JWT_PRIVATE_KEY_PATH", ""),
		JWTPublicKeyPath:  GetEnv("JWT_PUBLIC_KEY_PATH", ""),
		ServerAddr:        GetEnv("SERVER_ADDR", ":8080"),
		NatsURL:           GetEnv("NATS_URL", "nats://localhost:4222"),
		MinIOEndpoint:     GetEnv("MINIO_ENDPOINT", "localhost:9000"),
		MinIOAccessKey:    GetSecret("MINIO_ACCESS_KEY", "minioadmin"),
		MinIOSecretKey:    GetSecret("MINIO_SECRET_KEY", "minioadmin"),
		MinIOBucket:       GetEnv("MINIO_BUCKET", "eden-platform"),
		MinIORegion:       GetEnv("MINIO_REGION", "us-east-1"),
		MinIOUseSSL:       GetEnv("MINIO_USE_SSL", "false") == "true",
		RedisURL:          GetEnv("REDIS_URL", "localhost:6379"),
		RedisPassword:     GetSecret("REDIS_PASSWORD", ""),
		PlatformMode:      GetEnv("PLATFORM_MODE", "b2b"),
	}
}

// GetEnv reads an environment variable with a fallback default.
func GetEnv(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}

// GetSecret checks for a _FILE env var (Docker secrets convention).
func GetSecret(key, fallback string) string {
	if filePath := os.Getenv(key + "_FILE"); filePath != "" {
		data, err := os.ReadFile(filePath)
		if err != nil {
			slog.Warn("failed to read secret file", "key", key, "file", filePath, "error", err)
			return GetEnv(key, fallback)
		}
		return strings.TrimSpace(string(data))
	}
	return GetEnv(key, fallback)
}
