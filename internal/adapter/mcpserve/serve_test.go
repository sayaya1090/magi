package mcpserve

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/sayaya1090/magi/internal/core/embed"
	"github.com/sayaya1090/magi/internal/port"
)

// A store with three things written in it, one of which is about the topic under test.
func store(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	write := func(kind, name, body string) {
		t.Helper()
		d := filepath.Join(dir, kind)
		if err := os.MkdirAll(d, 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(d, name), []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	write("skills", "invoice-retry.md", `---
description: the invoice job is not idempotent on retry
firstSeen: 2026-07-01
lastSeen: 2026-08-01
observed: 4
---
Retrying a charge re-issues it. The idempotency key has to come from the request, not from a
timestamp, or two retries a second apart are two charges.
`)
	write("skills", "spacing.md", `---
description: spacing comes from the scale, never hand-written
observed: 1
---
Every margin is a token.
`)
	write("memories", "staging.md", "the staging database is restored from prod every Monday\n")
	return dir
}

func call(t *testing.T, s *Server, in string) string {
	t.Helper()
	var out strings.Builder
	if err := s.Serve(context.Background(), strings.NewReader(in), &out); err != nil {
		t.Fatal(err)
	}
	return out.String()
}

// The three-message conversation an MCP client actually has.
//
// Checked end to end rather than per method, because the failure that matters is not "tools/list
// returns two names" — it is a client that connects, asks and gets nothing it can use.
func TestAClientCanConnectAskAndReadTheAnswer(t *testing.T) {
	s := &Server{Name: "api", Role: "the billing API", Dir: store(t)}
	out := call(t, s, strings.Join([]string{
		`{"jsonrpc":"2.0","id":1,"method":"initialize","params":{}}`,
		`{"jsonrpc":"2.0","method":"notifications/initialized"}`,
		`{"jsonrpc":"2.0","id":2,"method":"tools/list"}`,
		`{"jsonrpc":"2.0","id":3,"method":"tools/call","params":{"name":"knows","arguments":{"about":"idempotency on retry"}}}`,
	}, "\n")+"\n")

	lines := strings.Split(strings.TrimSpace(out), "\n")
	// Three requests, three replies. A notification must not be answered — a client that gets a
	// reply to something it sent no id for is entitled to treat the server as broken.
	if len(lines) != 3 {
		t.Fatalf("%d replies to 3 requests and 1 notification:\n%s", len(lines), out)
	}
	var init struct {
		Result struct {
			ProtocolVersion string         `json:"protocolVersion"`
			Capabilities    map[string]any `json:"capabilities"`
		} `json:"result"`
	}
	if err := json.Unmarshal([]byte(lines[0]), &init); err != nil {
		t.Fatal(err)
	}
	if init.Result.ProtocolVersion == "" {
		t.Error("initialize answered no protocol version")
	}
	if _, ok := init.Result.Capabilities["tools"]; !ok {
		t.Errorf("the server did not declare tools; a client stops there: %v", init.Result.Capabilities)
	}

	var listed struct {
		Result struct {
			Tools []struct {
				Name        string          `json:"name"`
				Description string          `json:"description"`
				InputSchema json.RawMessage `json:"inputSchema"`
			} `json:"tools"`
		} `json:"result"`
	}
	if err := json.Unmarshal([]byte(lines[1]), &listed); err != nil {
		t.Fatal(err)
	}
	if len(listed.Result.Tools) != 3 {
		t.Fatalf("%d tools", len(listed.Result.Tools))
	}
	for _, tl := range listed.Result.Tools {
		if len(tl.InputSchema) == 0 {
			t.Errorf("%s has no input schema; a model cannot call what it cannot see the shape of", tl.Name)
		}
		// The companion's name is in every description. A model with three of these attached has
		// no other way to tell which server is which.
		if !strings.Contains(tl.Description, "api") {
			t.Errorf("%s's description does not say whose knowledge it is: %q", tl.Name, tl.Description)
		}
	}

	said := textOf(t, lines[2])
	if !strings.Contains(said, "invoice-retry") {
		t.Errorf("the entry about idempotency did not come back:\n%s", said)
	}
	// One line each, not the bodies: a search that returned four pages to answer a question that
	// might be settled by a title is a search that costs more than the answer is worth.
	if strings.Contains(said, "timestamp") {
		t.Errorf("knows returned the body; that is what detail is for:\n%s", said)
	}
	if !strings.Contains(said, "seen 4") {
		t.Errorf("how settled the lesson is did not survive:\n%s", said)
	}
	// And the one that is about something else stays out.
	if strings.Contains(said, "spacing") {
		t.Errorf("an unrelated entry came back:\n%s", said)
	}

	// Found by a word that is only in the BODY. A note titled "the staging database" whose body
	// names the table somebody asked about has to be findable by the table — searching titles
	// alone passes every other assertion here and fails the one use that matters.
	byBody := textOf(t, call(t, s, `{"jsonrpc":"2.0","id":1,"method":"tools/call","params":{"name":"knows","arguments":{"about":"timestamp"}}}`+"\n"))
	if !strings.Contains(byBody, "invoice-retry") {
		t.Errorf("a word that appears only in the body found nothing:\n%s", byBody)
	}
}

// detail returns the body, and says what exists when asked for something that does not.
func TestDetailReturnsTheWholeThingOrNamesWhatThereIs(t *testing.T) {
	s := &Server{Name: "api", Dir: store(t)}
	got := textOf(t, call(t, s, `{"jsonrpc":"2.0","id":1,"method":"tools/call","params":{"name":"detail","arguments":{"id":"invoice-retry"}}}`+"\n"))
	if !strings.Contains(got, "idempotency key has to come from the request") {
		t.Errorf("the body did not come back:\n%s", got)
	}
	if !strings.Contains(got, "api") {
		t.Errorf("the answer does not say whose it is:\n%s", got)
	}

	miss := textOf(t, call(t, s, `{"jsonrpc":"2.0","id":1,"method":"tools/call","params":{"name":"detail","arguments":{"id":"nope"}}}`+"\n"))
	// The ids it does have. A caller that mistyped one has no other way to find out, and guessing
	// again is the only move left to it.
	for _, want := range []string{"invoice-retry", "spacing", "staging"} {
		if !strings.Contains(miss, want) {
			t.Errorf("a wrong id did not list %q as an option:\n%s", want, miss)
		}
	}
}

// Nothing found is different from nothing written down, and both are different from an error.
func TestTheThreeWaysOfHavingNoAnswerReadDifferently(t *testing.T) {
	full := &Server{Name: "api", Dir: store(t)}
	got := textOf(t, call(t, full, `{"jsonrpc":"2.0","id":1,"method":"tools/call","params":{"name":"knows","arguments":{"about":"kubernetes ingress"}}}`+"\n"))
	if !strings.Contains(got, "3 entries") {
		t.Errorf("a miss should say the store is not empty, so the caller knows it asked the "+
			"wrong companion rather than a fresh one:\n%s", got)
	}

	empty := &Server{Name: "new", Dir: t.TempDir()}
	got = textOf(t, call(t, empty, `{"jsonrpc":"2.0","id":1,"method":"tools/call","params":{"name":"knows","arguments":{"about":"anything"}}}`+"\n"))
	if !strings.Contains(got, "nothing down") {
		t.Errorf("an empty store should say so:\n%s", got)
	}

	// A topic of nothing is refused rather than answered with the lot.
	line := call(t, full, `{"jsonrpc":"2.0","id":1,"method":"tools/call","params":{"name":"knows","arguments":{"about":"  "}}}`+"\n")
	var r struct {
		Result struct {
			IsError bool `json:"isError"`
		} `json:"result"`
	}
	if err := json.Unmarshal([]byte(line), &r); err != nil {
		t.Fatal(err)
	}
	if !r.Result.IsError {
		t.Error("an empty topic was answered rather than refused")
	}
}

// A method this server does not have is a JSON-RPC error; a TOOL it does not have is not.
//
// The difference is what a model does next. An unknown method means the client and server disagree
// about the protocol and there is nothing to retry; an unknown tool means this call was wrong and
// another one might not be.
func TestAnUnknownMethodAndAnUnknownToolAnswerDifferently(t *testing.T) {
	s := &Server{Name: "api", Dir: store(t)}
	var bad struct {
		Error *struct {
			Message string `json:"message"`
		} `json:"error"`
	}
	if err := json.Unmarshal([]byte(call(t, s, `{"jsonrpc":"2.0","id":1,"method":"resources/list"}`+"\n")), &bad); err != nil {
		t.Fatal(err)
	}
	if bad.Error == nil || !strings.Contains(bad.Error.Message, "resources/list") {
		t.Errorf("an unknown method did not come back as a protocol error: %+v", bad.Error)
	}

	var tool struct {
		Error  any `json:"error"`
		Result struct {
			IsError bool `json:"isError"`
		} `json:"result"`
	}
	if err := json.Unmarshal([]byte(call(t, s, `{"jsonrpc":"2.0","id":1,"method":"tools/call","params":{"name":"forget","arguments":{}}}`+"\n")), &tool); err != nil {
		t.Fatal(err)
	}
	if tool.Error != nil {
		t.Errorf("an unknown tool came back as a protocol error: %v", tool.Error)
	}
	if !tool.Result.IsError {
		t.Error("an unknown tool was not reported as a failed call either")
	}
}

func textOf(t *testing.T, line string) string {
	t.Helper()
	var r struct {
		Result struct {
			Content []struct {
				Text string `json:"text"`
			} `json:"content"`
		} `json:"result"`
	}
	if err := json.Unmarshal([]byte(strings.TrimSpace(line)), &r); err != nil {
		t.Fatalf("%v: %s", err, line)
	}
	if len(r.Result.Content) == 0 {
		t.Fatalf("no content: %s", line)
	}
	return r.Result.Content[0].Text
}

// A word that is not in the store still finds the entry, when a model is there to say they mean
// the same thing — and the answer says which kind of search it was.
//
// This is the one thing lexical ranking cannot do. "billing" and "invoice" share no characters, so
// no amount of IDF will connect them; every real question is phrased in the asker's words rather
// than in the words the answer happened to use.
func TestASemanticSearchFindsWhatTheWordsDoNot(t *testing.T) {
	// A backend that says the query is close to the invoice entry and far from the others. Faked,
	// because what is being tested here is the fusion and the reporting — the client that talks to
	// a real endpoint is tested against one in internal/core/embed.
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var in struct {
			Input []string `json:"input"`
		}
		_ = json.NewDecoder(r.Body).Decode(&in)
		data := make([]map[string]any, len(in.Input))
		for i, text := range in.Input {
			// Anything about invoices or billing points one way; everything else the other.
			v := []float32{0, 1}
			if strings.Contains(text, "invoice") || strings.Contains(text, "billing") {
				v = []float32{1, 0}
			}
			data[i] = map[string]any{"index": i, "embedding": v}
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"data": data})
	}))
	defer srv.Close()

	s := &Server{Name: "api", Dir: store(t), Embed: &embed.Client{
		BaseURL: srv.URL + "/v1", Model: "m", CacheDir: t.TempDir(), HTTP: srv.Client(),
	}}
	got := textOf(t, call(t, s, `{"jsonrpc":"2.0","id":1,"method":"tools/call","params":{"name":"knows","arguments":{"about":"billing"}}}`+"\n"))
	if !strings.Contains(got, "invoice-retry") {
		t.Errorf("a word the store never uses found nothing:\n%s", got)
	}
	if !strings.Contains(got, "meaning") {
		t.Errorf("the answer does not say it was a semantic search:\n%s", got)
	}

	// Without a model, the same question finds nothing — and says why, so nobody reads the miss as
	// "this companion knows nothing about billing".
	plain := &Server{Name: "api", Dir: store(t)}
	got = textOf(t, call(t, plain, `{"jsonrpc":"2.0","id":1,"method":"tools/call","params":{"name":"knows","arguments":{"about":"billing"}}}`+"\n"))
	if strings.Contains(got, "invoice-retry") {
		t.Errorf("a lexical search matched a word that is not in the store:\n%s", got)
	}

	// And a backend that is down costs the semantic half, not the answer.
	srv.Close()
	broken := &Server{Name: "api", Dir: store(t), Embed: &embed.Client{
		BaseURL: srv.URL + "/v1", Model: "m", CacheDir: t.TempDir(), HTTP: srv.Client(),
	}}
	got = textOf(t, call(t, broken, `{"jsonrpc":"2.0","id":1,"method":"tools/call","params":{"name":"knows","arguments":{"about":"idempotency retry"}}}`+"\n"))
	if !strings.Contains(got, "invoice-retry") {
		t.Errorf("a dead embedding endpoint took the whole search down:\n%s", got)
	}
	if !strings.Contains(got, "no embeddings") {
		t.Errorf("the search quietly became worse and did not say so:\n%s", got)
	}
}

