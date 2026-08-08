package main

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/sayaya1090/magi/internal/adapter/fleet"
	"github.com/sayaya1090/magi/internal/core/webpush"
)

// Only the moment a companion starts waiting is news.
//
// A blocked companion stays blocked until somebody answers it, so a notifier that looked at state
// would buzz every three seconds until then. The other direction is deliberately silent: "it is no
// longer waiting" is not worth waking a phone for, and notifications that say nothing are how
// people learn to swipe them away without reading.
func TestOnlyTheMomentOfBlockingIsAnnounced(t *testing.T) {
	was := map[string]fleet.State{}
	at := func(states ...fleet.State) []string {
		list := make([]fleet.Agent, len(states))
		for i, st := range states {
			list[i] = fleet.Agent{Socket: string(rune('a' + i)), Name: string(rune('a' + i)), State: st}
		}
		var names []string
		for _, a := range newlyWaiting(was, list) {
			names = append(names, a.Name)
		}
		return names
	}

	if got := at("working", "waiting"); strings.Join(got, ",") != "b" {
		t.Errorf("first look: %v — the one that is waiting is news", got)
	}
	if got := at("working", "waiting"); got != nil {
		t.Errorf("still waiting, announced again: %v", got)
	}
	if got := at("waiting", "waiting"); strings.Join(got, ",") != "a" {
		t.Errorf("a second one blocked: %v", got)
	}
	if got := at("waiting", "working"); got != nil {
		t.Errorf("unblocking was announced: %v — a notification that says nothing trains people to ignore them", got)
	}
	if got := at("waiting", "waiting"); strings.Join(got, ",") != "b" {
		t.Errorf("blocked again after being answered: %v", got)
	}

	// A companion that goes away is forgotten. Remembered as waiting, its next question would go
	// unannounced — the failure that is invisible, because nothing happens.
	was = map[string]fleet.State{}
	_ = at("waiting")
	if len(was) != 1 {
		t.Fatalf("nothing was remembered: %v", was)
	}
	if got := newlyWaiting(was, nil); got != nil || len(was) != 0 {
		t.Errorf("a departed companion left %v behind", was)
	}
	if got := at("waiting"); strings.Join(got, ",") != "a" {
		t.Errorf("it came back already blocked and said nothing: %v", got)
	}
}

