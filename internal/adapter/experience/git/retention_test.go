package git

import (
	"context"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/sayaya1090/magi/internal/port"
)

func TestLocalRetentionColdCandidatesPinArchiveRestore(t *testing.T) {
	ctx := context.Background()
	s := New(t.TempDir())
	s.retentionDir = t.TempDir()
	now := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	s.retentionNow = func() time.Time { return now }
	c := managedChange()
	receipt, err := s.ApplyExperience(ctx, c)
	if err != nil {
		t.Fatal(err)
	}
	status := func(action string) LocalRetention {
		t.Helper()
		r, err := s.LocalExperienceRetention(ctx, c.ObjectID, receipt.ContentRevision, action)
		if err != nil {
			t.Fatal(err)
		}
		return r
	}
	first := status("")
	if first.Cold || first.ArchiveCandidate {
		t.Fatal(first)
	}
	now = now.Add(30 * 24 * time.Hour)
	if r := status(""); !r.Cold || r.ArchiveCandidate || !r.Since.Equal(first.Since) {
		t.Fatal(r)
	}
	// Repeated reads are not applications.
	for i := 0; i < 3; i++ {
		if _, _, err := s.Retrieve(ctx, "compiler", nil); err != nil {
			t.Fatal(err)
		}
	}
	now = now.Add(60 * 24 * time.Hour)
	if r := status(""); !r.Cold || !r.ArchiveCandidate {
		t.Fatal(r)
	}
	// Becoming a candidate does not hide or physically remove anything.
	mem, _, err := s.Retrieve(ctx, "compiler", nil)
	if err != nil || len(mem) != 1 {
		t.Fatalf("candidate lost: %v %v", mem, err)
	}
	if r := status("pin"); r.Cold || r.ArchiveCandidate || !r.Pinned {
		t.Fatal(r)
	}
	if r := status("unpin"); !r.ArchiveCandidate {
		t.Fatal(r)
	}
	status("archive")
	mem, _, err = s.Retrieve(ctx, "compiler", nil)
	if err != nil || len(mem) != 0 {
		t.Fatalf("archive still recalled: %v %v", mem, err)
	}
	inv, err := s.Inventory(ctx)
	if err != nil || len(inv) != 1 || !inv[0].Retention.Archived {
		t.Fatalf("archive not manageable: %v %v", inv, err)
	}
	if r := status("restore"); r.Archived || r.Cold || !r.Since.Equal(now) {
		t.Fatal(r)
	}
	// A device-local action cannot restore withdrawn source knowledge.
	if err := s.Forget(ctx, "managed:"+c.ObjectID); err != nil {
		t.Fatal(err)
	}
	if _, err := s.LocalExperienceRetention(ctx, c.ObjectID, receipt.ContentRevision, "restore"); err == nil {
		t.Fatal("restored withdrawn source")
	}
}

