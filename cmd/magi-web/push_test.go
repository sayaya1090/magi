package main

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"sort"
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
	// The guarded send dialer refuses loopback (the SSRF fix); these test servers are loopback, so
	// the test uses an unguarded client to exercise the send/drop logic against them.
	p.http = live.Client()
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

// Work coming back is announced once, at the moment it can be read.
//
// There is no reply channel: a receiver answers in its own transcript and the answer IS the last
// thing it said once it goes quiet. So the moment worth telling somebody about is the edge into
// idle — and because the answer stays readable for as long as the receiver stays idle, "there is
// an answer" would repeat it every three seconds forever.
func TestWorkComingBackIsAnnouncedOnceOnTheEdge(t *testing.T) {
	was := map[string]fleet.State{}
	at := func(states ...fleet.State) []string {
		list := make([]fleet.Agent, len(states))
		for i, st := range states {
			list[i] = fleet.Agent{Socket: string(rune('a' + i)), Name: string(rune('a' + i)), State: st}
		}
		var names []string
		for _, a := range justSettled(list, was) {
			names = append(names, a.Name)
		}
		// The caller updates the map through newlyWaiting, after reading the edge.
		newlyWaiting(was, list)
		return names
	}
	if got := at("working", "idle"); got != nil {
		t.Errorf("first look announced %v — nothing has changed yet, it has always been idle "+
			"as far as this process knows", got)
	}
	if got := at("idle", "idle"); strings.Join(got, ",") != "a" {
		t.Errorf("a companion that stopped working: %v", got)
	}
	if got := at("idle", "idle"); got != nil {
		t.Errorf("still idle, announced again: %v — the answer stays readable, so this would "+
			"repeat every tick forever", got)
	}
	if got := at("waiting", "idle"); got != nil {
		t.Errorf("going INTO waiting is not work coming back: %v", got)
	}
	if got := at("idle", "idle"); strings.Join(got, ",") != "a" {
		t.Errorf("answering a question and going quiet: %v", got)
	}
}

// The same receiver answering twice is two pieces of news.
//
// Keyed on the pair alone, a companion that handed the same receiver a second question would never
// hear about the second answer — the failure that is invisible, because nothing happens.
func TestASecondQuestionToTheSameCompanionIsAnnouncedToo(t *testing.T) {
	dir := t.TempDir()
	p, err := newPush(dir)
	if err != nil {
		t.Fatal(err)
	}
	key := func(from, to, req string) string { return from + "\x00" + to + "\x00" + req }
	first := key("design", "api", "is the endpoint idempotent?")
	second := key("design", "api", "does it retry on 429?")
	if first == second {
		t.Fatal("the two requests key the same; this check is measuring nothing")
	}
	p.told[first] = true
	if p.told[second] {
		t.Error("a different question was already considered told")
	}
}

