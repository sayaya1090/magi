package app

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/sayaya1090/magi/internal/core/event"
	"github.com/sayaya1090/magi/internal/core/session"
	"github.com/sayaya1090/magi/internal/port"
)

func TestScopedExploreChildReadsOnlyItsDelegatedDirectory(t *testing.T) {
	llm := &scriptLLM{steps: [][]port.ProviderEvent{
		asideCall("read", `{"path":"inside.txt"}`),
		asideCall("read", `{"path":"../outside.txt"}`),
		asideCall("read", `{"path":"alias.txt"}`),
		asideText("done"),
	}}
	a, parent, _ := spawnApp(t, llm)
	dir := filepath.Join(parent.Workdir, "scope")
	if err := os.Mkdir(dir, 0700); err != nil {
		t.Fatal(err)
	}
	for path, body := range map[string]string{
		filepath.Join(dir, "inside.txt"):             "visible-fixture",
		filepath.Join(parent.Workdir, "outside.txt"): "outside-fixture",
	} {
		if err := os.WriteFile(path, []byte(body), 0600); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.Symlink("../outside.txt", filepath.Join(dir, "alias.txt")); err != nil {
		t.Fatal(err)
	}
	hooks := a.spawnFnFor(0, parent, event.Actor{Kind: event.ActorAgent, ID: "coder"}, "c1", "explore")
	res, err := hooks.Spawn(port.WithExploreReadScope(context.Background(), "scope"), port.SpawnSpec{
		Prompt: "find evidence", Tools: []string{"read"},
	})
	if err != nil || res.Err != "" {
		t.Fatalf("explore: %+v %v", res, err)
	}
	events, err := a.store.Read(context.Background(), session.SessionID(res.SessionID), 0)
	if err != nil {
		t.Fatal(err)
	}
	var results strings.Builder
	for _, ev := range events {
		if ev.Type == event.TypePartAppended {
			results.Write(ev.Data)
		}
	}
	out := results.String()
	if !strings.Contains(out, "visible-fixture") || strings.Contains(out, "outside-fixture") || strings.Count(out, "outside workdir") != 2 {
		t.Fatalf("scoped tool results: %s", out)
	}
}
