package main

import (
	"encoding/json"
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
const cards = rows().map(c => ({
  cls: c.className,
  text: c.text,
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
	if strings.Join(labels, "/") != "allow/always/deny" {
		t.Errorf("the answer buttons are %v, want allow/always/deny", labels)
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
	if strings.Join(labels, "/") != "answer" {
		t.Errorf("a question's buttons are %v, want a single answer", labels)
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
	if !strings.Contains(got["stateCls"].(string), "lost") {
		t.Error("the header is not marked, so a blocked fleet looks like a calm one")
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
	if !got["fleetHidden"].(bool) {
		t.Error("the fleet is still drawn underneath an agent's transcript")
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
	if strings.Join(labels, "/") != "allow/always/deny" {
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
	if got["title"].(string) != "(2) magi · companions" {
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
	want := "1 waiting|1 working|1 idle|2 gone"
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
console.log(JSON.stringify({stops, posts: RENDERED.filter(r => r.method === 'POST').map(r => r.fetched)}));
`)
	var stops []float64
	for _, s := range got["stops"].([]any) {
		stops = append(stops, s.(float64))
	}
	// waiting, working: stoppable. idle: nothing to stop. the two dead ones: nobody to tell.
	if len(stops) != 5 || stops[0] != 1 || stops[1] != 1 || stops[2] != 0 || stops[3] != 0 || stops[4] != 0 {
		t.Errorf("stop controls per row: %v — want one on the two that are running", stops)
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
		{"", "companions", "/"},
		{"?v=skills", "shared", "/?v=skills"},
		// The old address still lands: what a companion can reach joined what it has learned, and
		// a link somebody kept must not stop working because two lists became one screen.
		{"?v=mcp", "shared", "/?v=skills"},
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
	for _, want := range []string{"waiting", "/w/api", "mini", "10.0.0.12", "7"} {
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
remote.find(clicky).filter(b => b.className === 'stop')[0].onclick({preventDefault(){}, stopPropagation(){}});
local.find(clicky).filter(b => b.className === 'stop')[0].onclick({preventDefault(){}, stopPropagation(){}});
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
  byHand: ['tabFleet', 'tabSkills', 'tabMcp'].filter(id => byId[id].setDirectly),
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
  keys: g ? g.children.filter(c => c.className === 'gk').map(c => c.textContent) : [],
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
byId.notifyBtn.onclick();
// Drained with microtasks: this harness's setTimeout is a stub that never fires, deliberately —
// a test that waited on a real timer would wait for the page's three-second poll.
for (let i = 0; i < 20; i++) await Promise.resolve();
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
	if q["label"] != "your answer" {
		t.Errorf("the composer still asks for work while the agent waits on a question: %q", q["label"])
	}
	if q["send"] != "answer" {
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
	if p["label"] != "ask magi" {
		t.Errorf("a permission prompt took the composer away: %q — deciding not to do the thing at "+
			"all is a legitimate reply, and it is typed here", p["label"])
	}
	if p["support"] != "" {
		t.Errorf("the composer carries an answering note while nothing is being answered: %q", p["support"])
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
	if strings.Join(order, "|") != "zulu|alpha|no team" {
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
for (let i = 0; i < 20; i++) await Promise.resolve();
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
console.log(JSON.stringify({opened, closed, before, after, again,
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
	after := got["after"].(map[string]any)
	if after["stored"] != "dark" || after["attr"] != "dark" {
		t.Errorf("pressing the toggle on a light page gave %+v, want dark stored and applied", after)
	}
	// There is no second control for the theme. It used to be a select in the preferences dialog as
	// well, which is one preference with two ways to be wrong about it; the toggle is the only one
	// now, so what has to hold is that the toggle and the STORE agree — checked above.
	if got["themeSelect"] != false {
		t.Error("the preferences dialog carries a theme control again; the masthead toggle is the one")
	}
	if again := got["again"].(map[string]any); again["stored"] != "light" || again["attr"] != "light" {
		t.Errorf("pressing it again gave %+v, want light", again)
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
  state: byId.state.text,
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
		!strings.Contains(first["text"].(string), "every companion") {
		t.Errorf("the crossing rule does not say it crosses: %+v", first)
	}
	second := rows[1].(map[string]any)["text"].(string)
	if !strings.Contains(second, "only api") {
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
  handed: byId.handoffs.text, plan: byId.plan.text}));
`)
	text := got["text"].(string)
	for _, want := range []string{"82,000 / 100,000 tokens", "measured", "41 messages", "2 folds",
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
	if !strings.Contains(plan, "✓") || !strings.Contains(plan, "▸") {
		t.Errorf("the plan does not distinguish done from in-flight:\n%s", plan)
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
	if !strings.Contains(text, "~5,000 tokens") || !strings.Contains(text, "estimated") {
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
  state: byId.state.text, cls: byId.state.className,
}));
`)
	if !strings.Contains(got["before"].(string), "api") {
		t.Fatalf("the first load drew nothing: %q", got["before"])
	}
	if got["after"] != got["before"] {
		t.Errorf("losing the console redrew the table as %q", got["after"])
	}
	if got["state"] != "cannot reach magi-web" || got["cls"] != "lost" {
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
const text = byId.mcp.text;
const form = byId.mcp.children[byId.mcp.children.length - 1];
for (const i of form.find('md-outlined-text-field')) {
  if (i.name === 'name') i.value = 'tickets';
  if (i.name === 'command') i.value = 'uvx';
}
form.find('md-outlined-select')[0].value = '/s/a.sock';
await form.onsubmit({preventDefault(){}});
const drops = byId.mcp.find('md-text-button').filter(b => (b.className || '').split(' ').includes('drop'));
drops[1].onclick();   // asks
drops[1].onclick();   // acts
console.log(JSON.stringify({text, state: byId.state.text, posts: RENDERED.filter(r => r.to)}));
`)
	text := got["text"].(string)
	for _, want := range []string{"every companion here", "only api", "npx -y figma-mcp",
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
	if !strings.Contains(text, "does not report it") {
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
const title = fold.attrs.title;
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
		{"?v=skills", "shared", "/?v=skills"},
		{"", "companions", "/"},
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
	if tabs[0] != "companions" || tabs[1] != "shared" {
		t.Errorf("the first paint did not use the seeded pack: %v", tabs)
	}
	if first["ask"] != "ask magi" {
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
