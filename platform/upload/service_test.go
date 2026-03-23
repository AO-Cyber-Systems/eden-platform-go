package upload

import (
	"testing"
)

func TestAllowedContentTypes(t *testing.T) {
	expected := []string{
		"image/jpeg",
		"image/png",
		"image/gif",
		"image/webp",
		"application/pdf",
		"text/plain",
		"application/zip",
	}

	for _, ct := range expected {
		if !allowedContentTypes[ct] {
			t.Errorf("allowedContentTypes[%q] = false, want true", ct)
		}
	}
}

func TestMaxFileSize(t *testing.T) {
	expected := int64(100 * 1024 * 1024) // 100 MB
	if MaxFileSize != expected {
		t.Errorf("MaxFileSize = %d, want %d", MaxFileSize, expected)
	}
}
