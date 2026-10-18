package git

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/sayaya1090/magi/internal/atomicfile"
	"github.com/sayaya1090/magi/internal/filelock"
)

// LocalRetention is a device observation, not a replicated object state. Cold
// records remain fallback candidates; ArchiveCandidate never deletes content.
type LocalRetention struct {
	ApplicationCount int                  `json:"application_count"`
	References       map[string]time.Time `json:"references,omitempty"`
	Applications     map[string]string    `json:"applications,omitempty"`
	LastApplied      time.Time            `json:"last_applied,omitempty"`
	Protected        bool                 `json:"protected"`
	ContentRevision  string               `json:"content_revision"`
	Since            time.Time            `json:"since"`
	Pinned           bool                 `json:"pinned,omitempty"`
	Archived         bool                 `json:"archived,omitempty"`
	Cold             bool                 `json:"cold"`
	ArchiveCandidate bool                 `json:"archive_candidate"`
}

func (s *Store) retentionPath(id string) (string, error) {
	root := s.retentionDir
	if root == "" {
		config := os.Getenv("MAGI_CONFIG_DIR")
		if config == "" {
			base, err := os.UserConfigDir()
			if err != nil {
				return "", err
			}
			config = filepath.Join(base, "magi")
		}
		abs, err := filepath.Abs(s.dir)
		if err != nil {
			return "", err
		}
		root = filepath.Join(config, "experience-local", digestJSON(filepath.Clean(abs)))
	}
	return filepath.Join(root, id+".json"), nil
}

// retention observes a locally available revision. A new device or changed
// content gets a full observation period. Reading/loading never renews it.
// action is empty for observation, or an explicit local user pin/unpin/restore.
func (s *Store) retention(ctx context.Context, r managedRecord, action string) (LocalRetention, error) {
	return s.retentionUpdate(ctx, r, action, nil)
}
func (s *Store) retentionUpdate(ctx context.Context, r managedRecord, action string, update func(*LocalRetention, time.Time) error) (LocalRetention, error) {
	var state LocalRetention
	if r.Change.Owner != "plugin:engram" {
		return state, nil
	}
	path, err := s.retentionPath(r.Change.ObjectID)
	if err != nil {
		return state, err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		return state, err
	}
	unlock, err := filelock.Acquire(ctx, path+".lock")
	if err != nil {
		return state, err
	}
	defer unlock()
	now := time.Now().UTC()
	if s.retentionNow != nil {
		now = s.retentionNow().UTC()
	}
	b, err := atomicfile.ReadFile(path)
	exists := err == nil
	if exists {
		if json.Unmarshal(b, &state) != nil || state.Since.IsZero() || state.ContentRevision == "" {
			return state, fmt.Errorf("invalid local retention record; refusing overwrite")
		}
	} else if !errors.Is(err, os.ErrNotExist) {
		return state, err
	}
	revision := r.ContentRevision
	if revision == "" {
		revision = r.Revision
	}
	if revision == "" {
		return state, fmt.Errorf("managed content revision is missing")
	}
	changed := !exists || state.ContentRevision != revision
	if changed {
		state.LastApplied = time.Time{}
		state.References = nil
		state.ContentRevision = revision
		state.Since = now
	}
	if update != nil {
		if err := update(&state, now); err != nil {
			return state, err
		}
		changed = true
	}
	state.Protected = false
	for id, until := range state.References {
		if until.After(now) {
			state.Protected = true
		} else {
			delete(state.References, id)
			changed = true
		}
	}
	switch action {
	case "":
	case "pin":
		state.Pinned = true
		changed = true
	case "unpin":
		state.Pinned = false
		changed = true
	case "archive":
		state.Archived = true
		changed = true
	case "restore":
		state.Archived = false
		state.Since = now
		changed = true
	default:
		return state, fmt.Errorf("unknown local retention action")
	}
	age := now.Sub(state.Since)
	state.Cold = !state.Pinned && !state.Protected && age >= 30*24*time.Hour
	state.ArchiveCandidate = !state.Pinned && !state.Protected && age >= 90*24*time.Hour
	if changed {
		b, err = json.Marshal(state)
		if err != nil {
			return state, err
		}
		if err := atomicfile.Write(path, append(b, '\n'), 0600); err != nil {
			return state, err
		}
	}
	return state, nil
}

// LocalExperienceRetention exposes local age/pin status and explicit user intent.
// It cannot restore a withdrawn object or mutate its revision. No model tool is
// given this authority. Callers must supply the revision they displayed.
func (s *Store) LocalExperienceRetention(ctx context.Context, id, revision, action string) (LocalRetention, error) {
	var state LocalRetention
	if !managedID.MatchString(id) {
		return state, fmt.Errorf("invalid managed ID")
	}
	// Serialize with writes so an action cannot target a revision replaced meanwhile.
	unlock, err := filelock.Acquire(ctx, filepath.Join(s.dir, ".managed-write.lock"))
	if err != nil {
		return state, err
	}
	defer unlock()
	b, err := atomicfile.ReadFile(filepath.Join(s.dir, "managed", id+".json"))
	if err != nil {
		return state, err
	}
	var r managedRecord
	if json.Unmarshal(b, &r) != nil || r.Version != 1 || r.Change.ObjectID != id {
		return state, fmt.Errorf("invalid managed record")
	}
	if r.Change.State != "active" || r.Change.Owner != "plugin:engram" {
		return state, fmt.Errorf("local retention requires an active automatic object")
	}
	current := r.ContentRevision
	if current == "" {
		current = r.Revision
	}
	if revision != current {
		return state, fmt.Errorf("managed experience content revision conflict")
	}
	return s.retention(ctx, r, action)
}

// Public omits internal turn IDs and dedup receipts from governance responses.
func (s LocalRetention) Public() LocalRetention {
	s.ApplicationCount = len(s.Applications)
	s.Applications = nil
	s.References = nil
	return s
}
