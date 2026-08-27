package main

import (
	"context"
	"encoding/json"
	"fmt"
	"github.com/sayaya1090/magi/internal/webassets"
	"log"
	"net"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/sayaya1090/magi/internal/adapter/fleet"
	"github.com/sayaya1090/magi/internal/core/auth"
	"github.com/sayaya1090/magi/internal/core/text"
	"github.com/sayaya1090/magi/internal/core/webpush"
)

// Telling somebody who is not looking at the page.
//
// # What was here before
//
// The tab title, and nothing else. `(2) magi · companions` reaches a person with the tab in front
// of them; the fleet page exists for the person who is not. An agent that blocks on a question at
// 2am has waited until morning, and the console knew the whole time.
//
// # Why the browser's own mechanism
//
// A webhook into a notification service would be cheaper — one POST, no keys — and it would put a
// third party's account between somebody and their own machines, and need an app installed. The
// page is already installable: there is a manifest and an icon, so a phone that added it to its
// home screen can be woken by the console it already has. Nothing new to sign up for.
//
// # Who is told what
//
// This file was written for a console with one operator, where subscribing was open to whoever
// reached the port — the same access they already had to read every transcript on it. A console
// with people in it changes that, and in a direction easy to miss: the screens filter what they
// draw to a person's scope, and a notification leaves through a different door. The subscriber's
// name is recorded, and the watcher asks the ordinary read permission before it sends. What a
// person cannot open in the console does not arrive on their phone either.
//
// ⚠ A service worker only registers in a secure context, so a console reached over plain http from
// another machine cannot turn this on at all. The page says so rather than offering a switch that
// does nothing. Reached over localhost or behind TLS, it works.
type pushState struct {
	keys *webpush.Keys
	file string
	// A client of its own for sending, with an SSRF-guarded dialer that rejects a connection to an
	// internal address. Not the server's shared s.http, which peer forwarding uses and which may be
	// pointed at an internal peer by the operator on purpose.
	http *http.Client

	mu   sync.Mutex
	subs map[string]webpush.Subscription // by endpoint
	// who subscribed each endpoint, as the gateway named them, empty on a console with no gateway.
	// A notification is a read, and it goes out to a phone that is nowhere near the permission
	// check the page made — so it is the one read the console performs on somebody's behalf while
	// they are not asking. Without this the watcher would tell every browser about every companion,
	// which is the scope the screens stopped leaking and the pocket kept.
	who map[string]string

	// What each companion was doing when it was last looked at, so the watcher can notice a change
	// rather than re-announce the same block every few seconds.
	was map[string]fleet.State
	// Which handoffs have already been reported as answered, by asker+receiver+request. A handoff's
	// answer is the last thing the receiver said while idle, so it stays readable for as long as
	// the receiver stays idle — announcing on "there is an answer" would repeat it every tick.
	told map[string]bool
}

// storedSub is one subscription on disk. The endpoint is a credential — anyone holding it can send
// to that browser — so this file is written 0600 and never rendered into a page.
type storedSub struct {
	webpush.Subscription
	Added string `json:"added"`
	// Agent is the browser's own description of itself, kept only so a person clearing out old
	// subscriptions can tell which is the phone. Clipped, because a user agent string is long and
	// nothing reads it.
	Agent string `json:"agent,omitempty"`
	// Who the gateway named when this was subscribed. Absent in a file written before the console
	// had people in it, and that absence is read as nobody rather than as everybody: a phone
	// subscribed when the console had no policy has no claim on what the policy later says is out
	// of scope. Re-subscribing from the page fixes it in one tap.
	Who string `json:"who,omitempty"`
}

