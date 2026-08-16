package main

import (
	"bytes"
	"encoding/json"
	"net"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"github.com/sayaya1090/magi/internal/adapter/daemon"
)

// fakeDaemon answers one line per line, recording what actually reached it — per CONNECTION.
//
// Per connection because the door is not the only caller: daemon.List proves a companion alive by
// dialling it and asking for its status, so a single flat list of methods mixes that probe in with
// what the door carried, and the test would be asserting about two conversations at once.
func fakeDaemon(t *testing.T, sock string) (*[][]string, *sync.Mutex) {
	t.Helper()
	ln, err := net.Listen("unix", sock)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { ln.Close() })
	got := &[][]string{}
	mu := sync.Mutex{}
	// Accepting in a LOOP, because the door is not the first caller: daemon.List proves a
	// companion alive by dialling it, so a fake that answered once was already spent by the time
	// the thing under test connected — and the test hung rather than failed.
	go func() {
		for {
			c, err := ln.Accept()
			if err != nil {
				return
			}
			mu.Lock()
			*got = append(*got, nil)
			mine := len(*got) - 1
			mu.Unlock()
			go func() {
				defer c.Close()
				dec := json.NewDecoder(c)
				enc := json.NewEncoder(c)
				for {
					var req daemon.Request
					if dec.Decode(&req) != nil {
						return
					}
					mu.Lock()
					(*got)[mine] = append((*got)[mine], req.Method)
					mu.Unlock()
					_ = enc.Encode(daemon.Response{OK: true, Out: "did " + req.Method})
				}
			}()
		}
	}()
	return got, &mu
}

// A door carries four methods and refuses the rest without passing them on.
//
// The relay it is built from is deliberately dumb — it copies bytes and lets the daemon decide —
// which is right when the ssh key on the other side is your own. For somebody else's key it means
// `command="magi --relay …"` still hands over submit, steer, set-model and shell.
//
// The three alternating methods are exercised here; `watch`, the one that streams, has a test of
// its own below (a stream ends the connection, so it cannot sit in the middle of this sequence).
func TestTheFleetDoorCarriesThreeMethodsAndRefusesTheRest(t *testing.T) {
	cfg := shortTempDir(t)
	sock := filepath.Join(cfg, "daemon-api.sock")
	reached, mu := fakeDaemon(t, sock)
	stop, err := daemon.Publish(sock, t.TempDir(), "s1", daemon.Identity{})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(stop)

	var in bytes.Buffer
	in.WriteString(`{"socket":"` + sock + `"}` + "\n")
	for _, m := range []string{"about", "hand", "hand-state", "submit", "shell", "set-model"} {
		b, _ := json.Marshal(daemon.Request{Method: m})
		in.Write(append(b, '\n'))
	}
	var out, errOut bytes.Buffer
	if code := fleetDoor(&in, &out, &errOut, cfg); code != 0 {
		t.Fatalf("the door answered %d: %s", code, errOut.String())
	}

	mu.Lock()
	defer mu.Unlock()
	var carried bool
	for _, conn := range *reached {
		got := strings.Join(conn, ",")
		if got == "about,hand,hand-state" {
			carried = true
		}
		for _, refused := range []string{"submit", "shell", "set-model", "steer", "interrupt"} {
			if strings.Contains(got, refused) {
				t.Errorf("%q reached the daemon — the door is meant to be the filter (%q)", refused, got)
			}
		}
	}
	if !carried {
		t.Errorf("the three methods did not arrive together: %v", *reached)
	}
	// And the refusals came back as answers, not as a closed pipe: the caller is a client waiting
	// on a JSON line, and silence reads as "the far side is broken" rather than "no".
	var refusals int
	for _, line := range strings.Split(strings.TrimSpace(out.String()), "\n") {
		var resp daemon.Response
		if json.Unmarshal([]byte(line), &resp) == nil && !resp.OK && strings.Contains(resp.Err, "fleet door") {
			refusals++
		}
	}
	if refusals != 3 {
		t.Errorf("%d of the three refused methods were answered:\n%s", refusals, out.String())
	}
}

// streamingDaemon answers a watch with frames: each line pushed as it "happens", then the
// connection closed — which is the daemon's own way of saying nothing more is coming.
func streamingDaemon(t *testing.T, sock string, frames []daemon.Response) {
	t.Helper()
	ln, err := net.Listen("unix", sock)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { ln.Close() })
	go func() {
		for {
			c, err := ln.Accept()
			if err != nil {
				return
			}
			go func() {
				defer c.Close()
				dec := json.NewDecoder(c)
				enc := json.NewEncoder(c)
				for {
					var req daemon.Request
					if dec.Decode(&req) != nil {
						return
					}
					if req.Method != "watch" {
						_ = enc.Encode(daemon.Response{OK: true, Out: "did " + req.Method})
						continue
					}
					for _, f := range frames {
						_ = enc.Encode(f)
					}
					return // the stream ends with its connection
				}
			}()
		}
	}()
}

