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
)

func (f *fleetFixture) servers(t *testing.T) []mcpServer {
	t.Helper()
	w := httptest.NewRecorder()
	f.srv.mcp(w, httptest.NewRequest(http.MethodGet, "/mcp", nil))
	if w.Code != http.StatusOK {
		t.Fatalf("/mcp answered %d: %s", w.Code, w.Body.String())
	}
	var out []mcpServer
	if err := json.Unmarshal(w.Body.Bytes(), &out); err != nil {
		t.Fatalf("unreadable: %v", err)
	}
	return out
}

func writeConfig(t *testing.T, dir, body string) {
	t.Helper()
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "config.toml"), []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
}

// An MCP server is where a companion's reach leaves this machine. Which ones each has is the
// question a supervisor could not answer without opening five config files.
func TestTheMCPViewSaysWhoCanReachWhat(t *testing.T) {
	f := newFleetFixture(t)
	wd := shortTempDir(t)
	f.daemonAt(wd, "api", true)
	f.session("api", wd, "x", 1, false)

	writeConfig(t, f.cfgDir, `
[mcp.shared]
url = "http://localhost:3000/mcp"
`)
	writeConfig(t, filepath.Join(wd, ".magi"), `
[mcp.figma]
command = "npx"
args = ["-y", "figma-mcp"]
env = ["FIGMA_TOKEN=super-secret"]
`)

	list := f.servers(t)
	if len(list) != 2 {
		t.Fatalf("the view has %d servers: %+v", len(list), list)
	}
	// The one every companion here inherits comes first.
	if list[0].Tier != "global" || list[0].Name != "shared" {
		t.Errorf("the first entry is %+v", list[0])
	}
	got := list[1]
	if got.Tier != "project" || got.Companion != filepath.Base(wd) {
		t.Errorf("the project entry does not say whose it is: %+v", got)
	}
	if got.Command != "npx" || len(got.Args) != 2 {
		t.Errorf("the command is not carried: %+v", got)
	}
	// A token on a page is a token in a browser history, a screenshot and a support ticket.
	if len(got.EnvNames) != 1 || got.EnvNames[0] != "FIGMA_TOKEN" {
		t.Errorf("the env is reported as %v", got.EnvNames)
	}
	b, err := json.Marshal(got)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(b), "super-secret") {
		t.Errorf("the value went over the wire: %s", b)
	}
	// And it says which file to go and look at.
	if got.File != filepath.Join(wd, ".magi", "config.toml") {
		t.Errorf("the file is %q", got.File)
	}
}

