// Package storage provides an S3-compatible object-store abstraction with
// presigned URLs and per-bucket policy enforcement (size + MIME caps).
//
// Donor: eden-biz/storage. Uses minio-go (already a transitive dep via
// platform/upload) so adding storage doesn't drag in aws-sdk-go-v2.
//
// See TRD 19-01.
package storage

import (
	"context"
	"errors"
	"fmt"
	"io"
	"strings"
	"sync"
	"time"

	"github.com/minio/minio-go/v7"
	"github.com/minio/minio-go/v7/pkg/credentials"
)

// DefaultMaxBytes is the size cap applied when Policy.MaxBytes == 0.
// Matches platform/upload's pre-existing 100MB ceiling.
const DefaultMaxBytes int64 = 100 * 1024 * 1024

// Object is the metadata for a single stored object.
type Object struct {
	Key          string
	ContentType  string
	Size         int64
	ETag         string
	Metadata     map[string]string
	LastModified time.Time
}

// Policy applies per-bucket size + MIME constraints.
type Policy struct {
	MaxBytes      int64         // 0 = use DefaultMaxBytes
	AllowedTypes  []string      // exact ("image/jpeg") or prefix wildcards ("image/*"); empty = allow any
	DefaultExpiry time.Duration // for presigned URLs; 0 = 15min
}

// Errors
var (
	ErrPolicyViolation = errors.New("storage: policy violation")
	ErrNotFound        = errors.New("storage: not found")
)

// Client is the portable storage contract.
type Client interface {
	Put(ctx context.Context, key string, body io.Reader, contentType string, size int64, metadata map[string]string) (Object, error)
	Get(ctx context.Context, key string) (io.ReadCloser, Object, error)
	Delete(ctx context.Context, key string) error
	Stat(ctx context.Context, key string) (Object, error)
	PresignedPut(ctx context.Context, key, contentType string, size int64, expiry time.Duration) (string, error)
	PresignedGet(ctx context.Context, key string, expiry time.Duration) (string, error)
}

// Config configures the S3-compatible client.
type Config struct {
	Endpoint     string // e.g. "s3.amazonaws.com" or "minio:9000"
	AccessKey    string
	SecretKey    string
	UseSSL       bool
	Region       string
	Bucket       string
	UsePathStyle bool   // MinIO / R2: true; AWS S3: false
}

// s3Client is the production minio-go-backed implementation.
type s3Client struct {
	mc     *minio.Client
	bucket string
	policy Policy
}

// NewS3Client constructs an S3-compatible Client. The bucket is created if
// it doesn't exist (best-effort — fails open if Make permissions missing).
func NewS3Client(cfg Config, policy Policy) (Client, error) {
	if cfg.Endpoint == "" || cfg.Bucket == "" {
		return nil, fmt.Errorf("storage: endpoint and bucket are required")
	}
	mc, err := minio.New(cfg.Endpoint, &minio.Options{
		Creds:  credentials.NewStaticV4(cfg.AccessKey, cfg.SecretKey, ""),
		Secure: cfg.UseSSL,
		Region: cfg.Region,
	})
	if err != nil {
		return nil, fmt.Errorf("storage: minio client: %w", err)
	}
	return &s3Client{mc: mc, bucket: cfg.Bucket, policy: policy}, nil
}

func (c *s3Client) Put(ctx context.Context, key string, body io.Reader, contentType string, size int64, metadata map[string]string) (Object, error) {
	if err := c.policy.check(contentType, size); err != nil {
		return Object{}, err
	}
	info, err := c.mc.PutObject(ctx, c.bucket, key, body, size, minio.PutObjectOptions{
		ContentType:  contentType,
		UserMetadata: metadata,
	})
	if err != nil {
		return Object{}, fmt.Errorf("storage: put %q: %w", key, err)
	}
	return Object{
		Key:          key,
		ContentType:  contentType,
		Size:         info.Size,
		ETag:         info.ETag,
		Metadata:     metadata,
		LastModified: time.Now().UTC(),
	}, nil
}

func (c *s3Client) Get(ctx context.Context, key string) (io.ReadCloser, Object, error) {
	obj, err := c.mc.GetObject(ctx, c.bucket, key, minio.GetObjectOptions{})
	if err != nil {
		return nil, Object{}, fmt.Errorf("storage: get %q: %w", key, err)
	}
	stat, err := obj.Stat()
	if err != nil {
		_ = obj.Close()
		if minio.ToErrorResponse(err).Code == "NoSuchKey" {
			return nil, Object{}, ErrNotFound
		}
		return nil, Object{}, fmt.Errorf("storage: stat %q: %w", key, err)
	}
	return obj, Object{
		Key:          key,
		ContentType:  stat.ContentType,
		Size:         stat.Size,
		ETag:         stat.ETag,
		Metadata:     stat.UserMetadata,
		LastModified: stat.LastModified,
	}, nil
}

func (c *s3Client) Delete(ctx context.Context, key string) error {
	if err := c.mc.RemoveObject(ctx, c.bucket, key, minio.RemoveObjectOptions{}); err != nil {
		return fmt.Errorf("storage: delete %q: %w", key, err)
	}
	return nil
}

func (c *s3Client) Stat(ctx context.Context, key string) (Object, error) {
	stat, err := c.mc.StatObject(ctx, c.bucket, key, minio.StatObjectOptions{})
	if err != nil {
		if minio.ToErrorResponse(err).Code == "NoSuchKey" {
			return Object{}, ErrNotFound
		}
		return Object{}, fmt.Errorf("storage: stat %q: %w", key, err)
	}
	return Object{
		Key:          key,
		ContentType:  stat.ContentType,
		Size:         stat.Size,
		ETag:         stat.ETag,
		Metadata:     stat.UserMetadata,
		LastModified: stat.LastModified,
	}, nil
}

