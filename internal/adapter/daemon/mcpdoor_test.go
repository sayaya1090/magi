package daemon

import (
	"context"
	"strings"
	"testing"
)

// attachEngine is an engine that can attach tool servers, and remembers what it was asked.
type attachEngine struct {
	fakeEngine
	gotName, gotURL string
	gotHeaders      map[string]string
	tools           []string
	err             error
	detached        bool
	hadIt           bool
}

func (e *attachEngine) AttachToolServer(_ context.Context, name, url string, h map[string]string) ([]string, error) {
	e.gotName, e.gotURL, e.gotHeaders = name, url, h
	return e.tools, e.err
}

func (e *attachEngine) DetachToolServer(name string) bool {
	e.detached = true
	e.gotName = name
	return e.hadIt
}

// The door answers with the tools, because that is what the caller can act on. An add-in that
// attached and got "ok" cannot tell whether the companion now has its render tool.
func TestAttachDoorAnswersWithTheTools(t *testing.T) {
	eng := &attachEngine{tools: []string{"mcp__ppt__render", "mcp__ppt__open"}}
	resp := answerMCPAttach(context.Background(), eng, Request{
		Method: "mcp-attach", Name: "ppt", URL: "http://127.0.0.1:9/mcp",
		Headers: map[string]string{"Authorization": "Bearer x"},
	})
	if !resp.OK {
		t.Fatalf("refused: %s", resp.Err)
	}
	if len(resp.Tools) != 2 {
		t.Fatalf("answered %v — the names are the evidence", resp.Tools)
	}
	if eng.gotName != "ppt" || eng.gotURL != "http://127.0.0.1:9/mcp" {
		t.Errorf("passed %q %q", eng.gotName, eng.gotURL)
	}
	if eng.gotHeaders["Authorization"] != "Bearer x" {
		t.Error("headers did not reach the attach — that is how a helper authenticates")
	}
}

// A server that attached and offers nothing is not the same as a refusal, and JSON null is not the
// same as an empty list to the client reading it.
func TestAttachWithNoToolsAnswersAnEmptyList(t *testing.T) {
	resp := answerMCPAttach(context.Background(), &attachEngine{}, Request{Name: "x", URL: "http://h/"})
	if !resp.OK || resp.Tools == nil || len(resp.Tools) != 0 {
		t.Fatalf("ok=%v tools=%v — attached and offering nothing is still attached", resp.OK, resp.Tools)
	}
}

// A daemon built without the door says so, which a caller can tell from a server that refused.
func TestADaemonWithoutTheDoorSaysSo(t *testing.T) {
	resp := answerMCPAttach(context.Background(), &fakeEngine{}, Request{Name: "x", URL: "http://h/"})
	if resp.OK || !strings.Contains(resp.Err, "cannot attach") {
		t.Fatalf("answered ok=%v err=%q", resp.OK, resp.Err)
	}
}

// Detach reports whether there was anything to remove: a helper reconnecting after its own crash
// wants to know whether it cleaned up or was already clean.
func TestDetachSaysWhetherThereWasOne(t *testing.T) {
	had := &attachEngine{hadIt: true}
	if resp := answerMCPDetach(context.Background(), had, Request{Name: "ppt"}); !resp.OK {
		t.Fatalf("removing an attached server failed: %s", resp.Err)
	}
	none := &attachEngine{hadIt: false}
	resp := answerMCPDetach(context.Background(), none, Request{Name: "ppt"})
	if resp.OK || !strings.Contains(resp.Err, "no tool server") {
		t.Fatalf("answered ok=%v err=%q for a name nothing holds", resp.OK, resp.Err)
	}
}

// The refusal names what IS accepted, and the two new methods are in it.
func TestTheDoorIsInTheAcceptedList(t *testing.T) {
	list := acceptedMethods()
	for _, m := range []string{"mcp-attach", "mcp-detach"} {
		if !strings.Contains(list, m) {
			t.Errorf("%q missing from the accepted methods: %s", m, list)
		}
	}
}
