package port

import (
	"context"
	"time"

	"github.com/sayaya1090/magi/internal/core/companion"
)

// ProfileMaintenance is an internal session, not a user conversation or a tool
// environment. Its writer may change descriptive metadata only.
type ProfileMaintenance interface {
	ProfileMaintenanceState(context.Context, string) (ProfileMaintenanceState, error)
	BeginProfileMaintenance(context.Context, string, string, string, int64, string) (ProfileMaintenanceTicket, error)
	FinishProfileMaintenance(context.Context, string, ProfileMaintenanceTicket, *companion.Contract, string) (ProfileMaintenanceState, error)
}

type ProfileMaintenanceState struct {
	Enabled     bool                      `json:"enabled"`
	Pending     bool                      `json:"pending"`
	ID          string                    `json:"id"`
	Revision    string                    `json:"revision"`
	Checkpoints map[string]int64          `json:"checkpoints"`
	LastAttempt time.Time                 `json:"last_attempt"`
	LastSuccess time.Time                 `json:"last_success"`
	LastError   string                    `json:"last_error,omitempty"`
	Running     bool                      `json:"running"`
	History     []ProfileMaintenanceEntry `json:"history"`
	Current     companion.Contract        `json:"current"`
}
type ProfileMaintenanceEntry struct {
	At      time.Time `json:"at"`
	Source  string    `json:"source"`
	Through int64     `json:"through"`
	Status  string    `json:"status"`
	Added   []string  `json:"added,omitempty"`
	Updated []string  `json:"updated,omitempty"`
	Error   string    `json:"error,omitempty"`
}
type ProfileMaintenanceTicket struct {
	Token          string
	Source         string
	Through        int64
	EvidenceHash   string
	PolicyRevision string
}