// A watch crosses the door as a stream: every frame the daemon pushes arrives at the caller as it
// is pushed, and the daemon closing the stream ends the crossing cleanly. This is the channel that
// makes a remote hand-off's answer arrive when it happens rather than on a poll's clock — the door
// shipped without it, and every remote wait silently degraded to backoff polling.
func TestTheDoorStreamsAWatch(t *testing.T) {
	cfg := shortTempDir(t)
	sock := filepath.Join(cfg, "daemon-api.sock")
	streamingDaemon(t, sock, []daemon.Response{
		{OK: true, Handover: &daemon.Handover{News: "not started yet"}},
		{OK: true, Handover: &daemon.Handover{Done: true, Answer: "here it is"}},
	})
	stop, err := daemon.Publish(sock, t.TempDir(), "s1", daemon.Identity{})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(stop)

	var in bytes.Buffer
	in.WriteString(`{"socket":"` + sock + `"}` + "\n")
	b, _ := json.Marshal(daemon.Request{Method: "watch", Name: "r1"})
	in.Write(append(b, '\n'))
	var out, errOut bytes.Buffer
	if code := fleetDoor(&in, &out, &errOut, cfg); code != 0 {
		t.Fatalf("the door answered %d: %s", code, errOut.String())
	}
	lines := strings.Split(strings.TrimSpace(out.String()), "\n")
	if len(lines) != 2 {
		t.Fatalf("want the 2 pushed frames, got %d: %q", len(lines), out.String())
	}
	var first, last daemon.Response
	if json.Unmarshal([]byte(lines[0]), &first) != nil || first.Handover == nil ||
		first.Handover.News != "not started yet" {
		t.Errorf("first frame: %q", lines[0])
	}
	if json.Unmarshal([]byte(lines[1]), &last) != nil || last.Handover == nil ||
		!last.Handover.Done || last.Handover.Answer != "here it is" {
		t.Errorf("last frame: %q", lines[1])
	}
}

// The caller names a companion, and only one this account published.
//
// Under a forced command the socket is the one thing a caller still chooses. Dialled without a
// check it could be any unix socket on the machine — another program's, or another account's if
// the permissions were ever loosened.
func TestTheDoorOnlyOpensCompanionsThisAccountPublished(t *testing.T) {
	cfg := shortTempDir(t)
	// A socket that exists and is NOT a published companion: the shape of a mistake, and of an
	// attempt.
	other := filepath.Join(shortTempDir(t), "other.sock")
	ln, err := net.Listen("unix", other)
	if err != nil {
		t.Fatal(err)
	}
	defer ln.Close()

	var out, errOut bytes.Buffer
	code := fleetDoor(strings.NewReader(`{"socket":"`+other+`"}`+"\n"), &out, &errOut, cfg)
	if code == 0 {
		t.Error("the door opened a socket nobody published")
	}
	if !strings.Contains(errOut.String(), "no companion") {
		t.Errorf("the refusal does not say why: %q", errOut.String())
	}
	// It does not list what IS here, either: a caller who may not reach a companion may not
	// enumerate them.
	if strings.Contains(errOut.String(), "daemon-") {
		t.Errorf("the refusal names other companions: %q", errOut.String())
	}
}

// An empty or malformed opening is refused before anything is dialled.
func TestTheDoorAsksWhoIsWantedBeforeItConnects(t *testing.T) {
	cfg := shortTempDir(t)
	for _, first := range []string{"", "not json\n", "{}\n"} {
		var out, errOut bytes.Buffer
		if code := fleetDoor(strings.NewReader(first), &out, &errOut, cfg); code == 0 {
			t.Errorf("opening with %q was accepted", first)
		}
		if out.Len() != 0 {
			t.Errorf("opening with %q wrote %q to the caller", first, out.String())
		}
	}
}

// shortTempDir is a temp dir with a short path: a unix socket address is capped at ~104 bytes and
// a t.TempDir() under a long test name gets close.
func shortTempDir(t *testing.T) string {
	t.Helper()
	d, err := os.MkdirTemp("/tmp", "magidoor")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.RemoveAll(d) })
	return d
}

// The crossing this tree makes goes through the door, not the relay.
//
// The relay carries the whole protocol, so a key restricted to `command="magi --relay …"` can
// still submit, steer, set a model and run a shell command on the far machine. Whether the near
// side USES the door is what decides whether the far side's forced command can be narrow.
func TestACrossingIsMadeThroughTheDoor(t *testing.T) {
	src, err := os.ReadFile("hand.go")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(src), "doorTo(ctx, host, socket)") {
		t.Error("handAcross does not cross through the door")
	}
	if strings.Contains(string(src), "relayTo(ctx, host, socket)") {
		t.Error("handAcross still opens the wide relay")
	}
}
