package main

import (
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

// The page's own code, run.
//
// Everything here below the syntax check used to be unverified: whether a card gets the class its
// state deserves, whether a blocked agent grows the buttons that answer it, whether the empty state
// says how to start a daemon. The front end is a Go string, so no Go test could execute it and no
// browser is available to — which is how a dashboard ships with a card that renders "undefined".
//
// testdata/dom.mjs is the smallest object graph the page will accept. The page script is dropped in
// between it and a scenario, node runs the three together, and the scenario prints what came out as
// JSON so the assertions read like Go.

// runPage renders the page against the fake DOM with one fleet payload and returns what it drew.
// runPageAt is runPage with the page mounted somewhere other than the root.
func runPageAt(t *testing.T, base, epilogue string) map[string]any {
	t.Helper()
	t.Setenv("BASE", base)
	return runPage(t, `[]`, "", epilogue)
}

func runPage(t *testing.T, fleetJSON, query, epilogue string) map[string]any {
	t.Helper()
	node, err := exec.LookPath("node")
	if err != nil {
		t.Skip("no node on this machine; the page cannot be run here")
	}
	dir := t.TempDir()
	harness, err := os.ReadFile("testdata/dom.mjs")
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "dom.mjs"), harness, 0o644); err != nil {
		t.Fatal(err)
	}
	// The ids the fake answers to, taken FROM THE MARKUP rather than kept by hand beside it.
	//
	// That list had to be edited every time an element was added, and the way it announced a
	// missing entry was a thrown error in an unrelated test — five of them at once, naming a
	// lookup rather than the thing that had changed. A markup id is exactly the set the page can
	// ask for, so it is the set the fake should have.
	var ids []string
	for _, m := range regexp.MustCompile(`\sid="([A-Za-z][\w-]*)"`).FindAllStringSubmatch(indexHTML, -1) {
		ids = append(ids, m[1])
	}
	if len(ids) < 20 {
		t.Fatalf("only %d ids found in the markup; the scrape has lost its subject", len(ids))
	}
	list, err := json.Marshal(ids)
	if err != nil {
		t.Fatal(err)
	}
	// And the one markup ATTRIBUTE the page reads back off its own elements. The fake builds a stub
	// per id and knows nothing of what the HTML wrote on them, which is fine for the attributes the
	// page only ever sets — but data-may is written in the markup and read by applyMay, so without
	// this the permission gate looked as though it covered nothing.
	may := map[string]string{}
	for _, m := range regexp.MustCompile(`<[^>]*\sid="([A-Za-z][\w-]*)"[^>]*\sdata-may="(\w+)"`).
		FindAllStringSubmatch(indexHTML, -1) {
		may[m[1]] = m[2]
	}
	mayJSON, err := json.Marshal(may)
	if err != nil {
		t.Fatal(err)
	}
	// And which of them the markup starts HIDDEN. Every stub the fake builds was visible from its
	// first frame while the real page starts most of its panels closed, so a question like "is
	// there anything in the side pane" answered yes here and no in a browser — the fake diverging
	// in the direction that hides a bug, which is the one direction it must not.
	var hidden []string
	for _, m := range regexp.MustCompile(`<[^>]*\sid="([A-Za-z][\w-]*)"[^>]*\shidden[\s>]`).
		FindAllStringSubmatch(indexHTML, -1) {
		hidden = append(hidden, m[1])
	}
	hiddenJSON, err := json.Marshal(hidden)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "ids.mjs"),
		[]byte("export const MARKUP_IDS = "+string(list)+";\n"+
			"export const MARKUP_MAY = "+string(mayJSON)+";\n"+
			"export const MARKUP_HIDDEN = "+string(hiddenJSON)+";\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	// The page imports the vendored bundle by the path this binary serves it at. Node resolves
	// neither that path nor a fetch of it, so the file is copied in beside the page and the
	// specifier rewritten — the same bytes, reached differently.
	body := scriptBody(t, indexHTML)
	// RxJS is the real bundle: it is plain javascript and runs anywhere.
	rx, err := assetFS.ReadFile("vendor/rxjs.js")
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "rxjs.js"), rx, 0o644); err != nil {
		t.Fatal(err)
	}
	body = strings.ReplaceAll(body, "'/vendor/rxjs.js'", "'./rxjs.js'")

	// marked is the real bundle too, and for the same reason: only its lexer is imported, and a
	// lexer is a string in and objects out. Stubbing it would leave the one part of the transcript
	// these tests most need to see — what the renderer builds from those tokens — untested.
	mk, err := assetFS.ReadFile("vendor/marked.js")
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "marked.js"), mk, 0o644); err != nil {
		t.Fatal(err)
	}
	body = strings.ReplaceAll(body, "'/vendor/marked.js'", "'./marked.js'")

	// Material Web is STUBBED here, and that is a statement about what these tests check. The
	// library builds on custom elements, shadow roots and constructable stylesheets — a browser —
	// and giving the fake DOM enough of those to load it would mean writing a browser to test a
	// page. So the import resolves to nothing: the components stay un-upgraded, which in a DOM is
	// an ordinary element with its light-DOM children, and the page's own logic is what runs.
	//
	// What that leaves uncovered is the components' BEHAVIOUR — ripple, focus ring, disabled — and
	// that is the library's to get right, seen in the demo and in a browser. What it still covers
	// is everything this page decides: which element, what text, which handler, what it sends.
	if err := os.WriteFile(filepath.Join(dir, "material.js"), []byte(
		"// stub — see runPage in render_test.go\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	body = strings.ReplaceAll(body, "'/vendor/material.js'", "'./material.js'")
	script := "import { byId, RENDERED } from './dom.mjs';\n" + body + "\n" + epilogue
	main := filepath.Join(dir, "page.mjs")
	if err := os.WriteFile(main, []byte(script), 0o644); err != nil {
		t.Fatal(err)
	}
	cmd := exec.Command(node, main)
	// The English pack the binary serves, handed to the fake fetch: the page's labels come from
	// there in a browser, and a test that supplied its own copy would be checking a fixture.
	pack, err := assetFS.ReadFile("i18n/language.en.json")
	if err != nil {
		t.Fatal(err)
	}
	cmd.Env = append(os.Environ(), "FLEET_JSON="+fleetJSON, "QUERY="+query, "LANG_PACK="+string(pack))
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("the page threw while rendering:\n%s", out)
	}
	// The scenario's last line is the JSON; anything before it is the page's own noise.
	lines := strings.Split(strings.TrimSpace(string(out)), "\n")
	var got map[string]any
	if err := json.Unmarshal([]byte(lines[len(lines)-1]), &got); err != nil {
		t.Fatalf("the scenario did not print JSON (%v):\n%s", err, out)
	}
	return got
}

// rows() is the table's data rows: the head is a child of #fleet too, and it is not an agent.
const rowsHelper = `
const rows = () => byId.fleet.children.filter(c => c.className.startsWith('card'));
`

const dumpFleet = rowsHelper + `
await loadFleet();
// An icon is a shape and contributes no text, so the marks are collected as what they point at.
// Without this a row whose mark became a drawing reads, to a test, as a row with no mark.
const refsOf = n => (n.find('use') || []).map(u => u.attrs.href || '').join(' ');
const cards = rows().map(c => ({
  cls: c.className,
  text: c.text + ' ' + refsOf(c),
  buttons: (c.find('div').find(d => d.className === 'answer') || {find: () => []}).find(clicky).map(b => b.textContent),
  actions: (c.find('div').find(d => d.className === 'actions') || {find: () => []}).find(clicky).map(b => b.textContent),
  inputs: c.find('md-outlined-text-field').length,
}));
console.log(JSON.stringify({cards, state: byId.state.text, stateCls: byId.state.className}));
`

// The dashboard draws one card per agent, and the card's class is how a person picks it out of
// twenty at a glance.
func TestTheDashboardDrawsACardPerAgent(t *testing.T) {
	got := runPage(t, `[
      {"socket":"/s/a.sock","name":"api","workdir":"/w/api","state":"working","live":true,
       "task":"make the tests pass","steps":7,"idle":12,"here":true},
      {"socket":"/s/b.sock","name":"docs","workdir":"/w/docs","state":"stopped","live":false,
       "task":"done here","steps":0,"idle":7200,"here":false}
    ]`, "", dumpFleet)

	cards, _ := got["cards"].([]any)
	if len(cards) != 2 {
		t.Fatalf("drew %d cards for two agents", len(cards))
	}
	first := cards[0].(map[string]any)
	cls, text := first["cls"].(string), first["text"].(string)
	for _, want := range []string{"card", "working", "here"} {
		if !strings.Contains(cls, want) {
			t.Errorf("the working local agent's card is %q, missing %q", cls, want)
		}
	}
	for _, want := range []string{"api", "/w/api", "make the tests pass", "7", "this directory"} {
		if !strings.Contains(text, want) {
			t.Errorf("the card does not show %q: %q", want, text)
		}
	}
	// An age reads as an age. "7200" on a card is a number nobody converts in their head.
	second := cards[1].(map[string]any)["text"].(string)
	if !strings.Contains(second, "2h ago") {
		t.Errorf("the stopped agent's age is not readable: %q", second)
	}
	if s := got["state"].(string); !strings.Contains(s, "2 agents") {
		t.Errorf("the header says %q", s)
	}
}

// A blocked agent is the reason to look at a dashboard, so its card says what is being decided and
// carries the buttons that decide it. Those buttons are also the only control on the page that
// writes to an agent you have not opened.
func TestABlockedAgentGetsTheButtonsThatAnswerIt(t *testing.T) {
	got := runPage(t, `[
      {"socket":"/s/a.sock","name":"api","workdir":"/w/api","state":"waiting","live":true,
       "asking":"bash: rm -rf build  (destructive command detected)","askId":"call_7",
       "askKind":"permission","task":"clean the tree","steps":3,"idle":9,"here":false}
    ]`, "", dumpFleet)

	card := got["cards"].([]any)[0].(map[string]any)
	if !strings.Contains(card["cls"].(string), "waiting") {
		t.Errorf("a blocked agent's card is %q", card["cls"])
	}
	if !strings.Contains(card["text"].(string), "rm -rf build") {
		t.Errorf("the card does not say what is being decided: %q", card["text"])
	}
	var labels []string
	for _, b := range card["buttons"].([]any) {
		labels = append(labels, b.(string))
	}
	// Four answers, and the two in the middle say how long they last: one grants THIS tool for
	// THIS session and leaves the mode where it was, the other writes it into this companion's own
	// settings so a restart does not ask again. The terminal has had both for a long time; this
	// console offered only the first, so the only way to stop being asked tomorrow was to go and
	// find a terminal.
	if strings.Join(labels, "/") != "Allow/Stop asking for this tool/Keep for this companion/Deny" {
		t.Errorf("the answer buttons are %v", labels)
	}
	if n := card["inputs"].(float64); n != 0 {
		t.Errorf("a permission prompt drew %v text inputs; it is a choice, not a sentence", n)
	}
}

// A question is a sentence, so it gets somewhere to type it.
func TestAQuestionGetsSomewhereToTypeTheAnswer(t *testing.T) {
	got := runPage(t, `[
      {"socket":"/s/a.sock","name":"api","workdir":"/w/api","state":"waiting","live":true,
       "asking":"which branch should this land on?","askId":"q1#1","askKind":"question",
       "task":"port the handler","steps":1,"idle":4,"here":false}
    ]`, "", dumpFleet)

	card := got["cards"].([]any)[0].(map[string]any)
	if n := card["inputs"].(float64); n != 1 {
		t.Errorf("a question drew %v inputs, want one", n)
	}
	var labels []string
	for _, b := range card["buttons"].([]any) {
		labels = append(labels, b.(string))
	}
	if strings.Join(labels, "/") != "Answer" {
		t.Errorf("a question's buttons are %v, want a single Answer", labels)
	}
}

// Nothing running is a state with an answer, not a blank page.
func TestAnEmptyFleetSaysHowToStartOne(t *testing.T) {
	got := runPage(t, `[]`, "", `
await loadFleet();
console.log(JSON.stringify({empty: byId.fleet.text}));
`)
	// Asserted against the PACK, not against a copy of the wording. The page had one sentence and
	// the pack another — "No magi daemons under this config directory." against "No magi is running
	// under this config directory." — and each was true on its own, so nothing said they had
	// drifted. Now the page must show what the pack says, whatever the pack says.
	text := got["empty"].(string)
	for _, key := range []string{"empty.no_agents", "empty.no_agents_how"} {
		want := packEntry(t, key)
		// The markup in the second line is the pack's; the fake DOM reports text, not tags.
		want = strings.ReplaceAll(strings.ReplaceAll(want, "<code>", ""), "</code>", "")
		if !strings.Contains(text, want) {
			t.Errorf("the empty state does not carry %s (%q): %q", key, want, text)
		}
	}
}

// packEntry reads one label out of the English pack this binary serves.
func packEntry(t *testing.T, key string) string {
	t.Helper()
	raw, err := assetFS.ReadFile("i18n/language.en.json")
	if err != nil {
		t.Fatal(err)
	}
	var pack map[string]string
	if err := json.Unmarshal(raw, &pack); err != nil {
		t.Fatal(err)
	}
	v, ok := pack[key]
	if !ok {
		t.Fatalf("the pack has no %q", key)
	}
	return v
}

// Waiting agents are counted in the header, because the reason to glance at this page is to find
// out whether anything needs you.
func TestTheHeaderCountsWhatIsWaitingOnYou(t *testing.T) {
	got := runPage(t, `[
      {"socket":"/s/a.sock","name":"a","workdir":"/w/a","state":"waiting","live":true,
       "asking":"permission: bash","askId":"c1","askKind":"permission","idle":3},
      {"socket":"/s/b.sock","name":"b","workdir":"/w/b","state":"working","live":true,"idle":1},
      {"socket":"/s/c.sock","name":"c","workdir":"/w/c","state":"waiting","live":true,
       "asking":"permission: write","askId":"c2","askKind":"permission","idle":2}
    ]`, "", dumpFleet)
	state := got["state"].(string)
	if !strings.Contains(state, "2 waiting on you") {
		t.Errorf("the header says %q, and two agents are blocked", state)
	}
	// "asking", not "lost". They are different facts and they used to share this one class, so
	// whichever wrote last won — and the fleet poll writes every three seconds, which meant a
	// genuinely dropped stream showed for 400ms and then the console went back to claiming it was
	// connected. The red dot is the connection's; this one is the warn dot beside it.
	cls := got["stateCls"].(string)
	if !strings.Contains(cls, "asking") {
		t.Errorf("the header is %q, so a blocked fleet looks like a calm one", cls)
	}
	if strings.Contains(cls, "lost") {
		t.Errorf("the header is %q — that class means the stream is gone, and writing it for "+
			"somebody waiting both says the wrong thing and clears the real one", cls)
	}
}

// The transcript is the other half of the page: one row per part, each classed by what it is.
func TestTheTranscriptRowsAreClassedByWhatTheyAre(t *testing.T) {
	got := runPage(t, `[]`, "?d=%2Fs%2Fa.sock", `
draw([{who:'user',text:'do the thing'},{who:'thinking',text:'considering'},
      {who:'tool',text:'bash go build ./...'},{who:'failed',text:'exit 1'}]);
console.log(JSON.stringify({
  rows: byId.log.children.map(r => ({cls: r.className, text: r.text})),
  composerHidden: byId.f.hidden, fleetHidden: byId.fleet.hidden,
  bodyAttrs: Object.keys(document.body.attrs || {}).join(' '),
}));
`)
	rows := got["rows"].([]any)
	if len(rows) != 4 {
		t.Fatalf("four parts drew %d rows", len(rows))
	}
	want := []string{"row user", "row thinking", "row tool", "row failed"}
	for i, r := range rows {
		if cls := r.(map[string]any)["cls"].(string); cls != want[i] {
			t.Errorf("row %d is %q, want %q", i, cls, want[i])
		}
	}
	if !strings.Contains(rows[2].(map[string]any)["text"].(string), "go build") {
		t.Error("a tool row lost what the tool was asked to do")
	}
	// With ?d= the page is on one agent: the composer is there and the fleet is not.
	if got["composerHidden"].(bool) {
		t.Error("the composer is hidden on an agent's page")
	}
	// The list is gone, at every width. It used to stay beside the companion from 1200px, which was
	// the previous screen drawn over the current one — the roster is a monitoring view, there is a
	// screen whose whole job is that, and it is the screen you came from.
	if !got["fleetHidden"].(bool) {
		t.Error("the roster is still on screen beside a companion")
	}
	if strings.Contains(got["bodyAttrs"].(string), "list-detail") {
		t.Errorf("the page is still in list-detail: %v", got["bodyAttrs"])
	}
}

// And narrow, where there is no room for two: opening a companion replaces the list.
func TestANarrowScreenOpensACompanionInsteadOfTheList(t *testing.T) {
	t.Setenv("NARROW", "1")
	got := runPage(t, `[{"socket":"/s/a.sock","name":"api","workdir":"/w/a","state":"idle","live":true,"idle":1}]`,
		"?d=/s/a.sock", `
console.log(JSON.stringify({fleetHidden: byId.fleet.hidden,
  bodyAttrs: Object.keys(document.body.attrs || {}).join(' ')}));
`)
	if !got["fleetHidden"].(bool) {
		t.Error("the list is still drawn under the transcript on a narrow screen")
	}
	if strings.Contains(got["bodyAttrs"].(string), "list-detail") {
		t.Error("a narrow screen is in list-detail, and there is no room for two panes")
	}
}

// Sending goes to the agent the page is on, as a form post to the right route.
func TestSendingPostsToTheAgentOnScreen(t *testing.T) {
	got := runPage(t, `[]`, "?d=%2Fs%2Fa.sock", `
document.getElementById('t').value = 'do it again';
await f.onsubmit({preventDefault(){}});
const posts = RENDERED.filter(r => r.method === 'POST');
console.log(JSON.stringify({posts, subscribed: RENDERED.filter(r => r.subscribed).map(r => r.subscribed)}));
`)
	posts := got["posts"].([]any)
	if len(posts) != 1 {
		t.Fatalf("submitting sent %d requests", len(posts))
	}
	p := posts[0].(map[string]any)
	if !strings.HasPrefix(p["fetched"].(string), "/submit?d=") {
		t.Errorf("the steer went to %q", p["fetched"])
	}
	if !strings.Contains(p["body"].(string), "do+it+again") {
		t.Errorf("the text did not travel: %q", p["body"])
	}
	subs := got["subscribed"].([]any)
	if len(subs) != 1 || !strings.HasPrefix(subs[0].(string), "/events?d=") {
		t.Errorf("the page subscribed to %v", subs)
	}
}

// An agent's own page has to show what it is blocked on.
//
// This was the one place you could not see it. The prompt is not in the log — it is a question
// about what should happen, not a record of what did — so the transcript on an agent's page shows
// a run that has simply stopped, and the only way to find out was to go back to the fleet. Which
// is the opposite of where you would be: you opened this agent because you were watching it.
func TestAnAgentsOwnPageShowsThePromptItIsBlockedOn(t *testing.T) {
	got := runPage(t, `[
      {"socket":"/s/a.sock","name":"api","workdir":"/w/api","state":"waiting","live":true,
       "asking":"bash: rm -rf build","askId":"call_7","askKind":"permission","idle":5},
      {"socket":"/s/b.sock","name":"other","workdir":"/w/b","state":"working","live":true,"idle":1}
    ]`, "?d=%2Fs%2Fa.sock", `
await loadFleet();
const box = document.getElementById('prompt');
console.log(JSON.stringify({
  hidden: box.hidden, text: box.text,
  buttons: box.find(clicky).map(b => b.textContent),
  title: document.title,
}));
`)
	if got["hidden"].(bool) {
		t.Fatal("the agent's page hides the prompt it is waiting on — the transcript just stops")
	}
	if !strings.Contains(got["text"].(string), "rm -rf build") {
		t.Errorf("the prompt does not say what is being decided: %q", got["text"])
	}
	var labels []string
	for _, b := range got["buttons"].([]any) {
		labels = append(labels, b.(string))
	}
	if strings.Join(labels, "/") != "Allow/Stop asking for this tool/Keep for this companion/Deny" {
		t.Errorf("the answer buttons on the agent's page are %v", labels)
	}
	// One agent is waiting, so the tab says so — this page is often behind an app switcher — and it
	// names where you are, which with four sections is the outermost breadcrumb anybody reads.
	if got["title"].(string) != "(1) magi · a" {
		t.Errorf("the tab title is %q, and one agent is waiting on its own page", got["title"])
	}
}

// An agent that is not waiting shows no prompt, and the title goes back to plain. A bar that stays
// up after the thing is answered is worse than one that never appeared.
func TestThePromptGoesAwayWhenItIsAnswered(t *testing.T) {
	got := runPage(t, `[
      {"socket":"/s/a.sock","name":"api","workdir":"/w/api","state":"working","live":true,
       "task":"carrying on","steps":4,"idle":2}
    ]`, "?d=%2Fs%2Fa.sock", `
await loadFleet();
console.log(JSON.stringify({hidden: document.getElementById('prompt').hidden, title: document.title}));
`)
	if !got["hidden"].(bool) {
		t.Error("a working agent's page shows a prompt bar")
	}
	if got["title"].(string) != "magi · a" {
		t.Errorf("the tab title is %q with nothing waiting", got["title"])
	}
}

// The dashboard's title counts every agent that needs somebody, not just the one on screen.
func TestTheTabTitleCountsEveryWaitingAgent(t *testing.T) {
	got := runPage(t, `[
      {"socket":"/s/a.sock","name":"a","workdir":"/w/a","state":"waiting","live":true,
       "asking":"permission: bash","askId":"c1","askKind":"permission","idle":3},
      {"socket":"/s/b.sock","name":"b","workdir":"/w/b","state":"waiting","live":true,
       "asking":"permission: write","askId":"c2","askKind":"permission","idle":4},
      {"socket":"/s/c.sock","name":"c","workdir":"/w/c","state":"idle","live":true,"idle":9}
    ]`, "", `
await loadFleet();
console.log(JSON.stringify({title: document.title}));
`)
	if got["title"].(string) != "(2) magi · Companions" {
		t.Errorf("the tab title is %q, and two agents are waiting", got["title"])
	}
}

// An agent's page keeps polling the fleet, and that is not an accident of the dashboard's timer.
//
// The prompt an agent is blocked on reaches the browser through /fleet and nowhere else — it is
// not in the log and there is no event for it. Stop polling when a viewer opens an agent and the
// bar appears once, if the timing is lucky, and never again.
func TestAnAgentsPageKeepsPolling(t *testing.T) {
	got := runPage(t, `[]`, "?d=%2Fs%2Fa.sock", `
console.log(JSON.stringify({intervals: RENDERED.filter(r => r.interval).map(r => r.interval)}));
`)
	iv := got["intervals"].([]any)
	if len(iv) == 0 {
		t.Fatal("an agent's page arms no poll, so a prompt it blocks on would never appear")
	}
	if ms := iv[0].(float64); ms > 5000 {
		t.Errorf("the poll is every %gms; a person waiting to be asked notices that", ms)
	}
}

// Answering hits one URL with one target in it.
//
// The card builds its own ?d= because it can be answered from the dashboard, where the page has no
// target of its own; post() adds the page's target for everything else. Both at once produced
// /answer?d=X?d=X, which the server read as a path with a question mark in it and refused — and it
// was invisible on the dashboard, where post()'s half is empty, and broken on an agent's page,
// where it is not. Found by pressing the button in a browser, which is the only place the two
// halves meet.
func TestAnsweringSendsOneTargetNotTwo(t *testing.T) {
	for _, view := range []struct{ name, query string }{
		{"from the dashboard", ""},
		{"from the agent's own page", "?d=%2Fs%2Fa.sock"},
	} {
		got := runPage(t, `[
          {"socket":"/s/a.sock","name":"api","workdir":"/w/api","state":"waiting","live":true,
           "asking":"bash: rm -rf build","askId":"call_7","askKind":"permission","idle":3}
        ]`, view.query, rowsHelper+`
await loadFleet();
const box = document.getElementById('prompt').hidden ? rows()[0] : document.getElementById('prompt');
// fetch records the call synchronously, before its first await, so nothing needs waiting on.
box.find(clicky)[0].onclick({preventDefault(){}, stopPropagation(){}});
console.log(JSON.stringify({posts: RENDERED.filter(r => r.method === 'POST').map(r => r.fetched)}));
`)
		posts := got["posts"].([]any)
		if len(posts) != 1 {
			t.Fatalf("%s: pressing allow sent %d requests", view.name, len(posts))
		}
		u := posts[0].(string)
		if n := strings.Count(u, "?"); n != 1 {
			t.Errorf("%s: the answer went to %q — %d question marks, want one", view.name, u, n)
		}
		if !strings.HasPrefix(u, "/answer?d=") || strings.Count(u, "d=") != 1 {
			t.Errorf("%s: the answer went to %q", view.name, u)
		}
	}
}

const fiveStates = `[
  {"socket":"/s/a.sock","name":"api","workdir":"/w/api","state":"waiting","live":true,
   "asking":"bash: rm -rf build","askId":"c1","askKind":"permission","task":"clean up","steps":7,"idle":9,
   "host":"mini","addr":"10.0.0.12"},
  {"socket":"/s/b.sock","name":"docs","workdir":"/w/docs","state":"working","live":true,
   "task":"rewrite the page","steps":12,"idle":2,"host":"mini","addr":"10.0.0.12"},
  {"socket":"/s/c.sock","name":"scratch","workdir":"/w/scratch","state":"idle","live":true,
   "task":"done","idle":400,"host":"mini","addr":"10.0.0.12"},
  {"socket":"/s/d.sock","name":"old","workdir":"/w/old","state":"abandoned","live":false,
   "task":"gave up","steps":23,"idle":9000,"host":"mini","addr":"10.0.0.12"},
  {"socket":"/s/e.sock","name":"rel","workdir":"/w/rel","state":"stopped","live":false,
   "task":"tagged","idle":9000,"host":"mini","addr":"10.0.0.12"}
]`

// The console answers "does anything need me" before you read a single row.
//
// Counting rows to find that out is the work the summary removes, and it is the first thing a
// Kubernetes console shows for the same reason. The states fold into four buckets because a
// supervisor's next action is the same for the two dead ones.
func TestTheSummaryCountsEachStateAndFilters(t *testing.T) {
	got := runPage(t, fiveStates, "", rowsHelper+`
await loadFleet();
// The way to the board sits in this row too, and it is not a tile: it is a link out, not a filter.
const tiles = byId.summary.children.filter(t => t.className !== 'toboard')
  .map(t => ({k: t.text, pressed: !!t.selected, off: !!t.disabled}));
console.log(JSON.stringify({tiles, rows: rows().length}));
`)
	var got4 []string
	for _, tl := range got["tiles"].([]any) {
		got4 = append(got4, tl.(map[string]any)["k"].(string))
	}
	want := "1 Waiting|1 Working|1 Idle|2 Gone"
	if strings.Join(got4, "|") != want {
		t.Errorf("the summary reads %v, want %s", got4, want)
	}
	if n := got["rows"].(float64); n != 5 {
		t.Errorf("%v rows for five companions", n)
	}
}

// Trouble first. A list you have to read to find the problem is a list that hides it, so the order
// is waiting, working, idle, gone — and most recently active within each.
func TestTroubleSortsToTheTop(t *testing.T) {
	got := runPage(t, fiveStates, "", rowsHelper+`
await loadFleet();
// The state is a class among others (card · <state> · state), so it is picked out by name
// rather than by trimming a prefix off the whole list.
const states = ['waiting','working','idle','abandoned','stopped'];
console.log(JSON.stringify({order: rows().map(r =>
  (r.className || '').split(' ').find(c => states.includes(c)) || '?')}));
`)
	var order []string
	for _, o := range got["order"].([]any) {
		order = append(order, o.(string))
	}
	if strings.Join(order, ",") != "waiting,working,idle,abandoned,stopped" {
		t.Errorf("the rows came out %v", order)
	}
}

// Stopping must not require entering first. The row you want to halt is the one you are looking
// at, and making somebody open it to reach the button is how a runaway turn gets another thirty
// seconds. Only live ones that are doing something get the control.
func TestStoppingWorksFromTheList(t *testing.T) {
	got := runPage(t, fiveStates, "", rowsHelper+`
await loadFleet();
const stops = rows().map(r => r.find(clicky).filter(b => b.className === 'stop').length);
rows()[0].find(clicky).filter(b => b.className === 'stop')[0].onclick({preventDefault(){}, stopPropagation(){}});
// Pressing it asks first: halting a turn costs whatever the agent was in the middle of, and a row
// in a list is an easy thing to mis-hit.
const asked = byId.stopDialog.open === true;
const beforeConfirm = RENDERED.filter(r => r.method === 'POST').length;
byId.stopGo.onclick();
console.log(JSON.stringify({stops, asked, beforeConfirm,
  posts: RENDERED.filter(r => r.method === 'POST').map(r => r.fetched)}));
`)
	var stops []float64
	for _, s := range got["stops"].([]any) {
		stops = append(stops, s.(float64))
	}
	// waiting, working: stoppable. idle: nothing to stop. the two dead ones: nobody to tell.
	if len(stops) != 5 || stops[0] != 1 || stops[1] != 1 || stops[2] != 0 || stops[3] != 0 || stops[4] != 0 {
		t.Errorf("stop controls per row: %v — want one on the two that are running", stops)
	}
	if got["asked"] != true {
		t.Error("stopping a turn happened without asking")
	}
	if n, _ := got["beforeConfirm"].(float64); n != 0 {
		t.Errorf("%v requests went out before the question was answered", n)
	}
	posts := got["posts"].([]any)
	if len(posts) != 1 || !strings.HasPrefix(posts[0].(string), "/interrupt?d=") {
		t.Fatalf("pressing stop sent %v", posts)
	}
	if strings.Count(posts[0].(string), "?") != 1 {
		t.Errorf("the interrupt went to %q", posts[0])
	}
}

