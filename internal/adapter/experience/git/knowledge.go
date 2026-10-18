package git

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"strings"

	"github.com/sayaya1090/magi/internal/port"
)

func (s *Store) ManageKnowledge(ctx context.Context, action port.KnowledgeAction) ([]port.KnowledgeItem, error) {
	if action.Action != "" {
		if !strings.HasPrefix(action.Name, "managed:") || action.Revision == "" {
			return nil, fmt.Errorf("select a managed item and reload its revision before changing retention")
		}
		if _, err := s.LocalExperienceRetention(ctx, strings.TrimPrefix(action.Name, "managed:"), action.Revision, action.Action); err != nil {
			return nil, err
		}
	}
	inventory, err := s.Inventory(ctx)
	if err != nil {
		return nil, err
	}
	out := make([]port.KnowledgeItem, 0, len(inventory))
	for _, entry := range inventory {
		item := port.KnowledgeItem{Name: entry.Name, Kind: entry.Kind, Description: entry.Description}
		if r := entry.Retention; r != nil && r.ContentRevision != "" {
			item.Managed = true
			item.Revision = r.ContentRevision
			item.Pinned = r.Pinned
			item.Archived = r.Archived
			item.Cold = r.Cold
			item.ArchiveCandidate = r.ArchiveCandidate
			item.Protected = r.Protected
			item.ApplicationCount = r.ApplicationCount
			item.Since = r.Since
			item.LastApplied = r.LastApplied
		}
		out = append(out, item)
	}
	return out, nil
}

// ImportLegacySkill creates a pinned, user-owned snapshot. No host-success claim
// or executable verification snapshot is assigned to an imported document.
func (s *Store) ImportLegacySkill(ctx context.Context, skill port.Skill) error {
	if skill.Name == "" || skill.Body == "" || strings.ContainsAny(skill.Name, "/\\") {
		return fmt.Errorf("invalid legacy skill")
	}
	idHash := sha256.Sum256([]byte(skill.Name))
	id := "import_" + hex.EncodeToString(idHash[:])
	skill.DocumentDigest = ""
	skill.VerificationDigest = ""
	digest := digestJSON(skill)
	receipt, err := s.ApplyExperience(ctx, port.ExperienceChange{
		ObjectID: id, OperationID: "import_" + digest, ObservationID: "user-import:" + digest,
		Owner: "user:legacy-import", Source: "local-skill-import", HostOutcome: "user-imported",
		Scope: "project", Kind: "skill", State: "active", Skill: &skill,
	})
	if err != nil {
		return err
	}
	// A partial failure can be retried with the same idempotent publication.
	_, err = s.LocalExperienceRetention(ctx, receipt.ObjectID, receipt.ContentRevision, "")
	return err
}
