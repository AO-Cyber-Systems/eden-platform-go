package notification

import (
	"testing"

	"github.com/google/uuid"
)

func TestDeviceTokenRecord_Fields(t *testing.T) {
	record := DeviceTokenRecord{
		Token:    "fcm-token-123",
		Platform: "android",
		UserID:   uuid.New(),
	}

	if record.Token != "fcm-token-123" {
		t.Errorf("Token = %q, want %q", record.Token, "fcm-token-123")
	}
	if record.Platform != "android" {
		t.Errorf("Platform = %q, want %q", record.Platform, "android")
	}
	if record.UserID == uuid.Nil {
		t.Errorf("UserID is nil, want non-nil")
	}
}
