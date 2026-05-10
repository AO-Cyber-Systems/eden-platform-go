package httputil

import (
	"net/http/httptest"
	"testing"
)

func TestParsePagination_Defaults(t *testing.T) {
	r := httptest.NewRequest("GET", "/", nil)
	p := ParsePagination(r)
	if p.Limit != DefaultLimit {
		t.Fatalf("Limit = %d, want %d", p.Limit, DefaultLimit)
	}
	if p.Offset != 0 {
		t.Fatalf("Offset = %d, want 0", p.Offset)
	}
}

func TestParsePagination_ClampsLimit(t *testing.T) {
	r := httptest.NewRequest("GET", "/?limit=99999", nil)
	p := ParsePagination(r)
	if p.Limit != MaxLimit {
		t.Fatalf("oversize limit Limit = %d, want %d", p.Limit, MaxLimit)
	}

	r = httptest.NewRequest("GET", "/?limit=-3", nil)
	p = ParsePagination(r)
	if p.Limit != DefaultLimit {
		t.Fatalf("negative limit Limit = %d, want %d", p.Limit, DefaultLimit)
	}
}

func TestParsePagination_ClampsOffset(t *testing.T) {
	r := httptest.NewRequest("GET", "/?offset=-5", nil)
	p := ParsePagination(r)
	if p.Offset != 0 {
		t.Fatalf("negative offset = %d, want 0", p.Offset)
	}
}

func TestParsePagination_NonNumericFallsBackToDefault(t *testing.T) {
	r := httptest.NewRequest("GET", "/?limit=abc&offset=xyz", nil)
	p := ParsePagination(r)
	if p.Limit != DefaultLimit {
		t.Fatalf("garbage limit Limit = %d, want %d", p.Limit, DefaultLimit)
	}
	if p.Offset != 0 {
		t.Fatalf("garbage offset Offset = %d, want 0", p.Offset)
	}
}

func TestParsePage_OneIndexed(t *testing.T) {
	r := httptest.NewRequest("GET", "/?page=3&page_size=20", nil)
	p := ParsePage(r)
	if p.Limit != 20 {
		t.Fatalf("Limit = %d, want 20", p.Limit)
	}
	if p.Offset != 40 { // (3 - 1) * 20
		t.Fatalf("Offset = %d, want 40", p.Offset)
	}
}

func TestParsePage_PageBelowOneClampsToOne(t *testing.T) {
	r := httptest.NewRequest("GET", "/?page=0&page_size=10", nil)
	p := ParsePage(r)
	if p.Offset != 0 {
		t.Fatalf("Offset = %d, want 0 (page=0 clamps to 1)", p.Offset)
	}
}

func TestParsePage_PageSizeClamps(t *testing.T) {
	r := httptest.NewRequest("GET", "/?page=1&page_size=99999", nil)
	p := ParsePage(r)
	if p.Limit != MaxLimit {
		t.Fatalf("oversize page_size Limit = %d, want %d", p.Limit, MaxLimit)
	}

	r = httptest.NewRequest("GET", "/?page=1&page_size=0", nil)
	p = ParsePage(r)
	if p.Limit != DefaultLimit {
		t.Fatalf("zero page_size Limit = %d, want %d", p.Limit, DefaultLimit)
	}
}
