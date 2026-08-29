package main

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"

	"github.com/sayaya1090/magi/internal/adapter/fleet"
)

// Companions on other machines, reached by putting one console in front of the others.
//
// # Why federation is composition and not a protocol
//
// The thing to federate already exists: a magi-web serves /fleet as JSON and takes actions as form
// posts. So a console that watches several machines is a console that reads several consoles. No
// new wire format, no registry to run, no second implementation of the state derivation — a peer
// answers with the same nine fields its own page draws from, and if it ever stops doing that its
// own tests fail first.
//
// # Why we do not invent the authentication
//
// A peer is reached however the company allows: an `ssh -L` tunnel to a loopback port, or their own
// proxy with their own SSO in front of it. magi-web binds loopback and has no login of its own, and
// that stays true here — a peer list does not make this process a server on the network, it makes
// it a client of ports somebody already decided it may reach. Building our own accounts would be a
// second door beside the company's, and the second door is always the weaker one.
//
// # The one rule that matters
//
// A peer URL comes from the OPERATOR — a flag, or a config file they wrote — and never from a page,
// a query parameter, or another peer's response. It is a place this process will send commands to;
// treating a URL from the network as a peer is how a viewer becomes a relay for whoever can answer
// first. Same rule the ?d= allowlist follows one layer down.
type peer struct {
	Name string // what a person calls that machine, shown on every row that came from it
	Base string // http://127.0.0.1:7778 — a magi-web, usually through a tunnel
}

// parsePeers reads the repeatable -peer name=url flag.
func parsePeers(specs []string) ([]peer, error) {
	var out []peer
	seen := map[string]bool{}
	for _, s := range specs {
		name, base, ok := strings.Cut(s, "=")
		name, base = strings.TrimSpace(name), strings.TrimSpace(base)
		if !ok || name == "" || base == "" {
			return nil, fmt.Errorf("a peer is name=url, and %q is not — for example: -peer mini=http://127.0.0.1:7778", s)
		}
		u, err := url.Parse(base)
		if err != nil || u.Scheme == "" || u.Host == "" {
			return nil, fmt.Errorf("peer %q has an unusable url %q", name, base)
		}
		if seen[name] {
			// Two peers with one name means every row from them is ambiguous, and the actions that
			// route by name would reach whichever came first.
			return nil, fmt.Errorf("peer %q is named twice", name)
		}
		seen[name] = true
		out = append(out, peer{Name: name, Base: strings.TrimRight(base, "/")})
	}
	return out, nil
}

// peerTimeout bounds a call to another console. Long enough for a machine across a tunnel to answer
// with its list; short enough that one unreachable peer does not hold the page. The local
// companions are already listed by then — a federated view degrades to the part that answered.
const peerTimeout = 4 * time.Second

// fanOut asks every peer the same question at once.
//
// Three functions here had the same fifteen lines — a WaitGroup, a slice indexed by peer so the
// order is the operator's rather than the network's, a per-call timeout, and a flat append at the
// end. What actually differed was the question and what to do with a console that did not answer,
// so those are the two things left to the caller: ask, and then read err on each result.
//
// Indexed rather than appended from the goroutines: the rows a person reads should come back in
// the order they configured their peers in, not in the order the machines happened to reply.
//
// The per-call deadline overlaps with the timeout on the shared http client, deliberately: this one
// belongs to fanOut and holds for any ask, including one that does not go through that client. No
// test separates them, because from the outside they bound the same wait.
func fanOut[T any](ctx context.Context, peers []peer,
	ask func(context.Context, peer) ([]T, error)) []fanResult[T] {
	out := make([]fanResult[T], len(peers))
	var wg sync.WaitGroup
	for i, p := range peers {
		wg.Add(1)
		go func(i int, p peer) {
			defer wg.Done()
			cctx, cancel := context.WithTimeout(ctx, peerTimeout)
			defer cancel()
			list, err := ask(cctx, p)
			out[i] = fanResult[T]{Peer: p, List: list, Err: err}
		}(i, p)
	}
	wg.Wait()
	return out
}

// fanResult is one peer's answer, or its failure to give one.
type fanResult[T any] struct {
	Peer peer
	List []T
	Err  error
}

// getJSON reads one JSON list from a peer. The bound is on the body and not on trust: a peer is a
// console the operator pointed at, but a console with a runaway log should still not be able to
// make this process allocate without limit.
func getJSON[T any](ctx context.Context, c *http.Client, url string) ([]T, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}
	resp, err := c.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("%s answered %s", url, resp.Status)
	}
	var list []T
	if err := json.NewDecoder(io.LimitReader(resp.Body, 4<<20)).Decode(&list); err != nil {
		return nil, fmt.Errorf("%s: unreadable answer: %w", url, err)
	}
	return list, nil
}

