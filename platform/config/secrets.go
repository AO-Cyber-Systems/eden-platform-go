package config

import (
	"encoding/base64"
	"log/slog"
	"os"
	"strings"
)

// getSecretFor is the implementation backing GetSecret / GetSecretFor.
// Resolution order:
//
//  1. <key>__<env>     (env-specific override; only if env != "")
//  2. <key>            (env var literal)
//  3. <key>_FILE       (Docker secrets file)
//  4. <key>_BASE64     (base64-encoded value)
//  5. fallback
//
// On _FILE read or _BASE64 decode failure, the function logs a warning and
// falls through to the next strategy so partial misconfiguration doesn't
// silently swap in an empty string.
func getSecretFor(key, fallback, env string) string {
	if env != "" {
		if v := os.Getenv(key + "__" + env); v != "" {
			return v
		}
	}
	if v := os.Getenv(key); v != "" {
		return v
	}
	if filePath := os.Getenv(key + "_FILE"); filePath != "" {
		data, err := os.ReadFile(filePath)
		if err != nil {
			slog.Warn("config: failed to read secret file", "key", key, "file", filePath, "error", err)
		} else {
			return strings.TrimSpace(string(data))
		}
	}
	if b64 := os.Getenv(key + "_BASE64"); b64 != "" {
		decoded, err := base64.StdEncoding.DecodeString(b64)
		if err != nil {
			// Try URL-safe and raw variants before giving up.
			for _, dec := range []*base64.Encoding{base64.URLEncoding, base64.RawStdEncoding, base64.RawURLEncoding} {
				if d, e := dec.DecodeString(b64); e == nil {
					return string(d)
				}
			}
			slog.Warn("config: invalid base64 secret", "key", key+"_BASE64", "error", err)
		} else {
			return string(decoded)
		}
	}
	return fallback
}
