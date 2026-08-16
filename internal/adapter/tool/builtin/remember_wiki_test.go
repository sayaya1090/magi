package builtin

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/sayaya1090/magi/internal/port"
)

// remember{page:…} routes to the wiki, not the memories: one verb for "write this down", with the
// destination an argument — a second write tool would be a fork in the road where a weak model
// stalls choosing, and the waves showed what that costs.
func TestRememberWithAPageWritesTheWiki(t *testing.T) {
	var got port.Contribution
	env := port.ToolEnv{Propose: func(c port.Contribution) error { got = c; return nil }}
	res, err := (Remember{}).Execute(context.Background(), json.RawMessage(
		`{"text":"refresh is done by the sidecar","page":"auth flow","summary":"corrected owner","tags":["service map"]}`), env)
	if err != nil || res.IsError {
		t.Fatalf("err=%v res=%s", err, res.Content)
	}
	if len(got.Wiki) != 1 || len(got.Memories) != 0 {
		t.Fatalf("a page write must carry exactly one wiki edit and no memory, got %+v", got)
	}
	e := got.Wiki[0]
	if e.Page != "auth flow" || e.Summary != "corrected owner" || e.Text != "refresh is done by the sidecar" {
		t.Errorf("edit lost fields: %+v", e)
	}
	if len(e.Links) != 1 || e.Links[0] != "service map" {
		t.Errorf("tags must ride as links on a page write, got %v", e.Links)
	}
	if got.Source != "" {
		t.Errorf("the tool must leave Source for the app's companion stamp, got %q", got.Source)
	}
	if !strings.Contains(string(res.Content), "auth flow") {
		t.Errorf("the answer should name the page: %s", res.Content)
	}
}

// Without a page, nothing changes: the memory path is byte-for-byte the old contract.
func TestRememberWithoutAPageStaysAMemory(t *testing.T) {
	var got port.Contribution
	env := port.ToolEnv{Propose: func(c port.Contribution) error { got = c; return nil }}
	res, _ := (Remember{}).Execute(context.Background(), json.RawMessage(`{"text":"the timeout is 8s"}`), env)
	if res.IsError {
		t.Fatalf("%s", res.Content)
	}
	if len(got.Memories) != 1 || len(got.Wiki) != 0 || got.Source != "agent" {
		t.Fatalf("the memory path must be unchanged, got %+v", got)
	}
}