func (c *s3Client) PresignedPut(ctx context.Context, key, contentType string, size int64, expiry time.Duration) (string, error) {
	if err := c.policy.check(contentType, size); err != nil {
		return "", err
	}
	if expiry == 0 {
		expiry = c.policy.expiry()
	}
	u, err := c.mc.PresignedPutObject(ctx, c.bucket, key, expiry)
	if err != nil {
		return "", fmt.Errorf("storage: presigned put: %w", err)
	}
	return u.String(), nil
}

func (c *s3Client) PresignedGet(ctx context.Context, key string, expiry time.Duration) (string, error) {
	if expiry == 0 {
		expiry = c.policy.expiry()
	}
	u, err := c.mc.PresignedGetObject(ctx, c.bucket, key, expiry, nil)
	if err != nil {
		return "", fmt.Errorf("storage: presigned get: %w", err)
	}
	return u.String(), nil
}

// memoryClient is an in-memory Client for tests/dev. Presigned URLs are
// opaque tokens shaped like "memory://<bucket>/<key>?expires=<unix>".
type memoryClient struct {
	mu     sync.RWMutex
	bucket string
	policy Policy
	store  map[string]storedObj
}

type storedObj struct {
	data    []byte
	obj     Object
}

// NewMemoryClient constructs an in-memory client.
func NewMemoryClient(bucket string, policy Policy) Client {
	if bucket == "" {
		bucket = "memory"
	}
	return &memoryClient{bucket: bucket, policy: policy, store: make(map[string]storedObj)}
}

func (m *memoryClient) Put(_ context.Context, key string, body io.Reader, contentType string, size int64, metadata map[string]string) (Object, error) {
	if err := m.policy.check(contentType, size); err != nil {
		return Object{}, err
	}
	data, err := io.ReadAll(body)
	if err != nil {
		return Object{}, fmt.Errorf("storage: read body: %w", err)
	}
	if int64(len(data)) > size && size > 0 {
		// allow under-read but flag overshoot
		return Object{}, fmt.Errorf("storage: body larger than declared size")
	}
	obj := Object{
		Key:          key,
		ContentType:  contentType,
		Size:         int64(len(data)),
		ETag:         fmt.Sprintf("%x", len(data)),
		Metadata:     copyMap(metadata),
		LastModified: time.Now().UTC(),
	}
	m.mu.Lock()
	m.store[key] = storedObj{data: data, obj: obj}
	m.mu.Unlock()
	return obj, nil
}

func (m *memoryClient) Get(_ context.Context, key string) (io.ReadCloser, Object, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	s, ok := m.store[key]
	if !ok {
		return nil, Object{}, ErrNotFound
	}
	return io.NopCloser(strings.NewReader(string(s.data))), s.obj, nil
}

func (m *memoryClient) Delete(_ context.Context, key string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	delete(m.store, key)
	return nil
}

func (m *memoryClient) Stat(_ context.Context, key string) (Object, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	s, ok := m.store[key]
	if !ok {
		return Object{}, ErrNotFound
	}
	return s.obj, nil
}

func (m *memoryClient) PresignedPut(_ context.Context, key, contentType string, size int64, expiry time.Duration) (string, error) {
	if err := m.policy.check(contentType, size); err != nil {
		return "", err
	}
	if expiry == 0 {
		expiry = m.policy.expiry()
	}
	return fmt.Sprintf("memory://%s/%s?expires=%d&type=%s", m.bucket, key, time.Now().Add(expiry).Unix(), contentType), nil
}

func (m *memoryClient) PresignedGet(_ context.Context, key string, expiry time.Duration) (string, error) {
	if expiry == 0 {
		expiry = m.policy.expiry()
	}
	return fmt.Sprintf("memory://%s/%s?expires=%d&op=get", m.bucket, key, time.Now().Add(expiry).Unix()), nil
}

// check enforces the Policy. Returns ErrPolicyViolation with context.
func (p Policy) check(contentType string, size int64) error {
	max := p.MaxBytes
	if max == 0 {
		max = DefaultMaxBytes
	}
	if size > max {
		return fmt.Errorf("%w: size %d exceeds max %d", ErrPolicyViolation, size, max)
	}
	if len(p.AllowedTypes) == 0 {
		return nil
	}
	for _, allowed := range p.AllowedTypes {
		if matchContentType(allowed, contentType) {
			return nil
		}
	}
	return fmt.Errorf("%w: content-type %q not in allowlist", ErrPolicyViolation, contentType)
}

func (p Policy) expiry() time.Duration {
	if p.DefaultExpiry > 0 {
		return p.DefaultExpiry
	}
	return 15 * time.Minute
}

// matchContentType supports exact match and prefix wildcards ("image/*").
func matchContentType(pattern, ct string) bool {
	if pattern == ct {
		return true
	}
	if strings.HasSuffix(pattern, "/*") {
		prefix := strings.TrimSuffix(pattern, "/*")
		if i := strings.IndexByte(ct, '/'); i > 0 && ct[:i] == prefix {
			return true
		}
	}
	return false
}

func copyMap(in map[string]string) map[string]string {
	if in == nil {
		return nil
	}
	out := make(map[string]string, len(in))
	for k, v := range in {
		out[k] = v
	}
	return out
}
