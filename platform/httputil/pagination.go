package httputil

import (
	"net/http"
	"strconv"
)

// Pagination defaults. Override via the offset/limit (or page/page_size) query
// params; values out of range are clamped, not rejected.
const (
	DefaultLimit = 50
	MaxLimit     = 500
)

// Pagination holds an absolute Limit and Offset (both int32 to align with
// sqlc's generated query parameter types).
type Pagination struct {
	Limit  int32
	Offset int32
}

// ParsePagination reads ?limit=&offset= and clamps:
//   - limit ≤ 0 → DefaultLimit
//   - limit > MaxLimit → MaxLimit
//   - offset < 0 → 0
//
// Missing or non-numeric values fall back to defaults.
func ParsePagination(r *http.Request) Pagination {
	limit := parseIntParam(r, "limit", DefaultLimit)
	offset := parseIntParam(r, "offset", 0)

	if limit <= 0 {
		limit = DefaultLimit
	}
	if limit > MaxLimit {
		limit = MaxLimit
	}
	if offset < 0 {
		offset = 0
	}

	return Pagination{Limit: int32(limit), Offset: int32(offset)}
}

// ParsePage reads ?page=&page_size= and converts to limit/offset using the
// same clamping rules as ParsePagination. Page is 1-indexed; page < 1 is
// treated as page 1.
func ParsePage(r *http.Request) Pagination {
	page := parseIntParam(r, "page", 1)
	pageSize := parseIntParam(r, "page_size", DefaultLimit)

	if page < 1 {
		page = 1
	}
	if pageSize <= 0 {
		pageSize = DefaultLimit
	}
	if pageSize > MaxLimit {
		pageSize = MaxLimit
	}

	return Pagination{
		Limit:  int32(pageSize),
		Offset: int32((page - 1) * pageSize),
	}
}

func parseIntParam(r *http.Request, key string, defaultVal int) int {
	v := r.URL.Query().Get(key)
	if v == "" {
		return defaultVal
	}
	n, err := strconv.Atoi(v)
	if err != nil {
		return defaultVal
	}
	return n
}
