package config

import (
	"errors"
	"fmt"
	"log/slog"
	"os"
	"strconv"
	"strings"
	"time"
)

// PlatformConfig holds platform-level configuration.
type PlatformConfig struct {
	DatabaseURL    string
	JWTKeySeedPath string
	ServerAddr     string
	NatsURL        string
	MinIOEndpoint  string
	MinIOAccessKey string
	MinIOSecretKey string
	MinIOBucket    string
	MinIORegion    string
	MinIOUseSSL    bool
	RedisURL       string
	RedisPassword  string
	PlatformMode   string // "b2b" (default) or "b2c"
}

// IsB2C returns true when the platform is configured for individual users (no company concept).
func (c *PlatformConfig) IsB2C() bool {
	return c.PlatformMode == "b2c"
}

// Validate checks required fields and returns an aggregated error listing
// every missing or invalid value. DatabaseURL is required. PlatformMode must
// be "b2b" or "b2c". All other fields have sensible defaults from Load().
func (c *PlatformConfig) Validate() error {
	var missing []string
	if c.DatabaseURL == "" {
		missing = append(missing, "DATABASE_URL")
	}
	if c.PlatformMode != "" && c.PlatformMode != "b2b" && c.PlatformMode != "b2c" {
		missing = append(missing, fmt.Sprintf("PLATFORM_MODE=%q (must be b2b or b2c)", c.PlatformMode))
	}
	if len(missing) == 0 {
		return nil
	}
	return fmt.Errorf("config: missing or invalid: %s", strings.Join(missing, ", "))
}

// Load reads platform configuration from environment variables. Equivalent to
// LoadFor("") — no environment-specific overrides applied.
func Load() *PlatformConfig {
	return LoadFor("")
}

// LoadFor reads platform configuration applying any environment-specific
// overrides. For each PlatformConfig field, the resolution order is:
//
//  1. <KEY>__<env>      (highest precedence; e.g. DATABASE_URL__prod)
//  2. <KEY>             (standard env var)
//  3. <KEY>_FILE        (Docker-secrets convention)
//  4. <KEY>_BASE64      (base64-encoded secrets convention)
//  5. compiled-in default
//
// If `env` is empty, EDEN_ENV is consulted; if that too is empty, no
// per-environment lookup is performed (only steps 2-5 apply).
func LoadFor(env string) *PlatformConfig {
	if env == "" {
		env = os.Getenv("EDEN_ENV")
	}
	get := func(key, fallback string) string {
		return getEnvFor(key, fallback, env)
	}
	getSec := func(key, fallback string) string {
		return getSecretFor(key, fallback, env)
	}
	return &PlatformConfig{
		DatabaseURL:    get("DATABASE_URL", "postgres://localhost:5432/eden_dev?sslmode=disable"),
		JWTKeySeedPath: get("JWT_KEY_SEED_PATH", ""),
		ServerAddr:     get("SERVER_ADDR", ":8080"),
		NatsURL:        get("NATS_URL", "nats://localhost:4222"),
		MinIOEndpoint:  get("MINIO_ENDPOINT", "localhost:9000"),
		MinIOAccessKey: getSec("MINIO_ACCESS_KEY", "minioadmin"),
		MinIOSecretKey: getSec("MINIO_SECRET_KEY", "minioadmin"),
		MinIOBucket:    get("MINIO_BUCKET", "eden-platform"),
		MinIORegion:    get("MINIO_REGION", "us-east-1"),
		MinIOUseSSL:    GetBoolFor("MINIO_USE_SSL", false, env),
		RedisURL:       get("REDIS_URL", "localhost:6379"),
		RedisPassword:  getSec("REDIS_PASSWORD", ""),
		PlatformMode:   get("PLATFORM_MODE", "b2b"),
	}
}

// GetEnv reads an environment variable with a fallback default. Empty values
// fall back to the default. Equivalent to GetEnvFor(key, fallback, "").
func GetEnv(key, fallback string) string {
	return getEnvFor(key, fallback, "")
}

// GetEnvFor is the env-aware sibling of GetEnv. Looks up `<key>__<env>`
// before `<key>`. An empty `env` falls back to GetEnv behavior.
func GetEnvFor(key, fallback, env string) string {
	return getEnvFor(key, fallback, env)
}

func getEnvFor(key, fallback, env string) string {
	if env != "" {
		if v := os.Getenv(key + "__" + env); v != "" {
			return v
		}
	}
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}

// GetSecret reads a secret with the following resolution order:
//
//  1. <KEY>__<env>     (env-specific value; deferred to GetSecretFor)
//  2. <KEY>            (env var literal)
//  3. <KEY>_FILE       (Docker secrets — file contents)
//  4. <KEY>_BASE64     (base64-encoded value)
//  5. fallback
//
// Equivalent to GetSecretFor(key, fallback, "").
func GetSecret(key, fallback string) string {
	return getSecretFor(key, fallback, "")
}

// GetSecretFor is the env-aware sibling of GetSecret.
func GetSecretFor(key, fallback, env string) string {
	return getSecretFor(key, fallback, env)
}

// GetInt reads an integer env var. Empty / unparseable values return fallback.
func GetInt(key string, fallback int) int {
	return GetIntFor(key, fallback, "")
}

// GetIntFor is the env-aware sibling of GetInt.
func GetIntFor(key string, fallback int, env string) int {
	v := getEnvFor(key, "", env)
	if v == "" {
		return fallback
	}
	n, err := strconv.Atoi(v)
	if err != nil {
		slog.Warn("config: invalid integer", "key", key, "value", v, "error", err)
		return fallback
	}
	return n
}

// GetBool reads a boolean env var. Accepts strconv.ParseBool's set
// (1, t, T, TRUE, true, True, 0, f, F, FALSE, false, False). Empty /
// unparseable values return fallback.
func GetBool(key string, fallback bool) bool {
	return GetBoolFor(key, fallback, "")
}

// GetBoolFor is the env-aware sibling of GetBool.
func GetBoolFor(key string, fallback bool, env string) bool {
	v := getEnvFor(key, "", env)
	if v == "" {
		return fallback
	}
	b, err := strconv.ParseBool(v)
	if err != nil {
		slog.Warn("config: invalid bool", "key", key, "value", v, "error", err)
		return fallback
	}
	return b
}

// GetDuration reads a duration env var (Go duration syntax: "5s", "1h30m").
// Empty / unparseable values return fallback.
func GetDuration(key string, fallback time.Duration) time.Duration {
	return GetDurationFor(key, fallback, "")
}

// GetDurationFor is the env-aware sibling of GetDuration.
func GetDurationFor(key string, fallback time.Duration, env string) time.Duration {
	v := getEnvFor(key, "", env)
	if v == "" {
		return fallback
	}
	d, err := time.ParseDuration(v)
	if err != nil {
		slog.Warn("config: invalid duration", "key", key, "value", v, "error", err)
		return fallback
	}
	return d
}

// MustGet returns the value of `key`. If it is unset or empty, MustGet
// panics. Use only at boot time for values without a sensible default.
func MustGet(key string) string {
	v := os.Getenv(key)
	if v == "" {
		panic(fmt.Sprintf("config: required env var %q is not set", key))
	}
	return v
}

// Required returns nil if every named env var is set to a non-empty value,
// otherwise an aggregated error listing every missing key.
func Required(keys ...string) error {
	var missing []string
	for _, k := range keys {
		if os.Getenv(k) == "" {
			missing = append(missing, k)
		}
	}
	if len(missing) == 0 {
		return nil
	}
	return errors.New("config: required env vars not set: " + strings.Join(missing, ", "))
}
