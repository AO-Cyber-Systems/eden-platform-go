package upload

import (
	"context"
	"time"

	"github.com/google/uuid"
)

// Attachment represents an uploaded file attachment.
type Attachment struct {
	ID          uuid.UUID
	CompanyID   uuid.UUID
	UploaderID  uuid.UUID
	Filename    string
	ContentType string
	SizeBytes   int64
	StorageKey  string
	Status      string
	CreatedAt   time.Time
}

// UploadStore defines database operations for file uploads.
type UploadStore interface {
	CreateAttachment(ctx context.Context, a Attachment) (Attachment, error)
	GetAttachment(ctx context.Context, id uuid.UUID) (Attachment, error)
	UpdateAttachmentStatus(ctx context.Context, id uuid.UUID, status string) (Attachment, error)
}
