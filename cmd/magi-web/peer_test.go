package main

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/sayaya1090/magi/internal/adapter/fleet"
)

// A peer is another magi-web. These tests stand one up with httptest and point a console at it,
// which is exactly the arrangement in production minus the ssh tunnel in between.
func fakePeer(t *testing.T, list []fleet.Agent, record *[]string) *httptest.Server {
	t.Helper()
	mux := http.NewServeMux()
	mux.HandleFunc("/fleet", func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(list)
	})
	for _, path := range []string{"/interrupt", "/answer", "/submit"} {
		mux.HandleFunc(path, func(w http.ResponseWriter, r *http.Request) {
			_ = r.ParseForm()
			*record = append(*record, r.Method+" "+r.URL.Path+"?d="+r.URL.Query().Get("d")+" "+r.PostForm.Encode())
			w.WriteHeader(http.StatusNoContent)
		})
	}
	mux.HandleFunc("/events", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = w.Write([]byte("data: [{\"who\":\"user\",\"text\":\"from the other machine\"}]\n\n"))
		w.(http.Flusher).Flush()
	})
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	return srv
}

func federatedServer(t *testing.T, peers ...peer) *fleetFixture {
	t.Helper()
	f := newFleetFixture(t)
	f.srv.peers = peers
	f.srv.http = &http.Client{Timeout: 3 * time.Second}
	f.srv.stream = &http.Client{}
	return f
}

// A supervisor watches several machines from one page, and the rows say which machine each came
// from. Federation is composition: the thing being federated is the console we already have, so
// there is no second implementation of the state derivation to keep in step.
func TestTheFleetMergesEveryPeer(t *testing.T) {
	var seen []string
	remote := fakePeer(t, []fleet.Agent{
		{Socket: "/there/a.sock", Name: "fuzzer", Workdir: "/w/fuzzer", State: fleet.Working, Live: true, Here: true},
	}, &seen)
	f := federatedServer(t, peer{Name: "laptop", Base: remote.URL})
	wd := shortTempDir(t)
	f.daemonAt(wd, "local1", true)
	f.session("local1", wd, "here", 1, false)

	list := f.get()
	if len(list) != 2 {
		t.Fatalf("the merged fleet has %d rows: %+v", len(list), list)
	}
	var local, far fleet.Agent
	for _, a := range list {
		if a.Peer == "" {
			local = a
		} else {
			far = a
		}
	}
	if local.Session != "local1" {
		t.Errorf("the local row came back as %+v", local)
	}
	if far.Peer != "laptop" || far.Name != "fuzzer" {
		t.Errorf("the remote row came back as %+v", far)
	}
	// "this directory" is a fact about the console that answered, not about this one. Carried
	// across, every remote row would claim to be the one you are standing in.
	if far.Here {
		t.Error("a remote companion says it is in this console's directory")
	}
}

// A machine that has gone quiet is the thing a supervisor most needs to see, so it becomes a ROW
// rather than an error page or a silent omission — with the reason on it, so "the tunnel is down"
// and "that console has nothing" do not look the same.
func TestAnUnreachablePeerBecomesARowNotAnError(t *testing.T) {
	f := federatedServer(t, peer{Name: "gone", Base: "http://127.0.0.1:1"})
	wd := shortTempDir(t)
	f.daemonAt(wd, "local1", true)
	f.session("local1", wd, "here", 1, false)

	list := f.get()
	if len(list) != 2 {
		t.Fatalf("an unreachable peer produced %d rows, want the local one and a row for it", len(list))
	}
	var far fleet.Agent
	for _, a := range list {
		if a.Peer == "gone" {
			far = a
		}
	}
	if far.State != fleet.Stopped {
		t.Errorf("the unreachable console came back as %q", far.State)
	}
	if !strings.Contains(far.Task, "did not answer") {
		t.Errorf("the row does not say what is wrong: %q", far.Task)
	}
}

