package companionpolicy

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/sayaya1090/magi/internal/atomicfile"
	"github.com/sayaya1090/magi/internal/core/companion"
	"github.com/sayaya1090/magi/internal/port"
)

func maintenanceFixture(t *testing.T) (*Maintainer, string) {
	t.Helper()
	return &Maintainer{ConfigDir: t.TempDir()}, t.TempDir()
}
func maintenanceBegin(t *testing.T, m *Maintainer, wd, source string, seq int64) port.ProfileMaintenanceTicket {
	t.Helper()
	s, err := m.ProfileMaintenanceState(context.Background(), wd)
	if err != nil {
		t.Fatal(err)
	}
	ticket, err := m.BeginProfileMaintenance(context.Background(), wd, s.Revision, source, seq, strings.Repeat("a", 64))
	if err != nil {
		t.Fatal(err)
	}
	return ticket
}
func TestMaintenanceProtectsHumanFieldsAndChangesOnlyOwnedTasks(t *testing.T) {
	ctx := context.Background()
	m, wd := maintenanceFixture(t)
	ticket := maintenanceBegin(t, m, wd, "source", 10)
	first := &companion.Contract{Purpose: "Workspace", Tasks: []companion.Task{{ID: "build", Description: "Build the project"}}}
	state, err := m.FinishProfileMaintenance(ctx, wd, ticket, first, "")
	if err != nil || state.LastError != "" || len(state.Current.Tasks) != 1 || state.Checkpoints["source"] != 10 {
		t.Fatal(state, err)
	}
	r, err := m.load(wd)
	if err != nil {
		t.Fatal(err)
	}
	next := companion.Contract{Purpose: "Updated workspace", Tasks: []companion.Task{{ID: "build", Description: "Build and verify"}, {ID: "test", Description: "Run tests"}}}
	out, auto, added, updated := mergeMaintenanceProfile(r.State.Current, r.Automatic, next)
	if out.Purpose != "Updated workspace" || len(added) != 1 || len(updated) != 1 || len(auto.Tasks) != 2 {
		t.Fatal(out, added, updated)
	}
	human := r.State.Current
	human.Purpose = "Human purpose"
	human.Tasks = []companion.Task{{ID: "build", Description: "Human edit"}}
	out, _, _, updated = mergeMaintenanceProfile(human, r.Automatic, next)
	if out.Purpose != "Human purpose" || out.Tasks[0].Description != "Human edit" || len(updated) != 0 {
		t.Fatal("human edit overwritten", out)
	}
	out, _, _, _ = mergeMaintenanceProfile(human, r.Automatic, companion.Contract{})
	if len(out.Tasks) != 1 {
		t.Fatal("missing proposal deleted a task")
	}
	deleted := human
	deleted.Tasks = nil
	out, _, _, _ = mergeMaintenanceProfile(deleted, r.Automatic, *first)
	if len(out.Tasks) != 0 {
		t.Fatal("user-deleted automatic task resurrected")
	}
}
func TestMaintenanceLeaseCASAndAuthorityFailureAreDurable(t *testing.T) {
	ctx := context.Background()
	m, wd := maintenanceFixture(t)
	ticket := maintenanceBegin(t, m, wd, "source", 10)
	state, err := m.ProfileMaintenanceState(ctx, wd)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := m.BeginProfileMaintenance(ctx, wd, state.Revision, "other", 11, strings.Repeat("b", 64)); err == nil {
		t.Fatal("concurrent extraction accepted")
	}
	state, err = m.FinishProfileMaintenance(ctx, wd, ticket, &companion.Contract{Purpose: "bad", Operations: []companion.Operation{{ID: "grant"}}}, "")
	if err != nil || state.LastError == "" || state.Checkpoints["source"] != 0 || state.Running {
		t.Fatal(state, err)
	}
	reopened := &Maintainer{ConfigDir: m.ConfigDir}
	state, err = reopened.ProfileMaintenanceState(ctx, wd)
	if err != nil || len(state.History) != 1 || state.History[0].Status != "failed" {
		t.Fatal(state, err)
	}
	if _, err := m.BeginProfileMaintenance(ctx, wd, state.Revision, "other", 11, strings.Repeat("b", 64)); err == nil {
		t.Fatal("restart bypassed cooldown")
	}
	if _, err := m.FinishProfileMaintenance(ctx, wd, ticket, nil, ""); err == nil {
		t.Fatal("replayed ticket accepted")
	}
}
func TestMaintenanceConcurrentUserChangeRejectsProposal(t *testing.T) {
	ctx := context.Background()
	m, wd := maintenanceFixture(t)
	ticket := maintenanceBegin(t, m, wd, "source", 5)
	path, _, err := m.paths(wd)
	if err != nil {
		t.Fatal(err)
	}
	if err := atomicfile.Write(path, []byte("version = 1\npurpose = 'Human change'\n"), 0600); err != nil {
		t.Fatal(err)
	}
	state, err := m.FinishProfileMaintenance(ctx, wd, ticket, &companion.Contract{Purpose: "model", Tasks: []companion.Task{{ID: "build", Description: "Build"}}}, "")
	if err != nil || state.LastError == "" || state.Current.Purpose != "Human change" || len(state.Current.Tasks) > 0 {
		t.Fatal(state, err)
	}
}
func TestMaintenanceCrashExpiryAndCorruptSession(t *testing.T) {
	ctx := context.Background()
	m, wd := maintenanceFixture(t)
	ticket := maintenanceBegin(t, m, wd, "source", 1)
	r, err := m.load(wd)
	if err != nil {
		t.Fatal(err)
	}
	r.LeaseUntil = time.Now().Add(-time.Second)
	if err := m.save(wd, r); err != nil {
		t.Fatal(err)
	}
	if _, err := m.FinishProfileMaintenance(ctx, wd, ticket, nil, ""); err == nil {
		t.Fatal("expired lease published")
	}
	_, path, err := m.paths(wd)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte("broken"), 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := m.ProfileMaintenanceState(ctx, wd); err == nil {
		t.Fatal("corrupt session silently replaced")
	}
	b, err := os.ReadFile(path)
	if err != nil || string(b) != "broken" {
		t.Fatal(err)
	}
	if filepath.Base(path) != "maintenance.json" {
		t.Fatal(path)
	}
}
func TestMaintenanceRejectsInvalidMetadataWithoutPublishing(t *testing.T) {
	ctx := context.Background()
	m, wd := maintenanceFixture(t)
	ticket := maintenanceBegin(t, m, wd, "source", 1)
	state, err := m.FinishProfileMaintenance(ctx, wd, ticket, &companion.Contract{Purpose: "workspace", Tasks: []companion.Task{{ID: "INVALID", Description: "bad"}}}, "")
	if err != nil || state.LastError == "" || len(state.Current.Tasks) > 0 {
		t.Fatal(state, err)
	}
}

