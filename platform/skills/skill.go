// Package skills provides shared skill capability types used across all apps.
// Skills are named capabilities that the AI activates when user intent matches.
// A skill may be implemented as a single call, multi-step pipeline, MCP tool,
// or hybrid — the implementation is app-specific.
//
// These types are DB-agnostic: no pgx, pgtype, or database imports.
package skills

import "time"

// Cost tier constants.
const (
	CostTierLow    = "low"
	CostTierMedium = "medium"
	CostTierHigh   = "high"
)

// Visibility constants.
const (
	VisibilityPublic  = "public"
	VisibilityOrg     = "org"
	VisibilityTeam    = "team"
	VisibilityPrivate = "private"
)

// Status constants.
const (
	StatusActive   = "active"
	StatusProposed = "proposed"
	StatusArchived = "archived"
)

// Output format constants.
const (
	OutputFormatText  = "text"
	OutputFormatJSON  = "json"
	OutputFormatCode  = "code"
	OutputFormatImage = "image"
)

// Skill defines a named AI capability. Skills are routing/optimization hints
// that tell the gateway how to handle requests matching this capability.
type Skill struct {
	ID              string            `json:"id"`
	Name            string            `json:"name"`
	Description     string            `json:"description,omitempty"`
	Domain          string            `json:"domain,omitempty"`
	TaskTypes       []string          `json:"task_types,omitempty"`
	TriggerKeywords []string          `json:"trigger_keywords,omitempty"`
	TriggerPatterns []string          `json:"trigger_patterns,omitempty"`
	PreferredModels []ModelPreference `json:"preferred_models,omitempty"`
	PromptGuidance  string            `json:"prompt_guidance,omitempty"`
	OutputFormat    string            `json:"output_format,omitempty"`
	RequiredCaps    []string          `json:"required_caps,omitempty"`
	CostTier        string            `json:"cost_tier,omitempty"`
	IsSystem        bool              `json:"is_system"`
	Visibility      string            `json:"visibility"`
	Status          string            `json:"status"`
	CreatedAt       time.Time         `json:"created_at"`
	UpdatedAt       time.Time         `json:"updated_at"`
}

// ModelPreference expresses which model/provider is preferred for this skill.
type ModelPreference struct {
	ModelName   string `json:"model_name"`
	Provider    string `json:"provider"`
	Priority    int    `json:"priority"`
	CostQuality string `json:"cost_quality,omitempty"` // "speed", "balanced", "quality"
}

// SkillContext is the metadata an app sends with each AOSentry request
// to identify which skill/step is active.
type SkillContext struct {
	SkillID   string `json:"skill_id,omitempty"`
	SkillName string `json:"skill_name"`
	StepName  string `json:"step_name,omitempty"`
	StepIndex int    `json:"step_index,omitempty"`
	TotalSteps int   `json:"total_steps,omitempty"`
}