// newPush loads the console's push identity and whatever subscriptions it already has.
//
// The key is generated once and kept. A browser records which application server key subscribed it
// and drops a message signed by a different one, so regenerating this on restart would break every
// subscription already handed out — silently, since the browser simply ignores what it cannot
// verify. That is the whole reason it is on disk.
func newPush(cfgDir string) (*pushState, error) {
	keyFile := filepath.Join(cfgDir, "push-key")
	var keys *webpush.Keys
	seed, err := os.ReadFile(keyFile)
	switch {
	case err == nil:
		keys, err = webpush.KeysFromSeed(strings.TrimSpace(string(seed)), pushSubject)
		if err != nil {
			return nil, err
		}
	case os.IsNotExist(err):
		if keys, err = webpush.NewKeys(pushSubject); err != nil {
			return nil, err
		}
		if err := os.MkdirAll(cfgDir, 0o700); err != nil {
			return nil, err
		}
		if err := os.WriteFile(keyFile, []byte(keys.PrivateSeed()+"\n"), 0o600); err != nil {
			return nil, err
		}
	default:
		return nil, err
	}

	p := &pushState{
		http: &http.Client{Timeout: 5 * time.Minute, Transport: &http.Transport{DialContext: safePushDialer()}},
		keys: keys,
		file: filepath.Join(cfgDir, "push-subscriptions.json"),
		subs: map[string]webpush.Subscription{},
		who:  map[string]string{},
		was:  map[string]fleet.State{},
		told: map[string]bool{},
	}
	p.load()
	return p, nil
}

// pushSubject is the contact RFC 8292 asks an application server to publish. There is no address to
// give — this console belongs to whoever is running it and there is nobody to complain to — so it
// names the software, which is what the field is for when there is no operator to reach.
const pushSubject = "https://github.com/sayaya1090/magi"

func (p *pushState) load() {
	raw, err := os.ReadFile(p.file)
	if err != nil {
		return // no subscriptions yet, which is the ordinary state
	}
	var list []storedSub
	if err := json.Unmarshal(raw, &list); err != nil {
		// A file we cannot read is left alone rather than truncated: it is the only record of which
		// browsers asked to be told, and overwriting it would lose them without saying so.
		log.Printf("push: %s is unreadable (%v); no notifications will be sent until it is fixed", p.file, err)
		return
	}
	for _, s := range list {
		p.subs[s.Endpoint] = s.Subscription
		p.who[s.Endpoint] = s.Who
	}
}

// saveLocked writes the store. Callers hold p.mu.
func (p *pushState) saveLocked(agents map[string]string) {
	list := make([]storedSub, 0, len(p.subs))
	for _, s := range p.subs {
		list = append(list, storedSub{Subscription: s, Added: time.Now().UTC().Format(time.RFC3339),
			Agent: agents[s.Endpoint], Who: p.who[s.Endpoint]})
	}
	sort.Slice(list, func(i, j int) bool { return list[i].Endpoint < list[j].Endpoint })
	b, err := json.MarshalIndent(list, "", "  ")
	if err != nil {
		log.Printf("push: %v", err)
		return
	}
	if err := os.WriteFile(p.file, append(b, '\n'), 0o600); err != nil {
		log.Printf("push: cannot write %s: %v", p.file, err)
	}
}

