package main

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// The provider picker lists whoever recorded a shim address AND answers on it. Both halves
// matter: a name this process knows nothing about appears by doing what the others do, and a
// recorded port whose shim is gone is left out — a picker offering a dead provider would write a
// profile pointing at nothing.
func TestTheProviderListIsWhoeverServesRightNow(t *testing.T) {
	shim := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !strings.Contains(r.URL.Path, "/models") {
			http.NotFound(w, r)
			return
		}
		fmt.Fprint(w, `{"object":"list","data":[{"id":"alpha"},{"id":"beta"}]}`)
	}))
	defer shim.Close()
	port := shim.Listener.Addr().(interface{ String() string }).String()
	port = port[strings.LastIndex(port, ":")+1:]

	dir := t.TempDir()
	pd := filepath.Join(dir, "plugin-data")
	if err := os.MkdirAll(pd, 0o755); err != nil {
		t.Fatal(err)
	}
	// One provider that answers, one whose recorded port is dead, one file that is not a store.
	os.WriteFile(filepath.Join(pd, "someback.json"), []byte(`{"shim_port":`+port+`}`), 0o644)
	os.WriteFile(filepath.Join(pd, "deadback.json"), []byte(`{"shim_port":1}`), 0o644)
	os.WriteFile(filepath.Join(pd, "engram.json"), []byte(`{"outcomes":3}`), 0o644)

	s := &server{dataDir: dir}
	w := httptest.NewRecorder()
	s.providers(w, httptest.NewRequest(http.MethodGet, "/providers", nil))
	var got []providerInfo
	if err := json.Unmarshal(w.Body.Bytes(), &got); err != nil {
		t.Fatalf("unreadable answer: %v — %s", err, w.Body.String())
	}
	if len(got) != 1 || got[0].Name != "someback" {
		t.Fatalf("the picker offers %v; want exactly the one that answers", got)
	}
	if len(got[0].Models) != 2 || got[0].Models[0] != "alpha" {
		t.Errorf("the provider's catalog came back as %v", got[0].Models)
	}
	if !strings.HasSuffix(got[0].Base, "/v1") {
		t.Errorf("base %q is not the base_url a profile carries", got[0].Base)
	}
}
