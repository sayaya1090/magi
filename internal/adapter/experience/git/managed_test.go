package git

import (
	"context"
	"sync"
	"testing"

	"github.com/sayaya1090/magi/internal/port"
)

func managedChange() port.ExperienceChange {
	return port.ExperienceChange{ObjectID: "stable-object", OperationID: "op1", ObservationID: "observation1", Owner: "plugin:engram", Kind: "memory", State: "active", Memory: &port.Memory{Text: "compiler port memory original"}, Source: "plugin:engram"}
}

func TestManagedExperienceCorrectionWithdrawalAndRetry(t *testing.T) {
	ctx := context.Background()
	s := New(t.TempDir())
	c := managedChange()
	first, err := s.ApplyExperience(ctx, c)
	if err != nil {
		t.Fatal(err)
	}
	retry, err := s.ApplyExperience(ctx, c)
	if err != nil || retry != first {
		t.Fatalf("retry: %+v %v", retry, err)
	}
	c.OperationID = "op2"
	c.ExpectedRevision = first.Revision
	c.Memory = &port.Memory{Text: "compiler port memory corrected"}
	second, err := s.ApplyExperience(ctx, c)
	if err != nil {
		t.Fatal(err)
	}
	mem, _, err := s.Retrieve(ctx, "compiler", nil)
	if err != nil || len(mem) != 1 || mem[0].Text != c.Memory.Text {
		t.Fatalf("correction duplicated/stale: %+v %v", mem, err)
	}
	stale := c
	stale.OperationID = "stale"
	if _, err := s.ApplyExperience(ctx, stale); err == nil {
		t.Fatal("stale update accepted")
	}
	c.OperationID = "withdraw"
	c.State = "withdrawn"
	c.ExpectedRevision = second.Revision
	c.Memory = nil
	withdrawn, err := s.ApplyExperience(ctx, c)
	if err != nil {
		t.Fatal(err)
	}
	retry, err = s.ApplyExperience(ctx, c)
	if err != nil || retry != withdrawn {
		t.Fatalf("withdraw retry: %+v %v", retry, err)
	}
	mem, _, err = s.Retrieve(ctx, "compiler", nil)
	if err != nil || len(mem) != 0 {
		t.Fatalf("withdrawn object still retrieved: %+v %v", mem, err)
	}
	c.OperationID = "restore"
	c.State = "active"
	c.ExpectedRevision = withdrawn.Revision
	c.Memory = &port.Memory{Text: "compiler port restored"}
	if _, err := s.ApplyExperience(ctx, c); err != nil {
		t.Fatal(err)
	}
	inv, err := s.Inventory(ctx)
	if err != nil || len(inv) != 1 || inv[0].Name != "managed:stable-object" {
		t.Fatalf("managed inventory: %+v %v", inv, err)
	}
	if err := s.Forget(ctx, inv[0].Name); err != nil {
		t.Fatal(err)
	}
	mem, _, err = s.Retrieve(ctx, "compiler", nil)
	if err != nil || len(mem) != 0 {
		t.Fatalf("forgotten managed object still active: %+v %v", mem, err)
	}
}

func TestManagedExperienceRejectsForeignOwnerAndOperationReuse(t *testing.T) {
	ctx := context.Background()
	s := New(t.TempDir())
	c := managedChange()
	r, err := s.ApplyExperience(ctx, c)
	if err != nil {
		t.Fatal(err)
	}
	c.Memory = &port.Memory{Text: "different"}
	if _, err := s.ApplyExperience(ctx, c); err == nil {
		t.Fatal("operation reuse accepted")
	}
	c.OperationID = "foreign"
	c.ExpectedRevision = r.Revision
	c.Owner = "plugin:other"
	if _, err := s.ApplyExperience(ctx, c); err == nil {
		t.Fatal("foreign owner accepted")
	}
	c = managedChange()
	c.ObjectID = "../escape"
	if _, err := s.ApplyExperience(ctx, c); err == nil {
		t.Fatal("unsafe ID accepted")
	}
}

func TestManagedExperienceTwoStoreInstancesCAS(t *testing.T) {
	ctx := context.Background()
	dir := t.TempDir()
	s := New(dir)
	c := managedChange()
	r, err := s.ApplyExperience(ctx, c)
	if err != nil {
		t.Fatal(err)
	}
	start := make(chan struct{})
	results := make(chan error, 2)
	var wg sync.WaitGroup
	for _, op := range []string{"a", "b"} {
		wg.Add(1)
		go func(op string) {
			defer wg.Done()
			<-start
			next := c
			next.ExpectedRevision = r.Revision
			next.OperationID = op
			_, err := New(dir).ApplyExperience(ctx, next)
			results <- err
		}(op)
	}
	close(start)
	wg.Wait()
	close(results)
	success := 0
	for err := range results {
		if err == nil {
			success++
		}
	}
	if success != 1 {
		t.Fatalf("one CAS writer must win, got %d", success)
	}
}

func TestManagedAutomaticSkillRejectsUnverified(t *testing.T) {
	c := managedChange()
	c.Kind = "skill"
	c.Memory = nil
	c.Skill = &port.Skill{Name: "compiler", Body: "run compiler"}
	c.HostOutcome = "error"
	if _, err := New(t.TempDir()).ApplyExperience(context.Background(), c); err == nil {
		t.Fatal("unverified skill accepted")
	}
}

func TestManagedNewObservationDoesNotChangeContentIdentity(t *testing.T) {
	ctx := context.Background()
	s := New(t.TempDir())
	c := managedChange()
	first, err := s.ApplyExperience(ctx, c)
	if err != nil {
		t.Fatal(err)
	}
	before, _, err := s.Retrieve(ctx, "compiler", nil)
	if err != nil {
		t.Fatal(err)
	}
	c.ExpectedRevision = first.Revision
	c.OperationID = "independent-op"
	c.ObservationID = "independent-observation"
	second, err := s.ApplyExperience(ctx, c)
	if err != nil {
		t.Fatal(err)
	}
	after, _, err := s.Retrieve(ctx, "compiler", nil)
	if err != nil {
		t.Fatal(err)
	}
	if first.Revision == second.Revision || first.ContentRevision != second.ContentRevision || len(before) != 1 || len(after) != 1 || before[0].ID != after[0].ID {
		t.Fatalf("observation changed content identity: %+v %+v %+v %+v", first, second, before, after)
	}
}