// push answers what the page needs to subscribe, and takes subscriptions.
//
// Never forwarded to a peer. A subscription belongs to the console the browser is talking to,
// because that is the one whose watcher will send to it; routing it onward would have two consoles
// notifying for the same fleet.
func (s *server) push(w http.ResponseWriter, r *http.Request) {
	if s.pushes == nil {
		http.Error(w, "notifications are not configured on this console", http.StatusNotImplemented)
		return
	}
	p := s.pushes
	switch r.Method {
	case http.MethodGet:
		p.mu.Lock()
		n := len(p.subs)
		p.mu.Unlock()
		writeJSON(w, "push", map[string]any{"key": p.keys.PublicKey(), "count": n})
	case http.MethodPost:
		sub := webpush.Subscription{
			Endpoint: strings.TrimSpace(r.FormValue("endpoint")),
			P256dh:   strings.TrimSpace(r.FormValue("p256dh")),
			Auth:     strings.TrimSpace(r.FormValue("auth")),
		}
		if sub.Endpoint == "" || sub.P256dh == "" || sub.Auth == "" {
			http.Error(w, "a subscription needs an endpoint, a key and an auth secret", http.StatusBadRequest)
			return
		}
		// A push endpoint is a URL this process will later POST an encrypted payload to, so a
		// caller who could store an internal address (169.254.169.254, 127.0.0.1:port, a private
		// host) turned the console into a blind SSRF hop the moment a companion in their scope
		// started waiting. A real push service is always a public https URL; anything else is
		// refused here, before it is ever stored — and only on delete is that skipped, since a
		// delete removes by the exact string and reaches nothing.
		if r.FormValue("delete") != "1" {
			if err := safePushEndpoint(sub.Endpoint); err != nil {
				http.Error(w, err.Error(), http.StatusBadRequest)
				return
			}
		}
		p.mu.Lock()
		if r.FormValue("delete") == "1" {
			// Only your own. An endpoint is a credential and not a secret anybody guards — it is in
			// a browser, a log, a support ticket — and without this, knowing one was enough to
			// switch off somebody else's notifications from an account that may only read.
			if who := s.whoFrom(r); s.policy.Configured() && p.who[sub.Endpoint] != who {
				p.mu.Unlock()
				http.Error(w, "that subscription is not yours to remove", http.StatusForbidden)
				return
			}
			delete(p.subs, sub.Endpoint)
			delete(p.who, sub.Endpoint)
		} else {
			p.subs[sub.Endpoint] = sub
			// Recorded on every subscribe, not only the first: the same browser re-subscribes when
			// its endpoint rotates, and a stale name there would be a scope that outlived the
			// person it was granted to.
			p.who[sub.Endpoint] = s.whoFrom(r)
		}
		p.saveLocked(map[string]string{sub.Endpoint: text.Clip(r.UserAgent(), 120)})
		p.mu.Unlock()
		w.WriteHeader(http.StatusNoContent)
	default:
		http.Error(w, "GET or POST", http.StatusMethodNotAllowed)
	}
}

// watch tells the subscribed browsers when a companion starts waiting.
//
// # Why a poll and not the event stream
//
// The stream is per session and a browser opens it for the one companion it is looking at. This has
// to notice a companion nobody is looking at, which is the entire point, so it reads the same fleet
// list the page polls — through the same cache, so it costs a map read most of the time.
//
// # Why only the edge
//
// A companion stays blocked until somebody answers, so state alone would re-send every tick. Only
// the transition into waiting is an event. The opposite transition is not announced at all: "it is
// no longer waiting" is not worth a buzz, and a notification that arrives to say nothing is how
// people learn to swipe them away unread.
func (s *server) watch(ctx context.Context, every time.Duration) {
	t := time.NewTicker(every)
	defer t.Stop()
	// The first pass records what is already blocked without announcing it. Without this, starting
	// the console would notify for every agent that has been waiting since yesterday.
	first := true
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
		}
		list, err := fleet.ListCached(ctx, s.reader, s.cfgDir, s.here, &s.fleetCache)
		if err != nil {
			continue
		}
		p := s.pushes
		p.mu.Lock()
		news := newlyWaiting(p.was, list)
		// A companion that has just stopped working is the moment a handoff it was given can have an
		// answer: there is no reply channel, and the answer IS the last thing it said once it goes
		// quiet. Checked only on that edge, because reading it means replaying transcripts — the
		// cost /handoffs exists as its own endpoint to keep off the fleet poll.
		settled := justSettled(list, p.was)
		any := len(p.subs) > 0
		p.mu.Unlock()
		if first {
			first = false
			continue
		}
		for _, a := range news {
			if subs := p.mayHear(s.policy, a.Peer, a.Name); len(subs) > 0 {
				s.notify(a, subs)
			}
		}
		if len(settled) > 0 && any {
			s.notifyAnswers(ctx, settled)
		}
	}
}

// justSettled names the companions that were working or waiting a moment ago and are now idle.
//
// Read BEFORE newlyWaiting updates the map, so it sees the previous state. Called after, it would
// compare every companion against itself and never fire.
func justSettled(list []fleet.Agent, was map[string]fleet.State) []fleet.Agent {
	var out []fleet.Agent
	for _, a := range list {
		prev := was[a.Socket]
		if a.State == "idle" && (prev == "working" || prev == "waiting") {
			out = append(out, a)
		}
	}
	return out
}