// Where the machine is. Everything under one config directory is one machine, so on a laptop this
// reads as noise — until three tabs are forwarded from three hosts over ssh, which is the
// arrangement the whole split exists for, and then it is the only thing telling them apart.
func TestEachRowSaysWhereItRuns(t *testing.T) {
	got := runPage(t, fiveStates, "", rowsHelper+`
await loadFleet();
console.log(JSON.stringify({first: rows()[0].text}));
`)
	first := got["first"].(string)
	for _, want := range []string{"mini", "10.0.0.12"} {
		if !strings.Contains(first, want) {
			t.Errorf("the row does not say where it runs (%q): %q", want, first)
		}
	}
}

// The breadcrumb is both the answer to "where am I" and the way back — one element doing both, so
// they cannot disagree.
func TestTheBreadcrumbSaysWhereYouAreAndLeadsBack(t *testing.T) {
	fleet := runPage(t, fiveStates, "", `
console.log(JSON.stringify({back: back.text, sep: crumbSep.hidden, here: crumbHere.text, href: back.attrs.href}));
`)
	if fleet["sep"] != true || fleet["here"].(string) != "" {
		t.Errorf("on the fleet the crumb shows a second level: %+v", fleet)
	}
	agent := runPage(t, fiveStates, "?d=%2Fs%2Fa.sock", `
console.log(JSON.stringify({back: back.text, sep: crumbSep.hidden, here: crumbHere.text, href: back.attrs.href}));
`)
	if agent["sep"] != false {
		t.Error("on an agent's page the crumb has no separator")
	}
	if agent["here"].(string) == "" {
		t.Error("on an agent's page the crumb does not name it")
	}
	// The crumb names the SECTION you are in, not always the fleet: standing in connections under a
	// crumb reading "fleet" answers a question nobody asked and offers a way back to somewhere you
	// have not been.
	for _, tc := range []struct{ query, want, href string }{
		{"", "Companions", "/"},
		{"?v=skills", "Shared", "/?v=skills"},
		// The old address still lands: what a companion can reach joined what it has learned, and
		// a link somebody kept must not stop working because two lists became one screen.
		{"?v=mcp", "Shared", "/?v=skills"},
	} {
		// An empty fleet: the crumb is drawn by render() from the query alone, and handing the
		// other views a list of agents makes them throw on data shaped for a different screen.
		at := runPage(t, `[]`, tc.query, `
console.log(JSON.stringify({back: back.text, href: back.attrs.href, sep: crumbSep.hidden}));
`)
		if at["back"] != tc.want || at["href"] != tc.href {
			t.Errorf("in %q the crumb is %q → %q, want %q → %q", tc.query, at["back"], at["href"], tc.want, tc.href)
		}
		if at["sep"] != true {
			t.Errorf("in %q the crumb shows a second level with nothing in it", tc.query)
		}
	}
}

// Every destination is named, once, from the pack.
//
// This used to look for ">companions<" and the rest in the markup. The tabs carry no words there
// any more — paint() fills them from the language pack, which is what stops them being English on
// a Korean page — so the check moved to what it was really protecting: each place the router can
// reach has a name, and the name comes from the pack rather than from the page.
func TestTheTabsAreNamedAsPlaces(t *testing.T) {
	for _, view := range []string{"fleet", "skills", "board", "mcp"} {
		if !strings.Contains(indexHTML, "'"+view+"'") {
			t.Errorf("the router does not know %q", view)
		}
	}
	for _, key := range []string{"nav.companions", "nav.lessons", "nav.board", "nav.connections"} {
		if v := packEntry(t, key); strings.TrimSpace(v) == "" {
			t.Errorf("%s is empty in the pack; that destination has no name", key)
		}
	}
	// The strip itself, not the whole page: the same phrases are section headings in the CSS and
	// the javascript, where they are prose about the code rather than labels somebody reads.
	strip := indexHTML[strings.Index(indexHTML, "<md-tabs id="):]
	strip = strip[:strings.Index(strip, "</md-tabs>")]
	for _, gone := range []string{"what I had to say", "what they have learned", "what they can reach"} {
		if strings.Contains(strip, gone) {
			t.Errorf("a sentence-shaped tab label survived: %q", gone)
		}
	}
}

// A detail page says what it is showing. The transcript alone does not: it is the same rows of
// text whichever companion you opened.
func TestTheDetailHeaderNamesWhatYouAreLookingAt(t *testing.T) {
	got := runPage(t, fiveStates, "?d=%2Fs%2Fa.sock", `
await loadFleet();
const d = document.getElementById('detail');
console.log(JSON.stringify({hidden: d.hidden, text: d.text}));
`)
	if got["hidden"].(bool) {
		t.Fatal("the detail header is hidden on a companion's own page")
	}
	for _, want := range []string{"Waiting", "/w/api", "mini", "10.0.0.12", "7"} {
		if !strings.Contains(got["text"].(string), want) {
			t.Errorf("the header does not say %q: %q", want, got["text"])
		}
	}
}

// A row from another console links and acts THERE.
//
// The pair (peer, socket) is what identifies a companion once more than one machine is in the list:
// a socket path is only meaningful on the machine that owns it, so every link and every action a
// remote row produces has to carry the console's name beside it. Without that they resolve locally,
// where nothing by that path exists — and the failure would read as "no such companion" rather than
// "you dropped the machine".
func TestARemoteRowCarriesItsConsoleEverywhere(t *testing.T) {
	got := runPage(t, `[
      {"peer":"laptop","socket":"/there/a.sock","name":"fuzzer","workdir":"/w/fuzzer",
       "state":"working","live":true,"task":"finding the parser bug","steps":4,"idle":6,
       "host":"laptop","addr":"10.0.0.31"},
      {"socket":"/here/b.sock","name":"local","workdir":"/w/local","state":"working","live":true,
       "task":"on this machine","steps":1,"idle":1,"host":"mini","addr":"10.0.0.12"}
    ]`, "", rowsHelper+`
await loadFleet();
const remote = rows().find(r => r.text.includes('fuzzer'));
const local = rows().find(r => r.text.includes('local'));
// Each stop is asked and then confirmed, one at a time — the dialog is one element and the
// question it is holding is about one companion.
remote.find(clicky).filter(b => b.className === 'stop')[0].onclick({preventDefault(){}, stopPropagation(){}});
byId.stopGo.onclick();
local.find(clicky).filter(b => b.className === 'stop')[0].onclick({preventDefault(){}, stopPropagation(){}});
byId.stopGo.onclick();
console.log(JSON.stringify({
  remoteHref: remote.attrs.href, localHref: local.attrs.href,
  remoteText: remote.text,
  posts: RENDERED.filter(r => r.method === 'POST').map(r => r.fetched),
}));
`)
	if !strings.Contains(got["remoteHref"].(string), "p=laptop") {
		t.Errorf("a remote row links to %q, losing which console it is on", got["remoteHref"])
	}
	if strings.Contains(got["localHref"].(string), "p=") {
		t.Errorf("a local row carries a console name it does not have: %q", got["localHref"])
	}
	// The console it came from is what the eye needs before the hostname when three are federated.
	if !strings.Contains(got["remoteText"].(string), "laptop") {
		t.Errorf("the row does not say which console reported it: %q", got["remoteText"])
	}
	posts := got["posts"].([]any)
	if len(posts) != 2 {
		t.Fatalf("two stops sent %d requests", len(posts))
	}
	if !strings.Contains(posts[0].(string), "p=laptop") {
		t.Errorf("stopping a remote companion went to %q", posts[0])
	}
	if strings.Contains(posts[1].(string), "p=") {
		t.Errorf("stopping a local companion named a console: %q", posts[1])
	}
}

// Switching tabs goes through md-tabs, because that is the only place the indicator is animated.
//
// Tabs.activateTab measures the outgoing tab's indicator and slides the incoming one from there,
// and it runs only when the component changes its own selection — a page that writes active onto
// each tab, or that prevents the click's default, lands on the right tab with no motion between
// them. Neither is visible in the end state, so this asks how the selection got there. The
// animation itself needs a real browser; what is fixed here is the path into it.
func TestSwitchingTabsGoesThroughTheComponent(t *testing.T) {
	got := runPage(t, `[]`, "", `
globalThis.fetch = async () => ({ok: true, json: async () => []});
byId.tabSkills.onclick({preventDefault() { throw new Error('the click default was prevented'); }});
console.log(JSON.stringify({
  where: location.search,
  on: byId.tabs.activeTabIndex,
  // tabMcp is not in the markup and never was — the fake's hand-kept id list answered for it
  // anyway, so this filter has been reading a stub since it was written.
  byHand: ['tabFleet', 'tabSkills'].filter(id => byId[id].setDirectly),
}));
`)
	if got["where"] != "?v=skills" {
		t.Errorf("clicking the lessons tab went to %q", got["where"])
	}
	if got["on"].(float64) != 1 {
		t.Errorf("md-tabs has tab %v active, and experience is the second", got["on"])
	}
	if hand := got["byHand"].([]any); len(hand) > 0 {
		t.Errorf("the page set active on %v itself; md-tabs animates its indicator only when it is "+
			"the one selecting, so those tabs change with no motion", hand)
	}
}

// The page asks for the language its reader actually reads, from where the page is mounted.
//
// Two things went wrong here at once and neither made a sound. The url was built with a leading
// slash, so on a project site under /magi/ it reached for /i18n/… at the domain root and 404d; and
// the locale came from navigator.language alone, which is one tag out of an ordered list the
// browser publishes. A reader whose browser asks for Korean first got English either way.
func TestThePageAsksForTheLanguageTheReaderReads(t *testing.T) {
	t.Setenv("BASE", "/magi/")
	t.Setenv("LANG_TAGS", "ko-KR,en-US")
	got := runPage(t, `[]`, "", `
console.log(JSON.stringify({asked: RENDERED.filter(r => String(r.fetched).includes('i18n')).map(r => r.fetched)}));
`)
	asked := got["asked"].([]any)
	if len(asked) == 0 {
		t.Fatal("the page asked for no language pack at all")
	}
	if asked[0] != "/magi/i18n/language.ko.json" {
		t.Errorf("the page asked for %q; the browser asks for Korean first and the page is under /magi/", asked[0])
	}
}

// A language nothing is written in falls through to the next one the browser asked for.
func TestAnUnwrittenLanguageFallsToTheNextOneAsked(t *testing.T) {
	t.Setenv("LANG_TAGS", "fr-FR,ko-KR,en-US")
	got := runPage(t, `[]`, "", `
console.log(JSON.stringify({asked: RENDERED.filter(r => String(r.fetched).includes('i18n')).map(r => r.fetched)}));
`)
	asked := got["asked"].([]any)
	if len(asked) == 0 || asked[0] != "/i18n/language.ko.json" {
		t.Errorf("asked for %v; nothing is written in French and Korean is the next one requested", asked)
	}
}

// A decision arrives with the grounds to decide it, in the order the skill asked for them.
//
// The report crosses four hops to get here — tool, session, socket, fleet row — and the point of
// all of them is this block. Checked on both surfaces that draw a prompt: the fleet, where a
// supervisor sees it without opening anything, and the companion's own page.
func TestABlockedAgentShowsWhatItWantsDecidedOn(t *testing.T) {
	fleet := `[
      {"socket":"/s/a.sock","name":"api","workdir":"/w/api","state":"waiting","live":true,
       "asking":"which branch should this land on?","askId":"q1#1","askKind":"question",
       "report":[{"key":"tried","text":"built and tested both"},
                 {"key":"stakes","text":"main is hard to undo"},
                 {"key":"lean","text":"the branch, it is unreviewed"}],
       "task":"land the work","steps":3,"idle":5}]`

	onFleet := runPage(t, fleet, "", rowsHelper+`
await loadFleet();
const g = rows()[0].find('div').find(d => String(d.className).includes('grounds'));
console.log(JSON.stringify({
  // Each section is a box of its own now, so the keys are gathered from inside them. The order
  // across the sections is what this checks, and that is the order they were appended in.
  keys: g ? g.children.map(s => (s.children.find(c => c.className === 'gk') || {}).textContent || '') : [],
  text: g ? g.text : '',
}));
`)
	var keys []string
	for _, k := range onFleet["keys"].([]any) {
		keys = append(keys, k.(string))
	}
	if strings.Join(keys, "|") != "tried|stakes|lean" {
		t.Errorf("the fleet row shows %v; the skill's order is part of the report", keys)
	}
	if !strings.Contains(onFleet["text"].(string), "main is hard to undo") {
		t.Errorf("the grounds did not reach the row: %q", onFleet["text"])
	}

	// And on the companion's own page, where the prompt is a dock rather than a cell.
	own := runPage(t, fleet, "?d=%2Fs%2Fa.sock", `
await loadFleet();   // an agent's page polls the fleet for its own row; the prompt comes from it
console.log(JSON.stringify({text: byId.prompt.text, has: !!byId.prompt.find('div').find(d => String(d.className).includes('grounds'))}));
`)
	if own["has"] != true || !strings.Contains(own["text"].(string), "unreviewed") {
		t.Errorf("the agent's own page drew the question without its grounds: %+v", own)
	}
}

// Below the two-column width a companion is two screens, not one column of six cards.
//
// Stacked, the order was: the facts, then the conversation, then four cards of which three are
// history. Measured at 430px the transcript began 1073px down a 900px screen — off it — and the
// composer, fixed at the foot, was nowhere near the words it answers. Above the breakpoint both
// columns are visible and there is nothing to switch between, so the strip must not appear there.
func TestOnANarrowScreenACompanionIsTwoPanels(t *testing.T) {
	const one = `[{"socket":"/s/a.sock","name":"api","workdir":"/w/api","state":"working","live":true,
	   "task":"add the key","steps":3,"idle":4,"session":"s1"}]`
	probe := `
await loadFleet();
const vis = id => !byId[id].hidden;
const out = {tabs: !byId.ptabs.hidden, talk: {log: vis('log'), detail: vis('detail'), side: vis('side')}};
if (!byId.ptabs.hidden) {
  byId.ptabs.activeTabIndex = 1;
  byId.ptabs.dispatchEvent({type: 'change'});
  out.state = {log: vis('log'), detail: vis('detail'), side: vis('side')};
}
console.log(JSON.stringify(out));
`
	t.Run("narrow", func(t *testing.T) {
		t.Setenv("NARROW", "1")
		got := runPage(t, one, "?d=%2Fs%2Fa.sock", probe)
		if got["tabs"] != true {
			t.Fatal("no way to reach the other half: the strip is hidden on a stacked layout")
		}
		talk := got["talk"].(map[string]any)
		if talk["log"] != true || talk["detail"] != false || talk["side"] != false {
			t.Errorf("the conversation panel shows %v — it must be the conversation and nothing above it", talk)
		}
		state, ok := got["state"].(map[string]any)
		if !ok {
			t.Fatal("switching panels produced nothing")
		}
		if state["log"] != false || state["detail"] != true || state["side"] != true {
			t.Errorf("the status panel shows %v", state)
		}
	})
	t.Run("wide", func(t *testing.T) {
		got := runPage(t, one, "?d=%2Fs%2Fa.sock", probe)
		if got["tabs"] != false {
			t.Error("the strip is drawn where both columns already fit")
		}
		talk := got["talk"].(map[string]any)
		if talk["log"] != true || talk["detail"] != true || talk["side"] != true {
			t.Errorf("the two-column layout hid something: %v", talk)
		}
	})
}

// The permission prompt is asked for before anything is awaited.
//
// requestPermission needs transient user activation. An await hands the turn back to the event
// loop and the activation is spent by the time the call is reached, so it resolves 'default'
// having shown nobody a prompt — which from outside is a switch that does nothing. This is a
// property of ORDER, not of what gets called: the old code called requestPermission too, one
// await too late.
func TestThePermissionPromptComesBeforeAnyAwait(t *testing.T) {
	got := runPage(t, `[]`, "", `
// Cleared first: the switch paints itself on load, and that look at the registration is not part
// of what the click does.
globalThis.ORDER.length = 0;
byId.notifySwitch.dispatchEvent({type: 'change'});
// Drained with microtasks: this harness's setTimeout is a stub that never fires, deliberately —
// a test that waited on a real timer would wait for the page's three-second poll.
for (let i = 0; i < 80; i++) await Promise.resolve();
console.log(JSON.stringify({order: globalThis.ORDER, why: byId.notifyWhy.textContent}));
`)
	var order []string
	for _, v := range got["order"].([]any) {
		order = append(order, v.(string))
	}
	if len(order) == 0 {
		t.Fatal("the switch reached nothing at all")
	}
	if order[0] != "requestPermission" {
		t.Errorf("the page did %v first — the prompt must be asked for in the click's own turn, "+
			"before any await spends the user activation", order)
	}
	// And a refusal stops there rather than registering a worker nobody can be notified through.
	for _, step := range order {
		if step == "register" || step == "subscribe" {
			t.Errorf("permission was refused and the page went on to %s: %v", step, order)
		}
	}
}

// A day on the board is the reader's day, not UTC's.
//
// dayOf was the first ten characters of an RFC3339 string — the UTC day — compared against
// todayISO(), which is the local one. East of UTC in the evening the two disagree, so the board
// showed the wrong day's work and filtered out sessions running at that moment. Measured at 00:30
// in UTC+9, six cards became one, and the survivor was the only one whose UTC and local days
// happened to straddle midnight.
func TestABoardDayIsTheReadersDay(t *testing.T) {
	// A session that ran this morning, local time. In UTC+9 its RFC3339 string carries yesterday's
	// date, which is exactly the case that used to vanish.
	got := runPage(t, `[{"socket":"/s/a.sock","name":"api","workdir":"/w/api","state":"idle","live":true,"session":"s1","idle":9}]`,
		"?v=board", `
const local = new Date();
local.setHours(7, 0, 0, 0);
const ended = new Date(local.getTime() + 3600000);
globalThis.fetch = async (p) => {
  const u = String(p).split('?')[0];
  if (u === '/history') return {ok: true, json: async () => [
    {id: 's1', title: 'the morning run', started: local.toISOString(), ended: ended.toISOString(), ago: 60},
  ]};
  if (u === '/fleet') return {ok: true, json: async () => [
    {socket: '/s/a.sock', name: 'api', workdir: '/w/api', state: 'idle', live: true, session: 's1', idle: 9},
  ]};
  return {ok: true, json: async () => []};
};
await loadBoard();
console.log(JSON.stringify({
  cards: byId.board.find('div').filter(d => String(d.className).includes('wcard')).length,
  utcDay: local.toISOString().slice(0, 10),
  localDay: new Date(local.getTime() - local.getTimezoneOffset() * 60000).toISOString().slice(0, 10),
}));
`)
	if got["cards"].(float64) != 1 {
		t.Errorf("%v cards for one session that ran this morning — the board is comparing a local "+
			"day against a UTC one (utc %v, local %v)",
			got["cards"], got["utcDay"], got["localDay"])
	}
}

// Answering one moves to the next one still waiting.
//
// Two blocked companions among twenty is a list you hunt through twice: the count at the top takes
// you to the first, and after that you scroll a table looking for the other pause marker.
// Answering is a queue, not a browse, and the difference is whether the page advances.
func TestAnsweringAdvancesToTheNextOneWaiting(t *testing.T) {
	got := runPage(t, `[
      {"socket":"/s/a.sock","name":"api","workdir":"/w/a","state":"waiting","live":true,
       "asking":"rm -rf build","askId":"c1","askKind":"permission","idle":3},
      {"socket":"/s/b.sock","name":"ops","workdir":"/w/b","state":"waiting","live":true,
       "asking":"restart the box","askId":"c2","askKind":"permission","idle":8}]`, "", rowsHelper+`
await loadFleet();
// Answer the FIRST one.
globalThis.SCROLLED.length = 0;
await rows()[0].find('md-filled-tonal-button')[0].onclick({preventDefault(){}, stopPropagation(){}});
for (let i = 0; i < 30; i++) await Promise.resolve();
console.log(JSON.stringify({scrolled: globalThis.SCROLLED}));
`)
	var to []string
	for _, v := range got["scrolled"].([]any) {
		to = append(to, v.(string))
	}
	if len(to) == 0 {
		t.Fatal("answering left the reader where they were; the second blocked companion is " +
			"somewhere down a table they now have to hunt through")
	}
	// The one just answered is skipped: the fleet is polled and its row can still be drawn as
	// waiting for one more tick, so landing back on it would look like the answer did not take.
	if strings.Contains(to[0], "a.sock") {
		t.Errorf("it scrolled back to the row just answered: %v", to)
	}
	if !strings.Contains(to[0], "b.sock") {
		t.Errorf("it did not reach the other one still waiting: %v", to)
	}
}

// A question is answered in the composer, and the composer says so.
//
// Both boxes drawn, an agent's page had two text fields stacked: the upper one answering the
// question and the lower one addressed at an agent that is not listening, with nothing on the page
// saying which was which. One field in two roles instead — and a permission prompt, whose controls
// are buttons, keeps the composer for work.
func TestTheComposerBecomesTheAnswerFieldForAQuestion(t *testing.T) {
	probe := `
await loadFleet();
console.log(JSON.stringify({
  label: byId.t.attrs['label'], support: byId.cnote.textContent || '',
  send: byId.send.textContent,
  fieldsInPrompt: byId.prompt.find('md-outlined-text-field').length,
}));
`
	q := runPage(t, `[
      {"socket":"/s/a.sock","name":"api","workdir":"/w/api","state":"waiting","live":true,
       "asking":"which branch should this land on?","askId":"q1#1","askKind":"question","idle":5}]`,
		"?d=%2Fs%2Fa.sock", probe)
	if q["label"] != "Your answer" {
		t.Errorf("the composer still asks for work while the agent waits on a question: %q", q["label"])
	}
	if q["send"] != "Answer" {
		t.Errorf("the button still says %q, which sends the text somewhere it will not be read", q["send"])
	}
	if q["support"] == "" {
		t.Error("the field changed its job and said nothing about it")
	}
	if q["fieldsInPrompt"].(float64) != 0 {
		t.Errorf("%v text fields in the prompt as well as the composer", q["fieldsInPrompt"])
	}

	p := runPage(t, `[
      {"socket":"/s/a.sock","name":"api","workdir":"/w/api","state":"waiting","live":true,
       "asking":"rm -rf build","askId":"p1#1","askKind":"permission","idle":3}]`,
		"?d=%2Fs%2Fa.sock", probe)
	if p["label"] != "Ask magi" {
		t.Errorf("a permission prompt took the composer away: %q — deciding not to do the thing at "+
			"all is a legitimate reply, and it is typed here", p["label"])
	}
	if p["support"] != "" {
		t.Errorf("the composer carries an answering note while nothing is being answered: %q", p["support"])
	}
}

// The button that sends carries ONE mark, however many times its job changes.
//
// It is written as label() then withMark(): label keeps the mark so the word can be rewritten
// without losing it, and withMark used to put the new one in front of the old — so the first time
// the companion started asking something, the composer's button grew a second mark and kept it for
// the rest of the session. Reported from a live console.
//
// The symbols are registered by hand here. This harness runs the page against markup with no
// sprite, so every icon site takes the fallback and the drawn path had no test at all — which is
// how a button with two icons in it shipped.
func TestTheSendButtonKeepsOneMarkWhenItChangesItsJob(t *testing.T) {
	got := runPage(t, `[
      {"socket":"/s/a.sock","name":"api","workdir":"/w/api","state":"waiting","live":true,
       "asking":"which branch should this land on?","askId":"q1#1","askKind":"question","idle":5}]`,
		"?d=%2Fs%2Fa.sock", `
byId['i-ss-paper-plane'] = {}; byId['i-sl-reply'] = {};
const marks = () => byId.send.children.filter(k => k.attrs && k.attrs.slot === 'icon').length;
await loadFleet();
const asking = marks();
answerMode(null);          // the question was answered; the button goes back to sending
const sending = marks();
answerMode({askId: 'q1#1', askKind: 'question'});
console.log(JSON.stringify({asking: asking, sending: sending, again: marks(),
  word: byId.send.textContent}));
`)
	for _, c := range []struct {
		when string
		n    float64
	}{{"answering", got["asking"].(float64)}, {"back to sending", got["sending"].(float64)},
		{"answering again", got["again"].(float64)}} {
		if c.n != 1 {
			t.Errorf("the send button carries %v marks while %s — one control, one icon", c.n, c.when)
		}
	}
	if got["word"] == "" {
		t.Error("the button lost its word")
	}
}

// A prompt with no report is the prompt it always was, not an empty box.
//
// A companion on an older build, or one whose report was lost between its socket and this page,
// must not render as though somebody left the reasoning blank.
func TestAPromptWithNoReportDrawsNoEmptyBlock(t *testing.T) {
	got := runPage(t, `[
      {"socket":"/s/a.sock","name":"api","workdir":"/w/api","state":"waiting","live":true,
       "asking":"rm -rf build","askId":"p1#1","askKind":"permission","steps":1,"idle":3}]`, "", rowsHelper+`
await loadFleet();
console.log(JSON.stringify({grounds: rows()[0].find('div').filter(d => String(d.className).includes('grounds')).length,
  asking: rows()[0].text.includes('rm -rf build')}));
`)
	if got["grounds"].(float64) != 0 {
		t.Error("a prompt with no report still drew a grounds block")
	}
	if got["asking"] != true {
		t.Error("the prompt itself went missing")
	}
}

// The count of what needs a person rides the section that holds them, in both navigations.
//
// M3 puts a badge on a navigation item for exactly this, and it earns its place when the rail is
// collapsed: the words are gone and the shape is all there is, so a number on it is the only thing
// saying somebody is blocked. Hidden at zero — a badge that is always there is one the eye stops
// reading — and set from one place so the rail and the tabs cannot claim different numbers.
func TestTheWaitingCountRidesTheNavigation(t *testing.T) {
	two := runPage(t, `[
      {"socket":"/s/a.sock","name":"api","workdir":"/w/api","state":"waiting","live":true,
       "asking":"rm -rf build","askId":"p1#1","askKind":"permission","steps":1,"idle":3},
      {"socket":"/s/b.sock","name":"ops","workdir":"/w/ops","state":"waiting","live":true,
       "asking":"which region?","askId":"q1#1","askKind":"question","steps":2,"idle":9},
      {"socket":"/s/c.sock","name":"docs","workdir":"/w/docs","state":"working","live":true,
       "task":"writing","steps":4,"idle":1}
    ]`, "", `
await loadFleet();
console.log(JSON.stringify({rail: byId.railBadge.value, railHidden: byId.railBadge.hidden,
  tab: byId.tabBadge.value, tabHidden: byId.tabBadge.hidden}));
`)
	if two["rail"] != "2" || two["tab"] != "2" {
		t.Errorf("two agents are blocked and the badges read rail=%v tab=%v", two["rail"], two["tab"])
	}
	if two["railHidden"] == true || two["tabHidden"] == true {
		t.Error("the badges are hidden while two agents wait")
	}

	none := runPage(t, `[
      {"socket":"/s/c.sock","name":"docs","workdir":"/w/docs","state":"working","live":true,
       "task":"writing","steps":4,"idle":1}
    ]`, "", `
await loadFleet();
console.log(JSON.stringify({railHidden: byId.railBadge.hidden, tabHidden: byId.tabBadge.hidden,
  value: byId.railBadge.value}));
`)
	if none["railHidden"] != true || none["tabHidden"] != true {
		t.Errorf("nothing is waiting and the badges are still drawn: %+v", none)
	}
	if none["value"] != "" {
		t.Errorf("the badge kept the value %q after the last agent unblocked", none["value"])
	}
}

// Repainting the tab's word must not take its badge with it.
//
// A tab holds a label and a badge, and textContent replaces everything a node holds — so writing
// the word straight onto the tab deleted the count. Found by the fake DOM the day it was taught
// what textContent actually does.
func TestRepaintingATabKeepsItsBadge(t *testing.T) {
	got := runPage(t, `[
      {"socket":"/s/a.sock","name":"api","workdir":"/w/api","state":"waiting","live":true,
       "asking":"rm -rf build","askId":"p1#1","askKind":"permission","steps":1,"idle":3}
    ]`, "", `
await loadFleet();
labels$.next({'nav.companions': '컴패니언'});
console.log(JSON.stringify({word: byId.tabFleet.text, badge: byId.tabBadge.value,
  // Anywhere inside the tab. The badge sits in a wrapper beside the label now, because a tab
  // STACKS what is slotted into it and two siblings put the count on a line of its own.
  still: byId.tabFleet.find(n => n === byId.tabBadge).length > 0}));
`)
	if got["still"] != true || got["badge"] != "1" {
		t.Errorf("the language change dropped the badge: %+v", got)
	}
	if !strings.Contains(got["word"].(string), "컴패니언") {
		t.Errorf("the tab did not repaint: %q", got["word"])
	}
}

