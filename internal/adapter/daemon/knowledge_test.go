package daemon

import (
	"context"
	"encoding/json"
	expgit "github.com/sayaya1090/magi/internal/adapter/experience/git"
	"net"
	"testing"
	"time"

	"github.com/sayaya1090/magi/internal/port"
)

type knowledgeEngine struct {
	*fakeEngine
	action port.KnowledgeAction
}

func (e *knowledgeEngine) ManageKnowledge(_ context.Context, action port.KnowledgeAction) ([]port.KnowledgeItem, error) {
	e.action = action
	return []port.KnowledgeItem{{Name: "managed:one", Revision: "revision", Managed: true, Pinned: action.Action == "pin"}}, nil
}
func TestKnowledgeDoorCarriesDisplayedRevision(t *testing.T) {
	e := &knowledgeEngine{fakeEngine: &fakeEngine{}}
	r := answerKnowledge(context.Background(), e, Request{Args: json.RawMessage(`{"name":"managed:one","revision":"revision","action":"pin"}`)})
	if !r.OK || len(r.Knowledge) != 1 || !r.Knowledge[0].Pinned || e.action.Revision != "revision" {
		t.Fatal(r, e.action)
	}
	if r := answerKnowledge(context.Background(), e, Request{Args: json.RawMessage(`[]`)}); r.OK {
		t.Fatal("invalid action accepted")
	}
	if r := answerKnowledge(context.Background(), &fakeEngine{}, Request{}); r.OK {
		t.Fatal("unsupported engine accepted")
	}
}

type storedKnowledgeEngine struct {
	*fakeEngine
	port.KnowledgeManager
}

func TestKnowledgeControlOverConnectionRejectsStaleArchive(t *testing.T) {
	t.Setenv("MAGI_CONFIG_DIR", t.TempDir())
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	store := expgit.New(t.TempDir())
	if err := store.ImportLegacySkill(ctx, port.Skill{Name: "handmade", Body: "human procedure"}); err != nil {
		t.Fatal(err)
	}
	server, client := net.Pipe()
	defer client.Close()
	if err := client.SetDeadline(time.Now().Add(5 * time.Second)); err != nil {
		t.Fatal(err)
	}
	go serveConn(ctx, &storedKnowledgeEngine{fakeEngine: &fakeEngine{}, KnowledgeManager: store}, server, "", func() {}, func() {})
	encoder, decoder := json.NewEncoder(client), json.NewDecoder(client)
	exchange := func(args json.RawMessage) Response {
		t.Helper()
		if err := encoder.Encode(Request{Method: "knowledge", Args: args}); err != nil {
			t.Fatal(err)
		}
		var r Response
		if err := decoder.Decode(&r); err != nil {
			t.Fatal(err)
		}
		return r
	}
	initial := exchange(nil)
	if !initial.OK || len(initial.Knowledge) != 1 || !initial.Knowledge[0].Pinned {
		t.Fatal(initial)
	}
	item := initial.Knowledge[0]
	bad, _ := json.Marshal(port.KnowledgeAction{Name: item.Name, Revision: "stale", Action: "archive"})
	if refused := exchange(bad); refused.OK {
		t.Fatal("stale revision accepted over connection")
	}
	good, _ := json.Marshal(port.KnowledgeAction{Name: item.Name, Revision: item.Revision, Action: "archive"})
	changed := exchange(good)
	if !changed.OK || len(changed.Knowledge) != 1 || !changed.Knowledge[0].Archived {
		t.Fatal(changed)
	}
}
