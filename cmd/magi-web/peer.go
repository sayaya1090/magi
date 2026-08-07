package main

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
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

// fleetOf asks one peer for its companions and stamps them with its name.
func (s *server) fleetOf(ctx context.Context, p peer) ([]fleet.Agent, error) {
	ctx, cancel := context.WithTimeout(ctx, peerTimeout)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, p.Base+"/fleet", nil)
	if err != nil {
		return nil, err
	}
	resp, err := s.http.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("%s answered %s", p.Base, resp.Status)
	}
	var list []fleet.Agent
	if err := json.NewDecoder(io.LimitReader(resp.Body, 4<<20)).Decode(&list); err != nil {
		return nil, fmt.Errorf("%s: unreadable fleet: %w", p.Base, err)
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
	if len(s.peers) == 0 {
		return local
	}
	type result struct {
		list []fleet.Agent
		err  error
		p    peer
	}
	results := make([]result, len(s.peers))
	var wg sync.WaitGroup
	for i, p := range s.peers {
		wg.Add(1)
		go func(i int, p peer) {
			defer wg.Done()
			list, err := s.fleetOf(ctx, p)
			results[i] = result{list: list, err: err, p: p}
		}(i, p)
	}
	wg.Wait()

	out := local
	for _, r := range results {
		if r.err != nil {
			out = append(out, unreachable(r.p, r.err))
			continue
		}
		out = append(out, r.list...)
	}
	return out
}

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
	msg, _ := io.ReadAll(io.LimitReader(resp.Body, 8<<10))
	w.WriteHeader(resp.StatusCode)
	_, _ = w.Write(msg)
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
