package main

import (
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"slices"
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
console.log(JSON.stringify({cards, state: byId.state.textContent, stateCls: byId.state.className}));
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
	text := got["empty"].(string)
	for _, want := range []string{"No magi daemons", "magi --daemon"} {
		if !strings.Contains(text, want) {
			t.Errorf("the empty state does not say %q: %q", want, text)
		}
	}
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
const tiles = byId.summary.children.map(t => ({k: t.text, pressed: !!t.selected, off: !!t.disabled}));
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
		{"?v=interventions", "corrections", "/?v=interventions"},
		{"?v=skills", "lessons", "/?v=skills"},
		{"?v=mcp", "connections", "/?v=mcp"},
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

// The tabs are nouns. A tab is a place you are standing, and "what I had to say" is a sentence
// about one — which also does not fit beside three others on a phone.
func TestTheTabsAreNamedAsPlaces(t *testing.T) {
	for _, want := range []string{">companions<", ">corrections<", ">lessons<", ">connections<"} {
		if !strings.Contains(indexHTML, want) {
			t.Errorf("the tab strip has no %s", want)
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

// The supervisor's evening pass: what did I have to step in and say, and what of it is a rule?
//
// One correction is a remark. The SAME one to three companions is a rule waiting to be written, and
// counting that by hand across five transcripts is exactly the work nobody does — so the grouping
// and the ordering are the whole feature, not decoration on a list.
func TestInterventionsGroupByWhatWasSaidAndCount(t *testing.T) {
	got := runPage(t, `[]`, "?v=interventions", `
globalThis.fetch = async (p) => ({ok: true, json: async () => p.startsWith('/interventions') ? [
  {"companion":"api","kind":"steer","text":"run the tests before you say it is done","at":"2026-08-07T09:00:00Z","afterSec":40},
  {"companion":"docs","kind":"steer","text":"Run the tests  before you say it is done","at":"2026-08-07T10:00:00Z","afterSec":12},
  {"companion":"api","kind":"steer","text":"do not touch that file","at":"2026-08-07T11:00:00Z","afterSec":5},
  {"companion":"api","kind":"denied","text":"bash","at":"2026-08-07T08:00:00Z","afterSec":9}
] : []});
await loadInterventions();
console.log(JSON.stringify({
  rows: byId.ivs.children.map(r => ({cls: r.className, text: r.text})),
  state: byId.state.textContent,
}));
`)
	rows := got["rows"].([]any)
	if len(rows) != 3 {
		t.Fatalf("four interventions grouped into %d rows; the two spellings of one sentence are one thing", len(rows))
	}
	first := rows[0].(map[string]any)["text"].(string)
	// Most repeated first: that is the promotion candidate, and it is the reason to open this page.
	if !strings.HasPrefix(strings.TrimSpace(first), "2×") {
		t.Errorf("the most repeated correction is not first: %q", first)
	}
	// And it says which companions said it to, because "everywhere" and "one of them" promote to
	// different tiers.
	for _, want := range []string{"api", "docs"} {
		if !strings.Contains(first, want) {
			t.Errorf("the row does not say where it happened (%q): %q", want, first)
		}
	}
	// A refusal is the shortest correction there is, and it reads as one.
	var denied string
	for _, r := range rows {
		if strings.Contains(r.(map[string]any)["cls"].(string), "denied") {
			denied = r.(map[string]any)["text"].(string)
		}
	}
	if !strings.Contains(denied, "refused") || !strings.Contains(denied, "bash") {
		t.Errorf("the denial row reads %q", denied)
	}
	if !strings.Contains(got["state"].(string), "4 interventions") {
		t.Errorf("the header says %q", got["state"])
	}
}

// Nothing yet is a sentence, not a blank page — and it says what fills it, because a supervisor who
// has never steered mid-turn has no way to guess what this page is for.
func TestAnEmptyInterventionsPageSaysWhatFillsIt(t *testing.T) {
	got := runPage(t, `[]`, "?v=interventions", `
globalThis.fetch = async () => ({ok: true, json: async () => []});
await loadInterventions();
console.log(JSON.stringify({text: byId.ivs.text}));
`)
	for _, want := range []string{"Nothing to promote", "steer", "refuse"} {
		if !strings.Contains(got["text"].(string), want) {
			t.Errorf("the empty page does not mention %q: %q", want, got["text"])
		}
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
  byHand: ['tabFleet', 'tabIv', 'tabSkills', 'tabMcp'].filter(id => byId[id].setDirectly),
}));
`)
	if got["where"] != "?v=skills" {
		t.Errorf("clicking the lessons tab went to %q", got["where"])
	}
	if got["on"].(float64) != 2 {
		t.Errorf("md-tabs has tab %v active, and lessons is the third", got["on"])
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

// One button, two meanings, and the width decides which.
//
// Wide: the rail is already on screen and the button widens it, so the page behind stays usable
// and there is no scrim. Narrow: the same drawer comes in OVER the page, so there is one, and
// picking a destination closes it — a drawer left open on a phone hides the thing you navigated to.
func TestTheDrawerMeansOneThingOnEachWidth(t *testing.T) {
	wide := runPage(t, `[]`, "", `
byId.menu.onclick();
console.log(JSON.stringify({nav: document.body.getAttribute('nav'), scrim: byId.scrim.hidden,
  said: byId.menu.getAttribute('aria-expanded')}));
`)
	if wide["nav"] != "open" || wide["scrim"] != true {
		t.Errorf("on a wide screen the drawer reads %+v; the page behind it stays reachable", wide)
	}
	if wide["said"] != "true" {
		t.Errorf("the button says aria-expanded=%v after opening", wide["said"])
	}

	t.Setenv("NARROW", "1")
	narrow := runPage(t, `[]`, "", `
globalThis.fetch = async () => ({ok: true, json: async () => []});
byId.menu.onclick();
const opened = {nav: document.body.getAttribute('nav'), scrim: byId.scrim.hidden};
byId.railSkills.onclick({preventDefault(){}});
console.log(JSON.stringify({opened, after: document.body.getAttribute('nav'), where: location.search}));
`)
	op := narrow["opened"].(map[string]any)
	if op["nav"] != "open" || op["scrim"] != false {
		t.Errorf("on a phone the drawer reads %+v; it covers the page, so it needs a scrim", op)
	}
	if narrow["after"] != nil {
		t.Errorf("the drawer is still %v after picking a destination, hiding what was navigated to", narrow["after"])
	}
	if narrow["where"] != "?v=skills" {
		t.Errorf("the rail went to %q", narrow["where"])
	}
}

// The rail and the tabs are two ways to say the same thing, so they must say the same thing.
func TestTheRailAgreesWithTheTabs(t *testing.T) {
	got := runPage(t, `[]`, "?v=mcp", `
globalThis.fetch = async () => ({ok: true, json: async () => []});
console.log(JSON.stringify({
  on: byId.tabs.activeTabIndex,
  lit: ['railFleet','railIv','railSkills','railMcp'].filter(id => byId[id].hasAttribute('selected')),
}));
`)
	if got["on"].(float64) != 3 {
		t.Errorf("the tabs have %v active and connections is the fourth", got["on"])
	}
	lit := got["lit"].([]any)
	if len(lit) != 1 || lit[0] != "railMcp" {
		t.Errorf("the rail lights %v while the tabs are on connections", lit)
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
console.log(JSON.stringify({head, rows: rows().map(r => r.children.length)}));
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
  ivOn: !!byId.tabIv.active, fleetHidden: byId.fleet.hidden, ivsHidden: byId.ivs.hidden}));
`)
	if fleet["tabs"].(bool) || fleet["fleetOn"] != true || fleet["ivOn"] != false {
		t.Errorf("on the fleet the tabs read %+v", fleet)
	}
	if fleet["ivsHidden"] != true || fleet["fleetHidden"] != false {
		t.Errorf("the fleet view shows the wrong list: %+v", fleet)
	}
	ivs := runPage(t, `[]`, "?v=interventions", `
globalThis.fetch = async () => ({ok: true, json: async () => []});
console.log(JSON.stringify({fleetOn: !!byId.tabFleet.active, ivOn: !!byId.tabIv.active,
  fleetHidden: byId.fleet.hidden, ivsHidden: byId.ivs.hidden, summaryHidden: byId.summary.hidden}));
`)
	if ivs["ivOn"] != true || ivs["fleetOn"] != false {
		t.Errorf("on the interventions page the tabs read %+v", ivs)
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

// Promotion is the reason the corrections page exists, and the tier is the person's choice.
//
// A project fact promoted to global leaks one project's truth into another's prompts, quietly, and
// nobody finds the cause weeks later. So the button that crosses that boundary is a different
// button, and the one that does not only appears when there is a single project to mean.
func TestPromotingOffersTheTierThePersonCanActuallyPick(t *testing.T) {
	got := runPage(t, `[]`, "?v=interventions", `
globalThis.fetch = async (p, init) => {
  if (init && init.method === 'POST') { RENDERED.push({fetched: p, body: init.body.toString()}); return {ok: true, status: 204, text: async () => ''}; }
  return {ok: true, json: async () => p.startsWith('/interventions') ? [
    {"companion":"api","socket":"/s/a.sock","kind":"steer","text":"do not touch vendor","at":"2026-08-07T11:00:00Z"},
    {"companion":"api","socket":"/s/a.sock","kind":"steer","text":"run the tests first","at":"2026-08-07T09:00:00Z"},
    {"companion":"docs","socket":"/s/b.sock","kind":"steer","text":"Run the tests first","at":"2026-08-07T10:00:00Z"}
  ] : []};
};
await loadInterventions();
const byText = t => byId.ivs.children.find(r => r.text.includes(t));
const shared = byText('run the tests first'), single = byText('do not touch vendor');
console.log(JSON.stringify({
  sharedButtons: shared.find(clicky).map(b => b.textContent),
  singleButtons: single.find(clicky).map(b => b.textContent),
  sharedNote: shared.text,
}));
`)
	var shared, single []string
	for _, b := range got["sharedButtons"].([]any) {
		shared = append(shared, b.(string))
	}
	for _, b := range got["singleButtons"].([]any) {
		single = append(single, b.(string))
	}
	// Said to two companions: no single project to put it in, so only the crossing button, and a
	// line saying why the other one is missing.
	if strings.Join(shared, "|") != "to every companion" {
		t.Errorf("a correction given to two companions offers %v", shared)
	}
	if !strings.Contains(got["sharedNote"].(string), "no single project") {
		t.Errorf("nothing says why the project button is absent: %q", got["sharedNote"])
	}
	// Said to one: both tiers are meaningful, and the project one names it.
	if len(single) != 2 || !strings.Contains(single[0], "api") || single[1] != "to every companion" {
		t.Errorf("a correction given to one companion offers %v", single)
	}
}

// What the buttons send: the words verbatim, the tier named, and for a project rule the companion
// it belongs to — resolved by the server from what it published, never from a path the page chose.
func TestPromotionSendsTheWordsAndTheTier(t *testing.T) {
	got := runPage(t, `[]`, "?v=interventions", `
globalThis.fetch = async (p, init) => {
  if (init && init.method === 'POST') { RENDERED.push({fetched: p, method: 'POST', body: init.body.toString()}); return {ok: true, status: 204, text: async () => ''}; }
  return {ok: true, json: async () => p.startsWith('/interventions') ? [
    {"companion":"api","socket":"/s/a.sock","kind":"steer","text":"do not touch vendor","at":"2026-08-07T11:00:00Z"}
  ] : []};
};
await loadInterventions();
const row = byId.ivs.children[0];
row.find(clicky)[0].onclick();          // rule for api
row.find(clicky)[1] && row.find(clicky)[1].onclick();
await new Promise(r => queueMicrotask(r));
console.log(JSON.stringify({posts: RENDERED.filter(r => r.method === 'POST')}));
`)
	posts := got["posts"].([]any)
	if len(posts) == 0 {
		t.Fatal("pressing promote sent nothing")
	}
	first := posts[0].(map[string]any)
	if !strings.Contains(first["fetched"].(string), "/promote?d=") {
		t.Errorf("a project rule went to %q without naming the companion", first["fetched"])
	}
	body := first["body"].(string)
	if !strings.Contains(body, "scope=project") || !strings.Contains(body, "do+not+touch+vendor") {
		t.Errorf("the promotion body is %q", body)
	}
}

// What the companions have learned, and which of it crosses between them.
//
// The tier is the whole of context hygiene and the page has to make it impossible to miss: "every
// companion" and "only this one" are different sentences, not a colour difference, because the
// decision a supervisor makes on this page is exactly which of the two a rule should be.
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
const rows = byId.skills.children;
rows[1].find('md-text-button')[0].onclick();
console.log(JSON.stringify({
  rows: rows.map(r => ({cls: r.className, text: r.text})),
  state: byId.state.textContent,
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
	// Forgetting a project rule names the companion it belongs to; a global one has nobody to name.
	posts := got["posts"].([]any)
	if len(posts) != 1 {
		t.Fatalf("pressing forget sent %d requests", len(posts))
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
  state: byId.state.textContent, cls: byId.state.className,
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

// How far into a turn somebody stepped in.
//
// The engine has derived this since interventions existed and nothing ever showed it, which threw
// away the distinction it was derived for: a steer eight seconds in corrects the INSTRUCTION and a
// rule can prevent the next one, while one twenty minutes in corrects the WORK and no rule would
// have helped. They promote to different things, so the page has to say which happened.
func TestACorrectionSaysHowFarIntoTheTurnItCame(t *testing.T) {
	got := runPage(t, `[]`, "?v=interventions", `
globalThis.fetch = async (p) => ({ok: true, json: async () => p.startsWith('/interventions') ? [
  {kind:'steer', text:'not that file', companion:'api', socket:'/s/a.sock', at:'2026-08-07T01:00:00Z', afterSec: 8},
  {kind:'steer', text:'not that file', companion:'web', socket:'/s/b.sock', at:'2026-08-07T02:00:00Z', afterSec: 1200},
  {kind:'steer', text:'use the gateway', companion:'api', socket:'/s/a.sock', at:'2026-08-07T03:00:00Z', afterSec: 45},
] : []});
await loadInterventions();
console.log(JSON.stringify({text: byId.ivs.text}));
`)
	text := got["text"].(string)
	// A group whose members were interrupted at different moments carries both ends: the same words
	// said early to one companion and late to another are not the same correction twice.
	if !strings.Contains(text, "stepped in 8s–20m into the turn") {
		t.Errorf("the spread is not shown:\n%s", text)
	}
	// And one moment says just the one.
	if !strings.Contains(text, "stepped in 45s into the turn") {
		t.Errorf("a single correction does not say when:\n%s", text)
	}
	// The suffix that belongs to "how long ago" must not follow "how long into".
	if strings.Contains(text, "ago into the turn") {
		t.Errorf("the duration was rendered as an age:\n%s", text)
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

// The console's composer is addressed on the fleet view: the work goes to whoever does that thing,
// and which machine they are on is not the asker's problem.
func TestTheFleetComposerSendsWorkToAnAddress(t *testing.T) {
	got := runPage(t, `[]`, "", `
globalThis.fetch = async (p, init) => {
  if (init && init.method === 'POST') { RENDERED.push({to: p, body: init.body.toString()}); return {ok: true, status: 204, text: async () => ''}; }
  return {ok: true, json: async () => [
    {socket:'/s/d.sock', name:'design', role:'component specs and visual review', team:'frontend', hub:true, state:'idle', workdir:'/w/d', session:'d', idle:5},
    {socket:'/s/b.sock', name:'buttons', role:'components', team:'frontend', state:'idle', workdir:'/w/b', session:'b', idle:9},
    {socket:'/s/a.sock', name:'api', role:'the billing API', state:'working', workdir:'/w/a', session:'a', idle:1},
  ]};
};
await loadFleet();
byId.t.value = 'spec the empty state';
byId.to.value = 'component specs';
await f.onsubmit({preventDefault(){}});
const before = RENDERED.filter(r => r.to).length;
byId.t.value = 'another thing';
byId.to.value = '';
await f.onsubmit({preventDefault(){}});
console.log(JSON.stringify({
  posts: RENDERED.filter(r => r.to),
  refused: RENDERED.filter(r => r.to).length === before,
  state: byId.state.textContent,
  suggestions: byId.roles.children.map(o => o.value),
  fleetText: byId.fleet.text,
}));
`)
	posts := got["posts"].([]any)
	if len(posts) != 1 {
		t.Fatalf("one addressed send produced %d requests: %v", len(posts), posts)
	}
	first := posts[0].(map[string]any)
	if !strings.HasPrefix(first["to"].(string), "/dispatch") {
		t.Errorf("the work did not go to the dispatcher: %v", first["to"])
	}
	body := first["body"].(string)
	if !strings.Contains(body, "to=component+specs") || !strings.Contains(body, "text=spec+the+empty+state") {
		t.Errorf("the request lost the address or the words: %q", body)
	}
	// An empty address is refused rather than guessed: guessing sends somebody's turn into the
	// wrong workspace, and nobody finds out until the work comes back from the wrong place.
	if got["refused"] != true {
		t.Error("work with nobody named was sent anyway")
	}
	if !strings.Contains(got["state"].(string), "who it is for") {
		t.Errorf("the page does not say what is missing: %q", got["state"])
	}
	// Both kinds of address are offered, because a person should not have to remember which one a
	// given companion declared.
	// Names, roles and teams are all valid addresses, and a team is offered once rather than once
	// per member — the list is read by a person typing, not by a machine.
	sugg := make([]string, 0, 8)
	for _, v := range got["suggestions"].([]any) {
		sugg = append(sugg, v.(string))
	}
	for _, want := range []string{"design", "the billing API", "frontend"} {
		if !slices.Contains(sugg, want) {
			t.Errorf("%q is not offered as an address: %v", want, sugg)
		}
	}
	if n := strings.Count(strings.Join(sugg, "\x00"), "frontend"); n != 1 {
		t.Errorf("the team is offered %d times: %v", n, sugg)
	}
	// And the row says which one answers for the team, because that is where work addressed to the
	// team actually lands.
	if !strings.Contains(got["fleetText"].(string), "frontend · speaks for it") {
		t.Errorf("the hub is not marked:\n%s", got["fleetText"])
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
drops[1].onclick();
console.log(JSON.stringify({text, state: byId.state.textContent, posts: RENDERED.filter(r => r.to)}));
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
for (const [name, el] of [['corrections', tabIv], ['lessons', tabSkills],
                          ['connections', tabMcp], ['companions', tabFleet]]) {
  el.onclick({preventDefault(){}});
  seen.push({name, search: location.search, crumb: back.text, href: back.attrs.href,
             ivs: byId.ivs.hidden, skills: byId.skills.hidden, mcp: byId.mcp.hidden,
             fleet: byId.fleet.hidden});
}
// And the crumb itself, which names a section and must lead to the one it names.
tabSkills.onclick({preventDefault(){}});
back.onclick({preventDefault(){}});
console.log(JSON.stringify({seen, afterCrumb: location.search}));
`)
	seen := got["seen"].([]any)
	want := []struct{ search, crumb, href string }{
		{"?v=interventions", "corrections", "/?v=interventions"},
		{"?v=skills", "lessons", "/?v=skills"},
		{"?v=mcp", "connections", "/?v=mcp"},
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
	// Each panel is shown only on its own tab.
	if seen[0].(map[string]any)["ivs"] != false || seen[1].(map[string]any)["skills"] != false ||
		seen[2].(map[string]any)["mcp"] != false || seen[3].(map[string]any)["fleet"] != false {
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
const first = {tabs: [tabFleet.text, tabIv.text, tabSkills.text, tabMcp.text],
               ask: byId.t.attrs.label};
// A pack arriving afterwards repaints what is already on screen.
labels$.next({'nav.companions': '컴패니언', 'nav.corrections': '교정',
              'nav.lessons': '배운 것', 'nav.connections': '연결',
              'label.ask': 'magi에게 요청'});
const after = [tabFleet.text, tabIv.text, tabSkills.text, tabMcp.text];
const askAfter = byId.t.attrs.label;
// Something drawn by hand before a pack lands must survive it.
byId.detail.replaceChildren(cell('f', 'a thing somebody was reading'));
labels$.next({'nav.companions': 'x'});
console.log(JSON.stringify({first, after, askAfter, kept: byId.detail.text}));
`)
	first := got["first"].(map[string]any)
	tabs := first["tabs"].([]any)
	if tabs[0] != "companions" || tabs[2] != "lessons" {
		t.Errorf("the first paint did not use the seeded pack: %v", tabs)
	}
	if first["ask"] != "ask magi" {
		t.Errorf("the composer's label came from nowhere: %q", first["ask"])
	}
	after := got["after"].([]any)
	if after[0] != "컴패니언" || after[3] != "연결" {
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
