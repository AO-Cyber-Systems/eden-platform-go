package audit

import (
	"context"

	"github.com/google/uuid"
)

// AuditStore defines database operations for audit logging.
type AuditStore interface {
	CreateAuditLog(ctx context.Context, companyID, actorID uuid.UUID, action, resource, resourceID, ipAddress string, details []byte) error
}
