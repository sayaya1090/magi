package provider

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// The roster is whoever recorded a shim address AND answers on it — both halves, because each
// alone writes profiles pointing at nothing: a name with no probe offers dead backends, a probe
// with no record has nowhere to look.
func TestDiscoverKeepsOnlyTheShimsThatAnswer(t *testing.T) {
	shim := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !strings.Contains(r.URL.Path, "/models") {
			http.NotFound(w, r)
			return
		}
		fmt.Fprint(w, `{"object":"list","data":[{"id":"alpha"},{"id":"beta"}]}`)
	}))
	defer shim.Close()
	addr := shim.Listener.Addr().String()
	port := addr[strings.LastIndex(addr, ":")+1:]

	dir := t.TempDir()
	pd := filepath.Join(dir, "plugin-data")
	if err := os.MkdirAll(pd, 0o755); err != nil {
		t.Fatal(err)
	}
	os.WriteFile(filepath.Join(pd, "live.json"), []byte(`{"shim_port":`+port+`}`), 0o644)
	os.WriteFile(filepath.Join(pd, "dead.json"), []byte(`{"shim_port":1}`), 0o644)
	os.WriteFile(filepath.Join(pd, "engram.json"), []byte(`{"outcomes":3}`), 0o644)

	got := Discover(context.Background(), dir, "")
	if len(got) != 1 || got[0].Name != "live" {
		t.Fatalf("the roster is %v; want exactly the shim that answers", got)
	}
	if len(got[0].Models) != 2 || got[0].Models[0] != "alpha" {
		t.Errorf("the catalog came back as %v", got[0].Models)
	}
	if !strings.HasSuffix(got[0].Base, "/v1") {
		t.Errorf("base %q is not the base_url a profile carries", got[0].Base)
	}
}

// A machine where no plugin ever stored anything is the common case, and it is an empty roster,
// not an error.
func TestNoStoresIsAnEmptyRoster(t *testing.T) {
	if got := Discover(context.Background(), t.TempDir(), ""); len(got) != 0 {
		t.Fatalf("an empty data dir produced %v", got)
	}
}

// A plugin that routes to a REMOTE gateway records provider_base instead of a shim port, and may
// record the catalog it last saw — because a gateway's /models often sits behind the auth this
// unauthenticated probe cannot send. The recorded catalog stands in only while the server itself
// answers: on a dead address it would be a picker entry pointing at nothing.
func TestARemoteGatewayIsOnTheRosterThroughItsRecord(t *testing.T) {
	gw := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusUnauthorized) // the probe carries no token, and the gateway says so
	}))
	defer gw.Close()

	dir := t.TempDir()
	pd := filepath.Join(dir, "plugin-data")
	os.MkdirAll(pd, 0o755)
	os.WriteFile(filepath.Join(pd, "codemate.json"),
		[]byte(`{"provider_base":"`+gw.URL+`/v1","provider_models":["gw-large","gw-small"]}`), 0o644)
	os.WriteFile(filepath.Join(pd, "gone.json"),
		[]byte(`{"provider_base":"http://127.0.0.1:1/v1","provider_models":["ghost"]}`), 0o644)

	got := Discover(context.Background(), dir, "")
	if len(got) != 1 || got[0].Name != "codemate" {
		t.Fatalf("the roster is %v; want the reachable gateway and not the dead one", got)
	}
	if len(got[0].Models) != 2 || got[0].Models[0] != "gw-large" {
		t.Errorf("the recorded catalog came back as %v", got[0].Models)
	}
}

// The config file's backend is on the roster as "default" — without it every switch is a one-way
// door. It is probed like the rest, and it yields when a plugin's record already names the same
// address, so a plugin that took over the default does not list the same backend twice.
func TestTheConfigBackendIsTheDefaultEntry(t *testing.T) {
	ollama := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, `{"object":"list","data":[{"id":"gpt-oss:20b"}]}`)
	}))
	defer ollama.Close()

	got := Discover(context.Background(), t.TempDir(), ollama.URL+"/v1")
	if len(got) != 1 || got[0].Name != "default" || got[0].Base != ollama.URL+"/v1" {
		t.Fatalf("the roster is %v; want the config backend as default", got)
	}
	if len(got[0].Models) != 1 || got[0].Models[0] != "gpt-oss:20b" {
		t.Errorf("the default catalog came back as %v", got[0].Models)
	}

	// A dead config backend is not offered — the picker would write a profile pointing at nothing.
	if got := Discover(context.Background(), t.TempDir(), "http://127.0.0.1:1/v1"); len(got) != 0 {
		t.Fatalf("a dead config backend still made the roster: %v", got)
	}

	// A plugin already at that address wins the name: one backend, one entry.
	dir := t.TempDir()
	pd := filepath.Join(dir, "plugin-data")
	os.MkdirAll(pd, 0o755)
	port := ollama.Listener.Addr().String()
	port = port[strings.LastIndex(port, ":")+1:]
	os.WriteFile(filepath.Join(pd, "own.json"), []byte(`{"shim_port":`+port+`}`), 0o644)
	got = Discover(context.Background(), dir, ollama.URL+"/v1")
	if len(got) != 1 || got[0].Name != "own" {
		t.Fatalf("one address produced two entries: %v", got)
	}
}
