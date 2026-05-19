package audit

import (
	"context"
	"encoding/json"
	"log/slog"
	"time"

	"github.com/google/uuid"
)

// Event represents an audit log event to be written asynchronously.
//
// AC-2 / AUD-* extensions (Obj 9, TRD 09-02): the optional fields below
// (Decision/ActorKind/SubjectID/SubjectKind/MFA/Federation/Risk) carry the
// non-repudiation + recertification context that the signed audit pipeline
// (SignedStore + PostgresBufferStore + Forwarder) marshals into the canonical
// JWS payload. Existing emitters that leave them zero-valued are unaffected;
// the canonical encoder omits empty fields.
type Event struct {
	CompanyID  string
	ActorID    string
	Action     string
	Resource   string
	ResourceID string
	Details    map[string]any
	IPAddress  string

	// Decision attaches an "allow" | "deny" | "partial" string to
	// security-relevant events (auth attempts, RBAC checks, recertification
	// outcomes). Empty for events without a decision semantic.
	Decision string

	// ActorKind disambiguates the actor: "human" | "service" | "admin" |
	// "system". Required for AC-2 evidence to separate human vs automated
	// changes.
	ActorKind string

	// SubjectID is the entity the event is ABOUT (vs ActorID, who triggered
	// it). For account suspension, ActorID is the admin and SubjectID is the
	// suspended account.
	SubjectID string

	// SubjectKind disambiguates the subject domain: "account" | "group" |
	// "role" | "tenant" | etc.
	SubjectKind string

	// MFA captures factors presented + verified for the session that
	// produced the event. Populated by AUTH-* events.
	MFA *MFAAttestation

	// Federation records the federation chain for federated logins.
	// Populated by AUD-04 federation.assertion.* events.
	Federation []FederationLink

	// Risk captures the risk evaluator's output (score + signals) for the
	// session/event. Populated by AUTH-07 evaluations.
	Risk *RiskAttestation
}

// MFAAttestation captures what MFA factors a session presented and verified.
// Populated by AUTH-* events; serialized into the canonical audit JSON as the
// "mfa" object.
type MFAAttestation struct {
	Presented       []string `json:"presented,omitempty"`
	Verified        []string `json:"verified,omitempty"`
	StepUpSatisfied bool     `json:"step_up_satisfied,omitempty"`
	AALAchieved     string   `json:"aal_achieved,omitempty"`
}

// FederationLink records one hop in a federation chain. AUD-04 events emit a
// slice of FederationLink to preserve the full IdP path (e.g., Login.gov →
// AOID for civilian agencies).
type FederationLink struct {
	IDP       string `json:"idp,omitempty"`
	Level     string `json:"level,omitempty"`
	TrustLink string `json:"trust_link,omitempty"`
}

// RiskAttestation captures the AUTH-07 risk evaluator output: total score and
// the signals that contributed to it.
type RiskAttestation struct {
	Score   int32        `json:"score,omitempty"`
	Signals []RiskSignal `json:"signals,omitempty"`
}

// RiskSignal is one triggered risk indicator + its contribution to the score.
type RiskSignal struct {
	Signal string `json:"signal,omitempty"`
	Weight int32  `json:"weight,omitempty"`
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
