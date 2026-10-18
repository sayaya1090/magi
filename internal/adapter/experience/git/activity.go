package git

import (
	"context"
	"encoding/json"
	"fmt"
	"path/filepath"
	"strings"
	"time"

	"github.com/sayaya1090/magi/internal/atomicfile"
	"github.com/sayaya1090/magi/internal/filelock"
	"github.com/sayaya1090/magi/internal/port"
)

func (s *Store) ExperienceReferences(ctx context.Context, name string) ([]port.ExperienceReference, error) {
	paths, err := filepath.Glob(filepath.Join(s.dir, "managed", "*.json"))
	if err != nil {
		return nil, err
	}
	var out []port.ExperienceReference
	for _, path := range paths {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		b, err := atomicfile.ReadFile(path)
		if err != nil {
			return nil, err
		}
		var r managedRecord
		if json.Unmarshal(b, &r) != nil || r.Version != 1 || filepath.Base(path) != r.Change.ObjectID+".json" {
			return nil, fmt.Errorf("invalid managed record")
		}
		if r.Change.Owner != "plugin:engram" || r.Change.State != "active" || r.Change.Skill == nil || r.Change.Skill.Name != name {
			continue
		}
		sk := r.Change.Skill
		rev := r.ContentRevision
		if rev == "" {
			rev = r.Revision
		}
		out = append(out, port.ExperienceReference{Kind: "skill", ObjectID: r.Change.ObjectID, ContentRevision: rev, Name: name, DocumentDigest: sk.DocumentDigest, VerificationDigest: sk.VerificationDigest})
	}
	return out, nil
}
func (s *Store) updateActivity(ctx context.Context, ref port.ExperienceReference, update func(*LocalRetention, time.Time) error) error {
	if !managedID.MatchString(ref.ObjectID) {
		return fmt.Errorf("invalid managed ID")
	}
	unlock, err := filelock.Acquire(ctx, filepath.Join(s.dir, ".managed-write.lock"))
	if err != nil {
		return err
	}
	defer unlock()
	b, err := atomicfile.ReadFile(filepath.Join(s.dir, "managed", ref.ObjectID+".json"))
	if err != nil {
		return err
	}
	var r managedRecord
	if json.Unmarshal(b, &r) != nil || r.Version != 1 || r.Change.ObjectID != ref.ObjectID {
		return fmt.Errorf("invalid managed record")
	}
	revision := r.ContentRevision
	if revision == "" {
		revision = r.Revision
	}
	if ref.Kind == "skill" {
		if r.Change.Skill == nil || r.Change.Skill.Name != ref.Name || r.Change.Skill.DocumentDigest != ref.DocumentDigest || r.Change.Skill.VerificationDigest != ref.VerificationDigest {
			return fmt.Errorf("managed skill identity conflict")
		}
	} else if ref.Kind != "memory" || r.Change.Memory == nil {
		return fmt.Errorf("managed activity kind mismatch")
	}

	if revision != ref.ContentRevision || r.Change.State != "active" || r.Change.Owner != "plugin:engram" {
		return fmt.Errorf("managed activity revision/state conflict")
	}
	_, err = s.retentionUpdate(ctx, r, "", update)
	return err
}
func (s *Store) ProtectExperience(ctx context.Context, ref port.ExperienceReference, id string, until time.Time) error {
	return s.updateActivity(ctx, ref, func(st *LocalRetention, now time.Time) error {
		if id == "" || len(id) > 128 || !until.After(now) || until.After(now.Add(5*time.Minute)) {
			return fmt.Errorf("invalid activity lease")
		}
		if st.References == nil {
			st.References = map[string]time.Time{}
		}
		for key, expiry := range st.References {
			if !expiry.After(now) {
				delete(st.References, key)
			}
		}
		if _, exists := st.References[id]; !exists && len(st.References) >= 256 {
			return fmt.Errorf("activity reference limit reached")
		}
		st.References[id] = until
		return nil
	})
}
func (s *Store) ReleaseExperience(ctx context.Context, ref port.ExperienceReference, id string) error {
	return s.updateActivity(ctx, ref, func(st *LocalRetention, _ time.Time) error { delete(st.References, id); return nil })
}
func (s *Store) ConfirmExperienceApplication(ctx context.Context, ref port.ExperienceReference, observation, evidence string) error {
	if ref.Kind != "skill" || observation == "" || len(observation) > 4096 || evidence == "" || len(evidence) > 4096 || ref.DocumentDigest == "" || ref.VerificationDigest == "" {
		return fmt.Errorf("application requires document, verification and execution evidence")
	}
	return s.updateActivity(ctx, ref, func(st *LocalRetention, now time.Time) error {
		if st.Applications == nil {
			st.Applications = map[string]string{}
		}
		if old, ok := st.Applications[observation]; ok {
			if old != evidence {
				return fmt.Errorf("application observation conflict")
			}
			return nil
		}
		turn, _, ok := strings.Cut(observation, ":")
		if !ok || !st.References[turn].After(now) {
			return fmt.Errorf("application requires an active host reference")
		}
		// Keep bounded local dedup receipts. Reject rather than silently forget IDs.
		if len(st.Applications) >= 4096 {
			return fmt.Errorf("application receipt limit reached")
		}
		st.Applications[observation] = evidence
		st.LastApplied = now
		st.Since = now
		st.Archived = false
		return nil
	})
}

func (s *Store) ManagedMemoryReferences(ctx context.Context, ids []string) ([]port.ExperienceReference, error) {
	var out []port.ExperienceReference
	for _, id := range ids {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		if !strings.HasPrefix(id, "managed:") {
			continue
		}
		object, revision, ok := strings.Cut(strings.TrimPrefix(id, "managed:"), "@")
		if !ok || !managedID.MatchString(object) {
			continue
		}
		b, err := atomicfile.ReadFile(filepath.Join(s.dir, "managed", object+".json"))
		if err != nil {
			return nil, err
		}
		var r managedRecord
		if json.Unmarshal(b, &r) != nil || r.Version != 1 || r.Change.ObjectID != object {
			return nil, fmt.Errorf("invalid managed record")
		}
		current := r.ContentRevision
		if current == "" {
			current = r.Revision
		}
		if revision == current && r.Change.Owner == "plugin:engram" && r.Change.State == "active" && r.Change.Memory != nil {
			out = append(out, port.ExperienceReference{Kind: "memory", ObjectID: object, ContentRevision: revision})
		}
	}
	return out, nil
}
