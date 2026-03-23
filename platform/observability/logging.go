package observability

import (
	"log/slog"
	"os"
	"strings"
)

// InitLogging configures the default slog logger.
// level and format can be empty strings to use environment variables
// LOG_LEVEL (default "info") and LOG_FORMAT (default "text").
func InitLogging(level, format string) {
	if level == "" {
		level = envOrDefault("LOG_LEVEL", "info")
	}
	if format == "" {
		format = envOrDefault("LOG_FORMAT", "text")
	}

	slogLevel := parseLevel(level)

	opts := &slog.HandlerOptions{Level: slogLevel}

	var handler slog.Handler
	switch strings.ToLower(format) {
	case "json":
		handler = slog.NewJSONHandler(os.Stdout, opts)
	default:
		handler = slog.NewTextHandler(os.Stdout, opts)
	}

	slog.SetDefault(slog.New(handler))
}

func parseLevel(level string) slog.Level {
	switch strings.ToLower(level) {
	case "debug":
		return slog.LevelDebug
	case "warn", "warning":
		return slog.LevelWarn
	case "error":
		return slog.LevelError
	default:
		return slog.LevelInfo
	}
}

func envOrDefault(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}
