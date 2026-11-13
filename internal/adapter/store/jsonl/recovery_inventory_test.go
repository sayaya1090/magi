package jsonl

import (
	"context"
	"encoding/json"
	"github.com/sayaya1090/magi/internal/core/event"
	"github.com/sayaya1090/magi/internal/core/session"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestRecoveryInventoryReadsHeadersAndKeepsCloneOwnership(t *testing.T) {
	s := newStore(t)
	ctx := context.Background()
	for _, test := range []struct{ id, work, project, parent string }{
		{"root", wd, "", ""}, {"child", "/clone", wd, "root"}, {"foreign", "/another", "", ""},
	} {
		data, _ := json.Marshal(event.SessionCreatedData{Workdir: test.work, Project: test.project, Parent: test.parent})
		if _, err := s.Append(ctx, session.SessionID(test.id), event.Event{Type: event.TypeSessionCreated, TS: time.Now(), Data: data}); err != nil {
			t.Fatal(err)
		}
	}
	// Recovery topology does not parse bodies: an invalid tail cannot erase a valid header.
	f, err := os.OpenFile(filepath.Join(s.projectsDir(), encodeWorkdir(wd), "root.jsonl"), os.O_APPEND|os.O_WRONLY, 0600)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = f.WriteString("invalid body\n"); err != nil {
		t.Fatal(err)
	}
	if err = f.Close(); err != nil {
		t.Fatal(err)
	}
	metas, err := s.AllSessions(ctx, wd)
	if err != nil || len(metas) != 2 {
		t.Fatalf("inventory: %+v %v", metas, err)
	}
	for _, m := range metas {
		if m.ID == "child" && (m.Parent != "root" || m.Workdir != "/clone") {
			t.Fatalf("clone: %+v", m)
		}
	}
	canceled, cancel := context.WithCancel(ctx)
	cancel()
	if _, err = s.AllSessions(canceled, wd); err == nil {
		t.Fatal("ignored cancellation")
	}
}