// notifyAnswers tells the asker that work it handed out has come back.
//
// # Why this needed a channel at all
//
// A companion answers in its own transcript and the asker reads it. That is cheap and honest — no
// queue to lose things, no callback arriving mid-turn to derail the asker — but it means the answer
// sits there until somebody looks, and the person who would look is the one who walked away. The
// console has collected these on one page for a while; this is the part that reaches somebody who
// is not on that page.
//
// The notification names the ASKER, not the receiver. "buttons finished" is a fact about a
// companion; "design's question came back" is the thing the reader is waiting on.
func (s *server) notifyAnswers(ctx context.Context, settled []fleet.Agent) {
	list, err := fleet.Handoffs(ctx, s.reader, s.cfgDir, "", &s.fleetCache)
	if err != nil {
		return
	}
	done := map[string]bool{}
	for _, a := range settled {
		done[a.Name] = true
	}
	p := s.pushes
	for _, h := range list {
		if !done[h.To] || h.Answer == "" {
			continue
		}
		// Keyed on the request rather than on the pair: one companion can hand the same receiver two
		// different questions, and the second one is news even though the first was announced.
		key := h.From + "\x00" + h.To + "\x00" + h.Request
		p.mu.Lock()
		seen := p.told[key]
		p.told[key] = true
		p.mu.Unlock()
		if seen {
			continue
		}
		body, err := json.Marshal(map[string]string{
			// Who answered belongs in the body, not the headline. The guide caps a title at 29
			// characters and warns that a title cut off there does not come back when the
			// notification expands — and two names plus " answered " passes 29 on ordinary names.
			// The body has room and survives expansion, so it carries the pair.
			"title": h.From + " answered",
			"body":  text.Clip(h.To+": "+h.Answer, 80),
			"url":   "/?d=" + h.Socket,
			"tag":   "handoff:" + key,
		})
		if err != nil {
			continue
		}
		// Both names, because the payload carries both: the title is the asker and the body is the
		// receiver and what it said. Somebody scoped to one of the pair would be told the other
		// exists, and told what it answered.
		if subs := p.mayHear(s.policy, "", h.From, h.To); len(subs) > 0 {
			s.send(body, subs)
		}
	}
}

// mayHear is the subscriptions of people who may read every companion a payload names.
//
// A notification is a read performed while nobody is asking, so it is checked as one — the same
// Allows the routes use, not a scope test of its own. Two consequences worth stating: a person
// whose role lost `read` stops being buzzed without anybody remembering to unsubscribe them, and a
// subscription with no name attached hears nothing once the console has people in it.
func (p *pushState) mayHear(policy auth.Policy, peer string, names ...string) []webpush.Subscription {
	p.mu.Lock()
	defer p.mu.Unlock()
	out := make([]webpush.Subscription, 0, len(p.subs))
	for endpoint, sub := range p.subs {
		ok := true
		for _, name := range names {
			if !policy.Allows(p.who[endpoint], auth.Read, name, peer) {
				ok = false
				break
			}
		}
		if ok {
			out = append(out, sub)
		}
	}
	return out
}

// newlyWaiting returns the companions that have just started waiting, and updates was in place.
//
// Its own function because the rule is the whole feature and the rest of the loop is plumbing. A
// companion stays blocked until somebody answers it, so "is waiting" would fire every tick until
// then; only the EDGE is an event. Leaving does not fire at all — "it is no longer waiting" is not
// worth a buzz, and notifications that say nothing teach people to swipe them away unread.
func newlyWaiting(was map[string]fleet.State, list []fleet.Agent) []fleet.Agent {
	seen := make(map[string]bool, len(list))
	var news []fleet.Agent
	for _, a := range list {
		seen[a.Socket] = true
		if was[a.Socket] != "waiting" && a.State == "waiting" {
			news = append(news, a)
		}
		was[a.Socket] = a.State
	}
	// A companion that went away is forgotten, so that if it comes back already blocked it is news
	// again. Kept, it would be remembered as waiting and its next question would go unannounced.
	for sock := range was {
		if !seen[sock] {
			delete(was, sock)
		}
	}
	return news
}

