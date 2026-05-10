package storage

import (
	"context"
	"errors"
	"io"
	"strings"
	"testing"
	"time"
)

func TestMemoryRoundTrip(t *testing.T) {
	c := NewMemoryClient("test", Policy{})
	ctx := context.Background()

	body := strings.NewReader("hello")
	obj, err := c.Put(ctx, "k1", body, "text/plain", 5, map[string]string{"x": "1"})
	if err != nil {
		t.Fatal(err)
	}
	if obj.Size != 5 || obj.ContentType != "text/plain" {
		t.Errorf("unexpected obj: %+v", obj)
	}

	r, got, err := c.Get(ctx, "k1")
	if err != nil {
		t.Fatal(err)
	}
	defer r.Close()
	data, _ := io.ReadAll(r)
	if string(data) != "hello" {
		t.Errorf("expected hello, got %q", string(data))
	}
	if got.Metadata["x"] != "1" {
		t.Errorf("metadata not preserved: %+v", got.Metadata)
	}
}

func TestMemoryStatNotFound(t *testing.T) {
	c := NewMemoryClient("test", Policy{})
	if _, err := c.Stat(context.Background(), "missing"); !errors.Is(err, ErrNotFound) {
		t.Errorf("expected ErrNotFound, got %v", err)
	}
}

func TestMemoryDelete(t *testing.T) {
	c := NewMemoryClient("test", Policy{})
	ctx := context.Background()
	_, _ = c.Put(ctx, "k", strings.NewReader("x"), "text/plain", 1, nil)
	if err := c.Delete(ctx, "k"); err != nil {
		t.Fatal(err)
	}
	if _, err := c.Stat(ctx, "k"); !errors.Is(err, ErrNotFound) {
		t.Errorf("expected ErrNotFound after delete, got %v", err)
	}
}

func TestPolicyEnforcesMaxBytes(t *testing.T) {
	c := NewMemoryClient("test", Policy{MaxBytes: 4})
	_, err := c.Put(context.Background(), "k", strings.NewReader("toolong"), "text/plain", 7, nil)
	if !errors.Is(err, ErrPolicyViolation) {
		t.Errorf("expected ErrPolicyViolation, got %v", err)
	}
}

func TestPolicyEnforcesContentTypeAllowlist(t *testing.T) {
	c := NewMemoryClient("test", Policy{AllowedTypes: []string{"image/jpeg", "image/png"}})
	_, err := c.Put(context.Background(), "k", strings.NewReader("x"), "application/pdf", 1, nil)
	if !errors.Is(err, ErrPolicyViolation) {
		t.Errorf("expected ErrPolicyViolation for disallowed type, got %v", err)
	}

	if _, err := c.Put(context.Background(), "k2", strings.NewReader("x"), "image/jpeg", 1, nil); err != nil {
		t.Errorf("expected allowed type to succeed, got %v", err)
	}
}

func TestPolicyWildcardContentType(t *testing.T) {
	c := NewMemoryClient("test", Policy{AllowedTypes: []string{"image/*"}})
	_, err := c.Put(context.Background(), "k", strings.NewReader("x"), "image/webp", 1, nil)
	if err != nil {
		t.Errorf("expected wildcard match, got %v", err)
	}
	_, err = c.Put(context.Background(), "k2", strings.NewReader("x"), "video/mp4", 1, nil)
	if !errors.Is(err, ErrPolicyViolation) {
		t.Errorf("expected wildcard mismatch to fail, got %v", err)
	}
}

func TestPresignedURLs(t *testing.T) {
	c := NewMemoryClient("bucket", Policy{DefaultExpiry: time.Minute})
	url, err := c.PresignedPut(context.Background(), "k", "image/png", 100, 0)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(url, "memory://bucket/k") {
		t.Errorf("unexpected URL: %s", url)
	}
	if !strings.Contains(url, "type=image/png") {
		t.Errorf("expected content-type in URL: %s", url)
	}

	gurl, err := c.PresignedGet(context.Background(), "k", 0)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(gurl, "op=get") {
		t.Errorf("expected get URL marker: %s", gurl)
	}
}

func TestPresignedPutEnforcesPolicy(t *testing.T) {
	c := NewMemoryClient("b", Policy{MaxBytes: 4})
	if _, err := c.PresignedPut(context.Background(), "k", "text/plain", 100, 0); !errors.Is(err, ErrPolicyViolation) {
		t.Errorf("expected ErrPolicyViolation, got %v", err)
	}
}

func TestS3ClientRequiresConfig(t *testing.T) {
	if _, err := NewS3Client(Config{}, Policy{}); err == nil {
		t.Errorf("expected error for empty config")
	}
}