// Teams group the list, and the groups keep the rule the flat list followed.
//
// Trouble first is why the rows are ordered the way they are, and grouping by team would scatter
// the blocked agents across the page and take it back. So the groups are ordered by the worst
// state in each: a team holding somebody blocked comes before a team that is merely busy. The
// unnamed group goes last however its members are doing — "these belong to no team" is a remark
// about the roster, not something anybody scans for.
func TestTeamsGroupTheFleetWithoutHidingTrouble(t *testing.T) {
	got := runPage(t, `[
      {"socket":"/s/1","name":"ops","workdir":"/w/1","state":"working","live":true,
       "team":"alpha","task":"deploying","steps":2,"idle":3},
      {"socket":"/s/2","name":"loose","workdir":"/w/2","state":"waiting","live":true,
       "asking":"rm -rf build","askId":"p#1","askKind":"permission","steps":1,"idle":1},
      {"socket":"/s/3","name":"design","workdir":"/w/3","state":"idle","live":true,
       "team":"zulu","hub":true,"task":"specced it","steps":5,"idle":40},
      {"socket":"/s/4","name":"buttons","workdir":"/w/4","state":"waiting","live":true,
       "team":"zulu","asking":"which token?","askId":"q#1","askKind":"question","steps":3,"idle":9}
    ]`, "", rowsHelper+`
await loadFleet();
const heads = byId.fleet.children.filter(c => c.className === 'teamhead');
console.log(JSON.stringify({
  order: heads.map(h => h.find('div').find(d => d.className === 'tname').textContent),
  hub: heads.map(h => { const x = h.find('div').find(d => d.className === 'thub'); return x ? x.textContent : ''; }),
  badges: heads.map(h => { const b = h.find('md-badge')[0]; return b ? b.value : ''; }),
  rows: rows().length,
}));
`)
	var order []string
	for _, o := range got["order"].([]any) {
		order = append(order, o.(string))
	}
	// frontend holds a blocked agent; infra is only working; the unnamed one is last regardless.
	// "zulu" sorts after "alpha"; it comes first anyway, because it is the one holding somebody up.
	if strings.Join(order, "|") != "zulu|alpha|No team" {
		t.Errorf("the groups are ordered %v — a team holding somebody blocked comes first, and the "+
			"unnamed group last", order)
	}
	hub := got["hub"].([]any)
	if !strings.Contains(hub[0].(string), "design") {
		t.Errorf("the zulu heading does not say who answers for it: %q", hub[0])
	}
	badges := got["badges"].([]any)
	if badges[0] != "1" || badges[1] != "" {
		t.Errorf("the headings count %v blocked; zulu has one and alpha none", badges)
	}
	if got["rows"].(float64) != 4 {
		t.Errorf("%v rows drawn for four agents", got["rows"])
	}
}

// The edges of grouping, each of which the data can actually produce.
func TestTeamGroupingEdges(t *testing.T) {
	run := func(fleet, query string) map[string]any {
		return runPage(t, fleet, query, rowsHelper+`
await loadFleet();
const heads = byId.fleet.children.filter(c => c.className === 'teamhead');
const txt = d => { const x = d; return x ? x.textContent : ''; };
console.log(JSON.stringify({
  heads: heads.map(h => txt(h.find('div').find(c => c.className === 'tname'))),
  hubs:  heads.map(h => txt(h.find('div').find(c => c.className === 'thub'))),
  counts: heads.map(h => txt(h.find('div').find(c => c.className === 'tn'))),
  rows: rows().length,
}));
`)
	}
	str := func(v any) []string {
		var out []string
		for _, x := range v.([]any) {
			out = append(out, x.(string))
		}
		return out
	}

	// One team and nobody outside it. Still headed: the heading is what names the team and says who
	// answers for it, and that is worth a line even when there is only one.
	one := run(`[
      {"socket":"/s/1","name":"a","workdir":"/w/1","state":"working","live":true,"team":"infra","hub":true,"task":"x","steps":1,"idle":2},
      {"socket":"/s/2","name":"b","workdir":"/w/2","state":"idle","live":true,"team":"infra","task":"y","steps":0,"idle":9}
    ]`, "")
	if h := str(one["heads"]); len(h) != 1 || h[0] != "infra" {
		t.Errorf("a single team drew %v", h)
	}
	if c := str(one["counts"]); c[0] != "2" {
		t.Errorf("the heading counts %v of two members", c)
	}

	// Two companions claiming to speak for one team. A misconfiguration, and the heading says so
	// rather than picking one and looking settled.
	two := run(`[
      {"socket":"/s/1","name":"a","workdir":"/w/1","state":"idle","live":true,"team":"infra","hub":true,"task":"x","steps":0,"idle":9},
      {"socket":"/s/2","name":"b","workdir":"/w/2","state":"idle","live":true,"team":"infra","hub":true,"task":"y","steps":0,"idle":9}
    ]`, "")
	hub := str(two["hubs"])[0]
	if !strings.Contains(hub, "a") || !strings.Contains(hub, "b") {
		t.Errorf("two hubs and the heading names %q — picking one hides a misconfiguration", hub)
	}

	// Filtering leaves no empty headings behind: the groups are built from the filtered rows.
	filtered := run(`[
      {"socket":"/s/1","name":"a","workdir":"/w/1","state":"waiting","live":true,"team":"zulu","asking":"q","askId":"q#1","askKind":"question","steps":1,"idle":1},
      {"socket":"/s/2","name":"b","workdir":"/w/2","state":"idle","live":true,"team":"alpha","task":"y","steps":0,"idle":90}
    ]`, "")
	if h := str(filtered["heads"]); len(h) != 2 {
		t.Fatalf("unfiltered, the two teams draw %v", h)
	}
}

// No teams, no headings. A single-workspace machine declares none, and a heading saying so would
// be furniture over every list.
func TestAFleetWithNoTeamsDrawsNoHeadings(t *testing.T) {
	got := runPage(t, `[
      {"socket":"/s/1","name":"api","workdir":"/w/1","state":"working","live":true,"task":"x","steps":1,"idle":2},
      {"socket":"/s/2","name":"ops","workdir":"/w/2","state":"idle","live":true,"task":"y","steps":0,"idle":90}
    ]`, "", rowsHelper+`
await loadFleet();
console.log(JSON.stringify({heads: byId.fleet.children.filter(c => c.className === 'teamhead').length,
  rows: rows().length}));
`)
	if got["heads"].(float64) != 0 {
		t.Errorf("%v headings drawn for a fleet with no teams", got["heads"])
	}
	if got["rows"].(float64) != 2 {
		t.Errorf("%v rows for two agents", got["rows"])
	}
}

// A pack that arrives after a list is drawn still reaches it.
//
// The lists are built by functions and a function reads its words at draw time, so a pack landing
// afterwards left them in the old language until somebody navigated away and back. Seen on the
// lessons page as an English "forget" beside a Korean "정말?" on the same control — half a button
// in each language.
//
// The transcript and the detail panel are deliberately NOT repainted: a pack can land mid
// interaction, and re-rendering there is what once wiped a panel somebody was reading.
func TestALatePackReachesTheListsAlreadyDrawn(t *testing.T) {
	got := runPage(t, `[]`, "?v=skills", `
globalThis.fetch = async (p) => ({ok: true, json: async () => String(p).startsWith('/skills') ? [
  {"name":"skill-x","description":"a rule","tier":"global","observed":1,"lastSeen":"2026-08-06"}
] : []});
await loadSkills();
const before = byId.skills.text;
byId.detail.replaceChildren(cell('', 'a thing somebody was reading'));
labels$.next({'action.forget': '잊기', 'nav.shared': '공유'});
// Drain the microtasks the repaint's own fetch runs on. NOT a second loadSkills() by hand — that
// would be the test doing the thing it is checking for, and it would pass with the fix removed.
for (let i = 0; i < 80; i++) await Promise.resolve();
console.log(JSON.stringify({before, after: byId.skills.text, kept: byId.detail.text, title: document.title}));
`)
	if strings.Contains(got["before"].(string), "잊기") {
		t.Fatal("the fixture was already translated; this test cannot see the change")
	}
	if !strings.Contains(got["after"].(string), "잊기") {
		t.Errorf("the list kept the old language after the pack landed: %q", got["after"])
	}
	if got["kept"] != "a thing somebody was reading" {
		t.Errorf("repainting the lists wiped the detail panel: %q", got["kept"])
	}
	if !strings.Contains(got["title"].(string), "공유") {
		t.Errorf("the tab title stayed in the old language: %q — it is the one word a reader sees "+
			"without looking at the page", got["title"])
	}
}

// What a person had to step in and say, on the companion it is about.
//
// This replaces a promotion pipeline: group the words, count the repeats, offer to make the
// repeated ones permanent. The premise did not hold — what somebody says mid-turn is nearly always
// about that task, the grouping only matched identical wording, and it needed somebody to visit a
// screen and curate. What survives is the part that never needed the words to match: how often this
// companion had to be corrected, and what was refused.
func TestACompanionsPageSaysWhatYouHadToStepInAndSay(t *testing.T) {
	got := runPage(t, `[
      {"socket":"/s/a.sock","name":"api","workdir":"/w/api","state":"working","live":true,
       "task":"x","steps":2,"idle":3}
    ]`, "?d=%2Fs%2Fa.sock", `
globalThis.fetch = async (p) => ({ok: true, json: async () => String(p).startsWith('/interventions') ? [
  {"companion":"api","socket":"/s/a.sock","kind":"steer","text":"use the tokens","at":"2026-08-08T04:00:00Z","afterSec":8},
  {"companion":"api","socket":"/s/a.sock","kind":"denied","text":"call_31","at":"2026-08-07T04:00:00Z","afterSec":95},
  {"companion":"other","socket":"/s/z.sock","kind":"steer","text":"not this one","at":"2026-08-08T04:00:00Z","afterSec":5}
] : String(p).startsWith('/fleet') ? [
  {"socket":"/s/a.sock","name":"api","workdir":"/w/api","state":"working","live":true,"task":"x","steps":2,"idle":3}
] : []});
await loadFleet();
await new Promise(r => { let n = 0; const tick = () => (++n > 20 ? r() : Promise.resolve().then(tick)); tick(); });
console.log(JSON.stringify({hidden: byId.intervened.hidden, text: byId.intervened.text,
  rows: byId.intervened.find('div').filter(d => String(d.className).startsWith('iv2')).length}));
`)
	if got["hidden"] == true {
		t.Fatal("the panel is hidden while this companion has been corrected twice")
	}
	txt := got["text"].(string)
	if !strings.Contains(txt, "use the tokens") {
		t.Errorf("what was said is not on the page: %q", txt)
	}
	if strings.Contains(txt, "not this one") {
		t.Errorf("another companion's correction is on this one's page: %q", txt)
	}
	if got["rows"].(float64) != 2 {
		t.Errorf("%v rows for two interventions on this companion", got["rows"])
	}
	// A count that does not depend on the words matching, which is the whole reason this replaced
	// the grouping.
	if !strings.Contains(txt, "1") {
		t.Errorf("the heading does not count them: %q", txt)
	}
}

// The rail widens, and the theme has a control of its own.
//
// There is no drawer on a phone any more: the tabs navigate, the theme toggle sits in the masthead,
// and the rest is a section at the foot of the page. So what is left to check is that the rail's
// button opens and closes one state, and that the toggle and the select in that section are one
// setting rather than two — they write and read the same stored preference, and a page where the
// quick control and the full one disagree is worse than having only one of them.
func TestTheRailOpensAndTheThemeToggleIsTheSameSetting(t *testing.T) {
	got := runPage(t, `[]`, "", `
globalThis.fetch = async () => ({ok: true, json: async () => []});
byId.railMenu.onclick();
const opened = {nav: document.body.getAttribute('nav'), said: byId.railMenu.getAttribute('aria-expanded')};
byId.railMenu.onclick();
const closed = {nav: document.body.getAttribute('nav'), said: byId.railMenu.getAttribute('aria-expanded')};

// Nothing chosen yet: the machine answers, and the fake reports a light one.
const before = {stored: localStorage.getItem('theme'), attr: document.documentElement.getAttribute('color-theme')};
byId.themeToggle.onclick();
const after = {stored: localStorage.getItem('theme'), attr: document.documentElement.getAttribute('color-theme')};
byId.themeToggle.onclick();
const again = {stored: localStorage.getItem('theme'), attr: document.documentElement.getAttribute('color-theme')};
byId.themeToggle.onclick();
// …and back to following the machine, which is the stop the old two-state toggle could not reach.
const round = {stored: localStorage.getItem('theme'), attr: document.documentElement.getAttribute('color-theme')};
console.log(JSON.stringify({opened, closed, before, after, again, round,
  // A real look, not a constant: the dialog must not hold a second control for the theme.
  themeSelect: byId.prefsForm.find(n => String(n.tag).endsWith('-select')).length > 1}));
`)
	op, cl := got["opened"].(map[string]any), got["closed"].(map[string]any)
	if op["nav"] != "open" || op["said"] != "true" {
		t.Errorf("the rail did not open: %+v", op)
	}
	if cl["nav"] != nil || cl["said"] != "false" {
		t.Errorf("the rail did not close again: %+v", cl)
	}

	before := got["before"].(map[string]any)
	if before["stored"] != nil || before["attr"] != nil {
		t.Errorf("something was stored before anybody chose: %+v", before)
	}
	// A ring of three, starting where the console does: follow the machine, then light, then dark.
	// The two-stop version could not return to "system" — choosing once meant never getting back
	// without clearing storage — and every stop moves the sky, so no press looks dead.
	after := got["after"].(map[string]any)
	if after["stored"] != "light" || after["attr"] != "light" {
		t.Errorf("pressing the toggle while following the machine gave %+v, want light", after)
	}
	// There is no second control for the theme. It used to be a select in the preferences dialog as
	// well, which is one preference with two ways to be wrong about it; the toggle is the only one
	// now, so what has to hold is that the toggle and the STORE agree — checked above.
	if got["themeSelect"] != false {
		t.Error("the preferences dialog carries a theme control again; the masthead toggle is the one")
	}
	if again := got["again"].(map[string]any); again["stored"] != "dark" || again["attr"] != "dark" {
		t.Errorf("pressing it again gave %+v, want dark", again)
	}
	// The stop that matters: the ring comes back to it, and "system" is the ABSENCE of the
	// attribute — the stylesheet's own media query is what answers then.
	if r := got["round"].(map[string]any); r["stored"] != "system" || r["attr"] != nil {
		t.Errorf("the third press gave %+v, want the machine followed again", r)
	}
}

// The rail and the tabs are two ways to say the same thing, so they must say the same thing.
func TestTheRailAgreesWithTheTabs(t *testing.T) {
	got := runPage(t, `[]`, "?v=mcp", `
globalThis.fetch = async () => ({ok: true, json: async () => []});
console.log(JSON.stringify({
  on: byId.tabs.activeTabIndex,
  lit: ['railFleet','railSkills'].filter(id => byId[id].hasAttribute('selected')),
}));
`)
	// ?v=mcp resolves to the shared destination, which is the second tab.
	if got["on"].(float64) != 1 {
		t.Errorf("the tabs have %v active and shared is the second", got["on"])
	}
	lit := got["lit"].([]any)
	if len(lit) != 1 || lit[0] != "railSkills" {
		t.Errorf("the rail lights %v while the tabs are on the shared destination", lit)
	}
}

// A row has exactly as many cells as the header has columns, whatever the agent is doing.
//
// The row is a CSS grid with a fixed seven-column template, so a cell that appears only sometimes
// does not add a column — it shifts every cell after it one to the right and wraps the last one
// onto a line of its own. That is what the plan indicator did: an agent with a todo list had its
// task rendered in the step count's 72px and its buttons on a second row, and an agent without one
// looked fine. Counted here because the fake DOM has no CSS and cannot see the damage, only the
// arithmetic that causes it.
func TestARowHasAsManyCellsAsTheHeaderHasColumns(t *testing.T) {
	got := runPage(t, `[
      {"socket":"/s/a.sock","name":"api","workdir":"/w/api","state":"working","live":true,
       "task":"with a plan","steps":7,"idle":12,"planDone":1,"planTotal":4},
      {"socket":"/s/b.sock","name":"docs","workdir":"/w/docs","state":"working","live":true,
       "task":"without one","steps":2,"idle":9},
      {"socket":"/s/c.sock","name":"ops","workdir":"/w/ops","state":"waiting","live":true,
       "asking":"rm -rf build","askId":"p1#1","askKind":"permission","steps":1,"idle":3,
       "planDone":2,"planTotal":3}
    ]`, "", rowsHelper+`
await loadFleet();
const head = byId.fleet.children.find(c => c.className === 'thead').children.length;
// Only the cells that are columns. A block marked span takes the whole row on purpose.
const cols = r => r.children.filter(c => !String(c.className).split(' ').includes('span')).length;
console.log(JSON.stringify({head, rows: rows().map(cols)}));
`)
	head := int(got["head"].(float64))
	if head == 0 {
		t.Fatal("the table head drew no columns")
	}
	for i, n := range got["rows"].([]any) {
		if int(n.(float64)) != head {
			t.Errorf("row %d has %v cells and the header has %d columns — every cell after the "+
				"extra one lands in the wrong column and the last wraps", i, n, head)
		}
	}
}

// The tabs say which resource is on screen and switch without a reload — and a companion's own page
// is neither of them, being one level in.
func TestTheTabsSayWhichResourceIsShowing(t *testing.T) {
	fleet := runPage(t, `[]`, "", `
// Which tab is current is the component's own active property, not a class of ours.
console.log(JSON.stringify({tabs: byId.tabs.hidden, fleetOn: !!byId.tabFleet.active,
  ivOn: !!byId.tabSkills.active, fleetHidden: byId.fleet.hidden, ivsHidden: byId.skills.hidden}));
`)
	if fleet["tabs"].(bool) || fleet["fleetOn"] != true || fleet["ivOn"] != false {
		t.Errorf("on the fleet the tabs read %+v", fleet)
	}
	if fleet["ivsHidden"] != true || fleet["fleetHidden"] != false {
		t.Errorf("the fleet view shows the wrong list: %+v", fleet)
	}
	ivs := runPage(t, `[]`, "?v=skills", `
globalThis.fetch = async () => ({ok: true, json: async () => []});
console.log(JSON.stringify({fleetOn: !!byId.tabFleet.active, ivOn: !!byId.tabSkills.active,
  fleetHidden: byId.fleet.hidden, ivsHidden: byId.skills.hidden, summaryHidden: byId.summary.hidden}));
`)
	if ivs["ivOn"] != true || ivs["fleetOn"] != false {
		t.Errorf("on the experience page the tabs read %+v", ivs)
	}
	if ivs["ivsHidden"] != false || ivs["fleetHidden"] != true || ivs["summaryHidden"] != true {
		t.Errorf("the interventions view shows the wrong list: %+v", ivs)
	}
	agent := runPage(t, `[]`, "?d=%2Fs%2Fa.sock", `
console.log(JSON.stringify({tabs: byId.tabs.hidden}));
`)
	if agent["tabs"] != true {
		t.Error("a companion's own page shows the resource tabs; it is one level in, not a resource list")
	}
}

// What the companions have learned, and which of it crosses between them.
//
// The tier is the whole of context hygiene and the page has to make it impossible to miss: "every
// companion" and "only this one" are different sentences, not a colour difference, because the
// decision a supervisor makes on this page is exactly which of the two a rule should be.
const tr_forget = "forget"

func TestTheSkillsPageSaysWhatEachRuleReaches(t *testing.T) {
	got := runPage(t, `[]`, "?v=skills", `
globalThis.fetch = async (p, init) => {
  if (init && init.method === 'POST') { RENDERED.push({fetched: p, method: 'POST', body: init.body.toString()}); return {ok: true, status: 204, text: async () => ''}; }
  return {ok: true, json: async () => p.startsWith('/skills') ? [
    {"name":"skill-commit-style","description":"commit messages carry the issue number","tier":"global","observed":3,"firstSeen":"2026-07-14","lastSeen":"2026-08-07"},
    {"name":"skill-auth","description":"the auth service uses X","tier":"project","companion":"api","socket":"/s/a.sock","observed":1,"firstSeen":"2026-08-06","lastSeen":"2026-08-06","groups":["crew"]}
  ] : []};
};
await loadSkills();
// The tools row is the screen's own controls — find, reach, write down — not a rule.
const rows = byId.skills.children.filter(r => r.className !== 'sectionhead' && r.className !== 'skfind' && r.className !== 'skwrite');
const drop = rows[1].find('md-text-button')[0];
// Once asks. A destructive control that acts on the first press has no confirmation at all, and
// the error colour it used to rely on lives on :hover, which a touch screen does not have.
drop.onclick();
const afterOne = {posts: RENDERED.filter(r => r.method === 'POST').length,
                  label: drop.textContent, armed: (drop.className || '').includes('armed')};
drop.onclick();
console.log(JSON.stringify({
  rows: rows.map(r => ({cls: r.className, text: r.text})),
  // The message, which is #note's now: #state carries the count and the poll rebuilds it, so a
  // notice written there lived for whatever was left of the interval.
  state: byId.note.text,
  afterOne,
  posts: RENDERED.filter(r => r.method === 'POST'),
}));
`)
	rows := got["rows"].([]any)
	if len(rows) != 2 {
		t.Fatalf("two rules drew %d rows", len(rows))
	}
	first := rows[0].(map[string]any)
	if !strings.Contains(first["cls"].(string), "global") ||
		!strings.Contains(first["text"].(string), "Every companion") {
		t.Errorf("the crossing rule does not say it crosses: %+v", first)
	}
	second := rows[1].(map[string]any)["text"].(string)
	if !strings.Contains(second, "Only api") {
		t.Errorf("the project rule does not say whose it is: %q", second)
	}
	// The two facts a decision is made on, and neither is visible anywhere else after the day it
	// was written.
	// Both ends when they differ: a rule learned three weeks ago and still turning up is settled,
	// and one learned and last seen the same day never recurred. The second row is the second case
	// and says so with one date, not a range from itself to itself.
	if !strings.Contains(first["text"].(string), "3×") ||
		!strings.Contains(first["text"].(string), "2026-07-14 → 2026-08-07") {
		t.Errorf("the row carries no history: %q", first["text"])
	}
	if !strings.Contains(second, "last 2026-08-06") || strings.Contains(second, "→") {
		t.Errorf("a rule seen once drew a range from itself to itself: %q", second)
	}
	if !strings.Contains(got["state"].(string), "1 crossing") {
		t.Errorf("the header does not count what crosses: %q", got["state"])
	}
	// One press asks and sends nothing; the second acts.
	one := got["afterOne"].(map[string]any)
	if one["posts"].(float64) != 0 {
		t.Errorf("the first press already sent %v requests — nothing asked", one["posts"])
	}
	if one["armed"] != true || one["label"] == tr_forget {
		t.Errorf("the first press did not turn the control into a question: %+v", one)
	}

	// Forgetting a project rule names the companion it belongs to; a global one has nobody to name.
	posts := got["posts"].([]any)
	if len(posts) != 1 {
		t.Fatalf("confirming forget sent %d requests", len(posts))
	}
	u := posts[0].(map[string]any)["fetched"].(string)
	if !strings.Contains(u, "/forget?d=") {
		t.Errorf("forgetting a project rule went to %q", u)
	}
	if b := posts[0].(map[string]any)["body"].(string); !strings.Contains(b, "tier=project") {
		t.Errorf("the request does not say which tier: %q", b)
	}
}

// The two facts about a companion's head that exist nowhere else on the page: how full it is, and
// how much of its history has already been summarised away.
func TestTheDetailSaysHowFullTheContextIsAndWhatWasFolded(t *testing.T) {
	got := runPage(t, `[]`, "", `
globalThis.fetch = async (p) => ({ok: true, json: async () => p.startsWith('/context') ? {
  model: 'qwen3', window: 100000, used: 82000, estimated: false, messages: 41,
  cached: 61500, cacheReported: true,
  compactions: 2, shed: 31000, lastBefore: 40000, lastAfter: 9000, lastAt: '2026-08-07T04:31:07Z',
  topics: ['internal/parse.go', 'discussion'],
} : p.startsWith('/plan') ? [
  {content: 'read the current empty states', status: 'completed'},
  {content: 'write the spec', status: 'in_progress'},
  {content: 'get it reviewed', status: 'pending'},
] : p.startsWith('/handoffs') ? [
  {from: 'api', to: 'design', socket: '/s/d.sock', request: 'spec the empty state', state: 'idle',
   answer: 'here are the tokens'},
  {from: 'api', to: 'ops', socket: '/s/o.sock', request: 'check the alert', state: 'working'},
] : []});
await drawDetail({socket: '/s/a.sock', name: 'api', state: 'working', workdir: '/w/api',
                  steps: 3, idle: 4, session: 's1', host: 'mini', addr: '10.0.0.4', pid: 4127});
const box = byId.detail;
const bars = box.find('div').filter(d => (d.className || '').split(' ').includes('bar') || (d.className || '') === 'bar tight');
console.log(JSON.stringify({text: box.text, fields: box.children.length,
  fill: bars.length ? bars[0].children[0].style.width : '',
  handed: byId.handoffs.text,
  // Text and marks: a tick that became a drawing is invisible to a reader of text alone.
  // Text and marks: a tick that became a drawing is invisible to a reader of text alone.
  plan: byId.plan.text + ' ' + (byId.plan.find('use') || []).map(u => u.attrs.href || '').join(' ')}));
`)
	text := got["text"].(string)
	for _, want := range []string{"82,000 / 100,000 tokens", "Measured", "41 messages", "2 folds",
		"31,000 tokens shed", "40,000→9,000", "internal/parse.go",
		// Which model, because the window above is that model's and /route can change it
		// mid-session with nothing else on the page saying so.
		"qwen3",
		// Host, address and pid together: what you need to go and look at the process by hand,
		// which is the reason a supervisor opens this panel at all when something is wedged.
		"mini · 10.0.0.4 · pid 4127",
		// What the backend served from its own cache, when it says at all.
		"75% of it cached",
		// And when the last fold happened: "twice" says nothing about whether it was this
		// minute or yesterday.
		"04:31Z"} {
		if !strings.Contains(text, want) {
			t.Errorf("the detail does not say %q:\n%s", want, text)
		}
	}
	// The bar is drawn only because a window is known. An empty track for an unknown window reads
	// as "nearly empty", which is the opposite of what it would mean.
	if got["fill"] != "82%" {
		t.Errorf("the fill is at %v, not the measured share", got["fill"])
	}

	// The plan it is working through, as it last recorded it.
	plan := got["plan"].(string)
	for _, want := range []string{"read the current empty states", "write the spec", "get it reviewed"} {
		if !strings.Contains(plan, want) {
			t.Errorf("the plan is missing %q:\n%s", want, plan)
		}
	}
	// Done and in-flight are told apart, by whatever this build has to tell them apart with: the
	// characters where there are no icons, and the drawn marks where there are. Asserting on the
	// character alone made a build WITH icons look like a plan that had stopped distinguishing.
	if !strings.Contains(plan, "✓") && !strings.Contains(plan, "i-sl-check") {
		t.Errorf("nothing marks a finished step:\n%s", plan)
	}
	if !strings.Contains(plan, "▸") && !strings.Contains(plan, "i-sl-chevron-right") {
		t.Errorf("nothing marks the step in hand:\n%s", plan)
	}

	// What it handed to the others, and what came back. A companion answers in its own transcript,
	// so without this a person walks five pages to learn whether the work is done.
	handed := got["handed"].(string)
	if !strings.Contains(handed, "design") || !strings.Contains(handed, "spec the empty state") {
		t.Errorf("the handed-out work is missing:\n%s", handed)
	}
	if !strings.Contains(handed, "here are the tokens") {
		t.Errorf("the answer to a finished handoff is missing:\n%s", handed)
	}
	// One still running reports that it is running rather than a line taken mid-thought.
	if !strings.Contains(handed, "still working") {
		t.Errorf("a running handoff does not say so:\n%s", handed)
	}

	got = runPage(t, `[]`, "", `
globalThis.fetch = async (p) => ({ok: true, json: async () => p.startsWith('/context') ? {
  used: 5000, estimated: true, messages: 3, compactions: 0,
} : []});
await drawDetail({socket: '/s/a.sock', name: 'api', state: 'idle', workdir: '/w/api', session: 's1'});
console.log(JSON.stringify({text: byId.detail.text,
  bars: byId.detail.find('div').filter(d => (d.className || '').split(' ').includes('bar') || (d.className || '') === 'bar tight').length}));
`)
	text = got["text"].(string)
	if !strings.Contains(text, "~5,000 tokens") || !strings.Contains(text, "Estimated") {
		t.Errorf("an estimate is not marked as one:\n%s", text)
	}
	if strings.Contains(text, "/ ") {
		t.Errorf("an unknown window was printed as a denominator:\n%s", text)
	}
	if got["bars"].(float64) != 0 {
		t.Error("a fill bar was drawn against a window nobody knows")
	}
	// Nothing has been folded, so nothing claims it has.
	if strings.Contains(text, "fold") {
		t.Errorf("a session with no compactions reports one:\n%s", text)
	}
}

