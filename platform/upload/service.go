package upload

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	"github.com/google/uuid"
	"github.com/minio/minio-go/v7"
)

// MaxFileSize is the maximum allowed upload size (100 MB).
const MaxFileSize int64 = 100 * 1024 * 1024

var allowedContentTypes = map[string]bool{
	"image/jpeg":       true,
	"image/png":        true,
	"image/gif":        true,
	"image/webp":       true,
	"application/pdf":  true,
	"text/plain":       true,
	"application/zip":  true,
	"application/msword": true,
	"application/vnd.openxmlformats-officedocument.wordprocessingml.document": true,
	"application/vnd.ms-excel": true,
	"application/vnd.openxmlformats-officedocument.spreadsheetml.sheet": true,
}

// Service handles file upload operations with MinIO.
type Service struct {
	store  UploadStore
	minio  *minio.Client
	bucket string
}

// NewService creates a new upload service.
func NewService(store UploadStore, minioClient *minio.Client, bucket string) (*Service, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	exists, err := minioClient.BucketExists(ctx, bucket)
	if err != nil {
		return nil, fmt.Errorf("check bucket: %w", err)
	}
	if !exists {
		if err := minioClient.MakeBucket(ctx, bucket, minio.MakeBucketOptions{}); err != nil {
			return nil, fmt.Errorf("create bucket %q: %w", bucket, err)
		}
		slog.Info("created MinIO bucket", "bucket", bucket)
	}

	return &Service{store: store, minio: minioClient, bucket: bucket}, nil
}

// RequestUploadURL creates a pending attachment and returns a presigned PUT URL.
func (s *Service) RequestUploadURL(ctx context.Context, companyID, uploaderID uuid.UUID, filename, contentType string, sizeBytes int64) (uploadURL, attachmentID string, err error) {
	if sizeBytes <= 0 || sizeBytes > MaxFileSize {
		return "", "", fmt.Errorf("file size must be between 1 byte and %d bytes", MaxFileSize)
	}
	if !allowedContentTypes[contentType] {
		return "", "", fmt.Errorf("content type %q is not allowed", contentType)
	}

	storageKey := fmt.Sprintf("uploads/%s/%s/%s", companyID, uuid.New().String(), filename)

	attachment, err := s.store.CreateAttachment(ctx, Attachment{
		ID:          uuid.New(),
		CompanyID:   companyID,
		UploaderID:  uploaderID,
		Filename:    filename,
		ContentType: contentType,
		SizeBytes:   sizeBytes,
		StorageKey:  storageKey,
		Status:      "pending",
	})
	if err != nil {
		return "", "", fmt.Errorf("create attachment: %w", err)
	}

	presignedURL, err := s.minio.PresignedPutObject(ctx, s.bucket, storageKey, 5*time.Minute)
	if err != nil {
		return "", "", fmt.Errorf("generate presigned URL: %w", err)
	}

	return presignedURL.String(), attachment.ID.String(), nil
}

// CompleteUpload verifies the upload and marks it complete.
func (s *Service) CompleteUpload(ctx context.Context, attachmentID, uploaderID uuid.UUID) (Attachment, string, error) {
	attachment, err := s.store.GetAttachment(ctx, attachmentID)
	if err != nil {
		return Attachment{}, "", fmt.Errorf("get attachment: %w", err)
	}

	if attachment.UploaderID != uploaderID {
		return Attachment{}, "", fmt.Errorf("unauthorized: uploader mismatch")
	}
	if attachment.Status != "pending" {
		return Attachment{}, "", fmt.Errorf("attachment is not pending")
	}

	_, err = s.minio.StatObject(ctx, s.bucket, attachment.StorageKey, minio.StatObjectOptions{})
	if err != nil {
		return Attachment{}, "", fmt.Errorf("object not found: %w", err)
	}

	updated, err := s.store.UpdateAttachmentStatus(ctx, attachmentID, "complete")
	if err != nil {
		return Attachment{}, "", fmt.Errorf("update status: %w", err)
	}

	downloadURL, err := s.minio.PresignedGetObject(ctx, s.bucket, attachment.StorageKey, time.Hour, nil)
	if err != nil {
		return updated, "", nil
	}

	return updated, downloadURL.String(), nil
}

// GetDownloadURL generates a presigned GET URL.
func (s *Service) GetDownloadURL(ctx context.Context, storageKey string) (string, error) {
	url, err := s.minio.PresignedGetObject(ctx, s.bucket, storageKey, time.Hour, nil)
	if err != nil {
		return "", fmt.Errorf("generate download URL: %w", err)
	}
	return url.String(), nil
}