// Acting on a companion happens on the console that owns it. Nothing here can reach it — the socket
// path belongs to that machine and its daemon is not ours to dial.
func TestActionsGoToTheConsoleThatOwnsTheCompanion(t *testing.T) {
	var seen []string
	remote := fakePeer(t, nil, &seen)
	f := federatedServer(t, peer{Name: "laptop", Base: remote.URL})
	q := "?d=" + url.QueryEscape("/there/a.sock") + "&p=laptop"

	if w := post(t, f.srv, f.srv.interrupt, "/interrupt"+q, nil); w.Code != http.StatusNoContent {
		t.Fatalf("/interrupt replied %d: %s", w.Code, w.Body.String())
	}
	if w := post(t, f.srv, f.srv.answer, "/answer"+q, url.Values{
		"call": {"c1"}, "kind": {"permission"}, "text": {"allow"}}); w.Code != http.StatusNoContent {
		t.Fatalf("/answer replied %d: %s", w.Code, w.Body.String())
	}
	if len(seen) != 2 {
		t.Fatalf("the peer saw %v", seen)
	}
	if !strings.HasPrefix(seen[0], "POST /interrupt?d=/there/a.sock") {
		t.Errorf("the interrupt arrived as %q", seen[0])
	}
	if !strings.Contains(seen[1], "call=c1") || !strings.Contains(seen[1], "text=allow") {
		t.Errorf("the answer lost its body: %q", seen[1])
	}
}

// A console name that is not configured routes NOWHERE, and says so in its own words.
//
// Falling through to the local lookup was the first version, and it failed with "no daemon at …" —
// which sends whoever is reading to look for a companion when the real answer is that this console
// federates nobody by that name. The peer list is the operator's; a name off the wire only ever
// gets looked up in it.
func TestAnUnknownPeerNameRoutesNowhere(t *testing.T) {
	var seen []string
	remote := fakePeer(t, nil, &seen)
	f := federatedServer(t, peer{Name: "laptop", Base: remote.URL})

	w := post(t, f.srv, f.srv.interrupt, "/interrupt?d=/x.sock&p=evil", nil)
	if w.Code != http.StatusNotFound {
		t.Errorf("an unknown console replied %d, want 404: %s", w.Code, w.Body.String())
	}
	for _, want := range []string{"no console named", "laptop"} {
		if !strings.Contains(w.Body.String(), want) {
			t.Errorf("the refusal does not say %q: %q", want, w.Body.String())
		}
	}
	if len(seen) != 0 {
		t.Errorf("something was forwarded anyway: %v", seen)
	}
}

// Opening a remote companion is not a different page: its transcript is streamed through.
func TestARemoteTranscriptStreamsThrough(t *testing.T) {
	var seen []string
	remote := fakePeer(t, nil, &seen)
	f := federatedServer(t, peer{Name: "laptop", Base: remote.URL})

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	r := httptest.NewRequest(http.MethodGet, "/events?d="+url.QueryEscape("/there/a.sock")+"&p=laptop", nil).WithContext(ctx)
	rec := newStreamRecorder()
	done := make(chan struct{})
	go func() { defer close(done); f.srv.events(rec, r) }()
	if !rec.waitFor(t, "from the other machine", 4*time.Second) {
		t.Fatalf("the remote transcript did not arrive: %q", rec.body())
	}
	cancel()
	<-done
}

// The peer list is parsed once, at startup, from the operator's flags — and a list that cannot be
// trusted to be unambiguous is worse than none: two consoles with one name make every row from
// them ambiguous and every action route to whichever came first.
func TestPeerFlagsAreParsedStrictly(t *testing.T) {
	good, err := parsePeers([]string{"mini=http://127.0.0.1:7778", "laptop=http://127.0.0.1:7779/"})
	if err != nil {
		t.Fatalf("a well-formed pair was refused: %v", err)
	}
	if len(good) != 2 || good[0].Name != "mini" || good[1].Base != "http://127.0.0.1:7779" {
		t.Errorf("parsed as %+v", good)
	}
	for _, bad := range []string{"nourl", "=http://x", "mini=", "mini=notaurl"} {
		if _, err := parsePeers([]string{bad}); err == nil {
			t.Errorf("%q was accepted", bad)
		}
	}
	if _, err := parsePeers([]string{"a=http://x", "a=http://y"}); err == nil {
		t.Error("two consoles with one name were accepted")
	}
}