func TestLocalRetentionNewDeviceRevisionAndCorruption(t *testing.T) {
	ctx := context.Background()
	s := New(t.TempDir())
	s.retentionDir = t.TempDir()
	now := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	s.retentionNow = func() time.Time { return now }
	c := managedChange()
	r, err := s.ApplyExperience(ctx, c)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = s.LocalExperienceRetention(ctx, c.ObjectID, r.ContentRevision, ""); err != nil {
		t.Fatal(err)
	}
	now = now.Add(100 * 24 * time.Hour)
	// A second device does not inherit another device's aging or pin decisions.
	peer := New(s.dir)
	peer.retentionDir = t.TempDir()
	peer.retentionNow = s.retentionNow
	p, err := peer.LocalExperienceRetention(ctx, c.ObjectID, r.ContentRevision, "")
	if err != nil || p.Cold {
		t.Fatalf("no new-device grace: %v %v", p, err)
	}
	// Another successful observation of the SAME content must not renew lifetime.
	c.ExpectedRevision = r.Revision
	c.OperationID = "same-content-observation"
	repeated, err := s.ApplyExperience(ctx, c)
	if err != nil {
		t.Fatal(err)
	}
	if repeated.ContentRevision != r.ContentRevision {
		t.Fatal("observation changed content identity")
	}
	r = repeated
	aged, err := s.LocalExperienceRetention(ctx, c.ObjectID, r.ContentRevision, "")
	if err != nil || !aged.Cold {
		t.Fatalf("repeated observation renewed lifetime: %v %v", aged, err)
	}
	// Corrected content gets a new observation period, old UI revisions cannot act.
	c.ExpectedRevision = r.Revision
	c.OperationID = "corrected"
	c.Memory = &port.Memory{Text: "compiler corrected"}
	next, err := s.ApplyExperience(ctx, c)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = s.LocalExperienceRetention(ctx, c.ObjectID, r.ContentRevision, "pin"); err == nil {
		t.Fatal("stale action accepted")
	}
	updated, err := s.LocalExperienceRetention(ctx, c.ObjectID, next.ContentRevision, "")
	if err != nil || updated.Cold {
		t.Fatalf("new content aged immediately: %v %v", updated, err)
	}
	path, err := s.retentionPath(c.ObjectID)
	if err != nil {
		t.Fatal(err)
	}
	if err = os.WriteFile(path, []byte("broken"), 0600); err != nil {
		t.Fatal(err)
	}
	if _, err = s.LocalExperienceRetention(ctx, c.ObjectID, next.ContentRevision, "restore"); err == nil {
		t.Fatal("corrupt local state overwritten")
	}
	b, _ := os.ReadFile(path)
	if string(b) != "broken" {
		t.Fatal("corrupt evidence lost")
	}
}

func TestColdIsFallbackAcrossMemoryAndSkillCaps(t *testing.T) {
	mem := []Scored[port.Memory]{{Score: 100, Cold: true, V: port.Memory{Text: "cold"}}, {Score: 1, V: port.Memory{Text: "active"}}}
	if got := topMemories(mem, 1); len(got) != 1 || got[0].Text != "active" {
		t.Fatal(got)
	}
	sk := []Scored[port.Skill]{{Score: 100, Cold: true, V: port.Skill{Name: "cold"}}, {Score: 1, V: port.Skill{Name: "active"}}}
	if got := topSkills(sk, 1); len(got) != 1 || got[0].Name != "active" {
		t.Fatal(got)
	}
	if got := topSkills(sk, 3); len(got) != 2 || got[1].Name != "cold" {
		t.Fatal(got)
	}
}

func TestLocalRetentionConcurrentUserActionsRemainRecoverable(t *testing.T) {
	ctx := context.Background()
	s := New(t.TempDir())
	s.retentionDir = t.TempDir()
	c := managedChange()
	r, err := s.ApplyExperience(ctx, c)
	if err != nil {
		t.Fatal(err)
	}
	var wg sync.WaitGroup
	errs := make(chan error, 20)
	for i := 0; i < 20; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			other := New(s.dir)
			other.retentionDir = s.retentionDir
			action := "pin"
			if i%2 == 0 {
				action = "unpin"
			}
			_, err := other.LocalExperienceRetention(ctx, c.ObjectID, r.ContentRevision, action)
			errs <- err
		}(i)
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		if err != nil {
			t.Fatal(err)
		}
	}
	if _, err := s.LocalExperienceRetention(ctx, c.ObjectID, r.ContentRevision, "restore"); err != nil {
		t.Fatal(err)
	}
	// Local preferences are outside the replicable/Git-backed experience root.
	if _, err := os.Stat(filepath.Join(s.dir, "managed", c.ObjectID+".json")); err != nil {
		t.Fatal(err)
	}
	path, err := s.retentionPath(c.ObjectID)
	if err != nil || filepath.Dir(path) != s.retentionDir {
		t.Fatalf("wrong local directory: %s %v", path, err)
	}
}
