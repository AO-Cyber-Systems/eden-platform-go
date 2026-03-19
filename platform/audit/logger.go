package audit

import (
	"context"
	"encoding/json"
	"log/slog"
	"time"

	"github.com/google/uuid"
)

// Event represents an audit log event to be written asynchronously.
type Event struct {
	CompanyID  string
	ActorID    string
	Action     string
	Resource   string
	ResourceID string
	Details    map[string]any
	IPAddress  string
}

// Logger provides async buffered audit log writing.
type Logger struct {
	events  chan Event
	store   AuditStore
	done    chan struct{}
	stopped chan struct{}
}

// NewLogger creates a new async audit logger.
func NewLogger(store AuditStore) *Logger {
	return &Logger{
		events:  make(chan Event, 10000),
		store:   store,
		done:    make(chan struct{}),
		stopped: make(chan struct{}),
	}
}

// Start launches the background batch-insert goroutine.
func (l *Logger) Start() {
	go l.run()
}

// Log sends an event to the logger. Never blocks; drops if channel full.
func (l *Logger) Log(event Event) {
	select {
	case l.events <- event:
	default:
		slog.Warn("audit log channel full, dropping event",
			"action", event.Action, "actor", event.ActorID)
	}
}

// Stop signals the logger to drain remaining events and shut down.
func (l *Logger) Stop() {
	close(l.done)
	<-l.stopped
}

func (l *Logger) run() {
	defer close(l.stopped)

	ticker := time.NewTicker(100 * time.Millisecond)
	defer ticker.Stop()

	var batch []Event

	flush := func() {
		if len(batch) == 0 {
			return
		}
		if l.store == nil {
			batch = batch[:0]
			return
		}
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()

		for _, e := range batch {
			detailsJSON, _ := json.Marshal(e.Details)
			if detailsJSON == nil {
				detailsJSON = []byte("{}")
			}
			companyID, err := uuid.Parse(e.CompanyID)
			if err != nil {
				slog.Warn("audit log: invalid company ID", "company_id", e.CompanyID)
				continue
			}
			actorID, err := uuid.Parse(e.ActorID)
			if err != nil {
				slog.Warn("audit log: invalid actor ID", "actor_id", e.ActorID)
				continue
			}
			if err := l.store.CreateAuditLog(ctx, companyID, actorID, e.Action, e.Resource, e.ResourceID, e.IPAddress, detailsJSON); err != nil {
				slog.Warn("audit log: failed to insert", "action", e.Action, "error", err)
			}
		}
		batch = batch[:0]
	}

	for {
		select {
		case e := <-l.events:
			batch = append(batch, e)
			if len(batch) >= 50 {
				flush()
			}
		case <-ticker.C:
			flush()
		case <-l.done:
			for {
				select {
				case e := <-l.events:
					batch = append(batch, e)
				default:
					flush()
					return
				}
			}
		}
	}
}
