// Package attachments provides a polymorphic file-attachment abstraction:
// any record (any table) can have files via (owner_type, owner_id).
// Donor: eden-biz/attachments. See TRD 19-02.
package attachments

import (
	"context"
	"errors"
	"fmt"
	"io"
	"sort"
	"sync"
	"time"

	"github.com/aocybersystems/eden-platform-go/platform/storage"
	"github.com/google/uuid"
)

// Owner is the polymorphic association target.
type Owner struct {
	Type string
	ID   string
}

// Attachment is the metadata persisted in the Store.
type Attachment struct {
	ID          string
	Owner       Owner
	StorageKey  string
	Filename    string
	ContentType string
	Size        int64
	UploadedBy  string
	Metadata    map[string]string
	CreatedAt   time.Time
}

// Errors
var (
	ErrNotFound       = errors.New("attachments: not found")
	ErrInvalidOwner   = errors.New("attachments: invalid owner (Type and ID required)")
)

// Store persists Attachment metadata. Consumers wire either MemoryStore or
// PostgresStore.
type Store interface {
	Insert(ctx context.Context, a Attachment) error
	GetByID(ctx context.Context, id string) (Attachment, error)
	ListByOwner(ctx context.Context, owner Owner) ([]Attachment, error)
	Delete(ctx context.Context, id string) error
}

// Service is the consumer-facing API.
type Service struct {
	store   Store
	storage storage.Client
	prefix  string // e.g. "attachments/"
}

// NewService wires a Service. prefix is prepended to generated storage keys
// (default "attachments/").
func NewService(store Store, sc storage.Client, prefix string) *Service {
	if prefix == "" {
		prefix = "attachments/"
	}
	return &Service{store: store, storage: sc, prefix: prefix}
}

// Attach uploads body to storage AND records the attachment.
func (s *Service) Attach(ctx context.Context, owner Owner, body io.Reader, filename, contentType string, size int64, uploadedBy string) (Attachment, error) {
	if owner.Type == "" || owner.ID == "" {
		return Attachment{}, ErrInvalidOwner
	}
	id := uuid.New().String()
	key := fmt.Sprintf("%s%s/%s/%s", s.prefix, owner.Type, owner.ID, id)
	if _, err := s.storage.Put(ctx, key, body, contentType, size, map[string]string{
		"owner_type":  owner.Type,
		"owner_id":    owner.ID,
		"uploaded_by": uploadedBy,
	}); err != nil {
		return Attachment{}, fmt.Errorf("attachments: storage put: %w", err)
	}
	att := Attachment{
		ID:          id,
		Owner:       owner,
		StorageKey:  key,
		Filename:    filename,
		ContentType: contentType,
		Size:        size,
		UploadedBy:  uploadedBy,
		CreatedAt:   time.Now().UTC(),
	}
	if err := s.store.Insert(ctx, att); err != nil {
		// Best-effort cleanup on metadata failure to avoid orphan blobs.
		_ = s.storage.Delete(context.Background(), key)
		return Attachment{}, fmt.Errorf("attachments: store insert: %w", err)
	}
	return att, nil
}

// AttachFromKey records an attachment for an already-uploaded storage key.
// Used when the caller used PresignedPut directly.
func (s *Service) AttachFromKey(ctx context.Context, owner Owner, storageKey, filename, contentType string, size int64, uploadedBy string) (Attachment, error) {
	if owner.Type == "" || owner.ID == "" {
		return Attachment{}, ErrInvalidOwner
	}
	att := Attachment{
		ID:          uuid.New().String(),
		Owner:       owner,
		StorageKey:  storageKey,
		Filename:    filename,
		ContentType: contentType,
		Size:        size,
		UploadedBy:  uploadedBy,
		CreatedAt:   time.Now().UTC(),
	}
	if err := s.store.Insert(ctx, att); err != nil {
		return Attachment{}, fmt.Errorf("attachments: store insert: %w", err)
	}
	return att, nil
}

// List returns attachments for a given owner, oldest first.
func (s *Service) List(ctx context.Context, owner Owner) ([]Attachment, error) {
	if owner.Type == "" || owner.ID == "" {
		return nil, ErrInvalidOwner
	}
	return s.store.ListByOwner(ctx, owner)
}

// Get returns a single attachment by ID.
func (s *Service) Get(ctx context.Context, id string) (Attachment, error) {
	return s.store.GetByID(ctx, id)
}

// Remove deletes both the storage blob and the metadata record.
func (s *Service) Remove(ctx context.Context, id string) error {
	att, err := s.store.GetByID(ctx, id)
	if err != nil {
		return err
	}
	if err := s.storage.Delete(ctx, att.StorageKey); err != nil {
		return fmt.Errorf("attachments: storage delete: %w", err)
	}
	if err := s.store.Delete(ctx, id); err != nil {
		return fmt.Errorf("attachments: store delete: %w", err)
	}
	return nil
}

// PresignedDownload returns a presigned GET URL for the attachment.
func (s *Service) PresignedDownload(ctx context.Context, id string, expiry time.Duration) (string, error) {
	att, err := s.store.GetByID(ctx, id)
	if err != nil {
		return "", err
	}
	return s.storage.PresignedGet(ctx, att.StorageKey, expiry)
}

// MemoryStore is an in-process Store for tests/dev.
type MemoryStore struct {
	mu sync.RWMutex
	m  map[string]Attachment
}

// NewMemoryStore constructs an empty MemoryStore.
func NewMemoryStore() *MemoryStore { return &MemoryStore{m: make(map[string]Attachment)} }

func (s *MemoryStore) Insert(_ context.Context, a Attachment) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.m[a.ID] = a
	return nil
}

func (s *MemoryStore) GetByID(_ context.Context, id string) (Attachment, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	a, ok := s.m[id]
	if !ok {
		return Attachment{}, ErrNotFound
	}
	return a, nil
}

func (s *MemoryStore) ListByOwner(_ context.Context, owner Owner) ([]Attachment, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	out := make([]Attachment, 0)
	for _, a := range s.m {
		if a.Owner == owner {
			out = append(out, a)
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].CreatedAt.Before(out[j].CreatedAt) })
	return out, nil
}

func (s *MemoryStore) Delete(_ context.Context, id string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, ok := s.m[id]; !ok {
		return ErrNotFound
	}
	delete(s.m, id)
	return nil
}
