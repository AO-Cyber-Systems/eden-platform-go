package notification

import (
	"context"

	"github.com/google/uuid"
)

// NotificationStore defines database operations for notifications.
type NotificationStore interface {
	GetDeviceTokens(ctx context.Context, userID uuid.UUID) ([]DeviceTokenRecord, error)
	DeleteDeviceToken(ctx context.Context, token string, userID uuid.UUID) error
}

// DeviceTokenRecord represents a stored device token.
type DeviceTokenRecord struct {
	Token    string
	Platform string
	UserID   uuid.UUID
}

// Dispatcher defines the notification dispatch interface.
type Dispatcher interface {
	SendPush(ctx context.Context, tokens []DeviceTokenRecord, title, body string, data map[string]string) error
	IsEnabled() bool
}
