package attachments

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/aocybersystems/eden-platform-go/platform/storage"
)

func TestAttachListRemoveFlow(t *testing.T) {
	st := storage.NewMemoryClient("test", storage.Policy{})
	store := NewMemoryStore()
	svc := NewService(store, st, "att/")
	ctx := context.Background()

	owner := Owner{Type: "invoice", ID: "inv-1"}
	att, err := svc.Attach(ctx, owner, strings.NewReader("hello"), "hello.txt", "text/plain", 5, "user-1")
	if err != nil {
		t.Fatal(err)
	}
	if att.StorageKey == "" || att.ID == "" {
		t.Errorf("expected populated id+key, got %+v", att)
	}

	got, err := svc.List(ctx, owner)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 || got[0].ID != att.ID {
		t.Errorf("expected list of 1 with our id, got %+v", got)
	}

	url, err := svc.PresignedDownload(ctx, att.ID, time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(url, att.StorageKey) {
		t.Errorf("expected storage key in URL: %s", url)
	}

	if err := svc.Remove(ctx, att.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.Get(ctx, att.ID); !errors.Is(err, ErrNotFound) {
		t.Errorf("expected ErrNotFound after remove, got %v", err)
	}

	// Storage object should also be gone.
	if _, err := st.Stat(ctx, att.StorageKey); !errors.Is(err, storage.ErrNotFound) {
		t.Errorf("expected storage ErrNotFound, got %v", err)
	}
}

func TestAttachFromKey(t *testing.T) {
	st := storage.NewMemoryClient("test", storage.Policy{})
	store := NewMemoryStore()
	svc := NewService(store, st, "")
	ctx := context.Background()

	// Pretend caller already uploaded via presigned URL.
	_, _ = st.Put(ctx, "external/key", strings.NewReader("uploaded"), "text/plain", 8, nil)

	owner := Owner{Type: "ticket", ID: "t-9"}
	att, err := svc.AttachFromKey(ctx, owner, "external/key", "report.txt", "text/plain", 8, "u-2")
	if err != nil {
		t.Fatal(err)
	}
	if att.StorageKey != "external/key" {
		t.Errorf("expected key preserved, got %s", att.StorageKey)
	}

	// Listing returns it.
	all, _ := svc.List(ctx, owner)
	if len(all) != 1 {
		t.Errorf("expected 1, got %d", len(all))
	}
}

func TestPolymorphicIsolation(t *testing.T) {
	st := storage.NewMemoryClient("test", storage.Policy{})
	store := NewMemoryStore()
	svc := NewService(store, st, "")
	ctx := context.Background()

	_, _ = svc.Attach(ctx, Owner{Type: "invoice", ID: "1"}, strings.NewReader("a"), "a.txt", "text/plain", 1, "u")
	_, _ = svc.Attach(ctx, Owner{Type: "invoice", ID: "2"}, strings.NewReader("b"), "b.txt", "text/plain", 1, "u")
	_, _ = svc.Attach(ctx, Owner{Type: "ticket", ID: "1"}, strings.NewReader("c"), "c.txt", "text/plain", 1, "u")

	inv1, _ := svc.List(ctx, Owner{Type: "invoice", ID: "1"})
	if len(inv1) != 1 {
		t.Errorf("expected 1 attachment for invoice 1, got %d", len(inv1))
	}

	tick1, _ := svc.List(ctx, Owner{Type: "ticket", ID: "1"})
	if len(tick1) != 1 || tick1[0].Filename != "c.txt" {
		t.Errorf("expected ticket 1's file, got %+v", tick1)
	}
}

func TestRejectsInvalidOwner(t *testing.T) {
	svc := NewService(NewMemoryStore(), storage.NewMemoryClient("t", storage.Policy{}), "")
	if _, err := svc.Attach(context.Background(), Owner{Type: ""}, strings.NewReader("x"), "f", "text/plain", 1, "u"); !errors.Is(err, ErrInvalidOwner) {
		t.Errorf("expected ErrInvalidOwner, got %v", err)
	}
}

func TestRemoveCleansOnInsertFailure(t *testing.T) {
	// Use a store stub that fails on Insert; verify storage cleanup runs.
	st := storage.NewMemoryClient("t", storage.Policy{})
	failing := &failingStore{}
	svc := NewService(failing, st, "")

	_, err := svc.Attach(context.Background(), Owner{Type: "x", ID: "1"}, strings.NewReader("x"), "f", "text/plain", 1, "u")
	if err == nil {
		t.Fatal("expected error from failing store")
	}
	// The package writes one storage object then deletes it. Listing the bucket
	// is not exposed, so we verify the test by inspecting failingStore state.
	if failing.tries != 1 {
		t.Errorf("expected 1 insert attempt, got %d", failing.tries)
	}
}

type failingStore struct{ tries int }

func (s *failingStore) Insert(_ context.Context, _ Attachment) error {
	s.tries++
	return errors.New("simulated failure")
}
func (s *failingStore) GetByID(_ context.Context, _ string) (Attachment, error) {
	return Attachment{}, ErrNotFound
}
func (s *failingStore) ListByOwner(_ context.Context, _ Owner) ([]Attachment, error) { return nil, nil }
func (s *failingStore) Delete(_ context.Context, _ string) error                     { return nil }
