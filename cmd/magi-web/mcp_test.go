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