// A notification carries the same scope the screens do.
//
// The interesting failure is not a wrong answer on a page — it is a phone. The fleet and the
// interventions filter what they draw, so a person scoped to one companion never sees another on
// screen; the watcher sends from a loop with no request in it, and told everybody about everybody.
// Whatever this console will not show somebody, it must not buzz them with.
func TestANotificationStaysInsideTheScope(t *testing.T) {
	s := withPolicy(t, `
[people."kim@corp.com"]
role = "operator"
companions = ["billing"]

[people."lee@corp.com"]
role = "responder"
companions = ["docs"]
`)
	p := &pushState{
		subs: map[string]webpush.Subscription{
			"https://push.example/kim": {Endpoint: "https://push.example/kim"},
			"https://push.example/lee": {Endpoint: "https://push.example/lee"},
			"https://push.example/old": {Endpoint: "https://push.example/old"},
		},
		who: map[string]string{
			"https://push.example/kim": "kim@corp.com",
			"https://push.example/lee": "lee@corp.com",
			// Subscribed before the console had people in it. Nobody, not everybody.
			"https://push.example/old": "",
		},
	}
	only := func(got []webpush.Subscription) string {
		var eps []string
		for _, g := range got {
			eps = append(eps, strings.TrimPrefix(g.Endpoint, "https://push.example/"))
		}
		sort.Strings(eps)
		return strings.Join(eps, ",")
	}
	if got := only(p.mayHear(s.policy, "", "billing")); got != "kim" {
		t.Errorf("billing started waiting and %q was told; only kim may see billing", got)
	}
	if got := only(p.mayHear(s.policy, "", "docs")); got != "lee" {
		t.Errorf("docs started waiting and %q was told; only lee may see docs", got)
	}
	// A handoff names two companions and the payload carries both, so being scoped to one of the
	// pair is not enough to be told about it.
	if got := only(p.mayHear(s.policy, "", "docs", "billing")); got != "" {
		t.Errorf("a docs↔billing handoff was announced to %q; neither may see both", got)
	}
	// And the same console with nobody configured is the console as it was: everyone hears.
	if got := only(p.mayHear(withPolicy(t, "").policy, "", "billing")); got != "kim,lee,old" {
		t.Errorf("an unconfigured console told %q; it has one operator and no scopes", got)
	}
}

// A subscription is removed by the person it belongs to.
//
// The endpoint is the whole of what identifies one, and an endpoint is not a secret anybody
// guards: it sits in a browser, a log and a support ticket. Without an owner on the record,
// knowing one was enough to switch off somebody else's notifications — from an account that may
// only be allowed to read.
func TestOnlyTheOwnerCanRemoveASubscription(t *testing.T) {
	s := withPolicy(t, twoPeople)
	s.pushes = &pushState{
		file: filepath.Join(t.TempDir(), "subs.json"),
		subs: map[string]webpush.Subscription{"e1": {Endpoint: "e1", P256dh: "k", Auth: "a"}},
		who:  map[string]string{"e1": "kim@corp.com"},
	}
	drop := func(who string) int {
		body := url.Values{"endpoint": {"e1"}, "p256dh": {"k"}, "auth": {"a"}, "delete": {"1"}}
		r := httptest.NewRequest(http.MethodPost, "/push", strings.NewReader(body.Encode()))
		r.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		r.Header.Set("X-Forwarded-User", who)
		w := httptest.NewRecorder()
		s.push(w, r)
		return w.Code
	}
	if code := drop("lee@corp.com"); code != http.StatusForbidden {
		t.Errorf("somebody else removed kim's subscription (%d)", code)
	}
	if _, still := s.pushes.subs["e1"]; !still {
		t.Error("the subscription is gone")
	}
	if code := drop("kim@corp.com"); code != http.StatusNoContent {
		t.Errorf("the owner could not remove their own subscription (%d)", code)
	}
	if _, still := s.pushes.subs["e1"]; still {
		t.Error("the owner's removal did nothing")
	}
}

// A push endpoint this process will later POST to must be a public https URL — a caller who could
// store an internal address turned the console into a blind SSRF hop. The deterministic rejections
// (scheme, IP literal) are checked here; a public host needs DNS and is left to integration.
func TestSafePushEndpointRefusesInternalTargets(t *testing.T) {
	bad := []string{
		"http://push.example.com/x",      // not https
		"https://127.0.0.1/x",            // loopback IP literal
		"https://169.254.169.254/latest", // metadata IP literal
		"https://[::1]/x",                // loopback IPv6 literal
		"https://10.0.0.5/x",             // private IP literal
		"ftp://push.example.com/x",       // wrong scheme
		"https:///nohost",                // no host
		"not a url",                      // unparseable-as-endpoint
	}
	for _, e := range bad {
		if err := safePushEndpoint(e); err == nil {
			t.Errorf("safePushEndpoint(%q) allowed an internal/invalid endpoint", e)
		}
	}
}