// A console that cannot be reached is not a machine with no agents on it.
//
// The two look identical to anyone who does not read the status line — an empty page — and they
// mean opposite things: one is "everything is idle", the other is "you are looking at a stale
// screen and do not know it". So a failed load says so and leaves the last good rows alone.
func TestLosingTheConsoleDoesNotDrawAnEmptyFleet(t *testing.T) {
	got := runPage(t, `[]`, "", `
let answer = [{socket: '/s/a.sock', name: 'api', state: 'working', task: 'building', workdir: '/w/api', session: 's1', steps: 2, idle: 1, live: true}];
globalThis.fetch = async () => {
  if (answer === null) throw new Error('connection refused');
  return {ok: true, json: async () => answer};
};
await loadFleet();
const drawn = byId.fleet.text;
answer = null;
await loadFleet();
console.log(JSON.stringify({
  before: drawn, after: byId.fleet.text,
  state: byId.note.text, cls: byId.state.className,
}));
`)
	if !strings.Contains(got["before"].(string), "api") {
		t.Fatalf("the first load drew nothing: %q", got["before"])
	}
	if got["after"] != got["before"] {
		t.Errorf("losing the console redrew the table as %q", got["after"])
	}
	if got["state"] != "Can't reach magi-web" || got["cls"] != "lost" {
		t.Errorf("the page does not say it lost the console: %+v", got)
	}
}

// The detail panel is redrawn by every fleet poll, three seconds apart, for as long as the tab is
// open. What it shows about the context costs a replay of the whole log — the exact cost the fleet
// cache exists to avoid paying per row per poll — so it is asked for again only when the transcript
// has actually moved, and re-rendered from what was held when it has not.
func TestTheContextIsNotReplayedOnEveryPoll(t *testing.T) {
	got := runPage(t, `[]`, "", `
let asks = 0;
globalThis.fetch = async (p) => {
  if (!p.startsWith('/context')) return {ok: true, json: async () => []};
  return {ok: true, json: async () => ({model: 'qwen3', window: 100, used: 40, messages: ++asks})};
};
const agent = (steps, state) => ({socket: '/s/a.sock', name: 'api', state: state || 'idle',
                               workdir: '/w', session: 's1', steps});
await drawDetail(agent(7));
const first = byId.detail.text;
await drawDetail(agent(7));   // the same poll answer, twice more
await drawDetail(agent(7));
const idle = byId.detail.text;
await drawDetail(agent(8));   // the transcript moved
const moved = byId.detail.text;
await drawDetail(agent(8, 'working'));  // the turn ended: same steps, different state
console.log(JSON.stringify({asks, first, idle, moved, restated: byId.detail.text}));
`)
	if got["asks"].(float64) != 3 {
		t.Errorf("three idle polls, one new step and one state change asked %v times, want 3", got["asks"])
	}
	// A turn ending writes the provider's real prompt count without adding a tool call, so a panel
	// keyed on steps alone would keep showing its estimate for as long as the companion sat idle.
	if !strings.Contains(got["restated"].(string), "3 messages") {
		t.Errorf("the end of a turn did not refresh the reading:\n%s", got["restated"])
	}
	// Held, not skipped: the panel is rebuilt from scratch each time, so a cached answer still has
	// to be drawn. The first version of this returned early and left the panel a field short.
	if got["idle"] != got["first"] {
		t.Errorf("a redraw with nothing new lost the context:\n%s", got["idle"])
	}
	if !strings.Contains(got["first"].(string), "1 messages") {
		t.Fatalf("the first draw did not render the answer:\n%s", got["first"])
	}
	if !strings.Contains(got["moved"].(string), "2 messages") {
		t.Errorf("a companion that took a step was not asked again:\n%s", got["moved"])
	}
}

// Two polls overlap the moment one of them is slow. The late answer must not land on a panel that
// has been rebuilt since — it would append a second copy of every field, or put older numbers
// under newer ones.
func TestASlowContextAnswerDoesNotLandOnALaterPanel(t *testing.T) {
	got := runPage(t, `[]`, "", `
let release;
const held = new Promise(r => { release = r; });
let n = 0;
globalThis.fetch = async (p) => {
  if (!p.startsWith('/context')) return {ok: true, json: async () => []};
  const mine = ++n;
  if (mine === 1) await held;                       // the first ask is the slow one
  return {ok: true, json: async () => ({model: 'qwen3', used: 10 * mine, messages: mine})};
};
const agent = steps => ({socket: '/s/a.sock', name: 'api', state: 'working', workdir: '/w', session: 's1', steps});
const slow = drawDetail(agent(1));
const quick = drawDetail(agent(2));
await quick;
release();
await slow;
const text = byId.detail.text;
console.log(JSON.stringify({text, contexts: (text.match(/messages/g) || []).length}));
`)
	if got["contexts"].(float64) != 1 {
		t.Errorf("the panel carries %v context blocks:\n%s", got["contexts"], got["text"])
	}
	if !strings.Contains(got["text"].(string), "2 messages") {
		t.Errorf("the stale answer won:\n%s", got["text"])
	}
}

// What each companion can reach, and adding one. The list is read to answer one question — which
// of them can see that thing — so the transport line is complete rather than tidied, and the env
// is named without its values.
func TestTheMCPTabListsServersAndAddsOne(t *testing.T) {
	got := runPage(t, `[]`, "?v=mcp", `
globalThis.fetch = async (p, init) => {
  if (init && init.method === 'POST') { RENDERED.push({to: p, body: init.body.toString()}); return {ok: true, status: 200, text: async () => 'ok'}; }
  if (p.startsWith('/fleet')) return {ok: true, json: async () => [
    {socket:'/s/a.sock', name:'api', state:'idle', workdir:'/w/a', session:'a', idle:1},
  ]};
  return {ok: true, json: async () => [
    {name:'shared', tier:'global', url:'http://localhost:3000/mcp', file:'/cfg/config.toml'},
    {name:'figma', tier:'project', companion:'api', socket:'/s/a.sock', command:'npx',
     args:['-y','figma-mcp'], envNames:['FIGMA_TOKEN'], file:'/w/a/.magi/config.toml'},
  ]};
};
fleetSeen = [{socket:'/s/a.sock', name:'api'}];
await loadMCP();
// The form lives in the dialog now, and the page shows one button that opens it.
// At the head of the section, not under everything it holds — a dozen servers used to stand
// between somebody and the way to add one.
const head = byId.mcp.children[0];
const opener = head.find('md-filled-tonal-button')[0];
if (!opener) throw new Error('the section head carries no button to open the dialog: ' + head.text);
opener.onclick();
const form = byId.mcpForm;
const text = byId.mcp.text + ' ' + form.text;
[...form.querySelectorAll('md-outlined-select')].find(e => e.name === 'kind').value = 'stdio';
for (const i of form.querySelectorAll('md-outlined-text-field')) {
  if (i.name === 'name') i.value = 'tickets';
  if (i.name === 'command') i.value = 'uvx';
}
// By NAME, not by position: the form asks which kind of server first now, so the first select is
// that one and the reach picker is the second.
[...form.querySelectorAll('md-outlined-select')].find(e => e.name === 'who').value = '/s/a.sock';
byId.mcpDialog.returnValue = 'add';
await form.onsubmit({preventDefault(){}});
const drops = byId.mcp.find('md-text-button').filter(b => (b.className || '').split(' ').includes('drop'));
drops[1].onclick();   // asks
drops[1].onclick();   // acts
console.log(JSON.stringify({text, state: byId.state.text, posts: RENDERED.filter(r => r.to)}));
`)
	text := got["text"].(string)
	for _, want := range []string{"Every companion here", "Only api", "npx -y figma-mcp",
		"needs FIGMA_TOKEN", "/w/a/.magi/config.toml"} {
		if !strings.Contains(text, want) {
			t.Errorf("the list does not say %q:\n%s", want, text)
		}
	}
	// It says when a change takes effect, which is the thing somebody would otherwise discover by
	// wondering why their new server never appeared.
	if !strings.Contains(text, "next starts") {
		t.Errorf("the form does not say when it takes effect:\n%s", text)
	}
	posts := got["posts"].([]any)
	if len(posts) != 2 {
		t.Fatalf("adding and removing sent %d requests: %v", len(posts), posts)
	}
	add := posts[0].(map[string]any)
	if !strings.Contains(add["to"].(string), "d=%2Fs%2Fa.sock") {
		t.Errorf("the new server went to %q", add["to"])
	}
	if b := add["body"].(string); !strings.Contains(b, "name=tickets") || !strings.Contains(b, "command=uvx") {
		t.Errorf("the definition did not travel: %q", b)
	}
	// Removing a global server names the tier, since there is no socket to route by.
	del := posts[1].(map[string]any)
	if b := del["body"].(string); !strings.Contains(b, "name=figma") || !strings.Contains(b, "delete=1") {
		t.Errorf("the removal came out as %q", b)
	}
}

// A backend that says nothing about a cache is not a backend whose cache never hits.
//
// This is the ordinary case: measured on 2026-08-07, the default local backend sends
// prompt/completion/total and no details block. Drawing 0% would report a working cache as broken,
// so the panel says which situation it is in and shows no figure.
func TestABackendThatReportsNoCacheIsNotACacheThatMissed(t *testing.T) {
	got := runPage(t, `[]`, "", `
globalThis.fetch = async (p) => ({ok: true, json: async () => p.startsWith('/context') ? {
  model: 'qwen3', window: 100000, used: 82000, estimated: false, messages: 41,
} : []});
await drawDetail({socket: '/s/a.sock', name: 'api', state: 'idle', workdir: '/w', session: 's1'});
console.log(JSON.stringify({text: byId.detail.text}));
`)
	text := got["text"].(string)
	if strings.Contains(text, "% of it cached") {
		t.Errorf("a figure was drawn for a backend that reported none:\n%s", text)
	}
	if !strings.Contains(text, "doesn't report it") {
		t.Errorf("the panel does not say why there is no figure:\n%s", text)
	}
}

// The lever sits beside the reading it answers, and it says what it costs. "Compact" is a word that
// sounds free; the live window loses the original wording, and only the shards keep it.
func TestTheContextPanelOffersTheFold(t *testing.T) {
	got := runPage(t, `[]`, "", `
let asks = 0;
globalThis.fetch = async (p, init) => {
  if (init && init.method === 'POST') { RENDERED.push({to: p}); return {ok: true, status: 204, text: async () => ''}; }
  if (!p.startsWith('/context')) return {ok: true, json: async () => []};
  return {ok: true, json: async () => ({model: 'qwen3', window: 100000, used: 82000, messages: ++asks})};
};
const agent = {socket: '/s/a.sock', name: 'api', state: 'idle', workdir: '/w', session: 's1'};
await drawDetail(agent);
const fold = byId.detail.find('md-text-button').filter(b => (b.className || '').split(' ').includes('fold'))[0];
// data-tip, not title: the page draws its own tooltip so that it also appears on keyboard focus,
// which a native title never does.
const title = fold.attrs['data-tip'];
await fold.onclick();
// The same companion, unchanged: without invalidation the panel would hold pre-fold numbers, and
// nothing about an idle companion would ever change the key that would refresh them.
await drawDetail(agent);
console.log(JSON.stringify({title, disabled: fold.disabled, asks,
  posts: RENDERED.filter(r => r.to)}));
`)
	if !strings.Contains(got["title"].(string), "recalled") {
		t.Errorf("the button does not say what a fold costs: %q", got["title"])
	}
	posts := got["posts"].([]any)
	if len(posts) != 1 || !strings.HasPrefix(posts[0].(map[string]any)["to"].(string), "/compact") {
		t.Fatalf("pressing it sent %v", posts)
	}
	if !strings.Contains(posts[0].(map[string]any)["to"].(string), "d=%2Fs%2Fa.sock") {
		t.Errorf("the fold was not aimed at the companion: %v", posts[0])
	}
	// Pressed once. A second press before the first lands would fold twice.
	if got["disabled"] != true {
		t.Error("the button stayed pressable while its request was in flight")
	}
	// And the reading is asked again. The fold changed exactly the thing the panel is showing, and
	// nothing about an idle companion would otherwise change the key that refreshes it — the panel
	// would sit on pre-fold numbers indefinitely.
	if got["asks"].(float64) != 2 {
		t.Errorf("the context was asked %v times across a fold, want 2", got["asks"])
	}
}

// Clicking a tab lands on that tab, and the crumb goes where it says it goes.
//
// Nothing covered this until the tabs were renamed and somebody asked whether they still worked —
// at which point the harness answered "no" for four screens that were fine, because its pushState
// was a no-op and the page navigates by pushing a url and re-reading it. A fake that cannot express
// navigation cannot check navigation.
func TestClickingATabLandsOnIt(t *testing.T) {
	got := runPage(t, `[]`, "", `
globalThis.fetch = async () => ({ok: true, json: async () => []});
const seen = [];
for (const [name, el] of [['shared', tabSkills], ['companions', tabFleet]]) {
  el.onclick({preventDefault(){}});
  seen.push({name, search: location.search, crumb: back.text, href: back.attrs.href,
             ivs: byId.skills.hidden, skills: byId.skills.hidden, mcp: byId.mcp.hidden,
             fleet: byId.fleet.hidden});
}
// And the crumb itself, which names a section and must lead to the one it names.
tabSkills.onclick({preventDefault(){}});
back.onclick({preventDefault(){}});
console.log(JSON.stringify({seen, afterCrumb: location.search}));
`)
	seen := got["seen"].([]any)
	want := []struct{ search, crumb, href string }{
		{"?v=skills", "Shared", "/?v=skills"},
		{"", "Companions", "/"},
	}
	for i, w := range want {
		row := seen[i].(map[string]any)
		if row["search"] != w.search {
			t.Errorf("%v left the url at %q, want %q", row["name"], row["search"], w.search)
		}
		if row["crumb"] != w.crumb || row["href"] != w.href {
			t.Errorf("%v: the crumb reads %q → %q, want %q → %q",
				row["name"], row["crumb"], row["href"], w.crumb, w.href)
		}
	}
	// Each panel is shown only on its own tab — and the corrections panel now belongs to the
	// experience page, which is the whole of that merge.
	// The shared destination shows BOTH halves; the fleet shows neither.
	if seen[0].(map[string]any)["skills"] != false || seen[0].(map[string]any)["mcp"] != false ||
		seen[1].(map[string]any)["fleet"] != false {
		t.Errorf("a tab did not reveal its own panel: %v", seen)
	}
	// The crumb led where it said: from lessons, back to lessons' own url is wrong — it names the
	// section you are IN, so following it must stay there rather than jumping to the fleet.
	if got["afterCrumb"] != "?v=skills" {
		t.Errorf("the crumb read \"lessons\" and led to %q", got["afterCrumb"])
	}
}

// The labels come from the pack, and a locale arriving later repaints them without redrawing the
// view — a pack can land mid-interaction, and re-rendering there wipes what somebody was reading.
func TestTheLabelsComeFromTheLanguagePack(t *testing.T) {
	got := runPage(t, `[]`, "", `
// The first paint uses the seed the server inlines — no dotted keys, no flash.
const first = {tabs: [tabFleet.text, tabSkills.text],
               ask: byId.t.attrs.label};
// A pack arriving afterwards repaints what is already on screen.
labels$.next({'nav.companions': '컴패니언', 'nav.shared': '공유', 'nav.lessons': '경험',
              'nav.board': '보드', 'nav.connections': 'MCP',
              'label.ask': 'magi에게 요청'});
const after = [tabFleet.text, tabSkills.text];
const askAfter = byId.t.attrs.label;
// Something drawn by hand before a pack lands must survive it.
byId.detail.replaceChildren(cell('f', 'a thing somebody was reading'));
labels$.next({'nav.companions': 'x'});
console.log(JSON.stringify({first, after, askAfter, kept: byId.detail.text}));
`)
	first := got["first"].(map[string]any)
	tabs := first["tabs"].([]any)
	if tabs[0] != "Companions" || tabs[1] != "Shared" {
		t.Errorf("the first paint did not use the seeded pack: %v", tabs)
	}
	if first["ask"] != "Ask magi" {
		t.Errorf("the composer's label came from nowhere: %q", first["ask"])
	}
	after := got["after"].([]any)
	if after[0] != "컴패니언" || after[1] != "공유" {
		t.Errorf("a pack arriving later did not reach the tabs: %v", after)
	}
	if got["askAfter"] != "magi에게 요청" {
		t.Errorf("a pack arriving later did not reach the label: %q", got["askAfter"])
	}
	// Found the hard way: the pack used to trigger a full render, so a panel drawn while the fetch
	// was in flight lost its contents the moment the language answered.
	if got["kept"] != "a thing somebody was reading" {
		t.Errorf("a late language pack wiped the screen: %q", got["kept"])
	}
}

// Every key the page asks for exists in both packs, and neither pack carries a key nobody asks for.
func TestTheLanguagePacksMatchWhatThePageUses(t *testing.T) {
	used := map[string]bool{}
	for _, m := range regexp.MustCompile(`tr\('([a-z_.]+)'`).FindAllStringSubmatch(indexHTML, -1) {
		used[m[1]] = true
	}
	for _, m := range regexp.MustCompile(`'([a-z_]+\.[a-z_]+)': *'nav`).FindAllStringSubmatch(indexHTML, -1) {
		used[m[1]] = true
	}
	if len(used) < 5 {
		t.Fatalf("only %d keys found in the page — the scan has lost its subject", len(used))
	}
	for _, locale := range []string{"en", "ko"} {
		b, err := assetFS.ReadFile("i18n/language." + locale + ".json")
		if err != nil {
			t.Fatal(err)
		}
		var pack map[string]string
		if err := json.Unmarshal(b, &pack); err != nil {
			t.Fatalf("%s: %v", locale, err)
		}
		for key := range used {
			if _, ok := pack[key]; !ok {
				t.Errorf("the page asks for %q and language.%s.json does not have it — that label "+
					"renders as its own key", key, locale)
			}
		}
	}
}

// The router builds its urls from where the page is mounted.
//
// The binary serves at the root, so every url looked like /?v=skills. The same page published as a
// project site lives under /<repo>/, where that escapes to the domain root — clicks still worked
// because they are intercepted, but the address pushed was wrong and a reload landed nowhere.
func TestTheRouterKnowsWhereThePageIsMounted(t *testing.T) {
	for _, tc := range []struct{ base, wantTab, wantHome string }{
		{"/", "/?v=skills", "/"},
		{"/magi/", "/magi/?v=skills", "/magi/"},
		{"/deep/er/", "/deep/er/?v=skills", "/deep/er/"},
	} {
		got := runPageAt(t, tc.base, `
globalThis.fetch = async () => ({ok: true, json: async () => []});
tabSkills.onclick({preventDefault(){}});
const pushed = location.search;
const tabHref = tabSkills.attrs.href;
go(null);
console.log(JSON.stringify({tabHref, home: back.attrs.href, pushed}));
`)
		if got["tabHref"] != tc.wantTab {
			t.Errorf("mounted at %s the tab links to %q, want %q", tc.base, got["tabHref"], tc.wantTab)
		}
		if got["home"] != tc.wantHome {
			t.Errorf("mounted at %s the crumb links to %q, want %q", tc.base, got["home"], tc.wantHome)
		}
		// The query survives the base: it is what the page reads to know which view it is on.
		if got["pushed"] != "?v=skills" {
			t.Errorf("mounted at %s the pushed query is %q", tc.base, got["pushed"])
		}
	}
}

// The transcript renders markdown, and renders raw HTML in it as text.
//
// The terminal has rendered markdown since it existed and this page showed the source: a table
// arrived as a wall of pipes and a fenced block as its delimiters run together with its contents.
//
// The second half of this is the security property the whole approach rests on. Markdown permits
// raw HTML; the lexer reports it as a token rather than refusing it; and a transcript is arbitrary
// output from a model and from tools. Because the renderer builds nodes instead of a string, that
// token is drawn as characters — so the assertion here is not "it was escaped" (which would mean a
// string was built and then cleaned) but "no such element exists".
func TestTheTranscriptRendersMarkdownAndNeverBuildsHTMLFromIt(t *testing.T) {
	got := runPage(t, `[]`, "?d=%2Fs%2Fa.sock", `
draw([{who:'assistant',text:"| head | two |\n|---|---|\n| a | b |\n\n`+"```"+`go\nfunc main() {}\n`+"```"+`\n\nsee <img src=x onerror=alert(1)> and **bold**"}]);
// Walk the tree the renderer built.
const walk = (n, out) => { out.push(n); for (const k of n.children || []) if (k && k.children) walk(k, out); return out; };
const nodes = walk(byId.log, []);
const tags = nodes.map(n => n.tag);
const textOf = n => (n.textContent || '') + (n.children || []).map(textOf).join('');
console.log(JSON.stringify({
  tags: tags,
  tableText: nodes.filter(n => n.tag === 'table').map(textOf).join(''),
  codeText: nodes.filter(n => n.tag === 'pre').map(textOf).join(''),
  strongText: nodes.filter(n => n.tag === 'strong').map(textOf).join(''),
  allText: textOf(byId.log),
}));`)

	tags, _ := got["tags"].([]any)
	has := func(tag string) bool {
		for _, v := range tags {
			if s, _ := v.(string); s == tag {
				return true
			}
		}
		return false
	}
	for _, want := range []string{"table", "th", "td", "pre", "code", "strong"} {
		if !has(want) {
			t.Errorf("markdown produced no <%s>; the transcript is still showing source. tags=%v", want, tags)
		}
	}
	if s, _ := got["tableText"].(string); !strings.Contains(s, "head") || !strings.Contains(s, "b") {
		t.Errorf("the table has no cells: %q", s)
	}
	if s, _ := got["codeText"].(string); !strings.Contains(s, "func main()") {
		t.Errorf("the fenced block lost its contents: %q", s)
	}

	// The whole point: an img tag in the source is characters, not an element.
	if has("img") {
		t.Error("raw HTML in a transcript became an element — a tool result can now run script in the console")
	}
	if s, _ := got["allText"].(string); !strings.Contains(s, "<img src=x onerror=alert(1)>") {
		t.Errorf("the raw HTML was neither drawn as text nor kept: %q", s)
	}
}

// Reasoning and tool rows arrive folded, with a summary that says what is inside.
//
// Not hidden — folded. A thousand-line tool result used to sit open between two sentences, so
// reading a conversation meant scrolling past the machinery of it. A failed one starts open: it is
// the row somebody came to read.
func TestReasoningAndToolRowsFold(t *testing.T) {
	got := runPage(t, `[]`, "?d=%2Fs%2Fa.sock", `
draw([{who:'assistant',text:'plain'},{who:'thinking',text:'considering the options'},
      {who:'tool',text:'bash',tool:'bash',args:'go build ./...'},
      {who:'failed',text:'exit 1'}]);
const textOf = n => (n.textContent || '') + (n.children || []).map(textOf).join('');
const rows = byId.log.children.map(r => {
  const f = (r.children || []).find(c => c.tag === 'details');
  return { cls: r.className, folded: !!f, open: f ? !!f.open : false,
           summary: f ? textOf((f.children || [])[0] || {}) : '' };
});
console.log(JSON.stringify({rows: rows}));`)

	rows, _ := got["rows"].([]any)
	if len(rows) != 4 {
		t.Fatalf("drew %d rows, want 4", len(rows))
	}
	at := func(i int) map[string]any { m, _ := rows[i].(map[string]any); return m }

	// What was said stays where it can be read.
	if at(0)["folded"] == true {
		t.Error("an assistant reply was folded away; that is the thing the page is for")
	}
	// The machinery folds.
	for _, i := range []int{1, 2, 3} {
		if at(i)["folded"] != true {
			t.Errorf("row %d (%v) is not folded", i, at(i)["cls"])
		}
	}
	// And says enough to decide whether to open it.
	if s, _ := at(1)["summary"].(string); !strings.Contains(s, "considering the options") {
		t.Errorf("the reasoning summary says %q, which does not say what is inside", s)
	}
	if s, _ := at(1)["summary"].(string); !strings.Contains(s, "Reasoning") {
		t.Errorf("the reasoning row is not labelled: %q", s)
	}
	if s, _ := at(2)["summary"].(string); !strings.Contains(s, "bash") || !strings.Contains(s, "go build") {
		t.Errorf("a tool call's summary is %q — it should name the tool and what it was asked", s)
	}
	// A failure is the row somebody came to read, so it is not behind a press.
	if at(3)["open"] != true {
		t.Error("a failed tool call arrived folded shut")
	}
	if at(1)["open"] == true || at(2)["open"] == true {
		t.Error("reasoning or a successful tool call arrived open; they are the noise this folds")
	}
}

// A diff is coloured by what each line does, and a list with dashes in it is not.
//
// The terminal has coloured these since it had a transcript; here they arrived as a wall in which
// the one thing a diff is for — which lines went and which arrived — was what you had to work out
// by reading the first character of every row.
//
// The second half matters as much. Deciding "diff" from a leading plus or minus would paint every
// bulleted list half green and half red, which is worse than not colouring it: it would be saying
// something untrue about what changed. A hunk header is required.
func TestADiffIsColouredAndAListIsNot(t *testing.T) {
	got := runPage(t, `[]`, "?d=%2Fs%2Fa.sock", `
draw([
  {who:'result',text:"diff --git a/x b/x\n--- a/x\n+++ b/x\n@@ -1,2 +1,2 @@\n-gone\n+arrived\n stays"},
  {who:'result',text:"- one\n- two\n+ not a diff"},
]);
const walk = (n, out) => { out.push(n); for (const k of n.children || []) if (k && k.children) walk(k, out); return out; };
const nodes = walk(byId.log, []);
const cls = nodes.map(n => n.className).filter(Boolean);
const textOf = n => (n.textContent || '') + (n.children || []).map(textOf).join('');
console.log(JSON.stringify({
  classes: cls,
  added: nodes.filter(n => n.className === 'dadd').map(textOf),
  deleted: nodes.filter(n => n.className === 'ddel').map(textOf),
}));`)

	classes, _ := got["classes"].([]any)
	count := func(want string) int {
		n := 0
		for _, c := range classes {
			if s, _ := c.(string); s == want {
				n++
			}
		}
		return n
	}
	if count("dadd") != 1 || count("ddel") != 1 {
		t.Errorf("the diff has %d added and %d deleted lines, want 1 each: %v", count("dadd"), count("ddel"), classes)
	}
	if count("dhunk") != 1 {
		t.Errorf("the hunk header is not marked: %v", classes)
	}
	// Three file lines: the "diff --git", the ---, and the +++. None of them is an add or a delete.
	if count("dfile") != 3 {
		t.Errorf("the file headers are %d, want 3 — one of them was taken for a change: %v", count("dfile"), classes)
	}
	added, _ := got["added"].([]any)
	if len(added) != 1 || !strings.Contains(added[0].(string), "arrived") {
		t.Errorf("the added line is %v", added)
	}
	// And the bulleted list is untouched. It is a TOOL RESULT here on purpose: an assistant reply
	// goes through markdown and never reaches the diff path at all, so putting the list there made
	// this half of the test vacuous — it passed with the hunk-header requirement removed.
	if count("dadd") != 1 || count("ddel") != 1 {
		t.Errorf("a list with dashes in it was coloured as a diff: %v", classes)
	}
}

// How a thing ended is a glyph, not only a colour.
//
// A state told only in ink is a state some readers are not told, and these three rows each carried
// their outcome in the colour alone: an orphaned result, a failed one, and an error that ended a
// turn.
func TestAnOutcomeIsAGlyphAndNotOnlyAColour(t *testing.T) {
	got := runPage(t, `[]`, "?d=%2Fs%2Fa.sock", `
draw([{who:'result',text:'it worked'},{who:'failed',text:'it did not'},
      {who:'error',text:'the provider closed the stream'}]);
// Text and marks: an outcome that became a drawing carries no text for a walker to find, and the
// point of this test is that the outcome is said in more than colour — a symbol counts.
const textOf = n => (n.textContent || '') + (n.attrs && n.attrs.href ? ' ' + n.attrs.href : '') +
                    (n.children || []).map(textOf).join('');
console.log(JSON.stringify({rows: byId.log.children.map(r => ({cls: r.className, text: textOf(r)}))}));`)

	rows, _ := got["rows"].([]any)
	if len(rows) != 3 {
		t.Fatalf("drew %d rows, want 3", len(rows))
	}
	// The character, or the symbol that replaced it. Both say the same thing and a build has one of
	// the two; asserting only on the character failed a build whose marks are drawings.
	want := []struct{ cls, glyph, drawn string }{
		{"result", "✓", "i-sl-check"}, {"failed", "✗", "i-sl-xmark"}, {"error", "✗", "i-sl-xmark"}}
	for i, w := range want {
		m, _ := rows[i].(map[string]any)
		cls, _ := m["cls"].(string)
		text, _ := m["text"].(string)
		if !strings.Contains(cls, w.cls) {
			t.Errorf("row %d is %q, want a %s row", i, cls, w.cls)
		}
		if !strings.Contains(text, w.glyph) && !strings.Contains(text, w.drawn) {
			t.Errorf("a %s row says %q, with neither %s nor a drawn mark in it", w.cls, text, w.glyph)
		}
	}
}

// Opening one folded row opens that row, and nothing else on the screen moves.
//
// It used to open every row of the same kind. In use that is not a convenience: you click a tool
// call to see what it ran, everything above and below expands, and the row you clicked is
// somewhere else by the time the page settles. The preference is still remembered per kind and
// still decides how the NEXT rows arrive — this is about the ones already in front of you.
func TestOpeningOneRowLeavesTheRestWhereTheyWere(t *testing.T) {
	got := runPage(t, `[]`, "?d=%2Fs%2Fa.sock", `
draw([{who:'thinking',text:'first'},{who:'assistant',text:'a reply'},{who:'thinking',text:'second'}]);
const folds = [];
const walk = n => { for (const k of n.children || []) { if (k.className === 'txt fold') folds.push(k); walk(k); } };
walk(byId.log);
// Open the first one the way a press does.
folds[0].open = true;
folds[0].dispatchEvent({type: 'toggle'});
console.log(JSON.stringify({open: folds.map(f => !!f.open), count: folds.length}));`)

	if n, _ := got["count"].(float64); n != 2 {
		t.Fatalf("found %v folded rows, want 2", got["count"])
	}
	open, _ := got["open"].([]any)
	if len(open) != 2 || open[0] != true {
		t.Fatalf("the row that was pressed did not open: %v", open)
	}
	if open[1] == true {
		t.Error("opening one row opened another one that nobody pressed")
	}
}

