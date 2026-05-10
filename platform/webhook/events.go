package webhook

// Standard event-type constants. Donor: aosentry/internal/webhook.
//
// Consumers MAY define their own event types; these are the canonical
// platform-published vocabulary so subscribers can wildcard-match across
// products. Wildcards: "*" matches everything, "<prefix>.*" matches any
// event whose type starts with the prefix.
const (
	// Identity / access events.
	EventKeyCreated  = "key.created"
	EventKeyDeleted  = "key.deleted"
	EventKeyBlocked  = "key.blocked"
	EventUserCreated = "user.created"
	EventUserDeleted = "user.deleted"
	EventTeamCreated = "team.created"

	// Budget / spend events.
	EventBudgetExceeded = "budget.exceeded"
	EventSpendAlert     = "spend.alert"

	// Safety / health events.
	EventGuardrailBlock = "guardrail.blocked"
	EventModelError     = "model.error"
	EventHealthDown     = "health.down"
	EventHealthUp       = "health.up"
	EventAnomalyDetect  = "anomaly.detected"
)
