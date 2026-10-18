package git

import (
	"context"
	"testing"
	"time"

	"github.com/sayaya1090/magi/internal/port"
)

func activityStore(t *testing.T) (*Store, port.ExperienceReference, *time.Time) {
	t.Helper()
	s := New(t.TempDir())
	s.retentionDir = t.TempDir()
	now := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	s.retentionNow = func() time.Time { return now }
	c := managedChange()
	c.Kind = "skill"
	c.Memory = nil
	c.HostOutcome = "verified"
	c.Skill = &port.Skill{Name: "compiler", Body: "procedure", DocumentDigest: "documenthash", VerificationDigest: "scripthash"}
	if _, err := s.ApplyExperience(context.Background(), c); err != nil {
		t.Fatal(err)
	}
	refs, err := s.ExperienceReferences(context.Background(), "compiler")
	if err != nil || len(refs) != 1 {
		t.Fatalf("refs %v %v", refs, err)
	}
	if _, err := s.LocalExperienceRetention(context.Background(), refs[0].ObjectID, refs[0].ContentRevision, ""); err != nil {
		t.Fatal(err)
	}
	return s, refs[0], &now
}
func TestActivityProtectionExpiresAndReferencesAreIndependent(t *testing.T) {
	s, ref, now := activityStore(t)
	ctx := context.Background()
	*now = now.Add(100 * 24 * time.Hour)
	state := func() LocalRetention {
		t.Helper()
		r, e := s.LocalExperienceRetention(ctx, ref.ObjectID, ref.ContentRevision, "")
		if e != nil {
			t.Fatal(e)
		}
		return r
	}
	if !state().ArchiveCandidate {
		t.Fatal("not aged")
	}
	for _, id := range []string{"turn-one", "turn-two"} {
		if err := s.ProtectExperience(ctx, ref, id, now.Add(time.Minute)); err != nil {
			t.Fatal(err)
		}
	}
	if r := state(); !r.Protected || r.Cold || r.ArchiveCandidate {
		t.Fatal(r)
	}
	if err := s.ReleaseExperience(ctx, ref, "turn-one"); err != nil {
		t.Fatal(err)
	}
	if !state().Protected {
		t.Fatal("one turn released another turn's reference")
	}
	*now = now.Add(2 * time.Minute)
	if r := state(); r.Protected || !r.Cold || !r.ArchiveCandidate {
		t.Fatal("crash left permanent protection", r)
	}
	if err := s.ProtectExperience(ctx, ref, "forever", now.Add(24*time.Hour)); err == nil {
		t.Fatal("unbounded lease accepted")
	}
}
func TestVerifiedApplicationRenewsOnceAndRestoresLocalArchive(t *testing.T) {
	s, ref, now := activityStore(t)
	ctx := context.Background()
	*now = now.Add(100 * 24 * time.Hour)
	if _, err := s.LocalExperienceRetention(ctx, ref.ObjectID, ref.ContentRevision, "archive"); err != nil {
		t.Fatal(err)
	}
	if err := s.ProtectExperience(ctx, ref, "turn", now.Add(time.Minute)); err != nil {
		t.Fatal(err)
	}
	if err := s.ConfirmExperienceApplication(ctx, ref, "turn:call", "call:hash"); err != nil {
		t.Fatal(err)
	}
	first, err := s.LocalExperienceRetention(ctx, ref.ObjectID, ref.ContentRevision, "")
	if err != nil {
		t.Fatal(err)
	}
	if first.LastApplied.IsZero() || first.Cold || first.Archived || len(first.Applications) != 1 {
		t.Fatal(first)
	}
	*now = now.Add(10 * 24 * time.Hour)
	if err := s.ConfirmExperienceApplication(ctx, ref, "turn:call", "call:hash"); err != nil {
		t.Fatal(err)
	}
	second, _ := s.LocalExperienceRetention(ctx, ref.ObjectID, ref.ContentRevision, "")
	if !second.Since.Equal(first.Since) || !second.LastApplied.Equal(first.LastApplied) {
		t.Fatal("retry extended retention")
	}
	if err := s.ConfirmExperienceApplication(ctx, ref, "turn:call", "different"); err == nil {
		t.Fatal("conflicting receipt accepted")
	}
	forged := ref
	forged.VerificationDigest = "untrusted"
	if err := s.ConfirmExperienceApplication(ctx, forged, "new", "fake"); err == nil {
		t.Fatal("forged verifier accepted")
	}
	if err := s.Forget(ctx, "managed:"+ref.ObjectID); err != nil {
		t.Fatal(err)
	}
	if err := s.ConfirmExperienceApplication(ctx, ref, "new", "call"); err == nil {
		t.Fatal("verification resurrected withdrawn source")
	}
}

func TestManagedMemoryReferenceIsProtectedWithoutClaimingApplication(t *testing.T) {
	ctx := context.Background()
	s := New(t.TempDir())
	s.retentionDir = t.TempDir()
	now := time.Now().UTC()
	s.retentionNow = func() time.Time { return now }
	c := managedChange()
	receipt, err := s.ApplyExperience(ctx, c)
	if err != nil {
		t.Fatal(err)
	}
	refs, err := s.ManagedMemoryReferences(ctx, []string{"managed:" + c.ObjectID + "@" + receipt.ContentRevision})
	if err != nil || len(refs) != 1 {
		t.Fatalf("memory refs: %v %v", refs, err)
	}
	ref := refs[0]
	if err := s.ProtectExperience(ctx, ref, "task", now.Add(time.Minute)); err != nil {
		t.Fatal(err)
	}
	status, err := s.LocalExperienceRetention(ctx, ref.ObjectID, ref.ContentRevision, "")
	if err != nil || !status.Protected {
		t.Fatalf("memory unprotected: %v %v", status, err)
	}
	if err := s.ConfirmExperienceApplication(ctx, ref, "task:call", "claim"); err == nil {
		t.Fatal("memory retrieval became verified application")
	}
	wrong, err := s.ManagedMemoryReferences(ctx, []string{"managed:" + c.ObjectID + "@stale", "legacy-memory"})
	if err != nil || len(wrong) != 0 {
		t.Fatalf("wrong revision accepted: %v %v", wrong, err)
	}
	public := status.Public()
	if public.References != nil || public.Applications != nil {
		t.Fatal("private host receipts leaked")
	}
}

func TestApplicationsNeedAnActiveReferenceAndRevisionMatch(t *testing.T) {
	s, ref, now := activityStore(t)
	ctx := context.Background()
	if err := s.ConfirmExperienceApplication(ctx, ref, "turn:call", "exit0"); err == nil {
		t.Fatal("unreferenced use accepted")
	}
	if err := s.ProtectExperience(ctx, ref, "turn", now.Add(time.Minute)); err != nil {
		t.Fatal(err)
	}
	stale := ref
	stale.ContentRevision = "stale"
	if err := s.ConfirmExperienceApplication(ctx, stale, "turn:call", "exit0"); err == nil {
		t.Fatal("stale revision accepted")
	}
	*now = now.Add(2 * time.Minute)
	if err := s.ConfirmExperienceApplication(ctx, ref, "turn:call", "exit0"); err == nil {
		t.Fatal("expired task reference accepted")
	}
}
