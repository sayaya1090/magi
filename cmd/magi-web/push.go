package main

import (
	"context"
	"encoding/json"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/sayaya1090/magi/internal/adapter/fleet"
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
// # What this does NOT do
//
// It does not decide who may subscribe. This console has no users and no login — whoever reaches
// the port reaches the page, which is the standing position here and belongs to whatever put the
// port there. So anyone who can open the console can point a notification at their own browser.
// That is the same access they already have to read every transcript on it.
//
// ⚠ A service worker only registers in a secure context, so a console reached over plain http from
// another machine cannot turn this on at all. The page says so rather than offering a switch that
// does nothing. Reached over localhost or behind TLS, it works.
type pushState struct {
	keys *webpush.Keys
	file string

	mu   sync.Mutex
	subs map[string]webpush.Subscription // by endpoint

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
		keys: keys,
		file: filepath.Join(cfgDir, "push-subscriptions.json"),
		subs: map[string]webpush.Subscription{},
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
	}
}

// saveLocked writes the store. Callers hold p.mu.
func (p *pushState) saveLocked(agents map[string]string) {
	list := make([]storedSub, 0, len(p.subs))
	for _, s := range p.subs {
		list = append(list, storedSub{Subscription: s, Added: time.Now().UTC().Format(time.RFC3339), Agent: agents[s.Endpoint]})
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
		p.mu.Lock()
		if r.FormValue("delete") == "1" {
			delete(p.subs, sub.Endpoint)
		} else {
			p.subs[sub.Endpoint] = sub
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
		subs := make([]webpush.Subscription, 0, len(p.subs))
		for _, sub := range p.subs {
			subs = append(subs, sub)
		}
		p.mu.Unlock()
		if first {
			first = false
			continue
		}
		for _, a := range news {
			s.notify(a, subs)
		}
		if len(settled) > 0 && len(subs) > 0 {
			s.notifyAnswers(ctx, settled, subs)
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
func (s *server) notifyAnswers(ctx context.Context, settled []fleet.Agent, subs []webpush.Subscription) {
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
			"title": h.To + " answered " + h.From,
			"body":  text.Clip(h.Answer, 160),
			"url":   "/?d=" + h.Socket,
			"tag":   "handoff:" + key,
		})
		if err != nil {
			continue
		}
		s.send(body, subs)
	}
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
		"body": text.Clip(a.Asking, 160),
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
		switch err := s.pushes.keys.Send(s.http, sub, body, 5*time.Minute); {
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
	http.ServeContent(w, r, "sw.js", time.Time{}, strings.NewReader(swJS))
}

const swJS = `// magi's service worker. It exists to receive notifications; it caches nothing.
//
// Caching would make the console show a stale fleet, and a stale fleet is worse than no fleet: the
// whole page is an answer to "what is happening right now".
self.addEventListener('push', e => {
  let m = {};
  try { m = e.data ? e.data.json() : {}; } catch (_) { m = {body: e.data ? e.data.text() : ''}; }
  e.waitUntil(self.registration.showNotification(m.title || 'magi', {
    body: m.body || '',
    tag: m.tag || 'magi',
    // Replaces the earlier notification for this companion without a second buzz: the tag already
    // makes it silent-replace on some platforms and explicit on the rest.
    renotify: false,
    icon: '/icon.svg',
    badge: '/icon.svg',
    data: {url: m.url || '/'},
  }));
});
self.addEventListener('notificationclick', e => {
  e.notification.close();
  const want = new URL(e.notification.data && e.notification.data.url || '/', self.location.origin).href;
  // A console already open is focused rather than opened again. Somebody with the fleet on a second
  // screen does not want a third copy of it.
  e.waitUntil(clients.matchAll({type: 'window', includeUncontrolled: true}).then(list => {
    for (const c of list) {
      if (c.url.startsWith(self.location.origin) && 'focus' in c) return c.navigate(want).then(x => x.focus());
    }
    return clients.openWindow(want);
  }));
});
`