// A person at the console editing their own companion's config is the same act as opening the file
// — so it lands in the file, and it says that a running daemon did not change.
func TestAServerCanBeAddedChangedAndRemoved(t *testing.T) {
	f := newFleetFixture(t)
	wd := shortTempDir(t)
	sock := f.daemonAt(wd, "api", true)
	f.session("api", wd, "x", 1, false)
	writeConfig(t, filepath.Join(wd, ".magi"), "model = \"qwen3\"\n")

	add := post(t, f.srv, f.srv.mcp, "/mcp?d="+url.QueryEscape(sock), url.Values{
		"name": {"tickets"}, "command": {"npx"}, "args": {"-y\ntickets-mcp"},
		"env": {"TICKETS_TOKEN=abc"}})
	if add.Code != http.StatusOK {
		t.Fatalf("adding answered %d: %s", add.Code, add.Body.String())
	}
	// The one thing somebody would otherwise discover by wondering why their server never appeared.
	if !strings.Contains(add.Body.String(), "next starts") {
		t.Errorf("the answer does not say when it takes effect: %s", add.Body.String())
	}
	list := f.servers(t)
	if len(list) != 1 || list[0].Name != "tickets" || list[0].Command != "npx" {
		t.Fatalf("after adding: %+v", list)
	}
	if len(list[0].Args) != 2 || list[0].Args[1] != "tickets-mcp" {
		t.Errorf("the args came back as %v", list[0].Args)
	}

	// Changing one key leaves the rest of the file alone.
	if w := post(t, f.srv, f.srv.mcp, "/mcp?d="+url.QueryEscape(sock), url.Values{
		"name": {"tickets"}, "command": {"uvx"}, "args": {"tickets-mcp"}}); w.Code != http.StatusOK {
		t.Fatalf("changing answered %d: %s", w.Code, w.Body.String())
	}
	body, err := os.ReadFile(filepath.Join(wd, ".magi", "config.toml"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(body), `model = "qwen3"`) {
		t.Errorf("the rest of the config was lost:\n%s", body)
	}
	if list := f.servers(t); list[0].Command != "uvx" {
		t.Errorf("the change did not land: %+v", list[0])
	}

	// And removing takes the whole table.
	if w := post(t, f.srv, f.srv.mcp, "/mcp?d="+url.QueryEscape(sock), url.Values{
		"name": {"tickets"}, "delete": {"1"}}); w.Code != http.StatusOK {
		t.Fatalf("removing answered %d: %s", w.Code, w.Body.String())
	}
	if list := f.servers(t); len(list) != 0 {
		t.Errorf("after removing: %+v", list)
	}
	if body, _ := os.ReadFile(filepath.Join(wd, ".magi", "config.toml")); strings.Contains(string(body), "tickets") {
		t.Errorf("the table is still there:\n%s", body)
	}
}

// The refusals. Each one is a shape that would otherwise sit in the file looking configured.
func TestAnUnusableServerDefinitionIsRefused(t *testing.T) {
	f := newFleetFixture(t)
	wd := shortTempDir(t)
	sock := f.daemonAt(wd, "api", true)
	f.session("api", wd, "x", 1, false)
	q := "/mcp?d=" + url.QueryEscape(sock)

	for _, tc := range []struct {
		why  string
		vals url.Values
		want int
	}{
		{"neither a url nor a command", url.Values{"name": {"x"}}, http.StatusBadRequest},
		{"both, so the command would never run",
			url.Values{"name": {"x"}, "url": {"http://a"}, "command": {"npx"}}, http.StatusBadRequest},
		{"a name that is not a table header",
			url.Values{"name": {"my server"}, "command": {"npx"}}, http.StatusBadRequest},
		// The allowlist ([A-Za-z0-9_-]) shared with profiles/cron: a newline (the corruption vuln),
		// and characters the old denylist let through — each would split or break the TOML header.
		{"a newline in the name",
			url.Values{"name": {"foo\nbar"}, "command": {"npx"}}, http.StatusBadRequest},
		{"a comma in the name",
			url.Values{"name": {"a,b"}, "command": {"npx"}}, http.StatusBadRequest},
		{"a colon in the name",
			url.Values{"name": {"a:b"}, "command": {"npx"}}, http.StatusBadRequest},
		{"a non-ASCII letter in the name",
			url.Values{"name": {"café"}, "command": {"npx"}}, http.StatusBadRequest},
		{"no name at all", url.Values{"command": {"npx"}}, http.StatusBadRequest},
	} {
		if w := post(t, f.srv, f.srv.mcp, q, tc.vals); w.Code != tc.want {
			t.Errorf("%s: answered %d, want %d (%s)", tc.why, w.Code, tc.want, w.Body.String())
		}
	}
	if list := f.servers(t); len(list) != 0 {
		t.Errorf("a refused definition was written anyway: %+v", list)
	}
	// A GET does not change anything.
	if w := get(t, f.srv.mcp, "/mcp?name=x&command=npx"); w.Code != http.StatusOK {
		t.Errorf("GET /mcp answered %d", w.Code)
	}
}

// A page on another site cannot change anything here.
//
// Loopback keeps the network out and does nothing about the browser: any page the operator visits
// can POST to 127.0.0.1, a form-urlencoded body is a CORS simple request so it goes without a
// preflight, and the attacker never needs to read the reply. Measured before the guard existed —
// a page served from a different port wrote [mcp.pwned] command = "/bin/sh" into the global
// config, and a daemon runs its configured MCP servers at startup. That is arbitrary code
// execution from visiting a web page.
//
// The headers here are the ones a browser sets and script cannot: Origin is on the forbidden list,
// and Sec-Fetch-Site is fetch metadata. A request carrying neither is not a browser — curl, a
// script, the operator's own shell — and is allowed, because this server is loopback-only.
// guardsEverything asks the table whether every route refuses a cross-site POST. It is the wiring
// half of the check: the guard existing and the guard being ON every route are two facts.
func guardsEverything(s *server) bool {
	for path, h := range s.routes() {
		r := httptest.NewRequest(http.MethodPost, "http://127.0.0.1:7777"+path, nil)
		r.Host = "127.0.0.1:7777"
		r.Header.Set("Origin", "http://evil.example")
		r.Header.Set("Sec-Fetch-Site", "cross-site")
		w := httptest.NewRecorder()
		h(w, r)
		if w.Code != http.StatusForbidden {
			return false
		}
	}
	return true
}

func TestAnotherSiteCannotChangeAnything(t *testing.T) {
	// Through routes(), which is what the server mounts — not through sameSiteOnly directly. A test
	// that calls the wrapper by hand passes whether or not anything uses it, and this one was that
	// test until removing the wrapping left it green.
	var reached bool
	srv := &server{}
	table := srv.routes()
	inner := srv.handlers()["/mcp"]
	if inner == nil || table["/mcp"] == nil {
		t.Fatal("no /mcp route to guard")
	}
	// Stand a counter in for the real handler, wrapped the way routes() wraps.
	h := sameSiteOnly(func(w http.ResponseWriter, r *http.Request) { reached = true })
	if !guardsEverything(srv) {
		t.Error("some route reaches its handler without the cross-site guard")
	}

	for _, c := range []struct {
		what   string
		method string
		hdr    map[string]string
		want   int
	}{
		{"a page on another site", "POST", map[string]string{
			"Origin": "http://evil.example", "Sec-Fetch-Site": "cross-site"}, http.StatusForbidden},
		{"another port on this machine", "POST", map[string]string{
			"Origin": "http://localhost:8899", "Sec-Fetch-Site": "cross-site"}, http.StatusForbidden},
		{"a sibling site with no fetch metadata", "POST", map[string]string{
			"Origin": "http://evil.example"}, http.StatusForbidden},
		{"the console itself", "POST", map[string]string{
			"Origin": "http://127.0.0.1:7777", "Sec-Fetch-Site": "same-origin"}, http.StatusOK},
		{"curl, which sends neither", "POST", nil, http.StatusOK},
		// A cross-origin READ is the browser's problem and it already refuses to hand over the
		// reply; blocking it here would break nothing an attacker has and the page's own polling.
		{"a cross-site GET", "GET", map[string]string{"Sec-Fetch-Site": "cross-site"}, http.StatusOK},
	} {
		reached = false
		r := httptest.NewRequest(c.method, "http://127.0.0.1:7777/mcp", nil)
		r.Host = "127.0.0.1:7777"
		for k, v := range c.hdr {
			r.Header.Set(k, v)
		}
		w := httptest.NewRecorder()
		h(w, r)
		if w.Code != c.want {
			t.Errorf("%s: got %d, want %d", c.what, w.Code, c.want)
		}
		if got := reached; got != (c.want == http.StatusOK) {
			t.Errorf("%s: the handler was %sreached", c.what, map[bool]string{true: "", false: "not "}[got])
		}
	}
}
