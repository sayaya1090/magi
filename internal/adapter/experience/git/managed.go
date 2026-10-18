package git

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"

	"github.com/sayaya1090/magi/internal/atomicfile"
	"github.com/sayaya1090/magi/internal/filelock"
	"github.com/sayaya1090/magi/internal/port"
)

// Managed records are local projections with explicit identity and CAS. They do
// not rewrite legacy memory/skill files or claim to implement fleet replication.
// One atomic JSON file contains both current state and idempotency receipts, so
// a crash cannot publish content without its operation marker.
type managedRecord struct {
	Version         int                         `json:"version"`
	Change          port.ExperienceChange       `json:"change"`
	Revision        string                      `json:"revision"`
	ContentRevision string                      `json:"content_revision"`
	Operations      map[string]managedOperation `json:"operations"`
}
type managedOperation struct {
	Digest  string                 `json:"digest"`
	Receipt port.ExperienceReceipt `json:"receipt"`
}

var managedID = regexp.MustCompile(`^[a-zA-Z0-9_-]{1,128}$`)

func digestJSON(v any) string {
	b, _ := json.Marshal(v)
	sum := sha256.Sum256(b)
	return hex.EncodeToString(sum[:])
}

func (s *Store) ApplyExperience(ctx context.Context, c port.ExperienceChange) (port.ExperienceReceipt, error) {
	var empty port.ExperienceReceipt
	if !managedID.MatchString(c.ObjectID) || !managedID.MatchString(c.OperationID) || c.Owner == "" || c.ObservationID == "" {
		return empty, fmt.Errorf("managed experience requires safe object/operation IDs, owner and observation")
	}
	if c.Scope != "" && c.Scope != "project" {
		return empty, fmt.Errorf("managed experience currently supports project scope only")
	}
	if c.State != "active" && c.State != "withdrawn" && c.State != "archived" {
		return empty, fmt.Errorf("invalid managed experience state")
	}
	if c.Kind != "memory" && c.Kind != "skill" {
		return empty, fmt.Errorf("invalid managed experience kind")
	}
	if c.State == "active" && ((c.Kind == "memory" && (c.Memory == nil || c.Memory.Text == "" || c.Skill != nil)) || (c.Kind == "skill" && (c.Skill == nil || c.Skill.Name == "" || c.Skill.Body == "" || c.Memory != nil))) {
		return empty, fmt.Errorf("managed experience payload does not match its kind")
	}
	if c.Kind == "skill" && c.State == "active" && c.HostOutcome != "verified" && !(c.Owner == "user:legacy-import" && c.Source == "local-skill-import" && c.HostOutcome == "user-imported") {
		return empty, fmt.Errorf("automatic skill requires host verified")
	}
	if err := os.MkdirAll(s.dir, 0o700); err != nil {
		return empty, err
	}
	release, err := filelock.Acquire(ctx, filepath.Join(s.dir, ".managed-write.lock"))
	if err != nil {
		return empty, err
	}
	defer release()
	dir := filepath.Join(s.dir, "managed")
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return empty, err
	}
	path := filepath.Join(dir, c.ObjectID+".json")
	var old managedRecord
	b, err := atomicfile.ReadFile(path)
	if err == nil {
		if err := json.Unmarshal(b, &old); err != nil || old.Version != 1 || old.Change.ObjectID != c.ObjectID {
			return empty, fmt.Errorf("invalid managed record; refusing overwrite")
		}
		// Owner comes from the host bridge, never a model-authored owner claim.
		if old.Change.Owner != c.Owner || old.Change.Kind != c.Kind {
			return empty, fmt.Errorf("managed experience ownership/kind mismatch")
		}
	} else if !errors.Is(err, os.ErrNotExist) {
		return empty, err
	}
	opDigest := digestJSON(c)
	if op, ok := old.Operations[c.OperationID]; ok {
		if op.Digest != opDigest {
			return empty, fmt.Errorf("operation ID reused with different content")
		}
		return op.Receipt, nil
	}
	if old.Revision != c.ExpectedRevision {
		return empty, fmt.Errorf("managed experience revision conflict")
	}
	if old.Revision == "" && c.State != "active" {
		return empty, fmt.Errorf("cannot retire missing managed experience")
	}
	// Retain payload behind a tombstone for audit/restore; retrieval checks state.
	if c.State != "active" {
		c.Memory = old.Change.Memory
		c.Skill = old.Change.Skill
	}
	revision := digestJSON(struct {
		Parent string
		Change port.ExperienceChange
	}{old.Revision, c})
	contentRevision := digestJSON(struct {
		Kind, State string
		Memory      *port.Memory
		Skill       *port.Skill
	}{c.Kind, c.State, c.Memory, c.Skill})
	receipt := port.ExperienceReceipt{ObjectID: c.ObjectID, Revision: revision, ContentRevision: contentRevision, State: c.State}
	if old.Operations == nil {
		old.Operations = map[string]managedOperation{}
	}
	old.Operations[c.OperationID] = managedOperation{Digest: opDigest, Receipt: receipt}
	record := managedRecord{Version: 1, Change: c, Revision: revision, ContentRevision: contentRevision, Operations: old.Operations}
	b, err = json.MarshalIndent(record, "", "  ")
	if err != nil {
		return empty, err
	}
	if err := atomicfile.Write(path, append(b, '\n'), 0o600); err != nil {
		return empty, err
	}
	return receipt, nil
}