// notify sends one companion's block to every subscribed browser.
func (s *server) notify(a fleet.Agent, subs []webpush.Subscription) {
	body, err := json.Marshal(map[string]string{
		"title": a.Name + " is waiting",
		// The question itself, not "an agent needs you": a person deciding whether to get their
		// phone out is deciding about this question, and a notification that withholds it makes
		// them open the console to find out whether it was worth opening the console.
		"body": text.Clip(a.Asking, 80),
		"url":  "/?d=" + a.Socket,
		// One tag per companion, so a second block from the same one replaces the first rather than
		// stacking. Three notifications from an agent that asked three things in a row is the same
		// information three times.
		"tag": a.Socket,
	})
	if err != nil {
		return
	}
	s.send(body, subs)
}

// send posts one payload to every subscription, forgetting the ones that are gone.
func (s *server) send(body []byte, subs []webpush.Subscription) {
	for _, sub := range subs {
		// Five minutes. "Somebody is waiting for you" is worth nothing an hour later, and a
		// notification that arrives after the question was answered teaches people to ignore them.
		switch err := s.pushes.keys.Send(s.pushes.http, sub, body, 5*time.Minute); {
		case err == nil:
		case err == webpush.Gone:
			s.pushes.mu.Lock()
			delete(s.pushes.subs, sub.Endpoint)
			s.pushes.saveLocked(nil)
			s.pushes.mu.Unlock()
		default:
			log.Printf("push: %v", err)
		}
	}
}

// serviceWorker is the script that receives a push while no page is open.
//
// It has to be served from the root to claim the whole scope, and it has to be its own file — a
// worker cannot be inlined into the page it serves. Deliberately small: everything it knows arrives
// in the message, so a change to what a notification says needs no new worker and no reinstall,
// which is the part of this that would otherwise go stale on somebody's phone for months.
func (s *server) serviceWorker(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "text/javascript; charset=utf-8")
	w.Header().Set("Cache-Control", "no-cache")
	// No modification time: the worker is compiled in, so its date is the binary's and a browser
	// asking "has it changed" wants the answer no-cache already gives. A wrong date here would let
	// a phone keep an old worker across an upgrade.
	http.ServeContent(w, r, "sw.js", time.Time{}, strings.NewReader(webassets.ServiceWorker))
}

// safePushEndpoint refuses, at subscribe time and WITHOUT a DNS lookup, an endpoint that could
// never be a real web-push target: a non-https scheme, no host, or an IP literal (FCM/Mozilla/Apple
// endpoints are named, not numbered — and an IP literal is how a caller would name an internal
// service directly). The DNS-dependent case — a name that resolves to an internal address, including
// a rebind that answers benign now and internal at send — is caught authoritatively by the send
// dialer (safePushDialer), which checks the address actually connected. Keeping this half DNS-free
// means it needs no network and cannot be defeated by resolution timing.
func safePushEndpoint(raw string) error {
	u, err := url.Parse(raw)
	if err != nil {
		return fmt.Errorf("that endpoint is not a URL")
	}
	if u.Scheme != "https" {
		return fmt.Errorf("a push endpoint must be https")
	}
	host := u.Hostname()
	if host == "" {
		return fmt.Errorf("that endpoint has no host")
	}
	if ip := net.ParseIP(host); ip != nil {
		return fmt.Errorf("a push endpoint must be a hostname, not an address")
	}
	return nil
}

// safePushDialer is the http.Client dialer for push sends: it rejects a connection whose resolved
// address is internal (loopback/link-local/private/unspecified), so a subscription whose hostname
// resolves — now or later, benign or rebound — to an internal target cannot be reached. This is the
// authoritative SSRF guard; the subscribe check is only an early, DNS-free refusal.
func safePushDialer() func(context.Context, string, string) (net.Conn, error) {
	d := &net.Dialer{
		Timeout: peerTimeout,
		Control: func(network, address string, c syscall.RawConn) error {
			host, _, err := net.SplitHostPort(address)
			if err != nil {
				return err
			}
			ip := net.ParseIP(host)
			if ip == nil {
				return fmt.Errorf("push endpoint did not resolve to an address")
			}
			if !ip.IsGlobalUnicast() || ip.IsPrivate() || ip.IsLinkLocalUnicast() || ip.IsLinkLocalMulticast() {
				return fmt.Errorf("push endpoint resolved to a non-public address")
			}
			return nil
		},
	}
	return d.DialContext
}
