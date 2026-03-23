package devstore

import (
	"context"
	"sort"
	"time"

	platformv1 "github.com/aocybersystems/eden-platform-go/gen/go/platform/v1"
	"github.com/aocybersystems/eden-platform-go/platform/audit"
	"github.com/google/uuid"
)

var _ audit.AuditStore = (*AuditStore)(nil)

type auditLogEntry struct {
	ID         uuid.UUID
	CompanyID  uuid.UUID
	ActorID    uuid.UUID
	Action     string
	Resource   string
	ResourceID string
	Details    []byte
	IPAddress  string
	CreatedAt  time.Time
}

// AuditStore implements audit.AuditStore and provides query capabilities for the dev server.
type AuditStore struct {
	backend *Backend
}

func (s *AuditStore) CreateAuditLog(_ context.Context, companyID, actorID uuid.UUID, action, resource, resourceID, ipAddress string, details []byte) error {
	s.backend.mu.Lock()
	defer s.backend.mu.Unlock()

	entry := auditLogEntry{
		ID:         uuid.New(),
		CompanyID:  companyID,
		ActorID:    actorID,
		Action:     action,
		Resource:   resource,
		ResourceID: resourceID,
		Details:    details,
		IPAddress:  ipAddress,
		CreatedAt:  time.Now().UTC(),
	}
	s.backend.state.auditLogs = append(s.backend.state.auditLogs, entry)
	return nil
}

// QueryAuditLogs queries audit logs with filtering and pagination, returning proto messages directly.
func (s *AuditStore) QueryAuditLogs(_ context.Context, companyID uuid.UUID, limit, offset int, actorID *uuid.UUID, action, resource *string) ([]*platformv1.AuditLogEntry, int, error) {
	s.backend.mu.RLock()
	defer s.backend.mu.RUnlock()

	// Filter
	var filtered []auditLogEntry
	for _, entry := range s.backend.state.auditLogs {
		if entry.CompanyID != companyID {
			continue
		}
		if actorID != nil && entry.ActorID != *actorID {
			continue
		}
		if action != nil && entry.Action != *action {
			continue
		}
		if resource != nil && entry.Resource != *resource {
			continue
		}
		filtered = append(filtered, entry)
	}

	// Sort by CreatedAt DESC
	sort.Slice(filtered, func(i, j int) bool {
		return filtered[i].CreatedAt.After(filtered[j].CreatedAt)
	})

	total := len(filtered)

	// Apply offset + limit
	if offset >= len(filtered) {
		return nil, total, nil
	}
	filtered = filtered[offset:]
	if limit > 0 && limit < len(filtered) {
		filtered = filtered[:limit]
	}

	// Convert to proto
	entries := make([]*platformv1.AuditLogEntry, 0, len(filtered))
	for _, e := range filtered {
		entries = append(entries, &platformv1.AuditLogEntry{
			Id:          e.ID.String(),
			CompanyId:   e.CompanyID.String(),
			ActorId:     e.ActorID.String(),
			Action:      e.Action,
			Resource:    e.Resource,
			ResourceId:  e.ResourceID,
			DetailsJson: string(e.Details),
			IpAddress:   e.IPAddress,
			CreatedAt:   e.CreatedAt.Format(time.RFC3339),
		})
	}

	return entries, total, nil
}