// A working row says what it is inside of, when the tool inside it is saying anything.
//
// "working · 7 steps" answers how much has been done and answers nothing about a turn that has
// spent ten minutes in ONE call — which is the row somebody is squinting at wondering whether to
// interrupt. The tool knows, and its note reaches the browser on this poll and by no other route:
// it rides a transient event that never leaves the daemon's process and is never written to the log.
func TestAWorkingRowShowsWhatTheCallIsSaying(t *testing.T) {
	got := runPage(t, `[
      {"socket":"/s/a.sock","name":"api","workdir":"/w/api","state":"working","live":true,
       "task":"make the tests pass","doing":"check 6, 4m12s elapsed, still running","steps":7,"idle":12},
      {"socket":"/s/b.sock","name":"docs","workdir":"/w/docs","state":"idle","live":true,
       "task":"done here","steps":0,"idle":30}
    ]`, "", dumpFleet)

	cards := got["cards"].([]any)
	if len(cards) != 2 {
		t.Fatalf("drew %d cards for two agents", len(cards))
	}
	busy := cards[0].(map[string]any)["text"].(string)
	if !strings.Contains(busy, "check 6, 4m12s elapsed") {
		t.Errorf("the working row does not say what the call reported: %q", busy)
	}
	// Still says what was ASKED. The note is the detail under it, not a replacement — a row that
	// showed only "check 6" would have lost which agent is doing what.
	if !strings.Contains(busy, "make the tests pass") {
		t.Errorf("the note displaced the request: %q", busy)
	}
	// And the mark, because a line of tool output dropped into a row with nothing in front of it
	// reads as part of the request. The hourglass where this build has no icons, the turning
	// spinner where it has.
	if !strings.Contains(busy, "⏳") && !strings.Contains(busy, "i-sl-spinner-third") {
		t.Errorf("the note is not marked as one: %q", busy)
	}
}

// On a companion's own page it goes under the bar, on the call it belongs to.
//
// The bar says "still going", which after four minutes is the part you already believe. This is the
// part that decides whether to wait — and it comes from the fleet poll rather than the transcript
// stream, so this also checks the two are joined up at all.
func TestTheRunningCallSaysWhatItIsWaitingOn(t *testing.T) {
	got := runPage(t, `[{"socket":"/s/a.sock","name":"api","workdir":"/w/api","state":"working",
       "live":true,"task":"make the tests pass","doing":"check 6, 4m12s elapsed, still running",
       "steps":7,"idle":12}]`, "?d=%2Fs%2Fa.sock", `
await loadFleet();
draw([{who:'user',text:'wait for the build'},{who:'tool',tool:'wait_for',args:'for: build',pending:true}]);
const notes = byId.log.find('div').filter(d => d.className === 'note').map(d => d.textContent);
console.log(JSON.stringify({notes, bars: byId.log.find('md-linear-progress').length}));
`)
	notes, _ := got["notes"].([]any)
	if len(notes) != 1 {
		t.Fatalf("%d notes on a transcript with one running call: %v", len(notes), notes)
	}
	if !strings.Contains(notes[0].(string), "check 6, 4m12s elapsed") {
		t.Errorf("the note says %q", notes[0])
	}
	if n := got["bars"].(float64); n != 1 {
		t.Errorf("%v progress bars beside it", n)
	}
}

// And when nothing is reporting, nothing is drawn. An empty note rendered as "⏳" would be a
// heartbeat with no pulse behind it.
func TestAQuietCallDrawsNoNote(t *testing.T) {
	got := runPage(t, `[{"socket":"/s/a.sock","name":"api","workdir":"/w/api","state":"working",
       "live":true,"task":"make the tests pass","steps":7,"idle":12}]`, "?d=%2Fs%2Fa.sock", `
await loadFleet();
draw([{who:'user',text:'wait for the build'},{who:'tool',tool:'wait_for',args:'for: build',pending:true}]);
const notes = byId.log.find('div').filter(d => d.className === 'note').map(d => d.textContent);
console.log(JSON.stringify({notes, bars: byId.log.find('md-linear-progress').length}));
`)
	if notes, _ := got["notes"].([]any); len(notes) != 0 {
		t.Errorf("a quiet call drew %v", notes)
	}
	// The bar is still there: the call IS running, which is what the bar says.
	if n := got["bars"].(float64); n != 1 {
		t.Errorf("%v progress bars on a running call with no note", n)
	}
}

const withACompanionElsewhere = `[
  {"socket":"/s/b.sock","name":"docs","workdir":"/w/docs","state":"working","live":true,
   "task":"rewrite the page","steps":12,"idle":2,"host":"mini","addr":"10.0.0.12"},
  {"socket":"/s/far.sock","name":"design","workdir":"/w/design","state":"idle","elsewhere":true,
   "live":true,"role":"screens","idle":40,"host":"buildbox","instance":"you@buildbox"}
]`

// A companion on another machine is shown, counted apart from the dead, and not opened from here.
//
// The link is the part that matters. Its socket is a path on ITS filesystem, and this console
// resolves paths against its own — which on two machines set up by one person is frequently a real
// companion, the wrong one. A row that quietly opened somebody else is worse than no row.
func TestACompanionElsewhereIsShownAndNotOpenedFromHere(t *testing.T) {
	got := runPage(t, withACompanionElsewhere, "", rowsHelper+`
await loadFleet();
const far = rows().find(r => String(r.text).includes('design'));
// attrs.href, not .href: the fake DOM stores what was assigned and has no getter for it, so
// reading the property answers undefined whether or not the page set one. Found by mutation —
// the first version of this asserted on .href and passed with the guard removed.
console.log(JSON.stringify({
  found: !!far,
  href: far ? (far.attrs.href || '') : 'no row',
  clickable: far ? !!far.onclick : true,
  tiles: byId.summary.children.filter(t => t.className !== 'toboard').map(t => t.text),
}));
`)
	if got["found"] != true {
		t.Fatal("the companion on buildbox is not in the list")
	}
	if h := got["href"].(string); h != "" {
		t.Errorf("the row links to %q — this console cannot open a companion on another machine", h)
	}
	if got["clickable"] == true {
		t.Error("clicking the row opens a socket path that belongs to another machine")
	}
	var tiles []string
	for _, tl := range got["tiles"].([]any) {
		tiles = append(tiles, tl.(string))
	}
	// Four tiles, and no fifth for the companion elsewhere. The tiles are a row of states — what
	// something is DOING — and "elsewhere" is a place: nothing here dials a companion on another
	// instance, so that tile said where the row was while the row's own host column already said
	// it, and said nothing about what it was doing. It is also not folded into any of the four:
	// counting it as idle would be the invention the whole remote path refuses to make.
	if strings.Join(tiles, "|") != "0 Waiting|1 Working|0 Idle|0 Gone" {
		t.Errorf("the summary reads %v", tiles)
	}
}

// The three councillors keep the hues they have in the terminal.
//
// Three voices in one colour is a wall of identical rows, and which of them said the thing you are
// reading is most of what a council transcript is for. The tokens were declared in this page from
// the beginning and nothing used them.
func TestEachCouncillorIsDrawnInTheirOwnColour(t *testing.T) {
	got := runPage(t, `[]`, "?d=%2Fs%2Fa.sock", `
draw([{who:'council',member:'Melchior',decision:'done',text:'Melchior: accept'},
      {who:'council',member:'Balthasar',decision:'continue',text:'Balthasar: reject'},
      {who:'council',member:'Casper',decision:'continue',text:'Casper: reject'},
      {who:'council',member:'Rei',decision:'done',text:'Rei: accept'}]);
const cls = [];
const walk = n => { for (const k of n.children || []) { if (String(k.className).startsWith('row ')) cls.push(k.className); walk(k); } };
walk(byId.log);
console.log(JSON.stringify({cls: cls}));`)

	cls, _ := got["cls"].([]any)
	if len(cls) != 4 {
		t.Fatalf("drew %d council rows, want 4: %v", len(cls), cls)
	}
	for i, want := range []string{"m-melchior", "m-balthasar", "m-casper"} {
		if s, _ := cls[i].(string); !strings.Contains(s, want) {
			t.Errorf("council row %d is %q, without %q", i, s, want)
		}
	}
	// A member the palette has no seat for falls back to the council colour rather than becoming
	// a class named after whatever the log happened to say.
	if s, _ := cls[3].(string); strings.Contains(s, "m-rei") || strings.Contains(s, "m-Rei") {
		t.Errorf("a name out of the log became a selector: %q", s)
	}
	// The verdict still owns the summary: what a row SAYS is done or continue, whoever said it.
	if s, _ := cls[1].(string); !strings.Contains(s, "v-continue") {
		t.Errorf("the verdict class was lost when the member class arrived: %q", s)
	}
}

// magi's own voice is a voice, not an unstyled fallthrough.
//
// Prompts written by magi rather than by the person reconstruct as system messages, and this page
// draws a row per role — so these landed as `.row.system`, for which there was no rule at all and
// a gutter that said the word "system". That has been true of every compaction summary since
// compaction existed; the orchestrator's nudges joined them.
func TestMagisOwnVoiceIsDrawnAsItsOwn(t *testing.T) {
	got := runPage(t, `[]`, "?d=%2Fs%2Fa.sock", `
draw([{who:'user',text:'rename a.txt'},
      {who:'system',text:'You stopped without saying you are finished.'}]);
const rows = [];
const walk = n => { for (const k of n.children || []) { if (String(k.className).startsWith('row ')) rows.push({cls:k.className, who:(k.children[0]||{}).textContent}); walk(k); } };
walk(byId.log);
console.log(JSON.stringify({rows: rows}));`)

	rows, _ := got["rows"].([]any)
	if len(rows) != 2 {
		t.Fatalf("drew %d rows, want 2: %v", len(rows), rows)
	}
	sys, _ := rows[1].(map[string]any)
	if cls, _ := sys["cls"].(string); !strings.Contains(cls, "row system") {
		t.Errorf("magi's own prompt is not classed as its own: %q", cls)
	}
	// The gutter names the speaker, not the mechanism that carried it.
	if who, _ := sys["who"].(string); who == "system" || who == "" {
		t.Errorf("the gutter says %q", who)
	}
}

// A council seat has a way one level in — its NAME — and a round's outcome does not.
//
// The fold under a verdict holds the member's reasoning. The screen behind it holds what the
// member was JUDGING — which is the half that makes a vote checkable, and the half a transcript
// row has no room for. An outcome is a tally with nothing behind it that is not already on the row.
//
// The way in used to be a button beside the row. It read as furniture on every council row, and
// the name is what somebody points at when they want to know what that member saw.
func TestOnlyAMembersVoteLeadsOneLevelIn(t *testing.T) {
	got := runPage(t, `[]`, "?d=%2Fs%2Fa.sock", `
draw([{who:'council',member:'Melchior',decision:'done',round:1,text:'Melchior: accept'},
      {who:'council',decision:'done',round:1,tally:'3 done',text:'the council says done'},
      {who:'assistant',text:'finished'}]);
const deep = [], furniture = [];
const walk = n => { for (const k of n.children || []) {
  if (String(k.className).includes('whoin')) deep.push(k.textContent);
  if (String(k.className).includes('deeper')) furniture.push(k.textContent);
  walk(k); } };
walk(byId.log);
console.log(JSON.stringify({deep: deep, furniture: furniture}));`)

	deep, _ := got["deep"].([]any)
	if len(deep) != 1 {
		t.Fatalf("%d rows offer a way in, want 1 (the member's vote): %v", len(deep), deep)
	}
	// And it is the name that leads there, not a control sitting beside it.
	if name, _ := deep[0].(string); !strings.Contains(strings.ToLower(name), "council") && name == "" {
		t.Errorf("the way in is labelled %q", name)
	}
	if extra, _ := got["furniture"].([]any); len(extra) != 0 {
		t.Errorf("a second control sits beside the row: %v", extra)
	}
}

// A seat that changed its mind opens on the vote that counted.
//
// A member votes twice when a rebuttal round turns it around, and both rows are in the transcript.
// Reading the first put a rejection behind a row that said the opposite.
func TestASeatThatChangedItsMindOpensOnTheVoteThatCounted(t *testing.T) {
	got := runPage(t, `[]`, "?d=%2Fs%2Fa.sock&cr=1%3AMelchior", `
lastRows = [{who:'council',member:'Melchior',round:1,decision:'continue',why:'the tests do not run'},
            {who:'council',member:'Melchior',round:1,decision:'done',why:'they run now'}];
await drawVerdict({socket:'/s/a.sock'}, '1:Melchior');
// The fake node's textContent is its own text, not its subtree's, so the tree is walked.
const seen = [];
const walk = n => { if (n.textContent) seen.push(n.textContent); for (const k of n.children || []) walk(k); };
walk(byId.agentdetail);
console.log(JSON.stringify({text: seen.join(' | ')}));`)

	txt, _ := got["text"].(string)
	if !strings.Contains(txt, "they run now") {
		t.Errorf("the detail shows a vote the member replaced:\n%s", txt)
	}
	if strings.Contains(txt, "the tests do not run") {
		t.Errorf("the superseded vote is on screen:\n%s", txt)
	}
}

// Past work is one level in, not in the pane.
//
// The pane is meant to stay open, so what is in it has to be worth the width all the time: the
// plan moves, the queue moves, what was handed out moves. A list of finished sessions does not —
// you go and look at it, and while you are looking at it that is the screen you want.
func TestPastWorkIsAScreenAndNotAPaneCard(t *testing.T) {
	got := runPage(t, `[
      {"id":"s_old","title":"rename the tokens","ago":8000},
      {"id":"s_now","title":"the turn it is in","ago":3,"current":true}
    ]`, "?d=%2Fs%2Fa.sock&past=", `
await drawPast({socket:'/s/a.sock'});
const rows = [];
const walk = n => { for (const k of n.children || []) { if (String(k.className).startsWith('hs')) rows.push({txt: k.textContent, opens: !!k.onclick, tag: k.tag}); walk(k); } };
walk(byId.agentdetail);
console.log(JSON.stringify({inPane: 'history' in byId, rows: rows}));`)

	if inPane, _ := got["inPane"].(bool); inPane {
		t.Error("the list is still a card in the pane")
	}
	rows, _ := got["rows"].([]any)
	if len(rows) != 2 {
		t.Fatalf("drew %d past sessions, want 2: %v", len(rows), rows)
	}
	// The rows open. The list answers "what has it been doing"; the session behind a row answers
	// "what happened in that one", and there was no way to ask the second at all.
	for i, r := range rows {
		m, _ := r.(map[string]any)
		if opens, _ := m["opens"].(bool); !opens {
			t.Errorf("row %d does not open: %v", i, m)
		}
		// A control, so it is reachable by keyboard and announces itself as one.
		if tag, _ := m["tag"].(string); tag != "button" {
			t.Errorf("row %d is a %q rather than a button", i, tag)
		}
	}
}

// A decision with grounds can be read at the width the grounds need.
//
// The bar above the composer is the right shape for "run this command?" — a line and two buttons.
// It is the wrong shape for the case this exists for: an agent that worked for an hour while
// nobody watched and now asks something whose answer depends on what it found. That is three
// sections of prose, and a strip under a transcript is where prose goes to be skipped.
func TestADecisionWithGroundsOpensAtFullWidth(t *testing.T) {
	fleet := `[{"socket":"/s/a.sock","name":"design","workdir":"/w","state":"waiting","live":true,
      "asking":"Which branch should this land on?","askId":"q1","askKind":"question",
      "report":[{"key":"tried","text":"ran the suite on both branches"},
                {"key":"lean","text":"engine-ui-split"}]}]`
	got := runPage(t, fleet, "?d=%2Fs%2Fa.sock&ask=q1", `
await drawAsk({socket:'/s/a.sock'});
const seen = [];
const walk = n => { if (n.textContent) seen.push(n.textContent); for (const k of n.children || []) walk(k); };
walk(byId.agentdetail);
const fields = [];
const walk2 = n => { for (const k of n.children || []) { if (k.tag === 'md-outlined-text-field') fields.push(1); walk2(k); } };
walk2(byId.agentdetail);
console.log(JSON.stringify({text: seen.join(' | '), fields: fields.length}));`)

	txt, _ := got["text"].(string)
	for _, want := range []string{"Which branch", "tried", "ran the suite on both", "lean"} {
		if !strings.Contains(txt, want) {
			t.Errorf("the decision screen is missing %q:\n%s", want, txt)
		}
	}
	// And it is answerable from there. A screen you have to leave to act on is a screen that has
	// made the decision harder, not easier.
	if n, _ := got["fields"].(float64); n < 1 {
		t.Errorf("nothing to answer with on the screen: %v", got)
	}
}

// A question answered from somewhere else does not leave a dead screen up.
//
// The prompt is not in the log — it is a question about what should happen, not a record of what
// did — so it is read from the fleet poll. When the poll stops carrying it, there is nothing here
// to answer and the screen returns to the conversation.
func TestADecisionAlreadyAnsweredReturnsToTheConversation(t *testing.T) {
	got := runPage(t, `[{"socket":"/s/a.sock","name":"design","workdir":"/w","state":"idle","live":true}]`,
		"?d=%2Fs%2Fa.sock&ask=q1", `
await drawAsk({socket:'/s/a.sock'});
console.log(JSON.stringify({url: location.search.includes('ask='), kids: byId.agentdetail.children.length}));`)

	if still, _ := got["url"].(bool); still {
		t.Error("the address still points at a question nobody can answer")
	}
	if n, _ := got["kids"].(float64); n > 0 {
		t.Errorf("a dead decision screen was drawn: %v children", n)
	}
}

// A successful tool call says what it answered, not what it was asked twice.
//
// Only failures used to carry their output, on the reasoning that a success is noise and the
// arguments are more useful. The row folds, so a success costs nothing until somebody opens it —
// and when they opened it they got the arguments they had just read in the summary line, again,
// with the answer nowhere. "What did the grep find" is most of why anybody opens a tool call.
func TestASuccessfulToolCallCarriesWhatItAnswered(t *testing.T) {
	got := runPage(t, `[]`, "?d=%2Fs%2Fa.sock", `
draw([{who:'tool',tool:'grep',args:'{"pattern":"empty-state"}',ok:true,
       out:'src/list.tsx\nsrc/table.tsx'}]);
const pre = [], labels = [];
const walk = n => { for (const k of n.children || []) {
  if (k.tag === 'pre') pre.push(k.textContent);
  if ((k.className || '') === 'foldk') labels.push(k.textContent);
  walk(k); } };
walk(byId.log);
console.log(JSON.stringify({pre: pre, labels: labels}));`)

	// Two blocks, in the order the call happened: what it was asked, then what it said. They were
	// one blob with a rule of box characters between them, which tells a reader that something
	// changed there and leaves them to work out what.
	pre, _ := got["pre"].([]any)
	if len(pre) != 2 {
		t.Fatalf("drew %d blocks, want the question and the answer: %v", len(pre), pre)
	}
	asked, _ := pre[0].(string)
	answered, _ := pre[1].(string)
	if !strings.Contains(answered, "src/list.tsx") {
		t.Errorf("the output is missing:\n%s", answered)
	}
	if !strings.Contains(asked, "empty-state") {
		t.Errorf("what it was asked is missing:\n%s", asked)
	}
	labels, _ := got["labels"].([]any)
	if len(labels) != 2 {
		t.Errorf("the two blocks are not named: %v", labels)
	}
}

// The pane's handle says which way it is.
//
// It carried aria-expanded from the start, so a screen reader was told and an eye was not — the
// same icon in the same colour whether the pane was open or shut, which makes it a button you
// press to find out.
func TestThePaneHandleSaysWhetherItIsOpen(t *testing.T) {
	got := runPage(t, `[]`, "?d=%2Fs%2Fa.sock", `
const t0 = byId.sideToggle.attrs['aria-expanded'];
byId.sideToggle.onclick();
const t1 = byId.sideToggle.attrs['aria-expanded'];
console.log(JSON.stringify({before: t0, after: t1}));`)

	before, _ := got["before"].(string)
	after, _ := got["after"].(string)
	if before == after {
		t.Errorf("pressing the handle did not change what it claims: %q → %q", before, after)
	}
	// And the claim is drawn, not only announced. The stylesheet is read out of the page itself:
	// a rule that exists only in a test's idea of the page is a rule nobody ships.
	if !strings.Contains(indexHTML, "#sideToggle[aria-expanded=\"true\"]") {
		t.Error("nothing paints the open state, so only a screen reader is told")
	}
}

// The prompt bar places a question in the run its call is asking.
func TestThePromptBarSaysWhichOfHowMany(t *testing.T) {
	fleet := `[{"socket":"/s/a.sock","name":"design","workdir":"/w","state":"waiting","live":true,
      "asking":"and the scope?","askId":"q2","askKind":"question","askIndex":2,"askTotal":3}]`
	got := runPage(t, fleet, "?d=%2Fs%2Fa.sock", `
drawPrompt({socket:'/s/a.sock', state:'waiting', asking:'and the scope?', askId:'q2',
            askKind:'question', askIndex:2, askTotal:3});
const seen = [];
const walk = n => { if (n.textContent) seen.push(n.textContent); for (const k of n.children || []) walk(k); };
walk(byId.prompt);
console.log(JSON.stringify({text: seen.join(' | ')}));`)

	txt, _ := got["text"].(string)
	if !strings.Contains(txt, "2 of 3") {
		t.Errorf("the bar does not place the question in its run:\n%s", txt)
	}
}

// One question is not numbered: "1 of 1" answers something nobody asked.
func TestALoneQuestionIsNotNumbered(t *testing.T) {
	got := runPage(t, `[]`, "?d=%2Fs%2Fa.sock", `
drawPrompt({socket:'/s/a.sock', state:'waiting', asking:'go ahead?', askId:'q1',
            askKind:'question', askIndex:1, askTotal:1});
const seen = [];
const walk = n => { if (n.textContent) seen.push(n.textContent); for (const k of n.children || []) walk(k); };
walk(byId.prompt);
console.log(JSON.stringify({text: seen.join(' | ')}));`)

	txt, _ := got["text"].(string)
	if strings.Contains(txt, "1 of 1") {
		t.Errorf("a lone question is numbered anyway:\n%s", txt)
	}
	if !strings.Contains(txt, "go ahead?") {
		t.Errorf("the question is missing:\n%s", txt)
	}
}

// Leaving a companion takes its pane with it.
//
// The cards were hidden by a list of ids kept by hand in render, and the scheduled-work card was
// added without being added to it — so it stayed on screen over the fleet list, showing one
// agent's jobs while you looked at all of them. A hand-kept list cannot fail a build when somebody
// adds the fifth card; a walk of the pane cannot miss one.
//
// The cards come from the MARKUP, so this fails if one is added and render stops covering it. The
// fake DOM has no tree of its own, and listing them here by hand would be the same hand-kept list
// one layer down.
func TestLeavingACompanionHidesEveryCardInThePane(t *testing.T) {
	aside := indexHTML[strings.Index(indexHTML, `<aside id="side">`):]
	aside = aside[:strings.Index(aside, "</aside>")]
	var ids []string
	for _, m := range regexp.MustCompile(`id="(\w+)"`).FindAllStringSubmatch(aside, -1) {
		ids = append(ids, m[1])
	}
	if len(ids) < 3 {
		t.Fatalf("found %d cards in the pane markup — the scrape has lost its subject", len(ids))
	}
	list, err := json.Marshal(ids)
	if err != nil {
		t.Fatal(err)
	}

	got := runPage(t, "[]", "", `
// The pane, as the markup has it, shown the way a companion's page leaves them.
byId.side.children = `+string(list)+`.map(id => byId[id]);
for (const c of byId.side.children) c.hidden = false;
render();
console.log(JSON.stringify({showing: byId.side.children.filter(c => !c.attrs.hidden).map(c => c.id)}));`)

	if showing, _ := got["showing"].([]any); len(showing) != 0 {
		t.Errorf("these stayed on screen over the fleet list: %v", showing)
	}
}

// A transcript says when, not only what.
//
// The terminal has stamped every user and assistant block with HH:MM since it had a transcript.
// The page had no time on it anywhere — and the reason was one layer down: a message is rebuilt
// from the log and the rebuild dropped the envelope's timestamp, so the console, which only ever
// reads rebuilt messages, had nothing to draw even though the log had recorded it all along.
func TestTheTranscriptSaysWhenEachThingWasSaid(t *testing.T) {
	got := runPage(t, `[]`, "?d=%2Fs%2Fa.sock", `
draw([{who:'user', text:'go and do it', at:'2026-08-11T04:05:00Z'},
      {who:'assistant', text:'done', at:'2026-08-11T04:07:00Z'},
      {who:'assistant', text:'from an older log with no stamp'}]);
const when = [];
const walk = n => { for (const k of n.children || []) { if ((k.className||'') === 'when') when.push(k.textContent); walk(k); } };
walk(byId.log);
console.log(JSON.stringify({when: when}));`)
	when, _ := got["when"].([]any)
	if len(when) != 2 {
		t.Fatalf("the page drew %d times for three rows, two of which are stamped: %v", len(when), when)
	}
	// Local time, so the assertion is on the shape and the gap rather than on the reader's zone.
	for _, w := range when {
		if s, _ := w.(string); len(s) != 5 || s[2] != ':' {
			t.Errorf("a time reads as %q", w)
		}
	}
	if when[0] == when[1] {
		t.Errorf("both rows say %v — the stamp is not the row's own", when[0])
	}
}

// A frame that changes one row rebuilds one row.
//
// The transcript is re-sent whole two and a half times a second and it used to be re-BUILT whole
// just as often — every row of an hour-long session thrown away and made again, markdown and all,
// four hundred times a minute.
//
// It cost more than time. A fold is a node, so its open state died with it: pressing one open and
// waiting 400ms opened every row of the same kind, because the frame that replaced it read the
// per-kind preference back for all of them.
func TestAFrameThatChangesOneRowRebuildsOneRow(t *testing.T) {
	got := runPage(t, `[]`, "?d=%2Fs%2Fa.sock", `
const rows = [];
for (let i = 0; i < 40; i++) rows.push({who:'assistant', text:'line ' + i, at:'2026-08-11T04:0' + (i%10) + ':00Z'});
draw(rows);
const first = byId.log.children.slice();
// The same frame again: nothing changed, so nothing is rebuilt.
draw(rows.map(r => Object.assign({}, r)));
const again = byId.log.children.slice();
const kept = again.filter((n, i) => n === first[i]).length;
// One more row arrives, as a turn's next line does.
draw([...rows, {who:'assistant', text:'the new one', at:'2026-08-11T04:11:00Z'}]);
const grown = byId.log.children.slice();
const keptAfterGrowth = grown.slice(0, 40).filter((n, i) => n === first[i]).length;
// And an EARLIER row changing takes the rows after it with it, which is what a compaction does.
// A fold opened by hand stays open across the frames that follow. It is the same fact as the
// node identity above, and it is the one somebody notices: the row you pressed open used to shut
// itself — or open every row of its kind — a few hundred milliseconds later.
draw([...rows, {who:'tool', tool:'read', args:'{"path":"go.mod"}', ok:true, out:'module magi'}]);
const fold = byId.log.children[40].children.find(c => c.tag === 'details');
fold.open = true;
draw([...rows, {who:'tool', tool:'read', args:'{"path":"go.mod"}', ok:true, out:'module magi'}]);
const stillOpen = byId.log.children[40].children.find(c => c.tag === 'details').open;
const rewritten = rows.map((r, i) => i === 5 ? Object.assign({}, r, {text:'rewritten'}) : r);
draw(rewritten);
const after = byId.log.children.slice();
console.log(JSON.stringify({
  kept: kept, keptAfterGrowth: keptAfterGrowth, grownLen: grown.length, stillOpen: stillOpen,
  keptBeforeEdit: after.slice(0, 5).filter((n, i) => n === first[i]).length,
  rebuiltFromEdit: after.slice(5).filter((n, i) => n === first[i + 5]).length,
  finalLen: after.length}));`)
	num := func(k string) int {
		f, _ := got[k].(float64)
		return int(f)
	}
	if num("kept") != 40 {
		t.Errorf("an unchanged frame kept %d of 40 rows", num("kept"))
	}
	if num("keptAfterGrowth") != 40 || num("grownLen") != 41 {
		t.Errorf("one new row rebuilt the transcript: kept %d, now %d rows",
			num("keptAfterGrowth"), num("grownLen"))
	}
	if got["stillOpen"] != true {
		t.Error("a fold pressed open shut itself on the next frame")
	}
	// A row that changed does not keep its node, and neither do the rows after it: the page cannot
	// know whether what follows still belongs where it was.
	if num("keptBeforeEdit") != 5 {
		t.Errorf("the rows before an edit were rebuilt too (%d of 5 kept)", num("keptBeforeEdit"))
	}
	if num("rebuiltFromEdit") != 0 {
		t.Errorf("%d rows from the edit on kept a node built for different content", num("rebuiltFromEdit"))
	}
	if num("finalLen") != 40 {
		t.Errorf("the rewritten transcript has %d rows", num("finalLen"))
	}
}