// A companion says what it can be asked to do, not only what it has written down.
//
// The question a model has FIRST is which companion to ask, and `knows` cannot answer it: it
// searches a store, and a store that comes back empty says nothing about whether this was the right
// door. Before `about`, all a peer advertised was a name and a role clause inside two tool
// descriptions — so three written procedures and a connection to the design tooling were invisible
// from outside, and the choice of who to ask was a guess.
func TestACompanionAdvertisesWhatItCanBeAskedToDo(t *testing.T) {
	s := &Server{
		Name: "design", Role: "the design system", Dir: store(t),
		Team: "frontend", Hub: true, Workdir: "/w/design-system",
		Skills: []port.Skill{
			{Name: "spec-review", Description: "how a component spec is checked\nbefore it ships"},
			{Name: "token-audit", Description: "find hardcoded colours"},
		},
		Reach: []string{"figma", "storybook"},
	}
	got := textOf(t, call(t, s, `{"jsonrpc":"2.0","id":1,"method":"tools/call","params":{"name":"about","arguments":{}}}`+"\n"))

	for _, want := range []string{
		"design", "the design system", "frontend", "/w/design-system",
		"spec-review", "how a component spec is checked before it ships", // flattened onto one line
		"token-audit", "figma", "storybook",
	} {
		if !strings.Contains(got, want) {
			t.Errorf("about does not mention %q:\n%s", want, got)
		}
	}
	// Being the hub is worth saying: addressing the team reaches this one, so a caller that has it
	// has already reached whoever answers for the rest.
	if !strings.Contains(got, "answers for the team") {
		t.Errorf("the hub does not say so:\n%s", got)
	}
	// And it is in the tool list, or nothing would ever call it.
	list := call(t, s, `{"jsonrpc":"2.0","id":1,"method":"tools/list"}`+"\n")
	if !strings.Contains(list, `"about"`) {
		t.Errorf("about is not advertised: %s", list)
	}
}