// A subscription arrives, is kept, and can be taken back.
func TestASubscriptionIsKeptAndCanBeWithdrawn(t *testing.T) {
	dir := t.TempDir()
	p, err := newPush(dir)
	if err != nil {
		t.Fatal(err)
	}
	s := &server{pushes: p}

	get := func() map[string]any {
		w := httptest.NewRecorder()
		s.push(w, httptest.NewRequest(http.MethodGet, "/push", nil))
		var out map[string]any
		if err := json.Unmarshal(w.Body.Bytes(), &out); err != nil {
			t.Fatalf("%v: %s", err, w.Body)
		}
		return out
	}
	// The page cannot subscribe without the application server key, so this is the one thing GET
	// must always carry.
	if k, _ := get()["key"].(string); len(k) != 87 || !strings.HasPrefix(k, "B") {
		t.Errorf("the key is %q; a base64url P-256 point is 87 characters and starts with B", k)
	}

	post := func(v url.Values) int {
		r := httptest.NewRequest(http.MethodPost, "/push", strings.NewReader(v.Encode()))
		r.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		w := httptest.NewRecorder()
		s.push(w, r)
		return w.Code
	}
	full := url.Values{"endpoint": {"https://push.example/x"}, "p256dh": {"BAAA"}, "auth": {"AAAA"}}
	if code := post(full); code != http.StatusNoContent {
		t.Fatalf("subscribing answered %d", code)
	}
	if n, _ := get()["count"].(float64); n != 1 {
		t.Errorf("count %v after one subscription", n)
	}

	// A partial subscription is refused rather than stored: two of the three fields are useless
	// without the third, and a half-record would fail at send time on a machine nobody is watching.
	for _, missing := range []string{"endpoint", "p256dh", "auth"} {
		v := url.Values{}
		for k, val := range full {
			if k != missing {
				v[k] = val
			}
		}
		if code := post(v); code != http.StatusBadRequest {
			t.Errorf("a subscription with no %s was answered %d", missing, code)
		}
	}

	// On disk, and readable only by its owner: the endpoint is a credential, since anyone who holds
	// it can send to that browser.
	f := filepath.Join(dir, "push-subscriptions.json")
	st, err := os.Stat(f)
	if err != nil {
		t.Fatal(err)
	}
	if m := st.Mode().Perm(); m != 0o600 {
		t.Errorf("subscriptions are mode %o; the endpoint is a credential", m)
	}
	if st2, err := os.Stat(filepath.Join(dir, "push-key")); err != nil {
		t.Error(err)
	} else if m := st2.Mode().Perm(); m != 0o600 {
		t.Errorf("the private key is mode %o", m)
	}

	// And it survives a restart. A console that forgot its subscriptions on restart would go quiet
	// with nothing to show for it.
	again, err := newPush(dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(again.subs) != 1 {
		t.Errorf("%d subscriptions came back", len(again.subs))
	}
	// The identity must come back too: a browser records which key subscribed it and silently drops
	// messages signed by another, so a new key on restart is a console that notifies nobody.
	if again.keys.PublicKey() != p.keys.PublicKey() {
		t.Error("the push identity changed across a restart; every subscription already handed out is dead")
	}

	if code := post(url.Values{"endpoint": {"https://push.example/x"}, "p256dh": {"-"}, "auth": {"-"}, "delete": {"1"}}); code != http.StatusNoContent {
		t.Fatalf("unsubscribing answered %d", code)
	}
	if n, _ := get()["count"].(float64); n != 0 {
		t.Errorf("count %v after withdrawing", n)
	}
}

// What the phone is shown, and what happens to a subscription that has died.
func TestTheNotificationCarriesTheQuestionAndDeadSubscriptionsAreDropped(t *testing.T) {
	dir := t.TempDir()
	p, err := newPush(dir)
	if err != nil {
		t.Fatal(err)
	}

	var bodies int
	dead := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		bodies++
		w.WriteHeader(http.StatusGone)
	}))
	defer dead.Close()
	live := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		bodies++
		w.WriteHeader(http.StatusCreated)
	}))
	defer live.Close()

	const key = "BCVxsr7N_eNgVRqvHtD0zTZsEc6-VV-JvLexhqUzORcxaOzi6-AYWXvTBHm4bjyPjs7Vd8pZGH6SRpkNtoIAiw4"
	subs := []webpush.Subscription{
		{Endpoint: live.URL + "/a", P256dh: key, Auth: "BTBZMqHH6r4Tts7J_aSIgg"},
		{Endpoint: dead.URL + "/b", P256dh: key, Auth: "BTBZMqHH6r4Tts7J_aSIgg"},
	}
	for _, sub := range subs {
		p.subs[sub.Endpoint] = sub
	}
	s := &server{pushes: p, http: live.Client()}
	s.notify(fleet.Agent{
		Socket: "/s/design.sock", Name: "design",
		Asking: "which surface should the empty state sit on?",
	}, subs)

	if bodies != 2 {
		t.Errorf("%d of 2 subscriptions were sent to", bodies)
	}
	// The one that answered 410 is forgotten; the other is not. Keeping dead subscriptions costs a
	// request per stale phone on every alert, and dropping live ones on a blip is silent.
	if _, still := p.subs[dead.URL+"/b"]; still {
		t.Error("a subscription the push service says is gone was kept")
	}
	if _, ok := p.subs[live.URL+"/a"]; !ok {
		t.Error("a working subscription was dropped")
	}
}