// The gutter says who, in words, and the person's name is the one their companion uses.
//
// The role is what the server calls a row. The console printed it raw — "user", "assistant" — so a
// plugin that renamed the person (an SSO bridge putting the authenticated username there) showed
// up in the terminal and nowhere else, and even the fallback disagreed: "you" in one window and
// "user" in the other, for the same conversation.
func TestTheGutterNamesWhoInWords(t *testing.T) {
	got := runPage(t, `[{"socket":"/s/a.sock","name":"a","live":true,"state":"idle","user":"sayaya","session":"s_1"}]`,
		"?d=%2Fs%2Fa.sock", `
await loadFleet();
draw([{who:'user', text:'go and do it'},
      {who:'assistant', text:'done'},
      {who:'system', text:'you stopped without saying you are finished'},
      {who:'tool', tool:'read', args:'{}', ok:true, out:'ok'}]);
const who = byId.log.children.map(r => (r.children.find(c => (c.className||'') === 'who') || {}).textContent);
console.log(JSON.stringify({who: who}));`)
	who, _ := got["who"].([]any)
	if len(who) != 4 {
		t.Fatalf("drew %d rows: %v", len(who), who)
	}
	if who[0] != "sayaya" {
		t.Errorf("the person is called %q, not the name their companion uses", who[0])
	}
	// The companion is the companion, not the API's word for the role.
	if who[1] != "magi" {
		t.Errorf("the answer is attributed to %q", who[1])
	}
	if who[2] == "system" {
		t.Error("magi's own voice is labelled with the mechanism rather than the speaker")
	}
	// A tool is a tool: nothing to translate and nobody to name.
	if who[3] != "tool" {
		t.Errorf("a tool call is attributed to %q", who[3])
	}
}

// The strip says what is running beside the turn, without being opened.
//
// A child used to be reachable only through a button inside the facts card — so finding out that
// anything was happening at all meant opening a card to look. The terminal has kept this along its
// bottom since it had one, and it is the answer to "is something going on", which is a question you
// do not ask by navigating.
func TestTheStripSaysWhatIsRunningBesideTheTurn(t *testing.T) {
	got := runPage(t, `[{"socket":"/s/a.sock","name":"a","live":true,"state":"working","session":"s_1"}]`,
		"?d=%2Fs%2Fa.sock", `
ROUTES['/jobs'] = {children: [
    {id: 's_kid', tool: 'scout', task: 'find every empty state', running: true},
    {id: 's_gone', tool: 'refine', task: 'tighten the wording', running: false, err: 'context cancelled'}],
  background: [{id: 'bg', command: 'go build ./...', running: true, tail: 'one\nthe last line'}]};
// loadJobs is started by the poll and awaited here directly, because the fake's setTimeout does
// not run anything — the strip's fetch has to be the thing that is waited on.
await loadFleet();
await loadJobs({socket: '/s/a.sock'});
// The fake's textContent is a node's OWN text, so a chip's words are gathered from its parts.
const words = n => (n.textContent || '') + (n.children || []).map(words).join(' ');
const chips = byId.strip.children.map(c => ({
  cls: c.className, tag: c.tag, text: words(c),
  dot: (c.children || []).some(k => (k.className || '') === 'jdot')}));
byId.strip.children[0].onclick();
console.log(JSON.stringify({hidden: byId.strip.hidden, chips, url: location.search}));`)
	if got["hidden"] == true {
		t.Fatal("three things are running and the strip is hidden")
	}
	chips, _ := got["chips"].([]any)
	if len(chips) != 3 {
		t.Fatalf("the strip drew %d chips: %v", len(chips), chips)
	}
	first, _ := chips[0].(map[string]any)
	// Running, said by the chip itself. On the pressable one that is its class — the button
	// component lays out its own label box and a dot handed to it lands wherever that box puts it.
	if cls, _ := first["cls"].(string); !strings.Contains(cls, "live") {
		t.Errorf("a running child is drawn as %q and says nothing about running", cls)
	}
	if tag, _ := first["tag"].(string); tag != "button" {
		t.Errorf("a child is drawn as %q — it is a way into its own screen and has to be pressable", tag)
	}
	// The one that ended badly is marked as such; the one still going is not.
	second, _ := chips[1].(map[string]any)
	if cls, _ := second["cls"].(string); !strings.Contains(cls, "bad") {
		t.Errorf("a child that ended badly is drawn as %q", cls)
	}
	if cls, _ := second["cls"].(string); strings.Contains(cls, "live") {
		t.Error("a finished child is still pulsing")
	}
	// A background command shows the end of its output, which is the part that matters.
	third, _ := chips[2].(map[string]any)
	// The one that is not a control carries the dot, which is where a dot can be placed.
	if third["dot"] != true {
		t.Error("a running background command does not say it is running")
	}
	if text, _ := third["text"].(string); !strings.Contains(text, "the last line") {
		t.Errorf("the command's chip reads %q", text)
	}
	if tag, _ := third["tag"].(string); tag == "button" {
		t.Error("a background command is drawn as a control, and pressing it does nothing")
	}
	// Pressing a child goes to its screen rather than doing something in place.
	if u, _ := got["url"].(string); !strings.Contains(u, "sub=s_kid") {
		t.Errorf("pressing the child went to %q", u)
	}
}

// An edit shows the change it makes, and a plan shows the plan.
//
// Both arrived as the call's raw arguments: an edit as its old and new text escaped into one JSON
// line — the least readable form of the most important thing an agent does — and a plan as the
// same JSON the panel three inches away turns into ticked lines. The terminal has drawn both
// properly since it had a transcript.
func TestAnEditShowsItsChangeAndAPlanShowsItsPlan(t *testing.T) {
	got := runPage(t, `[]`, "?d=%2Fs%2Fa.sock", `
draw([
  {who:'tool', tool:'edit', ok:true, args:'{"path":"cmd/main.go","old":"a","new":"b"}',
   diff:'-a\n+b', out:'"edited"'},
  {who:'tool', tool:'todo_write', ok:true,
   args:JSON.stringify({todos:[{content:'read the failing test',status:'completed'},
                               {content:'fix the parser',status:'in_progress'},
                               {content:'ship it',status:'pending'}]})},
  {who:'tool', tool:'grep', ok:true, args:'{"pattern":"empty"}', out:'"src/list.tsx\\nsrc/table.tsx"'},
]);
const rows = byId.log.children;
// .text, not .textContent: the summary is a mark node and a text node now, and the element's own
// string is empty. The recursive getter is what a reader of that line actually sees.
const sum = r => (r.children.find(c => (c.className||'').includes('fold')) || {children:[]})
  .children.filter(c => c.tag === 'summary').map(c => c.text)[0] || '';
const words = n => (n.textContent || '') + (n.children || []).map(words).join(' ');
const body = r => { const f = r.children.find(c => (c.className||'').includes('fold')); return f ? words(f) : ''; };
const cls = n => { const out=[]; const walk=k=>{ out.push(k.className||''); (k.children||[]).forEach(walk); }; walk(n); return out; };
console.log(JSON.stringify({
  editSum: sum(rows[0]), editBody: body(rows[0]), editCls: cls(rows[0]).filter(c => c === 'dadd' || c === 'ddel'),
  planSum: sum(rows[1]), planCls: cls(rows[1]).filter(c => c.startsWith('td ')), planBody: body(rows[1]),
  grepSum: sum(rows[2])}));`)
	str := func(k string) string { s, _ := got[k].(string); return s }
	// The edit: the path on the line you can see, the change coloured behind it, and no JSON.
	if !strings.Contains(str("editSum"), "cmd/main.go") {
		t.Errorf("the edit's summary reads %q", str("editSum"))
	}
	if strings.Contains(str("editBody"), `"old"`) {
		t.Errorf("the edit is still shown as its arguments:\n%s", str("editBody"))
	}
	if n := len(got["editCls"].([]any)); n != 2 {
		t.Errorf("the diff was drawn with %d coloured lines, want an added and a removed one", n)
	}
	// The plan: a count where the argument preview was, and ticked lines instead of JSON.
	if !strings.Contains(str("planSum"), "1/3") {
		t.Errorf("the plan's summary reads %q", str("planSum"))
	}
	if rows, _ := got["planCls"].([]any); len(rows) != 3 {
		t.Errorf("the plan drew %d rows", len(rows))
	}
	if !strings.Contains(str("planBody"), "fix the parser") || strings.Contains(str("planBody"), "status") {
		t.Errorf("the plan body reads %q", str("planBody"))
	}
	// And an ordinary call says what came back, on the line that is visible while it is shut.
	if !strings.Contains(str("grepSum"), "⟶") || !strings.Contains(str("grepSum"), "src/list.tsx") {
		t.Errorf("a finished call's summary does not say what it found: %q", str("grepSum"))
	}
}

// The copy control hands over the SOURCE, which is the thing selecting the page cannot give you.
//
// Select an answer with a table in it and copy: what lands on the clipboard is the rendered cells
// run together, without the pipes that made it a table. What somebody pasting an answer elsewhere
// wants is what was written. The terminal has copied the source since it had a transcript.
func TestCopyingAMessageHandsOverWhatWasWritten(t *testing.T) {
	got := runPage(t, `[]`, "?d=%2Fs%2Fa.sock", `
const table = '| where | today |\n|---|---|\n| fleet | nothing |';
draw([{who:'user', text:'spec the empty state'},
      {who:'assistant', text:table},
      {who:'tool', tool:'read', args:'{}', ok:true, out:'ok'}]);
const chip = r => (r.children.find(c => (c.className||'') === 'who') || {children:[]})
  .children.filter(c => (c.className||'').startsWith('copy'))[0];
const rows = byId.log.children;
const onProse = !!chip(rows[0]) && !!chip(rows[1]);
chip(rows[1]).onclick({preventDefault(){}, stopPropagation(){}});
await new Promise(r => queueMicrotask(r));
console.log(JSON.stringify({
  onProse, onTool: !!chip(rows[2]),
  wrote: CLIPBOARD.slice(-1)[0] || '',
  labelled: !!chip(rows[0]).attrs['aria-label']}));`)
	if got["onProse"] != true {
		t.Error("the rows that are prose have no way to copy what they say")
	}
	// Not on a tool call: its arguments and output are already preformatted text on the page, and
	// a control on every row of a busy transcript is furniture.
	if got["onTool"] == true {
		t.Error("a tool call carries a copy control")
	}
	if got["labelled"] != true {
		t.Error("the control is a glyph with no name — nothing announces it")
	}
	wrote, _ := got["wrote"].(string)
	if !strings.Contains(wrote, "|---|---|") {
		t.Errorf("what reached the clipboard is not the source that was written: %q", wrote)
	}
}

// Killing a companion turns the light out on the page that is showing it.
//
// The dot had two inputs and both were about this console's link to the server that serves it —
// which outlives the daemon. So stopping a companion left a green dot and the word "connected"
// beside a page whose subject no longer existed.
func TestAStoppedCompanionTurnsTheLightOut(t *testing.T) {
	got := runPage(t, `[{"socket":"/s/a.sock","name":"a","live":true,"state":"idle","session":"s_1"}]`,
		"?d=%2Fs%2Fa.sock", `
await loadFleet();
const first = byId.state.className;
// The same companion, no longer answering. This is what the probe reports the moment its socket
// stops accepting a dial.
FLEET[0].live = false;
await loadFleet();
const after = byId.state.className;
// And gone from the list outright, which is what happens once its socket file is cleaned up.
FLEET.length = 0;
await loadFleet();
console.log(JSON.stringify({first, after, missing: byId.state.className, note: byId.note.textContent}));`)
	first, _ := got["first"].(string)
	if strings.Contains(first, "lost") {
		t.Fatalf("a live companion starts out lost: %q", first)
	}
	after, _ := got["after"].(string)
	if !strings.Contains(after, "lost") {
		t.Errorf("a companion that stopped answering leaves the console reading %q", after)
	}
	if miss, _ := got["missing"].(string); !strings.Contains(miss, "lost") {
		t.Errorf("a companion gone from the fleet leaves the console reading %q", miss)
	}
	// Said, not only drawn: a state carried by one dot is a state some readers are not told.
	if note, _ := got["note"].(string); note == "" {
		t.Error("nothing says what happened")
	}
}

// Leaving a companion takes everything that was about it off the screen.
//
// Third time for this shape: the pane's cards were a hand-kept list of ids until one was forgotten
// and stayed up over the fleet list; the fix was to walk the pane, and the strip in the dock was
// outside that walk. What keeps it honest is that there is one place, so this asserts the place —
// nothing drawn for a companion survives the navigation away from it.
func TestLeavingACompanionClearsWhatWasAboutIt(t *testing.T) {
	got := runPage(t, `[{"socket":"/s/a.sock","name":"a","live":true,"state":"working","session":"s_1"}]`,
		"?d=%2Fs%2Fa.sock", `
ROUTES['/jobs'] = {children: [{id: 's_kid', tool: 'scout', task: 'look', running: true}]};
await loadFleet();
await loadJobs({socket: '/s/a.sock'});
draw([{who:'user', text:'do it'}, {who:'assistant', text:'done'}]);
const before = {strip: byId.strip.children.length, rows: byId.log.children.length};
// Away to the list, which is what pressing the crumb does: the address loses its ?d=.
location.search = '';
render();
console.log(JSON.stringify({before, strip: byId.strip.children.length, stripHidden: byId.strip.hidden,
  rows: byId.log.children.length, detailHidden: byId.detail.hidden,
  cards: [...byId.side.children].filter(c => !c.hidden).length}));`)
	before, _ := got["before"].(map[string]any)
	if n, _ := before["strip"].(float64); n == 0 {
		t.Fatal("precondition: nothing was in the strip to leave behind")
	}
	if n, _ := got["strip"].(float64); n != 0 {
		t.Errorf("%v chips followed the reader to the fleet list", n)
	}
	if got["stripHidden"] != true {
		t.Error("the strip is still up on a screen with no companion on it")
	}
	if n, _ := got["rows"].(float64); n != 0 {
		t.Errorf("%v transcript rows survived the navigation", n)
	}
	if got["detailHidden"] != true {
		t.Error("the facts of the companion you left are still on screen")
	}
	if n, _ := got["cards"].(float64); n != 0 {
		t.Errorf("%v pane cards are still showing", n)
	}
}

// A long transcript keeps only its tail in the page.
//
// The frame-by-frame rebuild went first and offscreen rows stopped costing layout, but every row a
// session ever produced was still a subtree in the document — which is the thing that was actually
// reported: the count itself, after a long day on one companion.
func TestOnlyTheTailOfALongTranscriptIsInThePage(t *testing.T) {
	got := runPage(t, `[]`, "?d=%2Fs%2Fa.sock", `
const many = [];
for (let i = 0; i < 600; i++) many.push({who:'assistant', text:'line ' + i});
draw(many);
const rows = () => byId.log.children.filter(c => (c.className||'') !== 'above');
const gap = () => byId.log.children.filter(c => (c.className||'') === 'above');
const windowed = rows().length;
const last = rows()[rows().length - 1];
const hasSpacer = gap().length;
// One more arrives: the window does NOT re-slice for a single row, or the reuse it sits on would
// rebuild every row of the window on every arrival.
const before = rows()[0];
draw([...many, {who:'assistant', text:'the new one'}]);
const heldStill = rows()[0] === before;
// Scrolling up asks for more of it.
const grew = reachUp();
draw([...many, {who:'assistant', text:'the new one'}]);
console.log(JSON.stringify({windowed, hasSpacer, heldStill, grew,
  last: (function deep(n) { return (n.textContent || '') + (n.children || []).map(deep).join(' '); })(last),
  after: rows().length}));`)
	num := func(k string) int { f, _ := got[k].(float64); return int(f) }
	if n := num("windowed"); n == 0 || n > 220 {
		t.Errorf("600 rows put %d in the page", n)
	}
	if num("hasSpacer") != 1 {
		t.Error("nothing stands in for the rows that are not there — the scrollbar is a lie")
	}
	// The tail is what is kept: the end of a conversation is where the reader is.
	if s, _ := got["last"].(string); !strings.Contains(s, "line 599") {
		t.Errorf("the window kept the wrong end: it ends at %q", s)
	}
	if got["heldStill"] != true {
		t.Error("one new row re-sliced the window, which rebuilds every row in it")
	}
	// A chunk, not a row: "more" has to be worth the scroll that asked for it, and one extra row
	// arriving in the same frame would satisfy a looser assertion than this without any reaching
	// having happened at all.
	if got["grew"] != true || num("after") < num("windowed")+50 {
		t.Errorf("scrolling up brought nothing back: %d then %d", num("windowed"), num("after"))
	}
}

// What this companion can call is asked of the daemon holding it, and an unanswered roster is not
// an empty one.
//
// The registry is assembled at startup from the config, the plugins that loaded and the MCP servers
// that answered. A console listing the built-ins would be describing a companion that does not
// exist — and would be wrong most confidently on the one whose plugin failed to load. So the screen
// shows what came back, and when nothing did it says the daemon did not say rather than drawing an
// empty list, which reads as "this agent has no tools".
func TestAnUnansweredToolRosterDoesNotReadAsNoTools(t *testing.T) {
	got := runPage(t, `[{"socket":"/s/a.sock","name":"a","live":true,"state":"working","session":"s_1"}]`,
		"?d=%2Fs%2Fa.sock&insp=tools", `
const words = n => (n.textContent || '') + (n.children || []).map(words).join(' ');
ROUTES['/tools'] = ['bash', 'read', 'sqlite_query'];
await drawTools({socket: '/s/a.sock'});
const answered = words(byId.agentdetail);
ROUTES['/tools'] = [];
await drawTools({socket: '/s/a.sock'});
const silent = words(byId.agentdetail);
console.log(JSON.stringify({answered, silent}));`)

	answered, _ := got["answered"].(string)
	// The roster that came over the socket, including the one no built-in list could have known.
	for _, want := range []string{"bash", "sqlite_query"} {
		if !strings.Contains(answered, want) {
			t.Errorf("the roster the daemon gave is missing %q:\n%s", want, answered)
		}
	}
	silent, _ := got["silent"].(string)
	if strings.TrimSpace(silent) == "" || !strings.Contains(silent, "did not say") {
		t.Errorf("a daemon that could not answer left the screen saying %q — which reads as a companion with no tools", silent)
	}
}

// The loop map keeps its alignment, and a fork says what it has changed.
//
// The map IS its spacing: the same text rendered as markdown is a paragraph of step numbers. And
// the comparison with where a fork came from is read from the log rather than from a flag the
// terminal happens to be holding, which is why a console arriving later can show it at all.
func TestTheLoopScreenKeepsTheMapAndTheForkComparison(t *testing.T) {
	got := runPage(t, `[{"socket":"/s/a.sock","name":"a","live":true,"state":"working","session":"s_1"}]`,
		"?d=%2Fs%2Fa.sock&insp=loop", `
ROUTES['/loop'] = {map: '1  plan\n2  work', origin: 's_parent', diff: '+ one line'};
await drawLoop({socket: '/s/a.sock'});
const pre = [];
const walk = n => { if (n.tag === 'pre') pre.push(n.textContent || ''); for (const k of n.children || []) walk(k); };
walk(byId.agentdetail);
const words = n => (n.textContent || '') + (n.children || []).map(words).join(' ');
console.log(JSON.stringify({pre, text: words(byId.agentdetail)}));`)

	pre, _ := got["pre"].([]any)
	if len(pre) != 2 {
		t.Fatalf("the map and the diff are the two things that must keep their line breaks; %d blocks kept theirs: %v", len(pre), pre)
	}
	if s, _ := pre[0].(string); !strings.Contains(s, "1  plan\n2  work") {
		t.Errorf("the map lost its shape: %q", s)
	}
	if s, _ := got["text"].(string); !strings.Contains(s, "s_parent") {
		t.Errorf("a forked session does not say where it came from:\n%s", s)
	}
}

// A language landing after the screen was drawn reaches the crumb you are standing under.
//
// The page is served with its English pack inlined so the first paint has words, and the chosen
// pack arrives a moment later. paint() repaints the labels written into the markup — and it
// repainted the FIRST crumb for exactly this reason — but the third one is written by render(),
// which does not run again. Measured on a Korean browser standing in past work: "companions /
// design / What it has done", every other label around it Korean.
func TestALateLanguageReachesTheCrumbYouAreStandingUnder(t *testing.T) {
	got := runPage(t, `[{"socket":"/s/a.sock","name":"a","live":true,"state":"working","session":"s_1"}]`,
		"?d=%2Fs%2Fa.sock&past=", `
// The pack lands after the screen has been drawn, which is the order a browser gets it in.
labels$.next({...L, 'field.history': '지나온 작업', 'nav.companions': '컴패니언'});
console.log(JSON.stringify({deep: crumbDeep.text, back: back.text}));`)

	if got["deep"] != "지나온 작업" {
		t.Errorf("the crumb kept the seed pack's wording: %q", got["deep"])
	}
	// The first crumb is the control: it was already repainted, and must still be.
	if got["back"] != "컴패니언" {
		t.Errorf("the first crumb reads %q", got["back"])
	}
}

// Nothing inside the report-format form submits it.
//
// The form is method="dialog" and a button in a form defaults to submit, so the ✕ beside a section
// closed the whole dialog — the row came off a form nobody could then save, and every other edit
// went with it. Reported as "it shuts before I can save". The same default was on the add control.
func TestNoControlInsideTheFormatFormClosesTheDialog(t *testing.T) {
	got := runPage(t, `[{"socket":"/s/a.sock","name":"a","live":true,"state":"waiting","session":"s_1"}]`, "?d=%2Fs%2Fa.sock", `
openFormat({socket:'/s/a.sock'}, {from:'workspace', sections:[{key:'tried', prompt:'what you ran'}]});
const kinds = [];
const walk = n => {
  const t = (n.tag || '').toLowerCase();
  if (t.includes('button')) kinds.push(t + ':' + (n.attrs.type || 'default'));
  for (const k of n.children || []) walk(k);
};
walk(byId.fmtForm);
console.log(JSON.stringify({kinds}));`)

	kinds, _ := got["kinds"].([]any)
	if len(kinds) < 2 {
		t.Fatalf("the form drew %d buttons; it has an add control and a remove control", len(kinds))
	}
	for _, k := range kinds {
		if s, _ := k.(string); !strings.HasSuffix(s, ":button") {
			t.Errorf("%s submits the form it is in, which closes the dialog", s)
		}
	}
}

// A handed-out job names somebody, and the name leads to them.
//
// The card is drawn for the case that matters — the companion named IS one this console can see —
// because that is the branch that draws a link, and the branch that draws a link is the one that
// threw: the row variable was called el, which shadows the page's own el(), so the moment the name
// became an anchor the whole card disappeared. Reported as "the handed-out card is gone".
func TestAHandedOutJobLinksToTheCompanionItNames(t *testing.T) {
	got := runPage(t, `[
      {"socket":"/s/a.sock","name":"design","live":true,"state":"working","session":"s_1"},
      {"socket":"/s/b.sock","name":"buttons","live":true,"state":"working","session":"s_2"}
    ]`, "?d=%2Fs%2Fa.sock", `
ROUTES['/handoffs'] = [
  {from: 'design', to: 'buttons', request: 'make the toggle read its state', state: 'working'},
  {from: 'design', to: 'nobody-here', request: 'a companion this console cannot see', state: 'idle'}];
await loadFleet();
await drawHandoffs({socket: '/s/a.sock'});
const box = byId.handoffs;
const names = [];
const walk = n => { if (String(n.className || '') === 'to') names.push({tag: n.tag, href: n.attrs.href || '', text: n.textContent});
                    for (const k of n.children || []) walk(k); };
walk(box);
console.log(JSON.stringify({hidden: box.hidden, names}));`)

	if got["hidden"] == true {
		t.Fatal("two handed-out jobs and the card is hidden")
	}
	names, _ := got["names"].([]any)
	if len(names) != 2 {
		t.Fatalf("the card drew %d names for two jobs: %v", len(names), names)
	}
	known, _ := names[0].(map[string]any)
	if tag, _ := known["tag"].(string); tag != "a" {
		t.Errorf("a companion this console can see is drawn as %q, with no way to it", tag)
	}
	if href, _ := known["href"].(string); !strings.Contains(href, "%2Fs%2Fb.sock") {
		t.Errorf("the link goes to %q rather than to that companion", href)
	}
	// And the one nobody here answers to stays plain: an anchor to nowhere is worse than a word.
	unknown, _ := names[1].(map[string]any)
	if tag, _ := unknown["tag"].(string); tag == "a" {
		t.Error("a name this console cannot resolve was drawn as a link anyway")
	}
}

// The model is a control, and choosing one tells the companion that holds the run.
//
// The terminal has had /model since it had slash commands; the console showed the name and left
// you to go and find a terminal. The list is the daemon's own — a console cannot know what backend
// that process is configured against — and the current model is always in the menu even when the
// backend no longer lists it, or the select would show something the companion is not on.
func TestTheModelIsAMenuThatTellsTheDaemon(t *testing.T) {
	got := runPage(t, `[{"socket":"/s/a.sock","name":"a","live":true,"state":"working","session":"s_1"}]`,
		"?d=%2Fs%2Fa.sock", `
ROUTES['/model'] = ['gpt-oss:120b-cloud', 'llama4:scout'];
ROUTES['/context'] = {model: 'qwen3-coder-next', used: 1000, window: 128000};
await loadFleet();
const grid = byId.detail;
// The field is built by the context draw, which is the half of the card that lands late.
await drawDetail({socket: '/s/a.sock', state: 'working', workdir: '/w', session: 's_1'});
// The roster is fetched without being awaited by the draw — it fills the menu when it lands, the
// same way it does in a browser — so the test drains the microtask queue rather than assuming.
for (let i = 0; i < 80; i++) await Promise.resolve();
// The model's menu, found by what is IN it rather than by a label: the field carries no floating
// label any more — the row it sits in already says the word — so the label was no longer a way to
// tell it from the approval menu beside it.
const sel = (function find(n) {
  if ((n.tag || '') === 'md-outlined-select' &&
      (n.children || []).some(o => String(o.value || '').includes('qwen3-coder-next'))) return n;
  for (const k of n.children || []) { const hit = find(k); if (hit) return hit; }
  return null;
})(grid);
const opts = sel ? sel.children.map(o => o.value) : [];
const before = sel ? sel.value : '';
// Choosing one posts it, and the value it posts is the one that was chosen.
sel.value = 'llama4:scout';
// The handler is async and the fake's setTimeout does not run anything, so the post is awaited
// through the promise the dispatch returns nothing about — a microtask drain is enough here.
sel.dispatchEvent({type: 'change'});
await Promise.resolve(); await Promise.resolve(); await Promise.resolve();
console.log(JSON.stringify({opts, before,
  posted: RENDERED.filter(r => r.method === 'POST').map(r => r.fetched + ' ' + r.body)}));`)

	opts, _ := got["opts"].([]any)
	var names []string
	for _, o := range opts {
		names = append(names, o.(string))
	}
	// The one it is on, then what the daemon offered.
	if len(names) != 3 || names[0] != "qwen3-coder-next" {
		t.Fatalf("the menu holds %v; the model it is on must be in it", names)
	}
	if got["before"] != "qwen3-coder-next" {
		t.Errorf("the menu shows %q while the companion runs qwen3-coder-next", got["before"])
	}
	posted, _ := got["posted"].([]any)
	last := ""
	if len(posted) > 0 {
		last, _ = posted[len(posted)-1].(string)
	}
	if !strings.Contains(last, "/model") || !strings.Contains(last, "llama4") {
		t.Errorf("choosing a model sent %q — the daemon holding the run was never told", last)
	}
}

// A row nothing has changed about is the row that is already on screen.
//
// The list redraws every three seconds. Rebuilt whole, every row's state layer and animation
// restarted four times a minute to say what the last one said — and anything attached to a row
// went with it: the field somebody is typing an answer into, the menu they have open, the hover
// they were aiming at. Only what CHANGED may be replaced.
func TestAnUnchangedRowSurvivesThePoll(t *testing.T) {
	got := runPage(t, `[
      {"socket":"/s/a.sock","name":"api","workdir":"/w/api","state":"waiting","live":true,
       "asking":"which branch?","askId":"q1","askKind":"question","task":"land it","steps":3,"idle":5},
      {"socket":"/s/b.sock","name":"docs","workdir":"/w/docs","state":"idle","live":true,
       "task":"done here","steps":1,"idle":30}
    ]`, "", rowsHelper+`
await loadFleet();
const first = rows().map(r => r);
// The same answer again: nothing about either companion has changed.
await loadFleet();
const again = rows().map(r => r);
// And now one of them starts working. Its row is rebuilt; the other's is not.
FLEET[1].state = 'working';
FLEET[1].task = 'writing the page';
await loadFleet();
const after = rows().map(r => r);
// And now ONLY the sentence changes — the state stays where it is. A signature that watches the
// state and not what the row says would keep the old sentence on screen forever, which is the
// failure this caching can produce and the one worth a test of its own.
FLEET[1].task = 'reading what it wrote';
await loadFleet();
const later = rows().map(r => r);
console.log(JSON.stringify({
  keptA: first[0] === again[0], keptB: first[1] === again[1],
  stillA: again[0] === after[0], changedB: again[1] !== after[1],
  text: after[1].text, saidLater: later[1].text}));`)

	if got["keptA"] != true || got["keptB"] != true {
		t.Error("a poll that reported no change rebuilt the rows anyway")
	}
	if got["stillA"] != true {
		t.Error("one companion changed and the OTHER one's row was thrown away with it")
	}
	if got["changedB"] != true {
		t.Error("a companion started working and its row was not redrawn")
	}
	if s, _ := got["text"].(string); !strings.Contains(s, "writing the page") {
		t.Errorf("the rebuilt row does not say what changed: %q", s)
	}
	// The sentence alone changing is enough to redraw. This is the half that catches a signature
	// which has fallen behind what the row draws.
	if s, _ := got["saidLater"].(string); !strings.Contains(s, "reading what it wrote") {
		t.Errorf("only the task changed and the row kept the old one: %q", s)
	}
}