func (s *Store) managedPool(ctx context.Context, query string) ([]Scored[port.Memory], []Scored[port.Skill], error) {
	paths, err := filepath.Glob(filepath.Join(s.dir, "managed", "*.json"))
	if err != nil {
		return nil, nil, err
	}
	if len(paths) == 0 {
		return nil, nil, nil
	}
	release, err := filelock.Acquire(ctx, filepath.Join(s.dir, ".managed-write.lock"))
	if err != nil {
		return nil, nil, err
	}
	defer release()
	var memories []Scored[port.Memory]
	var skills []Scored[port.Skill]
	terms := tokenize(query)
	for _, path := range paths {
		b, err := atomicfile.ReadFile(path)
		if err != nil {
			return nil, nil, err
		}
		var r managedRecord
		if err := json.Unmarshal(b, &r); err != nil || r.Version != 1 || filepath.Base(path) != r.Change.ObjectID+".json" {
			return nil, nil, fmt.Errorf("invalid managed record: %s", filepath.Base(path))
		}
		if r.Change.State != "active" {
			continue
		}
		retention, err := s.retention(ctx, r, "")
		if err != nil {
			return nil, nil, err
		}
		if retention.Archived {
			continue
		}
		if r.Change.Kind == "memory" && r.Change.Memory != nil {
			m := *r.Change.Memory
			contentRevision := r.ContentRevision
			if contentRevision == "" {
				contentRevision = r.Revision
			}
			m.ID = "managed:" + r.Change.ObjectID + "@" + contentRevision
			memories = append(memories, Scored[port.Memory]{Score: overlap(terms, m.Text), Cold: retention.Cold, V: m})
		} else if r.Change.Kind == "skill" && r.Change.Skill != nil {
			sk := *r.Change.Skill
			skills = append(skills, Scored[port.Skill]{Score: overlap(terms, sk.Name+" "+sk.Description+" "+sk.Body), Cold: retention.Cold, V: sk})
		}
	}
	return memories, skills, nil
}

func (s *Store) managedInventory(ctx context.Context) ([]SkillInfo, error) {
	paths, err := filepath.Glob(filepath.Join(s.dir, "managed", "*.json"))
	if err != nil {
		return nil, err
	}
	if len(paths) == 0 {
		return nil, nil
	}
	release, err := filelock.Acquire(ctx, filepath.Join(s.dir, ".managed-write.lock"))
	if err != nil {
		return nil, err
	}
	defer release()
	var out []SkillInfo
	for _, path := range paths {
		b, err := atomicfile.ReadFile(path)
		if err != nil {
			return nil, err
		}
		var r managedRecord
		if err := json.Unmarshal(b, &r); err != nil || r.Version != 1 || filepath.Base(path) != r.Change.ObjectID+".json" {
			return nil, fmt.Errorf("invalid managed record")
		}
		if r.Change.State != "active" {
			continue
		}
		retention, err := s.retention(ctx, r, "")
		if err != nil {
			return nil, err
		}
		info := SkillInfo{Name: "managed:" + r.Change.ObjectID, Kind: r.Change.Kind, Observed: 1}
		if retentionOwner(r.Change.Owner) {
			public := retention.Public()
			info.Retention = &public
		}
		if r.Change.Memory != nil {
			info.Description = r.Change.Memory.Text
			info.Body = r.Change.Memory.Text
		}
		if r.Change.Skill != nil {
			info.Description = r.Change.Skill.Description
			info.Body = r.Change.Skill.Body
		}
		out = append(out, info)
	}
	return out, nil
}

// Local administration withdraws a managed object rather than deleting its
// retry history. A later stale plugin update conflicts instead of resurrecting it.
func (s *Store) forgetManaged(ctx context.Context, id string) error {
	if !managedID.MatchString(id) {
		return fmt.Errorf("invalid managed ID")
	}
	b, err := atomicfile.ReadFile(filepath.Join(s.dir, "managed", id+".json"))
	if err != nil {
		return err
	}
	var r managedRecord
	if err := json.Unmarshal(b, &r); err != nil || r.Version != 1 || r.Change.ObjectID != id {
		return fmt.Errorf("invalid managed record")
	}
	c := r.Change
	c.ExpectedRevision = r.Revision
	c.State = "withdrawn"
	c.Memory = nil
	c.Skill = nil
	nonce := make([]byte, 16)
	if _, err := rand.Read(nonce); err != nil {
		return err
	}
	c.OperationID = hex.EncodeToString(nonce)
	c.ObservationID = "local-withdrawal:" + c.OperationID
	_, err = s.ApplyExperience(ctx, c)
	return err
}
