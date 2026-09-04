package daemon

import (
	"context"
	"errors"
	"strings"
	"testing"
)

// attachEngine is an engine that can attach tool servers, and remembers what it was asked.
type attachEngine struct {
	// owner 는 문이 실어 온 주인. **적어 두지 않으면 「누구 것으로 붙였는가」가 시험 밖으로 샌다** —
	// 그 값이 이 인자가 생긴 이유다(SESSION_SCOPE.md).
	owner string
	fakeEngine
	gotName, gotURL string
	gotHeaders      map[string]string
	tools           []string
	err             error
	detachErr       error
	detached        bool
	hadIt           bool
}

func (e *attachEngine) AttachToolServer(_ context.Context, owner, name, url string, h map[string]string) ([]string, error) {
	e.owner = owner
	e.gotName, e.gotURL, e.gotHeaders = name, url, h
	return e.tools, e.err
}

func (e *attachEngine) DetachToolServer(name string) (bool, error) {
	e.detached = true
	e.gotName = name
	return e.hadIt, e.detachErr
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
	if !resp.OK || resp.Removed || resp.Err != "" {
		t.Fatalf("answered ok=%v removed=%v err=%q — already clean is the answer the caller wanted, "+
			"not a failure it has to parse", resp.OK, resp.Removed, resp.Err)
	}
}

// …and a refusal is neither of those. The door removes what the door attached, so a name that
// belongs to a server the operator declared comes back as an error, not as "there was none" —
// which would send a reconnecting helper on to attach under a name it can never have.
func TestDetachRefusedIsNotTheSameAsNothingToRemove(t *testing.T) {
	eng := &attachEngine{detachErr: errors.New("declared in this daemon's config")}
	resp := answerMCPDetach(context.Background(), eng, Request{Name: "ppt"})
	if resp.OK || resp.Err == "" {
		t.Fatalf("a refusal answered ok=%v err=%q", resp.OK, resp.Err)
	}
	if resp.Removed {
		t.Error("a refusal reported a removal")
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

// A client has to be able to ASK whether the door is there. An application that is itself a tool
// server attaches to whatever daemon is running — including one built before the door existed — and
// the only alternative to a handshake answer is calling the method to read the error back, which
// cannot tell "this build does not know it" from "this build refused you".
func TestTheHandshakeAdvertisesTheDoor(t *testing.T) {
	has := func(caps []string, want string) bool {
		for _, c := range caps {
			if c == want {
				return true
			}
		}
		return false
	}
	if !has(capsOf(&attachEngine{}), "tool-servers") {
		t.Errorf("a daemon that attaches did not say so: %v", capsOf(&attachEngine{}))
	}
	if _, ok := answers["mcp-attach"]; !ok {
		t.Error("…and the reverse would be worse: advertised and not dispatched")
	}
	// The capability belongs to the ENGINE, not to the build. The door is an optional interface, so
	// a daemon carrying an engine without it would otherwise advertise the door and then refuse —
	// which is the distinction the capability exists to make, reappearing one layer down. What the
	// caller needs before it offers a companion to a person is "this daemon will take it".
	if has(capsOf(struct{ Engine }{}), "tool-servers") {
		t.Error("a daemon that cannot attach advertised the door")
	}
	if !has(capsOf(struct{ Engine }{}), "handshake") {
		t.Error("the build-level floor went missing with it")
	}
}