// A language that lands late reaches the screen somebody is standing on.
//
// A deep screen is drawn once, by render(), and the pack arrives a moment after the first paint —
// so the decision screen, whose whole job is to be read, sat in the seeded English under a Korean
// crumb and beside a Korean rail. paint() redraws it now, unless there is something typed into it:
// a late pack must never take away what a person is in the middle of.
func TestALateLanguageReachesTheScreenYouAreOn(t *testing.T) {
	fleet := `[{"socket":"/s/a.sock","name":"api","workdir":"/w/api","state":"waiting","live":true,
       "asking":"which branch?","askId":"q1","askKind":"question","session":"s_1",
       "report":[{"key":"tried","text":"built both"}],"task":"land it","steps":1,"idle":3}]`

	got := runPage(t, fleet, "?d=%2Fs%2Fa.sock&ask=q1", `
await loadFleet();
labels$.next({...L, 'ask.deciding': '결정', 'ask.your_answer': '당신의 답', 'action.answer': '응답'});
for (let i = 0; i < 80; i++) await Promise.resolve();
const words = n => (n.textContent || '') + (n.children || []).map(words).join(' ');
const before = words(byId.agentdetail);
// And with something typed, the same pack change leaves the screen alone.
const field = byId.agentdetail.find('md-outlined-text-field')[0];
if (field) field.value = '반쯤 쓴 답';
labels$.next({...L, 'ask.your_answer': 'CHANGED-AGAIN'});
for (let i = 0; i < 80; i++) await Promise.resolve();
console.log(JSON.stringify({after: words(byId.agentdetail), kept: field ? field.value : ''}));`)

	after, _ := got["after"].(string)
	if !strings.Contains(after, "당신의 답") {
		t.Errorf("the screen kept the seeded wording after the pack landed:\n%s", after)
	}
	if strings.Contains(after, "CHANGED-AGAIN") {
		t.Error("a pack landing while somebody was typing redrew the screen under them")
	}
	if got["kept"] != "반쯤 쓴 답" {
		t.Errorf("what was typed did not survive: %q", got["kept"])
	}
}

// The composer reaches the conversation on screen, or it does not reach at all.
//
// One predicate covers three situations that used to be three behaviours, one of them silent: the
// companion's own page (send), a session it is not in (ask, move, then send), and — the quiet one
// — a page whose companion moved away while somebody was reading it, where the box looked live and
// typed into a conversation that was no longer on screen.
func TestTheComposerSaysWhichConversationItWouldReach(t *testing.T) {
	fleet := `[{"socket":"/s/a.sock","name":"api","workdir":"/w/api","state":"idle","live":true,
       "session":"s_now","idle":9}]`
	probe := `
// Delegating rather than replacing: the harness's own fetch is what serves the fleet this test was
// given, and an override that answered everything left the page with no companions at all.
const base = globalThis.fetch;
globalThis.fetch = async (p, o) => {
  if (String(p).split('?')[0] === '/transcript') {
    return {ok: true, json: async () => [{who: 'user', text: 'last week'}]};
  }
  return base(p, o);
};
await loadFleet();
render();
console.log(JSON.stringify({
  moving: movingTo(), composerHidden: byId.f.hidden,
  fieldOff: byId.t.attrs['disabled'] !== undefined,
  label: byId.t.attrs['label'] || '',
}));
`
	// On the conversation the companion IS in: nothing to move, and the ordinary label.
	same := runPage(t, fleet, "?d=%2Fs%2Fa.sock&past=s_now", probe)
	if same["moving"] != "" {
		t.Errorf("it offered to move to the conversation the companion is already in: %v", same["moving"])
	}
	if same["composerHidden"] == true {
		t.Error("the composer is hidden on the conversation the companion is in")
	}

	// On another of its conversations: the composer stays, and says where it would go.
	other := runPage(t, fleet, "?d=%2Fs%2Fa.sock&past=s_older", probe)
	if other["moving"] != "s_older" {
		t.Errorf("moving to %v — a session screen must offer the move", other["moving"])
	}
	if other["composerHidden"] == true {
		t.Error("the composer was taken away on a session screen — the conversation IS on screen there")
	}
	if other["fieldOff"] == true {
		t.Error("the field is disabled while the companion is idle, which is when a move is allowed")
	}
	if !strings.Contains(other["label"].(string), "s_older") {
		t.Errorf("the field does not name the conversation it would reach: %q", other["label"])
	}
}

// A companion mid-turn cannot be moved, so the box does not take a sentence it cannot deliver.
func TestTheComposerRefusesToMoveACompanionMidTurn(t *testing.T) {
	got := runPage(t, `[{"socket":"/s/a.sock","name":"api","workdir":"/w/api","state":"working","live":true,
       "session":"s_now","task":"building","idle":0}]`,
		"?d=%2Fs%2Fa.sock&past=s_older", `
await loadFleet();
render();
console.log(JSON.stringify({
  fieldOff: byId.t.attrs['disabled'] !== undefined,
  sendOff: byId.send.attrs['disabled'] !== undefined,
  why: byId.cnote.textContent || '',
}));
`)
	if got["fieldOff"] != true || got["sendOff"] != true {
		t.Error("the composer accepts work for a companion that cannot be moved yet")
	}
	if got["why"] == "" {
		t.Error("it refuses without saying why, which reads as a broken page")
	}
}

// A board card is one conversation, and pressing it opens that one.
//
// The card knows its session — it has an id, a title, a start and an end — and the link used to
// throw it away and land on whatever that companion is doing now.
func TestABoardCardOpensTheConversationItDraws(t *testing.T) {
	got := runPage(t, `[{"socket":"/s/a.sock","name":"api","workdir":"/w/api","state":"idle","live":true,"session":"s1","idle":9}]`,
		"?v=board", `
const now = new Date();
globalThis.fetch = async (p) => {
  const u = String(p).split('?')[0];
  if (u === '/history') return {ok: true, json: async () => [
    {id: 's_older', title: 'the billing refactor', started: now.toISOString(), ended: now.toISOString(), ago: 60},
  ]};
  if (u === '/fleet') return {ok: true, json: async () => [
    {socket: '/s/a.sock', name: 'api', workdir: '/w/api', state: 'idle', live: true, session: 's1', idle: 9},
  ]};
  return {ok: true, json: async () => []};
};
await loadBoard();
const links = byId.board.find('a').filter(a => String(a.className).includes('wwhat'));
console.log(JSON.stringify({n: links.length, href: links.length ? links[0].attrs['href'] : ''}));
`)
	if got["n"].(float64) == 0 {
		t.Fatal("no card was drawn")
	}
	href, _ := got["href"].(string)
	if !strings.Contains(href, "past=s_older") {
		t.Errorf("the card links to %q — it drops the one conversation it knows about, so a middle "+
			"click and a copied url land somewhere else", href)
	}
}

// A question that came with a list of answers is drawn as that list.
//
// The agent decided the answer is one of these, and a free-text box asks somebody to retype an
// option they cannot see — the prompt is transient and belongs to the daemon's bus, so the console
// only knows the options if the fleet row carries them. It did not, so the console drew a text
// field for every multiple choice and the person had to guess the exact wording.
func TestAQuestionWithOptionsIsAnsweredByPressingOne(t *testing.T) {
	got := runPage(t, `[{"socket":"/s/a.sock","name":"api","workdir":"/w/api","state":"waiting","live":true,
       "asking":"which surface should the empty state sit on?","askId":"q1#1","askKind":"question",
       "askOptions":["surface","surface-container-low","surface-dim"],"idle":5}]`, "", rowsHelper+`
await loadFleet();
const box = rows()[0].find('div').filter(d => String(d.className).includes('answer'))[0];
const buttons = box ? box.find('md-outlined-button') : [];
console.log(JSON.stringify({
  buttons: buttons.map(b => b.textContent),
  fields: box ? box.find('md-outlined-text-field').length : -1,
}));
`)
	want := []any{"surface", "surface-container-low", "surface-dim"}
	got2, _ := got["buttons"].([]any)
	if len(got2) != len(want) {
		t.Fatalf("drew %v, wanted a button per option (%v)", got2, want)
	}
	for i := range want {
		if got2[i] != want[i] {
			t.Errorf("option %d is %q, wanted %q — the answer travels as text, so the wording is the answer",
				i, got2[i], want[i])
		}
	}
	if got["fields"].(float64) != 0 {
		t.Error("a text field was drawn beside the options, which asks for a fourth answer to a closed question")
	}
}

// A question with no list keeps the box: not every question is a choice.
func TestAnOpenQuestionStillGetsAField(t *testing.T) {
	got := runPage(t, `[{"socket":"/s/a.sock","name":"api","workdir":"/w/api","state":"waiting","live":true,
       "asking":"what should the empty state say?","askId":"q1#1","askKind":"question","idle":5}]`, "", rowsHelper+`
await loadFleet();
const box = rows()[0].find('div').filter(d => String(d.className).includes('answer'))[0];
console.log(JSON.stringify({fields: box ? box.find('md-outlined-text-field').length : -1}));
`)
	if got["fields"].(float64) != 1 {
		t.Errorf("an open question drew %v fields", got["fields"])
	}
}

// The page leaves out what this person would only be refused for.
//
// Hiding is NOT the check — the server refuses either way, and a page that confused the two is how
// a console somebody called read-only turns out to have been writable by anybody who opened the
// network tab. What it buys is the other half: a composer for somebody who may not prompt is a box
// that swallows what they typed, and a button that only ever answers 403 teaches people to
// distrust the screen.
func TestThePageLeavesOutWhatThisPersonMayNotDo(t *testing.T) {
	probe := func(can string) string {
		return `
const base = globalThis.fetch;
globalThis.fetch = async (p, o) => {
  if (String(p).split('?')[0] === '/me') return {ok: true, json: async () => ({can: ` + can + `})};
  return base(p, o);
};
await loadMe();
await loadFleet();
render();
console.log(JSON.stringify({
  stopHidden: byId.stop.hidden === true,
  fieldOff: byId.t.attrs['disabled'] !== undefined,
  label: byId.t.attrs['label'] || '',
  empty: byId.fleet.find('div').filter(d => String(d.className).includes('empty')).length,
}));
`
	}
	fleet := `[{"socket":"/s/a.sock","name":"api","workdir":"/w/api","state":"idle","live":true,"session":"s1","idle":9}]`

	// A viewer: no stopping, no typing, and the field says which of the two it is.
	viewer := runPage(t, fleet, "?d=%2Fs%2Fa.sock", probe(`['read']`))
	if viewer["stopHidden"] != true {
		t.Error("a viewer is offered the button that ends somebody else's turn")
	}
	if viewer["fieldOff"] != true {
		t.Error("a viewer gets a live composer — a box that swallows what they typed")
	}
	if !strings.Contains(viewer["label"].(string), "may not") {
		t.Errorf("the field does not say why it is shut: %q", viewer["label"])
	}

	// A responder: stopping and answering are the same permission, so the button stays.
	responder := runPage(t, fleet, "?d=%2Fs%2Fa.sock", probe(`['read', 'answer']`))
	if responder["stopHidden"] == true {
		t.Error("somebody who may answer cannot stop a turn, which is the same act")
	}

	// An operator, and the console with nobody configured, are the same screen.
	op := runPage(t, fleet, "?d=%2Fs%2Fa.sock", probe(`['read', 'answer', 'prompt']`))
	if op["fieldOff"] == true {
		t.Error("somebody who may prompt was given a dead composer")
	}
}

// Somebody a gateway let in but nobody gave a role to is told that, not shown an empty fleet.
//
// The same blank screen was two different facts — "no companions yet" and "you may not see them" —
// and the first sends somebody off to start a daemon that is already running.
func TestAPersonWithNoRoleIsToldSoRatherThanShownAnEmptyFleet(t *testing.T) {
	got := runPage(t, `[]`, "", `
const base = globalThis.fetch;
globalThis.fetch = async (p, o) => {
  if (String(p).split('?')[0] === '/me') return {ok: true, json: async () => ({can: []})};
  return base(p, o);
};
await loadMe();
await loadFleet();
const empties = byId.fleet.find('div').filter(d => String(d.className).includes('empty'));
console.log(JSON.stringify({text: empties.length ? empties[0].innerHTML || empties[0].textContent : ''}));
`)
	said, _ := got["text"].(string)
	if said == "" {
		t.Fatal("nothing was drawn at all")
	}
	if strings.Contains(said, "No companions") || strings.Contains(said, "magi -daemon") {
		t.Errorf("they were told the fleet is empty: %q", said)
	}
}

// A question with a list is answered by pressing one on the agent's page too.
//
// The fleet row drew the buttons and the agent's own page did not: a question turned the composer
// into an answer field and that was the whole offer. The options had reached the page — the report
// above argues about them, since the default contract asks what each option costs and which one
// the agent leans to — so the person was reading the words and retyping one of them, exactly, into
// a box under prose that had scrolled away.
func TestThePromptBarOffersTheOptionsItWasGiven(t *testing.T) {
	got := runPage(t, `[]`, "?d=%2Fs%2Fa.sock", `
drawPrompt({socket:'/s/a.sock', state:'waiting', asking:'which surface?', askId:'q1',
            askKind:'question', askIndex:1, askTotal:1,
            askOptions:['surface', 'surface-dim']});
const buttons = byId.prompt.find('md-outlined-button').map(b => b.textContent);
console.log(JSON.stringify({
  buttons: buttons,
  // The composer stays the way to say something that is not on the list, so there must be exactly
  // one field in play: the one at the bottom of the page.
  fields: byId.prompt.find('md-outlined-text-field').length,
  note: byId.cnote.textContent,
}));`)

	buttons, _ := got["buttons"].([]any)
	if len(buttons) != 2 || buttons[0] != "surface" || buttons[1] != "surface-dim" {
		t.Errorf("the bar drew %v, wanted a button per option — the answer travels as text, so the "+
			"wording is the answer", buttons)
	}
	if got["fields"].(float64) != 0 {
		t.Error("a second text field was drawn in the bar, over the composer that is already one")
	}
	if note, _ := got["note"].(string); !strings.Contains(note, "Pick one") {
		t.Errorf("the composer says %q — it does not say the list above is the offer", note)
	}
}

// And the list is not the only thing that can be said.
//
// The answer travels as text, so something outside the list is deliverable and is sometimes the
// true answer. On the ask screen there is no composer to say it in — a deep screen hides it — so
// the way to say "neither" has to be here, behind a press rather than beside the options: a field
// standing open next to three buttons asks "type something" as loudly as they ask "pick one".
func TestTheAskScreenCanSayNoneOfThose(t *testing.T) {
	got := runPage(t, `[]`, "?d=%2Fs%2Fa.sock", `
const box = answerBox({socket:'/s/a.sock', state:'waiting', asking:'which surface?', askId:'q1',
                       askKind:'question', askOptions:['surface', 'surface-dim']}, true);
const before = box.find('md-outlined-text-field').length;
const more = box.find('md-text-button')[0];
if (more) more.onclick({preventDefault(){}, stopPropagation(){}});
console.log(JSON.stringify({
  before: before,
  offered: !!more,
  after: box.find('md-outlined-text-field').length,
  stillOptions: box.find('md-outlined-button').length,
  // Where it opened, not only that it opened: the field takes the place the button stood in, so
  // it lands under the options rather than at the end of whatever else the box holds.
  order: box.children.map(k => k.tag).join(','),
}));`)

	if got["offered"] != true {
		t.Fatal("the ask screen offers no way to answer anything but the list, and hides the composer")
	}
	if got["before"].(float64) != 0 {
		t.Error("the field stands open beside the options, which asks to type as loudly as it asks to pick")
	}
	if got["after"].(float64) != 1 {
		t.Errorf("pressing it opened %v fields", got["after"])
	}
	if got["stillOptions"].(float64) != 2 {
		t.Errorf("opening the field took the options away (%v left)", got["stillOptions"])
	}
	if order, _ := got["order"].(string); order != "md-outlined-button,md-outlined-button,md-outlined-text-field,md-filled-button" {
		t.Errorf("the field opened at %q — it takes the place the button stood in, which is under "+
			"the options and not at the end of the box", order)
	}
}

// The stand-in replaces a node where it stood.
//
// About the fake DOM rather than the page: the page's one caller happens to put the control it
// replaces last, so a replaceWith that appended instead would look identical there and be wrong
// the first time somebody puts anything after it. A stand-in is only worth having where it agrees
// with the DOM before the page depends on the difference.
func TestTheFakeDOMReplacesInPlace(t *testing.T) {
	got := runPage(t, `[]`, "", `
const box = document.createElement('div');
const a = document.createElement('span'); a.textContent = 'a';
const mid = document.createElement('span'); mid.textContent = 'mid';
const z = document.createElement('span'); z.textContent = 'z';
box.append(a, mid, z);
const one = document.createElement('em'); one.textContent = '1';
const two = document.createElement('em'); two.textContent = '2';
mid.replaceWith(one, two);
console.log(JSON.stringify({
  order: box.children.map(k => k.textContent).join(','),
  orphaned: mid.parentNode === null,
  adopted: one.parentNode === box,
}));`)

	if order, _ := got["order"].(string); order != "a,1,2,z" {
		t.Errorf("replaceWith left %q, wanted a,1,2,z — the DOM puts the new nodes where the old one was", order)
	}
	if got["orphaned"] != true {
		t.Error("the replaced node kept its parent, so a page could still reach what it took out")
	}
	if got["adopted"] != true {
		t.Error("the new nodes were not adopted, so asking one which box it is in answers wrong")
	}
}

// A reader at the bottom stays at the bottom when the dock grows under them.
//
// The dock is fixed, so its own height moves nothing; what moves is the padding the page reserves
// for it. A question arriving — with three options to press, now that it draws them — makes the
// document taller under somebody who was reading its last line, and the line they were reading
// goes under the bar. The transcript's redraw anchors itself for exactly this reason; the dock was
// the other thing that changes height and did not.
func TestGrowingTheDockKeepsAReaderAtTheBottom(t *testing.T) {
	got := runPage(t, `[]`, "?d=%2Fs%2Fa.sock", `
// At the bottom: the window's height covers the whole document from where it sits.
window.scrollY = 0; window.innerHeight = 800;
document.body.offsetHeight = 400; document.body.scrollHeight = 400;
window.scrolledTo = null;
byId.dock.offsetHeight = 260;                       // a question with options just opened
measureDock();
const wasStuck = window.scrolledTo;

// And a reader who had scrolled up is left where they are: the page grew below them, off screen.
window.scrollY = 0; window.innerHeight = 200;
document.body.offsetHeight = 4000; document.body.scrollHeight = 4000;
window.scrolledTo = null;
byId.dock.offsetHeight = 320;
measureDock();
console.log(JSON.stringify({atBottom: wasStuck, readingAbove: window.scrolledTo}));`)

	if got["atBottom"] == nil {
		t.Error("the page grew under a reader at the bottom and left them above it, reading the bar")
	} else if got["atBottom"].(float64) != 400 {
		t.Errorf("it scrolled to %v, not to the foot of the document", got["atBottom"])
	}
	if got["readingAbove"] != nil {
		t.Errorf("it threw a reader who was mid-transcript to %v", got["readingAbove"])
	}
}

// A card opens the conversation on the companion that had it.
//
// Cards are grouped into a lane by team, or by name when there is no team, and the owner was found
// again inside the lane BY NAME. A companion is named after its directory, so two workspaces whose
// last path segment matches — ~/work/api and ~/side/api, or the same project checked out twice —
// are two companions called api, in one lane, and every card in it resolved to the first.
//
// What that costs is not a wrong link. It is a session id handed to a daemon that does not own it:
// the move is refused, correctly, and what the console had offered was a conversation it could not
// open on the companion it named.
func TestABoardCardAddressesTheCompanionThatOwnsIt(t *testing.T) {
	fleet := `[{"socket":"/s/one.sock","name":"api","workdir":"/w/work/api","state":"idle","live":true,"idle":3},
	           {"socket":"/s/two.sock","name":"api","workdir":"/w/side/api","state":"idle","live":true,"idle":4}]`
	got := runPage(t, fleet, "", `
const base = globalThis.fetch;
globalThis.fetch = async (p, o) => {
  const u = String(p);
  if (u.startsWith('/history')) {
    // Each companion has one conversation, today, and they are not the same conversation.
    const which = u.includes('two.sock') ? 'two' : 'one';
    return {ok: true, json: async () => [{
      id: 's_' + which, title: 'the ' + which + ' work',
      started: new Date().toISOString(), ended: new Date().toISOString(),
    }]};
  }
  return base(p, o);
};
history.pushState({}, '', '/?v=board');
await loadFleet();
render();
await loadBoard();
const cards = byId.board.find('a').filter(x => String(x.className).includes('wwhat'));
console.log(JSON.stringify({hrefs: cards.map(x => x.attrs['href'] || x.href || '')}));`)

	hrefs, _ := got["hrefs"].([]any)
	if len(hrefs) != 2 {
		t.Fatalf("the board drew %d cards, wanted one per companion: %v", len(hrefs), hrefs)
	}
	for _, h := range hrefs {
		u, _ := h.(string)
		// The pairing is what matters: one.sock's conversation must not be offered on two.sock.
		if strings.Contains(u, "s_one") && !strings.Contains(u, "one.sock") {
			t.Errorf("a card for the first companion's conversation addresses %q", u)
		}
		if strings.Contains(u, "s_two") && !strings.Contains(u, "two.sock") {
			t.Errorf("a card for the second companion's conversation addresses %q — its session "+
				"belongs to a workspace that daemon has never seen", u)
		}
	}
}

// A move the companion refused is not followed by the send.
//
// post() answers with the REASON it failed and an empty string when it did not. The move-and-send
// path compared that against false, which a string never is, so a refused move — mid-turn, or a
// conversation belonging to another workspace — went straight on to submit. The words landed in
// whatever conversation the companion was actually in, which is by definition the one nobody was
// looking at, while the refusal went past as a toast.
func TestARefusedMoveDoesNotSendAnyway(t *testing.T) {
	fleet := `[{"socket":"/s/a.sock","name":"api","workdir":"/w/api","state":"idle","live":true,
       "session":"s_now","idle":9}]`
	got := runPage(t, fleet, "?d=%2Fs%2Fa.sock&past=s_older", `
const base = globalThis.fetch;
const asked = [];
globalThis.fetch = async (p, o) => {
  const u = String(p), path = u.split('?')[0];
  if (path === '/transcript') return {ok: true, json: async () => [{who: 'user', text: 'last week'}]};
  if (path === '/resume') { asked.push('resume'); return {ok: false, text: async () => 's_older is not a conversation of this workspace'}; }
  if (path === '/submit') { asked.push('submit'); return {ok: true, text: async () => ''}; }
  return base(p, o);
};
await loadFleet();
render();
byId.t.value = 'the words';
byId.f.onsubmit({preventDefault(){}});
// The move is behind a confirmation, which is where the send is decided.
byId.stopGo.onclick();
// Timers are recorded rather than run here, so the wait is the microtask queue: the flow is a
// fetch, its body, and a then.
for (let i = 0; i < 8; i++) await Promise.resolve();
console.log(JSON.stringify({asked: asked, kept: byId.t.value}));`)

	asked, _ := got["asked"].([]any)
	if len(asked) == 0 || asked[0] != "resume" {
		t.Fatalf("the flow did not try to move first: %v", asked)
	}
	for _, a := range asked[1:] {
		if a == "submit" {
			t.Error("it sent the message after the companion refused to move — into the conversation " +
				"it was already in, which is the one not on screen")
		}
	}
	if got["kept"] != "the words" {
		t.Errorf("what was typed is %q; a send that did not happen must leave it in the box", got["kept"])
	}
}

// The page follows the last line even when the row that arrived grows after it lands.
//
// A row is content-visibility:auto with an intrinsic size of 3.5rem, so one that has just been
// appended reports THAT height until the browser lays it out — a frame later, because it is at the
// bottom of the viewport. Scrolling once therefore aims at a document that is about to get taller,
// and the reader is left short of the end by the difference. The rows this bites are the tall ones:
// reported from a live console as "the request is there and the answer under it is not", which is
// exactly a tool call, its output and its fold arriving as one 3.5rem guess.
func TestTheBottomIsReachedAfterALateGrowingRow(t *testing.T) {
	got := runPage(t, `[]`, "?d=%2Fs%2Fa.sock", `
window.scrollY = 0; window.innerHeight = 800;
document.body.offsetHeight = 400; document.body.scrollHeight = 400;
const went = [];
window.scrollTo = (x, y) => {
  went.push(y);
  // The browser lays the new row out after the first scroll, and the page gets taller.
  if (went.length === 1) document.body.scrollHeight = 2400;
};
toBottom();
console.log(JSON.stringify({went: went}));`)

	went, _ := got["went"].([]any)
	if len(went) < 2 {
		t.Fatalf("it scrolled %v and stopped — one pass cannot see a row that grows after it", went)
	}
	if last := went[len(went)-1]; last != float64(2400) {
		t.Errorf("it ended at %v, and the document is 2400 tall by then", last)
	}
}

// And once more when the typeface lands.
//
// The transcript is set in a web font with font-display:swap, so every line is measured in a
// fallback face first and re-measured when the real one arrives. On a cold load that happens after
// the first draw: the page changes height under somebody who was already at the foot of it. Same
// failure as the late-growing row, from the other direction, and the same one line fixes it.
func TestTheBottomIsReachedAgainWhenTheFontsLand(t *testing.T) {
	got := runPage(t, `[]`, "?d=%2Fs%2Fa.sock", `
window.scrollY = 0; window.innerHeight = 800;
document.body.offsetHeight = 400; document.body.scrollHeight = 400;
// After the page has finished starting up, so what is measured is the FONTS landing and not
// something else that scrolled on the way in.
for (let i = 0; i < 8; i++) await Promise.resolve();
window.scrolledTo = null;
document.fonts._land();
for (let i = 0; i < 8; i++) await Promise.resolve();
console.log(JSON.stringify({after: window.scrolledTo}));`)

	if got["after"] == nil {
		t.Error("the fonts landed, the lines re-measured, and the reader was left above the end")
	}
}

// When the question goes, the composer stops being an answer box.
//
// One field in two roles, and the role was set when the question appeared and cleared nowhere.
// Interrupt the turn — or let somebody answer from another console — and the box still said "your
// answer" and still posted to /answer, with the call id of a question that no longer exists. A
// fresh request typed there went nowhere, which reads as the page having stopped listening.
func TestTheComposerLeavesAnswerModeWhenTheQuestionDoes(t *testing.T) {
	got := runPage(t, `[]`, "?d=%2Fs%2Fa.sock", `
drawPrompt({socket:'/s/a.sock', state:'waiting', asking:'which one?', askId:'q1',
            askKind:'question', askIndex:1, askTotal:1});
const asking = {answering: !!answering, label: byId.t.attrs['label'] || ''};
// The turn was interrupted: the companion is idle and there is no question any more.
drawPrompt({socket:'/s/a.sock', state:'idle'});
console.log(JSON.stringify({
  asking: asking,
  after: {answering: !!answering, label: byId.t.attrs['label'] || '', hidden: byId.prompt.hidden},
}));`)

	asking, _ := got["asking"].(map[string]any)
	if asking["answering"] != true {
		t.Fatal("the composer never entered answer mode, so this test proves nothing")
	}
	after, _ := got["after"].(map[string]any)
	if after["answering"] == true {
		t.Error("the question is gone and the composer is still answering it — what gets typed " +
			"there posts to a call id that no longer exists")
	}
	if after["label"] == asking["label"] {
		t.Errorf("the field still says %q", after["label"])
	}
	if after["hidden"] != true {
		t.Error("the prompt bar stayed up")
	}
}

// A tool call's arguments are read as the named object they are.
//
// They were printed as the JSON they arrived in, which is the form they have because a wire needs
// one. A call to write a file is its path and its whole body escaped onto a single line, and the
// thing somebody opened the fold to read is in there behind \n and \". The shape is not a mystery
// — it is a small object whose keys are the tool's own parameter names.
func TestToolArgumentsAreDrawnAsFields(t *testing.T) {
	got := runPage(t, `[]`, "?d=%2Fs%2Fa.sock", `
draw([{who:'tool', tool:'write', ok:true,
       args:'{"path":"cmd/main.go","text":"package main\\n\\nfunc main() {}\\n"}'}]);
const fold = byId.log.find('details')[0];
const keys = fold.find('div').filter(d => String(d.className).includes('argk')).map(d => d.textContent);
const vals = fold.find('pre').map(p => ({cls: String(p.className || ''), text: p.textContent}));
console.log(JSON.stringify({keys: keys, vals: vals}));`)

	keys, _ := got["keys"].([]any)
	if len(keys) != 2 || keys[0] != "path" || keys[1] != "text" {
		t.Fatalf("the arguments were not named: %v", keys)
	}
	vals, _ := got["vals"].([]any)
	var body string
	for _, v := range vals {
		m, _ := v.(map[string]any)
		s, _ := m["text"].(string)
		if strings.Contains(s, `{"path"`) {
			t.Errorf("the raw JSON is still being printed: %q", s)
		}
		if strings.Contains(s, "package main") {
			body = s
			if cls, _ := m["cls"].(string); !strings.Contains(cls, "tall") {
				t.Errorf("a value with newlines shares the value column (%q)", cls)
			}
		}
	}
	if body == "" {
		t.Error("the file body is not in the fold at all")
	}
	if strings.Contains(body, `\n`) {
		t.Errorf("the body kept its escapes: %q", body)
	}
}