// What a companion can be ASKED to do travels. What it would RUN to do it does not.
//
// join.go refuses to copy an [mcp] entry between workspaces because the entry is a command this
// process would later start, and "the companion I joined told me to" is not a sentence anybody
// should find in an incident report. Advertising is that same act done at a distance and over a
// pipe, so the same rule holds — and a URL is covered too, being the one that does not look
// executable and can carry an internal host and a token in a query string.
func TestAdvertisingCarriesNamesAndNeverCommands(t *testing.T) {
	s := &Server{
		Name: "design", Dir: store(t),
		Reach: []string{"figma", "storybook"},
	}
	got := textOf(t, call(t, s, `{"jsonrpc":"2.0","id":1,"method":"tools/call","params":{"name":"about","arguments":{}}}`+"\n"))

	// The Server is not even GIVEN a command or a token to leak — which is the point, and is why
	// this asserts on the type as well as on the text: a later field carrying one would have to be
	// added here first.
	for _, forbidden := range []string{"npx", "command", "http://", "https://", "TOKEN", "API_KEY"} {
		if strings.Contains(got, forbidden) {
			t.Errorf("about leaked %q:\n%s", forbidden, got)
		}
	}
	if !strings.Contains(got, "figma") {
		t.Errorf("the name did not travel either:\n%s", got)
	}
}

// A companion with nothing declared says so rather than printing an empty heading.
func TestACompanionWithNothingToAdvertiseSaysThat(t *testing.T) {
	s := &Server{Name: "scratch", Dir: store(t)}
	got := textOf(t, call(t, s, `{"jsonrpc":"2.0","id":1,"method":"tools/call","params":{"name":"about","arguments":{}}}`+"\n"))
	if !strings.Contains(got, "no skills and no external tool servers") {
		t.Errorf("a bare companion printed:\n%s", got)
	}
	if strings.Contains(got, "written procedures for") {
		t.Errorf("an empty heading was printed:\n%s", got)
	}
}