// fleetOf asks one peer for its companions and stamps them with its name.
func (s *server) fleetOf(ctx context.Context, p peer) ([]fleet.Agent, error) {
	list, err := getJSON[fleet.Agent](ctx, s.http, p.Base+"/fleet")
	if err != nil {
		return nil, err
	}
	for i := range list {
		list[i].Peer = p.Name
		list[i].Here = false // "this directory" is a fact about the console that answered, not this one
	}
	return list, nil
}

// federated is the local companions plus every peer's, asked in parallel.
//
// A peer that does not answer becomes a ROW rather than an error: a machine that has gone quiet is
// the thing a supervisor most needs to see, and dropping it from the list is the one presentation
// that hides it. The row carries the reason, so "the tunnel is down" and "the console is up and has
// nothing" do not look the same.
func (s *server) federated(ctx context.Context, local []fleet.Agent) []fleet.Agent {
	// A copy, never an append onto the caller's slice: local may be the shared cached answer, and
	// two stream ticks appending peers onto one backing array is a race with no reader.
	out := make([]fleet.Agent, 0, len(local)+4)
	out = append(out, local...)
	return append(out, s.peerFleet(ctx)...)
}

// peerFleet is the other machines' companions, asked for at most every peerEvery.
//
// The local half of a roster is a directory read and costs nothing; this half is one HTTP request
// per peer per call, and it had no floor. That was survivable while a browser polled every three
// seconds and stopped being survivable when the roster became a stream: the loop behind it looks
// every 700ms, so one open tab was asking every peer 1.4 times a second — four times what the poll
// cost, paid by the machine at the other end, and multiplied by every viewer.
//
// The cluster's own gossip is explicit about this shape of cost: a round a minute, two hosts at
// random, capabilities capped, "because a member list is exchanged by every daemon every minute
// and anything per-member is paid N times by N machines". A console that asks every peer for
// everything, per viewer, per frame, is the same bill with nobody counting it.
//
// So the answer is shared and has a floor. A hundred viewers cost what one costs, and what they
// see is at most peerEvery old — which is inside the grain of the thing it describes: a companion
// picking up work is news for as long as the work takes.
func (s *server) peerFleet(ctx context.Context) []fleet.Agent {
	if len(s.peers) == 0 {
		return nil
	}
	s.peerAt.mu.Lock()
	fresh := time.Since(s.peerAt.when) < peerEvery && s.peerAt.done
	if fresh || s.peerAt.fetching {
		// Fresh, or somebody is already out asking: serve what we have. One fetcher at a time is
		// what makes "a hundred viewers cost what one costs" true in the stale window too — the
		// check-then-fetch used to drop the lock, so every tick that landed while a slow peer
		// took its four seconds launched a fan-out of its own, and a dead peer stalled every
		// live transcript frame behind the synchronous refresh.
		list, done := s.peerAt.list, s.peerAt.done
		s.peerAt.mu.Unlock()
		if fresh || done {
			return list
		}
		return nil // very first fetch still in flight: one empty frame beats a four-second stall
	}
	s.peerAt.fetching = true
	s.peerAt.mu.Unlock()

	var out []fleet.Agent
	for _, r := range fanOut(ctx, s.peers, s.fleetOf) {
		if r.Err != nil {
			out = append(out, unreachable(r.Peer, r.Err))
			continue
		}
		out = append(out, r.List...)
	}
	s.peerAt.mu.Lock()
	s.peerAt.list, s.peerAt.when, s.peerAt.done, s.peerAt.fetching = out, time.Now(), true, false
	s.peerAt.mu.Unlock()
	return out
}

// peerEvery is the floor under how often this console asks other machines for their companions.
//
// Three seconds, which is what the browser's own poll used to be — the number was fine, what was
// wrong was that it was paid per viewer and per frame rather than once by the console.
const peerEvery = 3 * time.Second

// unreachable is the row a peer gets when it does not answer.
func unreachable(p peer, err error) fleet.Agent {
	return fleet.Agent{
		Peer: p.Name, Name: p.Name, Workdir: p.Base, Host: p.Name,
		State: fleet.Stopped, Idle: -1,
		Task: "this console did not answer: " + fleet.Clip(err.Error(), 160),
	}
}

