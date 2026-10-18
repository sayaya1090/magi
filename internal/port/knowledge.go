package port

import (
	"context"
	"time"
)

// KnowledgeItem is the user-facing, project-local inventory. Private execution
// receipts and active-turn identifiers never cross this boundary.
type KnowledgeItem struct {
	Importable       bool      `json:"importable"`
	Name             string    `json:"name"`
	Kind             string    `json:"kind"`
	Description      string    `json:"description"`
	Revision         string    `json:"revision"`
	Managed          bool      `json:"managed"`
	Pinned           bool      `json:"pinned"`
	Archived         bool      `json:"archived"`
	Cold             bool      `json:"cold"`
	ArchiveCandidate bool      `json:"archive_candidate"`
	Protected        bool      `json:"protected"`
	ApplicationCount int       `json:"application_count"`
	Since            time.Time `json:"since"`
	LastApplied      time.Time `json:"last_applied"`
}
type KnowledgeAction struct {
	Name     string `json:"name"`
	Revision string `json:"revision"`
	Action   string `json:"action"`
}

// KnowledgeManager is a user control, deliberately absent from ToolEnv.
type KnowledgeManager interface {
	ManageKnowledge(context.Context, KnowledgeAction) ([]KnowledgeItem, error)
}

// LegacySkillImporter creates an explicitly user-owned snapshot, without touching source files.
type LegacySkillImporter interface {
	ImportLegacySkill(context.Context, Skill) error
}
