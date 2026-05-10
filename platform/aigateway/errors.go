package aigateway

import "errors"

// Sentinel errors returned by Client methods. Callers should compare via
// errors.Is so wrapped errors continue to match.
var (
	// ErrNotConfigured is returned by NewClient when Config.BaseURL is
	// empty, and by every Client method called on a nil client. Consumers
	// use this to gracefully degrade ("AI unavailable") when the gateway
	// is intentionally not provisioned.
	ErrNotConfigured = errors.New("aigateway: client not configured (BaseURL is empty)")

	// ErrUnauthorized is returned when the upstream gateway responds with
	// HTTP 401. Indicates a missing, invalid, or revoked API key.
	ErrUnauthorized = errors.New("aigateway: unauthorized (401)")

	// ErrBudgetExceeded is returned when the upstream gateway responds
	// with HTTP 402. The API key has hit its budget cap; spend resets per
	// the gateway's billing window.
	ErrBudgetExceeded = errors.New("aigateway: budget exceeded (402)")
)

// HTTPError is the structured error returned for non-retryable HTTP failures
// that don't map to a sentinel above. It carries the status code and the raw
// (truncated) response body for debugging.
type HTTPError struct {
	Status int
	Body   string
	Path   string
}

// Error formats the HTTP error.
func (e *HTTPError) Error() string {
	if e.Path != "" {
		return "aigateway: " + e.Path + " returned status " + itoa(e.Status) + ": " + e.Body
	}
	return "aigateway: status " + itoa(e.Status) + ": " + e.Body
}

// itoa is a tiny strconv.Itoa avoiding the import in the hot error path.
func itoa(i int) string {
	if i == 0 {
		return "0"
	}
	neg := false
	if i < 0 {
		neg = true
		i = -i
	}
	var buf [20]byte
	pos := len(buf)
	for i > 0 {
		pos--
		buf[pos] = byte('0' + i%10)
		i /= 10
	}
	if neg {
		pos--
		buf[pos] = '-'
	}
	return string(buf[pos:])
}