// proxy forwards one action to the peer that owns the companion it names.
//
// The request is rebuilt rather than passed through: the only things that cross are the method, the
// path, the target socket and the form body. A peer console is not a tunnel to arbitrary URLs, and
// the way it stops being one is that nothing else here is copied.
func (s *server) proxy(w http.ResponseWriter, r *http.Request, p peer, socket string) {
	// On a SHARED console, reads cross and changes do not.
	//
	// The request rebuilt below carries no identity — deliberately, because a header this console
	// forwarded would be one the far console has to trust from a machine rather than from its own
	// gateway. With one operator that costs nothing: the only person who could have sent it is the
	// person whose tunnel it goes down, so the far console's record naming "the operator" is true.
	//
	// With several people it stops being true. The action arrives unattributable — no name for the
	// far console's audit, nobody for its policy to check — and anybody admitted here would be
	// acting as the operator over there. This used to be prevented by refusing -exposed and -peer
	// together, which also removed the arrangement people actually want: one console, several
	// machines, several people looking. Looking is what a federated view is FOR, so looking stays
	// and the change is turned away — with the address of the console that can make it.
	if s.shared() && r.Method != http.MethodGet && r.Method != http.MethodHead {
		http.Error(w, "this console is shared, so it can show you "+p.Name+" and cannot act on it: "+
			"an action sent from here would arrive there with no name on it, and could be neither "+
			"checked nor recorded. Open "+p.Base+" and do it where that companion lives.",
			http.StatusForbidden)
		return
	}
	if err := r.ParseForm(); err != nil {
		http.Error(w, "unreadable form", http.StatusBadRequest)
		return
	}
	body := strings.NewReader(r.PostForm.Encode())
	target := p.Base + r.URL.Path + "?d=" + url.QueryEscape(socket)
	ctx, cancel := context.WithTimeout(r.Context(), peerTimeout)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, r.Method, target, body)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadGateway)
		return
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	resp, err := s.http.Do(req)
	if err != nil {
		http.Error(w, "the console on "+p.Name+" did not answer: "+err.Error(), http.StatusBadGateway)
		return
	}
	defer resp.Body.Close()
	// The peer's own words come back verbatim: it knows why it refused and this process does not.
	msg, rerr := io.ReadAll(io.LimitReader(resp.Body, 8<<10))
	if rerr != nil {
		// The status is the part that matters and it already arrived; a body that broke on the way
		// is worth saying rather than passing on as an empty reason.
		http.Error(w, "the console on "+p.Name+" answered "+resp.Status+
			" and its reason did not arrive: "+rerr.Error(), http.StatusBadGateway)
		return
	}
	w.WriteHeader(resp.StatusCode)
	if _, werr := w.Write(msg); werr != nil {
		log.Printf("magi-web: relaying %s's answer: %v", p.Name, werr)
	}
}

// proxyStream forwards a peer's event stream, so opening a remote companion is not a different
// page from opening a local one.
//
// Copied as it arrives and flushed per chunk: the whole value of this endpoint is that a line
// written on another machine reaches the screen now, and a buffer between them would hold the last
// frame back until the next one pushed it out.
func (s *server) proxyStream(w http.ResponseWriter, r *http.Request, p peer, socket string) {
	fl, ok := w.(http.Flusher)
	if !ok {
		http.Error(w, "streaming unsupported", http.StatusInternalServerError)
		return
	}
	target := p.Base + "/events?d=" + url.QueryEscape(socket)
	req, err := http.NewRequestWithContext(r.Context(), http.MethodGet, target, nil)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadGateway)
		return
	}
	// No peerTimeout here: an event stream is meant to stay open, and the deadline that ends it is
	// the reader going away, which the request's own context carries.
	resp, err := s.stream.Do(req)
	if err != nil {
		http.Error(w, "the console on "+p.Name+" did not answer: "+err.Error(), http.StatusBadGateway)
		return
	}
	defer resp.Body.Close()
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	buf := make([]byte, 32<<10)
	for {
		n, rerr := resp.Body.Read(buf)
		if n > 0 {
			if _, werr := w.Write(buf[:n]); werr != nil {
				return
			}
			fl.Flush()
		}
		if rerr != nil {
			return
		}
	}
}

// peerNamed finds a configured peer. Only the operator's list is consulted — a name that is not in
// it is not a place this process will send anything.
func (s *server) peerNamed(name string) (peer, bool) {
	for _, p := range s.peers {
		if p.Name == name {
			return p, true
		}
	}
	return peer{}, false
}
