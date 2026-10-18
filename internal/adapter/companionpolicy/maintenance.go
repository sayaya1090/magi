package companionpolicy

import (
	"bytes"
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"time"

	"github.com/BurntSushi/toml"
	"github.com/sayaya1090/magi/internal/atomicfile"
	"github.com/sayaya1090/magi/internal/core/companion"
	"github.com/sayaya1090/magi/internal/filelock"
	"github.com/sayaya1090/magi/internal/port"
)

// Maintainer owns one durable, bounded internal session per workspace. It has
// no tools, process runner, conversation submission, access or operation writer.
type Maintainer struct{ ConfigDir string }
type maintenanceRecord struct {
	Version    int                           `json:"version"`
	State      port.ProfileMaintenanceState  `json:"state"`
	LeaseUntil time.Time                     `json:"lease_until"`
	Ticket     port.ProfileMaintenanceTicket `json:"ticket"`
	Automatic  companion.Contract            `json:"automatic"`
	Evidence   []string                      `json:"evidence"`
}

func maintenanceHash(v any) string {
	b, _ := json.Marshal(v)
	s := sha256.Sum256(b)
	return hex.EncodeToString(s[:])
}
func (m *Maintainer) paths(wd string) (string, string, error) {
	policy, err := Path(m.ConfigDir, wd)
	if err != nil {
		return "", "", err
	}
	return policy, filepath.Join(filepath.Dir(policy), "maintenance.json"), nil
}
func (m *Maintainer) load(wd string) (maintenanceRecord, error) {
	policy, path, err := m.paths(wd)
	if err != nil {
		return maintenanceRecord{}, err
	}
	var r maintenanceRecord
	b, err := atomicfile.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		r.Version = 1
		r.State.ID = "profile_" + maintenanceHash(filepath.Dir(policy))[:24]
		r.State.Checkpoints = map[string]int64{}
	} else if err != nil {
		return r, err
	} else if json.Unmarshal(b, &r) != nil || r.Version != 1 || r.State.ID == "" || r.State.Checkpoints == nil {
		return r, fmt.Errorf("invalid maintenance session; refusing overwrite")
	}
	if r.State.PendingSources == nil {
		r.State.PendingSources = map[string]int64{}
	}
	r.State.Pending = len(r.State.PendingSources) > 0
	r.State.Current, err = Load(m.ConfigDir, wd)
	if err != nil {
		return r, err
	}
	r.State.Running = r.Ticket.Token != "" && time.Now().Before(r.LeaseUntil)
	if r.Ticket.Token != "" && !r.State.Running {
		r.State.LastError = "previous maintenance lease expired; no completion was confirmed"
	}
	r.State.Revision = maintenanceHash(struct {
		Ticket      port.ProfileMaintenanceTicket
		Checkpoints map[string]int64
		Attempt     time.Time
		Current     companion.Contract
	}{r.Ticket, r.State.Checkpoints, r.State.LastAttempt, r.State.Current})
	return r, nil
}
func (m *Maintainer) save(wd string, r maintenanceRecord) error {
	_, path, err := m.paths(wd)
	if err != nil {
		return err
	}
	b, err := json.Marshal(r)
	if err != nil {
		return err
	}
	return atomicfile.Write(path, append(b, '\n'), 0600)
}
func (m *Maintainer) lock(ctx context.Context, wd string) (func(), error) {
	policy, _, err := m.paths(wd)
	if err != nil {
		return nil, err
	}
	if err := os.MkdirAll(filepath.Dir(policy), 0700); err != nil {
		return nil, err
	}
	return filelock.Acquire(ctx, policy+".lock")
}
func (m *Maintainer) ProfileMaintenanceState(ctx context.Context, wd string) (port.ProfileMaintenanceState, error) {
	unlock, err := m.lock(ctx, wd)
	if err != nil {
		return port.ProfileMaintenanceState{}, err
	}
	defer unlock()
	r, err := m.load(wd)
	return r.State, err
}
func (m *Maintainer) BeginProfileMaintenance(ctx context.Context, wd, revision, source string, through int64, evidence string) (port.ProfileMaintenanceTicket, error) {
	var empty port.ProfileMaintenanceTicket
	unlock, err := m.lock(ctx, wd)
	if err != nil {
		return empty, err
	}
	defer unlock()
	r, err := m.load(wd)
	if err != nil {
		return empty, err
	}
	if source == "" || len(source) > 128 || through <= 0 || len(evidence) != 64 {
		return empty, fmt.Errorf("invalid maintenance evidence")
	}
	if r.State.Revision != revision || r.State.Running {
		return empty, fmt.Errorf("maintenance session changed or is already running")
	}
	if r.State.Checkpoints[source] >= through {
		return empty, port.ErrProfileEvidenceProcessed
	}
	if _, ok := r.State.Checkpoints[source]; !ok && len(r.State.Checkpoints) >= 256 {
		return empty, fmt.Errorf("maintenance source limit reached")
	}
	for _, hash := range r.Evidence {
		if hash == evidence {
			return empty, port.ErrProfileEvidenceProcessed
		}
	}
	// The durable cooldown applies across sources and daemon restarts.
	if !r.State.LastAttempt.IsZero() && time.Since(r.State.LastAttempt) < 10*time.Minute {
		return empty, fmt.Errorf("maintenance cooldown is active")
	}
	if r.Ticket.Token != "" {
		r.State.History = append(r.State.History, port.ProfileMaintenanceEntry{At: time.Now().UTC(), Source: r.Ticket.Source, Through: r.Ticket.Through, Status: "failed", Error: "previous maintenance lease expired; no completion was confirmed"})
		if len(r.State.History) > 32 {
			r.State.History = r.State.History[len(r.State.History)-32:]
		}
	}
	var token [16]byte
	if _, err := rand.Read(token[:]); err != nil {
		return empty, err
	}
	ticket := port.ProfileMaintenanceTicket{Token: hex.EncodeToString(token[:]), Source: source, Through: through, EvidenceHash: evidence, PolicyRevision: maintenanceHash(r.State.Current)}
	r.Ticket = ticket
	r.LeaseUntil = time.Now().Add(2 * time.Minute)
	r.State.LastAttempt = time.Now().UTC()
	r.State.LastError = ""
	r.State.Running = true
	if err := m.save(wd, r); err != nil {
		return empty, err
	}
	return ticket, nil
}
func (m *Maintainer) FinishProfileMaintenance(ctx context.Context, wd string, ticket port.ProfileMaintenanceTicket, proposal *companion.Contract, failure string) (port.ProfileMaintenanceState, error) {
	unlock, err := m.lock(ctx, wd)
	if err != nil {
		return port.ProfileMaintenanceState{}, err
	}
	defer unlock()
	r, err := m.load(wd)
	if err != nil {
		return r.State, err
	}
	if ticket.Token == "" || !reflect.DeepEqual(ticket, r.Ticket) || !r.State.Running {
		return r.State, fmt.Errorf("maintenance lease expired or ticket changed")
	}
	entry := port.ProfileMaintenanceEntry{At: time.Now().UTC(), Source: ticket.Source, Through: ticket.Through, Status: "unchanged"}
	if failure == "" && maintenanceHash(r.State.Current) != ticket.PolicyRevision {
		failure = "description changed while maintenance was running; no proposal applied"
	}
	if failure == "" && proposal != nil {
		if len(proposal.Access) > 0 || len(proposal.Operations) > 0 || len(proposal.Instructions) > 0 {
			failure = "maintenance proposal cannot change authority"
		} else {
			merged, automatic, added, updated := mergeMaintenanceProfile(r.State.Current, r.Automatic, *proposal)
			if len(added) > 0 || len(updated) > 0 || merged.Purpose != r.State.Current.Purpose || !reflect.DeepEqual(merged.Responsibilities, r.State.Current.Responsibilities) {
				if err := m.writeProfile(wd, merged); err != nil {
					failure = err.Error()
				} else {
					r.State.Current = merged
					r.Automatic = automatic
					entry.Status = "published"
					entry.Added = added
					entry.Updated = updated
				}
			}
		}
	}
	if failure != "" {
		if len(failure) > 2048 {
			failure = failure[:2048]
		}
		entry.Status = "failed"
		entry.Error = failure
		r.State.LastError = failure
	} else {
		r.State.LastSuccess = entry.At
		r.State.LastError = ""
		r.State.Checkpoints[ticket.Source] = ticket.Through
		if r.State.PendingSources[ticket.Source] <= ticket.Through {
			delete(r.State.PendingSources, ticket.Source)
		}
		r.Evidence = append(r.Evidence, ticket.EvidenceHash)
		if len(r.Evidence) > 256 {
			r.Evidence = r.Evidence[len(r.Evidence)-256:]
		}
	}
	r.State.History = append(r.State.History, entry)
	if len(r.State.History) > 32 {
		r.State.History = r.State.History[len(r.State.History)-32:]
	}
	r.State.Pending = len(r.State.PendingSources) > 0
	r.Ticket = port.ProfileMaintenanceTicket{}
	r.State.Running = false
	r.LeaseUntil = time.Time{}
	if err := m.save(wd, r); err != nil {
		return r.State, err
	}
	return r.State, nil
}