func TestMaintenancePublishesFollowupAndPreservesDeclaredAuthority(t *testing.T) {
	ctx := context.Background()
	m, wd := maintenanceFixture(t)
	writePolicy(t, m.ConfigDir, wd, wd, "")
	first := maintenanceBegin(t, m, wd, "first-source", 10)
	state, err := m.FinishProfileMaintenance(ctx, wd, first, &companion.Contract{Purpose: "Guessed purpose", Tasks: []companion.Task{{ID: "build", Description: "Build and report status"}}}, "")
	if err != nil || state.LastError != "" || state.Current.Purpose != "Inspect plugin sources" {
		t.Fatal(state, err)
	}
	// Advance the persisted cooldown, then reopen the session as another process.
	r, err := m.load(wd)
	if err != nil {
		t.Fatal(err)
	}
	r.State.LastAttempt = time.Now().Add(-11 * time.Minute)
	if err := m.save(wd, r); err != nil {
		t.Fatal(err)
	}
	reopened := &Maintainer{ConfigDir: m.ConfigDir}
	before, err := reopened.ProfileMaintenanceState(ctx, wd)
	if err != nil {
		t.Fatal(err)
	}
	ticket, err := reopened.BeginProfileMaintenance(ctx, wd, before.Revision, "second-source", 20, strings.Repeat("b", 64))
	if err != nil {
		t.Fatal(err)
	}
	state, err = reopened.FinishProfileMaintenance(ctx, wd, ticket, &companion.Contract{Purpose: "Another guess", Tasks: []companion.Task{{ID: "build", Description: "Build and report compiler diagnostics"}, {ID: "test", Description: "Run tests and return failures"}}}, "")
	if err != nil || state.LastError != "" || len(state.Current.Tasks) != 2 || state.Current.Tasks[0].Description != "Build and report compiler diagnostics" || len(state.History) != 2 || len(state.History[1].Added) != 1 || len(state.History[1].Updated) != 1 {
		t.Fatal(state, err)
	}
	if len(state.Current.Operations) != 1 || len(state.Current.Instructions) != 1 || state.Current.Instructions[0] != "Do not deploy" || state.Current.Purpose != "Inspect plugin sources" {
		t.Fatal("authority or human metadata changed", state.Current)
	}
}
