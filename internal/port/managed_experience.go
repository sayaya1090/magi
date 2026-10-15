package port

import "context"

// ManagedExperienceStore is an optional local lifecycle boundary. Existing,
// independently authored memories remain on ExperienceStore; they are never
// silently adopted, overwritten or withdrawn by a managed observation.
type ManagedExperienceStore interface {
	ApplyExperience(context.Context, ExperienceChange) (ExperienceReceipt, error)
}

type ExperienceChange struct {
	ObjectID         string  `json:"object_id"`
	ExpectedRevision string  `json:"expected_revision"`
	OperationID      string  `json:"operation_id"`
	ObservationID    string  `json:"observation_id"`
	Owner            string  `json:"owner"`
	Scope            string  `json:"scope"`
	Kind             string  `json:"kind"`  // memory or skill
	State            string  `json:"state"` // active, archived or withdrawn
	Memory           *Memory `json:"memory,omitempty"`
	Skill            *Skill  `json:"skill,omitempty"`
	Source           string  `json:"source"`
	HostOutcome      string  `json:"host_outcome"`
}

type ExperienceReceipt struct {
	ObjectID        string `json:"object_id"`
	Revision        string `json:"revision"`
	ContentRevision string `json:"content_revision"`
	State           string `json:"state"`
}