// Missing proposals never delete an offer. User-owned and subsequently edited
// fields are preserved; only exact previous automatic values may be replaced.
func mergeMaintenanceProfile(current, old, next companion.Contract) (companion.Contract, companion.Contract, []string, []string) {
	out := current
	automatic := old
	var added, updated []string
	if current.Purpose == "" || old.Purpose != "" && current.Purpose == old.Purpose {
		if next.Purpose != "" {
			out.Purpose = next.Purpose
			automatic.Purpose = next.Purpose
		}
	}
	if len(current.Responsibilities) == 0 || len(old.Responsibilities) > 0 && reflect.DeepEqual(current.Responsibilities, old.Responsibilities) {
		if len(next.Responsibilities) > 0 {
			out.Responsibilities = next.Responsibilities
			automatic.Responsibilities = next.Responsibilities
		}
	}
	out.Tasks = append([]companion.Task{}, current.Tasks...)
	automatic.Tasks = append([]companion.Task{}, old.Tasks...)
	for _, task := range next.Tasks {
		index := -1
		for i, t := range out.Tasks {
			if t.ID == task.ID {
				index = i
				break
			}
		}
		auto := -1
		for i, t := range automatic.Tasks {
			if t.ID == task.ID {
				auto = i
				break
			}
		}
		if index < 0 {
			if auto >= 0 {
				continue
			}
			out.Tasks = append(out.Tasks, task)
			automatic.Tasks = append(automatic.Tasks, task)
			added = append(added, task.ID)
		} else if auto >= 0 && reflect.DeepEqual(out.Tasks[index], automatic.Tasks[auto]) && !reflect.DeepEqual(out.Tasks[index], task) {
			out.Tasks[index] = task
			automatic.Tasks[auto] = task
			updated = append(updated, task.ID)
		}
	}
	return out, automatic, added, updated
}
func (m *Maintainer) writeProfile(wd string, profile companion.Contract) error {
	path, _, err := m.paths(wd)
	if err != nil {
		return err
	}
	raw := map[string]any{"version": int64(1)}
	if b, err := atomicfile.ReadFile(path); err == nil {
		if _, err := toml.Decode(string(b), &raw); err != nil {
			return err
		}
	} else if !errors.Is(err, os.ErrNotExist) {
		return err
	}
	raw["purpose"], raw["responsibilities"], raw["tasks"] = profile.Purpose, profile.Responsibilities, profile.Tasks
	var b bytes.Buffer
	if err := toml.NewEncoder(&b).Encode(raw); err != nil {
		return err
	}
	stage, err := os.MkdirTemp("", "magi-maintenance-check-")
	if err != nil {
		return err
	}
	defer os.RemoveAll(stage)
	staged, err := Path(stage, wd)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(staged), 0700); err != nil {
		return err
	}
	if err := atomicfile.Write(staged, b.Bytes(), 0600); err != nil {
		return err
	}
	if _, err := Load(stage, wd); err != nil {
		return err
	}
	return atomicfile.Write(path, b.Bytes(), 0600)
}

