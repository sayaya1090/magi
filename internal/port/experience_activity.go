package port

import (
	"context"
	"time"
)

// ExperienceReference identifies the exact automatic object revision read by
// a host. Digests are from publication, not a model's claim at verification time.
type ExperienceReference struct {
	ObjectID, ContentRevision, Kind, Name, DocumentDigest, VerificationDigest string
}

// ExperienceActivityStore accepts host observations only. It is deliberately
// absent from ToolEnv and the Lua model-facing proposal interface.
type ExperienceActivityStore interface {
	ExperienceReferences(context.Context, string) ([]ExperienceReference, error)
	ManagedMemoryReferences(context.Context, []string) ([]ExperienceReference, error)
	ProtectExperience(context.Context, ExperienceReference, string, time.Time) error
	ReleaseExperience(context.Context, ExperienceReference, string) error
	ConfirmExperienceApplication(context.Context, ExperienceReference, string, string) error
}

// ForegroundProcessTool is implemented by host process runners that report the
// actual exit status. Remote/model-authored text cannot opt into this contract.
type ForegroundProcessTool interface{ ObservesProcessExit() bool }
