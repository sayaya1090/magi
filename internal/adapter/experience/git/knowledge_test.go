package git

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/sayaya1090/magi/internal/port"
)

func TestKnowledgeUserActionsRespectRevisionAndRedactEvidence(t *testing.T) {
	s, ref, now := activityStore(t)
	ctx := context.Background()
	if err := s.ProtectExperience(ctx, ref, "private-turn", now.Add(time.Minute)); err != nil {
		t.Fatal(err)
	}
	if err := s.ConfirmExperienceApplication(ctx, ref, "private-turn:call", "private-session"); err != nil {
		t.Fatal(err)
	}
	list, err := s.ManageKnowledge(ctx, port.KnowledgeAction{})
	if err != nil || len(list) != 1 {
		t.Fatalf("inventory %+v %v", list, err)
	}
	item := list[0]
	if !item.Managed || !item.Protected || item.ApplicationCount != 1 || item.Revision != ref.ContentRevision {
		t.Fatal(item)
	}
	raw, err := json.Marshal(list)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(raw), "private-") {
		t.Fatal("private evidence leaked", string(raw))
	}
	for _, action := range []string{"pin", "unpin", "archive", "restore"} {
		list, err = s.ManageKnowledge(ctx, port.KnowledgeAction{Name: item.Name, Revision: item.Revision, Action: action})
		if err != nil || len(list) != 1 {
			t.Fatalf("%s %+v %v", action, list, err)
		}
		if action == "archive" && !list[0].Archived {
			t.Fatal("archived entry disappeared")
		}
	}
	for _, bad := range []port.KnowledgeAction{
		{Name: item.Name, Revision: "stale", Action: "archive"},
		{Name: item.Name, Action: "archive"},
		{Name: "../other", Revision: item.Revision, Action: "archive"},
		{Name: item.Name, Revision: item.Revision, Action: "delete"},
	} {
		if _, err := s.ManageKnowledge(ctx, bad); err == nil {
			t.Fatal("invalid action accepted", bad)
		}
	}
	current, err := s.ManageKnowledge(ctx, port.KnowledgeAction{})
	if err != nil || current[0].Archived {
		t.Fatal("refused action mutated retention", current, err)
	}
}
