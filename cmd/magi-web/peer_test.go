package main

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync/atomic"
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

// The rows a person reads come back in the order they configured their peers in, not in the order
// the machines happened to reply.
//
// Machines answer at wildly different speeds — one across a tunnel, one on the same host — so a
// list assembled as replies arrive reorders itself between two refreshes of a page nobody changed.
func TestTheMergedFleetKeepsTheOperatorsOrder(t *testing.T) {
	var seen []string
	// The first peer configured is the slow one, so anything that appends as answers land would
	// put it second.
	slow := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		time.Sleep(120 * time.Millisecond)
		_ = json.NewEncoder(w).Encode([]fleet.Agent{{Socket: "/s/slow.sock", Name: "slow"}})
	}))
	defer slow.Close()
	quick := fakePeer(t, []fleet.Agent{{Socket: "/s/quick.sock", Name: "quick"}}, &seen)
	f := federatedServer(t, peer{Name: "tunnel", Base: slow.URL}, peer{Name: "here", Base: quick.URL})

	for i := 0; i < 3; i++ {
		list := f.get()
		if len(list) != 2 {
			t.Fatalf("merged %d rows", len(list))
		}
		if list[0].Peer != "tunnel" || list[1].Peer != "here" {
			t.Fatalf("pass %d came back as %q then %q", i, list[0].Peer, list[1].Peer)
		}
	}
}

// A console that answers with an error is not a console with nothing to say.
//
// Both look like an empty list to whoever skips the status line, and the difference is the whole
// point of the row: "that machine is in trouble" versus "that machine is idle".
func TestAPeerThatAnswersAnErrorIsNotAnEmptyPeer(t *testing.T) {
	broken := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusServiceUnavailable)
		_, _ = w.Write([]byte(`[]`)) // a body that would decode perfectly well if anyone looked
	}))
	defer broken.Close()
	f := federatedServer(t, peer{Name: "sick", Base: broken.URL})

	list := f.get()
	if len(list) != 1 {
		t.Fatalf("a broken console produced %d rows: %+v", len(list), list)
	}
	if !strings.Contains(list[0].Task, "did not answer") || !strings.Contains(list[0].Task, "503") {
		t.Errorf("the row does not say what went wrong: %q", list[0].Task)
	}
}

// A peer is a console the operator pointed at, which is a reason to trust its intent and not its
// size: a console with a runaway log must not be able to make this process allocate without limit.
func TestAPeersAnswerIsBounded(t *testing.T) {
	huge := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Valid JSON, all the way to the end, and larger than the cap. Truncating it mid-array
		// would fail to parse for the wrong reason and pass whether or not a limit exists.
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`[`))
		row := `{"name":"x","task":"` + strings.Repeat("y", 4000) + `"},`
		for written := 0; written < 5<<20; written += len(row) {
			if _, err := w.Write([]byte(row)); err != nil {
				return
			}
		}
		_, _ = w.Write([]byte(`{"name":"last"}]`))
	}))
	defer huge.Close()
	f := federatedServer(t, peer{Name: "flood", Base: huge.URL})

	list := f.get()
	if len(list) != 1 || list[0].Peer != "flood" {
		t.Fatalf("an oversized answer produced %d rows", len(list))
	}
	if !strings.Contains(list[0].Task, "did not answer") {
		t.Errorf("an answer past the cap was accepted: %q", list[0].Task)
	}
}

// The workspace of a companion on ANOTHER machine, through the console in front of it.
//
// "Can I see the files on my other machine?" has two answers, and this is the one that is yes: a
// magi-web running there is a peer, and every companion route this console has — including the two
// that read a workspace — is answered by whichever console owns the companion. The path never
// leaves that machine, and this one resolves nothing against its own filesystem.
//
// (The other answer is no, and it is deliberate: a companion known only by gossip has no console
// in front of it, and the fleet door carries work rather than file contents. The pane says so.)
func TestAWorkspaceOnAnotherMachineIsReadThroughItsOwnConsole(t *testing.T) {
	var seen []string
	mux := http.NewServeMux()
	mux.HandleFunc("/files", func(w http.ResponseWriter, r *http.Request) {
		seen = append(seen, "files "+r.URL.Query().Get("d")+" "+r.URL.Query().Get("path"))
		_, _ = w.Write([]byte(`[{"name":"cmd","isDir":true},{"name":"go.mod","isDir":false}]`))
	})
	mux.HandleFunc("/file", func(w http.ResponseWriter, r *http.Request) {
		seen = append(seen, "file "+r.URL.Query().Get("path"))
		_ = json.NewEncoder(w).Encode(map[string]string{
			"path": r.URL.Query().Get("path"), "text": "     1\tmodule magi\n"})
	})
	remote := httptest.NewServer(mux)
	t.Cleanup(remote.Close)
	f := federatedServer(t, peer{Name: "buildbox", Base: remote.URL})
	q := "?d=" + url.QueryEscape("/there/a.sock") + "&p=buildbox"

	w := httptest.NewRecorder()
	f.srv.files(w, httptest.NewRequest(http.MethodGet, "/files"+q+"&path=.", nil))
	if w.Code != http.StatusOK || !strings.Contains(w.Body.String(), "go.mod") {
		t.Fatalf("the listing came back %d: %s", w.Code, w.Body.String())
	}
	w = httptest.NewRecorder()
	f.srv.file(w, httptest.NewRequest(http.MethodGet, "/file"+q+"&path=go.mod", nil))
	if w.Code != http.StatusOK || !strings.Contains(w.Body.String(), "module magi") {
		t.Fatalf("the file came back %d: %s", w.Code, w.Body.String())
	}
	// Asked of the other console, with the socket it uses on ITS machine — not opened here.
	if len(seen) != 2 || !strings.HasPrefix(seen[0], "files /there/a.sock") {
		t.Errorf("the far console saw %v", seen)
	}
}

// The other machines are asked on a clock this console keeps, not once per viewer per frame.
//
// The local half of a roster is a directory read; this half is an HTTP request per peer. It had no
// floor, which was survivable while a browser polled every three seconds and stopped being
// survivable when the roster became a stream: the loop behind it looks every 700ms, so one open tab
// asked every peer 1.4 times a second — paid by the machine at the other end, multiplied by every
// viewer. The cluster's own gossip is explicit about this shape of cost (a round a minute, two
// hosts, capabilities capped); a console with no floor is the same bill with nobody counting it.
func TestPeersAreAskedOnAClockNotOncePerViewer(t *testing.T) {
	// Its own peer, which counts what it is asked. The shared fake records the action routes and
	// several tests assert on exactly what is in that list.
	var asked int32
	remote := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/fleet" {
			atomic.AddInt32(&asked, 1)
		}
		_ = json.NewEncoder(w).Encode([]fleet.Agent{
			{Socket: "/there/a.sock", Name: "fuzzer", Workdir: "/w/fuzzer", State: fleet.Working, Live: true},
		})
	}))
	t.Cleanup(remote.Close)
	f := federatedServer(t, peer{Name: "laptop", Base: remote.URL})
	wd := shortTempDir(t)
	f.daemonAt(wd, "local1", true)

	for i := 0; i < 12; i++ {
		if list := f.get(); len(list) != 2 {
			t.Fatalf("read %d came back with %d rows", i, len(list))
		}
	}
	// Twelve reads in well under the floor: one trip, and every reader saw the same answer.
	if n := atomic.LoadInt32(&asked); n != 1 {
		t.Errorf("twelve reads made %d requests to the peer", n)
	}
}