// QueueProfileMaintenance coalesces repeated requests without replacing newer evidence.
func (m *Maintainer) QueueProfileMaintenance(ctx context.Context, wd, source string, through int64) error {
	if source == "" || len(source) > 128 || through <= 0 {
		return fmt.Errorf("invalid maintenance source")
	}
	unlock, err := m.lock(ctx, wd)
	if err != nil {
		return err
	}
	defer unlock()
	r, err := m.load(wd)
	if err != nil {
		return err
	}
	if r.State.Checkpoints[source] >= through || r.State.PendingSources[source] >= through {
		return nil
	}
	if _, known := r.State.Checkpoints[source]; !known {
		if _, queued := r.State.PendingSources[source]; !queued {
			sources := len(r.State.Checkpoints)
			for pending := range r.State.PendingSources {
				if _, processed := r.State.Checkpoints[pending]; !processed {
					sources++
				}
			}
			if sources >= 256 {
				return fmt.Errorf("maintenance source limit reached")
			}
		}
	}
	r.State.PendingSources[source] = through
	r.State.Pending = true
	return m.save(wd, r)
}

// AcknowledgeProfileMaintenance removes only work known to have been examined.
// A newer read queued during extraction must survive the older result.
func (m *Maintainer) AcknowledgeProfileMaintenance(ctx context.Context, wd, source string, through int64) error {
	unlock, err := m.lock(ctx, wd)
	if err != nil {
		return err
	}
	defer unlock()
	r, err := m.load(wd)
	if err != nil {
		return err
	}
	if pending, ok := r.State.PendingSources[source]; ok && pending <= through {
		delete(r.State.PendingSources, source)
		if r.State.Checkpoints[source] < through {
			r.State.Checkpoints[source] = through
		}
		r.State.Pending = len(r.State.PendingSources) > 0
		return m.save(wd, r)
	}
	return nil
}
