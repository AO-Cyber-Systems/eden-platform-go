package aosentry

import (
	"encoding/json"

	"github.com/aocybersystems/eden-platform-go/platform/skills"
)

// IntelligenceContext is the metadata payload apps send to AOSentry
// inside the ChatRequest.Metadata field under the "aodex_intelligence" key.
type IntelligenceContext struct {
	Memory            []MemoryFact      `json:"memory,omitempty"`
	Persona           *PersonaContext   `json:"persona,omitempty"`
	CrossConversation []EntityRef       `json:"cross_conversation,omitempty"`
	SkillContext      *skills.SkillContext `json:"skill_context,omitempty"`
	CostQualityPref   float64           `json:"cost_quality_pref,omitempty"`
	UserID            string            `json:"user_id,omitempty"`
	TeamID            string            `json:"team_id,omitempty"`
	OrgID             string            `json:"org_id,omitempty"`
}

// MemoryFact is a key-value fact from user memory.
type MemoryFact struct {
	Key       string  `json:"key"`
	Value     string  `json:"value"`
	Relevance float64 `json:"relevance,omitempty"`
}

// PersonaContext carries persona style/domain context.
type PersonaContext struct {
	Style  string `json:"style,omitempty"`
	Domain string `json:"domain,omitempty"`
}

// EntityRef is a cross-conversation entity reference.
type EntityRef struct {
	Label string `json:"label"`
	Type  string `json:"type,omitempty"`
}

// BuildMetadata constructs the Metadata JSON for a ChatRequest.
// Returns nil on error (fail-open — request proceeds without metadata).
func BuildMetadata(ctx *IntelligenceContext) json.RawMessage {
	if ctx == nil {
		return nil
	}

	wrapper := map[string]interface{}{
		"aodex_intelligence": ctx,
	}

	data, err := json.Marshal(wrapper)
	if err != nil {
		return nil
	}
	return data
}
