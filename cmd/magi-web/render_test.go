package main

import (
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
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
	script := "import { byId, RENDERED } from './dom.mjs';\n" +
		scriptBody(t, indexHTML) + "\n" + epilogue
	main := filepath.Join(dir, "page.mjs")
	if err := os.WriteFile(main, []byte(script), 0o644); err != nil {
		t.Fatal(err)
	}
	cmd := exec.Command(node, main)
	cmd.Env = append(os.Environ(), "FLEET_JSON="+fleetJSON, "QUERY="+query)
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
  buttons: (c.find('div').find(d => d.className === 'answer') || {find: () => []}).find('button').map(b => b.textContent),
  actions: (c.find('div').find(d => d.className === 'actions') || {find: () => []}).find('button').map(b => b.textContent),
  inputs: c.find('input').length,
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
  buttons: box.find('button').map(b => b.textContent),
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
	// One agent is waiting, so the tab says so — this page is often behind an app switcher.
	if got["title"].(string) != "(1) magi" {
		t.Errorf("the tab title is %q, and one agent is waiting", got["title"])
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
	if got["title"].(string) != "magi" {
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
	if got["title"].(string) != "(2) magi" {
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
box.find('button')[0].onclick({preventDefault(){}, stopPropagation(){}});
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
const tiles = byId.summary.children.map(t => ({k: t.text, pressed: t.getAttribute('aria-pressed'), off: t.disabled}));
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
console.log(JSON.stringify({order: rows().map(r => r.className.replace('card ', ''))}));
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
const stops = rows().map(r => r.find('button').filter(b => b.className === 'stop').length);
rows()[0].find('button').filter(b => b.className === 'stop')[0].onclick({preventDefault(){}, stopPropagation(){}});
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
	// The crumb's own word and its href are static markup, which the fake DOM never parses — they
	// are checked against the page source instead, where they actually live.
	if !strings.Contains(indexHTML, `<a href="/" id="back">fleet</a>`) {
		t.Error("the masthead has no fleet crumb linking home")
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