// And anything that is not a named object is left exactly as it was.
func TestToolOutputThatIsNotAnObjectIsLeftAlone(t *testing.T) {
	got := runPage(t, `[]`, "?d=%2Fs%2Fa.sock", `
draw([{who:'tool', tool:'bash', ok:true, args:'go test ./...', out:'ok  magi 1.2s'}]);
const fold = byId.log.find('details')[0];
console.log(JSON.stringify({
  fields: fold.find('div').filter(d => String(d.className).includes('argk')).length,
  pres: fold.find('pre').map(p => p.textContent),
}));`)

	if got["fields"].(float64) != 0 {
		t.Error("a plain command line was taken apart as though it were an object")
	}
	pres, _ := got["pres"].([]any)
	if len(pres) != 2 || pres[0] != "go test ./..." || pres[1] != "ok  magi 1.2s" {
		t.Errorf("the blocks changed: %v", pres)
	}
}

// The console can grant a tool past the end of the session, which only the terminal could.
//
// The permission modal has had four answers in the terminal since it had any, and the web drew
// three: the missing one is the only one that OUTLIVES the run, so somebody working from a browser
// had to go and find a terminal to stop being asked the same thing tomorrow.
func TestTheConsoleOffersTheAnswerThatSurvivesARestart(t *testing.T) {
	got := runPage(t, `[{"socket":"/s/a.sock","name":"api","workdir":"/w/api","state":"waiting","live":true,
       "asking":"permission: bash: rm -rf build","askId":"c1","askKind":"permission","idle":2}]`, "", rowsHelper+`
await loadFleet();
const box = rows()[0].find('div').filter(d => String(d.className).includes('answer'))[0];
const sent = [];
const base = globalThis.fetch;
globalThis.fetch = async (p, o) => {
  if (String(p).split('?')[0] === '/answer') { sent.push(String(o && o.body)); return {ok: true, text: async () => ''}; }
  return base(p, o);
};
// The third button is the one that lasts; press it and see what travels.
box.find('md-filled-tonal-button')[2].onclick({preventDefault(){}, stopPropagation(){}});
for (let i = 0; i < 6; i++) await Promise.resolve();
console.log(JSON.stringify({sent: sent}));`)

	sent, _ := got["sent"].([]any)
	if len(sent) != 1 {
		t.Fatalf("pressing it sent %v", sent)
	}
	body, _ := sent[0].(string)
	if !strings.Contains(body, "text=persist") {
		t.Errorf("it sent %q; the decision that survives a restart is persist", body)
	}
	if !strings.Contains(body, "kind=permission") || !strings.Contains(body, "call=c1") {
		t.Errorf("it did not answer the call it was drawn for: %q", body)
	}
}

// The people screen draws who may use this console, and does not draw at all for anybody else.
//
// Two halves of the same rule: the markup carries data-may="admin" so the screen is not offered to
// somebody who would be refused, and the server refuses regardless — hiding is for the person who
// would otherwise press a control that answers 403.
func TestTheAccessScreenIsDrawnForAnAdminAndHiddenFromEverybodyElse(t *testing.T) {
	fleet := `[]`
	page := `
const base = globalThis.fetch;
globalThis.fetch = async (p, o) => {
  const path = String(p).split('?')[0];
  if (path === '/me') return {ok: true, json: async () => ({can: CAN})};
  if (path === '/access') return {ok: true, json: async () => ({configured: true,
    instance: {who: 'you@studio', configDir: '/Users/you/.config/magi'},
    groups: [{who: 'platform', role: 'operator', can: ['read','admin'], companions: ['api']}],
    roles: [
      {name: 'operator', can: ['read','answer','prompt','curate','configure','admin','shell']},
      {name: 'viewer', can: ['read']}],
    people: [
      {who: 'kim@corp.com', role: 'operator', can: ['read','answer','prompt','curate','configure','admin','shell'], me: true},
      {who: 'lee@corp.com', role: 'viewer', companions: ['docs'], can: ['read']}]})};
  return base(p, o);
};
await loadMe();
history.pushState({}, '', '/?v=access');
// Through render(), which is what a person arriving at the address gets — including the decision
// about whether to ask the server at all.
render();
for (let i = 0; i < 8; i++) await Promise.resolve();
const names = byId.access.find('div').filter(d => String(d.className).split(' ').includes('who')).map(d => d.textContent);
console.log(JSON.stringify({
  hidden: byId.access.hidden,
  names: names,
  roles: byId.access.find('md-outlined-select').length,
  // The way IN, which is the part a person hits first and the part that was missing: the screen
  // existed and nothing on the page led to it.
  railHref: byId.railAccess.getAttribute('href'),
  railHidden: !!byId.railAccess.hidden,
  railWord: byId.railAccess.querySelector('.lbl').textContent,
  atFoot: byId.railFoot.children.includes(byId.railAccess),
  // Whose list this is. Without it the roster reads as the fleet's, and the fleet spans machines.
  whose: byId.access.find('div').filter(d => String(d.className).includes('instance'))
                    .map(d => d.children.map(k => k.textContent).join(' ')).join(''),
  // The anatomy, which is what says which line is which: a subheading over each half, and per row
  // a headline, the capabilities under it, and the scope under that.
  heads: byId.access.find('h3').map(h => String(h.className) + ':' + h.textContent),
  parts: byId.access.find('div').map(d => String(d.className)).filter(c => c),
  // Chips inside the ROWS, not only in the legend: a joined string of the same words passes any
  // check that only asks whether the class appears somewhere on the screen.
  // Chips are md-filter-chip now — a component, and a control that answers "who else may do this?"
  // — so they are counted as elements rather than as divs.
  rowchips: byId.access.find('md-filter-chip').length,
  scopechips: byId.access.find('md-input-chip').length,
}));
`
	admin := runPage(t, fleet, "?v=access", strings.Replace(page, "CAN", `['read','admin']`, 1))
	if admin["hidden"] == true {
		t.Fatal("an admin cannot see the screen")
	}
	names, _ := admin["names"].([]any)
	// The group is a row of the roster too, and the first one: on a console wired to a directory it
	// IS the roster and the people under it are the exceptions.
	if len(names) != 3 || names[0] != "@platform" {
		t.Errorf("the groups are not drawn above the people: %v", names)
	}
	names = names[1:]
	if len(names) != 2 || names[0] != "kim@corp.com" {
		t.Errorf("the list drew %v", names)
	}
	if admin["roles"].(float64) != 2 {
		t.Errorf("%v role pickers for two people", admin["roles"])
	}
	// A screen with no door is a screen nobody finds. The rail carries the way in, at its foot and
	// away from the two destinations somebody lives on, with a word on it rather than a bare icon.
	if admin["railHref"] != "/?v=access" {
		t.Errorf("the rail leads to %v", admin["railHref"])
	}
	if admin["railHidden"] == true {
		t.Error("an admin has no way to the access screen from the navigation")
	}
	if admin["atFoot"] != true {
		t.Error("the way to the access screen is not at the foot of the rail")
	}
	if w, _ := admin["railWord"].(string); strings.TrimSpace(w) == "" {
		t.Error("the rail's access item is an icon with no word")
	}
	// Two subheadings, because the screen holds two lists and they mean different things: what the
	// directory says, and who was given something it does not say.
	// Three: what the directory says, who was given something it does not, and what the words on
	// the chips mean.
	heads, _ := admin["heads"].([]any)
	if len(heads) != 3 {
		t.Errorf("the roster has %v subheadings", heads)
	}
	// Wearing the class that gives them their type role. A bare h3 inherits the body size, which is
	// the state this screen was in: everything the same size, and no way to tell a heading from a
	// row it heads.
	for _, h := range heads {
		if !strings.HasPrefix(fmt.Sprint(h), "rosterhead:") {
			t.Errorf("a subheading is drawn at whatever size it inherits: %v", h)
		}
	}
	// And each row is drawn in list anatomy rather than as three lines of the same size — which is
	// what it was, and the reason nobody could tell the name from what it buys.
	parts := fmt.Sprint(admin["parts"])
	for _, want := range []string{"who", "caps", "acclist", "caplegend", "accsay"} {
		if !strings.Contains(parts, want) {
			t.Errorf("no %q in the row's parts: %s", want, parts)
		}
	}
	// kim has seven, lee has one, the group has two — as chips, one element each.
	if n, _ := admin["rowchips"].(float64); n < 10 {
		t.Errorf("the rows carry %v capability chips; a joined string would look like this", n)
	}
	// A person's scope is a section of their own with a chip per companion, each removable: the ×
	// belongs where pressing it removes something that exists. On the capabilities it could not —
	// those come from the role, and "this operator, minus configure" is a sentence auth.toml has no
	// way to write.
	if !strings.Contains(parts, "scopes") {
		t.Errorf("no scope section under the people: %s", parts)
	}
	if n, _ := admin["scopechips"].(float64); n != 1 {
		t.Errorf("%v scope chips; lee is scoped to one companion", n)
	}
	// The list says which magi it governs. A console watching three machines draws three of these
	// screens the same way, and only this line tells them apart.
	if w, _ := admin["whose"].(string); !strings.Contains(w, "you@studio") {
		t.Errorf("the list does not say whose it is: %q", w)
	}

	// Somebody who may read but not admin: the screen is not there to arrive at, even by address.
	viewer := runPage(t, fleet, "?v=access", strings.Replace(page, "CAN", `['read']`, 1))
	// Hidden, which is the whole of what the PAGE can promise. What keeps the list from somebody
	// who may not have it is the server — TestOnlyAnAdminSeesOrChangesWhoMayUseThisConsole — and
	// that is deliberate: hiding is for the person who would otherwise be offered a control that
	// answers 403, never for the answer itself.
	if viewer["hidden"] != true {
		t.Error("a viewer reached the access screen by editing the address")
	}
	if viewer["railHidden"] != true {
		t.Error("a viewer is offered a rail item leading to a screen they will be refused")
	}
}

// The masthead says which magi this is.
//
// Three of these open in three tabs are the same page: before this, telling them apart meant
// recognising the companions in the list, which fails at exactly the moment it matters — when two
// machines are running the same work. And it stays quiet when the console cannot say, because a
// masthead reading "unknown" is worse than one that does not claim.
func TestTheMastheadNamesTheInstance(t *testing.T) {
	page := `
const base = globalThis.fetch;
globalThis.fetch = async (p, o) => {
  const path = String(p).split('?')[0];
  if (path === '/console') return {ok: true, json: async () => (CONSOLE)};
  return base(p, o);
};
loadConsole();
for (let i = 0; i < 8; i++) await Promise.resolve();
console.log(JSON.stringify({where: byId.whereami.textContent}));
`
	named := runPage(t, `[]`, "", strings.Replace(page, "CONSOLE",
		`{host: 'studio', user: 'you', configDir: '/Users/you/.config/magi'}`, 1))
	if named["where"] != "you@studio" {
		t.Errorf("the masthead says %q", named["where"])
	}
	// Half an answer is still an answer: the machine, when nothing can name the account.
	partial := runPage(t, `[]`, "", strings.Replace(page, "CONSOLE", `{host: 'studio'}`, 1))
	if partial["where"] != "studio" {
		t.Errorf("with no account the masthead says %q", partial["where"])
	}
	// And nothing at all rather than a guess.
	silent := runPage(t, `[]`, "", strings.Replace(page, "CONSOLE", `{}`, 1))
	if silent["where"] != "" {
		t.Errorf("a console that could say nothing claimed %q", silent["where"])
	}
}

// The report format is reached from the facts, not from a card in the side pane.
//
// The pane is what is HAPPENING — the plan, what was handed over, what somebody had to say. This
// changes about as often as the model does, and the terminal never carried it in a pane at all. It
// is a button beside the other two things you go and look at about a companion; as a row of its own
// it cost the card a line to show three words nobody was reading.
func TestTheReportFormatIsReachedFromTheFacts(t *testing.T) {
	got := runPage(t, `[]`, "", `
globalThis.fetch = async (p) => ({ok: true, json: async () =>
  String(p).startsWith('/report-format') ? {from: 'workspace', sections: [
    {key: 'tried', prompt: 'what you ran'},
    {key: 'stakes', prompt: 'what it costs'},
  ]} : []});
await drawDetail({socket: '/s/a.sock', name: 'api', state: 'working', workdir: '/w/api',
                  steps: 1, idle: 2, session: 's1'});
for (let i = 0; i < 12; i++) await Promise.resolve();
const button = byId.detail.find('button').filter(b => b.textContent === tr('field.report_format'));
if (button.length) button[0].onclick();
for (let i = 0; i < 12; i++) await Promise.resolve();
console.log(JSON.stringify({
  buttons: byId.detail.find('button').map(b => b.textContent),
  pane: byId.side.children.map(c => c.attrs.id || '').join(' '),
  // Pressing it opens the editor with what is in force, and says where that came from — the three
  // cases mean three different things by "save".
  dialog: byId.fmtForm.children.map(c => c.textContent).join(' | '),
  open: !!byId.fmtDialog.open,
}));
`)
	buttons := fmt.Sprint(got["buttons"])
	if !strings.Contains(buttons, "What it brings you when it asks") {
		t.Errorf("no way to the report format in the facts: %s", buttons)
	}
	if strings.Contains(got["pane"].(string), "reportfmt") {
		t.Error("the report format is still a card in the side pane")
	}
	if got["open"] != true {
		t.Error("pressing it did not open the editor")
	}
	if d, _ := got["dialog"].(string); !strings.Contains(d, "this companion's own") {
		t.Errorf("the editor does not say where the format in force came from: %q", d)
	}
}

// A control that opens an empty column reads as broken rather than as empty.
//
// The pane comes and goes with what a companion is doing: a fresh one has no plan, nothing handed
// over and nothing scheduled. Disabled rather than hidden, because a button that leaves the
// masthead moves everything beside it.
func TestTheSidePaneToggleSaysWhenThereIsNothingToOpen(t *testing.T) {
	page := `
globalThis.fetch = async (p) => ({ok: true, json: async () => (ANSWER)});
await drawDetail({socket: '/s/a.sock', name: 'api', state: 'working', workdir: '/w/api',
                  steps: 1, idle: 2, session: 's1'});
for (let i = 0; i < 12; i++) await Promise.resolve();
console.log(JSON.stringify({off: !!byId.sideToggle.disabled, says: byId.sideToggle.attrs['aria-label']}));
`
	empty := runPage(t, `[]`, "", strings.Replace(page, "ANSWER", `[]`, 1))
	if empty["off"] != true {
		t.Error("the control offers to open a pane with nothing in it")
	}
	if s, _ := empty["says"].(string); !strings.Contains(strings.ToLower(s), "nothing") {
		t.Errorf("a disabled control does not say why: %q", s)
	}

	full := runPage(t, `[]`, "", strings.Replace(page, "ANSWER",
		`String(p).startsWith('/plan') ? [{content: 'write the spec', status: 'in_progress'}] : []`, 1))
	if full["off"] != false {
		t.Error("a companion with a plan cannot open the pane holding it")
	}
}

// The other view of the companions: where they are running, and what is crossing between them.
//
// A table answers "what is each one doing". Once there is more than one machine in a fleet the
// other question is where all this is running and what actually travels — and the answer is not a
// list, because the thing being asked about is the shape. Two boundaries are drawn as two boxes:
// the machine, which is what a network reaches and what goes down, and the account on it, which is
// what owns a config directory, a policy and a key.
func TestTheMapDrawsMachinesAndTheAccountsInsideThem(t *testing.T) {
	got := runPage(t, `[]`, "?v=map", `
globalThis.fetch = async (p) => {
  const path = String(p).split('?')[0];
  if (path === '/fleet') return {ok: true, json: async () => ([
    {socket: '/s/d.sock', name: 'design', state: 'working', host: 'studio', instance: 'you@studio',
     trust: 'own', live: true, idle: 3, hub: true, team: 'frontend'},
    {socket: '/s/a.sock', name: 'api', state: 'waiting', host: 'studio', instance: 'you@studio',
     trust: 'own', live: true, idle: 9, team: 'backend', hub: true},
    // Another account on the SAME machine: one box around them both, two inside it. Same team as
    // the one above, which is the case teams exist for — a team is not a machine and not an owner.
    {socket: '/far/r.sock', name: 'risk', state: 'waiting', elsewhere: true, host: 'studio',
     instance: 'sam@studio', trust: 'admitted', live: true, idle: 12, team: 'backend'},
    {socket: '/far/t.sock', name: 'tests', state: 'working', elsewhere: true, host: 'buildbox',
     instance: 'you@buildbox', trust: 'admitted', live: true, idle: 30, team: 'backend'},
    // Nothing heard for fifteen minutes, and its record says nothing about what it was doing —
    // which is a daemon too old to say, and is not the same as idle.
    {socket: '/far/x.sock', name: 'deploy', state: 'remote', elsewhere: true, host: 'mini',
     instance: 'ops@mini', trust: 'unknown', live: false, idle: 900},
  ])};
  if (path === '/handoffs') return {ok: true, json: async () => ([
    {from: 'design', to: 'api', socket: '/s/a.sock', request: 'spec it', state: 'working'},
    {from: 'api', to: 'deploy', socket: '/far/x.sock', request: 'ship it', state: 'idle'},
  ])};
  return {ok: true, json: async () => ([])};
};
render();
for (let i = 0; i < 20; i++) await Promise.resolve();
const named = (root, cls) => root.find('div').filter(d => String(d.className).split(' ').includes(cls));
const machines = byId.map.find('div').filter(d => String(d.className).split(' ').includes('machine'));
console.log(JSON.stringify({
  hidden: byId.map.hidden,
  machines: machines.map(m => named(m, 'machinename').map(d => d.textContent).join('')),
  // The accounts inside each, which is the boundary a machine box does NOT draw: two accounts on
  // one host share a kernel and nothing else.
  accounts: machines.map(m => named(m, 'placename').map(d => d.textContent).join(',')),
  trust: machines.map(m => named(m, 'placetrust').map(d => d.textContent).join(',')),
  faroff: named(byId.map, 'faroff').length,
  links: byId.map.find('a').filter(a => String(a.className).includes('node')).length,
  down: byId.map.find('div').filter(d => String(d.className).includes('placeseen down')).length,
  // Teams, which cut across both boxes: a heading inside each box that has one, so a reader can
  // see that "backend" is one companion here, one on another account and one on another machine.
  teams: named(byId.map, 'teamlabel').map(d => d.textContent),
}));
`)
	if got["hidden"] == true {
		t.Fatal("the map is not drawn at its own address")
	}
	machines, _ := got["machines"].([]any)
	if len(machines) != 3 {
		t.Fatalf("five companions on three machines drew %v boxes", machines)
	}
	if machines[0] != "studio" || machines[2] != "mini" {
		t.Errorf("the machines are not ordered by what can be done with them: %v", machines)
	}
	// The machine is named once, by the box; the account inside it is not "you@studio" again.
	accounts, _ := got["accounts"].([]any)
	if accounts[0] != "you,sam" {
		t.Errorf("the accounts on the first machine are %v", accounts[0])
	}
	if tr := fmt.Sprint(got["trust"]); !strings.Contains(tr, "yours") || !strings.Contains(tr, "not admitted") {
		t.Errorf("a box does not say this console's relationship with it: %s", tr)
	}
	if n, _ := got["faroff"].(float64); n != 3 {
		t.Errorf("%v companions on other instances are drawn as unreachable; three are", n)
	}
	if n, _ := got["links"].(float64); n != 2 {
		t.Errorf("%v nodes link into a companion's page; only this instance's two should", n)
	}
	if n, _ := got["down"].(float64); n != 1 {
		t.Errorf("%v machines say nothing has been heard from them; one is quiet", n)
	}
	// backend is on three of the boxes and frontend on one, so four headings — the count is the
	// point: a team drawn once, in the box it happens to start in, would be a lie about where it is.
	teams := fmt.Sprint(got["teams"])
	if strings.Count(teams, "backend") != 3 || !strings.Contains(teams, "frontend") {
		t.Errorf("the teams are not shown where their companions are: %s", teams)
	}
}

// The workspace beside the conversation, read through the companion.
//
// A companion is bound to a directory and the console could say the path and nothing about what is
// in it. The tree is on the leading side, opposite the state pane — two panes, because a console
// with one makes somebody choose between seeing the files and seeing what the agent is doing — and
// an opened file shares the slot the facts card is in, behind a tab, because stacking them would
// push the transcript off the screen.
func TestAFileOpensIntoTheSlotTheFactsCardShares(t *testing.T) {
	got := runPage(t, `[]`, "?d=%2Fs%2Fa.sock", `
localStorage.setItem('files', 'open');
document.body.setAttribute('files', 'open');
const row = {socket: '/s/a.sock', name: 'api', state: 'working', workdir: '/w/api', session: 's1',
             live: true, idle: 2};
globalThis.fetch = async (p) => {
  const path = String(p).split('?')[0];
  // The fleet answers with this companion: the poll runs while the test does, and a poll that
  // finds nothing clears the card this is about.
  if (path === '/fleet') return {ok: true, json: async () => ([row])};
  // Not a checkout, so the git section draws nothing and the rows below are the tree's own.
  if (path === '/git') return {ok: true, json: async () => ({repo: false})};
  if (path === '/files') {
    const at = new URLSearchParams(String(p).split('?')[1] || '').get('path');
    if (at === 'cmd') return {ok: true, json: async () => ([{name: 'magi', isDir: true},
                                                            {name: 'main.go', isDir: false}])};
    return {ok: true, json: async () => ([{name: 'cmd', isDir: true},
                                          {name: 'README.md', isDir: false}])};
  }
  if (path === '/file') return {ok: true, json: async () =>
    ({path: 'README.md', text: '     1\tmagi\n     2\t====\n'})};
  return {ok: true, json: async () => ([])};
};
const a = row;
await loadTree(a);
for (let i = 0; i < 10; i++) await Promise.resolve();
const rows = () => byId.files.find('button').filter(b => String(b.className).includes('treerow'));
const names = () => rows().map(b => b.find('div').map(d => d.textContent).join(''));
const before = names();
// A directory opens under itself rather than replacing the tree.
rows().find(b => b.find('div').some(d => d.textContent === 'cmd')).onclick();
// The tree redraw asks git and the directory before it can finish, so this waits for more than one
// answer to land.
for (let i = 0; i < 40; i++) await Promise.resolve();
const expanded = names();
// The facts card is drawn just before, because the thing under test is what happens to it when a
// file arrives — with an empty card, "hidden" would be true for its own reason and the check would
// pass with the sharing removed.
await drawDetail(a);
// A file opens into the slot, behind a tab, and the facts card stands aside for it.
rows().find(b => b.find('div').some(d => d.textContent === 'README.md')).onclick();
for (let i = 0; i < 40; i++) await Promise.resolve();
console.log(JSON.stringify({
  before: before,
  expanded: expanded,
  tabs: byId.cardtabs.children.map(t => t.textContent || (t.find('div')[0] || {}).textContent || ''),
  tabsHidden: !!byId.cardtabs.hidden,
  // The code column only: the numbers are their own block now, unselectable, so what a drag copies
  // is what this reads.
  file: byId.fileview.find('pre').filter(p => String(p.className).includes('filecode'))
                     .map(p => p.text).join(''),
  gutter: byId.fileview.find('pre').filter(p => String(p.className).includes('filegutter'))
                       .map(p => p.textContent).join(''),
  // The whole path, in its own bar: this is the line somebody copies, and half of one pastes as
  // nothing.
  dir: byId.fileview.find('div').filter(d => String(d.className).includes('filedir'))
                    .map(d => d.textContent).join(''),
  fileHidden: !!byId.fileview.hidden,
  factsHidden: !!byId.detail.hidden,
  factsKids: byId.detail.children.length,
  // And it stays out of the way when the poll redraws it, which it does every three seconds.
  afterPoll: await (async () => { await drawDetail(a); return !!byId.detail.hidden; })(),
}));
`)
	before, _ := got["before"].([]any)
	if len(before) != 2 {
		t.Fatalf("the workspace root drew %v", before)
	}
	expanded, _ := got["expanded"].([]any)
	if len(expanded) != 4 {
		t.Errorf("opening a directory drew %v — its entries belong under it, not instead of it", expanded)
	}
	if got["tabsHidden"] == true {
		t.Error("a file is open and nothing says which of the two the slot is showing")
	}
	if !strings.Contains(fmt.Sprint(got["tabs"]), "README.md") {
		t.Errorf("the tabs are %v", got["tabs"])
	}
	// Line numbers as the tool wrote them: a person and their companion have to be able to point
	// at the same line 40.
	if f, _ := got["file"].(string); !strings.Contains(f, "magi") || strings.Contains(f, "1\t") {
		t.Errorf("the code column is %q — the numbers belong in their own, or a drag copies them", f)
	}
	if g, _ := got["gutter"].(string); !strings.Contains(g, "1\n2") {
		t.Errorf("the gutter is %q", g)
	}
	if got["fileHidden"] == true {
		t.Error("the file was fetched and not shown")
	}
	// The card has to have been drawn for this to mean anything: an empty one is hidden for its own
	// reason, and a test that could not tell the two apart would pass with the sharing removed.
	if n, _ := got["factsKids"].(float64); n == 0 {
		t.Fatal("the facts card was never drawn, so nothing here is testing that it stands aside")
	}
	if got["factsHidden"] != true {
		t.Error("the facts card and the file are both in the slot at once")
	}
	if got["afterPoll"] != true {
		t.Error("the poll put the facts card back over the file somebody is reading")
	}
	if got["dir"] != "README.md" {
		t.Errorf("the path bar says %q", got["dir"])
	}
}

// The branch, and what has not been committed, over the tree they are the state of.
//
// A file list with no branch on it is the same list on main and on a branch three commits deep,
// and which of those somebody is looking at changes what every row in it means. A workspace that
// is not a checkout says nothing at all — there is no branch to be on, and a heading over an
// emptiness is worse than no heading.
func TestTheFilePaneSaysWhatGitMakesOfTheWorkspace(t *testing.T) {
	page := `
localStorage.setItem('files', 'open');
document.body.setAttribute('files', 'open');
globalThis.fetch = async (p) => {
  const path = String(p).split('?')[0];
  if (path === '/git') return {ok: true, json: async () => (GIT)};
  if (path === '/files') return {ok: true, json: async () => ([{name: 'go.mod', isDir: false}])};
  return {ok: true, json: async () => ([])};
};
await loadTree({socket: '/s/a.sock', name: 'api', workdir: '/w/api', session: 's1'});
for (let i = 0; i < 40; i++) await Promise.resolve();
const named = cls => byId.files.find('div').filter(d => String(d.className).split(' ').includes(cls));
console.log(JSON.stringify({
  branch: named('gitbranch').map(d => d.textContent).join(''),
  ahead: named('gitab').map(d => d.textContent).join(' '),
  changed: byId.files.find('button').filter(b => String(b.className).includes('gitrow'))
                     .map(b => b.find('div').map(d => d.textContent).join(' ')),
}));
`
	on := runPage(t, `[]`, "?d=%2Fs%2Fa.sock", strings.Replace(page, "GIT",
		`{repo: true, branch: 'engine-ui-split', ahead: 2, changes: [
			{path: 'internal/app/git.go', kind: 'staged'},
			{path: 'docs/UI.md', kind: 'both'}]}`, 1))
	if on["branch"] != "engine-ui-split" {
		t.Errorf("the pane says the branch is %q", on["branch"])
	}
	if a, _ := on["ahead"].(string); !strings.Contains(a, "2") {
		t.Errorf("two commits to push are drawn as %q", a)
	}
	changed, _ := on["changed"].([]any)
	if len(changed) != 2 || !strings.Contains(fmt.Sprint(changed), "git.go") {
		t.Errorf("the uncommitted files are %v", changed)
	}
	// Each one says WHAT kind of change in words: git's XY pair is git's alphabet, and a console
	// printing "RM" is asking its reader to know it.
	if !strings.Contains(fmt.Sprint(changed), "staged") {
		t.Errorf("no word for the kind of change: %v", changed)
	}

	// A detached head is not a branch, and a directory nobody put under version control has no
	// section at all.
	off := runPage(t, `[]`, "?d=%2Fs%2Fa.sock", strings.Replace(page, "GIT",
		`{repo: true, head: 'abc12345', changes: []}`, 1))
	if b, _ := off["branch"].(string); !strings.Contains(b, "abc12345") {
		t.Errorf("a detached head is drawn as %q", b)
	}
	none := runPage(t, `[]`, "?d=%2Fs%2Fa.sock", strings.Replace(page, "GIT", `{repo: false}`, 1))
	if none["branch"] != "" {
		t.Errorf("a workspace that is not a checkout drew %q", none["branch"])
	}
}
