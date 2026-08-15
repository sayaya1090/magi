package daemon

import (
	"context"
	"reflect"
	"strings"
	"testing"

	"github.com/sayaya1090/magi/internal/core/cluster"
)

// jsonTags returns the set of json field names on a struct type — the tag before any comma, a field
// with `json:"-"` skipped, an untagged field falling back to its Go name.
func jsonTags(typ reflect.Type) map[string]bool {
	out := map[string]bool{}
	for i := 0; i < typ.NumField(); i++ {
		f := typ.Field(i)
		name := f.Name
		if tag := f.Tag.Get("json"); tag != "" {
			if c := strings.IndexByte(tag, ','); c >= 0 {
				tag = tag[:c]
			}
			if tag == "-" {
				continue
			}
			if tag != "" {
				name = tag
			}
		}
		out[name] = true
	}
	return out
}

// requireFields asserts every named json field is present on typ. It is an ADDITIVE-ONLY guard:
// adding a field is fine (not listed here, no failure), but removing or renaming one this lists fails
// — an older peer on the other end of the relay reads by these names, and encoding/json drops what it
// cannot match without a word, turning a shape change into silent wrong behaviour rather than an error.
func requireFields(t *testing.T, what string, typ reflect.Type, want ...string) {
	t.Helper()
	have := jsonTags(typ)
	for _, w := range want {
		if !have[w] {
			t.Errorf("%s no longer carries the %q json field — removing or renaming a wire field breaks "+
				"an older peer that reads it (additive-only: add fields, never drop them)", what, w)
		}
	}
}

// The wire structs are the contract between two possibly-different versions. These fields must not be
// removed or renamed; new ones may be added freely — that is the whole point of the negotiation. If
// this test fails because you ADDED a field, you will not see it here (additions are allowed); it
// fails only when a listed field is gone.
func TestWireStructsAreAdditiveOnly(t *testing.T) {
	requireFields(t, "daemon.Request", reflect.TypeOf(Request{}),
		"method", "session", "text", "callId", "decision", "answer", "name", "looking", "meeting", "n", "args", "ask")
	requireFields(t, "daemon.Response", reflect.TypeOf(Response{}),
		"ok", "error", "waiting", "doing", "out", "exit", "permission", "user", "jobs", "tools", "models", "why",
		"session", "handover", "version", "proto", "caps")
	requireFields(t, "cluster.Member", reflect.TypeOf(cluster.Member{}),
		"host", "socket", "name", "role", "team", "hub", "state", "account", "workdir", "can", "does",
		"waiting", "handling", "version", "seen", "by", "sig")
	// The published record is a cross-BUILD contract on one machine: a newer magi-web reads an older
	// daemon's record and vice versa (the console's Build line, the update button, the roster all
	// hang off it), so its field names are held the same way the socket protocol's are.
	requireFields(t, "daemon.Info", reflect.TypeOf(Info{}),
		"socket", "workdir", "session", "name", "role", "team", "hub", "can", "does", "waiting",
		"handling", "pid", "started", "host", "addr", "state", "account", "version")
}

// handshakeEngine is an Engine (via the embedded fake) that also describes and versions itself — what
// a real daemon does, so answerAbout can fill the handshake.
type handshakeEngine struct{ *fakeEngine }

func (handshakeEngine) About() string   { return "a test companion" }
func (handshakeEngine) Version() string { return "v9.9.9" }

// The about handshake carries the structured version + capabilities a caller negotiates on, on top of
// the rendered description, and a caller reads them back through PeerInfo.
func TestAboutHandshakeCarriesVersionAndCaps(t *testing.T) {
	eng := handshakeEngine{&fakeEngine{}}
	resp := answerAbout(context.Background(), eng, Request{Method: "about"})
	if !resp.OK {
		t.Fatalf("about was refused: %s", resp.Err)
	}
	if resp.Out == "" {
		t.Error("the rendered description was dropped when the handshake fields were added")
	}
	if resp.Version != "v9.9.9" {
		t.Errorf("version not carried: %q", resp.Version)
	}
	if resp.Proto != ProtoVersion {
		t.Errorf("proto = %d, want %d", resp.Proto, ProtoVersion)
	}
	p := PeerInfo{Version: resp.Version, Proto: resp.Proto, Caps: resp.Caps}
	if !p.Supports("handshake") {
		t.Errorf("the handshake capability was not advertised: %v", resp.Caps)
	}
	if p.Supports("no-such-capability") {
		t.Error("Supports returned true for a capability that was never advertised")
	}
}

// A daemon that predates the handshake sets none of the structured fields; a caller reads that zero
// value as proto 0 and no caps — "hold to old behaviour" — never as support for anything.
func TestPeerThatPredatesTheHandshakeSupportsNothing(t *testing.T) {
	var p PeerInfo // what a pre-handshake about reply decodes to
	if p.Proto != 0 || len(p.Caps) != 0 {
		t.Fatalf("a zero PeerInfo should be proto 0 / no caps: %+v", p)
	}
	if p.Supports("handshake") {
		t.Error("a pre-handshake peer must not be read as supporting the handshake")
	}
}

// An unknown method is REFUSED with a typed error, not silently dropped — the one graceful mismatch a
// newer client can fall back on when it calls a method an older daemon does not have.
func TestUnknownMethodIsRefusedNotDropped(t *testing.T) {
	err := dispatchNow(context.Background(), &fakeEngine{}, Request{Method: "no-such-method-xyz"})
	if err == nil {
		t.Fatal("an unknown method was accepted silently")
	}
	if !strings.Contains(err.Error(), "unknown method") {
		t.Errorf("the refusal does not name the problem: %v", err)
	}
}
